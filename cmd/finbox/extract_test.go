package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractCmdRejectsNonImage(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "not-image.jpg")
	os.WriteFile(p, []byte("plain text, wrong magic bytes here"), 0o644)
	var out, errb bytes.Buffer
	code := run([]string{"finbox", "extract", p}, &out, &errb)
	if code != 2 {
		t.Fatalf("exit = %d, want 2 (usage)", code)
	}
	if !strings.Contains(errb.String(), "formato no soportado") {
		t.Errorf("stderr = %q", errb.String())
	}
}

func TestExtractCmdMissingFile(t *testing.T) {
	var out, errb bytes.Buffer
	code := run([]string{"finbox", "extract", "/nope/missing.jpg"}, &out, &errb)
	if code != 3 {
		t.Fatalf("exit = %d, want 3", code)
	}
}
