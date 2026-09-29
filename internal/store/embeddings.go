package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
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
// receipt name when it differs, then item names. No amounts, dates or ids.
func BuildDoc(canon, cat, raw string, items []string) string {
	folded := Fold(canon)
	parts := []string{folded}
	if cat != "" {
		parts = append(parts, Fold(category.Label(cat)))
	}
	if r := Fold(raw); r != "" && r != folded {
		parts = append(parts, r)
	}
	doc := strings.Join(parts, docSep)
	n, seen := 0, map[string]bool{}
	for _, it := range items {
		if n == maxDocItems {
			break
		}
		f := Fold(it)
		if f == "" || seen[f] {
			continue
		}
		next := doc + docSep + f
		if len([]rune(next)) > maxDocRunes { // whole items only
			break
		}
		doc, seen[f], n = next, true, n+1
	}
	return doc
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

// TxnDoc is one expense's current doc next to the (model, hash) that is stored.
type TxnDoc struct{ ID, Doc, Hash, HaveModel, HaveHash string }

func (d TxnDoc) Stale(model string) bool { return d.HaveModel != model || d.HaveHash != d.Hash }

// TxnDocs builds the current doc of every active expense, or of onlyID alone
// when it is set.
func (s *Store) TxnDocs(ctx context.Context, onlyID string) ([]TxnDoc, error) {
	rows, err := s.pool.Query(ctx, `select t.id, coalesce(nullif(t.merchant_canon,''),t.merchant), coalesce(t.category,''), t.merchant,
		coalesce((select array_agg(i.name order by i.position) from transaction_items i where i.transaction_id=t.id),'{}'),
		coalesce(e.model,''), coalesce(e.doc_hash,'')
		from transactions t left join transaction_embeddings e on e.transaction_id=t.id
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
		if err := rows.Scan(&d.ID, &canon, &cat, &raw, &items, &d.HaveModel, &d.HaveHash); err != nil {
			return nil, err
		}
		d.Doc = BuildDoc(canon, cat, raw, items)
		d.Hash = DocHash(d.Doc)
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) UpsertEmbedding(ctx context.Context, txnID, model, hash, doc string, vec []float64) error {
	_, err := s.pool.Exec(ctx, `insert into transaction_embeddings (transaction_id, model, doc_hash, doc, embedding)
		values ($1,$2,$3,$4,$5::vector)
		on conflict (transaction_id) do update set model=excluded.model, doc_hash=excluded.doc_hash,
			doc=excluded.doc, embedding=excluded.embedding, created_at=now()`,
		txnID, model, hash, doc, vecLiteral(vec))
	return err
}

// Hit is a search result; Distance is lower-is-closer in both modes (cosine
// distance for vectors, 1 - word_similarity for trigrams).
type Hit struct {
	TxnRow
	Distance float64
}

// vecSearchSQL takes the query vector from a cte so both callers share the
// ranking, the filters and the limit.
const vecSearchSQL = `with q as (%s)
	select ` + txnRowCols + `, e.embedding <=> q.v as distance
	from q, transaction_embeddings e join transactions t on t.id = e.transaction_id
	where e.model = $1 and t.voided_at is null and (nullif($2,'')::uuid is null or t.id <> nullif($2,'')::uuid)
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
