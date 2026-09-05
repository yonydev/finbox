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

func listRow(shortID, merchant, currency string, amountMinor int64) store.TxnRow {
	return store.TxnRow{ID: shortID + "-0000-0000-0000-000000000000", ShortID: shortID,
		Merchant: merchant, Currency: currency, AmountMinor: amountMinor,
		OccurredOn: time.Date(2026, 8, 28, 0, 0, 0, 0, time.UTC)}
}

func TestListTableAlignsColumnsAndTotals(t *testing.T) {
	msgs := ListTable([]store.TxnRow{
		listRow("a3f2c9d1", "Walmart", "MXN", 36400),
		listRow("b4e1d0c2", "Oxxo", "MXN", 550),
	})
	if len(msgs) != 1 {
		t.Fatalf("msgs = %d: %q", len(msgs), msgs)
	}
	m := msgs[0]
	if !strings.HasPrefix(m, "<pre>") || !strings.HasSuffix(m, "</pre>") {
		t.Fatalf("not wrapped in <pre>: %q", m)
	}
	for _, want := range []string{"ID", "FECHA", "MONTO", "COMERCIO",
		"a3f2c9d1", "28/08", "$364.00", "$5.50", "TOTAL $369.50 MXN · 2"} {
		if !strings.Contains(m, want) {
			t.Errorf("table missing %q:\n%s", want, m)
		}
	}
	// amounts right-aligned in the same column: both lines end the amount
	// at the same offset, so the shorter one is left-padded
	if !strings.Contains(m, " $5.50") {
		t.Errorf("short amount not right-aligned:\n%s", m)
	}
}

func TestListTableEscapesAndTruncatesMerchant(t *testing.T) {
	msgs := ListTable([]store.TxnRow{
		listRow("a3f2c9d1", "Tacos <El Güero> de la esquina S.A.", "MXN", 36400),
	})
	m := msgs[0]
	if strings.Contains(m, "<El") {
		t.Errorf("unescaped HTML in table:\n%s", m)
	}
	if !strings.Contains(m, "…") {
		t.Errorf("long merchant not truncated:\n%s", m)
	}
	if strings.Contains(m, "esquina") {
		t.Errorf("merchant not truncated to column width:\n%s", m)
	}
}

func TestListTableMultiCurrencyTotals(t *testing.T) {
	msgs := ListTable([]store.TxnRow{
		listRow("a3f2c9d1", "Walmart", "MXN", 36400),
		listRow("b4e1d0c2", "Amazon", "USD", 1999),
	})
	if !strings.Contains(msgs[0], "TOTAL $364.00 MXN + $19.99 USD · 2") {
		t.Errorf("multi-currency total wrong:\n%s", msgs[0])
	}
}

func TestListTableEmpty(t *testing.T) {
	if msgs := ListTable(nil); len(msgs) != 0 {
		t.Fatalf("expected no messages for no rows, got %q", msgs)
	}
}
