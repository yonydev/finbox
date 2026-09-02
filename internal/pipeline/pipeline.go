package pipeline

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"finbox/internal/blob/fs"
	"finbox/internal/extract"
	"finbox/internal/imgtype"
	"finbox/internal/messages"
	"finbox/internal/store"
	"finbox/internal/validate"
)

const MaxImageBytes = 20 << 20

type Extractor interface {
	Extract(ctx context.Context, image []byte, mime string) (extract.Result, error)
}

type BlobStore interface {
	Put(ctx context.Context, key string, data []byte) error
	Get(ctx context.Context, key string) ([]byte, error)
}

type Deps struct {
	Store     *store.Store
	Blob      BlobStore
	Extractor Extractor
	Loc       *time.Location
	Log       *slog.Logger
	Backoff   func(attempt int) time.Duration // nil → default 2s·attempt
}

func (d Deps) backoff(attempt int) time.Duration {
	if d.Backoff != nil {
		return d.Backoff(attempt)
	}
	return time.Duration(attempt) * 2 * time.Second
}

type Outcome int

const (
	OutcomeAwaitingConfirm Outcome = iota + 1
	OutcomeFailed
	OutcomeDuplicate
	OutcomeRejected
)

type Result struct {
	ReceiptID     string
	Outcome       Outcome
	Validated     validate.Validated
	FailReason    string
	DuplicateOfID string
}

func IngestPhoto(ctx context.Context, d Deps, image []byte, tgMessageID, tgChatID int64, now time.Time) (Result, error) {
	if len(image) > MaxImageBytes {
		return Result{Outcome: OutcomeRejected, FailReason: messages.TooBig}, nil
	}
	ty, ok := imgtype.Sniff(image)
	if !ok {
		return Result{Outcome: OutcomeRejected, FailReason: messages.UnsupportedFormat}, nil
	}
	sum := sha256.Sum256(image)
	sha := hex.EncodeToString(sum[:])
	key := fs.Key(now.In(d.Loc), sha, ty.Ext())
	if err := d.Blob.Put(ctx, key, image); err != nil {
		return Result{}, err
	}
	rec, err := d.Store.CreateReceipt(ctx, store.CreateReceiptParams{
		BlobKey: key, BlobSHA256: sha, TgMessageID: tgMessageID, TgChatID: tgChatID,
	})
	var dup store.DuplicateBlobError
	if errors.As(err, &dup) {
		return Result{Outcome: OutcomeDuplicate, DuplicateOfID: dup.ExistingID}, nil
	}
	if err != nil {
		return Result{}, err
	}
	return runExtraction(ctx, d, rec, image, ty.MIME(), "pending", now)
}

// Reprocess re-runs extract+validate from the stored blob (spec §4 transitions).
func Reprocess(ctx context.Context, d Deps, receiptID string, now time.Time) (Result, error) {
	rec, err := d.Store.GetReceipt(ctx, receiptID)
	if err != nil {
		return Result{}, err
	}
	allowed := rec.Status == "pending" || rec.Status == "failed" || rec.Status == "discarded"
	if !allowed && rec.Status == "confirmed" {
		// A confirmed receipt is only reprocessable once its transaction has
		// been voided — ErrNotFound here means there's no active txn left.
		if _, err := d.Store.GetActiveTxnForReceipt(ctx, receiptID); errors.Is(err, store.ErrNotFound) {
			allowed = true
		} else if err != nil {
			return Result{}, err
		}
	}
	if !allowed {
		return Result{ReceiptID: rec.ID, Outcome: OutcomeRejected,
			FailReason: "solo se reprocesa un recibo pending/failed/discarded"}, nil
	}
	image, err := d.Blob.Get(ctx, rec.BlobKey)
	if err != nil {
		return Result{}, err
	}
	ty, ok := imgtype.Sniff(image)
	if !ok {
		return Result{ReceiptID: rec.ID, Outcome: OutcomeFailed, FailReason: "blob corrupto"}, nil
	}
	return runExtraction(ctx, d, rec, image, ty.MIME(), rec.Status, now)
}

func runExtraction(ctx context.Context, d Deps, rec store.Receipt, image []byte, mime, fromStatus string, now time.Time) (res Result, err error) {
	defer func() {
		if r := recover(); r != nil {
			reason := fmt.Sprintf("panic: %v", r)
			d.Log.Error("panic in runExtraction", "receipt", rec.ID, "panic", r)
			if _, terr := d.Store.Transition(ctx, rec.ID, fromStatus, "failed", reason); terr != nil {
				res, err = Result{}, terr
				return
			}
			res, err = Result{ReceiptID: rec.ID, Outcome: OutcomeFailed, FailReason: reason}, nil
		}
	}()
	res = Result{ReceiptID: rec.ID}
	exRes, err := extractWithRetry(ctx, d, image, mime)
	if err != nil {
		reason := failReason(err)
		ok, terr := d.Store.Transition(ctx, rec.ID, fromStatus, "failed", reason)
		if terr != nil {
			return Result{}, terr
		}
		if !ok {
			d.Log.Warn("transition lost race", "receipt", rec.ID, "from", fromStatus)
		}
		d.Log.Warn("receipt failed", "receipt", rec.ID, "reason", reason)
		res.Outcome, res.FailReason = OutcomeFailed, reason
		return res, nil
	}
	scrubbed := scrubResult(exRes)
	raw, _ := json.Marshal(scrubbed.Extraction)
	if err := d.Store.SetExtraction(ctx, rec.ID, raw, scrubbed.Model); err != nil {
		return Result{}, err
	}
	d.Log.Info("extraction done", "receipt", rec.ID, "model", scrubbed.Model,
		"prompt_tokens", exRes.PromptTokens, "completion_tokens", exRes.CompletionTokens)
	v, verr := validate.Run(scrubbed.Extraction, now, d.Loc)
	if verr != nil {
		reason := verr.Error()
		ok, terr := d.Store.Transition(ctx, rec.ID, fromStatus, "failed", reason)
		if terr != nil {
			return Result{}, terr
		}
		if !ok {
			d.Log.Warn("transition lost race", "receipt", rec.ID, "from", fromStatus)
		}
		d.Log.Warn("receipt failed", "receipt", rec.ID, "reason", reason)
		res.Outcome, res.FailReason = OutcomeFailed, reason
		return res, nil
	}
	if dup, err := d.Store.HasDuplicate(ctx, v.OccurredOn, v.AmountMinor); err == nil && dup {
		v.Warnings = append(v.Warnings, "⚠️ posible duplicado: ya hay un gasto con esa fecha y monto")
	}
	ok, err := d.Store.Transition(ctx, rec.ID, fromStatus, "awaiting_confirm", "")
	if err != nil {
		return Result{}, err
	}
	if !ok {
		d.Log.Warn("transition lost race", "receipt", rec.ID, "from", fromStatus)
	}
	res.Outcome, res.Validated = OutcomeAwaitingConfirm, v
	return res, nil
}

func extractWithRetry(ctx context.Context, d Deps, image []byte, mime string) (extract.Result, error) {
	var last error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return extract.Result{}, ctx.Err()
			case <-time.After(d.backoff(attempt)):
			}
		}
		res, err := d.Extractor.Extract(ctx, image, mime)
		if err == nil {
			return res, nil
		}
		last = err
		d.Log.Warn("extract attempt failed", "attempt", attempt, "err", err)
		if errors.Is(err, extract.ErrNonRetryable) {
			return extract.Result{}, err
		}
	}
	return extract.Result{}, last
}

func failReason(err error) string {
	if errors.Is(err, extract.ErrNonRetryable) {
		return "OpenAI rechazó la petición (¿API key inválida o sin crédito?)"
	}
	return "no pude leer el ticket (error temporal de extracción)"
}

func scrubResult(r extract.Result) extract.Result {
	r.Extraction.Merchant = validate.Scrub(r.Extraction.Merchant)
	for i := range r.Extraction.Items {
		r.Extraction.Items[i].Name = validate.Scrub(r.Extraction.Items[i].Name)
	}
	return r
}
