# Bot cutover guide

This document is for the riftbot maintainer (or anyone flipping
the bot from `https://api.riftcodex.com` to the local riftapi).
It assumes the local riftapi is already running on the host the
bot can reach (typically the same Pi, on `http://riftapi.lan` or
`http://<pi-lan-ip>:8080`).

## TL;DR

1. Stop the bot.
2. Change the env var `RIFTCODEX_BASE_URL` in the bot's
   environment to point at the local API
   (e.g. `http://riftapi.lan:8080`).
3. Restart the bot.
4. Run the test checklist below.
5. Monitor for 48 hours.

No bot code changes are required. The bot's `RiftcodexAdapter`
Zod schemas (in `src/infrastructure/apis/riftcodex.adapter.ts`)
already validate the response shape; if the local API ever
drifts from the riftcodex wire format, the bot will fail loud at
parse time, which is exactly what you want.

## What the bot calls, and what the local API returns

| Bot method | Bot's HTTP call | Local riftapi endpoint | Notes |
|---|---|---|---|
| `getCardByRiftboundId(riftboundId)` | `GET /cards/riftbound/{id}` | `GET /cards/riftbound/{id}` | Works. Returns an array; the adapter takes `[0]`. |
| `getCardById(id)` | `GET /cards/{id}` | `GET /cards/{id}` | ⚠️ **Will 404** — see below. |
| `getCardByName(name)` (via `searchCards`) | `GET /cards/name?fuzzy=...` | `GET /cards/name?fuzzy=...` | Works. The local API also accepts `?exact=`. |
| `getRandomCard()` | `GET /index/card-names` then local pick | `GET /index/card-names` | Works. Returns `{total, type, values: [...]}`; the adapter picks one. |

### The UUID caveat

The bot's `getCardById(id)` sends a riftcodex **UUID** in `{id}`.
The local riftapi has no UUIDs (the upstream gallery exposes only
`riftbound_id` codes like `ogn-011`). `GET /cards/{id}` matches
on `riftbound_id`, so any UUID lookup returns 404.

**This is fine in practice.** The bot's `searchCards` (which
returns cards with both `id` and `riftbound_id`) and
`getCardByRiftboundId` are the two paths that reach for a specific
card from a user command. `getCardById` is the *callback* path —
it's called when the user taps a result button in the inline
keyboard — and the bot can be updated to call
`getCardByRiftboundId` instead by reading the inline-result
payload's `riftbound_id` field (which is already there in the
riftcodex response).

If you want to avoid changing the bot, see the "Fallback" section
below — keeping the riftcodex adapter as a fallback for any
`getCardById` call lets the bot keep working with no code change.

## Env var changes

The bot reads `RIFTCODEX_BASE_URL` from its env (see the bot's
`config.ts` and `.env.example`). Change it from
`https://api.riftcodex.com` to your local API URL:

```bash
# .env
RIFTCODEX_BASE_URL=http://riftapi.lan:8080
```

If your bot is also running in Docker, you can either:
- Set the env var in the bot's `docker-compose.yml`, or
- Add `riftapi` to the bot's `docker-compose.yml` network and use
  `http://riftapi:8080` (the Docker network alias).

## Verification checklist

From `riftbot/PRODUCT_DESCRIPTION.md`:

- [ ] `/card Flameblade` — single match shows image + name
- [ ] `/card ahri` — multiple matches show buttons
- [ ] `/card ogn-011` — ID lookup shows image + name
- [ ] `/card nonexistent` — shows "No card found"
- [ ] `/random` — returns a valid card
- [ ] `/events` — shows upcoming events near Seville *(unaffected — uses a different adapter)*

The last item is unaffected because the events adapter goes to
`api.cloudflare.riftbound.uvsgames.com`, not riftcodex.

Quick smoke tests against the local API before flipping the bot:

```bash
# 1. /health → 200 once a sync has succeeded
curl -sS http://riftapi.lan:8080/health | jq .

# 2. /cards/riftbound/ogn-011 → a real card
curl -sS http://riftapi.lan:8080/cards/riftbound/ogn-011 | jq '.name, .riftbound_id, .set.set_id'

# 3. /cards/name?fuzzy=ahri → array of matching cards (or empty)
curl -sS 'http://riftapi.lan:8080/cards/name?fuzzy=ahri' | jq '.total, .items | length'

# 4. /index/card-names → array of names
curl -sS http://riftapi.lan:8080/index/card-names | jq '.total, .values | length'
```

All four should return 200 with the expected shape.

## The `/cards/{id}` UUID paths

If the bot relies on `getCardById` for any user-facing path (not
just the callback), the cleanest fix is in the bot, not here:

1. In `riftcodex.adapter.ts`, change the `getCardById` method to
   call `getCardByRiftboundId` when the id doesn't look like a
   UUID (or simply route all such calls through the riftbound
   endpoint).
2. Or: have the bot's inline-result buttons encode the
   `riftbound_id` (not the UUID) and call the riftbound endpoint
   directly.

If you'd rather not change the bot, the fallback strategy below
keeps the riftcodex adapter alive as a secondary source for the
UUID path only.

## Fallback: keep the riftcodex adapter as a safety net

The plan recommends (optionally) keeping the live riftcodex
adapter as a fallback for any 5xx from the local API. This is a
small, opt-in change to the bot:

```ts
// riftcodex.adapter.ts (sketch)
async getCardById(id: string): Promise<Card | null> {
  try {
    return await this.localApi.getCardById(id);
  } catch (e) {
    if (e instanceof HTTPError && e.status >= 500) {
      // Local API is down; fall back to the live riftcodex.
      return await this.liveApi.getCardById(id);
    }
    throw e;
  }
}
```

Default the env var that enables the fallback to `false` so the
default behaviour matches the original "no third-party
dependency" goal. The local API is the source of truth; the
fallback is a "don't break the bot during an outage" guard.

## Rollback

If the cutover reveals a problem, flip `RIFTCODEX_BASE_URL` back
to `https://api.riftcodex.com` and restart the bot. The local
riftapi can stay running in the background — the next time the
bot is pointed back at it, no data migration is needed (the
local store is just a snapshot of the upstream's data).

## What the local API does *not* have

A handful of riftcodex endpoints the bot does not currently call
return 404 against the local API. The list is fixed and
documented (see `docs/IMPLEMENTATION_PLAN.md` §4–§5):

- `GET /cards/tcgplayer/{id}` — 404, gallery has no tcgplayer_id.
- `GET /cards/{uuid-id}` — 404, no UUIDs.
- `GET /sets/{uuid-id}` — 404, no UUIDs.
- `GET /sets/tcgplayer/{id}` — 404.
- `GET /sets/cardmarket/{id}` — 404.
- `GET /index/keywords` — 404, requires text parsing.

If the bot starts calling any of these in the future, the
behaviour is the same as before the cutover: a 404, which the
bot's adapter turns into `null`.
