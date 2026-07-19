# Use Playriftbound.com Gallery as the Sole Data Source

The local card store is populated exclusively by scraping `playriftbound.com/en-us/card-gallery/` (the `__NEXT_DATA__` JSON blob in the page HTML). The maintainer wants the *lineage* of the data to be independent of any third party (not just runtime-independent of upstream), accepts the resulting data gap, and accepts the Riot Legal Jibber Jabber policy implications. See `docs/research/playriftbound-card-gallery.md` for the upstream's exact structure and the field-level reconciliation rules.

## Consequences

- **Card count gap**: the local store tops out at ~1,178 cards (5 sets). Any future sets released outside the gallery are not in the local store. This is acceptable as long as consumers do not need cards outside the 5 main sets.
- **Field gap**: `tcgplayer_id`, `text.flavour`, and `metadata.updated_on` are not present in the gallery JSON and are stored as `null`. Anything that reads these fields must tolerate nulls.
- **Derived fields**: `metadata.alternate_art`, `metadata.overnumbered`, and `metadata.signature` are not explicit in the gallery and are inferred during the sync transform (see `docs/research/playriftbound-card-gallery.md` §6 for the rules). The transform must be tested against a known card set so a regression is loud, not silent.
- **Riot Legal Jibber Jabber**: a self-hosted card library serving players is a use case Riot explicitly covers in their developer policy. The project's README, the API's root response, and any user-facing surface must include the attribution statement. Registration with the Riot Developer Portal is *not* being pursued for this project.
- **Upstream fragility**: the gallery is a Next.js Pages Router SPA. The card data is delivered as a JSON blob inside `<script id="__NEXT_DATA__">` in the initial HTML. A change to the page structure (different `blades` index, renamed fields) will silently break the scraper. The post-scrape pipeline must validate the parsed card count against an expected minimum and the schema against a sample card.
- **No fallback path**: there is no automatic fallback to a third-party source when the scrape fails. A failure means the store keeps its last successful snapshot.

## Considered Options

- **Pull from a third-party public API** — more cards, exact target schema, no schema mapping, no scraping fragility. Rejected because the maintainer wants lineage independence from any third-party data provider, not just runtime independence.
- **Register for the official Riot Developer Portal API** — fully compliant, but requires an application process, returns a different data shape that still needs a mapping, and introduces an API key + rate limits at sync time. Rejected as overkill for a hobby project.
- **Scrape playriftbound + augment missing fields from a third-party source** — most complete data, but two parsers, two sync code paths, and reintroduces the third-party lineage the maintainer wants to avoid. Rejected.
