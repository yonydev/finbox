package daytok

import (
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	mx := time.FixedZone("MX", -6*3600)
	now := time.Date(2026, time.September, 18, 12, 0, 0, 0, mx)
	cases := []struct {
		tok     string
		want    string
		wantErr bool
	}{
		{"", "2026-09-18", false},
		{"hoy", "2026-09-18", false},
		{"ayer", "2026-09-17", false},
		{"AYER", "2026-09-17", false},
		{"antier", "2026-09-16", false},
		{"15/09", "2026-09-15", false},
		{"15-09", "2026-09-15", false},
		{"18/09", "2026-09-18", false}, // today IS most recent
		{"31/12", "2025-12-31", false}, // future this year → last year
		{"15/09/2026", "2026-09-15", false},
		{"15-09-2026", "2026-09-15", false},
		{"2026-09-15", "2026-09-15", false},
		{"2026/09/15", "2026-09-15", false},
		{"31/12/2026", "2026-12-31", false}, // explicit year is never shifted
		{"32/09", "", true},
		{"15/13", "", true},
		{"29/02", "", true}, // 2026 is not a leap year
		{"29/02/2024", "2024-02-29", false},
		{"15", "", true},
		{"15/09/26", "", true}, // 2-digit year rejected
		{"a/b", "", true},
		{"-5/09", "", true},
	}
	for _, tc := range cases {
		got, err := Parse(tc.tok, now)
		if (err != nil) != tc.wantErr {
			t.Errorf("Parse(%q) err=%v", tc.tok, err)
			continue
		}
		if err == nil {
			if got.Format("2006-01-02") != tc.want {
				t.Errorf("Parse(%q) = %s, want %s", tc.tok, got.Format("2006-01-02"), tc.want)
			}
			if got.Location() != mx {
				t.Errorf("Parse(%q) lost location", tc.tok)
			}
		}
	}
	// year−1 border: in January, "15/09" is last September
	jan := time.Date(2026, time.January, 10, 0, 0, 0, 0, mx)
	if got, _ := Parse("15/09", jan); got.Format("2006-01-02") != "2025-09-15" {
		t.Errorf("january 15/09 = %s", got.Format("2006-01-02"))
	}
}
