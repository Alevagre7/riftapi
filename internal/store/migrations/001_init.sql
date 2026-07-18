-- 001_init.sql — initial schema for riftapi.
--
-- Three tables, all designed around the riftcodex wire format:
--   cards       — one row per Card. The full riftcodex Card JSON is
--                 stored in `payload`; the indexed columns (riftbound_id,
--                 name, set_id, collector_number) are denormalised for
--                 fast lookups by the API.
--   sets        — one row per Set. The full Set JSON is in `payload`;
--                 set_id and card_count are indexed.
--   sync_state  — exactly one row (id=1) holding the most recent sync's
--                 outcome. Read by the /health endpoint, written by the
--                 scraper.

CREATE TABLE IF NOT EXISTS cards (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    riftbound_id TEXT UNIQUE NOT NULL,
    public_code TEXT,
    set_id TEXT NOT NULL,
    collector_number INTEGER NOT NULL,
    name TEXT NOT NULL,
    clean_name TEXT NOT NULL DEFAULT '',
    payload JSON NOT NULL,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_cards_name       ON cards(name       COLLATE NOCASE);
CREATE INDEX IF NOT EXISTS idx_cards_clean_name ON cards(clean_name COLLATE NOCASE);
CREATE INDEX IF NOT EXISTS idx_cards_set        ON cards(set_id, collector_number);

CREATE TABLE IF NOT EXISTS sets (
    set_id TEXT PRIMARY KEY,
    card_count INTEGER NOT NULL,
    payload JSON NOT NULL
);

CREATE TABLE IF NOT EXISTS sync_state (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    last_sync_at TIMESTAMP,
    last_status TEXT,
    last_card_count INTEGER,
    last_build_id TEXT,
    last_error TEXT
);

-- Bootstrap the singleton sync_state row.
INSERT OR IGNORE INTO sync_state (id) VALUES (1);
