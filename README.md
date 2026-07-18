# riftapi

Self-hosted read-only HTTP API that mirrors the [Riftcodex](https://riftcodex.com/docs/) JSON shape for the Riftbound TCG. Runs on a Raspberry Pi 3B inside Docker. Data is populated by scraping `playriftbound.com` and stored in a local SQLite database. The API serves requests without calling any upstream at request time.

This project was created under Riot Games' "Legal Jibber Jabber" policy using assets owned by Riot Games. Riot Games does not endorse or sponsor this project.

## What is this?

riftapi is a drop-in replacement for `https://api.riftcodex.com` that you host yourself. It is consumed by [riftbot](../riftbot), a Telegram bot that uses the Riftcodex API to look up cards. By self-hosting, the bot no longer depends on a third-party uptime and the maintainer owns the data lineage (within the constraints noted below — see [ADR-0001](docs/adr/0001-scrape-playriftbound.md)).

The API is read-only. The sync binary is the only writer to the local SQLite file; it runs from a systemd timer at 03:00 every night (or on demand) and is gated by the `RIFTAPI_SYNC_ENABLED` env var so it only runs during Spoiler Season.

## Endpoints

| Method | Path | Notes |
|---|---|---|
| GET | `/` | API info, Legal Jibber Jabber attribution. |
| GET | `/health` | 200 if the last sync succeeded, 503 otherwise. |
| GET | `/cards` | Paginated, sortable (`name`, `collector_number`, `set_id`), filterable by `set_id`. |
| GET | `/cards/{id}` | Lookup by `riftbound_id` (e.g. `ogn-011`). The riftcodex `{id}` is a UUID we don't have; this 404s for UUIDs. |
| GET | `/cards/name?fuzzy=X` / `?exact=X` | Name search. |
| GET | `/cards/riftbound/{id}` | Lookup by `riftbound_id`, returns an array. |
| GET | `/cards/search?query=X` | Full-text search on `text.plain` (case-insensitive substring; FTS5 is a future optimisation). |
| GET | `/cards/tcgplayer/{id}` | Always 404 — the gallery does not expose `tcgplayer_id` (ADR-0001). |
| GET | `/sets` | Paginated. |
| GET | `/sets/{id}` | Always 404 — no set UUIDs. |
| GET | `/sets/set-id/{set_id}` | Lookup by upstream set code (e.g. `ogn`). |
| GET | `/sets/tcgplayer/{id}` / `/sets/cardmarket/{id}` | Always 404. |
| GET | `/index/card-names` | All card names, sorted. |
| GET | `/index/{types,supertypes,rarities,artists,energy,might,power}` | Distinct values of the field across all cards. |
| GET | `/index/{domains,tags}` | Distinct values from JSON array fields. |
| GET | `/index/keywords` | Not implemented (would require parsing `[Keyword]` tokens out of card text). |

All responses are the riftcodex wire format verbatim. CORS is open by default. See [docs/CUTOVER.md](docs/CUTOVER.md) for what the bot calls, what 404s, and the verification checklist.

## What's in the box

- `cmd/riftapi` — the read-only HTTP API server.
- `cmd/riftapi-sync` — the scraper; pulls `__NEXT_DATA__` from `playriftbound.com/en-us/card-gallery/`, transforms it to the riftcodex shape, and replaces the SQLite snapshot in a single transaction.
- `internal/api` — HTTP handlers, one per riftcodex endpoint. CORS middleware.
- `internal/scrape` — upstream HTTP client (with retry/backoff), `__NEXT_DATA__` parser, gallery → riftcodex transformer (TDD), syncer.
- `internal/store` — SQLite repository (WAL mode, transactional `SyncCards`).
- `internal/health` — sync state, `/health` endpoint, Telegram alert sender.
- `internal/domain` — Card, Set, Index types (the riftcodex shape).
- `internal/config` — env-var loader.
- `deploy/systemd/` — `riftapi-sync.service` and `riftapi-sync.timer` units for nightly syncs.
- `deploy/caddy/Caddyfile` — reverse proxy in front of the API on the host.
- `deploy/backup/backup.sh` — daily online backup with 7-day rolling retention.
- `docs/` — `CONTEXT.md` (glossary), `RUNBOOK.md` (operations), `CUTOVER.md` (bot cutover), `IMPLEMENTATION_PLAN.md`, ADRs, upstream research.

## Quick start (local dev)

```bash
# 1. Install Go 1.22+ if you don't have it.
# 2. Build both binaries.
make build
# 3. Configure. Copy the example and edit.
cp riftapi.example.env .env
$EDITOR .env
# 4. One-shot sync against live upstream.
RIFTAPI_DATABASE_PATH=./data/riftapi.db ./bin/riftapi-sync
# 5. Run the API.
RIFTAPI_DATABASE_PATH=./data/riftapi.db ./bin/riftapi
# 6. Hit it.
curl localhost:8080/cards/riftbound/ogn-011 | jq .
curl localhost:8080/health | jq .
```

## Docker (Pi 3B target)

```bash
docker compose up -d api          # the API, with restart: unless-stopped
docker compose run --rm sync     # one-off sync (driven by the systemd timer in production)
```

The nightly timer is the host-level unit in `deploy/systemd/riftapi-sync.timer` (see [docs/RUNBOOK.md](docs/RUNBOOK.md) for the install + flip-the-toggle walkthrough).

## Tests

```bash
make test                        # full suite, all 4 packages
go test -race -count=1 ./internal/store/...     # data layer
go test -race -count=1 ./internal/scrape/...    # scraper + transformer (TDD)
go test -race -count=1 ./internal/health/...    # alert sender
go test -race -count=1 ./internal/api/...       # HTTP endpoints
```

## Documentation

- [CONTEXT.md](CONTEXT.md) — domain glossary (Riftbound, Card, Set, Spoiler Season, …).
- [docs/adr/](docs/adr/) — architectural decision records.
- [docs/research/playriftbound-card-gallery.md](docs/research/playriftbound-card-gallery.md) — structural report on the upstream.
- [docs/IMPLEMENTATION_PLAN.md](docs/IMPLEMENTATION_PLAN.md) — phased build plan, verification gates.
- [docs/RUNBOOK.md](docs/RUNBOOK.md) — daily operations, failure modes, recovery.
- [docs/CUTOVER.md](docs/CUTOVER.md) — how to point the riftbot at the local API.

## Status

All seven phases of [the implementation plan](docs/IMPLEMENTATION_PLAN.md) are complete. The MVP (the four bot-critical endpoints), the full riftcodex mirror, and the ops surface (systemd + Caddy + backup + runbook) are in. The only remaining step is a config change in the sibling [riftbot](../riftbot) repo to point `RIFTCODEX_BASE_URL` at the local API; see [docs/CUTOVER.md](docs/CUTOVER.md) for the walkthrough.
