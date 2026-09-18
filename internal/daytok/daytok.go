// Package daytok parses short day tokens the way monthtok parses months:
// hoy|ayer|antier, DD/MM (most recent), DD/MM/YYYY and ISO YYYY-MM-DD.
package daytok

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

var relative = map[string]int{"hoy": 0, "ayer": -1, "antier": -2}

// Parse resolves a day token against `now`; "" means today. The result is
// midnight in now's location, so callers pass now.In(loc).
func Parse(tok string, now time.Time) (time.Time, error) {
	tok = strings.ToLower(strings.TrimSpace(tok))
	y, m, d := now.Date()
	if tok == "" {
		return time.Date(y, m, d, 0, 0, 0, 0, now.Location()), nil
	}
	if off, ok := relative[tok]; ok {
		return time.Date(y, m, d+off, 0, 0, 0, 0, now.Location()), nil
	}
	parts := strings.Split(strings.ReplaceAll(tok, "-", "/"), "/")
	nums := make([]int, len(parts))
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return time.Time{}, errInvalid(tok)
		}
		nums[i] = n
	}
	var day, month, year int
	switch {
	case len(nums) == 2:
		day, month, year = nums[0], nums[1], 0
	case len(nums) == 3 && len(parts[0]) == 4: // ISO: 4-digit year first
		year, month, day = nums[0], nums[1], nums[2]
	case len(nums) == 3 && len(parts[2]) == 4:
		day, month, year = nums[0], nums[1], nums[2]
	default:
		return time.Time{}, errInvalid(tok)
	}
	if year == 0 {
		year = y
	}
	t := time.Date(year, time.Month(month), day, 0, 0, 0, 0, now.Location())
	if t.Day() != day || t.Month() != time.Month(month) || t.Year() != year { // time.Date normalizes 29/02 → 1-mar
		return time.Time{}, errInvalid(tok)
	}
	if len(nums) == 2 && t.After(now) { // hasn't happened yet this year → most recent = last year
		t = t.AddDate(-1, 0, 0)
	}
	return t, nil
}

func errInvalid(tok string) error {
	return fmt.Errorf("fecha inválida: %q (usa DD/MM, YYYY-MM-DD, hoy o ayer)", tok)
}
