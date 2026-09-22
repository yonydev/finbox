package store

import (
	"context"
	"strings"
	"testing"
	"time"
)

func canonOf(t *testing.T, s *Store, txnID string) string {
	t.Helper()
	row, err := s.GetTransactionByID(context.Background(), txnID)
	if err != nil {
		t.Fatal(err)
	}
	return row.MerchantCanon
}

func TestRecanonTransactions(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	day := time.Date(2026, 8, 28, 0, 0, 0, 0, time.UTC)
	_, plain := confirmed(t, s, "sha-recanon-1", 3200, 1000, day) // merchant "Walmart"
	_, renamed := confirmed(t, s, "sha-recanon-2", 3201, 2000, day)
	if err := s.EditTransaction(ctx, renamed, map[string]any{"merchant_canon": "Mi Walmart"},
		[]FieldEdit{{Field: "merchant", Old: "Walmart", New: "Mi Walmart"}}); err != nil {
		t.Fatal(err)
	}

	// dry run reports the change without writing it
	changes, err := s.RecanonTransactions(ctx, strings.ToUpper, true)
	if err != nil || len(changes) != 1 || changes[0][0] != plain || changes[0][1] != "Walmart" || changes[0][2] != "WALMART" {
		t.Fatalf("dry run: %+v %v", changes, err)
	}
	if got := canonOf(t, s, plain); got != "Walmart" {
		t.Fatalf("dry run wrote %q", got)
	}

	changes, err = s.RecanonTransactions(ctx, strings.ToUpper, false)
	if err != nil || len(changes) != 1 {
		t.Fatalf("real run: %+v %v", changes, err)
	}
	if got := canonOf(t, s, plain); got != "WALMART" {
		t.Fatalf("canon = %q, want WALMART", got)
	}
	// a human rename is never overwritten, and the raw name never moves
	if got := canonOf(t, s, renamed); got != "Mi Walmart" {
		t.Fatalf("renamed row recanoned to %q", got)
	}
	row, _ := s.GetTransactionByID(ctx, plain)
	if row.Merchant != "Walmart" {
		t.Fatalf("raw merchant = %q", row.Merchant)
	}
	// idempotent: nothing left to change
	if changes, err := s.RecanonTransactions(ctx, strings.ToUpper, false); err != nil || len(changes) != 0 {
		t.Fatalf("second run: %+v %v", changes, err)
	}
	// and it logs nothing: only the human rename is in edit_log
	var n int
	if err := s.pool.QueryRow(ctx, `select count(*) from edit_log`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("edit_log rows = %d %v", n, err)
	}
}

func TestRecanonSkipsVoided(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	_, txnID := confirmed(t, s, "sha-recanon-3", 3210, 1000, time.Date(2026, 8, 28, 0, 0, 0, 0, time.UTC))
	if ok, err := s.VoidTransaction(ctx, txnID); err != nil || !ok {
		t.Fatalf("void: %v %v", ok, err)
	}
	if changes, err := s.RecanonTransactions(ctx, strings.ToUpper, true); err != nil || len(changes) != 0 {
		t.Fatalf("voided row recanoned: %+v %v", changes, err)
	}
}
