package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"finbox/internal/store"
)

func cliStore(t *testing.T) (*store.Store, string) {
	t.Helper()
	url := os.Getenv("TEST_DB_URL")
	if url == "" {
		t.Skip("TEST_DB_URL not set")
	}
	s := store.NewTest(t)
	// seed one confirmed txn
	ctx := context.Background()
	r, _ := s.CreateReceipt(ctx, store.CreateReceiptParams{BlobKey: "k", BlobSHA256: "sha-cli", TgMessageID: 1, TgChatID: 1})
	s.Transition(ctx, r.ID, "pending", "awaiting_confirm", "")
	txnID, _, _ := s.ConfirmReceipt(ctx, r.ID, store.NewTransaction{
		OccurredOn: time.Date(2026, 8, 28, 0, 0, 0, 0, time.UTC),
		Merchant:   "Walmart", AmountMinor: 36400, Currency: "MXN", Source: "receipt",
	}, 0)
	return s, txnID
}

func TestCLIListJSON(t *testing.T) {
	_, _ = cliStore(t)
	t.Setenv("FINBOX_DB_URL", os.Getenv("TEST_DB_URL"))
	var out, errb bytes.Buffer
	code := run([]string{"finbox", "list", "--json"}, &out, &errb)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	var rows []map[string]any
	if err := json.Unmarshal(out.Bytes(), &rows); err != nil || len(rows) != 1 {
		t.Fatalf("json: %v %s", err, out.String())
	}
	if rows[0]["merchant"] != "Walmart" || rows[0]["amount_minor"].(float64) != 36400 {
		t.Fatalf("row = %+v", rows[0])
	}
}

func TestCLIUnknownFlagJSONError(t *testing.T) {
	var out, errb bytes.Buffer
	code := run([]string{"finbox", "list", "--json", "--nope"}, &out, &errb)
	if code != 2 {
		t.Fatalf("exit = %d, want 2 (stderr: %s)", code, errb.String())
	}
	var e map[string]string
	if err := json.Unmarshal(errb.Bytes(), &e); err != nil || e["error"] == "" {
		t.Fatalf("stderr should be JSON {\"error\":...}, got %q (err: %v)", errb.String(), err)
	}
}

func TestCLIEditNotFoundExit3(t *testing.T) {
	_, _ = cliStore(t)
	t.Setenv("FINBOX_DB_URL", os.Getenv("TEST_DB_URL"))
	var out, errb bytes.Buffer
	code := run([]string{"finbox", "edit", "deadbeef", "--total", "10", "--json"}, &out, &errb)
	if code != 3 {
		t.Fatalf("exit = %d, want 3 (stderr: %s)", code, errb.String())
	}
	if !strings.Contains(errb.String(), "error") {
		t.Errorf("stderr should be JSON error, got %q", errb.String())
	}
}
