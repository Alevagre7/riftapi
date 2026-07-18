# riftapi Runbook

Operational reference for the self-hosted riftapi. Assumes the
deployment described in [`deploy/`](../deploy/) (systemd timer +
docker compose + Caddy reverse proxy + nightly backup).

## Topology

```
┌────────────────────────┐    ┌────────────────────────┐    ┌────────────────────────┐
│  playriftbound.com     │    │  Pi 3B                 │    │  riftbot (sibling)     │
│  (the upstream)         │ <─ │  docker compose:       │ <─ │  Telegram bot           │
│  HTTPS GET              │    │   ├─ riftapi (API)     │    │                        │
└────────────────────────┘    │   └─ riftapi-sync       │    └────────────────────────┘
                             │     (one-shot, via timer)│
                             │  Caddy (reverse proxy)  │
                             │  cron (backup)          │
                             └────────────────────────┘
```

The API process runs as a long-lived Docker container with
`restart: unless-stopped`. The sync is a one-shot container run by
the systemd timer at 03:00 every night. Backups run from cron at
04:00, an hour after the sync.

## Where things live

| Thing | Path |
|---|---|
| Repo (this directory) | `/opt/riftapi` |
| SQLite database | `/data/riftapi.db` (named volume `riftapi-data`) |
| Backups | `/var/backups/riftapi/` |
| systemd units | `/etc/systemd/system/riftapi-sync.{service,timer}` |
| Sync env vars | `/etc/riftapi/riftapi.env` |
| Caddy config | `/etc/caddy/Caddyfile` (or wherever your distro puts it) |
| API logs | `journalctl -u riftapi` (compose's logs) |
| Sync logs | `journalctl -u riftapi-sync.service` |
| Backup logs | `/var/log/backup.log` (set up in cron) |

## Flipping the sync toggle

The sync is gated by `RIFTAPI_SYNC_ENABLED=false` by default, so
accidental timer firings outside Spoiler Season are a no-op. To
turn the sync on for a Spoiler Season window:

```bash
# Edit the env file
sudo $EDITOR /etc/riftapi/riftapi.env
# Change: RIFTAPI_SYNC_ENABLED=false → RIFTAPI_SYNC_ENABLED=true

# Trigger one sync immediately (don't wait for 03:00)
sudo systemctl start riftapi-sync.service

# Check that it succeeded
journalctl -u riftapi-sync.service -n 50
# Or, after it returns:
curl -s http://localhost:8080/health
# → 200 means healthy. 503 means the last sync failed.
```

To turn the sync off again after the release:

```bash
# Edit the env file
sudo $EDITOR /etc/riftapi/riftapi.env
# Change: RIFTAPI_SYNC_ENABLED=true → RIFTAPI_SYNC_ENABLED=false
```

The systemd timer keeps running on the schedule; the env flag is
what stops it from doing anything. The `Persistent=true` setting on
the timer means a missed sync (e.g. the Pi was off at 03:00) will
be retried on the next boot — but only if `RIFTAPI_SYNC_ENABLED=true`.

## Reading the last sync status

The `/health` endpoint reports the last sync's outcome. Run from
any host on the LAN:

```bash
curl -s http://riftapi.lan/health | jq .
```

```json
{
  "status": "ok",
  "last_sync_at": "2026-07-19T03:00:42Z",
  "last_card_count": 1178,
  "last_status": "ok",
  "last_error": ""
}
```

`status` is `ok` (HTTP 200) or `unhealthy` (HTTP 503). `unhealthy`
covers both "the sync never ran" and "the last sync failed"; the
two are distinguished by `last_status` (empty means never ran,
`failed` means the syncer returned an error).

For a quick post-mortem, the same fields are in the
`sync_state` table:

```bash
sqlite3 /data/riftapi.db "SELECT last_sync_at, last_status, last_card_count, substr(last_error, 1, 80) FROM sync_state"
```

## Re-running a failed sync

A failed sync is usually upstream (the gallery page changed, the
bot-protection kicked in, or the JSON shape drifted). The first
step is to read the last error and the recent log:

```bash
journalctl -u riftapi-sync.service -n 100
sqlite3 /data/riftapi.db "SELECT last_error FROM sync_state"
```

If the upstream is the problem, wait a few hours and trigger a
retry manually:

```bash
sudo systemctl start riftapi-sync.service
```

If you suspect a local problem (wrong config, broken DB), inspect
the current snapshot before re-syncing:

```bash
# How many cards do we have?
sqlite3 /data/riftapi.db "SELECT COUNT(*) FROM cards"
# What sets?
sqlite3 /data/riftapi.db "SELECT set_id, card_count FROM sets ORDER BY set_id"
# Is the syncer's parser finding the upstream's HTML?
#   (Re-run the scraper with verbose logging.)
docker compose run --rm sync
```

A fresh `riftapi-sync` run is transactional: it upserts all cards
from the latest gallery parse and deletes anything that isn't in
the new set. There's no partial-failure mode — either the new
snapshot lands, or the old one stays untouched.

## Restoring from a backup

If the SQLite file is corrupted or you want to roll back to a
specific date, stop the API, replace the file, and restart.

```bash
# 1. Find the backup you want.
ls -lh /var/backups/riftapi/

# 2. Stop the API container. (The sync timer is safe — it would
#    just fail fast because the DB is being replaced.)
sudo docker compose stop api

# 3. Replace the database file.
sudo cp /var/backups/riftapi/riftapi-2026-07-19.db /data/riftapi.db
sudo chown 999:999 /data/riftapi.db   # the UID/GID the container runs as

# 4. Restart the API. The new file is picked up on the next open.
sudo docker compose start api
curl -s http://localhost:8080/health
```

If the backup is older than the last successful sync, the next
scheduled sync will *overwrite* the restored data. To prevent that,
turn off the sync until you've decided:

```bash
sudo $EDITOR /etc/riftapi/riftapi.env
# RIFTAPI_SYNC_ENABLED=false
```

## Rotating the Telegram bot token

The bot token lives in `/etc/riftapi/riftapi.env` (the env var
`RIFTAPI_TELEGRAM_BOT_TOKEN`). To rotate:

```bash
# 1. Revoke the old token in BotFather.
# 2. Get the new token from BotFather.
# 3. Update the env file.
sudo $EDITOR /etc/riftapi/riftapi.env
# 4. The next sync run picks up the new token. There is no running
#    process to restart — the sync binary reads the env at start.
```

The chat id (`RIFTAPI_TELEGRAM_ADMIN_CHAT_ID`) does not need to
change unless the admin's chat id changed (e.g. the admin deleted
their account and got a new one).

## Common failure modes

### Sync returns 503 on /health

`last_status` is `failed`; `last_error` has the upstream's
response or the parse error. Most common causes:

- **Upstream change**: the gallery page structure moved. The
  scraper's `TransformCard` log line names the offending card
  index. Fix the parser (in `internal/scrape/transform.go` or
  `parse.go`), redeploy, and re-run.
- **Upstream unavailable**: transient. Wait and let the next
  timer fire.
- **MinCount check failed**: the syncer parsed fewer cards than
  `RIFTAPI_SYNC_MIN_CARD_COUNT` (default 1100). This is a strong
  signal the upstream changed. Check the log for the actual count
  parsed and the per-card transform warnings.

### /health returns 200 but the bot can't find a card

The card was probably removed upstream. Confirm with:

```bash
sqlite3 /data/riftapi.db "SELECT riftbound_id, name FROM cards WHERE riftbound_id = 'ogn-123'"
# → empty: the card is not in the local store.
```

The riftcodex `id` field is a UUID. The bot's `getCardById` calls
`/cards/{id}` with a UUID, which 404s. The bot's
`getCardByRiftboundId` calls `/cards/riftbound/{id}` with a code
like `ogn-011`, which works. This is a known limitation documented
in IMPLEMENTATION_PLAN.md §6; the fix lives in the bot, not here.

### /cards/search returns nothing for a query that should match

The search uses SQLite `LIKE` on the `text.plain` column, with
the `%query%` pattern. The query is case-insensitive. If the
upstream card's text is `<p>Counter a spell.</p>`, searching for
`counter` matches; searching for `counter a` matches; searching
for `Counter,<br/>a` does not (the comma is there, but `<br/>` is
not part of text.plain). If a query is unexpectedly missing, check
the stored value:

```bash
sqlite3 /data/riftapi.db \
    "SELECT json_extract(payload, '\$.text.plain') FROM cards WHERE riftbound_id = 'ogn-011'"
```

If you want richer search (tokenization, ranking, multi-word
AND/OR), the next step is upgrading to SQLite FTS5 — out of scope
for the MVP.

### Disk full

The SQLite file is ~30 MB and the backup files are ~30 MB each.
On a Pi with a 16 GB SD card, you'll have years of runway. If the
disk does fill up:

1. The backup script will start failing. Check `/var/log/backup.log`.
2. The sync will start failing (SQLite can't write). The Telegram
   alert will fire.

Free up space by lowering the backup retention (`RETENTION_DAYS=3`
in the cron job) or by archiving old backups off-device.

### After a Pi reboot, the timer doesn't fire

The systemd timer has `Persistent=true`, so a missed sync is
retried on next boot — but only if `RIFTAPI_SYNC_ENABLED=true`.
Check both:

```bash
systemctl list-timers riftapi-sync
# → Next: the upcoming 03:00; Last: the last successful or failed fire.
sudo -u riftapi bash -c 'echo "RIFTAPI_SYNC_ENABLED=$RIFTAPI_SYNC_ENABLED"'
# → The env file value, after `EnvironmentFile=` expansion.
```

If the timer is "loaded but inactive", re-enable it:

```bash
sudo systemctl enable --now riftapi-sync.timer
```

## Maintenance cadence

| When | What |
|---|---|
| Daily (03:00) | Timer fires; sync runs (or no-ops if `RIFTAPI_SYNC_ENABLED=false`). |
| Daily (04:00) | Cron runs `backup.sh`. |
| Weekly | Skim `journalctl -u riftapi-sync --since "1 week ago"` for warnings. |
| Per Spoiler Season | Flip `RIFTAPI_SYNC_ENABLED=true` ~1 day before the first reveal; flip back to `false` the day after the set's full release. |
| Per Caddy/Raspbian upgrade | Re-read the Caddyfile; Caddy reloads itself automatically. |
| Per upstream gallery change | Inspect the syncer log, fix the parser, redeploy. |
