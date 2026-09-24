package telegram

import (
	"strings"
	"testing"
	"time"

	"finbox/internal/messages"
	"finbox/internal/store"
	"finbox/internal/validate"
)

func sampleValidated() validate.Validated {
	a := int64(18900)
	return validate.Validated{
		Merchant: "Tacos <El Güero> S.A. DE C.V.", MerchantCanon: "Tacos <El Güero>",
		OccurredOn: time.Date(2026, 8, 28, 0, 0, 0, 0, time.UTC),
		Currency:   "MXN", AmountMinor: 36400,
		Items:    []validate.Item{{Position: 1, Name: "Café", AmountMinor: &a}},
		Warnings: []string{"⚠️ fecha futura"},
	}
}

func TestCardEscapesAndShowsWarnings(t *testing.T) {
	c := Card("a3f2c9d1", sampleValidated(), false)
	for _, want := range []string{"a3f2c9d1", "<b>Tacos &lt;El Güero&gt;</b>", "$364.00 MXN", "⚠️ fecha futura", "Café",
		"<i>en el ticket: Tacos &lt;El Güero&gt; S.A. DE C.V.</i>"} { // canon ≠ raw: the ticket name stays one escaped line
		if !strings.Contains(c, want) {
			t.Errorf("card missing %q:\n%s", want, c)
		}
	}
	if strings.Contains(c, "<El") {
		t.Error("unescaped HTML in card")
	}
	if strings.Contains(c, "✏️") {
		t.Error("unedited card got a marker")
	}
}

func TestPendingCardHintsAndMarksEdits(t *testing.T) {
	c := PendingCard("a3f2c9d1", sampleValidated(), true)
	if !strings.Contains(c, messages.ReplyHint) {
		t.Errorf("pending card missing reply hint:\n%s", c)
	}
	if !strings.Contains(c, "</b> ✏️\n") {
		t.Errorf("edited card missing marker on the header line:\n%s", c)
	}
	if strings.Contains(SavedCard("a3f2c9d1", sampleValidated(), true), messages.ReplyHint) {
		t.Error("saved card must not invite more corrections")
	}
}

func TestCardTruncatesItems(t *testing.T) {
	v := sampleValidated()
	v.Items = nil
	for i := 0; i < 25; i++ {
		v.Items = append(v.Items, validate.Item{Position: i + 1, Name: "Item"})
	}
	c := Card("a3f2c9d1", v, false)
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
		Merchant: merchant, MerchantCanon: merchant, Currency: currency, AmountMinor: amountMinor,
		OccurredOn: time.Date(2026, 8, 28, 0, 0, 0, 0, time.UTC)}
}

func TestListTableAlignsColumnsAndTotals(t *testing.T) {
	m := ListTable([]store.TxnRow{
		listRow("a3f2c9d1", "Walmart", "MXN", 36400),
		listRow("b4e1d0c2", "Oxxo", "MXN", 550),
	})
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
	m := ListTable([]store.TxnRow{
		listRow("a3f2c9d1", "Tacos <El Güero> de la esquina S.A.", "MXN", 36400),
	})
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

func TestListTableMarksEditedRows(t *testing.T) {
	edited := listRow("a3f2c9d1", "Walmart", "MXN", 36400)
	edited.Edited = true
	m := ListTable([]store.TxnRow{edited, listRow("b4e1d0c2", "Oxxo", "MXN", 550)})
	if !strings.Contains(m, "Walmart ✏️") {
		t.Errorf("edited row missing marker:\n%s", m)
	}
	if strings.Contains(m, "Oxxo ✏️") {
		t.Errorf("unedited row got a marker:\n%s", m)
	}
	if !strings.Contains(m, "✏️ 1 (50%)") {
		t.Errorf("footer missing edited stat:\n%s", m)
	}
}

func TestListTableNoEditedStatWhenClean(t *testing.T) {
	m := ListTable([]store.TxnRow{listRow("a3f2c9d1", "Walmart", "MXN", 36400)})
	if strings.Contains(m, "✏️") {
		t.Errorf("clean list should not mention edits:\n%s", m)
	}
}

func TestListTableMultiCurrencyTotals(t *testing.T) {
	m := ListTable([]store.TxnRow{
		listRow("a3f2c9d1", "Walmart", "MXN", 36400),
		listRow("b4e1d0c2", "Amazon", "USD", 1999),
	})
	if !strings.Contains(m, "TOTAL $364.00 MXN + $19.99 USD · 2") {
		t.Errorf("multi-currency total wrong:\n%s", m)
	}
}

func TestListTableEmpty(t *testing.T) {
	if m := ListTable(nil); m != "" {
		t.Fatalf("expected empty message for no rows, got %q", m)
	}
}

func TestCardHidesTicketLineWhenCanonIsRaw(t *testing.T) {
	v := sampleValidated()
	v.Merchant = v.MerchantCanon
	if c := Card("a3f2c9d1", v, false); strings.Contains(c, "en el ticket") {
		t.Errorf("unchanged name should not repeat itself:\n%s", c)
	}
	// and a row that predates the canon column still shows its raw name
	v.Merchant, v.MerchantCanon = "Tacos", ""
	if c := Card("a3f2c9d1", v, false); !strings.Contains(c, "<b>Tacos</b>") || strings.Contains(c, "en el ticket") {
		t.Errorf("empty canon must fall back to raw:\n%s", c)
	}
}

func TestCardCategoryLine(t *testing.T) {
	if c := Card("a3f2c9d1", sampleValidated(), false); !strings.Contains(c, "🏷 categoría: sin categoría\n") {
		t.Errorf("uncategorized card missing the 🏷 line:\n%s", c)
	}
	v := sampleValidated()
	v.Category, v.CategorySource = "educacion", "human"
	if c := Card("a3f2c9d1", v, false); !strings.Contains(c, "🏷 categoría: educación\n") {
		t.Errorf("labeled card missing the 🏷 line:\n%s", c)
	}
	// the extractor's guess is flagged on the pending card, never once confirmed
	v.CategorySource = "llm"
	if c := Card("a3f2c9d1", v, false); !strings.Contains(c, "🏷 categoría: educación · sugerida\n") {
		t.Errorf("llm card missing the suffix:\n%s", c)
	}
	if c := SavedCard("a3f2c9d1", v, false); strings.Contains(c, "sugerida") {
		t.Errorf("saved card still says sugerida:\n%s", c)
	}
}

func TestMonthSummaryTable(t *testing.T) {
	got := MonthSummary(2026, time.August, []store.CategoryTotal{
		{Category: "super", Currency: "MXN", AmountMinor: 5000, Count: 1},
		{Category: "", Currency: "MXN", AmountMinor: 48400, Count: 2},
	})
	want := "<b>2026-08</b> · $534.00 MXN · 3 gastos\n<pre>" +
		"súper               $50.00   1\n" +
		"sin categoría      $484.00   2</pre>"
	if got != want {
		t.Errorf("MonthSummary =\n%s\nwant\n%s", got, want)
	}
	// a second currency: per-currency header totals and the code on each row
	mixed := MonthSummary(2026, time.August, []store.CategoryTotal{
		{Category: "", Currency: "MXN", AmountMinor: 48400, Count: 2},
		{Category: "", Currency: "USD", AmountMinor: 4550, Count: 1},
	})
	if !strings.Contains(mixed, "$484.00 MXN + $45.50 USD · 3 gastos") || !strings.Contains(mixed, "$45.50   1 USD") {
		t.Errorf("mixed =\n%s", mixed)
	}
	if MonthSummary(2026, time.August, nil) != "2026-08: sin gastos" {
		t.Error("empty month")
	}
}
