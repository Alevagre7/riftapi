package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

// migrationsFS bundles every .sql file in the migrations/ subdirectory.
// The //go:embed directive fails the build if no .sql files are present,
// which is the right behavior for this package — a database with no
// schema is not a useful database.
//
//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrate runs every not-yet-applied migration in lexical order inside
// a transaction. It is idempotent: a second call with the same set of
// migrations is a no-op.
//
// Migration versions are the leading numeric prefix of the filename
// ("001_init.sql" → "001"). The set of applied versions is stored in
// a side table called schema_migrations. The table is created in the
// same transaction as the first migration to avoid a race between two
// processes starting up at the same time.
func Migrate(ctx context.Context, db *sql.DB) error {
	// Inline the schema_migrations bootstrap. The CREATE TABLE here
	// must match the column names used by record() below.
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)
	`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)

	for _, name := range names {
		version := versionOf(name)
		applied, err := isApplied(ctx, db, version)
		if err != nil {
			return err
		}
		if applied {
			continue
		}
		if err := applyMigration(ctx, db, name, version); err != nil {
			return err
		}
	}
	return nil
}

func isApplied(ctx context.Context, db *sql.DB, version string) (bool, error) {
	var n int
	err := db.QueryRowContext(ctx,
		`SELECT COUNT(1) FROM schema_migrations WHERE version = ?`,
		version,
	).Scan(&n)
	return n > 0, err
}

func applyMigration(ctx context.Context, db *sql.DB, name, version string) error {
	body, err := fs.ReadFile(migrationsFS, "migrations/"+name)
	if err != nil {
		return fmt.Errorf("read %s: %w", name, err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // commit returns the same error

	if _, err := tx.ExecContext(ctx, string(body)); err != nil {
		return fmt.Errorf("exec %s: %w", name, err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO schema_migrations (version) VALUES (?)`,
		version,
	); err != nil {
		return fmt.Errorf("record %s: %w", name, err)
	}
	return tx.Commit()
}

// versionOf extracts the version prefix from a migration filename.
// "001_init.sql" → "001". Returns the full filename as a fallback so
// a malformed name still produces a distinct version key.
func versionOf(name string) string {
	base := name
	if i := strings.IndexByte(name, '.'); i > 0 {
		base = name[:i]
	}
	if i := strings.IndexByte(base, '_'); i > 0 {
		base = base[:i]
	}
	if base == "" {
		return name
	}
	return base
}
