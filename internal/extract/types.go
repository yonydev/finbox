package extract

import (
	"encoding/json"
	"errors"
)

// ErrNonRetryable marks extraction failures where retrying cannot help
// (bad API key, exhausted quota, bad request). Implementations wrap it;
// the pipeline checks it WITHOUT importing any implementation package.
var ErrNonRetryable = errors.New("extract: non-retryable")

type Item struct {
	Name     string `json:"name"`
	Quantity string `json:"quantity,omitempty"` // decimal string, e.g. "1.5"
	Amount   string `json:"amount,omitempty"`   // LINE TOTAL, decimal string; empty = unpriced
}

type Extraction struct {
	Merchant string `json:"merchant"`
	Date     string `json:"date"`     // YYYY-MM-DD
	Currency string `json:"currency"` // ISO 4217 or "" when unreadable
	Total    string `json:"total"`    // decimal string
	Items    []Item `json:"items"`
}

type Result struct {
	Extraction       Extraction
	Model            string
	RawJSON          []byte // the post-parse, pre-scrub document (scrubbed before persisting)
	PromptTokens     int
	CompletionTokens int
}

// numStr accepts both "12.50" and 12.5: the models return decimals as JSON
// numbers now and then, and a strict string field turned that into a dead-end
// ("no pude leer el ticket" after 3 retries). Kept as string on the struct so
// nothing downstream changes.
type numStr string

func (n *numStr) UnmarshalJSON(b []byte) error {
	if len(b) > 0 && b[0] == '"' {
		var s string
		err := json.Unmarshal(b, &s)
		*n = numStr(s)
		return err
	}
	var num json.Number // also swallows null (left empty)
	err := json.Unmarshal(b, &num)
	*n = numStr(num)
	return err
}

func (it *Item) UnmarshalJSON(b []byte) error {
	var w struct {
		Name     string `json:"name"`
		Quantity numStr `json:"quantity"`
		Amount   numStr `json:"amount"`
	}
	if err := json.Unmarshal(b, &w); err != nil {
		return err
	}
	*it = Item{Name: w.Name, Quantity: string(w.Quantity), Amount: string(w.Amount)}
	return nil
}

func (e *Extraction) UnmarshalJSON(b []byte) error {
	type plain Extraction // no methods: avoids recursing into this UnmarshalJSON
	var w struct {
		plain
		Total numStr `json:"total"` // shallower than plain.Total, so it wins the tag
	}
	if err := json.Unmarshal(b, &w); err != nil {
		return err
	}
	*e = Extraction(w.plain)
	e.Total = string(w.Total)
	return nil
}
