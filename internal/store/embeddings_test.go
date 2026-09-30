package store

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestBuildDoc(t *testing.T) {
	long := make([]string, 30)
	for i := range long {
		long[i] = strings.Repeat("x", 10) + string(rune('a'+i))
	}
	tests := []struct {
		name                    string
		canon, cat, raw         string
		items                   []string
		want                    string
		wantItems, wantMaxRunes int
	}{
		{name: "accents and case fold", canon: "FARMACIAS BENAVIDES", cat: "salud",
			raw: "Farmacias  Benavides S.A.B. de C.V.", items: []string{"GODONITES PAÑAL MED 11 UN"},
			want: "farmacias benavides · salud · farmacias benavides s.a.b. de c.v. · godonites panal med 11 un"},
		{name: "canon equal to raw drops raw", canon: "Oxxo", cat: "super", raw: "OXXO",
			want: "oxxo · super"},
		{name: "empty category", canon: "Oxxo", raw: "Oxxo", want: "oxxo"},
		{name: "label is the es-MX name", canon: "La Comer", cat: "super", raw: "La Comer",
			items: []string{"Pan bolillo"}, want: "la comer · super · pan bolillo"},
		{name: "items dedup after fold", canon: "Uber", raw: "Uber",
			items: []string{"Delivery Fee", "delivery  fee", "Service Fee"},
			want:  "uber · delivery fee · service fee"},
		{name: "20 item cap", canon: "x", raw: "x", items: long[:25], wantItems: 20},
		{name: "500 rune cap", canon: "x", raw: "x", items: long, wantMaxRunes: 500},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := BuildDoc(tc.canon, tc.cat, tc.raw, tc.items)
			if tc.want != "" && got != tc.want {
				t.Fatalf("BuildDoc = %q, want %q", got, tc.want)
			}
			if tc.wantItems != 0 {
				if n := strings.Count(got, docSep); n != tc.wantItems {
					t.Fatalf("%d items in %q, want %d", n, got, tc.wantItems)
				}
			}
			if tc.wantMaxRunes != 0 && len([]rune(got)) > tc.wantMaxRunes {
				t.Fatalf("doc is %d runes (max %d): %q", len([]rune(got)), tc.wantMaxRunes, got)
			}
		})
	}
	// "súper" folds to "super" in the label, so the doc is pure ASCII
	if d := BuildDoc("La Comer", "super", "La Comer", nil); DocHash(d) != DocHash("la comer · super") {
		t.Errorf("DocHash is not stable over the same doc")
	}
	if DocHash("a") == DocHash("b") || len(DocHash("a")) != 64 {
		t.Errorf("DocHash = %q", DocHash("a"))
	}
}

// unit returns the i-th basis vector of a 512-dim space: cosine distance 0 to
// itself, 1 to any other basis vector.
func unit(i int) []float64 {
	v := make([]float64, 512)
	v[i] = 1
	return v
}

func seedTxn(t *testing.T, s *Store, merchant string, day int) TxnRow {
	t.Helper()
	row, err := s.AddTransaction(context.Background(), NewTransaction{
		OccurredOn: time.Date(2026, 9, day, 0, 0, 0, 0, time.UTC),
		Merchant:   merchant, AmountMinor: 10000, Currency: "MXN", Source: "manual"})
	if err != nil {
		t.Fatal(err)
	}
	return row
}

func ids(hits []Hit) []string {
	out := make([]string, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.ShortID)
	}
	return out
}

func TestSearchEmbeddingsHandMadeVectors(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	const model = "test-model"
	t1, t2, t3 := seedTxn(t, s, "Uno", 3), seedTxn(t, s, "Dos", 2), seedTxn(t, s, "Tres", 1)
	dead := seedTxn(t, s, "Anulado", 4)
	for i, row := range []TxnRow{t1, t2, t3, dead} {
		if err := s.UpsertEmbedding(ctx, row.ID, model, "h", "doc", unit(min(i, 2))); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.VoidTransaction(ctx, dead.ID); err != nil { // same vector as t1, must not show up
		t.Fatal(err)
	}
	hits, err := s.SearchEmbeddings(ctx, model, unit(0))
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(hits); len(got) != 3 || got[0] != t1.ShortID {
		t.Fatalf("hits = %v, want 3 with %s first", got, t1.ShortID)
	}
	if hits[0].Distance != 0 || hits[1].Distance != 1 {
		t.Fatalf("distances = %v, %v; want 0 and 1", hits[0].Distance, hits[1].Distance)
	}
	if hits[1].ShortID != t2.ShortID || hits[2].ShortID != t3.ShortID { // tie broken by occurred_on desc
		t.Fatalf("tie order = %v", ids(hits))
	}
	if hits[0].MerchantCanon != "Uno" || hits[0].Currency != "MXN" {
		t.Fatalf("row columns not scanned: %+v", hits[0].TxnRow)
	}
	// an updated vector replaces the old one (upsert, not a second row)
	if err := s.UpsertEmbedding(ctx, t3.ID, model, "h2", "doc2", unit(0)); err != nil {
		t.Fatal(err)
	}
	if hits, err = s.SearchEmbeddings(ctx, model, unit(0)); err != nil || len(hits) != 3 {
		t.Fatalf("after upsert: %v hits, err %v", len(hits), err)
	}
	if err := s.UpsertEmbedding(ctx, t3.ID, model, "h", "doc", unit(2)); err != nil {
		t.Fatal(err)
	}
	// wrong model: the rows are there, none of them answers
	if hits, err := s.SearchEmbeddings(ctx, "otro-model", unit(0)); err != nil || len(hits) != 0 {
		t.Fatalf("wrong model: %d hits, err %v", len(hits), err)
	}
	sim, err := s.SearchSimilar(ctx, model, t1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(sim); len(got) != 2 || got[0] != t2.ShortID || got[1] != t3.ShortID {
		t.Fatalf("SearchSimilar = %v, want t2,t3 (self excluded)", got)
	}
	// an unindexed target has no vector to rank by: no rows, the bot says "nada parecido"
	if none, err := s.SearchSimilar(ctx, model, seedTxn(t, s, "Sin vector", 5).ID); err != nil || len(none) != 0 {
		t.Fatalf("unindexed target: %d hits, err %v", len(none), err)
	}
}

func TestSearchTrigramSeeded(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	farm := seedTxn(t, s, "Farmacias Benavides", 3)
	comer := seedTxn(t, s, "La Comer", 2)
	docs := map[string]string{
		farm.ID:  "farmacias benavides · salud · farmacias benavides s.a.b. de c.v. · godonites panal med 11 un",
		comer.ID: "la comer · super · pan bolillo",
	}
	for id, doc := range docs {
		if err := s.UpsertEmbedding(ctx, id, "m", DocHash(doc), doc, unit(0)); err != nil {
			t.Fatal(err)
		}
	}
	for _, q := range []string{Fold("PAÑALES"), "farmasia"} {
		hits, err := s.SearchTrigram(ctx, q, "")
		if err != nil {
			t.Fatal(err)
		}
		if len(hits) != 2 || hits[0].ShortID != farm.ShortID {
			t.Fatalf("%q → %v, want %s first", q, ids(hits), farm.ShortID)
		}
		if hits[0].Distance >= hits[1].Distance {
			t.Fatalf("%q distances not sorted: %v vs %v", q, hits[0].Distance, hits[1].Distance)
		}
	}
	hits, err := s.SearchTrigram(ctx, "panales", farm.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(hits); len(got) != 1 || got[0] != comer.ShortID {
		t.Fatalf("exclude ignored: %v", got)
	}
}

func TestTxnDocsStale(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	const model = "test-model"
	row := seedTxn(t, s, "La Comer", 2)
	docs, err := s.TxnDocs(ctx, "")
	if err != nil || len(docs) != 1 {
		t.Fatalf("%d docs, err %v", len(docs), err)
	}
	if docs[0].Doc != "la comer" || !docs[0].Stale(model) {
		t.Fatalf("unindexed row: %+v", docs[0])
	}
	if err := s.UpsertEmbedding(ctx, row.ID, model, docs[0].Hash, docs[0].Doc, unit(0)); err != nil {
		t.Fatal(err)
	}
	fresh := func() TxnDoc {
		t.Helper()
		d, err := s.TxnDocs(ctx, row.ID)
		if err != nil || len(d) != 1 {
			t.Fatalf("%d docs, err %v", len(d), err)
		}
		return d[0]
	}
	if fresh().Stale(model) {
		t.Fatal("fresh row reported stale")
	}
	if fresh().Stale("otro-model") {
		// a model change must re-embed everything
	} else {
		t.Fatal("model change not detected")
	}
	if err := s.EditTransaction(ctx, row.ID, map[string]any{"amount_minor": int64(999)}, nil); err != nil {
		t.Fatal(err)
	}
	if fresh().Stale(model) {
		t.Fatal("an amount edit must not re-embed")
	}
	if err := s.EditTransaction(ctx, row.ID,
		map[string]any{"category": "super", "category_source": "human"}, nil); err != nil {
		t.Fatal(err)
	}
	d := fresh()
	if !d.Stale(model) || d.Doc != "la comer · super" {
		t.Fatalf("category edit: %+v", d)
	}
}
