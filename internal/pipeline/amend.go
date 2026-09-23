package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"finbox/internal/correct"
	"finbox/internal/extract"
	"finbox/internal/merchant"
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
	patch := map[string]string{}
	if f.Total != "" {
		// validate.Run rejects non-positive totals; persisting one would strand
		// the card unconfirmable, so check the sign before the write
		minor, err := money.ParseMinor(f.Total, f.Currency)
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
		// the rename lands on the canon; the raw receipt text is kept as read
		patch["merchant_canon"] = validate.Scrub(f.Merchant) // the stored jsonb is post-scrub, always
	}
	if f.Date != "" {
		patch["date"] = f.Date
	}
	if f.Currency != "" {
		patch["currency"] = f.Currency
	}
	if f.Category != "" {
		patch["category"] = f.Category // validate.Run derives the source
	}
	raw, _ := json.Marshal(patch)
	prev, merged, err := d.Store.AmendExtraction(ctx, receiptID, raw)
	if errors.Is(err, store.ErrNotFound) {
		rec, err := d.Store.GetReceipt(ctx, receiptID)
		if err != nil {
			return Result{}, err
		}
		switch rec.Status {
		case "pending":
			res.FailReason = messages.ReceiptStillReading
		case "confirmed":
			res.FailReason = messages.AlreadySaved
		default:
			res.FailReason = messages.ReceiptInactive
		}
		return res, nil
	}
	if err != nil {
		return Result{}, err
	}
	var ex extract.Extraction
	if err := json.Unmarshal(merged, &ex); err != nil {
		return Result{}, err
	}
	if ex.Merchant == "" && ex.MerchantCanon != "" {
		// the receipt failed because the merchant was unreadable: `comercio X`
		// has to fill the raw too, or validate.Run keeps rejecting it
		ex.Merchant = ex.MerchantCanon
		fill, _ := json.Marshal(map[string]string{"merchant": ex.Merchant})
		if _, _, err := d.Store.AmendExtraction(ctx, receiptID, fill); err != nil {
			return Result{}, err
		}
	}
	v, verr := validate.Run(ex, now, d.Loc)
	if verr != nil {
		// progressive correction: the patch stays, the receipt stays (or
		// becomes) failed and the card says what is still missing
		if prev != "failed" {
			if _, terr := d.Store.Transition(ctx, receiptID, prev, "failed", verr.Error()); terr != nil {
				return Result{}, terr
			}
		}
		res.Outcome, res.FailReason = OutcomeFailed, verr.Error()
		return res, nil
	}
	if f.Total != "" { // the human is the authority on the total
		v.Warnings = slices.DeleteFunc(v.Warnings, func(w string) bool { return strings.HasPrefix(w, validate.ItemsWarnPrefix) })
	}
	if dup, err := d.Store.HasDuplicate(ctx, v.OccurredOn, v.AmountMinor); err == nil && dup {
		v.Warnings = append(v.Warnings, "⚠️ posible duplicado: ya hay un gasto con esa fecha y monto")
	}
	if prev == "failed" {
		if _, err := d.Store.Transition(ctx, receiptID, "failed", "awaiting_confirm", ""); err != nil {
			return Result{}, err
		}
	}
	res.Outcome, res.FailReason, res.Validated, res.Edited = OutcomeAwaitingConfirm, "", v, true
	return res, nil
}

// EditsFromRaw diffs the model's original extraction against what confirm is
// about to save, so the edit_log records exactly what the human corrected.
// A field absent or empty in raw is skipped: with no original value there is
// nothing to diff, only a phantom row.
func EditsFromRaw(raw []byte, v validate.Validated, source string) []store.FieldEdit {
	if len(raw) == 0 {
		return nil // never amended
	}
	var ex extract.Extraction
	if err := json.Unmarshal(raw, &ex); err != nil {
		return nil
	}
	var edits []store.FieldEdit
	add := func(field, old, final string) {
		if old == "" || old == final {
			return
		}
		edits = append(edits, store.FieldEdit{Field: field, Old: old, New: final, Source: source})
	}
	// the raw total reads against the raw currency: "285.50" is 28550 MXN
	// centavos but an invalid JPY amount
	rawCur := strings.ToUpper(strings.TrimSpace(ex.Currency))
	if money.Known(rawCur) {
		add("currency", rawCur, v.Currency)
	} else {
		rawCur = v.Currency // validate's fallback, not a human correction
	}
	if minor, err := money.ParseMinor(ex.Total, rawCur); err == nil {
		add("total", strconv.FormatInt(minor, 10), strconv.FormatInt(v.AmountMinor, 10))
	}
	// the user renames the canon, so that is what the edit_log records
	add("merchant", merchant.Canon(validate.ScrubMerchant(ex.Merchant)), v.MerchantCanon)
	add("date", ex.Date, v.OccurredOn.Format("2006-01-02"))
	add("category", ex.Category, v.Category)
	return edits
}
