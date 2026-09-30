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

	if _, err := EmbedTxn(ctx, d, row.ID); err != nil || calls != 1 {
		t.Fatalf("first: %d calls, err %v", calls, err)
	}
	if _, err := EmbedTxn(ctx, d, row.ID); err != nil || calls != 1 {
		t.Fatalf("second (same doc): %d calls, err %v", calls, err)
	}
	if err := st.EditTransaction(ctx, row.ID, map[string]any{"amount_minor": int64(999)}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := EmbedTxn(ctx, d, row.ID); err != nil || calls != 1 {
		t.Fatalf("after an amount edit: %d calls, err %v", calls, err)
	}
	if err := st.EditTransaction(ctx, row.ID,
		map[string]any{"category": "super", "category_source": "human"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := EmbedTxn(ctx, d, row.ID); err != nil || calls != 2 {
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
	if _, err := EmbedTxn(ctx, d, unindexed.ID); err != nil || calls != 2 {
		t.Fatalf("nil Embedder: %d calls, err %v", calls, err)
	}
	// a voided (or unknown) id has no doc: no call, no error
	if _, err := st.VoidTransaction(ctx, row.ID); err != nil {
		t.Fatal(err)
	}
	d.Embedder = &embed.Client{APIKey: "sk-test", BaseURL: srv.URL + "/"}
	if _, err := EmbedTxn(ctx, d, row.ID); err != nil || calls != 2 {
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

	if _, err := EmbedTxn(ctx, d, txnID); err != nil || calls != 1 || inputs != 3 {
		t.Fatalf("first: %d calls with %d inputs, err %v", calls, inputs, err)
	}
	items, err := st.ItemDocs(ctx, txnID)
	if err != nil || len(items) != 2 || items[0].Stale(embed.Model) || items[1].Stale(embed.Model) {
		t.Fatalf("items not indexed: %+v err %v", items, err)
	}
	if _, err := EmbedTxn(ctx, d, txnID); err != nil || calls != 1 {
		t.Fatalf("second (nothing stale): %d calls, err %v", calls, err)
	}
	// items are immutable, so a category edit re-embeds the expense alone
	if err := st.EditTransaction(ctx, txnID,
		map[string]any{"category": "super", "category_source": "human"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := EmbedTxn(ctx, d, txnID); err != nil || calls != 2 || inputs != 1 {
		t.Fatalf("category edit: %d calls with %d inputs, err %v", calls, inputs, err)
	}
	d.Embedder = nil
	if n, err := EmbedDocs(ctx, nil, st, nil, items); n != 0 || err != nil {
		t.Fatalf("nil embedder: %d vectors, err %v", n, err)
	}
}

// seedReceipt confirms a receipt with the given item names and returns the txn id.
func seedReceipt(t *testing.T, st *store.Store, sha, merchant string, day int, names ...string) string {
	t.Helper()
	ctx := context.Background()
	r, err := st.CreateReceipt(ctx, store.CreateReceiptParams{BlobKey: "k" + sha, BlobSHA256: sha, TgMessageID: int64(day), TgChatID: 1})
	if err != nil {
		t.Fatal(err)
	}
	st.Transition(ctx, r.ID, "pending", "awaiting_confirm", "")
	var items []store.NewItem
	for i, n := range names {
		items = append(items, store.NewItem{Position: i, Name: n})
	}
	txnID, ok, err := st.ConfirmReceipt(ctx, r.ID, store.NewTransaction{
		OccurredOn: time.Date(2026, 9, day, 0, 0, 0, 0, time.UTC),
		Merchant:   merchant, AmountMinor: 10000, Currency: "MXN", Source: "receipt", Items: items}, 0, nil)
	if err != nil || !ok {
		t.Fatalf("confirm: %v", err)
	}
	return txnID
}

// fakeOA answers both endpoints the pipeline calls and counts each.
type fakeOA struct {
	t                 *testing.T
	chat, emb, inputs int
	lastInput         string
	line              string
	chatStatus        int
}

func (f *fakeOA) serve() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/chat/completions":
			f.chat++
			if f.chatStatus != 0 {
				w.WriteHeader(f.chatStatus)
				return
			}
			msg, _ := json.Marshal(map[string]string{"search_line": f.line})
			json.NewEncoder(w).Encode(map[string]any{"choices": []any{
				map[string]any{"message": map[string]any{"content": string(msg)}}}})
		case "/embeddings":
			f.emb++
			var in struct {
				Input []string `json:"input"`
			}
			if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
				f.t.Error(err)
			}
			f.inputs, f.lastInput = len(in.Input), in.Input[0]
			data := make([]map[string]any, len(in.Input))
			for i := range data {
				vec := make([]float64, embed.Dims)
				vec[i%embed.Dims] = 1
				data[i] = map[string]any{"index": i, "embedding": vec}
			}
			json.NewEncoder(w).Encode(map[string]any{"data": data})
		default:
			f.t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
}

func TestLineKeyDeterministic(t *testing.T) {
	if LineKey("a") != LineKey("a") || LineKey("a") == LineKey("b") {
		t.Error("LineKey is not a function of the base doc alone")
	}
	if LineKey("a") != store.DocHash(embed.LinePrompt+"\na") {
		t.Error("the prompt must be part of the key")
	}
	if LineStale(store.TxnDoc{}) {
		t.Error(`an item doc (Base == "") never needs a line`)
	}
	if !LineStale(store.TxnDoc{Base: "a"}) {
		t.Error("a doc with no stored key needs a line")
	}
	if LineStale(store.TxnDoc{Base: "a", HaveLineKey: LineKey("a")}) {
		t.Error("a cached line must not be redone")
	}
}

func TestEmbedDocsWritesLinesOnce(t *testing.T) {
	st := store.NewTest(t)
	ctx := context.Background()
	f := &fakeOA{t: t, line: "Recibo de LUZ"}
	srv := f.serve()
	defer srv.Close()
	d := Deps{Store: st, Loc: time.UTC, Log: slog.Default(),
		Embedder: &embed.Client{APIKey: "sk-test", BaseURL: srv.URL + "/", ChatModel: "gpt-test"}}
	row, err := st.AddTransaction(ctx, store.NewTransaction{
		OccurredOn: time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC),
		Merchant:   "CFE", AmountMinor: 10000, Currency: "MXN", Source: "manual"})
	if err != nil {
		t.Fatal(err)
	}
	if n, err := EmbedTxn(ctx, d, row.ID); err != nil || n != 1 || f.chat != 1 || f.emb != 1 {
		t.Fatalf("first: %d vectors, %d chat, %d embed, err %v", n, f.chat, f.emb, err)
	}
	if f.lastInput != "cfe · recibo de luz" { // the line is folded before it is stored
		t.Fatalf("embedded doc = %q", f.lastInput)
	}
	docs, err := st.TxnDocs(ctx, row.ID)
	if err != nil || len(docs) != 1 {
		t.Fatal(err)
	}
	if docs[0].HaveLineKey != LineKey(docs[0].Base) || docs[0].Line != "recibo de luz" {
		t.Fatalf("stored line: %+v", docs[0])
	}
	if _, err := EmbedTxn(ctx, d, row.ID); err != nil || f.chat != 1 || f.emb != 1 {
		t.Fatalf("second (nothing stale): %d chat, %d embed, err %v", f.chat, f.emb, err)
	}
	if err := st.EditTransaction(ctx, row.ID, map[string]any{"amount_minor": int64(999)}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := EmbedTxn(ctx, d, row.ID); err != nil || f.chat != 1 || f.emb != 1 {
		t.Fatalf("amount edit: %d chat, %d embed, err %v", f.chat, f.emb, err)
	}
	// the category is part of the base doc, so the line is asked again
	if err := st.EditTransaction(ctx, row.ID,
		map[string]any{"category": "servicios", "category_source": "human"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := EmbedTxn(ctx, d, row.ID); err != nil || f.chat != 2 || f.emb != 2 {
		t.Fatalf("category edit: %d chat, %d embed, err %v", f.chat, f.emb, err)
	}
	// one line for the expense, none for the receipt lines: 1 chat, 3 embed inputs
	items := seedReceipt(t, st, "sha-lines", "La Comer", 6, "Agua", "Pan bolillo")
	if n, err := EmbedTxn(ctx, d, items); err != nil || n != 3 || f.chat != 3 || f.inputs != 3 {
		t.Fatalf("receipt: %d vectors, %d chat, %d inputs, err %v", n, f.chat, f.inputs, err)
	}
	// an empty answer is cached like any other: nothing to add, never asked again
	f.line = ""
	empty, err := st.AddTransaction(ctx, store.NewTransaction{
		OccurredOn: time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC),
		Merchant:   "Oxxo", AmountMinor: 100, Currency: "MXN", Source: "manual"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := EmbedTxn(ctx, d, empty.ID); err != nil || f.chat != 4 {
		t.Fatalf("empty line: %d chat, err %v", f.chat, err)
	}
	if _, err := EmbedTxn(ctx, d, empty.ID); err != nil || f.chat != 4 || f.emb != 4 {
		t.Fatalf("empty line cached: %d chat, %d embed, err %v", f.chat, f.emb, err)
	}
}

func TestEmbedDocsDegradesWithoutLine(t *testing.T) {
	st := store.NewTest(t)
	ctx := context.Background()
	f := &fakeOA{t: t, line: "luz", chatStatus: 500}
	srv := f.serve()
	defer srv.Close()
	emb := &embed.Client{APIKey: "sk-test", BaseURL: srv.URL + "/", ChatModel: "gpt-test"}
	for _, m := range []string{"CFE", "Smart Fit"} {
		if _, err := st.AddTransaction(ctx, store.NewTransaction{
			OccurredOn: time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC),
			Merchant:   m, AmountMinor: 100, Currency: "MXN", Source: "manual"}); err != nil {
			t.Fatal(err)
		}
	}
	docs := func() []store.TxnDoc {
		t.Helper()
		out, err := st.TxnDocs(ctx, "")
		if err != nil || len(out) != 2 {
			t.Fatalf("%d docs, err %v", len(out), err)
		}
		return out
	}
	// the chat is down: one call, not two, and the vectors land with base-only docs
	n, err := EmbedDocs(ctx, emb, st, docs(), nil)
	if n != 2 || err == nil || f.chat != 1 || f.emb != 1 {
		t.Fatalf("chat down: %d vectors, %d chat, %d embed, err %v", n, f.chat, f.emb, err)
	}
	for _, d := range docs() {
		if d.Doc != d.Base || d.Stale(embed.Model) {
			t.Fatalf("expected an indexed base-only doc: %+v", d)
		}
	}
	f.chatStatus = 0
	n, err = EmbedDocs(ctx, emb, st, docs(), nil)
	if n != 2 || err != nil || f.chat != 3 || f.emb != 2 {
		t.Fatalf("healed: %d vectors, %d chat, %d embed, err %v", n, f.chat, f.emb, err)
	}
	for _, d := range docs() {
		if d.Doc != d.Base+" · luz" || d.Stale(embed.Model) {
			t.Fatalf("expected a lined, indexed doc: %+v", d)
		}
	}
	// lines off: no chat call, the doc stays the base
	off := &embed.Client{APIKey: "sk-test", BaseURL: srv.URL + "/"}
	third, err := st.AddTransaction(ctx, store.NewTransaction{
		OccurredOn: time.Date(2026, 9, 9, 0, 0, 0, 0, time.UTC),
		Merchant:   "Oxxo", AmountMinor: 100, Currency: "MXN", Source: "manual"})
	if err != nil {
		t.Fatal(err)
	}
	only, err := st.TxnDocs(ctx, third.ID)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := EmbedDocs(ctx, off, st, only, nil); n != 1 || err != nil || f.chat != 3 {
		t.Fatalf("lines off: %d vectors, %d chat, err %v", n, f.chat, err)
	}
	if only[0].Doc != only[0].Base {
		t.Fatalf("lines off must leave the base doc: %+v", only[0])
	}
}
