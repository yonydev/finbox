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
		{"12.5", "JPY", 0, true},    // fraction digits > exponent
		{"364.355", "MXN", 0, true}, // 3 fraction digits
		{"", "MXN", 0, true},
		{"12a", "MXN", 0, true},
		{"1,200.00", "MXN", 120000, false},            // thousands separators tolerated
		{"$364.00", "MXN", 36400, false},              // currency symbol tolerated
		{"1234567890123456789012345", "MXN", 0, true}, // 25 digits overflow
		{"922337203685477580.07", "MXN", 0, true},     // exceeds max int64
		{"300,50", "MXN", 0, true},                    // decimal comma: reject, never read as 30050.00
		{"0,50", "MXN", 0, true},                      // decimal comma
		{"300 50", "MXN", 0, true},                    // interior space: reject, never glue digits
		{"1 300,50", "MXN", 0, true},                  // es/fr paste form
		{"1,3", "MXN", 0, true},                       // malformed thousands group
		{"12,34.00", "MXN", 0, true},                  // malformed thousands group
		{"-1,200.50", "MXN", -120050, false},          // signed thousands form still fine
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

func TestKnown(t *testing.T) {
	for cur, want := range map[string]bool{"MXN": true, "usd": true, "JPY": true, "CLP": false, "XYZ": false, "": false} {
		if Known(cur) != want {
			t.Errorf("Known(%q) = %v, want %v", cur, !want, want)
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
