package telegram

import (
	"context"
	"fmt"
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
	calls     []call
	nextMsg   int64
	file      []byte
	deleteErr error
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
func (f *fakeAPI) DeleteMessage(_ context.Context, chat, msgID int64) error {
	f.calls = append(f.calls, call{method: "delete", chat: chat, msgID: msgID})
	return f.deleteErr
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

func (okExtractor) Extract(context.Context, []byte, string, time.Time) (extract.Result, error) {
	return extract.Result{Extraction: extract.Extraction{
		Merchant: "Walmart", Date: "2026-08-28", Currency: "MXN", Total: "364.00",
		Items: []extract.Item{{Name: "Café", Amount: "364.00"}},
	}, Model: "gpt-4o-mini", RawJSON: []byte(`{}`)}, nil
}

func photoUpdate(updateID, userID int64) Update {
	return Update{UpdateID: updateID, Message: &Message{
		MessageID: updateID * 10, From: &User{ID: userID}, Chat: Chat{ID: userID},
		Photo: []PhotoSize{{FileID: "f1", FileSize: 100}},
	}}
}

func replyUpdate(updateID, userID, cardMsgID int64, text string) Update {
	return Update{UpdateID: updateID, Message: &Message{
		MessageID: updateID * 10, From: &User{ID: userID}, Chat: Chat{ID: userID}, Text: text,
		ReplyTo: &Message{MessageID: cardMsgID, Chat: Chat{ID: userID}},
	}}
}

// cardOf runs the photo flow and returns the receipt behind the card.
func cardOf(t *testing.T, b *Bot, st *store.Store, updateID int64) store.Receipt {
	t.Helper()
	b.HandleUpdate(context.Background(), photoUpdate(updateID, 111))
	recs, err := st.PendingReceipts(context.Background())
	if err != nil || len(recs) != 1 {
		t.Fatalf("recs = %+v, err = %v", recs, err)
	}
	return recs[0]
}

func callsOf(api *fakeAPI, from int, method string) []call {
	var out []call
	for _, c := range api.calls[from:] {
		if c.method == method {
			out = append(out, c)
		}
	}
	return out
}

func TestReplyCorrectsPendingCard(t *testing.T) {
	b, api, st := newBot(t, okExtractor{})
	rec := cardOf(t, b, st, 50)
	b.HandleUpdate(context.Background(), replyUpdate(51, 111, rec.TgCardMessageID, "285.50"))

	last := api.last()
	if last.method != "delete" || last.msgID != 510 {
		t.Fatalf("user reply not deleted: %+v", last)
	}
	edits := callsOf(api, 0, "edit")
	card := edits[len(edits)-1]
	if card.msgID != rec.TgCardMessageID || card.kb == nil {
		t.Fatalf("card not re-rendered in place: %+v", card)
	}
	for _, want := range []string{"$285.50", "✏️", "Café"} {
		if !strings.Contains(card.text, want) {
			t.Errorf("card missing %q:\n%s", want, card.text)
		}
	}
	if strings.Contains(card.text, "los items suman") {
		t.Errorf("human total must silence the items warning:\n%s", card.text)
	}
	after, _ := st.GetReceipt(context.Background(), rec.ID)
	if after.ExtractionRaw == nil {
		t.Fatal("extraction_raw not captured")
	}
}

func TestReplyProseTeachesAndChangesNothing(t *testing.T) {
	b, api, st := newBot(t, okExtractor{})
	rec := cardOf(t, b, st, 52)
	n := len(api.calls)
	b.HandleUpdate(context.Background(), replyUpdate(53, 111, rec.TgCardMessageID, "creo que si esta bien"))

	sends := callsOf(api, n, "send")
	if len(sends) != 1 || !strings.Contains(sends[0].text, "no te entendí") {
		t.Fatalf("sends = %+v", sends)
	}
	if len(callsOf(api, n, "edit")) != 0 || len(callsOf(api, n, "delete")) != 0 {
		t.Fatalf("prose reply touched the card: %+v", api.calls[n:])
	}
	after, _ := st.GetReceipt(context.Background(), rec.ID)
	if string(after.Extraction) != string(rec.Extraction) || after.ExtractionRaw != nil {
		t.Fatalf("extraction changed: %s", after.Extraction)
	}
}

func TestConfirmAfterReplyMarksEdited(t *testing.T) {
	b, api, st := newBot(t, okExtractor{})
	rec := cardOf(t, b, st, 54)
	b.HandleUpdate(context.Background(), replyUpdate(55, 111, rec.TgCardMessageID, "285.50"))
	b.HandleUpdate(context.Background(), Update{UpdateID: 56, CallbackQuery: &CallbackQuery{
		ID: "cbe", From: &User{ID: 111}, Data: "c|" + rec.ID,
		Message: &Message{MessageID: rec.TgCardMessageID, Chat: Chat{ID: 111}},
	}})
	rows, _ := st.ListTransactions(context.Background(), 10, 0, 0, time.UTC)
	if len(rows) != 1 || !rows[0].Edited || rows[0].AmountMinor != 28550 {
		t.Fatalf("rows = %+v", rows)
	}
	last := api.last()
	if !strings.Contains(last.text, "Guardado") || !strings.Contains(last.text, "✏️") {
		t.Fatalf("saved card = %+v", last)
	}
}

func TestReplyToConfirmedCardEditsTransaction(t *testing.T) {
	b, api, st := newBot(t, okExtractor{})
	rec := cardOf(t, b, st, 57)
	b.HandleUpdate(context.Background(), Update{UpdateID: 58, CallbackQuery: &CallbackQuery{
		ID: "cbs", From: &User{ID: 111}, Data: "c|" + rec.ID,
		Message: &Message{MessageID: rec.TgCardMessageID, Chat: Chat{ID: 111}},
	}})
	b.HandleUpdate(context.Background(), replyUpdate(59, 111, rec.TgCardMessageID, "comercio Soriana"))

	rows, _ := st.ListTransactions(context.Background(), 10, 0, 0, time.UTC)
	if len(rows) != 1 || rows[0].Merchant != "Walmart" || rows[0].MerchantCanon != "Soriana" || !rows[0].Edited {
		t.Fatalf("rows = %+v", rows)
	}
	edits := callsOf(api, 0, "edit")
	card := edits[len(edits)-1]
	for _, want := range []string{"Soriana", "Guardado", "✏️", "Café"} {
		if !strings.Contains(card.text, want) {
			t.Errorf("re-rendered card missing %q:\n%s", want, card.text)
		}
	}
}

func TestReplyToNonCardFallsThroughToRouter(t *testing.T) {
	b, api, st := newBot(t, okExtractor{})
	rec := cardOf(t, b, st, 60)
	n := len(api.calls)
	b.HandleUpdate(context.Background(), replyUpdate(61, 111, 9999, "15/09")) // not a card
	if !strings.Contains(api.last().text, "mándame una foto") {
		t.Fatalf("last = %+v", api.last())
	}
	if len(callsOf(api, n, "delete")) != 0 {
		t.Fatalf("deleted a message that was not a correction: %+v", api.calls[n:])
	}
	// a command replying to the card stays a command
	b.HandleUpdate(context.Background(), replyUpdate(62, 111, rec.TgCardMessageID, "/list"))
	if !strings.Contains(api.last().text, "sin gastos") {
		t.Fatalf("/list in a reply did not run as a command: %+v", api.last())
	}
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

func TestListCommandCarriesCloseButton(t *testing.T) {
	b, api, _ := newBot(t, okExtractor{})
	b.HandleUpdate(context.Background(), Update{UpdateID: 30, Message: &Message{
		MessageID: 300, From: &User{ID: 111}, Chat: Chat{ID: 111}, Text: "/list",
	}})
	last := api.last()
	if last.kb == nil || (*last.kb)[0][0].CallbackData != "x|-" {
		t.Fatalf("list reply missing close button: %+v", last)
	}
}

func TestCloseCallbackDeletesMessage(t *testing.T) {
	b, api, _ := newBot(t, okExtractor{})
	b.HandleUpdate(context.Background(), Update{UpdateID: 31, CallbackQuery: &CallbackQuery{
		ID: "cbx", From: &User{ID: 111}, Data: "x|-",
		Message: &Message{MessageID: 42, Chat: Chat{ID: 111}},
	}})
	last := api.last()
	if last.method != "delete" || last.msgID != 42 {
		t.Fatalf("expected delete of msg 42, last = %+v", last)
	}
}

func TestCloseCallbackFallsBackToEditWhenDeleteFails(t *testing.T) {
	b, api, _ := newBot(t, okExtractor{})
	api.deleteErr = context.DeadlineExceeded // any error: e.g. message older than 48h
	b.HandleUpdate(context.Background(), Update{UpdateID: 32, CallbackQuery: &CallbackQuery{
		ID: "cbx2", From: &User{ID: 111}, Data: "x|-",
		Message: &Message{MessageID: 43, Chat: Chat{ID: 111}},
	}})
	last := api.last()
	if last.method != "edit" || last.msgID != 43 || !strings.Contains(last.text, "cerrada") {
		t.Fatalf("expected collapse edit, last = %+v", last)
	}
}

type errExtractor struct{}

func (errExtractor) Extract(context.Context, []byte, string, time.Time) (extract.Result, error) {
	return extract.Result{}, fmt.Errorf("%w: ilegible", extract.ErrNonRetryable)
}

func TestFailedCardCanBeDiscarded(t *testing.T) {
	b, api, st := newBot(t, errExtractor{})
	b.HandleUpdate(context.Background(), photoUpdate(40, 111))
	last := api.last()
	if last.kb == nil || len((*last.kb)[0]) != 2 || !strings.HasPrefix((*last.kb)[0][1].CallbackData, "d|") {
		t.Fatalf("failed card missing discard button: %+v", last)
	}
	recs, _ := st.PendingReceipts(context.Background())
	if len(recs) != 1 {
		t.Fatalf("recs = %+v", recs)
	}
	rec := recs[0]
	b.HandleUpdate(context.Background(), Update{UpdateID: 41, CallbackQuery: &CallbackQuery{
		ID: "cbf", From: &User{ID: 111}, Data: "d|" + rec.ID,
		Message: &Message{MessageID: rec.TgCardMessageID, Chat: Chat{ID: 111}},
	}})
	if !strings.Contains(api.last().text, "Descartado") {
		t.Fatalf("last = %+v", api.last())
	}
	if recs, _ = st.PendingReceipts(context.Background()); len(recs) != 0 {
		t.Fatalf("failed receipt still pending: %+v", recs)
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

// A rename before confirming renames only what the user sees: the raw name the
// model read stays on the row, and confirm logs exactly one merchant edit.
func TestReplyRenamesMerchantBeforeConfirm(t *testing.T) {
	b, api, st := newBot(t, okExtractor{})
	rec := cardOf(t, b, st, 62)
	b.HandleUpdate(context.Background(), replyUpdate(63, 111, rec.TgCardMessageID, "comercio Soriana"))

	edits := callsOf(api, 0, "edit")
	card := edits[len(edits)-1]
	for _, want := range []string{"<b>Soriana</b>", "en el ticket: Walmart"} {
		if !strings.Contains(card.text, want) {
			t.Errorf("pending card missing %q:\n%s", want, card.text)
		}
	}
	b.HandleUpdate(context.Background(), Update{UpdateID: 64, CallbackQuery: &CallbackQuery{
		ID: "cbr", From: &User{ID: 111}, Data: "c|" + rec.ID,
		Message: &Message{MessageID: rec.TgCardMessageID, Chat: Chat{ID: 111}},
	}})
	rows, _ := st.ListTransactions(context.Background(), 10, 0, 0, time.UTC)
	if len(rows) != 1 || rows[0].Merchant != "Walmart" || rows[0].MerchantCanon != "Soriana" || !rows[0].Edited {
		t.Fatalf("rows = %+v", rows)
	}
	got := st.EditLogForTest(t, rows[0].ID)
	if len(got) != 1 || got[0] != "merchant Walmart>Soriana reply" {
		t.Fatalf("edit_log = %q", got)
	}
}
