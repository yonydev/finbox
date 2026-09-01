package pipeline

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"finbox/internal/extract"
	"finbox/internal/store"
)

type fakeBlob struct{ m map[string][]byte }

func (f *fakeBlob) Put(_ context.Context, k string, d []byte) error { f.m[k] = d; return nil }
func (f *fakeBlob) Get(_ context.Context, k string) ([]byte, error) {
	d, ok := f.m[k]
	if !ok {
		return nil, fmt.Errorf("missing %s", k)
	}
	return d, nil
}

type fakeExtractor struct {
	res   extract.Result
	err   error
	calls int
}

func (f *fakeExtractor) Extract(context.Context, []byte, string) (extract.Result, error) {
	f.calls++
	return f.res, f.err
}

func jpegBytes() []byte {
	return append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, make([]byte, 32)...)
}

func goodResult() extract.Result {
	return extract.Result{
		Extraction: extract.Extraction{Merchant: "Walmart", Date: "2026-08-28", Currency: "MXN", Total: "364.00"},
		Model:      "gpt-4o-mini", RawJSON: []byte(`{"merchant":"Walmart"}`),
	}
}

func deps(t *testing.T, ex Extractor) Deps {
	return Deps{
		Store: store.NewTest(t), Blob: &fakeBlob{m: map[string][]byte{}},
		Extractor: ex, Loc: time.UTC, Log: slog.Default(),
		Backoff: func(int) time.Duration { return 0 }, // retries must not sleep in tests
	}
}

var now = time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)

func TestIngestHappyPath(t *testing.T) {
	d := deps(t, &fakeExtractor{res: goodResult()})
	res, err := IngestPhoto(context.Background(), d, jpegBytes(), 100, 7, now)
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != OutcomeAwaitingConfirm {
		t.Fatalf("outcome = %v (%s)", res.Outcome, res.FailReason)
	}
	r, err := d.Store.GetReceipt(context.Background(), res.ReceiptID)
	if err != nil || r.Status != "awaiting_confirm" || r.Model != "gpt-4o-mini" {
		t.Fatalf("receipt %+v %v", r, err)
	}
}

func TestIngestRejectsBadImage(t *testing.T) {
	d := deps(t, &fakeExtractor{res: goodResult()})
	res, err := IngestPhoto(context.Background(), d, []byte("not an image at all........."), 101, 7, now)
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != OutcomeRejected {
		t.Fatalf("outcome = %v", res.Outcome)
	}
}

func TestIngestDuplicatePhoto(t *testing.T) {
	fe := &fakeExtractor{res: goodResult()}
	d := deps(t, fe)
	first, _ := IngestPhoto(context.Background(), d, jpegBytes(), 102, 7, now)
	res, err := IngestPhoto(context.Background(), d, jpegBytes(), 103, 7, now) // same bytes → same sha
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != OutcomeDuplicate || res.DuplicateOfID != first.ReceiptID {
		t.Fatalf("res = %+v", res)
	}
}

func TestIngestRetriesThenFails(t *testing.T) {
	fe := &fakeExtractor{err: errors.New("boom 503")}
	d := deps(t, fe)
	res, err := IngestPhoto(context.Background(), d, jpegBytes(), 104, 7, now)
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != OutcomeFailed || fe.calls != 3 { // 1 + 2 retries
		t.Fatalf("outcome=%v calls=%d", res.Outcome, fe.calls)
	}
}

func TestIngestNoRetryOnNonRetryable(t *testing.T) {
	fe := &fakeExtractor{err: fmt.Errorf("%w: 401", extract.ErrNonRetryable)}
	d := deps(t, fe)
	res, _ := IngestPhoto(context.Background(), d, jpegBytes(), 105, 7, now)
	if res.Outcome != OutcomeFailed || fe.calls != 1 {
		t.Fatalf("outcome=%v calls=%d", res.Outcome, fe.calls)
	}
}

func TestReprocessFromFailed(t *testing.T) {
	fe := &fakeExtractor{err: errors.New("boom")}
	d := deps(t, fe)
	res, _ := IngestPhoto(context.Background(), d, jpegBytes(), 106, 7, now)
	fe.err, fe.res = nil, goodResult()
	res2, err := Reprocess(context.Background(), d, res.ReceiptID, now)
	if err != nil || res2.Outcome != OutcomeAwaitingConfirm {
		t.Fatalf("reprocess: %+v %v", res2, err)
	}
}
