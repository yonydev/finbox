package money

import (
	"fmt"
	"math"
	"regexp"
	"strings"
)

var exponents = map[string]int{"MXN": 2, "USD": 2, "EUR": 2, "JPY": 0}

func Exponent(currency string) int {
	if e, ok := exponents[strings.ToUpper(currency)]; ok {
		return e
	}
	return 2
}

// Known reports whether the currency is in the exponent table. Callers taking
// user input must reject unknown codes: defaulting their exponent would store
// amounts at the wrong scale (e.g. CLP has no minor units).
func Known(currency string) bool {
	_, ok := exponents[strings.ToUpper(currency)]
	return ok
}

// thousandsForm is the only comma layout ParseMinor accepts ("1,300.50").
var thousandsForm = regexp.MustCompile(`^-?\d{1,3}(,\d{3})+(\.\d*)?$`)

// ParseMinor converts a decimal string to integer minor units using
// integer math only. Tolerates "$", spaces and "," thousands separators.
func ParseMinor(s, currency string) (int64, error) {
	return parseFixedPoint(s, Exponent(currency))
}

// ParseMilli converts a decimal string ("1.5") to integer milli-units
// (1500) with up to 3 decimals, integer math only. Used for quantities.
func ParseMilli(s string) (int64, error) {
	return parseFixedPoint(s, 3)
}

// parseFixedPoint scales a decimal string to an integer with `decimals`
// fraction digits, guarded against int64 overflow.
func parseFixedPoint(s string, decimals int) (int64, error) {
	clean := strings.ReplaceAll(strings.TrimSpace(s), "$", "")
	if strings.Contains(clean, ",") {
		// A comma is accepted only as a thousands separator. A decimal comma
		// ("300,50") must fail loudly — stripping it would store a 100x amount.
		if !thousandsForm.MatchString(clean) {
			return 0, fmt.Errorf("monto inválido: %q (usa punto decimal, ej. 300.50)", s)
		}
		clean = strings.ReplaceAll(clean, ",", "")
	}
	// Interior spaces are no longer stripped: "300 50" falls through to the
	// digit loop and errors instead of silently gluing into 30050.
	if clean == "" {
		return 0, fmt.Errorf("monto vacío")
	}
	neg := false
	if clean[0] == '-' {
		neg, clean = true, clean[1:]
	}
	intPart, fracPart := clean, ""
	if i := strings.IndexByte(clean, '.'); i >= 0 {
		intPart, fracPart = clean[:i], clean[i+1:]
	}
	if intPart == "" && fracPart == "" {
		return 0, fmt.Errorf("monto inválido: %q", s)
	}
	if len(fracPart) > decimals {
		return 0, fmt.Errorf("%q tiene más de %d decimales", s, decimals)
	}
	for len(fracPart) < decimals {
		fracPart += "0"
	}
	var n int64
	for _, r := range intPart + fracPart {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("monto inválido: %q", s)
		}
		digit := int64(r - '0')
		// Overflow guard: check if n*10 + digit would exceed MaxInt64
		if n > (math.MaxInt64-digit)/10 {
			return 0, fmt.Errorf("monto demasiado grande")
		}
		n = n*10 + digit
	}
	if neg {
		n = -n
	}
	return n, nil
}

func Format(minor int64, currency string) string {
	exp := Exponent(currency)
	sign := ""
	if minor < 0 {
		sign, minor = "-", -minor
	}
	if exp == 0 {
		return fmt.Sprintf("%s$%d", sign, minor)
	}
	pow := int64(1)
	for i := 0; i < exp; i++ {
		pow *= 10
	}
	return fmt.Sprintf("%s$%d.%0*d", sign, minor/pow, exp, minor%pow)
}
