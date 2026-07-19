# riftapi

Self-hosted read-only HTTP API that serves structured card data for the Riftbound TCG. Data is populated by scraping `playriftbound.com` and stored in a local SQLite database. The API serves requests without calling any upstream at request time.

This project was created under Riot Games' "Legal Jibber Jabber" policy using assets owned by Riot Games. Riot Games does not endorse or sponsor this project.

## What is this?

riftapi is a self-hosted API for Riftbound card data. You run the two binaries on a host of your choice, and they expose a JSON API you can query from any HTTP client — a Telegram bot, a web app, a CLI, a static site, an internal tool. By self-hosting, the data lives on infrastructure you control and the runtime request path never depends on a third-party service (the upstream is only consulted at sync time, which you control).

The API is read-only. The sync binary is the only writer to the local SQLite file; it runs from any scheduler you choose (cron, systemd timer, Kubernetes CronJob, GitHub Actions schedule, …) and is gated by the `RIFTAPI_SYNC_ENABLED` env var so it only runs when you want it to.

The JSON shape is the natural shape of the data on `playriftbound.com/en-us/card-gallery/` — one record per card, with classification, attributes, text, media, and set metadata. The shape is stable across syncs (the upstream is HTML rendered from a structured `__NEXT_DATA__` blob), so consumers can rely on it.

## Endpoints

| Method | Path | Returns |
|---|---|---|
| GET | `/` | API info, Legal Jibber Jabber attribution. |
| GET | `/health` | 200 if the last sync succeeded, 503 otherwise. |
| GET | `/cards` | Paginated, sortable (`name`, `collector_number`, `set_id`), filterable by `set_id`. |
| GET | `/cards/{id}` | Lookup by `riftbound_id` (e.g. `ogn-011`). |
| GET | `/cards/name?fuzzy=X` / `?exact=X` | Name search. |
| GET | `/cards/riftbound/{id}` | Lookup by `riftbound_id`, returns an array. |
| GET | `/cards/search?query=X` | Full-text search on `text.plain` (case-insensitive substring; FTS5 is a future optimisation). |
| GET | `/cards/tcgplayer/{id}` | Always 404 — external-service IDs are not present in the upstream data (see ADR-0001). |
| GET | `/sets` | Paginated. |
| GET | `/sets/{id}` | Always 404 — the project does not generate opaque internal IDs for sets. |
| GET | `/sets/set-id/{set_id}` | Lookup by upstream set code (e.g. `ogn`). |
| GET | `/sets/tcgplayer/{id}` / `/sets/cardmarket/{id}` | Always 404. |
| GET | `/index/card-names` | All card names, sorted. |
| GET | `/index/{types,supertypes,rarities,artists,energy,might,power}` | Distinct values of the field across all cards. |
| GET | `/index/{domains,tags}` | Distinct values from JSON array fields. |
| GET | `/index/keywords` | Not implemented (would require parsing `[Keyword]` tokens out of card text). |

CORS is open by default.

## What's in the box

- `cmd/riftapi` — the read-only HTTP API server.
- `cmd/riftapi-sync` — the scraper; pulls `__NEXT_DATA__` from `playriftbound.com/en-us/card-gallery/`, transforms it into the card data shape, and replaces the SQLite snapshot in a single transaction.
- `internal/api` — HTTP handlers, one per endpoint. CORS middleware.
- `internal/scrape` — upstream HTTP client (with retry/backoff), `__NEXT_DATA__` parser, gallery → card-data transformer (TDD), syncer.
- `internal/store` — SQLite repository (WAL mode, transactional `SyncCards`).
- `internal/health` — sync state, `/health` endpoint, Telegram alert sender.
- `internal/domain` — Card, Set, Index types.
- `internal/config` — env-var loader.
- `deploy/` — example ops files: systemd units, a Caddyfile, a backup script. **These are starting points**, not the only way to deploy. See [deploy/README.md](deploy/README.md).

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

## Running

The two binaries are self-contained static Go programs. They can run
anywhere that can reach `playriftbound.com` (for the sync) and
listen on a TCP port (for the API). Some options:

- **Direct on a host** — run the two binaries under your init
  system of choice (systemd, runit, s6, launchd, …). The example
  units in `deploy/systemd/` are one such configuration.
- **Docker / docker compose** — `docker compose up api` for the
  long-running service, `docker compose run --rm sync` for a
  one-shot sync. The included `docker-compose.yml` is a starting
  point.
- **Kubernetes** — the API is a stateless Deployment; the sync is
  a CronJob that runs `riftapi-sync` against a PersistentVolume
  for the SQLite file.
- **A VM, a serverless function, your laptop, anywhere** — the
  binaries don't care.

See [docs/RUNBOOK.md](docs/RUNBOOK.md) for the operations side
(reading /health, re-running a failed sync, restoring from
backup, common failure modes).

## Configuration

All runtime configuration is via environment variables — see
[riftapi.example.env](riftapi.example.env) for the full list with
defaults. The key ones:

| Variable | Purpose |
|---|---|
| `RIFTAPI_DATABASE_PATH` | Path to the SQLite file. Defaults to `/data/riftapi.db` (the in-container path in `docker-compose.yml`). |
| `RIFTAPI_API_BIND` / `RIFTAPI_API_PORT` | Where the API listens. Defaults to `0.0.0.0:8080`. |
| `RIFTAPI_SYNC_ENABLED` | Master switch for the sync. Default off. Flip to `true` for the duration of a Spoiler Season. |
| `RIFTAPI_SCRAPE_USER_AGENT` | User-Agent header for the upstream HTTP request. Include contact info per upstream norms. |
| `RIFTAPI_TELEGRAM_*` | Alert destination (bot token, chat id, on/off switch). Optional; sync runs fine without alerts. |

## Tests

```bash
make test                              # full suite, all 4 packages
go test -race -count=1 ./internal/store/...    # data layer
go test -race -count=1 ./internal/scrape/...   # scraper + transformer (TDD)
go test -race -count=1 ./internal/health/...   # alert sender
go test -race -count=1 ./internal/api/...      # HTTP endpoints
```

## Documentation

- [CONTEXT.md](CONTEXT.md) — domain glossary (Riftbound, Card, Set, Spoiler Season, …).
- [docs/adr/](docs/adr/) — architectural decision records.
- [docs/research/playriftbound-card-gallery.md](docs/research/playriftbound-card-gallery.md) — structural report on the upstream.
- [docs/IMPLEMENTATION_PLAN.md](docs/IMPLEMENTATION_PLAN.md) — phased build plan, verification gates.
- [docs/RUNBOOK.md](docs/RUNBOOK.md) — daily operations, failure modes, recovery.
- [deploy/README.md](deploy/README.md) — how to use the example ops files (systemd, Caddy, backup).

## Status

All seven phases of [the implementation plan](docs/IMPLEMENTATION_PLAN.md) are complete. The MVP, the full API surface, and the operational docs are in. The API is ready to serve; point your HTTP client at it.
