package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"

	"finbox/internal/category"
)

const (
	docSep      = " · "
	maxDocItems = 20
	maxDocRunes = 500
)

var docFold = strings.NewReplacer("á", "a", "é", "e", "í", "i", "ó", "o", "ú", "u", "ü", "u", "ñ", "n")

// Fold normalizes text for the search index: lowercase, accents stripped,
// whitespace collapsed. pg_trgm is case- but not accent-insensitive, so the
// doc and the query are folded here instead of installing unaccent.
func Fold(s string) string {
	return strings.Join(strings.Fields(docFold.Replace(strings.ToLower(s))), " ")
}

// BuildDoc is the text that gets embedded: merchant, category label, the raw
// receipt name when it differs, then item names, and a delivery phrase when the
// ticket has an app fee line. No amounts, dates or ids.
func BuildDoc(canon, cat, raw string, items []string) string {
	return buildDoc(Fold, canon, cat, raw, items)
}

// BuildPretty is BuildDoc without the accent stripping. Only the search line's
// model input uses it: "panal" is honeycomb, "pañal" is a diaper, and the model
// cannot tell which one a folded ticket meant.
func BuildPretty(canon, cat, raw string, items []string) string {
	return buildDoc(squash, canon, cat, raw, items)
}

// squash is Fold minus the replacer: lowercase and collapsed whitespace, accents kept.
func squash(s string) string { return strings.Join(strings.Fields(strings.ToLower(s)), " ") }

func buildDoc(norm func(string) string, canon, cat, raw string, items []string) string {
	folded := norm(canon)
	parts := []string{folded}
	if cat != "" {
		parts = append(parts, norm(category.Label(cat)))
	}
	if r := norm(raw); r != "" && r != folded {
		parts = append(parts, r)
	}
	doc := strings.Join(parts, docSep)
	n, seen := 0, map[string]bool{}
	for _, it := range items {
		if n == maxDocItems {
			break
		}
		f := norm(it)
		if f == "" || seen[f] {
			continue
		}
		next := doc + docSep + f
		if len([]rune(next)) > maxDocRunes { // whole items only
			break
		}
		doc, seen[f], n = next, true, n+1
	}
	// ponytail: every fee-line order in the corpus is Uber Eats (7/7, 2026-09-30); when Rappi/DiDi
	// receipts appear, drop the platform name here and let the search line name it.
	if slices.ContainsFunc(items, func(it string) bool { return strings.Contains(Fold(it), "delivery fee") }) {
		doc += docSep + "pedido a domicilio por app uber eats"
		switch cat {
		case "restaurantes":
			doc += docSep + "comida a domicilio"
		case "super":
			doc += docSep + "super a domicilio"
		}
	}
	return doc
}

// WithLine appends the model-written search line to a base doc. It is appended
// past maxDocRunes on purpose: the cap bounds the item list, not the line.
func WithLine(base, line string) string {
	if line == "" {
		return base
	}
	return base + docSep + line
}

func DocHash(doc string) string {
	sum := sha256.Sum256([]byte(doc))
	return hex.EncodeToString(sum[:])
}

// vecLiteral renders a vector as pgvector's text input format. fmt already
// prints a []float64 as "[1 0 0]", brackets included; only the separator differs.
func vecLiteral(v []float64) string {
	return strings.Join(strings.Fields(fmt.Sprint(v)), ",")
}

// TxnDoc is one expense's current doc next to the (model, hash) that is
// stored. It is also one receipt line; ID is then the item id.
//
// Base, Pretty, Line and HaveLineKey are expense docs only (item docs leave
// them zero: Base == "" is the contract LineStale reads): Base is the doc
// without the search line, Pretty the same text with its accents, which only
// the search-line call reads, Line and HaveLineKey what search_lines stores;
// Doc is Base with the stored Line appended.
type TxnDoc struct {
	ID, Doc, Hash, HaveModel, HaveHash string
	Base, Pretty, Line, HaveLineKey    string
}

func (d TxnDoc) Stale(model string) bool { return d.HaveModel != model || d.HaveHash != d.Hash }

// TxnDocs builds the current doc of every active expense, or of onlyID alone
// when it is set.
func (s *Store) TxnDocs(ctx context.Context, onlyID string) ([]TxnDoc, error) {
	rows, err := s.pool.Query(ctx, `select t.id, coalesce(nullif(t.merchant_canon,''),t.merchant), coalesce(t.category,''), t.merchant,
		coalesce((select array_agg(i.name order by i.position) from transaction_items i where i.transaction_id=t.id),'{}'),
		coalesce(e.model,''), coalesce(e.doc_hash,''), coalesce(l.doc_hash,''), coalesce(l.line,'')
		from transactions t left join transaction_embeddings e on e.transaction_id=t.id
		left join search_lines l on l.transaction_id=t.id
		where t.voided_at is null and (nullif($1,'')::uuid is null or t.id=nullif($1,'')::uuid)`, onlyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TxnDoc
	for rows.Next() {
		var d TxnDoc
		var canon, cat, raw string
		var items []string
		if err := rows.Scan(&d.ID, &canon, &cat, &raw, &items, &d.HaveModel, &d.HaveHash,
			&d.HaveLineKey, &d.Line); err != nil {
			return nil, err
		}
		d.Base, d.Pretty = BuildDoc(canon, cat, raw, items), BuildPretty(canon, cat, raw, items)
		d.Doc = WithLine(d.Base, d.Line)
		d.Hash = DocHash(d.Doc)
		out = append(out, d)
	}
	return out, rows.Err()
}

// UpsertSearchLine stores one expense's search line under the cache key that
// covers the prompt and the base doc.
func (s *Store) UpsertSearchLine(ctx context.Context, txnID, key, line, model string) error {
	_, err := s.pool.Exec(ctx, `insert into search_lines (transaction_id, doc_hash, line, model)
		values ($1,$2,$3,$4)
		on conflict (transaction_id) do update set doc_hash=excluded.doc_hash, line=excluded.line,
			model=excluded.model, created_at=now()`, txnID, key, line, model)
	return err
}

func (s *Store) UpsertEmbedding(ctx context.Context, txnID, model, hash, doc string, vec []float64) error {
	_, err := s.pool.Exec(ctx, `insert into transaction_embeddings (transaction_id, model, doc_hash, doc, embedding)
		values ($1,$2,$3,$4,$5::vector)
		on conflict (transaction_id) do update set model=excluded.model, doc_hash=excluded.doc_hash,
			doc=excluded.doc, embedding=excluded.embedding, created_at=now()`,
		txnID, model, hash, doc, vecLiteral(vec))
	return err
}

// ItemDocs builds the doc of every receipt line of every active expense, or of
// onlyTxnID's lines alone. The doc is the bare item name: a merchant prefix
// would pull every line of a ticket back toward the merchant, which is the
// dilution the per-item vector exists to remove.
func (s *Store) ItemDocs(ctx context.Context, onlyTxnID string) ([]TxnDoc, error) {
	rows, err := s.pool.Query(ctx, `select i.id, i.name, coalesce(e.model,''), coalesce(e.doc_hash,'')
		from transaction_items i join transactions t on t.id = i.transaction_id
		left join item_embeddings e on e.item_id = i.id
		where t.voided_at is null and (nullif($1,'')::uuid is null or t.id=nullif($1,'')::uuid)
		order by t.occurred_on, i.position`, onlyTxnID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TxnDoc
	for rows.Next() {
		var d TxnDoc
		var name string
		if err := rows.Scan(&d.ID, &name, &d.HaveModel, &d.HaveHash); err != nil {
			return nil, err
		}
		if d.Doc = Fold(name); d.Doc == "" {
			continue
		}
		d.Hash = DocHash(d.Doc)
		out = append(out, d)
	}
	return out, rows.Err()
}

// UpsertItemEmbedding stores one receipt line's vector. The select fills
// transaction_id, so the search query never joins transaction_items.
func (s *Store) UpsertItemEmbedding(ctx context.Context, itemID, model, hash, doc string, vec []float64) error {
	_, err := s.pool.Exec(ctx, `insert into item_embeddings (item_id, transaction_id, model, doc_hash, doc, embedding)
		select id, transaction_id, $2, $3, $4, $5::vector from transaction_items where id=$1
		on conflict (item_id) do update set model=excluded.model, doc_hash=excluded.doc_hash,
			doc=excluded.doc, embedding=excluded.embedding, created_at=now()`,
		itemID, model, hash, doc, vecLiteral(vec))
	return err
}

// Hit is a search result; Distance is lower-is-closer in every mode (cosine
// distance for vectors, 1 - word_similarity for trigrams), and in hybrid it is
// the distance of the list that found the hit, trigram preferred. TrgmRank and
// VecRank are 1-based positions in each list, 0 when the hit is not in it; a
// single mode fills only its own rank.
type Hit struct {
	TxnRow
	Distance          float64
	TrgmRank, VecRank int
}

// vecSearchSQL takes the query vector from a cte so both callers share the
// ranking, the filters and the limit. An expense is as close as its closest
// vector — its own or one of its receipt lines — so a long ticket stops
// diluting the item the query names; min() groups the union back to one row
// per expense.
const vecSearchSQL = `with q as (%s), d as (
		select e.transaction_id, min(e.embedding <=> q.v) as distance
		from q, (select transaction_id, embedding from transaction_embeddings where model = $1
		         union all
		         select transaction_id, embedding from item_embeddings where model = $1) e
		group by e.transaction_id)
	select ` + txnRowCols + `, d.distance
	from d join transactions t on t.id = d.transaction_id
	where t.voided_at is null and (nullif($2,'')::uuid is null or t.id <> nullif($2,'')::uuid)
	order by distance, t.occurred_on desc limit 5`

func (s *Store) SearchEmbeddings(ctx context.Context, model string, vec []float64) ([]Hit, error) {
	return s.scanHits(ctx, fmt.Sprintf(vecSearchSQL, `select $3::vector as v`), model, "", vecLiteral(vec))
}

// SearchSimilar ranks by the stored vector of txnID and leaves it out of the
// results. An unindexed target yields no rows.
func (s *Store) SearchSimilar(ctx context.Context, model, txnID string) ([]Hit, error) {
	return s.scanHits(ctx, fmt.Sprintf(vecSearchSQL,
		`select embedding as v from transaction_embeddings where transaction_id=nullif($2,'')::uuid and model=$1`), model, txnID)
}

// SearchTrigram is the no-API baseline: word_similarity scores the query
// against the best window of the doc, so a long item list is not penalized the
// way plain similarity would penalize it.
func (s *Store) SearchTrigram(ctx context.Context, query, excludeID string) ([]Hit, error) {
	return s.scanHits(ctx, `select `+txnRowCols+`, 1 - word_similarity($1, e.doc) as distance
		from transaction_embeddings e join transactions t on t.id = e.transaction_id
		where t.voided_at is null and (nullif($2,'')::uuid is null or t.id <> nullif($2,'')::uuid)
		order by distance, t.occurred_on desc limit 5`, query, excludeID)
}

func (s *Store) scanHits(ctx context.Context, sql string, args ...any) ([]Hit, error) {
	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Hit
	for rows.Next() {
		var h Hit
		if err := rows.Scan(&h.ID, &h.Merchant, &h.MerchantCanon, &h.Currency, &h.Source, &h.ReceiptID,
			&h.Category, &h.CategorySource, &h.OccurredOn, &h.AmountMinor, &h.Distance); err != nil {
			return nil, err
		}
		h.ShortID = h.ID[:8]
		out = append(out, h)
	}
	return out, rows.Err()
}
