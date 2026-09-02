package config

import (
	"testing"
	"time"
)

func TestMexicoCityLoads(t *testing.T) {
	if _, err := time.LoadLocation("America/Mexico_City"); err != nil {
		t.Fatalf("tzdata missing: %v", err)
	}
}
