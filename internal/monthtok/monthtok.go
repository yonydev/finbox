package monthtok

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

var tokens = map[string]time.Month{
	"jan": 1, "ene": 1, "feb": 2, "mar": 3, "apr": 4, "abr": 4,
	"may": 5, "jun": 6, "jul": 7, "aug": 8, "ago": 8, "sep": 9,
	"oct": 10, "nov": 11, "dec": 12, "dic": 12,
}

// Parse resolves a month token against `now` (spec §5).
func Parse(tok string, now time.Time) (int, time.Month, error) {
	tok = strings.ToLower(strings.TrimSpace(tok))
	if tok == "" {
		return now.Year(), now.Month(), nil
	}
	if y, m, ok := parseNumeric(tok); ok {
		return y, m, nil
	}
	m, ok := tokens[tok]
	if !ok {
		return 0, 0, fmt.Errorf("mes no reconocido: %q (usa jan|ene… o YYYY-MM)", tok)
	}
	year := now.Year()
	if m > now.Month() { // hasn't happened yet this year → most recent = last year
		year--
	}
	return year, m, nil
}

func parseNumeric(tok string) (int, time.Month, bool) {
	parts := strings.SplitN(tok, "-", 2)
	if len(parts) != 2 || len(parts[0]) != 4 {
		return 0, 0, false
	}
	y, err1 := strconv.Atoi(parts[0])
	m, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil || m < 1 || m > 12 {
		return 0, 0, false
	}
	return y, time.Month(m), true
}
