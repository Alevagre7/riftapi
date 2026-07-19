package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/xalevagre7/riftapi/internal/domain"
)

// CardRow is the database-level representation of a Card. The full
// card JSON lives in Payload as raw bytes; the other fields
// are the denormalised columns that the API indexes on.
type CardRow struct {
	RiftboundID     string
	PublicCode      string // empty when the upstream doesn't provide one
	SetID           string
	CollectorNumber int
	Name            string
	CleanName       string // lowercased, punctuation-stripped; searched by /cards/name
	Payload         []byte
}

// CardRepo is the read/write surface for cards. All methods are safe
// for concurrent use; the underlying *sql.DB serialises writes.
type CardRepo struct{ db *sql.DB }

// NewCardRepo returns a CardRepo backed by db. The db is shared with
// the other repositories; there is no per-repo connection.
func NewCardRepo(db *sql.DB) *CardRepo { return &CardRepo{db: db} }

// upsertSQL is the single source of truth for the upsert statement.
// Used by both Upsert (one row) and SyncCards (in a transaction).
const upsertSQL = `
	INSERT INTO cards (riftbound_id, public_code, set_id, collector_number, name, clean_name, payload)
	VALUES (?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(riftbound_id) DO UPDATE SET
		public_code      = excluded.public_code,
		set_id           = excluded.set_id,
		collector_number = excluded.collector_number,
		name             = excluded.name,
		clean_name       = excluded.clean_name,
		payload          = excluded.payload,
		updated_at       = CURRENT_TIMESTAMP
`

// Upsert inserts the card or replaces the existing row with the same
// riftbound_id. The Payload must be valid JSON; this method does not
// validate it (the transformer is the only writer and produces valid
// JSON by construction).
func (r *CardRepo) Upsert(ctx context.Context, row CardRow) error {
	if _, err := r.db.ExecContext(ctx, upsertSQL, r.args(row)...); err != nil {
		return fmt.Errorf("upsert card %s: %w", row.RiftboundID, err)
	}
	return nil
}

// SyncCards replaces the entire card set in a single transaction: it
// upserts every row in `rows` and deletes any pre-existing card whose
// riftbound_id is not in the new set. The result is that the store
// always contains exactly the cards from the most recent successful
// sync — no stale cards accumulate. Returns nil on commit, an error
// if any step fails (the transaction is rolled back so the store
// remains untouched).
//
// If rows is empty, every card in the store is deleted.
func (r *CardRepo) SyncCards(ctx context.Context, rows []CardRow) error {
	if len(rows) == 0 {
		_, err := r.db.ExecContext(ctx, "DELETE FROM cards")
		return err
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	for _, row := range rows {
		if _, err := tx.ExecContext(ctx, upsertSQL, r.args(row)...); err != nil {
			return fmt.Errorf("upsert card %s: %w", row.RiftboundID, err)
		}
	}

	placeholders := make([]string, len(rows))
	args := make([]any, len(rows))
	for i, row := range rows {
		placeholders[i] = "?"
		args[i] = row.RiftboundID
	}
	del := "DELETE FROM cards WHERE riftbound_id NOT IN (" + strings.Join(placeholders, ",") + ")"
	if _, err := tx.ExecContext(ctx, del, args...); err != nil {
		return fmt.Errorf("delete stale cards: %w", err)
	}
	return tx.Commit()
}

// args converts a CardRow to the parameter slice for upsertSQL. The
// PublicCode column is nullable; everything else is non-null.
func (r *CardRepo) args(row CardRow) []any {
	var publicCode sql.NullString
	if row.PublicCode != "" {
		publicCode = sql.NullString{String: row.PublicCode, Valid: true}
	}
	return []any{
		row.RiftboundID, publicCode, row.SetID, row.CollectorNumber,
		row.Name, row.CleanName, row.Payload,
	}
}

// GetByRiftboundID returns the card with the given riftbound_id (e.g.
// "ogn-011") or sql.ErrNoRows if no such card exists. The match is
	// case-insensitive; the wire format is case-sensitive but
// the bot's existing adapter is liberal in this regard.
func (r *CardRepo) GetByRiftboundID(ctx context.Context, id string) (*CardRow, error) {
	const q = `
		SELECT riftbound_id, public_code, set_id, collector_number, name, clean_name, payload
		FROM cards WHERE riftbound_id = ? COLLATE NOCASE
	`
	return r.scanOne(ctx, q, id)
}

// GetByName returns the unique card whose name matches exactly
// (case-insensitive), or sql.ErrNoRows if none exists.
func (r *CardRepo) GetByName(ctx context.Context, name string) (*CardRow, error) {
	const q = `
		SELECT riftbound_id, public_code, set_id, collector_number, name, clean_name, payload
		FROM cards WHERE name = ? COLLATE NOCASE
	`
	return r.scanOne(ctx, q, name)
}

// SearchByName returns cards whose name OR clean_name contains the
// query as a substring (case-insensitive). Results are sorted by
// name. The limit caps the result set; 0 means "no limit".
func (r *CardRepo) SearchByName(ctx context.Context, query string, limit int) ([]*CardRow, error) {
	pattern := "%" + query + "%"
	q := `
		SELECT riftbound_id, public_code, set_id, collector_number, name, clean_name, payload
		FROM cards WHERE name LIKE ? COLLATE NOCASE OR clean_name LIKE ? COLLATE NOCASE
		ORDER BY name
	`
	var args []any
	if limit > 0 {
		q += " LIMIT ?"
		args = []any{pattern, pattern, limit}
	} else {
		args = []any{pattern, pattern}
	}
	return r.scanMany(ctx, q, args...)
}

// ListNames returns all card names sorted alphabetically. Used by the
// /index/card-names endpoint.
func (r *CardRepo) ListNames(ctx context.Context) ([]string, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT name FROM cards ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// Count returns the number of cards currently in the store. Used by
// the sync health check and the /health endpoint.
func (r *CardRepo) Count(ctx context.Context) (int, error) {
	var n int
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM cards`).Scan(&n)
	return n, err
}

// GetRandomCard returns one card chosen uniformly at random from the
// store, or sql.ErrNoRows if the store is empty. The implementation
// uses SQLite's ORDER BY RANDOM() LIMIT 1, which is fine for the
// local store (~1.2k rows) and avoids the alternative of a
// count-and-offset dance. Used by the /cards/random endpoint.
func (r *CardRepo) GetRandomCard(ctx context.Context) (*CardRow, error) {
	const q = `
		SELECT riftbound_id, public_code, set_id, collector_number, name, clean_name, payload
		FROM cards ORDER BY RANDOM() LIMIT 1
	`
	return r.scanOne(ctx, q)
}

// All returns every card in the store. Intended for tests and the
// scraper's full-replace mode; not for the API surface.
func (r *CardRepo) All(ctx context.Context) ([]*CardRow, error) {
	const q = `
		SELECT riftbound_id, public_code, set_id, collector_number, name, clean_name, payload
		FROM cards ORDER BY set_id, collector_number
	`
	return r.scanMany(ctx, q)
}

// --- list / search --------------------------------------------------------

// ListCardsOptions is the parameter set for List and Search. SetID
// filters to a single set (empty = no filter). Sort is one of
// "name" (default), "collector_number", or "set_id". Dir is 1 for
// ascending (default) or -1 for descending. Page is 1-based; size
// is the page size (clamped to [1, 100]).
type ListCardsOptions struct {
	SetID string
	Sort  string
	Dir   int
	Page  int
	Size  int
}

// List returns cards matching the filter (set_id, sort, page, size),
// paginated. Returns the rows, the total count before pagination,
// and any error. The total is the count of rows matching the
	// filter, matching the search-response total field.
func (r *CardRepo) List(ctx context.Context, opts ListCardsOptions) ([]*CardRow, int, error) {
	return r.queryCards(ctx, opts, "")
}

// SearchText returns cards whose text.plain contains the query as
// a substring (case-insensitive). Same filter, sort, and pagination
// options as List. The query is matched against the stored
// text.plain (the HTML-stripped body of the card's rules text).
func (r *CardRepo) SearchText(ctx context.Context, query string, opts ListCardsOptions) ([]*CardRow, int, error) {
	return r.queryCards(ctx, opts, query)
}

func (r *CardRepo) queryCards(ctx context.Context, opts ListCardsOptions, textQuery string) ([]*CardRow, int, error) {
	where := []string{}
	args := []any{}

	if opts.SetID != "" {
		where = append(where, "set_id = ? COLLATE NOCASE")
		args = append(args, opts.SetID)
	}
	if textQuery != "" {
		where = append(where, "json_extract(payload, '$.text.plain') LIKE ?")
		args = append(args, "%"+textQuery+"%")
	}

	whereClause := ""
	if len(where) > 0 {
		whereClause = " WHERE " + strings.Join(where, " AND ")
	}

	var total int
	countQuery := "SELECT COUNT(1) FROM cards" + whereClause
	if err := r.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	page := opts.Page
	if page < 1 {
		page = 1
	}
	size := opts.Size
	if size < 1 {
		size = 50
	}
	if size > 100 {
		size = 100
	}
	offset := (page - 1) * size

	sortColumn := sortColumnFor(opts.Sort)
	direction := "ASC"
	if opts.Dir == -1 {
		direction = "DESC"
	}

	query := fmt.Sprintf(
		"SELECT riftbound_id, public_code, set_id, collector_number, name, clean_name, payload FROM cards%s ORDER BY %s %s LIMIT ? OFFSET ?",
		whereClause, sortColumn, direction,
	)
	args = append(args, size, offset)

	rows, err := r.scanMany(ctx, query, args...)
	return rows, total, err
}

func sortColumnFor(s string) string {
	switch s {
	case "collector_number":
		return "collector_number"
	case "set_id":
		return "set_id"
	case "name", "":
		return "name"
	default:
		return "name"
	}
}

// --- distinct values for /index/* ---------------------------------------

// DistinctStringField returns the distinct non-null values of a
// JSON string field across all cards, sorted. Used by the
// /index/* endpoints for fields like type, rarity, artist.
func (r *CardRepo) DistinctStringField(ctx context.Context, path string) ([]string, error) {
	query := fmt.Sprintf(
		`SELECT DISTINCT json_extract(payload, '$.%s') AS v FROM cards
		 WHERE json_extract(payload, '$.%s') IS NOT NULL
		 ORDER BY 1`,
		path, path,
	)
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s sql.NullString
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		if s.Valid {
			out = append(out, s.String)
		}
	}
	return out, rows.Err()
}

// DistinctIntField returns the distinct integer values of a JSON
// field across all cards, sorted ascending. Used by /index/energy,
// /index/might, /index/power. Non-integer values (null, missing,
// or non-numeric JSON) are skipped.
func (r *CardRepo) DistinctIntField(ctx context.Context, path string) ([]int, error) {
	query := fmt.Sprintf(
		`SELECT DISTINCT json_extract(payload, '$.%s') AS v FROM cards
		 WHERE json_extract(payload, '$.%s') IS NOT NULL
		   AND json_type(json_extract(payload, '$.%s')) = 'integer'
		 ORDER BY 1`,
		path, path, path,
	)
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int
	for rows.Next() {
		var n sql.NullInt64
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		if n.Valid {
			out = append(out, int(n.Int64))
		}
	}
	return out, rows.Err()
}

// DistinctArrayValues returns the distinct values of a JSON array
// field, unnested. Used by /index/domains and /index/tags.
func (r *CardRepo) DistinctArrayValues(ctx context.Context, path string) ([]string, error) {
	query := fmt.Sprintf(
		`SELECT DISTINCT value FROM cards, json_each(json_extract(payload, '$.%s'))
		 WHERE json_extract(payload, '$.%s') IS NOT NULL
		 ORDER BY 1`,
		path, path,
	)
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s sql.NullString
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		if s.Valid {
			out = append(out, s.String)
		}
	}
	return out, rows.Err()
}

func (r *CardRepo) scanOne(ctx context.Context, q string, args ...any) (*CardRow, error) {
	row := r.db.QueryRowContext(ctx, q, args...)
	out, err := scanCardRow(row)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *CardRepo) scanMany(ctx context.Context, q string, args ...any) ([]*CardRow, error) {
	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*CardRow
	for rows.Next() {
		row, err := scanCardRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// rowScanner is satisfied by both *sql.Row and *sql.Rows, so the same
// scan helper works for Get* (single row) and Search*/All (many rows).
type rowScanner interface {
	Scan(dest ...any) error
}

func scanCardRow(s rowScanner) (*CardRow, error) {
	var (
		row        CardRow
		publicCode sql.NullString
	)
	if err := s.Scan(&row.RiftboundID, &publicCode, &row.SetID, &row.CollectorNumber, &row.Name, &row.CleanName, &row.Payload); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, sql.ErrNoRows
		}
		return nil, err
	}
	if publicCode.Valid {
		row.PublicCode = publicCode.String
	}
	return &row, nil
}

// EncodeCard serialises a domain.Card to the JSON blob stored in the
// payload column. The JSON is the card data wire format verbatim.
// Used by the scraper (Phase 2) before Upsert.
func EncodeCard(c *domain.Card) ([]byte, error) {
	return json.Marshal(c)
}
