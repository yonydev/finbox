package telegram

import (
	"context"
	"time"

	"finbox/internal/pipeline"
)

func (b *Bot) BootSweep(ctx context.Context) error {
	if n, err := b.d.Store.PurgeProcessedUpdates(ctx, 7*24*time.Hour); err != nil {
		return err
	} else if n > 0 {
		b.d.Log.Info("purged processed_updates", "rows", n)
	}
	stale, err := b.d.Store.StalePending(ctx)
	if err != nil {
		return err
	}
	for _, rec := range stale {
		b.d.Log.Info("resuming stranded receipt", "receipt", rec.ID)
		res, err := pipeline.Reprocess(ctx, b.d, rec.ID, time.Now())
		if err != nil {
			b.d.Log.Error("boot reprocess failed", "receipt", rec.ID, "err", err)
			continue
		}
		if rec.TgChatID != 0 && rec.TgCardMessageID != 0 {
			b.renderResult(ctx, rec.TgChatID, rec.TgCardMessageID, res)
		}
	}
	return nil
}

// Poll runs the long-polling loop until ctx is cancelled.
func (b *Bot) Poll(ctx context.Context) error {
	var offset int64
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		ups, err := b.api.GetUpdates(ctx, offset, 60)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			b.d.Log.Error("getUpdates failed", "err", err)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(5 * time.Second):
			}
			continue
		}
		for _, u := range ups {
			b.HandleUpdate(ctx, u)
			if u.UpdateID >= offset {
				offset = u.UpdateID + 1
			}
		}
	}
}
