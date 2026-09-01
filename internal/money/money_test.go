package money

import "testing"

func TestParseMinor(t *testing.T) {
	cases := []struct {
		in, cur string
		want    int64
		wantErr bool
	}{
		{"364.35", "MXN", 36435, false},
		{"364", "MXN", 36400, false},
		{"364.5", "MXN", 36450, false},
		{"0.01", "USD", 1, false},
		{"-12.50", "MXN", -1250, false},
		{"1200", "JPY", 1200, false},
		{"12.5", "JPY", 0, true},   // fraction digits > exponent
		{"364.355", "MXN", 0, true}, // 3 fraction digits
		{"", "MXN", 0, true},
		{"12a", "MXN", 0, true},
		{"1,200.00", "MXN", 120000, false}, // thousands separators tolerated
		{"$364.00", "MXN", 36400, false},   // currency symbol tolerated
		{"1234567890123456789012345", "MXN", 0, true}, // 25 digits overflow
		{"922337203685477580.07", "MXN", 0, true},     // exceeds max int64
	}
	for _, tc := range cases {
		got, err := ParseMinor(tc.in, tc.cur)
		if (err != nil) != tc.wantErr {
			t.Errorf("ParseMinor(%q,%q) err=%v wantErr=%v", tc.in, tc.cur, err, tc.wantErr)
			continue
		}
		if err == nil && got != tc.want {
			t.Errorf("ParseMinor(%q,%q)=%d want %d", tc.in, tc.cur, got, tc.want)
		}
	}
}

func TestFormat(t *testing.T) {
	if got := Format(36435, "MXN"); got != "$364.35" {
		t.Errorf("Format = %q", got)
	}
	if got := Format(-1250, "MXN"); got != "-$12.50" {
		t.Errorf("Format = %q", got)
	}
	if got := Format(1200, "JPY"); got != "$1200" {
		t.Errorf("Format = %q", got)
	}
}
