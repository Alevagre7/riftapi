# RiftAPI Context

Self-hosted read-only HTTP API that mirrors the [Riftcodex](https://riftcodex.com/docs/) JSON shape for the Riftbound TCG. Runs on a Raspberry Pi 3B inside a Docker container. Data is populated by scraping `playriftbound.com` and stored in a local SQLite database. The API serves requests without calling any upstream at request time.

## Language

**Riftbound**:
The trading card game published by Riot Games. "Riftbound" is the game; cards in this project are Riftbound cards. Note: the project is *not* part of the official Riot Developer ecosystem.
_Avoid_: "TCG" (too generic in the assistant's vocabulary), "Riot cards" (Riot publishes several games)

**Card**:
A single print of a Riftbound card, identified by a `riftbound_id` (e.g. `ogn-011`) and a `set` + `collector_number`. Multiple prints of the same card (e.g. alternate art, showcase, overnumbered) are distinct Cards with their own `riftbound_id` and `collector_number`. A Card is the atomic unit served by the API.
_Avoid_: "Edition" (a Card already captures variants; "edition" is ambiguous with print runs), "Printing"

**Set**:
A group of Cards released together as a single product. Identified by a `set_id` (e.g. `OGN`, `UNL`, `SFD`, `VEN`). Each Card belongs to exactly one Set. Set metadata (release date, card count) is served by the API.
_Avoid_: "Expansion" (in TCG parlance, "expansion" usually refers to the release event, not the data grouping), "Release"

**Riftbound ID (a.k.a. public code)**:
The unique identifier of a Card within Riftbound's print numbering scheme. Format: `{SET_ID}-{COLLECTOR_NUMBER}/{SET_TOTAL}` (e.g. `OGN-011/298`). The API stores the bare form (`ogn-011`) as `riftbound_id` and the suffixed form as `public_code` when available.
_Avoid_: "Card ID" (too generic — every system has its own IDs), "Print code"

**Collector Number**:
The number of a Card within its Set, in the Set's own ordering. May be greater than the Set's nominal card count when a Card is "overnumbered" (a special print with a number above the standard max).
_Avoid_: "Card number", "Index"

**Alternate Art**:
A Card that shares its name and rules with another Card but has different artwork. In the local store, alternate arts are derived from the gallery by inspecting the trailing letter in the Riftbound ID (e.g. `ogn-066a` is the alternate art of `ogn-066`).
_Avoid_: "Alt art" (inconsistent with the Riftcodex schema's `alternate_art` field name)

**Spoiler Season**:
The 2–4 week window between an expansion's public announcement and its official release, during which new cards are revealed one or a few at a time. The project's nightly sync is *enabled* during Spoiler Season and *disabled* by default at all other times. The store is therefore stable between expansions and refreshed only when the bot maintainer explicitly enables the sync.
_Avoid_: "Pre-release window" (pre-release has a separate meaning in TCG events), "Preview season" (less precise)

**Gallery Snapshot**:
The set of Cards known to the local store at a given moment. The Snapshot is what the API serves. The Snapshot is replaced (not merged) by a successful sync.
_Avoid_: "Cache" (the store is authoritative between syncs, not a cache)

## Source of Truth

| Concept | Source | Notes |
|---|---|---|
| Card fields (name, type, rarity, text, image) | `playriftbound.com/en-us/card-gallery/` `__NEXT_DATA__` | Scraper is sole source |
| `tcgplayer_id` | not stored | Missing from gallery; left null |
| `text.flavour` | not stored | Not separated from rules text in gallery |
| `metadata.updated_on` | not stored | Not provided by gallery |
| Set release date | not stored | Not provided by gallery |
