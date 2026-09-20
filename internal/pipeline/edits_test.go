package pipeline

import (
	"testing"
	"time"

	"finbox/internal/validate"
)

func final() validate.Validated {
	return validate.Validated{
		Merchant: "Farmacia 24", OccurredOn: time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC),
		Currency: "MXN", AmountMinor: 28500,
	}
}

func edits(t *testing.T, raw string, v validate.Validated) map[string][2]string {
	t.Helper()
	got := map[string][2]string{}
	for _, e := range EditsFromRaw([]byte(raw), v, "reply") {
		if e.Source != "reply" {
			t.Errorf("edit %s source = %q", e.Field, e.Source)
		}
		got[e.Field] = [2]string{e.Old, e.New}
	}
	return got
}

func TestEditsFromRawNoDiff(t *testing.T) {
	cases := map[string]string{
		"nil raw":          "",
		"empty object":     `{}`,
		"empty values":     `{"merchant":"","date":"","currency":"","total":""}`,
		"identical":        `{"merchant":"Farmacia 24","date":"2026-09-15","currency":"MXN","total":"285.00"}`,
		"unknown currency": `{"currency":"MX$"}`,
		"lowercase mxn":    `{"currency":"mxn"}`,
		"unreadable total": `{"total":"ilegible"}`,
		"broken json":      `not json`,
	}
	for name, raw := range cases {
		if got := EditsFromRaw([]byte(raw), final(), "reply"); len(got) != 0 {
			t.Errorf("%s: want no edits, got %+v", name, got)
		}
	}
}

func TestEditsFromRawAllFields(t *testing.T) {
	got := edits(t, `{"merchant":"Farmacia","date":"2026-09-14","currency":"USD","total":"300.00"}`, final())
	want := map[string][2]string{
		"merchant": {"Farmacia", "Farmacia 24"},
		"date":     {"2026-09-14", "2026-09-15"},
		"currency": {"USD", "MXN"},
		"total":    {"30000", "28500"},
	}
	for f, w := range want {
		if got[f] != w {
			t.Errorf("%s = %v, want %v", f, got[f], w)
		}
	}
	if len(got) != 4 {
		t.Errorf("edits = %+v", got)
	}
}

func TestEditsFromRawTotalUsesRawCurrency(t *testing.T) {
	v := final()
	v.Currency, v.AmountMinor = "JPY", 285 // corrected MXN 285.50 → ¥285
	got := edits(t, `{"currency":"MXN","total":"285.50"}`, v)
	if got["total"] != [2]string{"28550", "285"} {
		t.Errorf("total = %v", got["total"])
	}
}
