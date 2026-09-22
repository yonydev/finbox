package store

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func jsonEq(t *testing.T, got []byte, want map[string]any) {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(got, &m); err != nil {
		t.Fatalf("unmarshal %s: %v", got, err)
	}
	if len(m) != len(want) {
		t.Fatalf("got %s, want %v", got, want)
	}
	for k, v := range want {
		if m[k] != v {
			t.Fatalf("got %s, want %v", got, want)
		}
	}
}

func TestAmendExtraction(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	r := mkReceipt(t, s, "sha-am1", 400)
	if err := s.SetExtraction(ctx, r.ID, []byte(`{"merchant":"OXXO","total":"285.00"}`), "m"); err != nil {
		t.Fatal(err)
	}
	s.Transition(ctx, r.ID, "pending", "awaiting_confirm", "")

	status, ex, err := s.AmendExtraction(ctx, r.ID, []byte(`{"total":"300.00"}`))
	if err != nil || status != "awaiting_confirm" {
		t.Fatalf("amend: %s %v", status, err)
	}
	jsonEq(t, ex, map[string]any{"merchant": "OXXO", "total": "300.00"})
	got, _ := s.GetReceipt(ctx, r.ID)
	jsonEq(t, got.ExtractionRaw, map[string]any{"merchant": "OXXO", "total": "285.00"})

	// second amend keeps the raw copy (S2: write-once)
	if _, ex, err = s.AmendExtraction(ctx, r.ID, []byte(`{"merchant":"Soriana"}`)); err != nil {
		t.Fatal(err)
	}
	jsonEq(t, ex, map[string]any{"merchant": "Soriana", "total": "300.00"})
	got, _ = s.GetReceipt(ctx, r.ID)
	jsonEq(t, got.ExtractionRaw, map[string]any{"merchant": "OXXO", "total": "285.00"})

	// a fresh extraction (retry/reprocess) drops the raw copy (S1)
	if err := s.SetExtraction(ctx, r.ID, []byte(`{"merchant":"X"}`), "m"); err != nil {
		t.Fatal(err)
	}
	if got, _ = s.GetReceipt(ctx, r.ID); got.ExtractionRaw != nil {
		t.Fatalf("raw after SetExtraction = %s", got.ExtractionRaw)
	}
}

func TestAmendExtractionFailedWithoutExtraction(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	r := mkReceipt(t, s, "sha-am2", 401)
	s.Transition(ctx, r.ID, "pending", "failed", "no pude leer")
	status, ex, err := s.AmendExtraction(ctx, r.ID, []byte(`{"total":"285"}`))
	if err != nil || status != "failed" {
		t.Fatalf("amend: %s %v", status, err)
	}
	jsonEq(t, ex, map[string]any{"total": "285"}) // NULL || patch must not be NULL
	got, _ := s.GetReceipt(ctx, r.ID)
	jsonEq(t, got.ExtractionRaw, map[string]any{})
}

func TestAmendExtractionStatusGuard(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	r := mkReceipt(t, s, "sha-am3", 402) // pending
	if _, _, err := s.AmendExtraction(ctx, r.ID, []byte(`{}`)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("pending: err = %v", err)
	}
	rID, _ := confirmed(t, s, "sha-am4", 403, 100, time.Date(2026, 8, 28, 0, 0, 0, 0, time.UTC))
	if _, _, err := s.AmendExtraction(ctx, rID, []byte(`{}`)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("confirmed: err = %v", err)
	}
}

func TestConfirmReceiptEditsAndGuard(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	day := time.Date(2026, 8, 28, 0, 0, 0, 0, time.UTC)
	r := mkReceipt(t, s, "sha-cg1", 410)
	s.Transition(ctx, r.ID, "pending", "awaiting_confirm", "")
	stale := time.Now().Add(-time.Hour)
	txn := NewTransaction{OccurredOn: day, Merchant: "W", AmountMinor: 100, Currency: "MXN", Source: "receipt",
		Edits: []FieldEdit{{Field: "total", Old: "285.00", New: "300.00", Source: "reply"}}}
	if _, ok, err := s.ConfirmReceipt(ctx, r.ID, txn, 0, &stale); err != nil || ok {
		t.Fatalf("stale guard: %v %v", ok, err)
	}
	cur, _ := s.GetReceipt(ctx, r.ID)
	txnID, ok, err := s.ConfirmReceipt(ctx, r.ID, txn, 0, &cur.UpdatedAt)
	if err != nil || !ok {
		t.Fatalf("confirm: %v %v", ok, err)
	}
	var src string
	if err := s.pool.QueryRow(ctx, `select source from edit_log where transaction_id=$1`, txnID).Scan(&src); err != nil || src != "reply" {
		t.Fatalf("edit_log source = %q %v", src, err)
	}
	// FieldEdit without Source defaults to cli
	if err := s.EditTransaction(ctx, txnID, map[string]any{"merchant_canon": "X"}, []FieldEdit{{Field: "merchant", Old: "W", New: "X"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.pool.QueryRow(ctx, `select source from edit_log where transaction_id=$1 and field='merchant'`, txnID).Scan(&src); err != nil || src != "cli" {
		t.Fatalf("default source = %q %v", src, err)
	}
}
