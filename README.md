# riftapi

Self-hosted read-only HTTP API that mirrors the [Riftcodex](https://riftcodex.com/docs/) JSON shape for the Riftbound TCG. Runs on a Raspberry Pi 3B inside Docker. Data is populated by scraping `playriftbound.com` and stored in a local SQLite database. The API serves requests without calling any upstream at request time.

This project was created under Riot Games' "Legal Jibber Jabber" policy using assets owned by Riot Games. Riot Games does not endorse or sponsor this project.

## What is this?

riftapi is a drop-in replacement for `https://api.riftcodex.com` that you host yourself. It is consumed by [riftbot](../riftbot), a Telegram bot that uses the Riftcodex API to look up cards. By self-hosting, the bot no longer depends on a third-party uptime and the maintainer owns the data lineage (within the constraints noted below — see [ADR-0001](docs/adr/0001-scrape-playriftbound.md)).

## What's in the box

- `cmd/riftapi` — the read-only HTTP API server.
- `cmd/riftapi-sync` — the scraper; pulls `__NEXT_DATA__` from `playriftbound.com/en-us/card-gallery/`, transforms it to the riftcodex shape, and writes a fresh SQLite snapshot.
- `internal/api` — HTTP handlers, one per riftcodex endpoint.
- `internal/scrape` — upstream client, parser, and gallery → riftcodex transformer.
- `internal/store` — SQLite repository (WAL mode, atomic snapshot swap).
- `internal/health` — sync state, `/health` endpoint, Telegram alert sender.
- `internal/domain` — Card, Set, Index types (the riftcodex shape).
- `internal/config` — env + config-file loader.

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
```

## Docker (Pi 3B target)

```bash
docker compose up -d api
docker compose run --rm sync       # one-off sync
```

The nightly timer is a host-level systemd unit (see [docs/RUNBOOK.md](docs/RUNBOOK.md) once Phase 7 is complete). It defaults to off and is flipped on during Spoiler Season.

## Documentation

- [CONTEXT.md](CONTEXT.md) — domain glossary (Riftbound, Card, Set, Spoiler Season, …).
- [docs/adr/](docs/adr/) — architectural decision records.
- [docs/research/playriftbound-card-gallery.md](docs/research/playriftbound-card-gallery.md) — structural report on the upstream.
- [docs/IMPLEMENTATION_PLAN.md](docs/IMPLEMENTATION_PLAN.md) — phased build order, verification gates.

## Status

Currently in development. The project layout and design are finalised; the build progresses through [the implementation plan](docs/IMPLEMENTATION_PLAN.md).
