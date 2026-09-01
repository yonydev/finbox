package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"finbox/internal/config"
	"finbox/internal/store"
)

func cmdMigrate(_ []string, stdout, stderr io.Writer) int {
	cfg, err := config.FromEnv(os.Getenv)
	if err != nil || cfg.DBURL == "" {
		fmt.Fprintln(stderr, "falta FINBOX_DB_URL")
		return exitUsage
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s, err := store.New(ctx, cfg.DBURL)
	if err != nil {
		fmt.Fprintf(stderr, "db: %v\n", err)
		return exitRuntime
	}
	defer s.Close()
	n, err := s.Migrate(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "migrate: %v\n", err)
		return exitRuntime
	}
	fmt.Fprintf(stdout, "migraciones aplicadas: %d\n", n)
	return exitOK
}
