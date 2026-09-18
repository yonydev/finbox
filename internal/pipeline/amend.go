package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"finbox/internal/correct"
	"finbox/internal/extract"
	"finbox/internal/messages"
	"finbox/internal/money"
	"finbox/internal/store"
	"finbox/internal/validate"
)

// Amend merges a reply-correction into a receipt that is awaiting confirmation
// (or failed — the correction may be exactly what it lacked), re-validates and
// returns a Result the caller renders like any other: persist → validate →
// transition, same shape as runExtraction.
func Amend(ctx context.Context, d Deps, receiptID string, f correct.Fields, now time.Time) (Result, error) {
	res := Result{ReceiptID: receiptID, Outcome: OutcomeRejected}
	rec, err := d.Store.GetReceipt(ctx, receiptID)
	if err != nil {
		return Result{}, err
	}
	switch rec.Status {
	case "pending":
		res.FailReason = messages.ReceiptStillReading
		return res, nil
	case "confirmed":
		res.FailReason = messages.AlreadySaved
		return res, nil
	case "discarded":
		res.FailReason = messages.ReceiptInactive
		return res, nil
	}
	var stored extract.Extraction
	_ = json.Unmarshal(rec.Extraction, &stored) // "null" or {} on a failed receipt is fine: zero value
	patch := map[string]string{}
	if f.Total != "" {
		// validate.Run rejects non-positive totals; persisting one would strand
		// the card unconfirmable, so check before the write
		currency := f.Currency
		if currency == "" {
			currency = stored.Currency
		}
		minor, err := money.ParseMinor(f.Total, currency)
		if err != nil {
			res.FailReason = err.Error()
			return res, nil
		}
		if minor <= 0 {
			res.FailReason = messages.TotalMustBePositive
			return res, nil
		}
		patch["total"] = f.Total
	}
	if f.Merchant != "" {
		patch["merchant"] = validate.Scrub(f.Merchant) // the stored jsonb is post-scrub, always
	}
	if f.Date != "" {
		patch["date"] = f.Date
	}
	if f.Currency != "" {
		patch["currency"] = f.Currency
	}
	raw, _ := json.Marshal(patch)
	prev, merged, err := d.Store.AmendExtraction(ctx, rec.ID, raw)
	if errors.Is(err, store.ErrNotFound) { // status changed under us
		res.FailReason = messages.ReceiptInactive
		return res, nil
	}
	if err != nil {
		return Result{}, err
	}
	var ex extract.Extraction
	if err := json.Unmarshal(merged, &ex); err != nil {
		return Result{}, err
	}
	v, verr := validate.Run(ex, now, d.Loc)
	if verr != nil {
		// progressive correction: the patch stays, the receipt stays (or
		// becomes) failed and the card says what is still missing
		if prev != "failed" {
			if _, terr := d.Store.Transition(ctx, rec.ID, prev, "failed", verr.Error()); terr != nil {
				return Result{}, terr
			}
		}
		res.Outcome, res.FailReason = OutcomeFailed, verr.Error()
		return res, nil
	}
	if f.Total != "" { // the human is the authority on the total
		kept := v.Warnings[:0]
		for _, w := range v.Warnings {
			if !strings.HasPrefix(w, validate.ItemsWarnPrefix) {
				kept = append(kept, w)
			}
		}
		v.Warnings = kept
	}
	if dup, err := d.Store.HasDuplicate(ctx, v.OccurredOn, v.AmountMinor); err == nil && dup {
		v.Warnings = append(v.Warnings, "⚠️ posible duplicado: ya hay un gasto con esa fecha y monto")
	}
	if prev == "failed" {
		if _, err := d.Store.Transition(ctx, rec.ID, "failed", "awaiting_confirm", ""); err != nil {
			return Result{}, err
		}
	}
	res.Outcome, res.FailReason, res.Validated = OutcomeAwaitingConfirm, "", v
	return res, nil
}
