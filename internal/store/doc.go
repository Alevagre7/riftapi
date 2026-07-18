// Package store is the SQLite repository layer. It owns the database
// connection, schema migrations, and the typed repositories that the
// scraper and the API read and write through.
//
// The connection uses WAL mode and a single *sql.DB per process. The
// scraper replaces the card set in a single transaction (see
// CardRepo.SyncCards) so the store is never in a partial state.
// See docs/IMPLEMENTATION_PLAN.md §1 for the full design.
package store
