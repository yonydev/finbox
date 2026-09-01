package extract

import "errors"

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
