# Structural Report: playriftbound.com Card Gallery

**Date**: 2026-07-19  
**Target URL**: `https://playriftbound.com/en-us/card-gallery/`  
**Canonical origin**: `https://playriftbound.com` (redirects from `https://riftbound.leagueoflegends.com`)  
**Purpose**: Structural report on the upstream that populates the local card store.

---

## 1. How the Card Gallery is Delivered

### Architecture

The site is a **Next.js Pages Router SPA** (Pages Router, not App Router). Evidence:

- Scripts served from `/_next/static/chunks/pages/[[...pathArray]]-*.js` — the catch-all dynamic route pattern of Pages Router.
- Inline `<script id="__NEXT_DATA__" type="application/json">` tag in the initial HTML.
- Build manifest at `/_next/static/{BUILD_ID}/_buildManifest.js`.
- Build ID (at time of inspection): **`ixIaRJ_TqzVKAL5oD4y1h`**.

### How Data Reaches the Browser

1. **Initial HTML contains a `__NEXT_DATA__` script tag** with the full page props — including **all card data** (1,178 cards) embedded as JSON. This is the server-side render pass. The gallery is a single HTML response ~3.3 MB, the JSON payload alone is ~3.2 MB.

2. **After hydration, the SPA lazy-loads additional pages** via a Riot Publishing Content API at:
   ```
   /publishing-content/v2.0/public/channel/riftbound_website/list/riftbound_gallery_cards?locale=en_US&from=0&limit=200
   ```
   This endpoint was confirmed to require authentication (HTTP 401 when fetched directly without site cookies/referer). It uses offset-based pagination (`from`, `limit`). The async metadata indicates 1,186 total items across 6 pages of 200.

3. **Next.js data endpoint** also serves the same page props:
   ```
   GET /_next/data/{BUILD_ID}/en-us/card-gallery.json
   ```
   This returns the same JSON as `__NEXT_DATA__` (confirmed: 3.2 MB JSON response, unauthenticated). This is the **cleanest JSON source**.

### Summary

| Delivery mechanism | Cards in payload | Auth needed | Notes |
|---|---|---|---|
| `__NEXT_DATA__` in HTML | 1,178 | No | Embedded in script tag |
| `/_next/data/{BUILD_ID}/...json` | 1,178 | No | Clean JSON-only endpoint |
| Async publishing API | 1,186 total | Yes (401) | Offset pagination, same-origin only |

The discrepancy (1,178 vs 1,186) likely means the initial SSR payload omits cards filtered by default UI state, while the async API returns all.

---

## 2. Fields Present Per Card

### Fields Extracted from `__NEXT_DATA__` Card Objects

| Field | Present | Type | Always? | Notes |
|---|---|---|---|---|
| `id` | ✅ | string | Always | e.g. `"unl-131-219"` — pattern `{set}-{collectorNumber}-{setMax}` |
| `collectorNumber` | ✅ | integer | Always | e.g. `131` |
| `name` | ✅ | string | Always | e.g. `"Abandon"` |
| `set` | ✅ | object | Always | `{ label: "Card Set", value: { id: "UNL", label: "Unleashed" } }` |
| `cardType.type` | ✅ | object[] | Always | Array of `{ id, label, icon }`. Types seen: `unit`, `spell`, `gear`, `battlefield`, `rune`, `legend` |
| `cardType.superType` | ⚠️ | object[] | 32% | Array of `{ id, label }`. Values: `champion`, `token`, `signature`, `basic` |
| `publicCode` | ✅ | string | Always | e.g. `"UNL-131/219"` |
| `rarity` | ✅ | object | Always | `{ label, value: { id, label, icon } }`. Values: `common`, `uncommon`, `rare`, `epic`, `showcase` |
| `domain` | ✅ | object | Always | `{ label, values: [{ id, label, icon }] }`. Domains: `fury`, `chaos`, `order`, `mind`, `body`, `calm`, `colorless` |
| `cardImage` | ✅ | object | Always | `{ type, provider, url, accessibilityText, dimensions, colors, mimeType }` |
| `orientation` | ✅ | string | Always | `"portrait"` (94%) or `"landscape"` (6%) |
| `illustrator` | ✅ | object | Always | `{ label, values: [{ id, label, icon }] }` — artist(s) |
| `text` | ✅ | object | 98% | `{ label, richText: { type: "html", body: "<p>...</p>" } }` |
| `energy` | ⚠️ | object | 81% | `{ label, value: { id, label } }` — energy cost |
| `might` | ⚠️ | object | 53% | `{ label, value: { id, label } }` |
| `power` | ⚠️ | object | 41% | `{ label, value: { id, label } }` |
| `tags` | ⚠️ | object | 69% | `{ label, tags: ["Tag1", ...] }`. Champion names, regions, etc. |
| `flags` | ⚠️ | array | 7% | `[{ id: "new", label: "New" }]` — only `"new"` seen. |

### Rich Text Formatting

Card text (`text.richText.body`) is **HTML-formatted** with `<p>`, `<br />`, and custom token icons:

```html
<p>[Reaction] (Play any time, even before spells and abilities resolve.)<br />
Counter a spell. Return it to its owner's hand instead of putting it in their trash.<br />
[Predict]. (Look at the top card of your Main Deck. You may recycle it.)</p>
```

Keywords are in `[Square Brackets]`. Resource symbols are inline tokens like `:rb_might:`, `:rb_energy_2:`, `:rb_exhaust:`, `:rb_rune_rainbow:`. No BBCode; no `<em>`/`<i>` tags found for flavour text (flavour text is not separated from rules text in the gallery data).

### What is Missing From the Gallery

The gallery data does **not** provide these fields directly:

| Missing Field | Why It Matters | Can It Be Derived? |
|---|---|---|
| `tcgplayer_id` | TCGPlayer product ID for price lookups | ❌ Not in gallery data. Must be sourced elsewhere (TCGPlayer itself) |
| `text.plain` | Plain-text (no HTML) version of card text | ✅ Can strip HTML from `text.richText.body` |
| `text.flavour` | Separate flavour/italic text | ⚠️ **Not distinguished** in gallery. Flavour text appears inline in the rich text body with no semantic markup. May need NLP/heuristic splitting. |
| `attributes` (as a structured sub-object) | The card data shape nests `energy`, `might`, `power` under `attributes` | ✅ Trivial to restructure |
| `classification` (as a structured sub-object) | The card data shape nests `type`, `supertype`, `rarity`, `domain` | ✅ Trivial to restructure |
| `set.set_id` / `set.label` | The card data shape wants flat `set_id`, `label` on `set` object | ✅ Derivable from gallery's `set.value.id` / `set.value.label` |
| `media.artist` | Artist as plain string | ✅ Derivable from `illustrator.values[0].label` |
| `media.accessibility_text` | Alt text for images | ✅ Derivable from `cardImage.accessibilityText` |
| `metadata.clean_name` | Name without special chars | ✅ Can be derived: strip punctuation, lowercase |
| `metadata.updated_on` | Last-updated timestamp | ❌ Not in gallery data |
| `metadata.alternate_art` | Boolean flag | ⚠️ Not explicit. Alternate arts have letter suffixes in `id` (e.g. `ogn-066a-298`). Can infer from pattern. |
| `metadata.overnumbered` | Boolean flag | ⚠️ Not explicit. Overnumbered cards have `collectorNumber` > `set.collectorNumberMax`. Can infer. |
| `metadata.signature` | Boolean flag | ⚠️ Not explicit. Cards with `superType: "signature"` are signatures. Can infer. |
| `set.card_count` | Number of cards in the set | ✅ Available in `__NEXT_DATA__` under `blades[2].sets.items[].collectorNumberMax` (but this is max, not count — count > max due to variants) |
| `set.published_on` | Release date | ❌ Not in gallery data |

---

## 3. Discovery / Enumeration

### Gallery Coverage

The `__NEXT_DATA__` contains cards from **5 sets**:

| Set ID | Set Name | Cards in Gallery | Collector Max | Notes |
|---|---|---|---|---|
| OGN | Origins | 352 | 298 | Includes alt arts, extended art, overnumbered |
| OGS | Proving Grounds | 24 | 24 | Fixed set |
| SFD | Spiritforged | 288 | 221 | Includes alt arts, overnumbered |
| UNL | Unleashed | 288 | 219 | Includes alt arts, overnumbered |
| VEN | Vendetta | 226 | 166 | Includes alt arts, overnumbered |
| **Total** | | **1,178** (1,186 via async) | | |

### Pagination

The initial SSR (`__NEXT_DATA__`) returns 1,178 cards. The async publishing API has 6 pages of 200 (1,186 total). The 8-card gap may be cards that require different default filter state.

The async API URL pattern:
```
/publishing-content/v2.0/public/channel/riftbound_website/list/riftbound_gallery_cards?locale=en_US&from={OFFSET}&limit=200
```

### Sitemap

`https://playriftbound.com/sitemap_index.xml` exists and lists locale-specific sitemaps. The English sitemap at `https://playriftbound.com/en-us/sitemap_loc.xml` contains **no individual card pages** — only:
- Homepage
- `/card-gallery/`
- `/get-started/`
- `/how-to-play/`
- `/news/*` (articles)
- `/rules-hub/`
- `/tcg-cards/`
- `/preorder/registration/`

No `/card/ogn-001-298` or similar per-card pages exist in the sitemap.

### Filter Controls Visible in UI

From the rendered HTML: filters for **Sort By**, **Set**, and "New Cards" toggle. The filtering is client-side (React state).

### Total Card Count

- A third-party public API for the same TCG reports **1,449** total cards (note: the upstream scraper does not depend on that service — it is mentioned here only to document the gap). This includes cards from more sets (e.g. promotional cards not in the gallery).
- Gallery (`__NEXT_DATA__`): **1,178** cards.
- Gallery (async API metadata): **1,186** cards.

---

## 4. Image Hosting

### Image URL Pattern

```
https://cmsassets.rgpub.io/sanity/images/dsfx7636/game_data_live/{HASH}-{WIDTH}x{HEIGHT}.png?accountingTag=RB
```

Sample:
```
https://cmsassets.rgpub.io/sanity/images/dsfx7636/game_data_live/89929cfa4417c99576477793529c6808af145919-744x1039.png?accountingTag=RB
```

### Host Details

- **Domain**: `cmsassets.rgpub.io` — Riot Games' Sanity CMS asset CDN.
- **No auth required**: Images are publicly accessible (no cookies, no referer checks on the CDN).
- **Dimensions**: Cards are standardised at 744×1039px (portrait) or 1039×744px (landscape).
- **MIME type**: `image/png`.
- **Additional image metadata** available in the JSON: `colors` (primary, secondary, label hex codes), `aspectRatio`.
- **Artist icons** come from `https://assetcdn.rgpub.io/...` — also public.

### Accessibility Text

Each card image includes `accessibilityText` in the JSON (used as `alt` text), for example:
> "Riftbound Spell: Abandon. [Reaction] (Play any time, even before spells and abilities resolve.)\nCounter a spell. Return it to its owner's hand instead of putting it in their trash.\n[Predict]. (Look at the top card of your Main Deck. You may recycle it.)"

This is a concise rules-text summary, useful for the card data's `media.accessibility_text` field.

---

## 5. ToS / Scraping Constraints

### robots.txt

```
User-agent: *
Allow: /

Sitemap: https://playriftbound.com/sitemap_index.xml
```

No disallowed paths. The card gallery (`/en-us/card-gallery/`) is explicitly allowed.

### Riot Games Developer Policy (Relevant to Scraping)

From `https://developer.riotgames.com/policies/riftbound`:

- **Registration required**: "If your product serves players, you must register it with us regardless of whether or not your product uses official documented APIs."
- **Use cases**: "Deck builders" and "Card libraries" are explicitly approved use cases. A self-hosted card library fits this category.
- **Monetization**: Must not simulate gameplay; must have a free tier; must be transformative.
- **Attribution required**: Must include Riot's Legal Jibber Jabber statement: _"This project was created under Riot Games' 'Legal Jibber Jabber' policy using assets owned by Riot Games. Riot Games does not endorse or sponsor this project."_
- **Assets**: "Your App may only use Riftbound assets (including cards) provided by the Riot API. No external or unofficial materials." — however the gallery page is a public-facing website, and parsing `__NEXT_DATA__` is a grey area. Using the official Riot API (via developer portal API key) would be the compliant approach.

### Riot Developer Portal API (Approach for Compliance)

The Riot Developer Portal documents a `riftbound-content-v1` API with `GET /riftbound/content/v1/contents`. However:
- It requires an API key from the developer portal.
- The data format is different from the gallery (returns `media` array vs `CardArtDTO` per a bug report).
- This is the officially sanctioned path, but getting a key requires an application process.

**No explicit scraping prohibition was found** on the public website's robots.txt or terms page, but Riot's developer policy does require registration for products serving players.

---

## 6. Recommended Scraping Strategy

### Primary Strategy: Parse `__NEXT_DATA__` from the Initial HTML

**Rationale**: The card gallery is a Next.js Pages Router SPA that embeds all card data as a JSON blob inside a `<script id="__NEXT_DATA__" type="application/json">` tag in the initial HTML. This gives you:

- ✅ All 1,178+ cards in a single HTTP GET request (no JavaScript execution needed)
- ✅ Clean, structured JSON — no HTML parsing needed
- ✅ No authentication required
- ✅ No pagination (on the first page at least)
- ✅ No headless browser required

**Implementation sketch**:
```python
import requests, json, re

resp = requests.get("https://playriftbound.com/en-us/card-gallery/")
match = re.search(r'__NEXT_DATA__" type="application/json">(.*?)</script>', resp.text, re.DOTALL)
data = json.loads(match.group(1))
cards = data["props"]["pageProps"]["page"]["blades"][2]["cards"]["items"]
```

Or fetch the Next.js data endpoint directly:
```python
import requests
resp = requests.get(f"https://playriftbound.com/_next/data/{BUILD_ID}/en-us/card-gallery.json")
data = resp.json()
cards = data["pageProps"]["page"]["blades"][2]["cards"]["items"]
```

**Build ID discovery**: Parse `_buildManifest.js` from the HTML, or just scrape the data endpoint once and follow redirects.

### Fallback Strategy: Use the Official Riot Content API

The Riot Developer Portal provides a `riftbound-content-v1` API. Getting an API key requires:
1. Registering your product at `https://developer.riotgames.com/`
2. Going through an application process (deck builders / card libraries are approved use cases)
3. Agreeing to Riot's terms (attribution, no monetization of raw data, etc.)

This is the **only fully compliant** strategy.

### Third Option (Not Recommended): HTML Parse with GoQuery

Since the gallery is a JS SPA, HTML parsing would only get the empty shell. The card data is only available via the `__NEXT_DATA__` script tag, which is already JSON — no HTML parsing needed.

### Top 3 Risks

1. **Build ID changes unpredictably** (Risk: Medium) — The Next.js build ID changes on every deployment. The `/_next/data/{BUILD_ID}/...` endpoint path changes with it. Mitigation: extract the build ID from `_buildManifest.js` or `_ssgManifest.js` in the page HTML before constructing the data URL. Or better: just parse `__NEXT_DATA__` from the initial HTML, which doesn't need the build ID at all.

2. **Embedded card data may be incomplete** (Risk: Medium) — The SSR payload includes 1,178 cards vs the async API's 1,186. Cards may be partitioned by default filter state (e.g. only showing "New Cards"). Mitigation: verify count parity; if needed, the publishing-content API requires cookies/referer spoofing to bypass 401.

3. **Riot policy compliance** (Risk: High — Legal) — Riot's developer policy states: *"If your product serves players, you must register it with us regardless of whether or not your product uses official documented APIs."* Scraping the public website without registration could violate their terms, even if robots.txt allows it. The safest path is to register for an official API key.

### Mapping to the Card Data Shape

To transform the gallery data into the local card data shape, the following is needed per card:

```python
def transform_gallery_card(g):
    return {
        "name": g["name"],
        "riftbound_id": g["id"],   # or derive from publicCode
        "collector_number": g["collectorNumber"],
        "attributes": {
            "energy": g.get("energy", {}).get("value", {}).get("id"),
            "might": g.get("might", {}).get("value", {}).get("id"),
            "power": g.get("power", {}).get("value", {}).get("id"),
        },
        "classification": {
            "type": g["cardType"]["type"][0]["label"],  # e.g. "Spell"
            "supertype": g["cardType"].get("superType", [{}])[0].get("label"),
            "rarity": g["rarity"]["value"]["label"],
            "domain": [d["label"] for d in g["domain"]["values"]],
        },
        "text": {
            "rich": g["text"]["richText"]["body"],
            "plain": strip_html(g["text"]["richText"]["body"]),
            "flavour": None,  # NOT AVAILABLE from gallery
        },
        "set": {
            "set_id": g["set"]["value"]["id"],
            "label": g["set"]["value"]["label"],
        },
        "media": {
            "image_url": g["cardImage"]["url"],
            "artist": g["illustrator"]["values"][0]["label"],
            "accessibility_text": g["cardImage"]["accessibilityText"],
        },
        "tags": g.get("tags", {}).get("tags", []),
        "orientation": g["orientation"],
        "metadata": {
            "clean_name": clean_name(g["name"]),
            "updated_on": None,  # NOT AVAILABLE
            "alternate_art": "a" in g["id"].split("-")[1],  # heuristic
            "overnumbered": g["collectorNumber"] > get_set_max(g["set"]["value"]["id"]),
            "signature": any(st["id"] == "signature" for st in g["cardType"].get("superType", [])),
        },
        # Note: tcgplayer_id, id (opaque internal ID)
        # are NOT available from the gallery
    }
```

Fields that **cannot** be obtained from the gallery and must be sourced elsewhere or left null:
- `id` (opaque internal UUID, not present in the gallery)
- `tcgplayer_id`
- `text.flavour`
- `metadata.updated_on`
- `set.card_count` (but available in top-level set metadata)

---

## Appendix: Observed API Endpoints

| Endpoint | Method | Auth | Description |
|---|---|---|---|
| `https://playriftbound.com/en-us/card-gallery/` | GET | No | HTML page with `__NEXT_DATA__` |
| `https://playriftbound.com/_next/data/{BUILD_ID}/en-us/card-gallery.json` | GET | No | JSON-only page props |
| `https://playriftbound.com/publishing-content/v2.0/public/channel/riftbound_website/list/riftbound_gallery_cards?...` | GET | Yes (401) | Paginated card data API |
| `https://playriftbound.com/publishing-content/v2.0/public/channel/riftbound_website/list/riftbound_gallery_sets?...` | GET | No | Set list (5 sets) |
| `https://developer.riotgames.com/apis#riftbound-content-v1` | GET | API Key | Official Riot Riftbound Content API |
