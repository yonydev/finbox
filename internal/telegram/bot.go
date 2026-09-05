package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"strconv"
	"strings"
	"time"

	"finbox/internal/command"
	"finbox/internal/extract"
	"finbox/internal/messages"
	"finbox/internal/pipeline"
	"finbox/internal/store"
	"finbox/internal/validate"
)

const anonymousAdminID = 1087968824 // reject Telegram's anonymous-admin pseudo-id

type Bot struct {
	api     API
	d       pipeline.Deps
	allowed map[int64]bool
}

func NewBot(api API, d pipeline.Deps, allowed []int64) *Bot {
	m := map[int64]bool{}
	for _, id := range allowed {
		m[id] = true
	}
	return &Bot{api: api, d: d, allowed: m}
}

func (b *Bot) authorized(userID int64) bool {
	if userID == anonymousAdminID {
		return false
	}
	if len(b.allowed) == 0 {
		b.d.Log.Warn("allowlist empty: refusing to process — add this id to TELEGRAM_ALLOWED_USER_IDS", "user_id", userID)
		return false
	}
	if !b.allowed[userID] {
		b.d.Log.Warn("ignoring non-allowlisted sender", "user_id", userID)
		return false
	}
	return true
}

// HandleUpdate processes one update; it never panics outward.
func (b *Bot) HandleUpdate(ctx context.Context, u Update) {
	defer func() {
		if r := recover(); r != nil {
			b.d.Log.Error("panic in update handler", "update_id", u.UpdateID, "panic", r)
		}
	}()
	from := int64(0)
	switch {
	case u.Message != nil && u.Message.From != nil:
		from = u.Message.From.ID
	case u.CallbackQuery != nil && u.CallbackQuery.From != nil:
		from = u.CallbackQuery.From.ID
	default:
		return
	}
	if !b.authorized(from) {
		return
	}
	kind := "text"
	switch {
	case u.CallbackQuery != nil:
		kind = "callback"
	case u.Message != nil && (len(u.Message.Photo) > 0 || u.Message.Document != nil):
		kind = "photo"
	}
	b.d.Log.Info("update", "update_id", u.UpdateID, "user_id", from, "kind", kind)
	done, err := b.d.Store.ClaimUpdate(ctx, u.UpdateID)
	if err != nil {
		b.d.Log.Error("claim failed", "update_id", u.UpdateID, "err", err)
		return
	}
	if done {
		return
	}
	completeSeparately := true
	switch {
	case u.CallbackQuery != nil:
		completeSeparately = b.handleCallback(ctx, u.UpdateID, u.CallbackQuery)
	case u.Message != nil && (len(u.Message.Photo) > 0 || u.Message.Document != nil):
		b.handlePhoto(ctx, u.Message)
	case u.Message != nil:
		b.handleText(ctx, u.Message)
	}
	if completeSeparately {
		if err := b.d.Store.CompleteUpdate(ctx, u.UpdateID); err != nil {
			b.d.Log.Error("complete failed", "update_id", u.UpdateID, "err", err)
		}
	}
}

func (b *Bot) handlePhoto(ctx context.Context, m *Message) {
	chat := m.Chat.ID
	var fileID string
	var size int64
	if len(m.Photo) > 0 {
		best := m.Photo[len(m.Photo)-1] // largest rendition last
		fileID, size = best.FileID, best.FileSize
	} else {
		fileID, size = m.Document.FileID, m.Document.FileSize
	}
	if size > pipeline.MaxImageBytes {
		b.send(ctx, chat, messages.TooBig)
		return
	}
	reading, err := b.api.SendMessage(ctx, chat, messages.Reading, nil)
	if err != nil {
		b.d.Log.Error("send failed", "err", err)
		return
	}
	f, err := b.api.GetFile(ctx, fileID)
	if err != nil {
		b.d.Log.Error("get file failed", "chat", chat, "err", err)
		b.edit(ctx, chat, reading.MessageID, messages.DownloadFailed, nil)
		return
	}
	img, err := b.api.Download(ctx, f.FilePath)
	if err != nil {
		b.d.Log.Error("download failed", "chat", chat, "err", err)
		b.edit(ctx, chat, reading.MessageID, messages.DownloadFailed, nil)
		return
	}
	res, err := pipeline.IngestPhoto(ctx, b.d, img, m.MessageID, chat, time.Now())
	if err != nil {
		b.d.Log.Error("ingest failed", "err", err)
		b.edit(ctx, chat, reading.MessageID, messages.SomethingWrong, nil)
		return
	}
	if res.Outcome == pipeline.OutcomeDuplicate {
		b.handleDuplicate(ctx, chat, reading.MessageID, res.DuplicateOfID)
		return
	}
	b.renderResult(ctx, chat, reading.MessageID, res)
	if res.ReceiptID != "" {
		if err := b.d.Store.SetCard(ctx, res.ReceiptID, chat, reading.MessageID); err != nil {
			b.d.Log.Error("set card failed", "err", err)
		}
	}
}

// handleDuplicate answers a re-sent photo whose blob_sha256 already exists
// — status-aware, so a discarded/failed receipt gets a fresh
// reprocess onto this new message instead of a dead-end "already processed".
func (b *Bot) handleDuplicate(ctx context.Context, chat, msgID int64, existingID string) {
	existing, err := b.d.Store.GetReceipt(ctx, existingID)
	if err != nil {
		b.d.Log.Error("get receipt for duplicate failed", "err", err)
		b.edit(ctx, chat, msgID, fmt.Sprintf("%s (<code>%s</code>)", messages.AlreadyProcessed, html.EscapeString(shortID(existingID))), nil)
		return
	}
	switch existing.Status {
	case "discarded", "failed":
		res, err := pipeline.Reprocess(ctx, b.d, existing.ID, time.Now())
		if err != nil {
			b.d.Log.Error("reprocess on duplicate failed", "err", err)
			b.edit(ctx, chat, msgID, messages.SomethingWrong, nil)
			return
		}
		b.renderResult(ctx, chat, msgID, res)
		if err := b.d.Store.SetCard(ctx, existing.ID, chat, msgID); err != nil {
			b.d.Log.Error("set card failed", "err", err)
		}
	case "awaiting_confirm", "pending":
		b.edit(ctx, chat, msgID, fmt.Sprintf("%s (<code>%s</code>)", messages.AwaitingYourConfirm, html.EscapeString(shortID(existing.ID))), nil)
	default: // confirmed, or anything unexpected
		b.edit(ctx, chat, msgID, fmt.Sprintf("%s (<code>%s</code>)", messages.AlreadyProcessed, html.EscapeString(shortID(existing.ID))), nil)
	}
}

// renderResult renders every outcome except OutcomeDuplicate, which
// handleDuplicate intercepts before this is ever called.
func (b *Bot) renderResult(ctx context.Context, chat, msgID int64, res pipeline.Result) {
	short := shortID(res.ReceiptID)
	switch res.Outcome {
	case pipeline.OutcomeAwaitingConfirm:
		kb := &InlineKeyboard{{
			{Text: messages.BtnConfirm, CallbackData: "c|" + res.ReceiptID},
			{Text: messages.BtnDiscard, CallbackData: "d|" + res.ReceiptID},
		}}
		b.edit(ctx, chat, msgID, Card(short, res.Validated), kb)
	case pipeline.OutcomeFailed:
		kb := &InlineKeyboard{{{Text: messages.BtnRetry, CallbackData: "r|" + res.ReceiptID}}}
		b.edit(ctx, chat, msgID, FailedCard(short, res.FailReason), kb)
	case pipeline.OutcomeRejected:
		b.edit(ctx, chat, msgID, html.EscapeString(res.FailReason), nil)
	}
}

// handleCallback returns true when the caller must CompleteUpdate separately
// (confirm/discard stamp completion inside their own DB transaction).
func (b *Bot) handleCallback(ctx context.Context, updateID int64, cb *CallbackQuery) bool {
	if err := b.api.AnswerCallbackQuery(ctx, cb.ID); err != nil {
		b.d.Log.Warn("answer callback failed", "err", err) // best-effort ack; worst case a lingering spinner
	}
	parts := strings.SplitN(cb.Data, "|", 2)
	if len(parts) != 2 || cb.Message == nil {
		return true
	}
	action, receiptID := parts[0], parts[1]
	chat, msgID := cb.Message.Chat.ID, cb.Message.MessageID
	if action == "x" { // close: no receipt attached, handle before the lookup
		if err := b.api.DeleteMessage(ctx, chat, msgID); err != nil {
			// Telegram refuses deletes on messages older than 48h — collapse instead
			b.edit(ctx, chat, msgID, messages.ListClosed, nil)
		}
		return true
	}
	rec, err := b.d.Store.GetReceipt(ctx, receiptID)
	if err != nil {
		b.edit(ctx, chat, msgID, messages.ReceiptNotFound, nil)
		return true
	}
	short := shortID(rec.ID)
	switch action {
	case "c":
		v, verr := b.validatedFromStored(rec)
		if verr != nil {
			b.edit(ctx, chat, msgID, FailedCard(short, "extracción corrupta, usa reintentar"), nil)
			return true
		}
		_, ok, err := b.d.Store.ConfirmReceipt(ctx, rec.ID, store.NewTransaction{
			OccurredOn: v.OccurredOn, Merchant: v.Merchant, AmountMinor: v.AmountMinor,
			Currency: v.Currency, Source: "receipt", Items: itemsToNew(v),
		}, updateID)
		if err != nil {
			b.d.Log.Error("confirm failed", "err", err)
			return true
		}
		if !ok {
			b.edit(ctx, chat, msgID, fmt.Sprintf("<code>%s</code> · %s", short, messages.AlreadySaved), nil)
			if err := b.d.Store.CompleteUpdate(ctx, updateID); err != nil {
				b.d.Log.Error("complete update failed", "err", err)
			}
			return false
		}
		b.edit(ctx, chat, msgID, SavedCard(short, v), nil)
		return false // completion stamped inside ConfirmReceipt's tx
	case "d":
		ok, err := b.d.Store.DiscardReceipt(ctx, rec.ID, updateID)
		if err != nil {
			b.d.Log.Error("discard failed", "err", err)
			return true
		}
		if ok {
			kb := &InlineKeyboard{{{Text: messages.BtnRetry, CallbackData: "r|" + rec.ID}}}
			b.edit(ctx, chat, msgID, DiscardedCard(short), kb)
		} else { // stale tap — never leave a dead card
			b.edit(ctx, chat, msgID, fmt.Sprintf("<code>%s</code> · %s", short, messages.AlreadySaved), nil)
		}
		return false
	case "r":
		res, err := pipeline.Reprocess(ctx, b.d, rec.ID, time.Now())
		if err != nil {
			b.d.Log.Error("reprocess failed", "err", err)
			return true
		}
		b.renderResult(ctx, chat, msgID, res)
		return true
	}
	return true
}

// validatedFromStored re-runs validation over the stored post-scrub extraction,
// so confirm always persists exactly what the card showed.
func (b *Bot) validatedFromStored(rec store.Receipt) (validate.Validated, error) {
	var ex extract.Extraction
	if err := json.Unmarshal(rec.Extraction, &ex); err != nil {
		return validate.Validated{}, err
	}
	return validate.Run(ex, time.Now(), b.d.Loc)
}

func (b *Bot) handleText(ctx context.Context, m *Message) {
	chat := m.Chat.ID
	if m.ReplyTo != nil {
		short := "<id>"
		if rec, err := b.d.Store.GetReceiptByCard(ctx, chat, m.ReplyTo.MessageID); err == nil {
			short = shortID(rec.ID)
		}
		b.send(ctx, chat, fmt.Sprintf(messages.EditComingSoon, short))
		return
	}
	fields := strings.Fields(m.Text)
	if len(fields) == 0 || !strings.HasPrefix(fields[0], "/") {
		b.send(ctx, chat, messages.NotACommand)
		return
	}
	cmd := fields[0]
	if i := strings.IndexByte(cmd, '@'); i >= 0 { // "/list@finbox_bot" → "/list"
		cmd = cmd[:i]
	}
	arg := ""
	if len(fields) > 1 {
		arg = fields[1]
	}
	now := time.Now()
	switch cmd {
	case "/start", "/help":
		b.send(ctx, chat, messages.HelpText)
	case "/list":
		limit := 10
		capped := false
		if n, err := strconv.Atoi(arg); err == nil {
			if n > 50 {
				n, capped = 50, true
			}
			if n > 0 {
				limit = n
			}
		}
		rows, err := command.List(ctx, b.d.Store, limit, "", now, b.d.Loc)
		if err != nil {
			b.send(ctx, chat, html.EscapeString(err.Error()))
			return
		}
		msgs := ListTable(rows)
		if len(msgs) == 0 {
			msgs = []string{messages.NoExpenses}
		}
		if capped {
			msgs = append(msgs, messages.ListCapNote)
		}
		closeKB := &InlineKeyboard{{{Text: messages.BtnClose, CallbackData: "x|-"}}}
		for _, m := range msgs {
			b.sendKB(ctx, chat, m, closeKB)
		}
	case "/month":
		year, mo, totals, count, err := command.Month(ctx, b.d.Store, arg, now, b.d.Loc)
		if err != nil {
			b.send(ctx, chat, html.EscapeString(err.Error()))
			return
		}
		b.send(ctx, chat, MonthSummary(year, mo, totals, count))
	case "/pending":
		recs, err := command.Pending(ctx, b.d.Store)
		if err != nil {
			b.send(ctx, chat, html.EscapeString(err.Error()))
			return
		}
		if len(recs) == 0 {
			b.send(ctx, chat, messages.NothingPending)
			return
		}
		var lines []string
		for _, r := range recs {
			line := fmt.Sprintf("<code>%s</code> · %s", shortID(r.ID), r.Status)
			if r.FailReason != "" {
				line += " · " + html.EscapeString(r.FailReason)
			}
			lines = append(lines, line)
		}
		for _, chunk := range Chunk(lines, Budget) {
			b.send(ctx, chat, chunk)
		}
	default:
		b.send(ctx, chat, messages.NotACommand)
	}
}

// send is the fire-and-forget counterpart of edit: reply failures are logged,
// never fatal — the bot must keep processing updates.
func (b *Bot) send(ctx context.Context, chat int64, text string) {
	b.sendKB(ctx, chat, text, nil)
}

func (b *Bot) sendKB(ctx context.Context, chat int64, text string, kb *InlineKeyboard) {
	if _, err := b.api.SendMessage(ctx, chat, text, kb); err != nil {
		b.d.Log.Error("send failed", "chat", chat, "err", err)
	}
}

func (b *Bot) edit(ctx context.Context, chat, msgID int64, text string, kb *InlineKeyboard) {
	if err := b.api.EditMessageText(ctx, chat, msgID, text, kb); err != nil {
		b.d.Log.Error("edit failed", "err", err)
	}
}

func shortID(id string) string {
	if len(id) >= 8 {
		return id[:8]
	}
	return id
}

func itemsToNew(v validate.Validated) []store.NewItem {
	items := make([]store.NewItem, 0, len(v.Items))
	for _, it := range v.Items {
		items = append(items, store.NewItem{
			Position: it.Position, Name: it.Name,
			QuantityMilli: it.QuantityMilli, AmountMinor: it.AmountMinor,
		})
	}
	return items
}
