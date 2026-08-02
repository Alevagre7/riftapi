package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// SetRow is the database-level representation of a Set. The full
// Set JSON is in Payload; the indexed fields are the
// primary key (SetID) and the denormalised CardCount.
type SetRow struct {
	SetID     string
	CardCount int
	Payload   []byte
}

// SetRepo is the read/write surface for sets.
type SetRepo struct{ db *sql.DB }

// NewSetRepo returns a SetRepo backed by db.
func NewSetRepo(db *sql.DB) *SetRepo { return &SetRepo{db: db} }

// Upsert inserts the set or replaces the existing row with the same
// set_id. The Payload must be valid JSON.
func (r *SetRepo) Upsert(ctx context.Context, row SetRow) error {
	if _, err := execSetUpsert(ctx, r.db, row); err != nil {
		return fmt.Errorf("upsert set %s: %w", row.SetID, err)
	}
	return nil
}

// GetByID returns the set with the given set_id (e.g. "ogn") or
// sql.ErrNoRows if no such set exists. The match is case-insensitive;
// set_ids are conventionally lowercase but the contract
// permits any case.
func (r *SetRepo) GetByID(ctx context.Context, setID string) (*SetRow, error) {
	const q = `SELECT set_id, card_count, payload FROM sets WHERE set_id = ? COLLATE NOCASE`
	row := r.db.QueryRowContext(ctx, q, setID)
	out := &SetRow{}
	if err := row.Scan(&out.SetID, &out.CardCount, &out.Payload); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, sql.ErrNoRows
		}
		return nil, err
	}
	return out, nil
}

// All returns every set ordered by set_id. Used by /sets and tests.
func (r *SetRepo) All(ctx context.Context) ([]*SetRow, error) {
	const q = `SELECT set_id, card_count, payload FROM sets ORDER BY set_id`
	rows, err := r.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*SetRow
	for rows.Next() {
		out = append(out, &SetRow{})
		if err := rows.Scan(&out[len(out)-1].SetID, &out[len(out)-1].CardCount, &out[len(out)-1].Payload); err != nil {
			return nil, err
		}
	}
	return out, rows.Err()
}

// ListSetsOptions is the parameter set for List.
type ListSetsOptions struct {
	Page int
	Size int
}

// List returns sets with pagination. Returns the rows, the total
// count before pagination, and any error. Page/size are clamped
// to [1, 100] to match CardRepo.ListCardsOptions behaviour.
func (r *SetRepo) List(ctx context.Context, opts ListSetsOptions) ([]*SetRow, int, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("begin read tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var total int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(1) FROM sets`).Scan(&total); err != nil {
		return nil, 0, err
	}

	_, size, offset := normalizePagination(opts.Page, opts.Size)

	rows, err := tx.QueryContext(ctx,
		`SELECT set_id, card_count, payload FROM sets ORDER BY set_id LIMIT ? OFFSET ?`,
		size, offset,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []*SetRow
	for rows.Next() {
		out = append(out, &SetRow{})
		if err := rows.Scan(&out[len(out)-1].SetID, &out[len(out)-1].CardCount, &out[len(out)-1].Payload); err != nil {
			return nil, 0, err
		}
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	if err := rows.Close(); err != nil {
		return nil, 0, err
	}
	if err := tx.Commit(); err != nil {
		return nil, 0, fmt.Errorf("commit read tx: %w", err)
	}
	return out, total, nil
}
