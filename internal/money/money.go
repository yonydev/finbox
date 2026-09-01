package money

import (
	"fmt"
	"math"
	"strings"
)

var exponents = map[string]int{"MXN": 2, "USD": 2, "EUR": 2, "JPY": 0}

func Exponent(currency string) int {
	if e, ok := exponents[strings.ToUpper(currency)]; ok {
		return e
	}
	return 2
}

// ParseMinor converts a decimal string to integer minor units using
// integer math only. Tolerates "$", spaces and "," thousands separators.
func ParseMinor(s, currency string) (int64, error) {
	exp := Exponent(currency)
	clean := strings.Map(func(r rune) rune {
		switch r {
		case '$', ',', ' ':
			return -1
		}
		return r
	}, strings.TrimSpace(s))
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
	if len(fracPart) > exp {
		return 0, fmt.Errorf("%q tiene más de %d decimales", s, exp)
	}
	for len(fracPart) < exp {
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
