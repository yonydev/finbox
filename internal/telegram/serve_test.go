package telegram

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"finbox/internal/pipeline"
	"finbox/internal/store"
)

type scriptedAPI struct {
	fakeAPI
	batches    [][]Update
	gotOffsets []int64
}

func (s *scriptedAPI) GetUpdates(ctx context.Context, offset int64, _ int) ([]Update, error) {
	s.gotOffsets = append(s.gotOffsets, offset)
	if len(s.batches) == 0 {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	b := s.batches[0]
	s.batches = s.batches[1:]
	return b, nil
}

func TestPollAdvancesOffsetAndStops(t *testing.T) {
	st := store.NewTest(t)
	api := &scriptedAPI{batches: [][]Update{
		{{UpdateID: 10, Message: &Message{From: &User{ID: 999}, Chat: Chat{ID: 999}, Text: "hi"}}}, // stranger → ignored, still acked
	}}
	d := pipeline.Deps{Store: st, Blob: &memBlob{m: map[string][]byte{}}, Extractor: okExtractor{}, Loc: time.UTC, Log: slog.Default()}
	b := NewBot(api, d, []int64{111})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := b.Poll(ctx); err != nil && ctx.Err() == nil {
		t.Fatal(err)
	}
	// second GetUpdates call must carry offset 11 (10+1)
	if len(api.gotOffsets) < 2 || api.gotOffsets[1] != 11 {
		t.Fatalf("offsets = %v", api.gotOffsets)
	}
}
