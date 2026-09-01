package store

import (
	"context"
	"embed"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrations embed.FS

type Store struct{ pool *pgxpool.Pool }

func New(ctx context.Context, dbURL string) (*Store, error) {
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return &Store{pool: pool}, nil
}

func (s *Store) Close() { s.pool.Close() }

// Migrate applies pending numbered migrations under an advisory lock.
func (s *Store) Migrate(ctx context.Context) (int, error) {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return 0, err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `select pg_advisory_lock(722001)`); err != nil {
		return 0, err
	}
	defer conn.Exec(ctx, `select pg_advisory_unlock(722001)`)
	if _, err := conn.Exec(ctx,
		`create table if not exists schema_migrations (name text primary key, applied_at timestamptz not null default now())`); err != nil {
		return 0, err
	}
	entries, err := migrations.ReadDir("migrations")
	if err != nil {
		return 0, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	applied := 0
	for _, name := range names {
		var exists bool
		if err := conn.QueryRow(ctx, `select exists(select 1 from schema_migrations where name=$1)`, name).Scan(&exists); err != nil {
			return applied, err
		}
		if exists {
			continue
		}
		sql, err := migrations.ReadFile("migrations/" + name)
		if err != nil {
			return applied, err
		}
		err = pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, string(sql)); err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
			_, err := tx.Exec(ctx, `insert into schema_migrations(name) values($1)`, name)
			return err
		})
		if err != nil {
			return applied, err
		}
		applied++
	}
	return applied, nil
}
