package command

import (
	"cmp"
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

const (
	rrfK = 60
	// Hybrid-only cutoffs; single modes return the raw top-5 for the eval.
	// ponytail: 0.59 sits in the 0.576-0.600 window measured 2026-09-29 on the prod dump
	// after item vectors: every vec-only true hit is <= 0.575 (pan 0.575, luz->CFE 0.548),
	// nonsense queries start at 0.601 (zzzzzz). One-hundredth margin each side; recalibrate
	// from the eval's distance columns + three nonsense queries after any model or doc
	// change, or when "nada parecido" fires on a query that should hit.
	trgmCutoff = 0.65
	vecCutoff  = 0.59
)

// Search returns the 5 expenses closest to text, nearest first. mode is the
// CLI's literal: "trgm" (no API call), "vec" (the query is embedded), "hybrid"
// (what the bot ships: the trigram hits at or under trgmCutoff fused by RRF
// with the vector top-5, then vector-only hits farther than vecCutoff dropped).
// A nil emb skips the vector list, so hybrid degrades to the truncated trigram
// list. "parecido a <id>" probes with the stored vector and doc of that expense
// and leaves it out of the results; it makes no API call.
func Search(ctx context.Context, st *store.Store, emb *embed.Client, mode, text string) ([]store.Hit, error) {
	if mode == "vec" && emb == nil {
		return nil, fmt.Errorf("modo vec sin cliente de embeddings")
	}
	q, txnID := store.Fold(text), ""
	if m := similarRe.FindStringSubmatch(q); m != nil {
		id, err := resolveTxn(ctx, st, m[1])
		if err != nil {
			return nil, err
		}
		docs, err := st.TxnDocs(ctx, id)
		if err != nil || len(docs) == 0 {
			return nil, err
		}
		q, txnID = docs[0].Doc, id
	}
	var trgm, vec []store.Hit
	var err error
	if mode != "vec" {
		if trgm, err = st.SearchTrigram(ctx, q, txnID); err != nil {
			return nil, err
		}
		if mode == "hybrid" { // noise never earns a rank: truncate before fusing
			for len(trgm) > 0 && trgm[len(trgm)-1].Distance > trgmCutoff {
				trgm = trgm[:len(trgm)-1]
			}
		}
		for i := range trgm {
			trgm[i].TrgmRank = i + 1
		}
	}
	if mode != "trgm" && emb != nil {
		if txnID != "" {
			vec, err = st.SearchSimilar(ctx, embed.Model, txnID)
		} else {
			var vecs [][]float64
			if vecs, err = emb.Embed(ctx, []string{q}); err == nil {
				vec, err = st.SearchEmbeddings(ctx, embed.Model, vecs[0])
			}
		}
		if err != nil {
			return nil, err
		}
		for i := range vec {
			vec[i].VecRank = i + 1
		}
	}
	switch mode {
	case "trgm":
		return trgm, nil
	case "vec":
		return vec, nil
	}
	return fuse(trgm, vec), nil
}

// fuse merges the two ranked lists with Reciprocal Rank Fusion, so the two
// incomparable distance scales never meet: a hit in both lists always outranks
// a single-list hit, and a tie keeps the trigram hit first because a literal
// match is the one the user can see in the row. Vector-only hits farther than
// vecCutoff are dropped, which is what "nada parecido" reads from.
func fuse(trgm, vec []store.Hit) []store.Hit {
	score, at := map[string]float64{}, map[string]int{}
	var out []store.Hit
	for _, list := range [][]store.Hit{trgm, vec} {
		for _, h := range list {
			rank := h.TrgmRank
			if rank == 0 {
				rank = h.VecRank
			}
			score[h.ID] += 1 / float64(rrfK+rank)
			if i, ok := at[h.ID]; ok { // already in from the trigram list: keep its distance
				out[i].VecRank = h.VecRank
				continue
			}
			at[h.ID] = len(out)
			out = append(out, h)
		}
	}
	kept := out[:0]
	for _, h := range out {
		if h.TrgmRank > 0 || h.Distance <= vecCutoff {
			kept = append(kept, h)
		}
	}
	slices.SortStableFunc(kept, func(a, b store.Hit) int { return cmp.Compare(score[b.ID], score[a.ID]) })
	if len(kept) > 5 {
		kept = kept[:5]
	}
	return kept
}
