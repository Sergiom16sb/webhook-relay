// Package postgres provides the Postgres-backed adapter for application.EventStore.
package postgres

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/jmoiron/sqlx"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrate applies all .sql files in the migrations directory, in lexicographic
// order. Each file runs in a single transaction. Idempotent files (CREATE
// TABLE IF NOT EXISTS) make this safe to re-run.
//
// Trade-off vs golang-migrate: this is ~30 lines and teaches the concept.
// golang-migrate tracks applied versions and prevents re-runs; useful in
// production with multiple replicas. For v1 (single instance, idempotent SQL)
// this is enough.
func Migrate(ctx context.Context, db *sqlx.DB) error {
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}

	// Sort by filename (0001_xxx.sql, 0002_xxx.sql, ...) for deterministic order.
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	for _, name := range names {
		body, err := fs.ReadFile(migrationsFS, "migrations/"+name)
		if err != nil {
			return fmt.Errorf("read %s: %w", name, err)
		}

		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin tx for %s: %w", name, err)
		}

		if _, err := tx.ExecContext(ctx, string(body)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply %s: %w", name, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit %s: %w", name, err)
		}
	}
	return nil
}
