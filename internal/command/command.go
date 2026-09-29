package command

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"finbox/internal/category"
	"finbox/internal/daytok"
	"finbox/internal/embed"
	"finbox/internal/merchant"
	"finbox/internal/messages"
	"finbox/internal/money"
	"finbox/internal/monthtok"
	"finbox/internal/pipeline"
	"finbox/internal/store"
	"finbox/internal/validate"
)

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

func Month(ctx context.Context, st *store.Store, tok string, now time.Time, loc *time.Location) (int, time.Month, []store.CategoryTotal, error) {
	year, m, err := monthtok.Parse(tok, now.In(loc))
	if err != nil {
		return 0, 0, nil, err
	}
	totals, err := st.MonthTotals(ctx, year, m, loc)
	return year, m, totals, err
}

func Pending(ctx context.Context, st *store.Store) ([]store.Receipt, error) {
	return st.PendingReceipts(ctx)
}

type AddOpts struct{ Total, Merchant, Date, Currency, Category string }

// Add records a manual expense — no receipt behind it. Negative totals are
// refunds/credits; zero is rejected (so is any missing required field).
// A category given here is the human's own: source 'human'.
func Add(ctx context.Context, st *store.Store, o AddOpts, now time.Time, loc *time.Location) (store.TxnRow, error) {
	if strings.TrimSpace(o.Merchant) == "" {
		return store.TxnRow{}, fmt.Errorf("falta el comercio (--merchant)")
	}
	currency := "MXN"
	if o.Currency != "" {
		currency = strings.ToUpper(strings.TrimSpace(o.Currency))
		if !money.Known(currency) {
			return store.TxnRow{}, fmt.Errorf("moneda no soportada %q (soportadas: MXN, USD, EUR, JPY)", o.Currency)
		}
	}
	minor, err := money.ParseMinor(o.Total, currency)
	if err != nil {
		return store.TxnRow{}, err
	}
	if minor == 0 {
		return store.TxnRow{}, fmt.Errorf("el total no puede ser 0 (usa negativo para reembolsos)")
	}
	day, err := daytok.Parse(o.Date, now.In(loc))
	if err != nil {
		return store.TxnRow{}, err
	}
	var slug, source string
	if o.Category != "" {
		ok := false
		if slug, ok = category.Parse(o.Category); !ok {
			return store.TxnRow{}, fmt.Errorf(messages.UnknownCategory, o.Category, strings.Join(category.Slugs, ", "))
		}
		source = "human"
	}
	name := validate.Scrub(strings.TrimSpace(o.Merchant))
	return st.AddTransaction(ctx, store.NewTransaction{
		OccurredOn: day, Merchant: name, MerchantCanon: merchant.Canon(name),
		Category: slug, CategorySource: source,
		AmountMinor: minor, Currency: currency, Source: "manual",
	})
}

// EditOpts names the fields to change; Source is recorded on each edit_log
// row ("" = cli).
type EditOpts struct{ Total, Merchant, Date, Currency, Category, Source string }

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

func Edit(ctx context.Context, st *store.Store, idPrefix string, o EditOpts, now time.Time, loc *time.Location) (store.TxnRow, error) {
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
		if !money.Known(c) {
			return store.TxnRow{}, fmt.Errorf("moneda no soportada %q (soportadas: MXN, USD, EUR, JPY)", o.Currency)
		}
		if money.Exponent(c) != money.Exponent(cur.Currency) && o.Total == "" {
			// relabeling across exponents rescales the stored minor units
			// (30000 MXN = $300.00 would become ¥30,000) — force a re-entry
			return store.TxnRow{}, fmt.Errorf("cambiar de %s a %s reescala el monto: pasa también --total", cur.Currency, c)
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
		if minor == 0 {
			return store.TxnRow{}, fmt.Errorf("el total no puede ser 0 (usa negativo para reembolsos)")
		}
		set["amount_minor"] = minor
		edits = append(edits, store.FieldEdit{Field: "total",
			Old: strconv.FormatInt(cur.AmountMinor, 10), New: strconv.FormatInt(minor, 10)})
	}
	if o.Merchant != "" {
		m := validate.Scrub(strings.TrimSpace(o.Merchant))
		if m == "" {
			return store.TxnRow{}, fmt.Errorf("el comercio no puede quedar vacío")
		}
		// the rename lands on the canon; the raw receipt text is never edited
		set["merchant_canon"] = m
		edits = append(edits, store.FieldEdit{Field: "merchant", Old: cur.MerchantCanon, New: m})
	}
	if o.Date != "" {
		day, err := daytok.Parse(o.Date, now.In(loc))
		if err != nil {
			return store.TxnRow{}, err
		}
		set["occurred_on"] = day
		edits = append(edits, store.FieldEdit{Field: "date",
			Old: cur.OccurredOn.Format("2006-01-02"), New: day.Format("2006-01-02")})
	}
	if o.Category != "" {
		slug, ok := category.Parse(o.Category)
		if !ok {
			return store.TxnRow{}, fmt.Errorf(messages.UnknownCategory, o.Category, strings.Join(category.Slugs, ", "))
		}
		set["category"], set["category_source"] = slug, "human"
		edits = append(edits, store.FieldEdit{Field: "category", Old: cur.Category, New: slug})
	}
	if len(set) == 0 {
		return store.TxnRow{}, fmt.Errorf("nada que editar: pasa --total, --merchant, --date, --currency o --category")
	}
	// re-running the same edit (a labeling script run twice) is not a
	// correction: no edit_log row when nothing changed
	edits = slices.DeleteFunc(edits, func(e store.FieldEdit) bool { return e.Old == e.New })
	for i := range edits {
		edits[i].Source = o.Source
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

func Reprocess(ctx context.Context, d pipeline.Deps, idPrefix string) (pipeline.Result, error) {
	kind, id, err := d.Store.ResolveID(ctx, idPrefix)
	if err != nil {
		return pipeline.Result{}, err
	}
	if kind != "receipt" {
		return pipeline.Result{}, fmt.Errorf("reprocess opera sobre recibos, no gastos")
	}
	return pipeline.Reprocess(ctx, d, id)
}

var similarRe = regexp.MustCompile(`^parecido a ([0-9a-f]{8}|[0-9a-f-]{36})$`)

// Search returns the 5 closest expenses to text, nearest first. emb nil = the
// trigram baseline. "parecido a <id>" ranks by the stored vector (or doc) of
// that expense and leaves it out of the results.
func Search(ctx context.Context, st *store.Store, emb *embed.Client, text string) ([]store.Hit, error) {
	q := store.Fold(text)
	if m := similarRe.FindStringSubmatch(q); m != nil {
		txnID, err := resolveTxn(ctx, st, m[1])
		if err != nil {
			return nil, err
		}
		if emb != nil {
			return st.SearchSimilar(ctx, embed.Model, txnID)
		}
		docs, err := st.TxnDocs(ctx, txnID)
		if err != nil || len(docs) == 0 {
			return nil, err
		}
		return st.SearchTrigram(ctx, docs[0].Doc, txnID)
	}
	if emb == nil {
		return st.SearchTrigram(ctx, q, "")
	}
	vecs, err := emb.Embed(ctx, []string{q})
	if err != nil {
		return nil, err
	}
	return st.SearchEmbeddings(ctx, embed.Model, vecs[0])
}
