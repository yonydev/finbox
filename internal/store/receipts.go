package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var ErrNotFound = errors.New("not found")

type DuplicateBlobError struct{ ExistingID string }

func (e DuplicateBlobError) Error() string {
	return fmt.Sprintf("duplicate blob, existing receipt %s", e.ExistingID)
}

type Receipt struct {
	ID, BlobKey, BlobSHA256, Status, FailReason, Model string
	Extraction                                         []byte
	TgMessageID, TgChatID, TgCardMessageID             int64
	CreatedAt                                          time.Time
}

type CreateReceiptParams struct {
	BlobKey, BlobSHA256   string
	TgMessageID, TgChatID int64
}

const receiptCols = `id, blob_key, blob_sha256, status, coalesce(fail_reason,''),
	coalesce(model,''), coalesce(extraction,'null'::jsonb),
	coalesce(tg_message_id,0), coalesce(tg_chat_id,0), coalesce(tg_card_message_id,0), created_at`

func scanReceipt(row pgx.Row) (Receipt, error) {
	var r Receipt
	err := row.Scan(&r.ID, &r.BlobKey, &r.BlobSHA256, &r.Status, &r.FailReason,
		&r.Model, &r.Extraction, &r.TgMessageID, &r.TgChatID, &r.TgCardMessageID, &r.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, ErrNotFound
	}
	return r, err
}

func (s *Store) CreateReceipt(ctx context.Context, p CreateReceiptParams) (Receipt, error) {
	row := s.pool.QueryRow(ctx, `insert into receipts (blob_key, blob_sha256, status, tg_message_id, tg_chat_id)
		values ($1,$2,'pending',$3,$4) returning `+receiptCols,
		p.BlobKey, p.BlobSHA256, p.TgMessageID, p.TgChatID)
	r, err := scanReceipt(row)
	var pgErr *pgconn.PgError
	// tg_message_id is ALSO unique — gate on the constraint name, not just 23505
	if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "receipts_blob_sha256_key" {
		var existing string
		if e2 := s.pool.QueryRow(ctx, `select id from receipts where blob_sha256=$1`, p.BlobSHA256).Scan(&existing); e2 == nil {
			return Receipt{}, DuplicateBlobError{ExistingID: existing}
		}
	}
	return r, err
}

func (s *Store) GetReceipt(ctx context.Context, id string) (Receipt, error) {
	return scanReceipt(s.pool.QueryRow(ctx, `select `+receiptCols+` from receipts where id=$1`, id))
}

func (s *Store) SetExtraction(ctx context.Context, id string, extraction []byte, model string) error {
	_, err := s.pool.Exec(ctx, `update receipts set extraction=$2, model=$3, updated_at=now() where id=$1`, id, extraction, model)
	return err
}

func (s *Store) SetCard(ctx context.Context, id string, chatID, cardMsgID int64) error {
	_, err := s.pool.Exec(ctx, `update receipts set tg_chat_id=$2, tg_card_message_id=$3, updated_at=now() where id=$1`, id, chatID, cardMsgID)
	return err
}

// GetReceiptByCard finds the receipt whose confirm/discard card lives at
// this chat+message (used to resolve a reply-to-summary text into a receipt).
func (s *Store) GetReceiptByCard(ctx context.Context, chatID, cardMsgID int64) (Receipt, error) {
	return scanReceipt(s.pool.QueryRow(ctx,
		`select `+receiptCols+` from receipts where tg_chat_id=$1 and tg_card_message_id=$2`, chatID, cardMsgID))
}

func (s *Store) Transition(ctx context.Context, id, from, to, failReason string) (bool, error) {
	tag, err := s.pool.Exec(ctx,
		`update receipts set status=$3, fail_reason=nullif($4,''), updated_at=now() where id=$1 and status=$2`,
		id, from, to, failReason)
	return tag.RowsAffected() == 1, err
}

func (s *Store) receiptsByStatus(ctx context.Context, statuses ...string) ([]Receipt, error) {
	rows, err := s.pool.Query(ctx, `select `+receiptCols+` from receipts where status = any($1) order by created_at`, statuses)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Receipt
	for rows.Next() {
		r, err := scanReceipt(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) PendingReceipts(ctx context.Context) ([]Receipt, error) {
	return s.receiptsByStatus(ctx, "awaiting_confirm", "failed")
}

func (s *Store) StalePending(ctx context.Context) ([]Receipt, error) {
	return s.receiptsByStatus(ctx, "pending")
}

func (s *Store) ClaimUpdate(ctx context.Context, updateID int64) (bool, error) {
	var completed *time.Time
	err := s.pool.QueryRow(ctx, `insert into processed_updates (update_id) values ($1)
		on conflict (update_id) do update set update_id = excluded.update_id
		returning completed_at`, updateID).Scan(&completed)
	if err != nil {
		return false, err
	}
	return completed != nil, nil
}

func (s *Store) CompleteUpdate(ctx context.Context, updateID int64) error {
	_, err := s.pool.Exec(ctx, `update processed_updates set completed_at=now() where update_id=$1`, updateID)
	return err
}

func (s *Store) PurgeProcessedUpdates(ctx context.Context, olderThan time.Duration) (int64, error) {
	tag, err := s.pool.Exec(ctx, `delete from processed_updates where created_at < now() - $1::interval`,
		fmt.Sprintf("%d seconds", int(olderThan.Seconds())))
	return tag.RowsAffected(), err
}
