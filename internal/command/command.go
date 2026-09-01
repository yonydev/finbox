package command

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"finbox/internal/money"
	"finbox/internal/monthtok"
	"finbox/internal/pipeline"
	"finbox/internal/store"
)

var iso4217 = regexp.MustCompile(`^[A-Z]{3}$`)

func List(ctx context.Context, st *store.Store, limit int, monthTok string, now time.Time, loc *time.Location) ([]store.TxnRow, error) {
	year, m := 0, time.January
	if monthTok != "" {
		var err error
		year, m, err = monthtok.Parse(monthTok, now.In(loc))
		if err != nil {
			return nil, err
		}
	}
	if limit <= 0 {
		limit = 10
	}
	return st.ListTransactions(ctx, limit, year, m, loc)
}

func Month(ctx context.Context, st *store.Store, tok string, now time.Time, loc *time.Location) (int, time.Month, []store.CurrencyTotal, int, error) {
	year, m, err := monthtok.Parse(tok, now.In(loc))
	if err != nil {
		return 0, 0, nil, 0, err
	}
	totals, count, err := st.MonthTotals(ctx, year, m, loc)
	return year, m, totals, count, err
}

func Pending(ctx context.Context, st *store.Store) ([]store.Receipt, error) {
	return st.PendingReceipts(ctx)
}

type EditOpts struct{ Total, Merchant, Date, Currency string }

// resolveTxn maps an id prefix of either kind to the active transaction id.
func resolveTxn(ctx context.Context, st *store.Store, idPrefix string) (string, error) {
	kind, id, err := st.ResolveID(ctx, idPrefix)
	if err != nil {
		return "", err
	}
	if kind == "transaction" {
		return id, nil
	}
	// receipt → its active (non-voided) transaction
	row, err := st.GetActiveTxnForReceipt(ctx, id)
	if err != nil {
		if err == store.ErrNotFound {
			return "", fmt.Errorf("%w: el recibo %s no tiene gasto activo", store.ErrNotFound, idPrefix)
		}
		return "", err
	}
	return row.ID, nil
}

func Edit(ctx context.Context, st *store.Store, idPrefix string, o EditOpts, loc *time.Location) (store.TxnRow, error) {
	txnID, err := resolveTxn(ctx, st, idPrefix)
	if err != nil {
		return store.TxnRow{}, err
	}
	// read current values for edit_log old_value
	cur, err := st.GetTransactionByID(ctx, txnID)
	if err != nil {
		return store.TxnRow{}, err
	}
	set := map[string]any{}
	var edits []store.FieldEdit
	currency := cur.Currency
	if o.Currency != "" {
		c := strings.ToUpper(strings.TrimSpace(o.Currency))
		if !iso4217.MatchString(c) { // the DB CHECK would reject "mxn" anyway — fail with a clear message instead
			return store.TxnRow{}, fmt.Errorf("moneda inválida %q (ISO 4217, ej. MXN)", o.Currency)
		}
		set["currency"] = c
		edits = append(edits, store.FieldEdit{Field: "currency", Old: cur.Currency, New: c})
		currency = c
	}
	if o.Total != "" {
		minor, err := money.ParseMinor(o.Total, currency)
		if err != nil {
			return store.TxnRow{}, err
		}
		if minor <= 0 {
			return store.TxnRow{}, fmt.Errorf("el total debe ser positivo (Phase 1)")
		}
		set["amount_minor"] = minor
		edits = append(edits, store.FieldEdit{Field: "total",
			Old: strconv.FormatInt(cur.AmountMinor, 10), New: strconv.FormatInt(minor, 10)})
	}
	if o.Merchant != "" {
		set["merchant"] = o.Merchant
		edits = append(edits, store.FieldEdit{Field: "merchant", Old: cur.Merchant, New: o.Merchant})
	}
	if o.Date != "" {
		day, err := time.ParseInLocation("2006-01-02", o.Date, loc)
		if err != nil {
			return store.TxnRow{}, fmt.Errorf("fecha inválida %q (usa YYYY-MM-DD)", o.Date)
		}
		set["occurred_on"] = day
		edits = append(edits, store.FieldEdit{Field: "date",
			Old: cur.OccurredOn.Format("2006-01-02"), New: o.Date})
	}
	if len(set) == 0 {
		return store.TxnRow{}, fmt.Errorf("nada que editar: pasa --total, --merchant, --date o --currency")
	}
	if err := st.EditTransaction(ctx, txnID, set, edits); err != nil {
		return store.TxnRow{}, err
	}
	return st.GetTransactionByID(ctx, txnID)
}

func Void(ctx context.Context, st *store.Store, idPrefix string) (string, error) {
	txnID, err := resolveTxn(ctx, st, idPrefix)
	if err != nil {
		return "", err
	}
	ok, err := st.VoidTransaction(ctx, txnID)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", store.ErrNotFound
	}
	return txnID, nil
}

func Reprocess(ctx context.Context, d pipeline.Deps, idPrefix string, now time.Time) (pipeline.Result, error) {
	kind, id, err := d.Store.ResolveID(ctx, idPrefix)
	if err != nil {
		return pipeline.Result{}, err
	}
	if kind != "receipt" {
		return pipeline.Result{}, fmt.Errorf("reprocess opera sobre recibos, no gastos")
	}
	return pipeline.Reprocess(ctx, d, id, now)
}
