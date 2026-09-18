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
	tok = strings.ReplaceAll(tok, "-", "/")
	for _, layout := range []string{"2/1/2006", "2006/1/2"} {
		if t, err := time.ParseInLocation(layout, tok, now.Location()); err == nil {
			return t, nil
		}
	}
	// DD/MM: this year, unless that is still ahead of us → most recent = last year
	t, err := time.ParseInLocation("2/1/2006", tok+"/"+strconv.Itoa(y), now.Location())
	if err != nil {
		return time.Time{}, fmt.Errorf("fecha inválida: %q (usa DD/MM, YYYY-MM-DD, hoy o ayer)", tok)
	}
	if t.After(now) {
		t = t.AddDate(-1, 0, 0)
	}
	return t, nil
}
