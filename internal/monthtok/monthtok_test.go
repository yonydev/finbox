package monthtok

import (
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	now := time.Date(2026, time.August, 31, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		tok     string
		wantY   int
		wantM   time.Month
		wantErr bool
	}{
		{"", 2026, time.August, false},      // current month
		{"aug", 2026, time.August, false},   // current month IS most recent
		{"ago", 2026, time.August, false},   // spanish
		{"dec", 2025, time.December, false}, // future month → last year
		{"dic", 2025, time.December, false},
		{"jan", 2026, time.January, false}, // past month this year
		{"2026-01", 2026, time.January, false},
		{"2025-12", 2025, time.December, false},
		{"AUG", 2026, time.August, false}, // case-insensitive
		{"agosto", 0, 0, true},            // full names not supported
		{"13", 0, 0, true},
		{"2026-13", 0, 0, true},
	}
	for _, tc := range cases {
		y, m, err := Parse(tc.tok, now)
		if (err != nil) != tc.wantErr {
			t.Errorf("Parse(%q) err=%v", tc.tok, err)
			continue
		}
		if err == nil && (y != tc.wantY || m != tc.wantM) {
			t.Errorf("Parse(%q) = %d-%v, want %d-%v", tc.tok, y, m, tc.wantY, tc.wantM)
		}
	}
}
