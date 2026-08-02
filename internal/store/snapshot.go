package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/xalevagre7/riftapi/internal/domain"
)

// sqlExecer is implemented by both *sql.DB and *sql.Tx. Keeping the write
// helpers on this small interface lets the standalone repository methods and
// the full-snapshot transaction share exactly the same SQL.
type sqlExecer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

const setUpsertSQL = `
	INSERT INTO sets (set_id, card_count, payload)
	VALUES (?, ?, ?)
	ON CONFLICT(set_id) DO UPDATE SET
		card_count = excluded.card_count,
		payload    = excluded.payload
`

func execCardUpsert(ctx context.Context, execer sqlExecer, row CardRow) (sql.Result, error) {
	return execer.ExecContext(ctx, upsertSQL, cardArgs(row)...)
}

func execSetUpsert(ctx context.Context, execer sqlExecer, row SetRow) (sql.Result, error) {
	return execer.ExecContext(ctx, setUpsertSQL, row.SetID, row.CardCount, row.Payload)
}

// replaceCards writes the supplied card set and removes rows from older
// snapshots that are not in it. It must be called inside a transaction when
// callers need the replacement to be atomic.
func replaceCards(ctx context.Context, execer sqlExecer, rows []CardRow) error {
	ids := make([]string, 0, len(rows))
	seen := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		if _, ok := seen[row.RiftboundID]; ok {
			continue
		}
		seen[row.RiftboundID] = struct{}{}
		ids = append(ids, row.RiftboundID)
		if _, err := execCardUpsert(ctx, execer, row); err != nil {
			return fmt.Errorf("upsert card %s: %w", row.RiftboundID, err)
		}
	}

	if len(ids) == 0 {
		if _, err := execer.ExecContext(ctx, "DELETE FROM cards"); err != nil {
			return fmt.Errorf("delete cards: %w", err)
		}
		return nil
	}

	placeholders := strings.Repeat("?,", len(ids))
	placeholders = strings.TrimSuffix(placeholders, ",")
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	if _, err := execer.ExecContext(ctx,
		"DELETE FROM cards WHERE riftbound_id NOT IN ("+placeholders+")", args...,
	); err != nil {
		return fmt.Errorf("delete stale cards: %w", err)
	}
	return nil
}

// replaceSets mirrors replaceCards for the sets table. Keeping this in the
// same snapshot transaction prevents removed sets from lingering after a
// successful sync.
func replaceSets(ctx context.Context, execer sqlExecer, rows []SetRow) error {
	ids := make([]string, 0, len(rows))
	seen := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		if _, ok := seen[row.SetID]; ok {
			continue
		}
		seen[row.SetID] = struct{}{}
		ids = append(ids, row.SetID)
		if _, err := execSetUpsert(ctx, execer, row); err != nil {
			return fmt.Errorf("upsert set %s: %w", row.SetID, err)
		}
	}

	if len(ids) == 0 {
		if _, err := execer.ExecContext(ctx, "DELETE FROM sets"); err != nil {
			return fmt.Errorf("delete sets: %w", err)
		}
		return nil
	}

	placeholders := strings.Repeat("?,", len(ids))
	placeholders = strings.TrimSuffix(placeholders, ",")
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	if _, err := execer.ExecContext(ctx,
		"DELETE FROM sets WHERE set_id NOT IN ("+placeholders+")", args...,
	); err != nil {
		return fmt.Errorf("delete stale sets: %w", err)
	}
	return nil
}

// SyncSnapshot atomically replaces cards and sets and records a successful
// sync state. If any write fails, the old cards, sets, and success state stay
// together. Failed runs should call SyncState().MarkFailed separately, after
// this transaction has rolled back.
func (s *Store) SyncSnapshot(ctx context.Context, cards []CardRow, sets []SetRow, inputCount int, buildID string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("store is not open")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin snapshot tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := replaceCards(ctx, tx, cards); err != nil {
		return fmt.Errorf("replace cards: %w", err)
	}
	if err := replaceSets(ctx, tx, sets); err != nil {
		return fmt.Errorf("replace sets: %w", err)
	}
	if err := markSyncOK(ctx, tx, inputCount, buildID); err != nil {
		return fmt.Errorf("mark snapshot ok: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit snapshot: %w", err)
	}
	return nil
}

func markSyncOK(ctx context.Context, execer sqlExecer, inputCount int, buildID string) error {
	now := time.Now().UTC()
	return updateSyncState(ctx, execer, &domain.SyncState{
		LastSyncAt:         &now,
		LastStatus:         domain.SyncStatusOK,
		LastSyncInputCount: inputCount,
		LastBuildID:        buildID,
	})
}
