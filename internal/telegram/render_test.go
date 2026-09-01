package telegram

import (
	"strings"
	"testing"
	"time"

	"finbox/internal/store"
	"finbox/internal/validate"
)

func sampleValidated() validate.Validated {
	a := int64(18900)
	return validate.Validated{
		Merchant: "Tacos <El Güero>", OccurredOn: time.Date(2026, 8, 28, 0, 0, 0, 0, time.UTC),
		Currency: "MXN", AmountMinor: 36400,
		Items:    []validate.Item{{Position: 1, Name: "Café", AmountMinor: &a}},
		Warnings: []string{"⚠️ fecha futura"},
	}
}

func TestCardEscapesAndShowsWarnings(t *testing.T) {
	c := Card("a3f2c9d1", sampleValidated())
	for _, want := range []string{"a3f2c9d1", "Tacos &lt;El Güero&gt;", "$364.00 MXN", "⚠️ fecha futura", "Café"} {
		if !strings.Contains(c, want) {
			t.Errorf("card missing %q:\n%s", want, c)
		}
	}
	if strings.Contains(c, "<El") {
		t.Error("unescaped HTML in card")
	}
}

func TestCardTruncatesItems(t *testing.T) {
	v := sampleValidated()
	v.Items = nil
	for i := 0; i < 25; i++ {
		v.Items = append(v.Items, validate.Item{Position: i + 1, Name: "Item"})
	}
	c := Card("a3f2c9d1", v)
	if !strings.Contains(c, "… y 15 más") {
		t.Errorf("card missing truncation note:\n%s", c)
	}
}

func TestChunk(t *testing.T) {
	lines := []string{strings.Repeat("a", 30), strings.Repeat("b", 30), strings.Repeat("c", 30)}
	got := Chunk(lines, 70) // 30+1+30 fits; third overflows
	if len(got) != 2 || !strings.Contains(got[0], "a") || !strings.Contains(got[1], "c") {
		t.Fatalf("chunks = %d: %q", len(got), got)
	}
}

func TestListLinesLeadWithShortID(t *testing.T) {
	rows := []store.TxnRow{{ID: "a3f2c9d1-0000-0000-0000-000000000000", ShortID: "a3f2c9d1",
		Merchant: "Walmart", Currency: "MXN", AmountMinor: 36400,
		OccurredOn: time.Date(2026, 8, 28, 0, 0, 0, 0, time.UTC)}}
	ls := ListLines(rows)
	if len(ls) != 1 || !strings.HasPrefix(ls[0], "<code>a3f2c9d1</code>") {
		t.Fatalf("lines = %q", ls)
	}
}
