package telegram

import (
	"context"
	"fmt"
	"html"
	"time"

	"finbox/internal/messages"
	"finbox/internal/pipeline"
)

const anonymousAdminID = 1087968824 // spec §5: reject Telegram's anonymous-admin pseudo-id

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

// HandleUpdate processes one update; it never panics outward (spec §4).
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
	done, err := b.d.Store.ClaimUpdate(ctx, u.UpdateID)
	if err != nil {
		b.d.Log.Error("claim failed", "update_id", u.UpdateID, "err", err)
		return
	}
	if done {
		return
	}
	switch {
	case u.Message != nil && (len(u.Message.Photo) > 0 || u.Message.Document != nil):
		b.handlePhoto(ctx, u.Message)
	}
	if err := b.d.Store.CompleteUpdate(ctx, u.UpdateID); err != nil {
		b.d.Log.Error("complete failed", "update_id", u.UpdateID, "err", err)
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
		b.api.SendMessage(ctx, chat, messages.TooBig, nil)
		return
	}
	reading, err := b.api.SendMessage(ctx, chat, messages.Reading, nil)
	if err != nil {
		b.d.Log.Error("send failed", "err", err)
		return
	}
	f, err := b.api.GetFile(ctx, fileID)
	if err != nil {
		b.edit(ctx, chat, reading.MessageID, messages.DownloadFailed, nil)
		return
	}
	img, err := b.api.Download(ctx, f.FilePath)
	if err != nil {
		b.edit(ctx, chat, reading.MessageID, messages.DownloadFailed, nil)
		return
	}
	res, err := pipeline.IngestPhoto(ctx, b.d, img, m.MessageID, chat, time.Now())
	if err != nil {
		b.d.Log.Error("ingest failed", "err", err)
		b.edit(ctx, chat, reading.MessageID, messages.SomethingWrong, nil)
		return
	}
	b.renderResult(ctx, chat, reading.MessageID, res)
	if res.ReceiptID != "" {
		b.d.Store.SetCard(ctx, res.ReceiptID, chat, reading.MessageID)
	}
}

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
	case pipeline.OutcomeDuplicate:
		b.edit(ctx, chat, msgID, fmt.Sprintf("%s (<code>%s</code>)", messages.AlreadyProcessed, html.EscapeString(shortID(res.DuplicateOfID))), nil)
	case pipeline.OutcomeRejected:
		b.edit(ctx, chat, msgID, html.EscapeString(res.FailReason), nil)
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
