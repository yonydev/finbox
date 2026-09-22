package store

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

var ErrAmbiguous = errors.New("ambiguous id prefix")

type NewItem struct {
	Position      int
	Name          string
	QuantityMilli *int64
	AmountMinor   *int64
}

type NewTransaction struct {
	OccurredOn time.Time
	Merchant   string
	// MerchantCanon is the name shown to the user; "" means "same as Merchant".
	MerchantCanon string
	AmountMinor   int64
	Currency      string
	Source        string
	Items         []NewItem
	Edits         []FieldEdit // corrections applied before confirm (reply-parser); logged in the same tx
}

type TxnRow struct {
	ID, ShortID, Merchant, MerchantCanon, Currency, Source, ReceiptID string
	OccurredOn                                                        time.Time
	AmountMinor                                                       int64
	Edited                                                            bool // has edit_log rows; only populated by ListTransactions
}

type CurrencyTotal struct {
	Currency    string
	AmountMinor int64
}

// FieldEdit is one edit_log row; Source "" means 'cli'.
type FieldEdit struct{ Field, Old, New, Source string }

const insertEditLog = `insert into edit_log (transaction_id, field, old_value, new_value, source)
	values ($1,$2,$3,$4,coalesce(nullif($5,''),'cli'))`

// AddTransaction inserts a manual (receipt-less) transaction and returns it.
func (s *Store) AddTransaction(ctx context.Context, t NewTransaction) (TxnRow, error) {
	var id string
	err := s.pool.QueryRow(ctx, `insert into transactions (occurred_on, merchant, merchant_canon, amount_minor, currency, source)
		values ($1,$2,coalesce(nullif($3,''),$2),$4,$5,$6) returning id`,
		t.OccurredOn, t.Merchant, t.MerchantCanon, t.AmountMinor, t.Currency, t.Source).Scan(&id)
	if err != nil {
		return TxnRow{}, err
	}
	return s.GetTransactionByID(ctx, id)
}

// ConfirmReceipt turns an awaiting_confirm receipt into a transaction. A
// non-nil ifUnchangedSince makes it a no-op when the receipt was modified
// after that instant — the caller built t from a card that may be stale.
func (s *Store) ConfirmReceipt(ctx context.Context, receiptID string, t NewTransaction, updateID int64, ifUnchangedSince *time.Time) (string, bool, error) {
	var txnID string
	confirmed := false
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx,
			`update receipts set status='confirmed', updated_at=now()
			where id=$1 and status='awaiting_confirm' and ($2::timestamptz is null or updated_at=$2)`, receiptID, ifUnchangedSince)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return nil // stale tap: no-op, completion-stamping is the caller's call
		}
		confirmed = true
		err = tx.QueryRow(ctx, `insert into transactions (receipt_id, occurred_on, merchant, merchant_canon, amount_minor, currency, source)
			values ($1,$2,$3,coalesce(nullif($4,''),$3),$5,$6,$7) returning id`,
			receiptID, t.OccurredOn, t.Merchant, t.MerchantCanon, t.AmountMinor, t.Currency, t.Source).Scan(&txnID)
		if err != nil {
			return err
		}
		for _, it := range t.Items {
			if _, err := tx.Exec(ctx, `insert into transaction_items (transaction_id, position, name, quantity_milli, amount_minor)
				values ($1,$2,$3,$4,$5)`, txnID, it.Position, it.Name, it.QuantityMilli, it.AmountMinor); err != nil {
				return err
			}
		}
		for _, e := range t.Edits {
			if _, err := tx.Exec(ctx, insertEditLog, txnID, e.Field, e.Old, e.New, e.Source); err != nil {
				return err
			}
		}
		if updateID != 0 {
			if _, err := tx.Exec(ctx, `update processed_updates set completed_at=now() where update_id=$1`, updateID); err != nil {
				return err
			}
		}
		return nil
	})
	return txnID, confirmed, err
}

func (s *Store) DiscardReceipt(ctx context.Context, receiptID string, updateID int64) (bool, error) {
	discarded := false
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx,
			`update receipts set status='discarded', updated_at=now() where id=$1 and status in ('awaiting_confirm','failed')`, receiptID)
		if err != nil {
			return err
		}
		discarded = tag.RowsAffected() == 1
		if updateID != 0 {
			_, err = tx.Exec(ctx, `update processed_updates set completed_at=now() where update_id=$1`, updateID)
		}
		return err
	})
	return discarded, err
}

func (s *Store) ListTransactions(ctx context.Context, limit, year int, month time.Month, loc *time.Location) ([]TxnRow, error) {
	q := `select t.id, t.merchant, coalesce(nullif(t.merchant_canon,''), t.merchant), t.currency, t.source, coalesce(t.receipt_id::text,''), t.occurred_on, t.amount_minor,
		exists(select 1 from edit_log el where el.transaction_id = t.id)
		from transactions t where t.voided_at is null`
	args := []any{}
	if year != 0 {
		from := time.Date(year, month, 1, 0, 0, 0, 0, loc)
		q += ` and t.occurred_on >= $1 and t.occurred_on < $2`
		args = append(args, from, from.AddDate(0, 1, 0))
	}
	q += fmt.Sprintf(` order by t.occurred_on desc, t.created_at desc limit %d`, limit)
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TxnRow
	for rows.Next() {
		var r TxnRow
		if err := rows.Scan(&r.ID, &r.Merchant, &r.MerchantCanon, &r.Currency, &r.Source, &r.ReceiptID, &r.OccurredOn, &r.AmountMinor, &r.Edited); err != nil {
			return nil, err
		}
		r.ShortID = r.ID[:8]
		out = append(out, r)
	}
	return out, rows.Err()
}

const txnRowCols = `id, merchant, coalesce(nullif(merchant_canon,''), merchant), currency, source, coalesce(receipt_id::text,''), occurred_on, amount_minor`

func scanTxnRow(row pgx.Row) (TxnRow, error) {
	var r TxnRow
	err := row.Scan(&r.ID, &r.Merchant, &r.MerchantCanon, &r.Currency, &r.Source, &r.ReceiptID, &r.OccurredOn, &r.AmountMinor)
	if errors.Is(err, pgx.ErrNoRows) {
		return TxnRow{}, ErrNotFound
	}
	if err != nil {
		return TxnRow{}, err
	}
	r.ShortID = r.ID[:8]
	return r, nil
}

// GetTransactionByID returns the active (non-voided) transaction with the given id.
func (s *Store) GetTransactionByID(ctx context.Context, id string) (TxnRow, error) {
	return scanTxnRow(s.pool.QueryRow(ctx, `select `+txnRowCols+` from transactions where id=$1 and voided_at is null`, id))
}

// GetActiveTxnForReceipt returns the active (non-voided) transaction for a receipt, if any.
func (s *Store) GetActiveTxnForReceipt(ctx context.Context, receiptID string) (TxnRow, error) {
	return scanTxnRow(s.pool.QueryRow(ctx, `select `+txnRowCols+` from transactions where receipt_id=$1 and voided_at is null`, receiptID))
}

func (s *Store) MonthTotals(ctx context.Context, year int, month time.Month, loc *time.Location) ([]CurrencyTotal, int, error) {
	from := time.Date(year, month, 1, 0, 0, 0, 0, loc)
	rows, err := s.pool.Query(ctx, `select currency, sum(amount_minor)::bigint, count(*) from transactions
		where voided_at is null and occurred_on >= $1 and occurred_on < $2
		group by currency order by currency`, from, from.AddDate(0, 1, 0))
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var totals []CurrencyTotal
	count := 0
	for rows.Next() {
		var ct CurrencyTotal
		var c int
		if err := rows.Scan(&ct.Currency, &ct.AmountMinor, &c); err != nil {
			return nil, 0, err
		}
		totals = append(totals, ct)
		count += c
	}
	return totals, count, rows.Err()
}

// merchant is not here on purpose: the raw receipt text is never edited, a
// rename writes merchant_canon.
var allowedEditCols = map[string]bool{"amount_minor": true, "merchant_canon": true, "occurred_on": true, "currency": true}

func (s *Store) EditTransaction(ctx context.Context, txnID string, set map[string]any, edits []FieldEdit) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		i := 2
		frags := []string{"updated_at=now()"}
		args := []any{txnID}
		for col, val := range set {
			if !allowedEditCols[col] {
				return fmt.Errorf("column %s is not editable", col)
			}
			frags = append(frags, fmt.Sprintf("%s=$%d", col, i))
			args = append(args, val)
			i++
		}
		tag, err := tx.Exec(ctx, `update transactions set `+strings.Join(frags, ", ")+` where id=$1 and voided_at is null`, args...)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return ErrNotFound
		}
		for _, e := range edits {
			if _, err := tx.Exec(ctx, insertEditLog, txnID, e.Field, e.Old, e.New, e.Source); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Store) VoidTransaction(ctx context.Context, txnID string) (bool, error) {
	tag, err := s.pool.Exec(ctx, `update transactions set voided_at=now(), updated_at=now() where id=$1 and voided_at is null`, txnID)
	return tag.RowsAffected() == 1, err
}

func (s *Store) HasDuplicate(ctx context.Context, occurredOn time.Time, amountMinor int64) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx, `select exists(select 1 from transactions where voided_at is null and occurred_on=$1 and amount_minor=$2)`,
		occurredOn, amountMinor).Scan(&exists)
	return exists, err
}

var hexPrefix = regexp.MustCompile(`^[0-9a-f]{8}$`)

// ResolveID resolves an 8-char uuid prefix or full uuid across receipts and
// transactions using a sargable range scan on the primary keys.
func (s *Store) ResolveID(ctx context.Context, prefix string) (string, string, error) {
	prefix = strings.ToLower(strings.TrimSpace(prefix))
	if len(prefix) == 36 { // full uuid: equality on both tables
		var id string
		if err := s.pool.QueryRow(ctx, `select id from receipts where id=$1`, prefix).Scan(&id); err == nil {
			return "receipt", id, nil
		}
		if err := s.pool.QueryRow(ctx, `select id from transactions where id=$1`, prefix).Scan(&id); err == nil {
			return "transaction", id, nil
		}
		return "", "", ErrNotFound
	}
	if !hexPrefix.MatchString(prefix) {
		return "", "", fmt.Errorf("id inválido: %q (8 hex o uuid completo)", prefix)
	}
	lo := prefix + "-0000-0000-0000-000000000000"
	hi := prefix + "-ffff-ffff-ffff-ffffffffffff"
	type hit struct{ kind, id string }
	var hits []hit
	for _, q := range []struct{ kind, sql string }{
		{"receipt", `select id from receipts where id between $1::uuid and $2::uuid limit 2`},
		{"transaction", `select id from transactions where id between $1::uuid and $2::uuid limit 2`},
	} {
		rows, err := s.pool.Query(ctx, q.sql, lo, hi)
		if err != nil {
			return "", "", err
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return "", "", err
			}
			hits = append(hits, hit{q.kind, id})
		}
		rows.Close()
	}
	switch len(hits) {
	case 0:
		return "", "", ErrNotFound
	case 1:
		return hits[0].kind, hits[0].id, nil
	default:
		return "", "", ErrAmbiguous
	}
}

// RecanonTransactions recomputes merchant_canon with canon for every active
// transaction the user has never renamed, and returns the changes as
// {id, raw, canon}. dryRun stops before the writes. Rows carrying a 'merchant'
// edit_log row are skipped: the human's name wins over the normalizer. Writes
// no edit_log rows — a normalizer pass is not a human edit — and is
// idempotent, so it is safe to run on every deploy.
func (s *Store) RecanonTransactions(ctx context.Context, canon func(string) string, dryRun bool) ([][3]string, error) {
	rows, err := s.pool.Query(ctx, `select t.id, t.merchant, t.merchant_canon from transactions t
		where t.voided_at is null
		  and not exists (select 1 from edit_log el where el.transaction_id = t.id and el.field = 'merchant')
		order by t.created_at`)
	if err != nil {
		return nil, err
	}
	var changes [][3]string
	for rows.Next() {
		var id, raw, cur string
		if err := rows.Scan(&id, &raw, &cur); err != nil {
			rows.Close()
			return nil, err
		}
		if c := canon(raw); c != cur {
			changes = append(changes, [3]string{id, raw, c})
		}
	}
	rows.Close() // not deferred: the updates below need the connection back
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if dryRun {
		return changes, nil
	}
	for _, c := range changes {
		if _, err := s.pool.Exec(ctx,
			`update transactions set merchant_canon=$2, updated_at=now() where id=$1 and merchant_canon <> $2`,
			c[0], c[2]); err != nil {
			return changes, err
		}
	}
	return changes, nil
}
