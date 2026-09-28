package correct

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	mx := time.FixedZone("MX", -6*3600)
	now := time.Date(2026, time.September, 18, 12, 0, 0, 0, mx)
	cases := []struct {
		in   string
		want Fields
	}{
		// bare values, classified by shape
		{"15", Fields{Total: "15"}},
		{"2026", Fields{Total: "2026"}},
		{"285.00", Fields{Total: "285.00"}},
		{"$285", Fields{Total: "285"}},
		{"$ 285", Fields{Total: "285"}},
		{"1,300.50", Fields{Total: "1300.50"}},
		{"-120", Fields{Total: "-120"}},
		{"15/09", Fields{Date: "2026-09-15"}},
		{"15-09", Fields{Date: "2026-09-15"}},
		{"15-09-2026", Fields{Date: "2026-09-15"}},
		{"2026-09-15", Fields{Date: "2026-09-15"}},
		{"2026/09/15", Fields{Date: "2026-09-15"}},
		{"31/12", Fields{Date: "2025-12-31"}},
		{"hoy", Fields{Date: "2026-09-18"}},
		{"AYER", Fields{Date: "2026-09-17"}},
		{"antier", Fields{Date: "2026-09-16"}},
		{"USD", Fields{Currency: "USD"}},
		{"usd", Fields{Currency: "USD"}},
		{"Oxxo", Fields{Merchant: "Oxxo"}},
		{"OXXO Reforma", Fields{Merchant: "OXXO Reforma"}},
		{"Soriana Híper", Fields{Merchant: "Soriana Híper"}},
		{"Sam", Fields{Merchant: "Sam"}}, // 3 letters but not a currency
		{"15/09 285", Fields{Date: "2026-09-15", Total: "285"}},
		{"USD 285", Fields{Currency: "USD", Total: "285"}},
		// keywords
		{"comercio 7 Eleven", Fields{Merchant: "7 Eleven"}},
		{"tienda Farmacia 24", Fields{Merchant: "Farmacia 24"}},
		{"comercio 365", Fields{Merchant: "365"}},
		{"moneda eur", Fields{Currency: "EUR"}},
		{"Día 15/09", Fields{Date: "2026-09-15"}},
		{"total 285 fecha 15/09", Fields{Total: "285", Date: "2026-09-15"}},
		{"FECHA AYER total 285", Fields{Date: "2026-09-17", Total: "285"}},
		{"fecha ayer comercio Soriana Híper", Fields{Date: "2026-09-17", Merchant: "Soriana Híper"}},
		{"comercio Oxxo total 285", Fields{Merchant: "Oxxo", Total: "285"}},
		{"Oxxo total 285", Fields{Merchant: "Oxxo", Total: "285"}},
		{"total 285 moneda jpy", Fields{Total: "285", Currency: "JPY"}},
		{"categoria super", Fields{Category: "super"}},
		{"cat Educación", Fields{Category: "educacion"}},
		{"categoría HOGAR", Fields{Category: "hogar"}},
		{"total 285 categoria super", Fields{Total: "285", Category: "super"}},
		{"super", Fields{Merchant: "super"}}, // bare word is still a merchant
	}
	for _, tc := range cases {
		got, err := Parse(tc.in, now, "")
		if err != nil {
			t.Errorf("Parse(%q) err=%v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("Parse(%q) = %+v, want %+v", tc.in, got, tc.want)
		}
	}
}

func TestParseRejects(t *testing.T) {
	now := time.Date(2026, time.September, 18, 12, 0, 0, 0, time.UTC)
	unparseable := []string{
		"", "tienda", "fecha", "comercio", "moneda",
		"Farmacia 24", "el total era 285.50", "creo que si esta bien",
		"total 285 monto 300", "285 300", "7Eleven", "fecha 15 09",
	}
	for _, in := range unparseable {
		if _, err := Parse(in, now, ""); !errors.Is(err, ErrUnparseable) {
			t.Errorf("Parse(%q) err=%v, want ErrUnparseable", in, err)
		}
	}
	// specific, teaching errors from the underlying packages
	for _, in := range []string{"0", "300,50", "moneda BTC", "32/09", "15/13", "29/02", "total 285.5 moneda jpy", "abc/def"} {
		_, err := Parse(in, now, "")
		if err == nil || errors.Is(err, ErrUnparseable) {
			t.Errorf("Parse(%q) err=%v, want a specific error", in, err)
		}
	}
	// the receipt's currency governs the total's decimals
	if _, err := Parse("285.5", now, "JPY"); err == nil {
		t.Error("285.5 in JPY should fail")
	}
}

func TestParseUnknownCategory(t *testing.T) {
	now := time.Date(2026, time.September, 18, 12, 0, 0, 0, time.UTC)
	_, err := Parse("categoria comida", now, "")
	if err == nil || errors.Is(err, ErrUnparseable) {
		t.Fatalf("err = %v, want a teaching error", err)
	}
	if !strings.Contains(err.Error(), "restaurantes") {
		t.Errorf("error must list the options: %v", err)
	}
	// ponytail: "cat" is a keyword, so a merchant starting with it mis-parses
	if f, err := Parse("comercio Cat Cafe", now, ""); err == nil {
		t.Errorf("known ceiling changed: %+v", f)
	}
}

func TestParseAdd(t *testing.T) {
	mx := time.FixedZone("MX", -6*3600)
	now := time.Date(2026, time.September, 18, 12, 0, 0, 0, mx)
	cases := []struct {
		in   string
		want Fields
	}{
		{"500 Limpieza Paty", Fields{Total: "500", Merchant: "Limpieza Paty"}},
		{"120 Uber fecha ayer", Fields{Total: "120", Merchant: "Uber", Date: "2026-09-17"}},
		{"350 Soriana categoria super", Fields{Total: "350", Merchant: "Soriana", Category: "super"}},
		{"-120 Uber", Fields{Total: "-120", Merchant: "Uber"}},
		{"$1,300.50 Oxxo moneda USD", Fields{Total: "1300.50", Merchant: "Oxxo", Currency: "USD"}},
		{"24 comercio Farmacia 24", Fields{Total: "24", Merchant: "Farmacia 24"}},
		// keywords in any order
		{"500 categoria servicios comercio Limpieza Paty", Fields{Total: "500", Merchant: "Limpieza Paty", Category: "servicios"}},
		{"500 Limpieza Paty fecha 15/09 categoria hogar", Fields{Total: "500", Merchant: "Limpieza Paty", Date: "2026-09-15", Category: "hogar"}},
	}
	for _, tc := range cases {
		got, err := ParseAdd(tc.in, now)
		if err != nil {
			t.Errorf("ParseAdd(%q) err=%v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ParseAdd(%q) = %+v, want %+v", tc.in, got, tc.want)
		}
	}
}

func TestParseAddRejects(t *testing.T) {
	now := time.Date(2026, time.September, 18, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		in   string
		want error
	}{
		{"", ErrNoAmount},
		{"Limpieza Paty", ErrNoAmount},
		{"Limpieza Paty 500", ErrNoAmount}, // the amount goes first, only
		{"ayer 500 Uber", ErrNoAmount},
		{"500", ErrNoMerchant},
		{"500 fecha ayer", ErrNoMerchant},
		{"24 Farmacia 24", ErrTwoNumbers},
		{"500 300 Uber", ErrTwoNumbers},
		{"500 Uber 15/09", ErrUnparseable}, // a bare date mixed with words
		{"500 pague la limpieza de la casa", ErrUnparseable},
		{"500 7Eleven", ErrUnparseable},
	} {
		if _, err := ParseAdd(tc.in, now); !errors.Is(err, tc.want) {
			t.Errorf("ParseAdd(%q) err=%v, want %v", tc.in, err, tc.want)
		}
	}
	// specific, teaching errors travel through untouched
	for _, in := range []string{"0 Uber", "500 Uber categoria comida", "500 Uber moneda BTC", "500 Uber fecha 32/09"} {
		_, err := ParseAdd(in, now)
		if err == nil || errors.Is(err, ErrUnparseable) || errors.Is(err, ErrNoAmount) ||
			errors.Is(err, ErrNoMerchant) || errors.Is(err, ErrTwoNumbers) {
			t.Errorf("ParseAdd(%q) err=%v, want a specific error", in, err)
		}
	}
}
