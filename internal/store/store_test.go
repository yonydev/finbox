package store

import (
	"context"
	"testing"
)

func TestMigrateIsIdempotent(t *testing.T) {
	s := NewTest(t)
	n, err := s.Migrate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("second Migrate applied %d, want 0", n)
	}
}
