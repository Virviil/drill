package core

import (
	"context"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Migrate applies the *.sql files in dir (sorted by name) that module has
// not applied yet. Each module (core, every door) keeps its own history.
func Migrate(ctx context.Context, db *pgxpool.Pool, module string, dir fs.FS) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	// Serialise concurrent starts.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(7234001)`); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS public.hub_migrations (
		module     TEXT NOT NULL,
		version    TEXT NOT NULL,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT now(),
		PRIMARY KEY (module, version))`); err != nil {
		return err
	}
	names, err := fs.Glob(dir, "*.sql")
	if err != nil {
		return err
	}
	sort.Strings(names)
	for _, name := range names {
		version := strings.TrimSuffix(name, ".sql")
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM public.hub_migrations WHERE module = $1 AND version = $2)`,
			module, version).Scan(&exists); err != nil {
			return err
		}
		if exists {
			continue
		}
		body, err := fs.ReadFile(dir, name)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, string(body)); err != nil {
			return fmt.Errorf("%s/%s: %w", module, version, err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO public.hub_migrations (module, version) VALUES ($1, $2)`, module, version); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
