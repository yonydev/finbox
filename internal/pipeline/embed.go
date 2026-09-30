package pipeline

import (
	"context"
	"slices"

	"finbox/internal/embed"
	"finbox/internal/store"
)

// StaleDocs keeps the docs whose stored (model, doc_hash) no longer match, so
// the caller can print them before EmbedDocs spends anything on them.
func StaleDocs(docs []store.TxnDoc) []store.TxnDoc {
	return slices.DeleteFunc(docs, func(d store.TxnDoc) bool { return !d.Stale(embed.Model) })
}

// EmbedDocs embeds the stale expense docs and item docs in one call and stores
// them; returns how many vectors were written. A nil Embedder writes nothing.
func EmbedDocs(ctx context.Context, emb *embed.Client, st *store.Store, txns, items []store.TxnDoc) (int, error) {
	if emb == nil {
		return 0, nil
	}
	txns, items = StaleDocs(txns), StaleDocs(items)
	texts := make([]string, 0, len(txns)+len(items))
	for _, d := range txns {
		texts = append(texts, d.Doc)
	}
	for _, d := range items {
		texts = append(texts, d.Doc)
	}
	if len(texts) == 0 {
		return 0, nil
	}
	// ponytail: one call; the API takes 2048 inputs per request, chunk when the corpus passes that.
	vecs, err := emb.Embed(ctx, texts)
	if err != nil {
		return 0, err
	}
	for i, d := range txns {
		if err := st.UpsertEmbedding(ctx, d.ID, embed.Model, d.Hash, d.Doc, vecs[i]); err != nil {
			return i, err
		}
	}
	for i, d := range items {
		if err := st.UpsertItemEmbedding(ctx, d.ID, embed.Model, d.Hash, d.Doc, vecs[len(txns)+i]); err != nil {
			return len(txns) + i, err
		}
	}
	return len(texts), nil
}

// EmbedTxn indexes one expense and its receipt lines for /search. It skips
// whatever is already fresh, so the save/edit hooks can call it
// unconditionally: a total or date edit costs two queries and zero API calls,
// and a category edit re-embeds the expense alone. A nil Embedder is a no-op:
// /search then runs on trigrams.
func EmbedTxn(ctx context.Context, d Deps, txnID string) error {
	if d.Embedder == nil {
		return nil
	}
	txns, err := d.Store.TxnDocs(ctx, txnID)
	if err != nil {
		return err
	}
	items, err := d.Store.ItemDocs(ctx, txnID)
	if err != nil {
		return err
	}
	_, err = EmbedDocs(ctx, d.Embedder, d.Store, txns, items)
	return err
}
