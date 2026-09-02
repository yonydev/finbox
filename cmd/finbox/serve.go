package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"finbox/internal/blob/fs"
	"finbox/internal/config"
	"finbox/internal/extract/openai"
	"finbox/internal/pipeline"
	"finbox/internal/store"
	"finbox/internal/telegram"
)

func cmdServe(_ []string, stdout, stderr io.Writer) int {
	cfg, err := config.FromEnv(os.Getenv)
	if err != nil {
		fmt.Fprintf(stderr, "config: %v\n", err)
		return exitUsage
	}
	for name, v := range map[string]string{
		"FINBOX_DB_URL": cfg.DBURL, "FINBOX_BLOB_DIR": cfg.BlobDir,
		"TELEGRAM_BOT_TOKEN": cfg.BotToken, "OPENAI_API_KEY": cfg.OpenAIKey,
	} {
		if v == "" {
			fmt.Fprintf(stderr, "falta %s\n", name)
			return exitUsage
		}
	}
	log := logger()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	st, err := store.New(ctx, cfg.DBURL)
	if err != nil {
		fmt.Fprintf(stderr, "db: %v\n", err)
		return exitRuntime
	}
	defer st.Close()
	if n, err := st.Migrate(ctx); err != nil {
		fmt.Fprintf(stderr, "migrate: %v\n", err)
		return exitRuntime
	} else if n > 0 {
		log.Info("applied migrations", "count", n)
	}

	api := telegram.NewClient(cfg.BotToken)
	d := pipeline.Deps{Store: st, Blob: fs.New(cfg.BlobDir),
		Extractor: openai.New(cfg.OpenAIKey, cfg.OpenAIModel), Loc: cfg.Loc, Log: log}
	bot := telegram.NewBot(api, d, cfg.AllowedUserIDs)

	if err := api.SetMyCommands(ctx, []telegram.BotCommand{
		{Command: "list", Description: "últimos gastos"},
		{Command: "month", Description: "total del mes"},
		{Command: "pending", Description: "recibos pendientes"},
		{Command: "help", Description: "ayuda"},
	}); err != nil {
		log.Error("set my commands failed", "err", err)
	}
	if err := bot.BootSweep(ctx); err != nil {
		log.Error("boot sweep failed", "err", err)
	}
	log.Info("finbox serve: polling", "allowlist_size", len(cfg.AllowedUserIDs))
	err = bot.Poll(ctx)
	if err != nil && ctx.Err() == nil {
		fmt.Fprintf(stderr, "poll: %v\n", err)
		return exitRuntime
	}
	fmt.Fprintln(stdout, "finbox serve: detenido")
	return exitOK
}
