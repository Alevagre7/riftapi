package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/xalevagre7/riftapi/internal/domain"
)

// SyncStateRepo owns the single row in sync_state. The schema enforces
// id=1 (CHECK constraint), so there is no key parameter on these
// methods — there is always exactly one SyncState.
type SyncStateRepo struct{ db *sql.DB }

// NewSyncStateRepo returns a SyncStateRepo backed by db.
func NewSyncStateRepo(db *sql.DB) *SyncStateRepo { return &SyncStateRepo{db: db} }

// Get returns the current sync state. The migration bootstraps the
// row, so Get always returns a non-nil *SyncState; the fields are zero
// values until the first sync run updates them.
func (r *SyncStateRepo) Get(ctx context.Context) (*domain.SyncState, error) {
	const q = `
		SELECT last_sync_at, last_status, last_card_count, last_build_id, last_error
		FROM sync_state WHERE id = 1
	`
	var (
		out        domain.SyncState
		lastSync   sql.NullTime
		lastStatus sql.NullString
		lastCount  sql.NullInt32
		lastBuild  sql.NullString
		lastError  sql.NullString
	)
	err := r.db.QueryRowContext(ctx, q).Scan(
		&lastSync, &lastStatus, &lastCount, &lastBuild, &lastError,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("sync_state bootstrap row missing: %w", err)
		}
		return nil, err
	}
	if lastSync.Valid {
		t := lastSync.Time
		out.LastSyncAt = &t
	}
	if lastStatus.Valid {
		out.LastStatus = domain.SyncStatus(lastStatus.String)
	}
	if lastCount.Valid {
		out.LastCardCount = int(lastCount.Int32)
	}
	if lastBuild.Valid {
		out.LastBuildID = lastBuild.String
	}
	if lastError.Valid {
		out.LastError = lastError.String
	}
	return &out, nil
}

// Update writes the supplied state. The single-row identity is
// preserved (the WHERE clause matches id=1) so a concurrent Update
// from another process is detected by the row count.
func (r *SyncStateRepo) Update(ctx context.Context, s *domain.SyncState) error {
	const q = `
		UPDATE sync_state
		SET last_sync_at = ?, last_status = ?, last_card_count = ?,
		    last_build_id = ?, last_error = ?
		WHERE id = 1
	`
	var (
		lastSync  sql.NullTime
		lastBuild sql.NullString
		lastError sql.NullString
	)
	if s.LastSyncAt != nil {
		lastSync = sql.NullTime{Time: *s.LastSyncAt, Valid: true}
	}
	if s.LastBuildID != "" {
		lastBuild = sql.NullString{String: s.LastBuildID, Valid: true}
	}
	if s.LastError != "" {
		lastError = sql.NullString{String: s.LastError, Valid: true}
	}
	res, err := r.db.ExecContext(ctx, q,
		lastSync, string(s.LastStatus), s.LastCardCount, lastBuild, lastError,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("expected 1 row updated, got %d (sync_state bootstrap row missing?)", n)
	}
	return nil
}

// MarkOK is a convenience for the common success path. It stamps
// last_sync_at = now and writes the supplied card count + build id.
func (r *SyncStateRepo) MarkOK(ctx context.Context, cardCount int, buildID string) error {
	now := time.Now().UTC()
	return r.Update(ctx, &domain.SyncState{
		LastSyncAt:    &now,
		LastStatus:    domain.SyncStatusOK,
		LastCardCount: cardCount,
		LastBuildID:   buildID,
	})
}

// MarkFailed is a convenience for the common failure path. It stamps
// last_sync_at = now, sets last_status = 'failed', and records the
// error message. The previous snapshot is left in place.
func (r *SyncStateRepo) MarkFailed(ctx context.Context, err error) error {
	now := time.Now().UTC()
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	return r.Update(ctx, &domain.SyncState{
		LastSyncAt:  &now,
		LastStatus:  domain.SyncStatusFailed,
		LastError:   msg,
	})
}
