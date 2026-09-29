package pipeline

import (
	"context"

	"finbox/internal/embed"
)

// EmbedTxn indexes one expense for /search. It skips when (model, doc_hash)
// already match, so the save/edit hooks can call it unconditionally and a
// total or date edit costs one query and zero API calls. A nil Embedder is a
// no-op: /search then runs on trigrams.
func EmbedTxn(ctx context.Context, d Deps, txnID string) error {
	if d.Embedder == nil {
		return nil
	}
	docs, err := d.Store.TxnDocs(ctx, txnID)
	if err != nil || len(docs) == 0 || !docs[0].Stale(embed.Model) {
		return err
	}
	vecs, err := d.Embedder.Embed(ctx, []string{docs[0].Doc})
	if err != nil {
		return err
	}
	return d.Store.UpsertEmbedding(ctx, docs[0].ID, embed.Model, docs[0].Hash, docs[0].Doc, vecs[0])
}
