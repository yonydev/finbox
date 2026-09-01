package fs

import (
	"context"
	"testing"
	"time"
)

func TestPutGetAndKey(t *testing.T) {
	s := New(t.TempDir())
	ctx := context.Background()
	key := Key(time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC), "abc123", ".jpg")
	if key != "2026/09/abc123.jpg" {
		t.Fatalf("key = %q", key)
	}
	if err := s.Put(ctx, key, []byte("data")); err != nil {
		t.Fatal(err)
	}
	if err := s.Put(ctx, key, []byte("data")); err != nil { // idempotent
		t.Fatal(err)
	}
	got, err := s.Get(ctx, key)
	if err != nil || string(got) != "data" {
		t.Fatalf("get: %q %v", got, err)
	}
	if _, err := s.Get(ctx, "2026/09/missing.jpg"); err == nil {
		t.Fatal("want error for missing key")
	}
}

func TestPutRejectsPathTraversal(t *testing.T) {
	s := New(t.TempDir())
	if err := s.Put(context.Background(), "../evil.jpg", []byte("x")); err == nil {
		t.Fatal("want error for traversal key")
	}
}
