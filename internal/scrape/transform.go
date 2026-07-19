// Package scrape fetches and decodes the upstream card gallery.
//
// This file is the third seam in the package: the per-card transform
// from the upstream's shape (defined by the data we extract from
// __NEXT_DATA__) into the local card data wire format. The
// field-by-field recipe is in docs/IMPLEMENTATION_PLAN.md §2 and
// the upstream shape is in docs/research/playriftbound-card-gallery.md
// §2.
//
// The transformer's contract is fully covered by transform_test.go.
// Changes here should be driven by a failing test, not by drift in
// the upstream.
package scrape

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/xalevagre7/riftapi/internal/domain"
)

// --- upstream shape --------------------------------------------------------
//
// These structs mirror the subset of the gallery's per-card JSON that
// we actually read. Every field is optional except the four with no
// `*` and no `omitempty`: id, collectorNumber, name, and the
// set/cardType/publicCode/rarity/domain/cardImage orientation/illustrator/text
// objects (these are always present per the research report, even
// when their inner values are empty arrays).

type galleryCard struct {
	ID              string                `json:"id"`
	CollectorNumber int                   `json:"collectorNumber"`
	Name            string                `json:"name"`
	Set             gallerySet            `json:"set"`
	CardType        galleryCardType       `json:"cardType"`
	PublicCode      string                `json:"publicCode"`
	Rarity          galleryRarity         `json:"rarity"`
	Domain          galleryDomain         `json:"domain"`
	CardImage       galleryImage          `json:"cardImage"`
	Orientation     string                `json:"orientation"`
	Illustrator     galleryIllustrator    `json:"illustrator"`
	Text            galleryText           `json:"text"`
	Energy          *galleryStat          `json:"energy"`
	Might           *galleryStat          `json:"might"`
	Power           *galleryStat          `json:"power"`
	Tags            *galleryTags          `json:"tags"`
}

type gallerySet struct {
	Label string `json:"label"`
	Value struct {
		ID    string `json:"id"`
		Label string `json:"label"`
	} `json:"value"`
}

type galleryCardType struct {
	Type      []galleryNamedItem `json:"type"`
	SuperType []galleryNamedItem `json:"superType"`
}

type galleryNamedItem struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Icon  string `json:"icon,omitempty"`
}

type galleryRarity struct {
	Label string          `json:"label"`
	Value galleryNamedItem `json:"value"`
}

type galleryDomain struct {
	Label  string             `json:"label"`
	Values []galleryNamedItem `json:"values"`
}

type galleryImage struct {
	Type              string `json:"type"`
	Provider          string `json:"provider"`
	URL               string `json:"url"`
	AccessibilityText string `json:"accessibilityText"`
}

type galleryIllustrator struct {
	Label  string             `json:"label"`
	Values []galleryNamedItem `json:"values"`
}

type galleryText struct {
	Label    string `json:"label"`
	RichText struct {
		Type string `json:"type"`
		Body string `json:"body"`
	} `json:"richText"`
}

type galleryStat struct {
	Label string          `json:"label"`
	Value galleryNamedItem `json:"value"`
}

type galleryTags struct {
	Label string   `json:"label"`
	Tags  []string `json:"tags"`
}

// --- transform -------------------------------------------------------------

// TransformCard converts a single gallery card (as raw JSON) into the
	// wire shape. The setMaxs map provides per-set collector
// maxima so the transformer can detect overnumbered prints; pass an
// empty map (not nil) if the caller has not pre-loaded the set
// metadata. With an empty map the overnumbered flag is conservatively
// false for every card, which is what the API will report until the
// first full sync lands.
//
// See docs/IMPLEMENTATION_PLAN.md §2 for the field-by-field recipe.
// All "always null" fields (TCGPlayerID, Text.Flavour,
// Metadata.UpdatedOn) are set to nil per ADR-0001.
func TransformCard(raw []byte, setMaxs map[string]int) (*domain.Card, error) {
	var gc galleryCard
	if err := json.Unmarshal(raw, &gc); err != nil {
		return nil, fmt.Errorf("unmarshal gallery card: %w", err)
	}
	if gc.ID == "" {
		return nil, fmt.Errorf("gallery card has empty id")
	}
	if gc.Name == "" {
		return nil, fmt.Errorf("gallery card %s has empty name", gc.ID)
	}

	riftboundID := deriveRiftboundID(gc.ID)
	setID := gc.Set.Value.ID
	setMax := setMaxs[setID]

	card := &domain.Card{
		ID:              riftboundID,
		Name:            gc.Name,
		RiftboundID:     riftboundID,
		CollectorNumber: gc.CollectorNumber,
		PublicCode:      gc.PublicCode,
		Classification: domain.Classification{
			Type:      firstLabel(gc.CardType.Type),
			Supertype: firstLabelPtr(gc.CardType.SuperType),
			Rarity:    gc.Rarity.Value.Label,
			Domain:    domainLabels(gc.Domain.Values),
		},
		Text: domain.Text{
			Rich:    gc.Text.RichText.Body,
			Plain:   stripHTML(gc.Text.RichText.Body),
			Flavour: nil, // see ADR-0001
		},
		Set: domain.CardSet{
			SetID: setID,
			Label: gc.Set.Value.Label,
		},
		Media: domain.Media{
			ImageURL:          gc.CardImage.URL,
			Artist:            firstLabelPtr(gc.Illustrator.Values),
			AccessibilityText: nilPtrIfEmpty(gc.CardImage.AccessibilityText),
		},
		Orientation: gc.Orientation,
		Tags:        cardTags(gc.Tags),
		Metadata: domain.Metadata{
			CleanName:    cleanName(gc.Name),
			UpdatedOn:    nil, // see ADR-0001
			AlternateArt: isAlternateArt(riftboundID),
			Overnumbered: setMax > 0 && gc.CollectorNumber > setMax,
			Signature:    hasSignatureSuperType(gc.CardType.SuperType),
		},
	}

	if attrs := buildAttributes(gc.Energy, gc.Might, gc.Power); attrs != nil {
		card.Attributes = attrs
	}

	return card, nil
}

// deriveRiftboundID converts the upstream's id (e.g. "ogn-011-298")
	// into the riftbound_id (e.g. "ogn-011"). The trailing
// "-{setMax}" segment is stripped. For alternate arts (e.g.
// "ogn-066a-298") the trailing letter is preserved.
//
// The publicCode field is intentionally not consulted: it sometimes
// omits the alternate-art letter suffix that the id always carries.
// The id is the source of truth.
func deriveRiftboundID(id string) string {
	if i := strings.LastIndexByte(id, '-'); i > 0 {
		return strings.ToLower(id[:i])
	}
	return strings.ToLower(id)
}

// firstLabel returns the label of the first element, or "" if empty.
func firstLabel(items []galleryNamedItem) string {
	if len(items) == 0 {
		return ""
	}
	return items[0].Label
}

// firstLabelPtr is like firstLabel but returns *string (nil when empty).
func firstLabelPtr(items []galleryNamedItem) *string {
	if len(items) == 0 {
		return nil
	}
	s := items[0].Label
	return &s
}

// domainLabels returns the label of each named item, or an empty slice
	// (not nil) when the input is empty — the contract uses
// `[]` rather than `null` for empty domain arrays.
func domainLabels(items []galleryNamedItem) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.Label)
	}
	return out
}

// nilPtrIfEmpty returns nil for an empty string, or a pointer to the
// string otherwise.
func nilPtrIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// cardTags returns nil when the upstream tags field is absent, or a
// pointer to the slice when present (so the JSON output preserves
// the null vs [] distinction).
func cardTags(gt *galleryTags) *[]string {
	if gt == nil {
		return nil
	}
	out := make([]string, 0, len(gt.Tags))
	out = append(out, gt.Tags...)
	return &out
}

// buildAttributes converts the three optional stat fields into a
// domain.Attributes struct. The upstream stores the numeric value as
// a string in `value.id` (e.g. "3"); strconv.Atoi parses it. If all
// three are missing, returns nil so the Attributes field is omitted
// from the JSON output (omitempty).
func buildAttributes(energy, might, power *galleryStat) *domain.Attributes {
	var a domain.Attributes
	set := false
	if energy != nil {
		if n, err := strconv.Atoi(energy.Value.ID); err == nil {
			a.Energy = &n
			set = true
		}
	}
	if might != nil {
		if n, err := strconv.Atoi(might.Value.ID); err == nil {
			a.Might = &n
			set = true
		}
	}
	if power != nil {
		if n, err := strconv.Atoi(power.Value.ID); err == nil {
			a.Power = &n
			set = true
		}
	}
	if !set {
		return nil
	}
	return &a
}

// --- text helpers ----------------------------------------------------------

// htmlTagRE matches an HTML opening or closing tag. The non-greedy
// match is intentional: a card with `<p>a</p> <p>b</p>` should become
// `a b`, not lose its content.
var htmlTagRE = regexp.MustCompile(`<[^>]*>`)

// htmlEntityRE matches the small set of entities the upstream emits
// inside card text. The full HTML5 set is large; this is the subset
// the gallery has been observed to use (per
// docs/research/playriftbound-card-gallery.md §2).
var htmlEntityRE = regexp.MustCompile(`&(amp|lt|gt|quot|#39);`)

// stripHTML removes HTML tags and decodes the common entities from s.
// Runs of whitespace (including those left by stripped tags) are
// collapsed to a single space and the result is trimmed. Suitable
	// for the text.plain field.
func stripHTML(s string) string {
	s = htmlTagRE.ReplaceAllString(s, "")
	s = htmlEntityRE.ReplaceAllStringFunc(s, func(m string) string {
		switch m {
		case "&amp;":
			return "&"
		case "&lt;":
			return "<"
		case "&gt;":
			return ">"
		case "&quot;":
			return "\""
		case "&#39;":
			return "'"
		}
		return m
	})
	return strings.Join(strings.Fields(s), " ")
}

// cleanName returns a search-friendly version of name: lowercased
// with word-separating punctuation (commas, periods, semicolons,
// parentheses) replaced by spaces, and intra-word marks (apostrophes,
// hyphens that join words in TCG names like "Kai'Sa") stripped
	// entirely. Runs of whitespace are collapsed. Used for the
// metadata.clean_name field.
//
//   "Abandon"              → "abandon"
//   "Jinx, Loose Cannon"   → "jinx loose cannon"
//   "Kai'Sa, Void"         → "kaisa void"
func cleanName(name string) string {
	lower := strings.ToLower(name)
	var b strings.Builder
	b.Grow(len(lower))
	for _, r := range lower {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '\'' || r == '-':
			// Intra-word marks: drop entirely so "Kai'Sa" searches
			// the same as "Kaisa".
		default:
			// Word-separating punctuation: replace with a space.
			b.WriteRune(' ')
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// isAlternateArt reports whether riftboundID represents an alternate
// art of a base card. The convention is a single trailing letter
// after at least one digit in the collector portion: "ogn-066a" is
// alternate art, "ogn-066" is not, "ogn-066ab" is not (only the
// single-letter suffix convention is in use as of this writing).
func isAlternateArt(riftboundID string) bool {
	i := strings.LastIndexByte(riftboundID, '-')
	if i < 0 {
		return false
	}
	collector := riftboundID[i+1:]
	n := len(collector)
	if n < 2 {
		return false
	}
	last := collector[n-1]
	if last < 'a' || last > 'z' {
		return false
	}
	for j := 0; j < n-1; j++ {
		c := collector[j]
		if c >= '0' && c <= '9' {
			return true
		}
	}
	return false
}

// hasSignatureSuperType reports whether items contains an entry with
	// id "signature". The metadata.signature flag is derived
// from this — the gallery does not expose the flag directly.
func hasSignatureSuperType(items []galleryNamedItem) bool {
	for _, it := range items {
		if it.ID == "signature" {
			return true
		}
	}
	return false
}
