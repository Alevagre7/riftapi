# Implementation Plan

Phased build order for `riftapi`, the self-hosted read-only HTTP API that mirrors the [Riftcodex](https://riftcodex.com/docs/) JSON shape for the Riftbound TCG.

This plan is **dependency-ordered**: each phase produces something runnable and verifiable, and later phases assume the outputs of earlier ones. Strategic decisions from the grill are referenced inline by their ADR or CONTEXT.md entry; do not relitigate them during the build.

---

## How to read this

- Each phase has a **Goal**, a **What you build** checklist, and a **Verify** block. Don't move to the next phase until the current one passes verification.
- Tactical decisions left unresolved during the grill are flagged in [§ Tactical questions to settle during the build](#tactical-questions-to-settle-during-the-build). Resolve them at the start of the phase they first become relevant.
- Recommended working mode: **test-first for the transformer and the API contract**, after-the-fact for plumbing. See [§ Testing approach](#testing-approach).

---

## Phase 0 — Project skeleton

**Goal**: a Go module that builds a binary, runs in Docker on `linux/arm64`, and lives in a directory layout that supports the rest of the plan.

**What you build**:
- [ ] `go mod init github.com/<you>/riftapi`
- [ ] Directory layout:
  ```
  riftapi/
  ├── cmd/
  │   ├── riftapi/        # the API server entry point
  │   └── riftapi-sync/   # the scraper entry point (separate binary, separate concerns)
  ├── internal/
  │   ├── config/         # env + config-file loader
  │   ├── store/          # SQLite repository
  │   ├── scrape/         # upstream client + parser + transformer
  │   ├── api/            # HTTP handlers, routing, response shapes
  │   ├── health/         # health check + Telegram alert
  │   └── domain/         # Card, Set, Index types (the riftcodex shape)
  ├── testdata/
  │   └── gallery/        # saved copies of __NEXT_DATA__ HTML for offline tests
  ├── docs/               # CONTEXT.md, ADR, research (already populated)
  ├── Dockerfile          # multi-stage, scratch or distroless final image
  ├── docker-compose.yml  # api service + sync sidecar
  ├── riftapi.example.env # documented env vars
  └── Makefile            # build, test, lint, run
  ```
- [ ] Go version: 1.22+ (current stable). Pin in `go.mod` and `Dockerfile`.
- [ ] SQLite driver: `modernc.org/sqlite` (pure Go, no CGO, ARM cross-compile works without a cross toolchain).
- [ ] Lint: `golangci-lint` with default linters. Format: `gofumpt`.
- [ ] CI: optional. Skip for now; add a `make ci` target that runs `go test ./...`, `go vet ./...`, `golangci-lint run`.

**Verify**:
- `make build` produces two binaries.
- `docker build --platform linux/arm64 .` succeeds.
- `docker compose up api` starts a service that responds 200 on a placeholder `/healthz`.

**Implements decisions**: 3 (Go), 10 (server framework: stdlib `net/http`).

---

## Phase 1 — Data layer

**Goal**: a SQLite database that holds a Card Snapshot, the Sets it references, and the last sync state. Schema is migration-friendly.

**What you build**:
- [ ] `internal/store/migrations/` with forward-only SQL migrations. Use a tiny home-grown migrator (run on startup) or `golang-migrate/migrate` if you prefer the lib.
- [ ] Schema. The first migration:
  ```sql
  -- cards: the full riftcodex Card shape, JSON-encoded for flexibility
  CREATE TABLE cards (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    riftbound_id TEXT UNIQUE NOT NULL,         -- e.g. 'ogn-011' (the bare form)
    public_code TEXT,                          -- e.g. 'ogn-011-298' when available
    set_id TEXT NOT NULL,                      -- 'OGN', 'UNL', etc.
    collector_number INTEGER NOT NULL,
    payload JSON NOT NULL,                     -- the full riftcodex Card JSON
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
    last_status TEXT,                -- 'ok' | 'partial' | 'failed'
    last_card_count INTEGER,
    last_build_id TEXT,              -- from upstream (if discoverable)
    last_error TEXT
  );
  INSERT INTO sync_state (id) VALUES (1);
  ```
- [ ] **Storage decision**: store the *full* riftcodex Card as a JSON blob in `payload` plus a few denormalised columns (`riftbound_id`, `name`, `set_id`, `collector_number`) for indexing. JSON keeps the row ~1 KB and lets the API surface evolve without migrations.
- [ ] **WAL mode** on every connection: `PRAGMA journal_mode=WAL;` and `PRAGMA synchronous=NORMAL;`. Allows concurrent reads during sync writes.
- [ ] Repository interface in `internal/store/` (e.g. `CardRepo`, `SetRepo`, `SyncRepo`) with concrete SQLite impls.
- [ ] Connection management: one `*sql.DB` per process, opened on startup, never closed until shutdown.
- [ ] **Atomic snapshot swap** helper used by the scraper: write to a temp file (`riftapi.db.tmp`), `PRAGMA wal_checkpoint(TRUNCATE)`, then `os.Rename` over the real file. The API keeps reading the old file (via a file handle reopened on each request, or by short-lived connections) until the rename lands.

**Verify**:
- `go test ./internal/store/...` exercises insert/read/indexes.
- Manual: `sqlite3 riftapi.db "SELECT COUNT(*) FROM cards;"` returns 0, then a fixture insert returns the count.
- Schema is migration-clean: `migrate up` is idempotent; running twice does not error.

**Implements decisions**: 4 (SQLite), 7 (storage location for the snapshot, indirectly).

**Tactical question to settle here**:
- DB file location: `/var/lib/riftapi/riftapi.db` (Linux convention, requires root or a dedicated user) or a mounted volume in Docker (`/data/riftapi.db`). Default to `/data/riftapi.db`; let config override.

---

## Phase 2 — Scraper & sync

**Goal**: a `riftapi-sync` binary that pulls `__NEXT_DATA__` from `playriftbound.com/en-us/card-gallery/`, transforms each card into the riftcodex shape, and writes a fresh snapshot atomically.

**What you build**:
- [ ] `internal/scrape/client.go` — single `GET` to the gallery URL with a 30s timeout, 2 retries with exponential backoff, custom `User-Agent` identifying the project (e.g. `riftapi/0.1 (+https://github.com/<you>/riftapi)`). Respect a 1 req/sec rate limit even though the upstream has no documented limit.
- [ ] `internal/scrape/parse.go` — extract the `__NEXT_DATA__` JSON blob from the response HTML. Use a single regex anchored on `<script id="__NEXT_DATA__" type="application/json">...</script>`. Verify the report's path: `data["props"]["pageProps"]["page"]["blades"][2]["cards"]["items"]`. If that index changes, fail loud (see health check below).
- [ ] `internal/scrape/transform.go` — gallery card → riftcodex Card. Per [the research report](../research/playriftbound-card-gallery.md) §6, the recipe is:
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
- [ ] `internal/scrape/sync.go` — orchestrate: fetch → parse → transform → write to a temp DB → health check → atomic swap → update `sync_state`.
- [ ] **Health check at the end of sync** (this is half of decision 10):
  - Card count must be ≥ 1100 (well below the expected ~1178 but loud if upstream returns a near-empty response).
  - A known sample of card IDs must resolve: `ogn-011`, `unl-001`, `sfd-001`, `ven-001`. If any of these are missing, fail.
  - On failure, leave the previous snapshot untouched and update `sync_state` with `last_status = 'failed'`, `last_error` populated.
- [ ] `cmd/riftapi-sync/main.go` — entry point: load config, run `scrape.Sync(ctx)`, exit non-zero on failure.

**Verify**:
- `go test ./internal/scrape/...` with at least three fixtures:
  - The report's `__NEXT_DATA__` saved as `testdata/gallery/2026-07-19.html` (or `.json` — save whichever is smaller).
  - A gallery JSON containing at least one alternate art, one overnumbered, and one signature card.
  - A gallery JSON that is *intentionally missing* a required blade index — the parser must fail loud with a clear error.
- Manual end-to-end: run the binary against live upstream once, inspect the resulting `riftapi.db` with `sqlite3`, confirm a `ogn-011` row exists and its `payload` looks like the riftcodex shape.

**Implements decisions**: 1, 5, 6, 7, 8 (all the data-source stuff). ADR-0001.

**Tactical question to settle here**:
- Do you want the scraper to also write a *raw* copy of the parsed JSON to `testdata/gallery/` after every run? Useful for debugging when upstream changes. Default: yes, gated by a `--archive` flag so it doesn't fill the disk in production.

---

## Phase 3 — `/health` + Telegram alert

**Goal**: the API exposes a `/health` endpoint that returns the last sync's status, and the sync job sends a Telegram message to the maintainer when a sync fails.

**What you build**:
- [ ] `internal/health/check.go` — read `sync_state`, return `{status, last_sync_at, last_card_count, last_error}`.
- [ ] `internal/api/health.go` — `GET /health` handler, returns 200 if `last_status == 'ok'`, 503 otherwise.
- [ ] `internal/health/alert.go` — Telegram alert sender. Uses `TELEGRAM_BOT_TOKEN` + `ADMIN_CHAT_ID` env vars to call `https://api.telegram.org/bot<token>/sendMessage`. One-line message: `"riftapi sync failed: <error>"`. No retry — if Telegram is down, the next sync will alert again.
- [ ] Wire into `cmd/riftapi-sync/main.go`: after a failed health check, call the alert sender.
- [ ] **Critical**: the alert sender is the *only* code path that uses `TELEGRAM_BOT_TOKEN`. The token is read in the sync binary, never the API binary. This keeps the read-only API free of write-capable secrets.
- [ ] Config: `TELEGRAM_ALERTS_ENABLED` (default `true`). If false, the alert is a no-op even with the token set. Useful for local dev.

**Verify**:
- `go test ./internal/health/...` with a mocked Telegram client (or a fake HTTP server that records the POST).
- Manual: delete a required card from a test DB, run sync, confirm the Telegram message arrives. Then point the API at that DB, hit `/health`, confirm 503.

**Implements decisions**: 10 (failure handling).

**Tactical questions to settle here**:
- Reuse the existing `riftbot` token + a chat ID you control, or create a new "riftapi-notifier" bot? **Default: reuse the riftbot token** — one less bot to manage, the maintainer already has a chat with it.
- Where do the chat ID and token live? **Default: env vars on the host that runs the sync job**, *not* in the API container. Means the API container has no Telegram-related env at all.

---

## Phase 4 — MVP API surface (bot-critical)

**Goal**: four endpoints live, returning the riftcodex shape, served by a Go stdlib `net/http` binary in a Docker container on the Pi.

**What you build**:
- [ ] `internal/api/server.go` — `http.ServeMux` with a tiny path-param helper (regex capture, ~20 lines). Don't add a router dependency.
- [ ] `internal/api/cards.go`, `internal/api/index.go`, etc. — handlers, one per endpoint.
- [ ] Handlers (only the 4 the bot uses today, per [PRODUCT_DESCRIPTION.md](../../riftbot/PRODUCT_DESCRIPTION.md)):
  - `GET /cards/name?fuzzy=<query>` — case-insensitive `LIKE` on `name` and `clean_name`. Returns an array of Cards (riftcodex returns an array for the single-name match too; respect the shape).
  - `GET /cards/{id}` — lookup by riftcodex `id` (UUID) **or** `riftbound_id` (the bot's adapter uses the riftcodex UUID, but be liberal — the bot's `card-repository.ts` calls `getCardById(id)` which is the UUID path; the riftbound_id path is separate). Match the riftcodex shape: return one Card or 404.
  - `GET /cards/riftbound/{id}` — case-insensitive lookup by `riftbound_id` (e.g. `ogn-011`). May return an array (alternate arts share a base id with a letter suffix).
  - `GET /index/card-names` — `SELECT name FROM cards ORDER BY name`. Returns `{total, type: "card-names", values: [...]}`, matching the riftcodex shape.
- [ ] Response shape: hand-write the JSON marshaller. The `payload` JSON blob on each row already matches riftcodex; the handlers just unmarshal and re-marshal with proper field ordering. No need for `encoding/json` struct tags acrobatics — `json.RawMessage` round-trips the blob.
- [ ] Error responses: `{ "error": "<code>", "message": "<human>" }` with 400/404/500/503. Match the riftcodex shape if it has one; check by curling.
- [ ] CORS: open by default (this is a hobby tool, the bot is the only consumer, but allowing browser access doesn't hurt).
- [ ] Server config: `PORT` (default `8080`), `BIND` (default `0.0.0.0`), `DATABASE_PATH` (default `/data/riftapi.db`).
- [ ] **Legal Jibber Jabber attribution** (decision 9): add the statement to a `GET /` handler that returns `{name: "riftapi", version: "...", upstream: "playriftbound.com", attribution: "This project was created under Riot Games' 'Legal Jibber Jabber' policy using assets owned by Riot Games. Riot Games does not endorse or sponsor this project."}`. Document in the README too.
- [ ] `cmd/riftapi/main.go` — entry point: load config, open DB, mount handlers, `http.ListenAndServe`.
- [ ] `Dockerfile` for the API: multi-stage `golang:1.22` → `gcr.io/distroless/static:nonroot` (or `scratch`), final image ~10 MB, runs as UID 65532.
- [ ] `docker-compose.yml`: `api` service on a named volume for `/data`. Read `DATABASE_PATH` from env.

**Verify**:
- `go test ./internal/api/...` with table-driven cases per endpoint: hit each handler with a known fixture DB, assert response shape and status codes.
- **Contract test**: start the API pointing at a fixture DB, `curl` each of the 4 endpoints, and assert the JSON shape matches a snapshot from the live `api.riftcodex.com` for the same query. (Catches drift in your response shape, which is exactly what would break the bot.)
- Manual: `docker compose up`, `curl localhost:8080/cards/riftbound/ogn-011`, confirm a card-shaped JSON response.

**Implements decisions**: 2 (full riftcodex mirror — but only 4 endpoints live), 3, 9 (attribution).

---

## Phase 5 — Full riftcodex mirror

**Goal**: every endpoint the riftcodex docs describe is implemented and behaves identically for the bot's use cases.

**What you build**:
- [ ] The remaining endpoints (in order of how the riftcodex API organises them):
  - [ ] `GET /cards?sort=&dir=&set_id=&page=&size=` — paginated list with sort.
  - [ ] `GET /cards/search?query=...` — full-text search on rules text. SQLite FTS5 virtual table indexed on `text.plain` (or the raw `payload`).
  - [ ] `GET /cards/riftbound/{id}` — already in MVP; verify pagination/array behaviour matches riftcodex.
  - [ ] `GET /cards/tcgplayer/{id}` — always returns 404 (gallery has no `tcgplayer_id`). Document this.
  - [ ] `GET /sets` — `SELECT * FROM sets`.
  - [ ] `GET /sets/{id}` — by riftcodex set UUID.
  - [ ] `GET /sets/set-id/{set_id}` — by `set_id` (e.g. `ogn`).
  - [ ] `GET /sets/tcgplayer/{id}`, `GET /sets/cardmarket/{id}` — always 404, same reason.
  - [ ] `GET /index/keywords`, `/types`, `/supertypes`, `/domains`, `/rarities`, `/artists`, `/energy`, `/might`, `/power`, `/tags` — `SELECT DISTINCT` on the relevant field, return the same `{total, type, values}` shape.
- [ ] Pagination: match riftcodex defaults (`page` default 1, `size` default 50, max 100). Return `{items, total, page, size, pages}` for paginated endpoints.

**Verify**:
- Same contract test as Phase 4, but now covering *every* endpoint against the live riftcodex for at least one query each.
- Bot smoke test: temporarily point `riftbot`'s `RIFTCODEX_BASE_URL` at the local API, run the test checklist from [PRODUCT_DESCRIPTION.md](../../riftbot/PRODUCT_DESCRIPTION.md) §"Testing Checklist", confirm every test passes.

**Implements decisions**: 2 (full mirror complete).

---

## Phase 6 — Bot cutover

**Goal**: the bot talks to the local API; the public `api.riftcodex.com` is no longer in the critical path.

**What you build** (in `../riftbot`):
- [ ] Set `RIFTCODEX_BASE_URL` to the new API's URL (e.g. `https://riftapi.lan`).
- [ ] No code changes: the bot's `RiftcodexAdapter` Zod schemas already validate the response shape. If anything in the local API's shape drifts, the bot will fail loud at parse time — exactly what you want.
- [ ] Run the test checklist from PRODUCT_DESCRIPTION.md end-to-end.
- [ ] Monitor for 48 hours; if the bot behaves identically, declare cutover complete.
- [ ] Optional follow-up: keep the riftcodex adapter as a *fallback* if the local API returns 5xx. This is a small change to `RiftcodexAdapter` and reintroduces a runtime upstream dependency — make it opt-in via env var, default off.

**Verify**:
- Bot behaves identically on the local API vs the live `api.riftcodex.com` for the test checklist.
- `/health` returns 200; alerts work; one full spoiler-season sync run.

**Implements decisions**: 12 (bot integration, finally).

---

## Phase 7 — Ops & hardening

**Goal**: the thing runs unattended for a year.

**What you build**:
- [ ] `systemd` unit files (host or sidecar container — your call at deploy time):
  - `riftapi-sync.timer` — `OnCalendar=*-*-* 03:00:00`, `Persistent=true`, `Wants=riftapi-sync.service`.
  - `riftapi-sync.service` — `Type=oneshot`, runs `riftapi-sync`, logs to journald.
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
- Manually disable/enable the timer with `systemctl --user stop riftapi-sync.timer` and confirm the next scheduled run is skipped.
- Replay a saved `testdata/gallery/` HTML through the sync binary end-to-end and confirm the resulting DB matches what the live scrape produces.
- Disaster drill: drop the DB, restore from the latest backup, confirm the API serves the restored data.

**Implements decisions**: 7, 8, 10 (all in production).

---

## Tactical questions to settle during the build

These are decisions that were intentionally deferred during the strategic grill because they only matter once you start writing code.

| Phase | Question | Recommended default |
|---|---|---|
| 1 | DB file location | `/data/riftapi.db` (mounted Docker volume) |
| 2 | Archive raw upstream JSON to `testdata/`? | Yes, gated by `--archive` flag |
| 3 | Reuse riftbot token vs new notifier bot? | Reuse the riftbot token; admin chat ID is a new env var |
| 3 | Where do Telegram env vars live? | On the host running the sync job, not in the API container |
| 4 | `text.flavour` heuristic? | No — leave as `null` (ADR-0001) |
| 7 | Timer on host vs in a sidecar container? | Sidecar — keeps the API and sync in one `docker compose` |
| 7 | TLS: Caddy vs direct? | Caddy — auto-renews, trivial config |
| 7 | Backup retention? | 7 days, then prune |

If a default doesn't fit when you get there, change it; update this table or the relevant ADR.

---

## Testing approach

**Test-first** for:
- `internal/scrape/transform.go` — the gallery → riftcodex transform. Many edge cases (alternate arts, overnumbered, missing fields, superType arrays of varying length, empty `tags`).
- `internal/api/*` — the contract tests. The response shape is the integration boundary with the bot; one silent field rename breaks the bot at runtime.

**After-the-fact** for:
- DB schema and migrations (trivially correct; test the migrations runner for idempotence).
- HTTP server plumbing.
- Config loader.

**Required fixtures** (in `testdata/gallery/`):
- A full gallery HTML/JSON with the expected ~1,178 cards. Snapshot once during development; re-snapshot when upstream meaningfully changes.
- A reduced gallery with one of each edge case: alternate art, overnumbered, signature, missing superType, empty tags, missing `text`, missing image.

**Contract tests** (in `internal/api/contract_test.go`):
- A test that runs the local API against a fixture DB and compares each endpoint's response to a snapshot from the live `api.riftcodex.com`. Skip on CI by default; run locally before each release.
- A test that runs the upstream *riftcodex* against the same query, compares the local API's response, fails on any field-level diff.

---

## Verification gates between phases

| After phase | Must pass before next |
|---|---|
| 0 | `make build` and `docker build` succeed |
| 1 | `go test ./internal/store/...` green; manual sqlite3 check |
| 2 | `go test ./internal/scrape/...` green with all fixtures; live scrape produces a DB with `ogn-011` |
| 3 | Telegram alert test green; `/health` returns 200 on a clean DB, 503 on a stale one |
| 4 | All 4 bot-critical endpoints respond with the riftcodex shape; contract test green |
| 5 | All riftcodex endpoints respond; full contract test suite green |
| 6 | Bot test checklist passes for 48 hours |
| 7 | Runbook is written and exercised once |
