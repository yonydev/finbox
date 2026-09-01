package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func confirmed(t *testing.T, s *Store, sha string, msg int64, amount int64, day time.Time) (string, string) {
	t.Helper()
	ctx := context.Background()
	r := mkReceipt(t, s, sha, msg)
	if ok, _ := s.Transition(ctx, r.ID, "pending", "awaiting_confirm", ""); !ok {
		t.Fatal("transition failed")
	}
	q := int64(1000)
	txnID, ok, err := s.ConfirmReceipt(ctx, r.ID, NewTransaction{
		OccurredOn: day, Merchant: "Walmart", AmountMinor: amount, Currency: "MXN", Source: "receipt",
		Items: []NewItem{{Position: 1, Name: "Café", QuantityMilli: &q, AmountMinor: &amount}},
	}, 900+msg)
	if err != nil || !ok {
		t.Fatalf("confirm: %v %v", ok, err)
	}
	return r.ID, txnID
}

func TestConfirmIsIdempotent(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	day := time.Date(2026, 8, 28, 0, 0, 0, 0, time.UTC)
	rID, _ := confirmed(t, s, "sha-c1", 300, 36400, day)
	_, ok, err := s.ConfirmReceipt(ctx, rID, NewTransaction{
		OccurredOn: day, Merchant: "X", AmountMinor: 1, Currency: "MXN", Source: "receipt",
	}, 999)
	if err != nil || ok {
		t.Fatalf("double confirm must be no-op: %v %v", ok, err)
	}
	rows, err := s.ListTransactions(ctx, 10, 0, 0, time.UTC)
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows = %d, %v", len(rows), err)
	}
	if rows[0].ShortID != rows[0].ID[:8] {
		t.Errorf("short id: %q vs %q", rows[0].ShortID, rows[0].ID)
	}
}

func TestVoidAllowsReconfirm(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	day := time.Date(2026, 8, 28, 0, 0, 0, 0, time.UTC)
	rID, txnID := confirmed(t, s, "sha-v1", 310, 5000, day)
	ok, err := s.VoidTransaction(ctx, txnID)
	if err != nil || !ok {
		t.Fatalf("void: %v %v", ok, err)
	}
	if rows, _ := s.ListTransactions(ctx, 10, 0, 0, time.UTC); len(rows) != 0 {
		t.Fatalf("voided txn still listed")
	}
	// re-confirm path: discarded/failed → awaiting again is handled elsewhere;
	// here: confirming again must be possible because the partial index ignores voided rows.
	if ok, _ := s.Transition(ctx, rID, "confirmed", "awaiting_confirm", ""); !ok {
		t.Fatal("reset transition failed")
	}
	if _, ok, err := s.ConfirmReceipt(ctx, rID, NewTransaction{
		OccurredOn: day, Merchant: "W", AmountMinor: 5100, Currency: "MXN", Source: "receipt",
	}, 998); err != nil || !ok {
		t.Fatalf("reconfirm after void: %v %v", ok, err)
	}
}

func TestMonthTotalsPerCurrency(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	aug := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	confirmed(t, s, "sha-m1", 320, 36400, aug)
	confirmed(t, s, "sha-m2", 321, 12000, aug)
	// a USD row proves currencies are NOT summed into one bucket
	r, _ := s.CreateReceipt(ctx, CreateReceiptParams{BlobKey: "kusd", BlobSHA256: "sha-m3", TgMessageID: 322, TgChatID: 7})
	s.Transition(ctx, r.ID, "pending", "awaiting_confirm", "")
	s.ConfirmReceipt(ctx, r.ID, NewTransaction{OccurredOn: aug, Merchant: "Hotel", AmountMinor: 4550, Currency: "USD", Source: "receipt"}, 0)

	totals, count, err := s.MonthTotals(ctx, 2026, time.August, time.UTC)
	if err != nil || count != 3 {
		t.Fatalf("count=%d err=%v", count, err)
	}
	if len(totals) != 2 || totals[0].Currency != "MXN" || totals[0].AmountMinor != 48400 ||
		totals[1].Currency != "USD" || totals[1].AmountMinor != 4550 {
		t.Fatalf("totals = %+v", totals)
	}
}

func TestEditWritesLog(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	day := time.Date(2026, 8, 28, 0, 0, 0, 0, time.UTC)
	_, txnID := confirmed(t, s, "sha-e1", 330, 18500, day)
	err := s.EditTransaction(ctx, txnID,
		map[string]any{"amount_minor": int64(28500)},
		[]FieldEdit{{Field: "total", Old: "18500", New: "28500"}})
	if err != nil {
		t.Fatal(err)
	}
	var n int
	s.pool.QueryRow(ctx, `select count(*) from edit_log where transaction_id=$1`, txnID).Scan(&n)
	if n != 1 {
		t.Fatalf("edit_log rows = %d", n)
	}
}

func TestGetTransactionByID(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	day := time.Date(2026, 8, 28, 0, 0, 0, 0, time.UTC)
	_, txnID := confirmed(t, s, "sha-g1", 350, 1234, day)

	tests := []struct {
		name    string
		id      string
		wantErr error
	}{
		{"found", txnID, nil},
		{"unknown id", "00000000-0000-0000-0000-000000000000", ErrNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			row, err := s.GetTransactionByID(ctx, tt.id)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr == nil && (row.ID != txnID || row.AmountMinor != 1234) {
				t.Fatalf("row = %+v", row)
			}
		})
	}

	if ok, err := s.VoidTransaction(ctx, txnID); err != nil || !ok {
		t.Fatalf("void: %v %v", ok, err)
	}
	if _, err := s.GetTransactionByID(ctx, txnID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("voided txn: err = %v, want ErrNotFound", err)
	}
}

func TestGetActiveTxnForReceipt(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	day := time.Date(2026, 8, 28, 0, 0, 0, 0, time.UTC)
	rID, txnID := confirmed(t, s, "sha-g2", 360, 2000, day)
	r2, err := s.CreateReceipt(ctx, CreateReceiptParams{BlobKey: "k2", BlobSHA256: "sha-g3", TgMessageID: 361, TgChatID: 7})
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name      string
		receiptID string
		wantErr   error
	}{
		{"active txn", rID, nil},
		{"receipt with no txn", r2.ID, ErrNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			row, err := s.GetActiveTxnForReceipt(ctx, tt.receiptID)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr == nil && row.ID != txnID {
				t.Fatalf("row = %+v", row)
			}
		})
	}

	if ok, err := s.VoidTransaction(ctx, txnID); err != nil || !ok {
		t.Fatalf("void: %v %v", ok, err)
	}
	if _, err := s.GetActiveTxnForReceipt(ctx, rID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after void: err = %v, want ErrNotFound", err)
	}
}

func TestResolveID(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	day := time.Date(2026, 8, 28, 0, 0, 0, 0, time.UTC)
	rID, txnID := confirmed(t, s, "sha-r1", 340, 700, day)
	kind, id, err := s.ResolveID(ctx, rID[:8])
	if err != nil || kind != "receipt" || id != rID {
		t.Fatalf("resolve receipt: %s %s %v", kind, id, err)
	}
	kind, id, err = s.ResolveID(ctx, txnID[:8])
	if err != nil || kind != "transaction" || id != txnID {
		t.Fatalf("resolve txn: %s %s %v", kind, id, err)
	}
	if _, _, err := s.ResolveID(ctx, "deadbeef"); !errors.Is(err, ErrNotFound) { // valid hex, matches nothing
		t.Fatalf("err = %v", err)
	}
	if _, _, err := s.ResolveID(ctx, "nothex!"); err == nil {
		t.Fatal("invalid prefix must error")
	}
}
