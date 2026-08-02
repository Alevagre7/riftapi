# Implementation Plan

Phased build order for `riftapi`, the self-hosted read-only HTTP API that serves structured card data for the Riftbound TCG.

This plan is **dependency-ordered**: each phase produces something runnable and verifiable, and later phases assume the outputs of earlier ones. Strategic decisions from the grill are referenced inline by their ADR or CONTEXT.md entry; do not relitigate them during the build.

> Historical note: the core phases and the repository's operational baseline
> are implemented, but this document is retained as design history. The
> unchecked bullets include optional or externally-owned follow-ups. Current
> operational instructions are in `README.md`, `deploy/README.md`, and
> `docs/RUNBOOK.md`.

---

## How to read this

- Each phase has a **Goal**, a **What you build** checklist, and a **Verify** block. Don't move to the next phase until the current one passes verification.
- Tactical decisions left unresolved during the grill are flagged in [§ Tactical questions to settle during the build](#tactical-questions-to-settle-during-the-build). Resolve them at the start of the phase they first become relevant.
- Recommended working mode: **test-first for the transformer and the API contract**, after-the-fact for plumbing. See [§ Testing approach](#testing-approach).

---

## Phase 0 — Project skeleton

**Goal**: a Go module that builds two self-contained static binaries (one for the API, one for the scraper) and lives in a directory layout that supports the rest of the plan. The binaries run anywhere Go 1.25+ can target — bare metal, containers, VMs, or your platform of choice. The root `Dockerfile` and `deploy/` directory are starting points for common deployments.

**What you build**:
- [ ] `go mod init github.com/<you>/riftapi`
- [ ] Directory layout:
  ```
  riftapi/
  ├── cmd/
  │   ├── riftapi/        # the API server entry point
  │   └── riftapi-scraper/ # the scraper entry point (separate binary, separate concerns)
  ├── internal/
  │   ├── config/         # env + config-file loader
  │   ├── store/          # SQLite repository
  │   ├── scrape/         # upstream client + parser + transformer
  │   ├── api/            # HTTP handlers, routing, response shapes
  │   ├── health/         # health check + Telegram alert
  │   └── domain/         # Card, Set, Index types (the card data shape)
  ├── testdata/
  │   └── gallery/        # saved copies of __NEXT_DATA__ HTML for offline tests
  ├── docs/               # CONTEXT.md, ADR, research (already populated)
  ├── Dockerfile          # api and scraper image targets
  ├── riftapi.example.env # documented env vars
  └── Makefile            # build, test, lint, run
  ```
- [ ] Go version: 1.25+. Pin in `go.mod` and `Dockerfile`.
- [ ] SQLite driver: `modernc.org/sqlite` (pure Go, no CGO, ARM cross-compile works without a cross toolchain).
- [ ] Lint: `golangci-lint` with default linters. Format: `gofumpt`.
- [ ] CI: optional. Skip for now; add a `make ci` target that runs `go test ./...`, `go vet ./...`, `golangci-lint run`.

**Verify**:
- `make build` produces two binaries.
- `make build GOOS=linux GOARCH=arm64` (or any other target) cross-compiles cleanly.
- `docker build .` (or `docker build --platform <arch> .`) succeeds.
- `docker build --target api .` produces the long-running API image.

**Implements decisions**: 3 (Go), 10 (server framework: stdlib `net/http`).

---

## Phase 1 — Data layer

**Goal**: a SQLite database that holds a Card Snapshot, the Sets it references, and the last sync state. Schema is migration-friendly.

**What you build**:
- [ ] `internal/store/migrations/` with forward-only SQL migrations. Use a tiny home-grown migrator (run on startup) or `golang-migrate/migrate` if you prefer the lib.
- [ ] Schema. The first migration:
  ```sql
  -- cards: the full card JSON, encoded for flexibility
  CREATE TABLE cards (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    riftbound_id TEXT UNIQUE NOT NULL,         -- e.g. 'ogn-011' (the bare form)
    public_code TEXT,                          -- e.g. 'ogn-011-298' when available
    set_id TEXT NOT NULL,                      -- 'OGN', 'UNL', etc.
    collector_number INTEGER NOT NULL,
    payload JSON NOT NULL,                     -- the full card JSON
    name TEXT NOT NULL,                        -- denormalised for sort/search
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
  );
  CREATE INDEX idx_cards_name ON cards(name);
  CREATE INDEX idx_cards_set  ON cards(set_id, collector_number);

  -- sets
  CREATE TABLE sets (
    set_id TEXT PRIMARY KEY,
    payload JSON NOT NULL,
    card_count INTEGER NOT NULL
  );

  -- sync state (one row, holds the latest snapshot metadata)
  CREATE TABLE sync_state (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    last_sync_at TIMESTAMP,
    last_status TEXT,                -- 'ok' | 'failed'
    last_card_count INTEGER,
    last_build_id TEXT,              -- from upstream (if discoverable)
    last_error TEXT
  );
  INSERT INTO sync_state (id) VALUES (1);
  ```
- [ ] **Storage decision**: store the *full* card data as a JSON blob in `payload` plus a few denormalised columns (`riftbound_id`, `name`, `set_id`, `collector_number`) for indexing. JSON keeps the row ~1 KB and lets the API surface evolve without migrations.
- [ ] **WAL mode** on every connection: `PRAGMA journal_mode=WAL;` and `PRAGMA synchronous=NORMAL;`. Allows concurrent reads during sync writes.
- [ ] Repository interface in `internal/store/` (e.g. `CardRepo`, `SetRepo`, `SyncRepo`) with concrete SQLite impls.
- [ ] Connection management: one `*sql.DB` per process, opened on startup, never closed until shutdown.
- [ ] **Atomic snapshot** helper used by the scraper: replace cards, sets,
  and the successful sync state in one SQLite transaction. Failed
  validation leaves the previous snapshot untouched.

**Verify**:
- `go test ./internal/store/...` exercises insert/read/indexes.
- Manual: `sqlite3 riftapi.db "SELECT COUNT(*) FROM cards;"` returns 0, then a fixture insert returns the count.
- Schema is migration-clean: `migrate up` is idempotent; running twice does not error.

**Implements decisions**: 4 (SQLite), 7 (storage location for the snapshot, indirectly).

**Tactical question to settle here**:
- DB file location: configurable via `RIFTAPI_DATABASE_PATH` env var, default `/data/riftapi.db` (a path that works equally well as a Docker volume mount target or a host path like `/var/lib/riftapi/riftapi.db` on a bare-metal install). Pick a path that fits your environment; the only requirement is that the process can read and write the file.

---

## Phase 2 — Scraper & sync

**Goal**: a `riftapi-scraper` binary that pulls `__NEXT_DATA__` from `playriftbound.com/en-us/card-gallery/`, transforms each card into the card data shape, and writes a fresh snapshot atomically.

**What you build**:
- [ ] `internal/scrape/client.go` — single `GET` to the gallery URL with a 30s timeout, 2 retries with exponential backoff, custom `User-Agent` identifying the project (e.g. `riftapi/0.1 (+https://github.com/<you>/riftapi)`). Respect a 1 req/sec rate limit even though the upstream has no documented limit.
- [ ] `internal/scrape/parse.go` — extract the `__NEXT_DATA__` JSON blob from the response HTML. Use a single regex anchored on `<script id="__NEXT_DATA__" type="application/json">...</script>`. Verify the report's path: `data["props"]["pageProps"]["page"]["blades"][2]["cards"]["items"]`. If that index changes, fail loud (see health check below).
- [ ] `internal/scrape/transform.go` — gallery card → card data shape. Per [the research report](../research/playriftbound-card-gallery.md) §6, the recipe is:
  - `riftbound_id` ← strip the trailing `/{total}` from `publicCode` (e.g. `ogn-011-298` → `ogn-011`).
  - `attributes.{energy,might,power}` ← parse the integer out of the gallery's `value.id` (which is a string).
  - `classification.type` ← `cardType.type[0].label`.
  - `classification.supertype` ← `cardType.superType[0].label` if present, else `null`.
  - `classification.rarity` ← `rarity.value.label`.
  - `classification.domain` ← `[d.label for d in domain.values]`.
  - `text.rich` ← `text.richText.body` (already HTML).
  - `text.plain` ← strip HTML tags from `text.rich`. (Don't try to separate flavour; set `text.flavour` to `null` — see ADR-0001.)
  - `set.set_id`, `set.label` ← `set.value.id`, `set.value.label`.
  - `media.image_url` ← `cardImage.url`.
  - `media.artist` ← `illustrator.values[0].label`.
  - `media.accessibility_text` ← `cardImage.accessibilityText`.
  - `tags` ← `tags.tags` if present, else `[]`.
  - `orientation` ← `cardImage.dimensions` decides `portrait` vs `landscape`; the gallery exposes it directly, use that.
  - `metadata.alternate_art` ← `bool`: the collector-number portion of the riftbound_id ends with a letter (`ogn-066a`).
  - `metadata.overnumbered` ← `bool`: `collectorNumber > set.collectorNumberMax`.
  - `metadata.signature` ← `bool`: any `cardType.superType` has `id == "signature"`.
  - `metadata.clean_name` ← lowercased, punctuation-stripped `name`.
  - `metadata.updated_on` ← `null` (not available from upstream).
  - `tcgplayer_id` ← `null`.
- [ ] `internal/scrape/sync.go` — orchestrate: fetch → parse → transform → validate → atomically replace the snapshot and update `sync_state`.
- [ ] **Health check at the end of sync** (this is half of decision 10):
  - Card count must be ≥ 1100 (well below the expected ~1178 but loud if upstream returns a near-empty response).
  - A known sample of card IDs must resolve: `ogn-011`, `unl-001`, `sfd-001`, `ven-001`. If any of these are missing, fail.
  - On failure, leave the previous snapshot untouched and update `sync_state` with `last_status = 'failed'`, `last_error` populated.
- [ ] `cmd/riftapi-scraper/main.go` — entry point: load config, run the sync, exit non-zero on failure.

**Verify**:
- `go test ./internal/scrape/...` with at least three fixtures:
  - The report's `__NEXT_DATA__` saved as `testdata/gallery/2026-07-19.html` (or `.json` — save whichever is smaller).
  - A gallery JSON containing at least one alternate art, one overnumbered, and one signature card.
  - A gallery JSON that is *intentionally missing* a required blade index — the parser must fail loud with a clear error.
- Manual end-to-end: run the binary against live upstream once, inspect the resulting `riftapi.db` with `sqlite3`, confirm a `ogn-011` row exists and its `payload` looks like the card data shape.

**Implements decisions**: 1, 5, 6, 7, 8 (all the data-source stuff). ADR-0001.

**Tactical question to settle here**:
- Do you want the scraper to also write a *raw* copy of the parsed JSON to `testdata/gallery/` after every run? Useful for debugging when upstream changes. Default: yes, gated by a `--archive` flag so it doesn't fill the disk in production.

---

## Phase 3 — `/health` + Telegram alert

**Goal**: the API exposes a `/health` endpoint that returns the last sync's status, and the sync job sends a Telegram message to the maintainer when a sync fails.

**What you build**:
- [ ] `internal/health/check.go` — hold shared sync-health decisions.
- [ ] `internal/api/health.go` — `GET /health` handler with `ok`/`degraded` (200) and failed/unreachable (`503`) states.
- [ ] `internal/health/alert.go` — Telegram alert sender. Uses `TELEGRAM_BOT_TOKEN` + `ADMIN_CHAT_ID` env vars to call `https://api.telegram.org/bot<token>/sendMessage`. One-line message: `"riftapi sync failed: <error>"`. No retry — if Telegram is down, the next sync will alert again.
- [ ] Wire into `cmd/riftapi-scraper/main.go`: after a failed sync, call the alert sender.
- [ ] **Critical**: the alert sender is the *only* code path that uses `TELEGRAM_BOT_TOKEN`. The token is read in the sync binary, never the API binary. This keeps the read-only API free of write-capable secrets.
- [ ] Config: `TELEGRAM_ALERTS_ENABLED` (default `true`). If false, the alert is a no-op even with the token set. Useful for local dev.

**Verify**:
- `go test ./internal/health/...` with a mocked Telegram client (or a fake HTTP server that records the POST).
- Manual: delete a required card from a test DB, run sync, confirm the Telegram message arrives. Then point the API at that DB, hit `/health`, confirm 503.

**Implements decisions**: 10 (failure handling).

**Tactical questions to settle here**:
- Reuse an existing Telegram bot token (if you have one for the destination) or create a dedicated notifier bot? **Default: dedicated bot** — the maintainer already has the chat with it.
- Where do the chat ID and token live? **Default: env vars on the host that runs the sync job**, *not* in the API container. Means the API container has no Telegram-related env at all.

---

## Phase 4 — MVP API surface (bot-critical)

**Goal**: four endpoints live, returning the card data shape, served by a Go stdlib `net/http` binary on any host.

**What you build**:
- [ ] `internal/api/server.go` — `http.ServeMux` with a tiny path-param helper (regex capture, ~20 lines). Don't add a router dependency.
- [ ] `internal/api/cards.go`, `internal/api/index.go`, etc. — handlers, one per endpoint.
- [ ] Handlers (only the 4 most-used endpoints to start; expand the surface in Phase 5):
- [ ] `GET /cards/name?fuzzy=<query>` — case-insensitive `LIKE` on `name` and `clean_name`. Returns an array of cards in the search-response shape.
- [ ] `GET /cards/{id}` — lookup by `riftbound_id` (e.g. `ogn-011`). Returns one card or 404.
- [ ] `GET /cards/riftbound/{id}` — lookup by the local `riftbound_id`, preserving the array response shape.
- [ ] `GET /index/card-names` — `SELECT DISTINCT name FROM cards ORDER BY name`. Returns `{total, type: "card-names", values: [...]}`.
- [ ] Response shape: hand-write the JSON marshaller. The `payload` JSON blob on each row already encodes the card data; the handlers just unmarshal and re-marshal with proper field ordering. No need for `encoding/json` struct tags acrobatics — `json.RawMessage` round-trips the blob.
- [ ] Error responses: `{ "error": "<code>", "message": "<human>" }` with 400/404/500/503. Keep the shape stable so consumers can parse it uniformly.
- [ ] CORS: open by default (this is a hobby tool, the bot is the only consumer, but allowing browser access doesn't hurt).
- [ ] Server config: `PORT` (default `8080`), `BIND` (default `0.0.0.0`), `DATABASE_PATH` (default `/data/riftapi.db`).
- [ ] **Legal Jibber Jabber attribution** (decision 9): add the statement to a `GET /` handler that returns `{name: "riftapi", version: "...", upstream: "playriftbound.com", attribution: "This project was created under Riot Games' 'Legal Jibber Jabber' policy using assets owned by Riot Games. Riot Games does not endorse or sponsor this project."}`. Document in the README too.
- [ ] `cmd/riftapi/main.go` — entry point: load config, open DB, mount handlers, `http.ListenAndServe`.
- [ ] `Dockerfile` for the API: multi-stage `golang:1.25` → `gcr.io/distroless/static:nonroot` (or `scratch`), final image ~10 MB, runs as UID 65532.
- [ ] Docker image targets: `api` for the long-running server and `sync` for the one-shot scraper.

**Verify**:
- `go test ./internal/api/...` with table-driven cases per endpoint: hit each handler with a known fixture DB, assert response shape and status codes.
- **Contract test**: start the API pointing at a fixture DB, `curl` each of the 4 endpoints, and assert the JSON shape matches a known-good reference for the same query. (Catches drift in your response shape — the most likely source of subtle consumer breakage.)
- Manual: run the `api` image, then `curl localhost:8080/cards/riftbound/ogn-011`, and confirm a card-shaped JSON response.

**Implements decisions**: 2 (card data surface — but only 4 endpoints live), 3, 9 (attribution).

---

## Phase 5 — Full card data surface

**Goal**: every endpoint a consumer might reasonably need is implemented.

**What you build**:
- [ ] The remaining endpoints (in order of typical use):
  - [ ] `GET /cards?sort=&dir=&set_id=&page=&size=` — paginated list with sort.
  - [ ] `GET /cards/search?query=...` — full-text search on rules text. SQLite FTS5 virtual table indexed on `text.plain` (or the raw `payload`).
  - [ ] `GET /cards/riftbound/{id}` — already in MVP; verify pagination/array behaviour.
  - [ ] `GET /cards/tcgplayer/{id}` — always returns 404 (gallery has no `tcgplayer_id`). Document this.
  - [ ] `GET /sets` — `SELECT * FROM sets`.
  - [ ] `GET /sets/{id}` — by opaque internal ID.
  - [ ] `GET /sets/set-id/{set_id}` — by `set_id` (e.g. `ogn`).
  - [ ] `GET /sets/tcgplayer/{id}`, `GET /sets/cardmarket/{id}` — always 404, same reason.
  - [ ] `GET /index/keywords`, `/types`, `/supertypes`, `/domains`, `/rarities`, `/artists`, `/energy`, `/might`, `/power`, `/tags` — `SELECT DISTINCT` on the relevant field, return the same `{total, type, values}` shape.
- [ ] Pagination: `page` default 1, `size` default 50, max 100. Return `{items, total, page, size, pages}` for paginated endpoints.

**Verify**:
- Same contract test as Phase 4, but now covering *every* endpoint against a known-good reference for at least one query each.
- Consumer smoke test: point at least one real consumer at the local API and run its standard test checklist. Confirm every test passes.

**Implements decisions**: 2 (full mirror complete).

---


## Phase 6 — Consumer validation (external)

**Goal**: validate the API against the bot or other consumer that will use
the deployment.

This phase is intentionally outside this repository: no consumer project is
included here. The API contract tests in `internal/api` cover the local
surface; the remaining work is to point the real consumer at a deployed API
and run its own command checklist.

---

## Phase 7 — Ops & hardening

**Goal**: the thing runs unattended for a year.

**What you build**:
- [ ] `systemd` unit files (host or sidecar container — your call at deploy time):
  - `riftapi-scraper.timer` — `OnCalendar=*-*-* 03:00:00`, `Persistent=true`, starts `riftapi-scraper.service`.
  - `riftapi-scraper.service` — `Type=oneshot`, runs `riftapi-scraper`, logs to journald.
  - `riftapi.service` — long-running, `Restart=always`, the API server.
- [ ] Config file: `/etc/riftapi/config.yaml` with the schema:
  ```yaml
  sync:
    enabled: false   # flip to true during spoiler season
  database:
    path: /data/riftapi.db
  api:
    bind: 0.0.0.0
    port: 8080
  alerts:
    telegram:
      enabled: true
      bot_token_env: TELEGRAM_BOT_TOKEN
      admin_chat_id_env: TELEGRAM_ADMIN_CHAT_ID
  ```
- [ ] TLS termination: Caddy on the Pi reverse-proxying to the API container. Caddy handles the Let's Encrypt dance if you have a public domain, or self-signed for `riftapi.lan`.
- [ ] Backup: cron-driven `sqlite3 riftapi.db ".backup /backups/riftapi-$(date +%F).db"` to a mounted volume. 7 days retention, then prune.
- [ ] Log rotation: journald defaults are fine; add a `SystemMaxUse=200M` in `/etc/systemd/journald.conf` if you care.
- [ ] Disk monitoring: a tiny cron that alerts if `/data` is < 1 GB free (the SQLite file is ~30 MB but you'll want headroom).
- [ ] A `docs/RUNBOOK.md` documenting: how to flip the sync toggle, how to read the last sync status, how to re-run a failed sync, how to restore from a backup, how to rotate the Telegram token.

**Verify**:
- Manually disable/enable the timer with `systemctl stop riftapi-scraper.timer` and confirm the next scheduled run is skipped.
- Replay a saved `testdata/gallery/` HTML through the sync binary end-to-end and confirm the resulting DB matches what the live scrape produces.
- Disaster drill: drop the DB, restore from the latest backup, confirm the API serves the restored data.

**Implements decisions**: 7, 8, 10 (all in production).

---

## Tactical questions to settle during the build

These are decisions that were intentionally deferred during the strategic grill because they only matter once you start writing code.

| Phase | Question | Recommended default |
|---|---|---|
| 1 | DB file location | `/data/riftapi.db` (a mounted volume or host path) |
| 2 | Archive raw upstream JSON to `testdata/`? | Yes, gated by `--archive` flag |
| 3 | Reuse an existing Telegram bot token vs new notifier bot? | Dedicated notifier bot; admin chat ID is a new env var |
| 3 | Where do Telegram env vars live? | On the host running the sync job, not in the API container |
| 4 | `text.flavour` heuristic? | No — leave as `null` (ADR-0001) |
| 7 | Timer on host vs in a sidecar container? | Host systemd timer running the scraper binary |
| 7 | TLS: Caddy vs direct? | Caddy — auto-renews, trivial config |
| 7 | Backup retention? | 7 days, then prune |

If a default doesn't fit when you get there, change it; update this table or the relevant ADR.

---

## Testing approach

**Test-first** for:
- `internal/scrape/transform.go` — the gallery → card-data transform. Many edge cases (alternate arts, overnumbered, missing fields, superType arrays of varying length, empty `tags`).
- `internal/api/*` — the contract tests. The response shape is the integration boundary with the bot; one silent field rename breaks the bot at runtime.

**After-the-fact** for:
- DB schema and migrations (trivially correct; test the migrations runner for idempotence).
- HTTP server plumbing.
- Config loader.

**Required fixtures** (in `testdata/gallery/`):
- A full gallery HTML/JSON with the expected ~1,178 cards. Snapshot once during development; re-snapshot when upstream meaningfully changes.
- A reduced gallery with one of each edge case: alternate art, overnumbered, signature, missing superType, empty tags, missing `text`, missing image.

**Contract tests** (in `internal/api/contract_test.go`):
- A test that runs the local API against a fixture DB and compares each endpoint's response to a known-good reference. Skip on CI by default; run locally before each release.
- A test that runs the upstream *playriftbound* against the same query, compares the local API's response, fails on any field-level diff.

---

## Verification gates between phases

| After phase | Must pass before next |
|---|---|
| 0 | `make build` and `docker build` succeed |
| 1 | `go test ./internal/store/...` green; manual sqlite3 check |
| 2 | `go test ./internal/scrape/...` green with all fixtures; live scrape produces a DB with `ogn-011` |
| 3 | Telegram alert test green; `/health` returns 200 on a clean DB, 503 on a stale one |
| 4 | All 4 most-used endpoints respond with the card data shape; contract test green |
| 5 | All endpoints respond; full contract test suite green |
| 6 | Bot test checklist passes for 48 hours |
| 7 | Runbook is written and exercised once |
