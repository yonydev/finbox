package store

import (
	"context"
	"os"
	"testing"
)

// NewTest returns a migrated, truncated store bound to TEST_DB_URL.
// Lives in a non-test file on purpose: _test.go helpers are invisible
// to other packages' test builds.
func NewTest(tb testing.TB) *Store {
	tb.Helper()
	url := os.Getenv("TEST_DB_URL")
	if url == "" {
		tb.Skip("TEST_DB_URL not set")
	}
	ctx := context.Background()
	s, err := New(ctx, url)
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(s.Close)
	if _, err := s.Migrate(ctx); err != nil {
		tb.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `truncate receipts, transactions, transaction_items, edit_log, processed_updates cascade`); err != nil {
		tb.Fatal(err)
	}
	return s
}
