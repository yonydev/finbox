package command

import (
	"context"
	"testing"
	"time"

	"finbox/internal/store"
)

func seed(t *testing.T) (*store.Store, string, string) {
	t.Helper()
	s := store.NewTest(t)
	ctx := context.Background()
	r, err := s.CreateReceipt(ctx, store.CreateReceiptParams{BlobKey: "k", BlobSHA256: "sha-cmd", TgMessageID: 1, TgChatID: 1})
	if err != nil {
		t.Fatal(err)
	}
	s.Transition(ctx, r.ID, "pending", "awaiting_confirm", "")
	txnID, ok, err := s.ConfirmReceipt(ctx, r.ID, store.NewTransaction{
		OccurredOn: time.Date(2026, 8, 28, 0, 0, 0, 0, time.UTC),
		Merchant:   "Tacos", AmountMinor: 18500, Currency: "MXN", Source: "receipt",
	}, 0)
	if err != nil || !ok {
		t.Fatal(err)
	}
	return s, r.ID, txnID
}

func TestEditByReceiptPrefixUpdatesTotal(t *testing.T) {
	s, rID, txnID := seed(t)
	ctx := context.Background()
	row, err := Edit(ctx, s, rID[:8], EditOpts{Total: "285.00"}, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if row.ID != txnID || row.AmountMinor != 28500 {
		t.Fatalf("row = %+v", row)
	}
}

func TestEditRejectsNegativeTotal(t *testing.T) {
	s, _, txnID := seed(t)
	if _, err := Edit(context.Background(), s, txnID[:8], EditOpts{Total: "-5.00"}, time.UTC); err == nil {
		t.Fatal("want error: Phase 1 is positive-only")
	}
}

func TestVoidByPrefix(t *testing.T) {
	s, _, txnID := seed(t)
	got, err := Void(context.Background(), s, txnID[:8])
	if err != nil || got != txnID {
		t.Fatalf("void: %q %v", got, err)
	}
}

func TestListClampAndMonth(t *testing.T) {
	s, _, _ := seed(t)
	now := time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC)
	rows, err := List(context.Background(), s, 10, "aug", now, time.UTC)
	if err != nil || len(rows) != 1 {
		t.Fatalf("list: %d %v", len(rows), err)
	}
	_, _, totals, count, err := Month(context.Background(), s, "", now, time.UTC)
	if err != nil || count != 1 || totals[0].AmountMinor != 18500 {
		t.Fatalf("month: %v %d %v", totals, count, err)
	}
}
