package pipeline

import (
	"context"
	"errors"
	"strings"
	"testing"

	"finbox/internal/correct"
	"finbox/internal/extract"
	"finbox/internal/messages"
	"finbox/internal/validate"
)

func itemsResult() extract.Result {
	r := goodResult()
	r.Extraction.Items = []extract.Item{{Name: "Café", Amount: "364.00"}}
	return r
}

func hasPrefix(ws []string, p string) bool {
	for _, w := range ws {
		if strings.HasPrefix(w, p) {
			return true
		}
	}
	return false
}

func TestAmendPendingCardMultiField(t *testing.T) {
	ctx := context.Background()
	d := deps(t, &fakeExtractor{res: itemsResult()})
	ing, _ := IngestPhoto(ctx, d, jpegBytes(), 200, 7, now)

	res, err := Amend(ctx, d, ing.ReceiptID, correct.Fields{Total: "400", Date: "2026-08-27", Merchant: "Soriana"}, now)
	if err != nil || res.Outcome != OutcomeAwaitingConfirm || res.ReceiptID != ing.ReceiptID {
		t.Fatalf("amend: %+v %v", res, err)
	}
	v := res.Validated
	if v.AmountMinor != 40000 || v.Merchant != "Walmart" || v.MerchantCanon != "Soriana" || v.OccurredOn.Format("2006-01-02") != "2026-08-27" || len(v.Items) != 1 {
		t.Fatalf("validated = %+v", v)
	}
	if hasPrefix(v.Warnings, validate.ItemsWarnPrefix) { // S4: the human's total is not second-guessed
		t.Fatalf("items warning must be suppressed: %v", v.Warnings)
	}
	rec, _ := d.Store.GetReceipt(ctx, ing.ReceiptID)
	if rec.Status != "awaiting_confirm" || !strings.Contains(string(rec.ExtractionRaw), `"364.00"`) {
		t.Fatalf("receipt %+v", rec)
	}
	// a second amend keeps the raw copy and a merchant-only patch shows the items warning again
	res, _ = Amend(ctx, d, ing.ReceiptID, correct.Fields{Merchant: "OXXO"}, now)
	if res.Outcome != OutcomeAwaitingConfirm || !hasPrefix(res.Validated.Warnings, validate.ItemsWarnPrefix) {
		t.Fatalf("second amend: %+v", res)
	}
	rec, _ = d.Store.GetReceipt(ctx, ing.ReceiptID)
	if !strings.Contains(string(rec.ExtractionRaw), `"Walmart"`) {
		t.Fatalf("raw overwritten: %s", rec.ExtractionRaw)
	}
}

func TestAmendRevivesFailedProgressively(t *testing.T) {
	ctx := context.Background()
	d := deps(t, &fakeExtractor{err: errors.New("boom")})
	ing, _ := IngestPhoto(ctx, d, jpegBytes(), 201, 7, now) // failed, extraction NULL

	res, err := Amend(ctx, d, ing.ReceiptID, correct.Fields{Total: "285"}, now)
	if err != nil || res.Outcome != OutcomeFailed || !strings.Contains(res.FailReason, "comercio") {
		t.Fatalf("first amend: %+v %v", res, err)
	}
	rec, _ := d.Store.GetReceipt(ctx, ing.ReceiptID)
	if rec.Status != "failed" || !strings.Contains(string(rec.Extraction), `"285"`) {
		t.Fatalf("receipt %+v", rec)
	}
	res, err = Amend(ctx, d, ing.ReceiptID, correct.Fields{Merchant: "OXXO", Date: "2026-08-28"}, now)
	if err != nil || res.Outcome != OutcomeAwaitingConfirm || res.Validated.AmountMinor != 28500 {
		t.Fatalf("second amend: %+v %v", res, err)
	}
	rec, _ = d.Store.GetReceipt(ctx, ing.ReceiptID)
	if rec.Status != "awaiting_confirm" || rec.FailReason != "" || string(rec.ExtractionRaw) != "{}" {
		t.Fatalf("receipt %+v", rec)
	}
}

func TestAmendRejects(t *testing.T) {
	ctx := context.Background()
	d := deps(t, &fakeExtractor{res: goodResult()})
	ing, _ := IngestPhoto(ctx, d, jpegBytes(), 202, 7, now)

	res, err := Amend(ctx, d, ing.ReceiptID, correct.Fields{Total: "-120"}, now)
	if err != nil || res.Outcome != OutcomeRejected || res.FailReason != messages.TotalMustBePositive {
		t.Fatalf("negative: %+v %v", res, err)
	}
	rec, _ := d.Store.GetReceipt(ctx, ing.ReceiptID)
	if rec.ExtractionRaw != nil || !strings.Contains(string(rec.Extraction), `"364.00"`) {
		t.Fatalf("rejected amend must not write: %+v", rec)
	}
	confirm(t, d, ing.ReceiptID)
	if res, _ = Amend(ctx, d, ing.ReceiptID, correct.Fields{Total: "1"}, now); res.Outcome != OutcomeRejected || res.FailReason != messages.AlreadySaved {
		t.Fatalf("confirmed: %+v", res)
	}
	// still pending (extraction in flight) → wait, don't retry
	other, _ := IngestPhoto(ctx, d, append(jpegBytes(), 1), 203, 7, now)
	d.Store.Transition(ctx, other.ReceiptID, "awaiting_confirm", "pending", "")
	if res, _ = Amend(ctx, d, other.ReceiptID, correct.Fields{Total: "1"}, now); res.FailReason != messages.ReceiptStillReading {
		t.Fatalf("pending: %+v", res)
	}
}
