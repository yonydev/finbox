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
