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

func TestEditAllowsNegativeRejectsZero(t *testing.T) {
	s, _, txnID := seed(t)
	row, err := Edit(context.Background(), s, txnID[:8], EditOpts{Total: "-5.00"}, time.UTC)
	if err != nil || row.AmountMinor != -500 {
		t.Fatalf("refund edit: %v %+v", err, row)
	}
	if _, err := Edit(context.Background(), s, txnID[:8], EditOpts{Total: "0"}, time.UTC); err == nil {
		t.Fatal("want error: zero total")
	}
}

func TestEditCurrencyExponentChangeRequiresTotal(t *testing.T) {
	s, _, txnID := seed(t) // seeded as 18500 MXN (exponent 2)
	if _, err := Edit(context.Background(), s, txnID[:8], EditOpts{Currency: "JPY"}, time.UTC); err == nil {
		t.Fatal("want error: exponent change without --total silently rescales the amount")
	}
	row, err := Edit(context.Background(), s, txnID[:8], EditOpts{Currency: "USD"}, time.UTC)
	if err != nil || row.Currency != "USD" || row.AmountMinor != 18500 {
		t.Fatalf("same-exponent relabel should pass: %v %+v", err, row)
	}
	row, err = Edit(context.Background(), s, txnID[:8], EditOpts{Currency: "JPY", Total: "185"}, time.UTC)
	if err != nil || row.Currency != "JPY" || row.AmountMinor != 185 {
		t.Fatalf("exponent change with total should pass: %v %+v", err, row)
	}
}

func TestEditRejectsUnknownCurrency(t *testing.T) {
	s, _, txnID := seed(t)
	if _, err := Edit(context.Background(), s, txnID[:8], EditOpts{Currency: "CLP", Total: "300"}, time.UTC); err == nil {
		t.Fatal("want error: unsupported currency would store a wrong-scale amount")
	}
}

func TestAddManualExpense(t *testing.T) {
	s := store.NewTest(t)
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	row, err := Add(context.Background(), s, AddOpts{Total: "285.00", Merchant: "Taller García", Date: "2026-09-01"}, now, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if row.Source != "manual" || row.ReceiptID != "" || row.AmountMinor != 28500 ||
		row.Currency != "MXN" || row.OccurredOn.Format("2006-01-02") != "2026-09-01" {
		t.Fatalf("row = %+v", row)
	}
	rows, err := List(context.Background(), s, 10, "", now, time.UTC)
	if err != nil || len(rows) != 1 || rows[0].ID != row.ID {
		t.Fatalf("added txn not listed: %v %+v", err, rows)
	}
}

func TestAddRefundNegativeTotal(t *testing.T) {
	s := store.NewTest(t)
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	row, err := Add(context.Background(), s, AddOpts{Total: "-120.00", Merchant: "Zapatería"}, now, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if row.AmountMinor != -12000 {
		t.Fatalf("row = %+v", row)
	}
}

func TestAddDefaultsDateToToday(t *testing.T) {
	s := store.NewTest(t)
	loc, _ := time.LoadLocation("America/Mexico_City")
	now := time.Date(2026, 9, 11, 2, 0, 0, 0, time.UTC) // still sep 10 in CDMX
	row, err := Add(context.Background(), s, AddOpts{Total: "50", Merchant: "Propina"}, now, loc)
	if err != nil {
		t.Fatal(err)
	}
	if got := row.OccurredOn.Format("2006-01-02"); got != "2026-09-10" {
		t.Fatalf("occurred_on = %s, want 2026-09-10 (local day)", got)
	}
}

func TestAddValidates(t *testing.T) {
	s := store.NewTest(t)
	now := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	cases := []AddOpts{
		{Total: "0", Merchant: "X"},                      // zero forbidden
		{Total: "50"},                                    // merchant missing
		{Total: "50", Merchant: "X", Currency: "CLP"},    // unsupported currency (same branch rejects "pesos")
		{Total: "50", Merchant: "X", Date: "10/09/2026"}, // bad date format
		{Total: "abc", Merchant: "X"},                    // bad amount
		{Total: "300,50", Merchant: "X"},                 // decimal comma must not reach the DB
	}
	for i, o := range cases {
		if _, err := Add(context.Background(), s, o, now, time.UTC); err == nil {
			t.Errorf("case %d (%+v): want error", i, o)
		}
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
