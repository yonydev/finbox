package extract

import (
	"encoding/json"
	"testing"
)

func TestUnmarshalAcceptsNumbersAndStrings(t *testing.T) {
	raw := `{"merchant":"Soriana","date":"2026-09-13","currency":"MXN","total":2601,
	         "items":[{"name":"Leche","quantity":2,"amount":"45.50"},{"name":"Pan","quantity":"1.5","amount":12},{"name":"Bolsa","quantity":null}]}`
	var ex Extraction
	if err := json.Unmarshal([]byte(raw), &ex); err != nil {
		t.Fatal(err)
	}
	if ex.Total != "2601" || ex.Merchant != "Soriana" || len(ex.Items) != 3 {
		t.Fatalf("got %+v", ex)
	}
	if ex.Items[0].Quantity != "2" || ex.Items[0].Amount != "45.50" || ex.Items[1].Quantity != "1.5" || ex.Items[1].Amount != "12" || ex.Items[2].Quantity != "" {
		t.Fatalf("items %+v", ex.Items)
	}
	if err := json.Unmarshal([]byte(`{"total":true}`), &ex); err == nil {
		t.Fatal("non-numeric total must still fail")
	}
	// Marshal shape is unchanged: strings, not numbers, so stored jsonb keeps its format.
	out, _ := json.Marshal(ex.Items[0])
	if string(out) != `{"name":"Leche","quantity":"2","amount":"45.50"}` {
		t.Fatalf("marshal %s", out)
	}
}
