package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"finbox/internal/extract"
	"finbox/internal/messages"
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
		Merchant: "Walmart", Date: "2026-08-28", Currency: "MXN", Total: "364.00", Category: "super",
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
	if !strings.Contains(api.last().text, "mándame la foto") {
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

// confirmUpdate presses the card's ✅ button.
func confirmUpdate(updateID int64, rec store.Receipt) Update {
	return Update{UpdateID: updateID, CallbackQuery: &CallbackQuery{
		ID: fmt.Sprintf("cb%d", updateID), From: &User{ID: 111}, Data: "c|" + rec.ID,
		Message: &Message{MessageID: rec.TgCardMessageID, Chat: Chat{ID: 111}},
	}}
}

// The whole provenance wiring end to end: the extractor's own category is
// stamped llm and logs nothing, a reply overwrites it as human and logs the
// correction. Fails if Amend drops the marker or Run ignores it.
func TestCategoryProvenanceThroughConfirm(t *testing.T) {
	b, _, st := newBot(t, okExtractor{})
	rec := cardOf(t, b, st, 70)
	b.HandleUpdate(context.Background(), replyUpdate(71, 111, rec.TgCardMessageID, "285.50"))
	b.HandleUpdate(context.Background(), confirmUpdate(72, rec))

	rows, _ := st.ListTransactions(context.Background(), 10, 0, 0, time.UTC)
	if len(rows) != 1 || rows[0].Category != "super" || rows[0].CategorySource != "llm" {
		t.Fatalf("rows = %+v", rows)
	}
	for _, e := range st.EditLogForTest(t, rows[0].ID) {
		if strings.HasPrefix(e, "category ") {
			t.Errorf("phantom category edit: %q", e)
		}
	}

	b2, api2, st2 := newBot(t, okExtractor{})
	rec2 := cardOf(t, b2, st2, 73)
	b2.HandleUpdate(context.Background(), replyUpdate(74, 111, rec2.TgCardMessageID, "categoria hogar"))
	pending := callsOf(api2, 0, "edit")
	card := pending[len(pending)-1]
	if !strings.Contains(card.text, "categoría: hogar") || strings.Contains(card.text, "sugerida") {
		t.Fatalf("corrected pending card = %s", card.text)
	}
	b2.HandleUpdate(context.Background(), confirmUpdate(75, rec2))
	rows2, _ := st2.ListTransactions(context.Background(), 10, 0, 0, time.UTC)
	if len(rows2) != 1 || rows2[0].Category != "hogar" || rows2[0].CategorySource != "human" {
		t.Fatalf("rows2 = %+v", rows2)
	}
	if got := st2.EditLogForTest(t, rows2[0].ID); len(got) != 1 || got[0] != "category super>hogar reply" {
		t.Fatalf("edit_log = %q", got)
	}
	if last := api2.last(); !strings.Contains(last.text, "Guardado") || strings.Contains(last.text, "sugerida") {
		t.Errorf("saved card = %s", last.text)
	}
}

func textUpdate(updateID int64, text string) Update {
	return Update{UpdateID: updateID, Message: &Message{
		MessageID: updateID * 10, From: &User{ID: 111}, Chat: Chat{ID: 111}, Text: text,
	}}
}

func cbUpdate(updateID int64, data string, cardMsgID int64) Update {
	return Update{UpdateID: updateID, CallbackQuery: &CallbackQuery{
		ID: fmt.Sprintf("cb%d", updateID), From: &User{ID: 111}, Data: data,
		Message: &Message{MessageID: cardMsgID, Chat: Chat{ID: 111}},
	}}
}

func TestAddSavesAndUndoes(t *testing.T) {
	b, api, st := newBot(t, okExtractor{})
	ctx := context.Background()
	b.HandleUpdate(ctx, textUpdate(80, "/add 500 Limpieza Paty categoria servicios"))

	card := api.last()
	for _, want := range []string{"Guardado", "Limpieza Paty", "servicios", "Deshacer"} {
		if !strings.Contains(card.text, want) {
			t.Errorf("saved card missing %q:\n%s", want, card.text)
		}
	}
	if strings.Contains(card.text, "en el ticket") {
		t.Errorf("manual row must not show the raw-name line:\n%s", card.text)
	}
	if card.kb == nil || len(*card.kb) != 1 || len((*card.kb)[0]) != 1 ||
		!strings.HasPrefix((*card.kb)[0][0].CallbackData, "v|") {
		t.Fatalf("undo button = %+v", card.kb)
	}
	rows, _ := st.ListTransactions(ctx, 10, 0, 0, time.UTC)
	if len(rows) != 1 || rows[0].Source != "manual" || rows[0].AmountMinor != 50000 ||
		rows[0].Category != "servicios" || rows[0].CategorySource != "human" {
		t.Fatalf("rows = %+v", rows)
	}

	undo := cbUpdate(81, (*card.kb)[0][0].CallbackData, card.msgID)
	b.HandleUpdate(ctx, undo)
	if rows, _ = st.ListTransactions(ctx, 10, 0, 0, time.UTC); len(rows) != 0 {
		t.Fatalf("undo left the row: %+v", rows)
	}
	last := api.last()
	if last.method != "edit" || last.msgID != card.msgID || !strings.Contains(last.text, "Deshecho") {
		t.Fatalf("undone card = %+v", last)
	}
	// replay the same update: dedup makes it a no-op
	n := len(api.calls)
	b.HandleUpdate(ctx, undo)
	if len(api.calls) != n {
		t.Fatalf("replayed update produced calls: %+v", api.calls[n:])
	}
	// second tap, new update id
	b.HandleUpdate(ctx, cbUpdate(82, undo.CallbackQuery.Data, card.msgID))
	if !strings.Contains(api.last().text, "ya estaba deshecho") {
		t.Fatalf("second tap = %+v", api.last())
	}
}

func TestAddTeaches(t *testing.T) {
	b, api, st := newBot(t, okExtractor{})
	cases := []struct {
		text, want string
		help       bool
	}{
		{"/add", "registra un gasto sin ticket", true},
		{"/add Limpieza Paty", "falta el monto", true},
		{"/add 500", "falta el comercio", true},
		{"/add 24 Farmacia 24", "dos números", true},
		{"/add 500 pague la limpieza de la casa", "no te entendí", true},
		{"/add 500 Uber categoria comida", "no conozco la categoría", false},
	}
	for i, tc := range cases {
		b.HandleUpdate(context.Background(), textUpdate(int64(90+i), tc.text))
		got := api.last().text
		if !strings.Contains(got, tc.want) {
			t.Errorf("%q → %q, want %q", tc.text, got, tc.want)
		}
		if hasHelp := strings.Contains(got, "el monto va primero"); hasHelp != tc.help {
			t.Errorf("%q: help = %v, want %v:\n%s", tc.text, hasHelp, tc.help, got)
		}
	}
	if rows, _ := st.ListTransactions(context.Background(), 10, 0, 0, time.UTC); len(rows) != 0 {
		t.Fatalf("a rejected /add saved something: %+v", rows)
	}
}

func TestAddWarnsAboutNearbyDuplicate(t *testing.T) {
	b, api, st := newBot(t, okExtractor{})
	ctx := context.Background()
	if _, err := st.AddTransaction(ctx, store.NewTransaction{
		OccurredOn: time.Now().In(time.UTC).AddDate(0, 0, -2), Merchant: "ADSUGAS",
		AmountMinor: 45200, Currency: "MXN", Source: "manual"}); err != nil {
		t.Fatal(err)
	}
	b.HandleUpdate(ctx, textUpdate(95, "/add 452 ADSUGAS"))
	last := api.last()
	if !strings.Contains(last.text, "posible duplicado") || !strings.Contains(last.text, "$452.00") {
		t.Fatalf("no duplicate hint:\n%s", last.text)
	}
	if rows, _ := st.ListTransactions(ctx, 10, 0, 0, time.UTC); len(rows) != 2 {
		t.Fatalf("the hint must not block the insert: %+v", rows)
	}
}

func TestReplyCorrectsManualCard(t *testing.T) {
	b, api, st := newBot(t, okExtractor{})
	ctx := context.Background()
	b.HandleUpdate(ctx, textUpdate(96, "/add 500 Limpieza Paty"))
	card := api.last()
	rows, _ := st.ListTransactions(ctx, 10, 0, 0, time.UTC)

	u := replyUpdate(97, 111, card.msgID, "categoria otros")
	u.Message.ReplyTo.Text = "🧾 " + rows[0].ShortID + " · Limpieza Paty" // Telegram delivers the card as plain text
	b.HandleUpdate(ctx, u)

	rows, _ = st.ListTransactions(ctx, 10, 0, 0, time.UTC)
	if len(rows) != 1 || rows[0].Category != "otros" || rows[0].CategorySource != "human" || !rows[0].Edited {
		t.Fatalf("rows = %+v", rows)
	}
	edits := callsOf(api, 0, "edit")
	if len(edits) == 0 {
		t.Fatal("card not re-rendered")
	}
	recard := edits[len(edits)-1]
	if recard.msgID != card.msgID || recard.kb == nil ||
		!strings.HasPrefix((*recard.kb)[0][0].CallbackData, "v|") {
		t.Fatalf("undo button lost on re-render: %+v", recard)
	}
	for _, want := range []string{"otros", "✏️", "Guardado", "Deshacer"} {
		if !strings.Contains(recard.text, want) {
			t.Errorf("re-rendered card missing %q:\n%s", want, recard.text)
		}
	}
}

func TestSearchTakesWholeLine(t *testing.T) {
	b, api, st := newBot(t, okExtractor{}) // Embedder nil: the trigram baseline, no network
	ctx := context.Background()
	vec := make([]float64, 512)
	vec[0] = 1
	seed := func(merchant, doc string, day int) store.TxnRow {
		t.Helper()
		row, err := st.AddTransaction(ctx, store.NewTransaction{
			OccurredOn: time.Date(2026, 9, day, 0, 0, 0, 0, time.UTC),
			Merchant:   merchant, AmountMinor: 12300, Currency: "MXN", Source: "manual"})
		if err != nil {
			t.Fatal(err)
		}
		if err := st.UpsertEmbedding(ctx, row.ID, "m", store.DocHash(doc), doc, vec); err != nil {
			t.Fatal(err)
		}
		return row
	}
	dulces := seed("Oxxo", "oxxo · super · chocolate kinder bueno · sabritas", 3)
	gemelo := seed("Oxxo Centro", "oxxo · super · chocolate kinder huevo", 4)
	seed("Uber", "uber · transporte", 2)

	b.HandleUpdate(ctx, textUpdate(70, "/search kinder bueno"))
	last := api.last()
	if !strings.Contains(last.text, "<pre>") || !strings.Contains(last.text, dulces.ShortID) {
		t.Fatalf("search reply = %q", last.text)
	}
	if strings.Contains(last.text, "TOTAL") { // a sum of five retrieved rows would be a lie
		t.Fatalf("search table must not sum: %q", last.text)
	}
	if !strings.Contains(last.text, "«kinder bueno»") || last.kb != closeKB {
		t.Fatalf("header/keyboard = %q %+v", last.text, last.kb)
	}

	b.HandleUpdate(ctx, textUpdate(71, "/search zzzzzz"))
	if last = api.last(); !strings.Contains(last.text, "nada parecido a «zzzzzz»") {
		t.Fatalf("no-hit reply = %q", last.text)
	}

	b.HandleUpdate(ctx, textUpdate(72, "/search parecido a "+dulces.ShortID))
	_, table, _ := strings.Cut(api.last().text, "<pre>") // the header echoes the query, ids only in the table
	if !strings.Contains(table, gemelo.ShortID) || strings.Contains(table, dulces.ShortID) {
		t.Fatalf("parecido a must answer with the OTHER rows: %q", table)
	}
	if strings.Contains(table, "Uber") { // too far: the cutoff trimmed it
		t.Fatalf("cutoff not applied: %q", table)
	}

	b.HandleUpdate(ctx, textUpdate(73, "/search parecido a 00000000"))
	if last = api.last(); last.text != fmt.Sprintf(messages.SearchNoSuchExpense, "00000000") {
		t.Fatalf("bad id reply = %q", last.text)
	}

	b.HandleUpdate(ctx, textUpdate(74, "/search"))
	if last = api.last(); last.text != messages.SearchHelp {
		t.Fatalf("empty query reply = %q", last.text)
	}
}
