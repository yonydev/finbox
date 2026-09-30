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

// LineKey is the search-line cache key: the prompt is part of it, so editing
// the prompt regenerates every line on the next reembed (≈100 calls, cents).
// It hashes the folded Base, not the accented Pretty the model reads: the two
// always change together, and Base is what the key has to track.
func LineKey(base string) string { return store.DocHash(embed.LinePrompt + "\n" + base) }

// LineStale reports whether d needs a new search line: none yet, or the
// prompt or the expense text changed. Item docs (Base == "") never do.
func LineStale(d store.TxnDoc) bool { return d.Base != "" && LineKey(d.Base) != d.HaveLineKey }

// WriteLines asks the model for one search line per stale expense doc, stores
// each as it arrives and appends it to the doc in place; returns how many were
// written. A nil client or an empty ChatModel writes nothing. The first error
// stops the loop (an outage must not cost 100 timeouts) and is returned after
// EmbedDocs has embedded the rest with their stored lines.
func WriteLines(ctx context.Context, emb *embed.Client, st *store.Store, txns []store.TxnDoc) (int, error) {
	if emb == nil || emb.ChatModel == "" {
		return 0, nil
	}
	n := 0
	for i := range txns {
		d := &txns[i]
		if !LineStale(*d) {
			continue
		}
		line, err := emb.SearchLine(ctx, d.Pretty)
		if err != nil {
			return n, err
		}
		line = store.Fold(line)
		key := LineKey(d.Base)
		if err := st.UpsertSearchLine(ctx, d.ID, key, line, emb.ChatModel); err != nil {
			return n, err
		}
		d.Line, d.HaveLineKey = line, key
		d.Doc = store.WithLine(d.Base, line)
		d.Hash = store.DocHash(d.Doc)
		n++
	}
	return n, nil
}

// EmbedDocs embeds the stale expense docs and item docs in one call and stores
// them; returns how many vectors were written. A nil Embedder writes nothing.
// A line failure is returned but never blocks the vectors.
func EmbedDocs(ctx context.Context, emb *embed.Client, st *store.Store, txns, items []store.TxnDoc) (int, error) {
	if emb == nil {
		return 0, nil
	}
	_, lineErr := WriteLines(ctx, emb, st, txns)
	txns, items = StaleDocs(txns), StaleDocs(items)
	texts := make([]string, 0, len(txns)+len(items))
	for _, d := range txns {
		texts = append(texts, d.Doc)
	}
	for _, d := range items {
		texts = append(texts, d.Doc)
	}
	if len(texts) == 0 {
		return 0, lineErr
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
	return len(texts), lineErr
}

// EmbedTxn indexes one expense and its receipt lines for /search. It skips
// whatever is already fresh, so the save/edit hooks can call it
// unconditionally: a total or date edit costs two queries and zero API calls,
// and a category edit re-embeds the expense alone. A nil Embedder is a no-op:
// /search then runs on trigrams. Returns how many vectors were written, so the
// caller can tell a search-line failure from a failure to index at all.
func EmbedTxn(ctx context.Context, d Deps, txnID string) (int, error) {
	if d.Embedder == nil {
		return 0, nil
	}
	txns, err := d.Store.TxnDocs(ctx, txnID)
	if err != nil {
		return 0, err
	}
	items, err := d.Store.ItemDocs(ctx, txnID)
	if err != nil {
		return 0, err
	}
	return EmbedDocs(ctx, d.Embedder, d.Store, txns, items)
}
