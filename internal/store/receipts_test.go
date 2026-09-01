package store

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func mkReceipt(t *testing.T, s *Store, sha string, msgID int64) Receipt {
	t.Helper()
	r, err := s.CreateReceipt(context.Background(), CreateReceiptParams{
		BlobKey: "2026/09/" + sha + ".jpg", BlobSHA256: sha, TgMessageID: msgID, TgChatID: 7,
	})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestReceiptLifecycle(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	r := mkReceipt(t, s, "sha-a", 100)
	if r.Status != "pending" {
		t.Fatalf("status = %s", r.Status)
	}

	if err := s.SetExtraction(ctx, r.ID, []byte(`{"merchant":"X"}`), "gpt-4o-mini"); err != nil {
		t.Fatal(err)
	}
	ok, err := s.Transition(ctx, r.ID, "pending", "awaiting_confirm", "")
	if err != nil || !ok {
		t.Fatalf("transition: %v %v", ok, err)
	}
	// illegal transition is a no-op, not an error
	ok, err = s.Transition(ctx, r.ID, "pending", "failed", "")
	if err != nil || ok {
		t.Fatalf("stale transition should be false, got %v %v", ok, err)
	}
	got, err := s.GetReceipt(ctx, r.ID)
	if err != nil || got.Status != "awaiting_confirm" {
		t.Fatalf("got %+v err %v", got, err)
	}
	// jsonb normalizes whitespace — compare semantically, never as a string
	var ex map[string]any
	if json.Unmarshal(got.Extraction, &ex) != nil || ex["merchant"] != "X" {
		t.Fatalf("extraction = %s", got.Extraction)
	}
}

func TestDuplicateBlob(t *testing.T) {
	s := NewTest(t)
	r := mkReceipt(t, s, "sha-dup", 200)
	_, err := s.CreateReceipt(context.Background(), CreateReceiptParams{
		BlobKey: "k2", BlobSHA256: "sha-dup", TgMessageID: 201, TgChatID: 7,
	})
	var dup DuplicateBlobError
	if !errors.As(err, &dup) || dup.ExistingID != r.ID {
		t.Fatalf("err = %v", err)
	}
}

func TestClaimComplete(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	done, err := s.ClaimUpdate(ctx, 555)
	if err != nil || done {
		t.Fatalf("first claim: %v %v", done, err)
	}
	done, err = s.ClaimUpdate(ctx, 555) // redelivery of in-flight update → NOT done
	if err != nil || done {
		t.Fatalf("in-flight reclaim: %v %v", done, err)
	}
	if err := s.CompleteUpdate(ctx, 555); err != nil {
		t.Fatal(err)
	}
	done, err = s.ClaimUpdate(ctx, 555) // after completion → done
	if err != nil || !done {
		t.Fatalf("completed reclaim: %v %v", done, err)
	}
	n, err := s.PurgeProcessedUpdates(ctx, 0) // everything is "older than 0"
	if err != nil || n != 1 {
		t.Fatalf("purge: %d %v", n, err)
	}
}
