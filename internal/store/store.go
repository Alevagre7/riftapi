package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/xalevagre7/riftapi/internal/domain"

	// Pure-Go SQLite driver. No CGO, ARM cross-compile works without
	// a cross toolchain, which matters for the linux/arm64 Pi target.
	_ "modernc.org/sqlite"
)

// Store owns the connection to the SQLite file. There is one Store per
// process; all repositories read through it. Closing the Store closes
// the underlying *sql.DB.
type Store struct {
	db   *sql.DB
	path string
}

// Open opens (or creates) the database at path, sets the pragmas that
// the runtime needs (WAL mode, normal synchronous, foreign keys), and
// runs all pending migrations. The returned Store is safe for
// concurrent use.
func Open(ctx context.Context, path string) (*Store, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := ensureParentDir(path); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("sql.Open: %w", err)
	}

	// Limit the connection pool. modernc.org/sqlite is single-writer
	// but allows many readers; one connection is enough for our scale
	// and removes any risk of contention. The pool can be raised later
	// if real concurrency becomes a bottleneck.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	// Pragmas. journal_mode=WAL is a database-level setting and
	// persists across connections; the others are connection-level but
	// applied here at startup so every connection opened from the pool
	// inherits them on first use.
	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA foreign_keys=ON",
		"PRAGMA busy_timeout=5000",
	}
	for _, p := range pragmas {
		if _, err := db.ExecContext(ctx, p); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("%s: %w", p, err)
		}
	}

	if err := Migrate(ctx, db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return &Store{db: db, path: path}, nil
}

func ensureParentDir(path string) error {
	if path == "" || path == ":memory:" || strings.HasPrefix(path, "file:") {
		return nil
	}
	dir := filepath.Dir(path)
	if dir == "." || dir == "" {
		return nil
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("create database directory %q: %w", dir, err)
	}
	return nil
}

// Close releases the underlying database connection. Idempotent.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// DB returns the underlying *sql.DB. Repository constructors take this;
// most callers should use a repository instead of touching the *sql.DB
// directly.
func (s *Store) DB() *sql.DB { return s.db }

// Path returns the on-disk path of the database file.
func (s *Store) Path() string { return s.path }

// JournalMode reports the active journal mode. Used by tests to confirm
// that Open() set WAL mode correctly. Returns the raw pragma result
// ("wal", "delete", "memory", etc.).
func (s *Store) JournalMode(ctx context.Context) (string, error) {
	var mode string
	if err := s.db.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode); err != nil {
		return "", err
	}
	return mode, nil
}

// Cards returns a CardRepo bound to this store's database.
func (s *Store) Cards() *CardRepo { return NewCardRepo(s.db) }

// Sets returns a SetRepo bound to this store's database.
func (s *Store) Sets() *SetRepo { return NewSetRepo(s.db) }

// SyncState returns a SyncStateRepo bound to this store's database.
func (s *Store) SyncState() *SyncStateRepo { return NewSyncStateRepo(s.db) }

// HealthSnapshot reads the card count and sync state from one SQLite read
// transaction. This prevents /health from combining the old card count with
// the new sync state (or vice versa) while a scraper commits a snapshot.
func (s *Store) HealthSnapshot(ctx context.Context) (int, *domain.SyncState, error) {
	if s == nil || s.db == nil {
		return 0, nil, fmt.Errorf("store is not open")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, nil, fmt.Errorf("begin health read tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var cardCount int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(1) FROM cards`).Scan(&cardCount); err != nil {
		return 0, nil, err
	}
	state, err := getSyncState(ctx, tx)
	if err != nil {
		return 0, nil, err
	}
	if err := tx.Commit(); err != nil {
		return 0, nil, fmt.Errorf("commit health read tx: %w", err)
	}
	return cardCount, state, nil
}
