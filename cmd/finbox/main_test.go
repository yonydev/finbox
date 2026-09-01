package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRouter(t *testing.T) {
	cases := []struct {
		name     string
		argv     []string
		wantCode int
		wantOut  string // substring of stdout
		wantErr  string // substring of stderr
	}{
		{"no args shows help", []string{"finbox"}, 2, "", "uso: finbox"},
		{"unknown subcommand", []string{"finbox", "nope"}, 2, "", "subcomando desconocido"},
		{"version", []string{"finbox", "version"}, 0, "finbox ", ""},
		{"help", []string{"finbox", "help"}, 0, "finbox ", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out, errb bytes.Buffer
			code := run(tc.argv, &out, &errb)
			if code != tc.wantCode {
				t.Fatalf("exit = %d, want %d", code, tc.wantCode)
			}
			if !strings.Contains(out.String(), tc.wantOut) {
				t.Errorf("stdout %q missing %q", out.String(), tc.wantOut)
			}
			if !strings.Contains(errb.String(), tc.wantErr) {
				t.Errorf("stderr %q missing %q", errb.String(), tc.wantErr)
			}
		})
	}
}
