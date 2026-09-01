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
