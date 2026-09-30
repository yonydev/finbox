package command

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"finbox/internal/embed"
	"finbox/internal/store"
)

func seed(t *testing.T) (*store.Store, string, string) {
	t.Helper()
	s := store.NewTest(t)
	ctx := context.Background()
	r, err := s.CreateReceipt(ctx, store.CreateReceiptParams{BlobKey: "k", BlobSHA256: "sha-cmd", TgMessageID: 1, TgChatID: 1})
	if err != nil {
		t.Fatal(err)
	}
	s.Transition(ctx, r.ID, "pending", "awaiting_confirm", "")
	txnID, ok, err := s.ConfirmReceipt(ctx, r.ID, store.NewTransaction{
		OccurredOn: time.Date(2026, 8, 28, 0, 0, 0, 0, time.UTC),
		Merchant:   "Tacos", AmountMinor: 18500, Currency: "MXN", Source: "receipt",
	}, 0, nil)
	if err != nil || !ok {
		t.Fatal(err)
	}
	return s, r.ID, txnID
}

func TestEditByReceiptPrefixUpdatesTotal(t *testing.T) {
	s, rID, txnID := seed(t)
	ctx := context.Background()
	row, err := Edit(ctx, s, rID[:8], EditOpts{Total: "285.00"}, time.Now(), time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if row.ID != txnID || row.AmountMinor != 28500 {
		t.Fatalf("row = %+v", row)
	}
}

func TestEditShortDateAndSource(t *testing.T) {
	s, _, txnID := seed(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	row, err := Edit(ctx, s, txnID[:8], EditOpts{Date: "ayer", Source: "reply"}, now, time.UTC)
	if err != nil || row.OccurredOn.Format("2006-01-02") != "2026-09-17" {
		t.Fatalf("row = %+v err = %v", row, err)
	}
	if _, err := Edit(ctx, s, txnID[:8], EditOpts{Date: "32/09"}, now, time.UTC); err == nil {
		t.Fatal("impossible date must error")
	}
}

func TestEditAllowsNegativeRejectsZero(t *testing.T) {
	s, _, txnID := seed(t)
	row, err := Edit(context.Background(), s, txnID[:8], EditOpts{Total: "-5.00"}, time.Now(), time.UTC)
	if err != nil || row.AmountMinor != -500 {
		t.Fatalf("refund edit: %v %+v", err, row)
	}
	if _, err := Edit(context.Background(), s, txnID[:8], EditOpts{Total: "0"}, time.Now(), time.UTC); err == nil {
		t.Fatal("want error: zero total")
	}
}

func TestEditCurrencyExponentChangeRequiresTotal(t *testing.T) {
	s, _, txnID := seed(t) // seeded as 18500 MXN (exponent 2)
	if _, err := Edit(context.Background(), s, txnID[:8], EditOpts{Currency: "JPY"}, time.Now(), time.UTC); err == nil {
		t.Fatal("want error: exponent change without --total silently rescales the amount")
	}
	row, err := Edit(context.Background(), s, txnID[:8], EditOpts{Currency: "USD"}, time.Now(), time.UTC)
	if err != nil || row.Currency != "USD" || row.AmountMinor != 18500 {
		t.Fatalf("same-exponent relabel should pass: %v %+v", err, row)
	}
	row, err = Edit(context.Background(), s, txnID[:8], EditOpts{Currency: "JPY", Total: "185"}, time.Now(), time.UTC)
	if err != nil || row.Currency != "JPY" || row.AmountMinor != 185 {
		t.Fatalf("exponent change with total should pass: %v %+v", err, row)
	}
}

func TestEditRejectsUnknownCurrency(t *testing.T) {
	s, _, txnID := seed(t)
	if _, err := Edit(context.Background(), s, txnID[:8], EditOpts{Currency: "CLP", Total: "300"}, time.Now(), time.UTC); err == nil {
		t.Fatal("want error: unsupported currency would store a wrong-scale amount")
	}
}

func TestAddManualExpense(t *testing.T) {
	s := store.NewTest(t)
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	row, err := Add(context.Background(), s, AddOpts{Total: "285.00", Merchant: "Taller García", Date: "2026-09-01"}, now, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if row.Source != "manual" || row.ReceiptID != "" || row.AmountMinor != 28500 ||
		row.Currency != "MXN" || row.OccurredOn.Format("2006-01-02") != "2026-09-01" {
		t.Fatalf("row = %+v", row)
	}
	rows, err := List(context.Background(), s, 10, "", now, time.UTC)
	if err != nil || len(rows) != 1 || rows[0].ID != row.ID {
		t.Fatalf("added txn not listed: %v %+v", err, rows)
	}
}

func TestAddRefundNegativeTotal(t *testing.T) {
	s := store.NewTest(t)
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	row, err := Add(context.Background(), s, AddOpts{Total: "-120.00", Merchant: "Zapatería"}, now, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if row.AmountMinor != -12000 {
		t.Fatalf("row = %+v", row)
	}
}

func TestAddDefaultsDateToToday(t *testing.T) {
	s := store.NewTest(t)
	loc, _ := time.LoadLocation("America/Mexico_City")
	now := time.Date(2026, 9, 11, 2, 0, 0, 0, time.UTC) // still sep 10 in CDMX
	row, err := Add(context.Background(), s, AddOpts{Total: "50", Merchant: "Propina"}, now, loc)
	if err != nil {
		t.Fatal(err)
	}
	if got := row.OccurredOn.Format("2006-01-02"); got != "2026-09-10" {
		t.Fatalf("occurred_on = %s, want 2026-09-10 (local day)", got)
	}
}

func TestAddValidates(t *testing.T) {
	s := store.NewTest(t)
	now := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	cases := []AddOpts{
		{Total: "0", Merchant: "X"},                   // zero forbidden
		{Total: "50"},                                 // merchant missing
		{Total: "50", Merchant: "X", Currency: "CLP"}, // unsupported currency (same branch rejects "pesos")
		{Total: "50", Merchant: "X", Date: "32/09"},   // impossible date
		{Total: "abc", Merchant: "X"},                 // bad amount
		{Total: "300,50", Merchant: "X"},              // decimal comma must not reach the DB
	}
	for i, o := range cases {
		if _, err := Add(context.Background(), s, o, now, time.UTC); err == nil {
			t.Errorf("case %d (%+v): want error", i, o)
		}
	}
}

func TestVoidByPrefix(t *testing.T) {
	s, _, txnID := seed(t)
	got, err := Void(context.Background(), s, txnID[:8])
	if err != nil || got != txnID {
		t.Fatalf("void: %q %v", got, err)
	}
}

func TestListClampAndMonth(t *testing.T) {
	s, _, _ := seed(t)
	now := time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC)
	rows, err := List(context.Background(), s, 10, "aug", now, time.UTC)
	if err != nil || len(rows) != 1 {
		t.Fatalf("list: %d %v", len(rows), err)
	}
	_, _, totals, err := Month(context.Background(), s, "", now, time.UTC)
	if err != nil || len(totals) != 1 || totals[0].AmountMinor != 18500 || totals[0].Count != 1 {
		t.Fatalf("month: %+v %v", totals, err)
	}
}

func TestEditScrubsMerchant(t *testing.T) {
	s, _, txnID := seed(t)
	row, err := Edit(context.Background(), s, txnID[:8], EditOpts{Merchant: "Pago 4111 1111 1111 1111"}, time.Now(), time.UTC)
	if err != nil || row.MerchantCanon != "Pago [redactado]" || row.Merchant != "Tacos" {
		t.Fatalf("merchant = %q canon = %q err = %v", row.Merchant, row.MerchantCanon, err)
	}
	if _, err := Edit(context.Background(), s, txnID[:8], EditOpts{Merchant: "  "}, time.Now(), time.UTC); err == nil {
		t.Error("blank merchant must be rejected")
	}
}

func TestEditCategory(t *testing.T) {
	s, _, txnID := seed(t)
	row, err := Edit(context.Background(), s, txnID[:8], EditOpts{Category: "Súper"}, time.Now(), time.UTC)
	if err != nil || row.Category != "super" || row.CategorySource != "human" {
		t.Fatalf("row = %+v err = %v", row, err)
	}
	if log := s.EditLogForTest(t, txnID); len(log) != 1 || log[0] != "category >super cli" {
		t.Fatalf("edit_log = %v", log)
	}
	// the same edit again changes nothing and must not log a phantom correction
	if _, err := Edit(context.Background(), s, txnID[:8], EditOpts{Category: "super"}, time.Now(), time.UTC); err != nil {
		t.Fatal(err)
	}
	if log := s.EditLogForTest(t, txnID); len(log) != 1 {
		t.Fatalf("no-op edit logged: %v", log)
	}
	if _, err := Edit(context.Background(), s, txnID[:8], EditOpts{Category: "comida"}, time.Now(), time.UTC); err == nil {
		t.Error("unknown slug must be rejected")
	}
}

func TestAddWithCategoryAndCanon(t *testing.T) {
	s := store.NewTest(t)
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	row, err := Add(context.Background(), s,
		AddOpts{Total: "500", Merchant: "Limpieza Paty S.A. DE C.V.", Category: "Súper"}, now, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if row.Category != "super" || row.CategorySource != "human" {
		t.Fatalf("row = %+v", row)
	}
	if row.Merchant != "Limpieza Paty S.A. DE C.V." || row.MerchantCanon != "Limpieza Paty" {
		t.Fatalf("raw = %q canon = %q", row.Merchant, row.MerchantCanon)
	}
	if _, err := Add(context.Background(), s, AddOpts{Total: "500", Merchant: "X", Category: "comida"}, now, time.UTC); err == nil {
		t.Error("unknown category must be rejected")
	}
}

func TestSearchParecidoA(t *testing.T) {
	s, _, txnID := seed(t) // "Tacos"
	ctx := context.Background()
	other, err := s.AddTransaction(ctx, store.NewTransaction{
		OccurredOn: time.Date(2026, 8, 29, 0, 0, 0, 0, time.UTC),
		Merchant:   "Taqueria El Paisa", AmountMinor: 9000, Currency: "MXN", Source: "manual"})
	if err != nil {
		t.Fatal(err)
	}
	vec := make([]float64, 512)
	vec[0] = 1
	for id, doc := range map[string]string{txnID: "tacos · restaurantes", other.ID: "taqueria el paisa · restaurantes"} {
		if err := s.UpsertEmbedding(ctx, id, embed.Model, store.DocHash(doc), doc, vec); err != nil {
			t.Fatal(err)
		}
	}
	// nil embedder: the trigram baseline, so no network anywhere in this test
	hits, err := Search(ctx, s, nil, "trgm", "PARECIDO A "+txnID[:8])
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].ID != other.ID {
		t.Fatalf("hits = %+v, want only %s", hits, other.ShortID)
	}
	if _, err := Search(ctx, s, nil, "trgm", "parecido a 00000000"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unknown id: err = %v, want ErrNotFound", err)
	}
	// plain text is not an id lookup: both rows are candidates
	if hits, err = Search(ctx, s, nil, "trgm", "Tacos"); err != nil || len(hits) != 2 || hits[0].ID != txnID {
		t.Fatalf("plain query: %+v err %v", hits, err)
	}
	// "parecido a" probes with the stored vector, so hybrid spends no API call
	dead := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("parecido a must not call the embeddings API")
	}))
	defer dead.Close()
	hits, err = Search(ctx, s, &embed.Client{APIKey: "sk-test", BaseURL: dead.URL + "/"}, "hybrid", "parecido a "+txnID[:8])
	if err != nil || len(hits) != 1 || hits[0].ID != other.ID {
		t.Fatalf("hybrid parecido a: %+v err %v", hits, err)
	}
}

func vecOf(at map[int]float64) []float64 {
	v := make([]float64, 512)
	for i, x := range at {
		v[i] = x
	}
	return v
}

func hit(id string, d float64, trgm, vec int) store.Hit {
	h := store.Hit{Distance: d, TrgmRank: trgm, VecRank: vec}
	h.ID = id
	return h
}

func TestFuse(t *testing.T) {
	tests := []struct {
		name      string
		trgm, vec []store.Hit
		want      []string
		wantRanks [2]int // ranks of the first hit
	}{
		{name: "trgm only is kept whatever its distance", trgm: []store.Hit{hit("a", 0.64, 1, 0)}, want: []string{"a"}, wantRanks: [2]int{1, 0}},
		{name: "vec only under the cutoff is kept", vec: []store.Hit{hit("a", 0.5, 0, 1)}, want: []string{"a"}, wantRanks: [2]int{0, 1}},
		{name: "vec only over the cutoff is dropped", vec: []store.Hit{hit("a", 0.75, 0, 1)}, want: nil},
		{name: "both lists outranks either single list",
			trgm: []store.Hit{hit("a", 0.1, 1, 0), hit("b", 0.2, 2, 0)},
			vec:  []store.Hit{hit("c", 0.3, 0, 1), hit("b", 0.4, 0, 2)},
			want: []string{"b", "a", "c"}, wantRanks: [2]int{2, 2}},
		{name: "a tie keeps the trigram hit first",
			trgm: []store.Hit{hit("a", 0.1, 1, 0)}, vec: []store.Hit{hit("b", 0.1, 0, 1)},
			want: []string{"a", "b"}, wantRanks: [2]int{1, 0}},
		{name: "at most five out of ten",
			trgm: []store.Hit{hit("a", 0, 1, 0), hit("b", 0, 2, 0), hit("c", 0, 3, 0), hit("d", 0, 4, 0), hit("e", 0, 5, 0)},
			vec:  []store.Hit{hit("f", 0, 0, 1), hit("g", 0, 0, 2), hit("h", 0, 0, 3), hit("i", 0, 0, 4), hit("j", 0, 0, 5)},
			want: []string{"a", "f", "b", "g", "c"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := fuse(tc.trgm, tc.vec)
			var gotIDs []string
			for _, h := range got {
				gotIDs = append(gotIDs, h.ID)
			}
			if strings.Join(gotIDs, ",") != strings.Join(tc.want, ",") {
				t.Fatalf("fuse = %v, want %v", gotIDs, tc.want)
			}
			if len(got) > 0 && tc.wantRanks != [2]int{} {
				if [2]int{got[0].TrgmRank, got[0].VecRank} != tc.wantRanks {
					t.Fatalf("ranks of %s = t%d v%d, want t%d v%d", got[0].ID,
						got[0].TrgmRank, got[0].VecRank, tc.wantRanks[0], tc.wantRanks[1])
				}
			}
		})
	}
}

// TestSearchHybridFuses seeds four expenses whose trigram and vector rankings
// disagree, so the fused order can only come from RRF.
func TestSearchHybridFuses(t *testing.T) {
	s := store.NewTest(t)
	ctx := context.Background()
	qvec := vecOf(map[int]float64{1: 1})
	calls, fail := 0, false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if fail {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{{"index": 0, "embedding": qvec}}})
	}))
	defer srv.Close()
	emb := &embed.Client{APIKey: "sk-test", BaseURL: srv.URL + "/"}

	seedHit := func(merchant, doc string, day int, vec []float64) string {
		t.Helper()
		row, err := s.AddTransaction(ctx, store.NewTransaction{
			OccurredOn: time.Date(2026, 9, day, 0, 0, 0, 0, time.UTC),
			Merchant:   merchant, AmountMinor: 10000, Currency: "MXN", Source: "manual"})
		if err != nil {
			t.Fatal(err)
		}
		if err := s.UpsertEmbedding(ctx, row.ID, embed.Model, store.DocHash(doc), doc, vec); err != nil {
			t.Fatal(err)
		}
		return row.ShortID
	}
	// x: trigram-perfect, vector-far. y: vector-perfect, trigram-noise.
	// z: both. w: vector-only but past vecCutoff.
	x := seedHit("Walmart", "walmart", 5, vecOf(map[int]float64{2: 1}))
	z := seedHit("Walmart Oxxo", "walmart oxxo", 4, qvec)
	y := seedHit("Oxxo", "oxxo", 3, qvec)
	w := seedHit("Zzz", "zzz", 2, vecOf(map[int]float64{1: 0.1, 3: 0.99498743710662}))

	order := func(hits []store.Hit) string {
		var out []string
		for _, h := range hits {
			out = append(out, h.ShortID)
		}
		return strings.Join(out, ",")
	}
	hits, err := Search(ctx, s, emb, "hybrid", "walmart")
	if err != nil || calls != 1 {
		t.Fatalf("%d calls, err %v", calls, err)
	}
	if got := order(hits); got != z+","+x+","+y {
		t.Fatalf("hybrid order = %s, want %s,%s,%s (w %s must be out)", got, z, x, y, w)
	}
	if hits[0].TrgmRank != 2 || hits[0].VecRank != 1 || hits[0].Distance > 0.01 {
		t.Fatalf("z = t%d v%d d%.3f, want t2 v1 with the trigram distance", hits[0].TrgmRank, hits[0].VecRank, hits[0].Distance)
	}
	if hits[1].TrgmRank != 1 || hits[1].VecRank != 4 || hits[1].Distance > 0.01 {
		t.Fatalf("x = t%d v%d d%.3f, want t1 v4 with the trigram distance", hits[1].TrgmRank, hits[1].VecRank, hits[1].Distance)
	}
	if hits[2].TrgmRank != 0 || hits[2].VecRank != 2 {
		t.Fatalf("y = t%d v%d, want t0 v2", hits[2].TrgmRank, hits[2].VecRank)
	}

	// single modes are raw: no truncation, no cutoff, no fusion
	trgm, err := Search(ctx, s, emb, "trgm", "walmart")
	if err != nil || calls != 1 {
		t.Fatalf("trgm mode called the API: %d calls, err %v", calls, err)
	}
	if len(trgm) != 4 || trgm[3].Distance <= trgmCutoff || trgm[0].VecRank != 0 {
		t.Fatalf("trgm raw top-5 = %+v", trgm)
	}
	vec, err := Search(ctx, s, emb, "vec", "walmart")
	if err != nil || calls != 2 {
		t.Fatalf("%d calls, err %v", calls, err)
	}
	if !strings.Contains(order(vec), w) || vec[0].TrgmRank != 0 {
		t.Fatalf("vec raw top-5 = %s (w %s must be in)", order(vec), w)
	}

	// no embedder: hybrid is the truncated trigram list alone
	nilEmb, err := Search(ctx, s, nil, "hybrid", "walmart")
	if err != nil || order(nilEmb) != x+","+z {
		t.Fatalf("nil emb: %s, err %v", order(nilEmb), err)
	}
	if _, err := Search(ctx, s, nil, "vec", "walmart"); err == nil {
		t.Fatal("vec mode without an embedder must fail")
	}
	fail = true
	if _, err := Search(ctx, s, emb, "hybrid", "walmart"); err == nil {
		t.Fatal("a failing embed call must surface: the bot decides the fallback")
	}
}
