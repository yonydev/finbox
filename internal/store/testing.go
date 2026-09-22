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

// EditLogForTest returns the edit_log rows of a transaction as
// "field old>new source", for assertions from other packages' tests.
func (s *Store) EditLogForTest(tb testing.TB, txnID string) []string {
	tb.Helper()
	rows, err := s.pool.Query(context.Background(),
		`select field, old_value, new_value, source from edit_log where transaction_id=$1 order by created_at, field`, txnID)
	if err != nil {
		tb.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var field, oldV, newV, source string
		if err := rows.Scan(&field, &oldV, &newV, &source); err != nil {
			tb.Fatal(err)
		}
		out = append(out, field+" "+oldV+">"+newV+" "+source)
	}
	if err := rows.Err(); err != nil {
		tb.Fatal(err)
	}
	return out
}
