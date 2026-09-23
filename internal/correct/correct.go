// Package correct parses a reply-correction ("15/09", "total 285 fecha ayer",
// "comercio Farmacia 24") into the fields it names. Deterministic, no LLM:
// a bare value is classified by shape, keywords disambiguate or combine.
package correct

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"finbox/internal/category"
	"finbox/internal/daytok"
	"finbox/internal/messages"
	"finbox/internal/money"
)

// Fields holds the corrected values as strings; empty = not mentioned.
// Total is a clean decimal string (no "$" or thousands commas), Date is
// YYYY-MM-DD, Currency is upper-case and known to money.
// Category is a category.Slugs entry.
type Fields struct{ Total, Merchant, Date, Currency, Category string }

// ErrUnparseable is the teaching error: the text is prose, ambiguous, or a
// keyword without a value. Nothing else is inferred from it.
var ErrUnparseable = errors.New("no te entendí")

var keywords = map[string]string{
	"total": "total", "monto": "total",
	"fecha": "date", "dia": "date", "día": "date",
	"comercio": "merchant", "tienda": "merchant",
	"moneda": "currency",
	// ponytail: "cat" is a keyword, so `comercio Cat Café` mis-parses — same
	// ceiling "dia" already has (`comercio Buen Dia`); drop the short alias if
	// it ever bites
	"categoria": "category", "categoría": "category", "cat": "category",
}

var numberTok = regexp.MustCompile(`^-?\$?[\d.,]+$`)

// Parse resolves text against now (already in the user's location) and the
// receipt's currency ("" = MXN), which the total is validated against.
func Parse(text string, now time.Time, currency string) (Fields, error) {
	toks := strings.Fields(strings.ReplaceAll(text, "$ ", "$")) // "$ 285" → "$285"
	if len(toks) == 0 {
		return Fields{}, ErrUnparseable
	}
	raw := map[string]string{}
	set := func(field, val string) error {
		if _, dup := raw[field]; dup || val == "" {
			return ErrUnparseable
		}
		raw[field] = val
		return nil
	}
	i := 0
	for i < len(toks) && keywords[strings.ToLower(toks[i])] == "" {
		i++
	}
	if err := bare(toks[:i], set); err != nil {
		return Fields{}, err
	}
	for i < len(toks) { // each keyword owns the tokens up to the next keyword
		field := keywords[strings.ToLower(toks[i])]
		j := i + 1
		for j < len(toks) && keywords[strings.ToLower(toks[j])] == "" {
			j++
		}
		vals := toks[i+1 : j]
		if field != "merchant" && len(vals) != 1 {
			return Fields{}, ErrUnparseable
		}
		if err := set(field, strings.Join(vals, " ")); err != nil {
			return Fields{}, err
		}
		i = j
	}
	return normalize(raw, now, currency)
}

// bare classifies keyword-less tokens one by one. Words only become a
// merchant when nothing else is mixed in: "Farmacia 24" is ambiguous
// (merchant with a number, or merchant + total?) and must use a keyword.
func bare(toks []string, set func(string, string) error) error {
	var words []string
	for _, t := range toks {
		lo := strings.ToLower(t)
		var err error
		switch {
		case lo == "hoy" || lo == "ayer" || lo == "antier" ||
			strings.Contains(t, "/") || strings.Contains(strings.TrimPrefix(t, "-"), "-"):
			err = set("date", t)
		case numberTok.MatchString(t):
			err = set("total", t)
		case len([]rune(t)) == 3 && money.Known(t):
			err = set("currency", t)
		case strings.ContainsAny(t, "0123456789"):
			err = ErrUnparseable
		default:
			words = append(words, t)
		}
		if err != nil {
			return err
		}
	}
	if len(words) == 0 {
		return nil
	}
	if len(words) > 4 || len(words) != len(toks) {
		return ErrUnparseable
	}
	return set("merchant", strings.Join(words, " "))
}

func normalize(raw map[string]string, now time.Time, currency string) (Fields, error) {
	var f Fields
	if c, ok := raw["currency"]; ok {
		f.Currency = strings.ToUpper(c)
		if !money.Known(f.Currency) {
			return Fields{}, fmt.Errorf("moneda no soportada %q (soportadas: MXN, USD, EUR, JPY)", c)
		}
		currency = f.Currency
	}
	if currency == "" {
		currency = "MXN"
	}
	if t, ok := raw["total"]; ok {
		minor, err := money.ParseMinor(t, currency)
		if err != nil {
			return Fields{}, err
		}
		if minor == 0 {
			return Fields{}, fmt.Errorf("el total no puede ser 0")
		}
		f.Total = strings.NewReplacer("$", "", ",", "").Replace(t)
	}
	if d, ok := raw["date"]; ok {
		day, err := daytok.Parse(d, now)
		if err != nil {
			return Fields{}, err
		}
		f.Date = day.Format("2006-01-02")
	}
	if c, ok := raw["category"]; ok {
		slug, valid := category.Parse(c)
		if !valid {
			return Fields{}, fmt.Errorf(messages.UnknownCategory, c, strings.Join(category.Slugs, ", "))
		}
		f.Category = slug
	}
	f.Merchant = raw["merchant"]
	return f, nil
}
