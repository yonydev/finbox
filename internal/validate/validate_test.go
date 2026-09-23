package validate

import (
	"strings"
	"testing"
	"time"

	"finbox/internal/extract"
)

func TestScrub(t *testing.T) {
	cases := []struct{ in, want string }{
		// 4111111111111111 passes Luhn → redacted
		{"pagado con 4111111111111111 gracias", "pagado con [redactado] gracias"},
		// separator-tolerant
		{"tarjeta 4111 1111 1111 1111", "tarjeta [redactado]"},
		// 18 digits = CLABE shape → redacted regardless of Luhn
		{"CLABE 032180000118359719", "CLABE [redactado]"},
		// Luhn-failing 16 digits kept (folio, not a card)
		{"folio 1234567890123456", "folio 1234567890123456"},
		// short digit runs untouched
		{"total 364.00 ref 12345", "total 364.00 ref 12345"},
	}
	for _, tc := range cases {
		if got := Scrub(tc.in); got != tc.want {
			t.Errorf("Scrub(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func base() extract.Extraction {
	return extract.Extraction{
		Merchant: "Walmart", Date: "2026-08-28", Currency: "MXN", Total: "364.00",
		Items: []extract.Item{{Name: "Café", Quantity: "1", Amount: "189.00"}, {Name: "Leche", Amount: "175.00"}},
	}
}

var now = time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)

func TestRunHappyPath(t *testing.T) {
	v, err := Run(base(), now, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if v.AmountMinor != 36400 || v.Merchant != "Walmart" || len(v.Items) != 2 {
		t.Fatalf("v = %+v", v)
	}
	if len(v.Warnings) != 0 {
		t.Fatalf("warnings = %v", v.Warnings)
	}
	if v.Items[1].Position != 2 {
		t.Errorf("positions not assigned: %+v", v.Items)
	}
}

func TestRunSoftFlags(t *testing.T) {
	ex := base()
	ex.Currency = ""              // → assumed MXN warning
	ex.Items[1].Amount = "100.00" // 189+100 ≠ 364 → sum warning
	ex.Date = "2026-09-15"        // future vs now → warning
	v, err := Run(ex, now, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(v.Warnings, "|")
	for _, frag := range []string{"asumí MXN", "suman", "fecha futura"} {
		if !strings.Contains(joined, frag) {
			t.Errorf("warnings %q missing %q", joined, frag)
		}
	}
}

func TestUnknownCurrencyAssumesMXN(t *testing.T) {
	ex := base()
	ex.Currency = "MX$" // what a digital Uber receipt's text layer prints
	v, err := Run(ex, now, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if v.Currency != "MXN" {
		t.Errorf("currency = %q, want MXN", v.Currency)
	}
	if !strings.Contains(strings.Join(v.Warnings, "|"), "no reconocida") {
		t.Errorf("warnings %v missing unknown-currency warning", v.Warnings)
	}
}

func TestLongFieldsCapped(t *testing.T) {
	ex := base()
	ex.Merchant = strings.Repeat("á", 500)
	ex.Items[0].Name = strings.Repeat("é", 500)
	v, err := Run(ex, now, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if n := len([]rune(v.Merchant)); n != 120 {
		t.Errorf("merchant runes = %d, want 120", n)
	}
	if n := len([]rune(v.Items[0].Name)); n != 80 {
		t.Errorf("item name runes = %d, want 80", n)
	}
}

func TestSumSkippedWhenUnpriced(t *testing.T) {
	ex := base()
	ex.Items[1].Amount = "" // unpriced line → sum check must not fire
	v, err := Run(ex, now, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range v.Warnings {
		if strings.Contains(w, "suman") {
			t.Fatalf("sum warning fired with unpriced item: %v", v.Warnings)
		}
	}
}

func TestQuantityOverflowIgnoredNotWrapped(t *testing.T) {
	ex := base()
	ex.Items[0].Quantity = "99999999999999999999" // overflows int64 milli units
	v, err := Run(ex, now, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if v.Items[0].QuantityMilli != nil {
		t.Errorf("quantity overflow should be silently dropped, got %v", *v.Items[0].QuantityMilli)
	}
}

func TestRunHardFailures(t *testing.T) {
	for _, mut := range []func(*extract.Extraction){
		func(e *extract.Extraction) { e.Total = "" },
		func(e *extract.Extraction) { e.Total = "-5.00" },
		func(e *extract.Extraction) { e.Date = "28/08/2026" },
		func(e *extract.Extraction) { e.Merchant = "" },
	} {
		ex := base()
		mut(&ex)
		if _, err := Run(ex, now, time.UTC); err == nil {
			t.Errorf("want hard failure for %+v", ex)
		}
	}
}

func TestNegativeDiscountCountsTowardSum(t *testing.T) {
	ex := base() // 189 + 175 = 364
	ex.Items = append(ex.Items, extract.Item{Name: "Descuento", Amount: "-50.00"})
	ex.Total = "314.00"
	v, err := Run(ex, now, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if v.Items[2].AmountMinor == nil || *v.Items[2].AmountMinor != -5000 {
		t.Fatalf("discount must be a priced negative item: %+v", v.Items[2])
	}
	for _, w := range v.Warnings {
		if strings.Contains(w, "suman") {
			t.Fatalf("items sum to the total with the discount; warning fired: %v", v.Warnings)
		}
	}
}

func TestRunCanon(t *testing.T) {
	ex := base()
	ex.Merchant = "  WALMART  S.A. DE C.V. "
	v, err := Run(ex, now, time.UTC)
	if err != nil || v.Merchant != "WALMART  S.A. DE C.V." || v.MerchantCanon != "WALMART" {
		t.Fatalf("normalized: %q / %q %v", v.Merchant, v.MerchantCanon, err)
	}
	// a stored correction wins over the normalizer, whitespace-only does not
	ex.MerchantCanon = "Mi Walmart 4111 1111 1111 1111"
	if v, _ := Run(ex, now, time.UTC); v.MerchantCanon != "Mi Walmart [redactado]" {
		t.Errorf("override = %q", v.MerchantCanon)
	}
	ex.MerchantCanon = "   "
	if v, _ := Run(ex, now, time.UTC); v.MerchantCanon != "WALMART" {
		t.Errorf("blank override = %q", v.MerchantCanon)
	}
}

func TestRunCategoryProvenance(t *testing.T) {
	ex := base()
	ex.Category = "Súper"
	v, err := Run(ex, now, time.UTC)
	if err != nil || v.Category != "super" || v.CategorySource != "human" {
		t.Fatalf("v = %+v err = %v", v, err)
	}
	ex.Category = "comida" // off-list: dropped, never stamped
	v, err = Run(ex, now, time.UTC)
	if err != nil || v.Category != "" || v.CategorySource != "" {
		t.Fatalf("off-list kept: %+v %v", v, err)
	}
}
