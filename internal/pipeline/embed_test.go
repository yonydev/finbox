package pipeline

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"finbox/internal/embed"
	"finbox/internal/store"
)

func TestEmbedTxnSkipsWhenFresh(t *testing.T) {
	st := store.NewTest(t)
	ctx := context.Background()
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":[{"index":0,"embedding":[` + strings.Repeat("0,", embed.Dims-1) + `1]}]}`))
	}))
	defer srv.Close()
	row, err := st.AddTransaction(ctx, store.NewTransaction{
		OccurredOn: time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC),
		Merchant:   "La Comer", AmountMinor: 10000, Currency: "MXN", Source: "manual"})
	if err != nil {
		t.Fatal(err)
	}
	d := Deps{Store: st, Loc: time.UTC, Log: slog.Default(),
		Embedder: &embed.Client{APIKey: "sk-test", BaseURL: srv.URL + "/"}}

	if err := EmbedTxn(ctx, d, row.ID); err != nil || calls != 1 {
		t.Fatalf("first: %d calls, err %v", calls, err)
	}
	if err := EmbedTxn(ctx, d, row.ID); err != nil || calls != 1 {
		t.Fatalf("second (same doc): %d calls, err %v", calls, err)
	}
	if err := st.EditTransaction(ctx, row.ID, map[string]any{"amount_minor": int64(999)}, nil); err != nil {
		t.Fatal(err)
	}
	if err := EmbedTxn(ctx, d, row.ID); err != nil || calls != 1 {
		t.Fatalf("after an amount edit: %d calls, err %v", calls, err)
	}
	if err := st.EditTransaction(ctx, row.ID,
		map[string]any{"category": "super", "category_source": "human"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := EmbedTxn(ctx, d, row.ID); err != nil || calls != 2 {
		t.Fatalf("after a category edit: %d calls, err %v", calls, err)
	}
	vec := make([]float64, embed.Dims)
	vec[embed.Dims-1] = 1
	hits, err := st.SearchEmbeddings(ctx, embed.Model, vec)
	if err != nil || len(hits) != 1 {
		t.Fatalf("%d hits, err %v", len(hits), err)
	}

	d.Embedder = nil // feature off: no error, nothing indexed
	unindexed, err := st.AddTransaction(ctx, store.NewTransaction{
		OccurredOn: time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC),
		Merchant:   "Oxxo", AmountMinor: 100, Currency: "MXN", Source: "manual"})
	if err != nil {
		t.Fatal(err)
	}
	if err := EmbedTxn(ctx, d, unindexed.ID); err != nil || calls != 2 {
		t.Fatalf("nil Embedder: %d calls, err %v", calls, err)
	}
	// a voided (or unknown) id has no doc: no call, no error
	if _, err := st.VoidTransaction(ctx, row.ID); err != nil {
		t.Fatal(err)
	}
	d.Embedder = &embed.Client{APIKey: "sk-test", BaseURL: srv.URL + "/"}
	if err := EmbedTxn(ctx, d, row.ID); err != nil || calls != 2 {
		t.Fatalf("voided row: %d calls, err %v", calls, err)
	}
}

// TestEmbedTxnIndexesItems: one save is one call, for the expense and every
// receipt line together.
func TestEmbedTxnIndexesItems(t *testing.T) {
	st := store.NewTest(t)
	ctx := context.Background()
	calls, inputs := 0, 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body struct {
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		inputs = len(body.Input)
		data := make([]map[string]any, len(body.Input))
		for i := range data {
			vec := make([]float64, embed.Dims)
			vec[i%embed.Dims] = 1
			data[i] = map[string]any{"index": i, "embedding": vec}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	defer srv.Close()
	r, err := st.CreateReceipt(ctx, store.CreateReceiptParams{BlobKey: "k", BlobSHA256: "sha-items", TgMessageID: 9, TgChatID: 1})
	if err != nil {
		t.Fatal(err)
	}
	st.Transition(ctx, r.ID, "pending", "awaiting_confirm", "")
	txnID, ok, err := st.ConfirmReceipt(ctx, r.ID, store.NewTransaction{
		OccurredOn: time.Date(2026, 9, 4, 0, 0, 0, 0, time.UTC),
		Merchant:   "La Comer", AmountMinor: 10000, Currency: "MXN", Source: "receipt",
		Items: []store.NewItem{{Position: 0, Name: "Agua"}, {Position: 1, Name: "Pan bolillo"}}}, 0, nil)
	if err != nil || !ok {
		t.Fatal(err)
	}
	d := Deps{Store: st, Loc: time.UTC, Log: slog.Default(),
		Embedder: &embed.Client{APIKey: "sk-test", BaseURL: srv.URL + "/"}}

	if err := EmbedTxn(ctx, d, txnID); err != nil || calls != 1 || inputs != 3 {
		t.Fatalf("first: %d calls with %d inputs, err %v", calls, inputs, err)
	}
	items, err := st.ItemDocs(ctx, txnID)
	if err != nil || len(items) != 2 || items[0].Stale(embed.Model) || items[1].Stale(embed.Model) {
		t.Fatalf("items not indexed: %+v err %v", items, err)
	}
	if err := EmbedTxn(ctx, d, txnID); err != nil || calls != 1 {
		t.Fatalf("second (nothing stale): %d calls, err %v", calls, err)
	}
	// items are immutable, so a category edit re-embeds the expense alone
	if err := st.EditTransaction(ctx, txnID,
		map[string]any{"category": "super", "category_source": "human"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := EmbedTxn(ctx, d, txnID); err != nil || calls != 2 || inputs != 1 {
		t.Fatalf("category edit: %d calls with %d inputs, err %v", calls, inputs, err)
	}
	d.Embedder = nil
	if n, err := EmbedDocs(ctx, nil, st, nil, items); n != 0 || err != nil {
		t.Fatalf("nil embedder: %d vectors, err %v", n, err)
	}
}
