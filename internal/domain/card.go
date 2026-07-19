// Package domain contains the typed shapes of the data the API serves and
// the scraper produces. Field names use snake_case to match the
// upstream gallery's JSON output byte-for-byte.
//
// The shapes are intentionally close to the wire format — there is no
// transformation layer between "what the database stores" and "what the
// API returns" beyond json.Marshal. If a field is nullable in the
// upstream data, it is a pointer here so that the JSON output
// preserves the null vs missing distinction.
package domain

// Card is a single print of a Riftbound card. See CONTEXT.md for the
// domain definition and ADR-0001 for the source.
type Card struct {
	ID              string         `json:"id"`
	Name            string         `json:"name"`
	RiftboundID     string         `json:"riftbound_id"`
	TCGPlayerID     *string        `json:"tcgplayer_id"`
	CollectorNumber int            `json:"collector_number"`
	Attributes      *Attributes    `json:"attributes,omitempty"`
	Classification  Classification `json:"classification"`
	Text            Text           `json:"text"`
	Set             CardSet        `json:"set"`
	Media           Media          `json:"media"`
	Tags            *[]string      `json:"tags,omitempty"`
	Orientation     string         `json:"orientation"`
	Metadata        Metadata       `json:"metadata"`

	// PublicCode is the suffixed form of the riftbound_id (e.g.
	// "ogn-011-298") as provided by the upstream. It is excluded
	// from JSON output (`json:"-"`) but is stored in the
	// cards.public_code column for forward use and easy cross-reference
	// with the upstream.
	PublicCode string `json:"-"`
}

// Attributes are the numeric gameplay stats on a Card. All three are
// nullable in the upstream data (only Units have might/power, only
// things with a cost have energy).
type Attributes struct {
	Energy *int `json:"energy"`
	Might  *int `json:"might"`
	Power  *int `json:"power"`
}

// Classification groups the categorical fields on a Card.
type Classification struct {
	Type      string   `json:"type"`
	Supertype *string  `json:"supertype"`
	Rarity    string   `json:"rarity"`
	Domain    []string `json:"domain"`
}

// Text is the rules and flavour text. `Rich` is HTML-formatted; `Plain`
// is HTML-stripped; `Flavour` is the italicised flavour text (null when
// upstream doesn't separate it from rules text — see ADR-0001).
type Text struct {
	Rich    string  `json:"rich"`
	Plain   string  `json:"plain"`
	Flavour *string `json:"flavour"`
}

// CardSet is the Set reference embedded inside a Card.
type CardSet struct {
	SetID string `json:"set_id"`
	Label string `json:"label"`
}

// Media is the image and artist info.
type Media struct {
	ImageURL         string  `json:"image_url"`
	Artist           *string `json:"artist"`
	AccessibilityText *string `json:"accessibility_text"`
}

// Metadata is the inferred/derived/cleaned fields. `UpdatedOn` is the
// only field that's nullable in a meaningful way — it is null when the
// upstream doesn't provide it (always, for gallery-sourced cards).
type Metadata struct {
	CleanName    string `json:"clean_name"`
	UpdatedOn    *string `json:"updated_on"`
	AlternateArt bool   `json:"alternate_art"`
	Overnumbered bool   `json:"overnumbered"`
	Signature    bool   `json:"signature"`
}
