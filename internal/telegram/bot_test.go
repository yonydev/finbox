package telegram

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"finbox/internal/extract"
	"finbox/internal/pipeline"
	"finbox/internal/store"
)

type call struct {
	method string
	chat   int64
	msgID  int64
	text   string
	kb     *InlineKeyboard
}

type fakeAPI struct {
	calls   []call
	nextMsg int64
	file    []byte
}

func (f *fakeAPI) GetUpdates(context.Context, int64, int) ([]Update, error) { return nil, nil }
func (f *fakeAPI) SendMessage(_ context.Context, chat int64, html string, kb *InlineKeyboard) (Message, error) {
	f.nextMsg++
	f.calls = append(f.calls, call{"send", chat, f.nextMsg, html, kb})
	return Message{MessageID: f.nextMsg, Chat: Chat{ID: chat}}, nil
}
func (f *fakeAPI) EditMessageText(_ context.Context, chat, msgID int64, html string, kb *InlineKeyboard) error {
	f.calls = append(f.calls, call{"edit", chat, msgID, html, kb})
	return nil
}
func (f *fakeAPI) AnswerCallbackQuery(_ context.Context, id string) error {
	f.calls = append(f.calls, call{method: "answer", text: id})
	return nil
}
func (f *fakeAPI) GetFile(_ context.Context, id string) (File, error) {
	return File{FileID: id, FilePath: "photos/x.jpg", FileSize: int64(len(f.file))}, nil
}
func (f *fakeAPI) Download(context.Context, string) ([]byte, error)  { return f.file, nil }
func (f *fakeAPI) SetMyCommands(context.Context, []BotCommand) error { return nil }

func (f *fakeAPI) last() call { return f.calls[len(f.calls)-1] }

func newBot(t *testing.T, ex pipeline.Extractor) (*Bot, *fakeAPI, *store.Store) {
	t.Helper()
	st := store.NewTest(t)
	api := &fakeAPI{file: append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, make([]byte, 16)...)}
	d := pipeline.Deps{Store: st, Blob: &memBlob{m: map[string][]byte{}}, Extractor: ex, Loc: time.UTC, Log: slog.Default()}
	return NewBot(api, d, []int64{111}), api, st
}

type memBlob struct{ m map[string][]byte }

func (b *memBlob) Put(_ context.Context, k string, d []byte) error { b.m[k] = d; return nil }
func (b *memBlob) Get(_ context.Context, k string) ([]byte, error) { return b.m[k], nil }

type okExtractor struct{}

func (okExtractor) Extract(context.Context, []byte, string) (extract.Result, error) {
	return extract.Result{Extraction: extract.Extraction{
		Merchant: "Walmart", Date: "2026-08-28", Currency: "MXN", Total: "364.00",
	}, Model: "gpt-4o-mini", RawJSON: []byte(`{}`)}, nil
}

func photoUpdate(updateID, userID int64) Update {
	return Update{UpdateID: updateID, Message: &Message{
		MessageID: updateID * 10, From: &User{ID: userID}, Chat: Chat{ID: userID},
		Photo: []PhotoSize{{FileID: "f1", FileSize: 100}},
	}}
}

func TestStrangerIsIgnored(t *testing.T) {
	b, api, _ := newBot(t, okExtractor{})
	b.HandleUpdate(context.Background(), photoUpdate(1, 999)) // not allowlisted
	if len(api.calls) != 0 {
		t.Fatalf("calls = %+v", api.calls)
	}
}

func TestPhotoHappyPath(t *testing.T) {
	b, api, st := newBot(t, okExtractor{})
	b.HandleUpdate(context.Background(), photoUpdate(2, 111))
	if len(api.calls) < 2 || api.calls[0].method != "send" || !strings.Contains(api.calls[0].text, "Leyendo") {
		t.Fatalf("calls = %+v", api.calls)
	}
	last := api.last()
	if last.method != "edit" || !strings.Contains(last.text, "Walmart") || last.kb == nil {
		t.Fatalf("last = %+v", last)
	}
	recs, _ := st.PendingReceipts(context.Background())
	if len(recs) != 1 || recs[0].TgCardMessageID == 0 {
		t.Fatalf("receipt card not saved: %+v", recs)
	}
}

func TestConfirmCallbackSavesAndIsIdempotent(t *testing.T) {
	b, api, st := newBot(t, okExtractor{})
	b.HandleUpdate(context.Background(), photoUpdate(3, 111))
	recs, _ := st.PendingReceipts(context.Background())
	rec := recs[0]
	cb := Update{UpdateID: 4, CallbackQuery: &CallbackQuery{
		ID: "cb1", From: &User{ID: 111}, Data: "c|" + rec.ID,
		Message: &Message{MessageID: rec.TgCardMessageID, Chat: Chat{ID: 111}},
	}}
	b.HandleUpdate(context.Background(), cb)
	if !strings.Contains(api.last().text, "Guardado") || api.last().kb != nil {
		t.Fatalf("last = %+v", api.last())
	}
	rows, _ := st.ListTransactions(context.Background(), 10, 0, 0, time.UTC)
	if len(rows) != 1 {
		t.Fatalf("rows = %d", len(rows))
	}
	// replay the same update: dedup makes it a no-op
	n := len(api.calls)
	b.HandleUpdate(context.Background(), cb)
	if len(api.calls) != n {
		t.Fatalf("replayed update produced calls: %+v", api.calls[n:])
	}
	// stale tap with a NEW update id: answered + AlreadySaved edit, no second txn
	cb2 := cb
	cb2.UpdateID = 5
	cb2.CallbackQuery = &CallbackQuery{ID: "cb2", From: &User{ID: 111}, Data: cb.CallbackQuery.Data, Message: cb.CallbackQuery.Message}
	b.HandleUpdate(context.Background(), cb2)
	rows, _ = st.ListTransactions(context.Background(), 10, 0, 0, time.UTC)
	if len(rows) != 1 {
		t.Fatalf("stale tap created a txn")
	}
}

func TestListCommandClampsAt50(t *testing.T) {
	b, api, _ := newBot(t, okExtractor{})
	b.HandleUpdate(context.Background(), Update{UpdateID: 6, Message: &Message{
		MessageID: 60, From: &User{ID: 111}, Chat: Chat{ID: 111}, Text: "/list 100",
	}})
	if !strings.Contains(api.last().text, "máx. 50") {
		t.Fatalf("last = %+v", api.last())
	}
}

func TestDiscardCardHasRetryButton(t *testing.T) {
	b, api, st := newBot(t, okExtractor{})
	b.HandleUpdate(context.Background(), photoUpdate(20, 111))
	recs, _ := st.PendingReceipts(context.Background())
	rec := recs[0]
	cb := Update{UpdateID: 21, CallbackQuery: &CallbackQuery{
		ID: "cbd", From: &User{ID: 111}, Data: "d|" + rec.ID,
		Message: &Message{MessageID: rec.TgCardMessageID, Chat: Chat{ID: 111}},
	}}
	b.HandleUpdate(context.Background(), cb)
	last := api.last()
	if last.kb == nil || len(*last.kb) == 0 || len((*last.kb)[0]) == 0 || (*last.kb)[0][0].CallbackData != "r|"+rec.ID {
		t.Fatalf("last = %+v", last)
	}
}

func TestResendAfterDiscardRevives(t *testing.T) {
	b, api, st := newBot(t, okExtractor{})
	b.HandleUpdate(context.Background(), photoUpdate(30, 111)) // update 1: photo
	recs, _ := st.PendingReceipts(context.Background())
	rec := recs[0]
	cb := Update{UpdateID: 31, CallbackQuery: &CallbackQuery{ // update 2: discard
		ID: "cbd2", From: &User{ID: 111}, Data: "d|" + rec.ID,
		Message: &Message{MessageID: rec.TgCardMessageID, Chat: Chat{ID: 111}},
	}}
	b.HandleUpdate(context.Background(), cb)

	b.HandleUpdate(context.Background(), photoUpdate(32, 111)) // update 3: same bytes, new message
	last := api.last()
	if strings.Contains(last.text, "ya procesé") {
		t.Fatalf("still reports AlreadyProcessed after discard: %+v", last)
	}
	if last.kb == nil || len(*last.kb) == 0 || (*last.kb)[0][0].CallbackData != "c|"+rec.ID {
		t.Fatalf("resend after discard did not revive an awaiting card: %+v", last)
	}
	revived, err := st.GetReceipt(context.Background(), rec.ID)
	if err != nil || revived.Status != "awaiting_confirm" {
		t.Fatalf("receipt status = %+v, err = %v", revived, err)
	}
	n, err := st.CountReceipts(context.Background())
	if err != nil || n != 1 {
		t.Fatalf("receipts count = %d, err = %v", n, err)
	}
}

func TestResendAfterConfirmStillReports(t *testing.T) {
	b, api, st := newBot(t, okExtractor{})
	b.HandleUpdate(context.Background(), photoUpdate(40, 111)) // update 1: photo
	recs, _ := st.PendingReceipts(context.Background())
	rec := recs[0]
	cb := Update{UpdateID: 41, CallbackQuery: &CallbackQuery{ // update 2: confirm
		ID: "cbc", From: &User{ID: 111}, Data: "c|" + rec.ID,
		Message: &Message{MessageID: rec.TgCardMessageID, Chat: Chat{ID: 111}},
	}}
	b.HandleUpdate(context.Background(), cb)

	b.HandleUpdate(context.Background(), photoUpdate(42, 111)) // update 3: same bytes, new message
	last := api.last()
	if !strings.Contains(last.text, "ya procesé") {
		t.Fatalf("resend after confirm did not report AlreadyProcessed: %+v", last)
	}
	rows, err := st.ListTransactions(context.Background(), 10, 0, 0, time.UTC)
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows = %d, err = %v", len(rows), err)
	}
}

func TestBootstrapEmptyAllowlist(t *testing.T) {
	st := store.NewTest(t)
	api := &fakeAPI{}
	d := pipeline.Deps{Store: st, Blob: &memBlob{m: map[string][]byte{}}, Extractor: okExtractor{}, Loc: time.UTC, Log: slog.Default()}
	b := NewBot(api, d, nil) // empty allowlist
	b.HandleUpdate(context.Background(), photoUpdate(7, 12345))
	if len(api.calls) != 0 {
		t.Fatal("empty allowlist must not process or reply")
	}
}
