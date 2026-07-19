package scrape_test

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/xalevagre7/riftapi/internal/scrape"
)

// --- helpers ---------------------------------------------------------------

// buildCardJSON composes a minimal gallery card JSON literal for a
// card with all the well-known fields populated. Tests override the
// fields they care about by string-replacing the placeholder values.
func buildCardJSON() string {
	return `{
		"id": "ogn-011-298",
		"collectorNumber": 11,
		"name": "Abandon",
		"set": {"label": "Set", "value": {"id": "OGN", "label": "Origins"}},
		"cardType": {
			"type": [{"id": "spell", "label": "Spell", "icon": "i"}],
			"superType": []
		},
		"publicCode": "OGN-011/298",
		"rarity": {"label": "Rarity", "value": {"id": "common", "label": "Common", "icon": "i"}},
		"domain": {"label": "Domain", "values": [{"id": "fury", "label": "Fury", "icon": "i"}]},
		"cardImage": {"url": "https://cdn.example/ogn-011.png", "accessibilityText": "Abandon", "dimensions": {"width": 744, "height": 1039}},
		"orientation": "portrait",
		"illustrator": {"label": "Artist", "values": [{"id": "1", "label": "Artist Name", "icon": "i"}]},
		"text": {"label": "Text", "richText": {"type": "html", "body": "<p>Counter a spell.</p>"}},
		"energy": {"label": "Energy", "value": {"id": "3", "label": "3"}},
		"might": null,
		"power": null,
		"tags": {"label": "Tags", "tags": ["Freljord", "Noxus"]}
	}`
}

// --- happy path ------------------------------------------------------------

func TestTransformCard_HappyPath(t *testing.T) {
	setMaxs := map[string]int{"OGN": 298}
	card, err := scrape.TransformCard([]byte(buildCardJSON()), setMaxs)
	if err != nil {
		t.Fatalf("TransformCard: %v", err)
	}

	if card.Name != "Abandon" {
		t.Errorf("Name = %q, want Abandon", card.Name)
	}
	if card.RiftboundID != "ogn-011" {
		t.Errorf("RiftboundID = %q, want ogn-011", card.RiftboundID)
	}
	if card.ID != "ogn-011" {
		t.Errorf("ID = %q, want ogn-011 (riftbound_id used as the id field)", card.ID)
	}
	if card.CollectorNumber != 11 {
		t.Errorf("CollectorNumber = %d, want 11", card.CollectorNumber)
	}
	if card.Set.SetID != "OGN" {
		t.Errorf("Set.SetID = %q, want OGN", card.Set.SetID)
	}
	if card.Set.Label != "Origins" {
		t.Errorf("Set.Label = %q, want Origins", card.Set.Label)
	}
	if card.Classification.Type != "Spell" {
		t.Errorf("Classification.Type = %q, want Spell", card.Classification.Type)
	}
	if card.Classification.Rarity != "Common" {
		t.Errorf("Classification.Rarity = %q, want Common", card.Classification.Rarity)
	}
	if !slices.Equal(card.Classification.Domain, []string{"Fury"}) {
		t.Errorf("Classification.Domain = %v, want [Fury]", card.Classification.Domain)
	}
	if card.Attributes == nil {
		t.Fatalf("Attributes = nil, want non-nil")
	}
	if card.Attributes.Energy == nil || *card.Attributes.Energy != 3 {
		t.Errorf("Energy = %v, want 3", card.Attributes.Energy)
	}
	if card.Attributes.Might != nil {
		t.Errorf("Might = %v, want nil for a spell", card.Attributes.Might)
	}
	if card.Attributes.Power != nil {
		t.Errorf("Power = %v, want nil for a spell", card.Attributes.Power)
	}
	if card.Text.Rich != "<p>Counter a spell.</p>" {
		t.Errorf("Text.Rich = %q", card.Text.Rich)
	}
	if card.Text.Plain != "Counter a spell." {
		t.Errorf("Text.Plain = %q, want 'Counter a spell.'", card.Text.Plain)
	}
	if card.Text.Flavour != nil {
		t.Errorf("Text.Flavour = %v, want nil per ADR-0001", *card.Text.Flavour)
	}
	if card.Media.ImageURL != "https://cdn.example/ogn-011.png" {
		t.Errorf("ImageURL = %q", card.Media.ImageURL)
	}
	if card.Media.Artist == nil || *card.Media.Artist != "Artist Name" {
		t.Errorf("Artist = %v, want 'Artist Name'", card.Media.Artist)
	}
	if card.Media.AccessibilityText == nil || *card.Media.AccessibilityText != "Abandon" {
		t.Errorf("AccessibilityText = %v, want 'Abandon'", card.Media.AccessibilityText)
	}
	if card.Orientation != "portrait" {
		t.Errorf("Orientation = %q, want portrait", card.Orientation)
	}
	if card.Tags == nil || !slices.Equal(*card.Tags, []string{"Freljord", "Noxus"}) {
		t.Errorf("Tags = %v, want [Freljord Noxus]", card.Tags)
	}
	if card.Metadata.CleanName != "abandon" {
		t.Errorf("CleanName = %q, want abandon", card.Metadata.CleanName)
	}
	if card.Metadata.AlternateArt {
		t.Errorf("AlternateArt = true, want false")
	}
	if card.Metadata.Overnumbered {
		t.Errorf("Overnumbered = true, want false (11 <= 298)")
	}
	if card.Metadata.Signature {
		t.Errorf("Signature = true, want false")
	}
	if card.Metadata.UpdatedOn != nil {
		t.Errorf("UpdatedOn = %v, want nil per ADR-0001", card.Metadata.UpdatedOn)
	}
	if card.TCGPlayerID != nil {
		t.Errorf("TCGPlayerID = %v, want nil per ADR-0001", card.TCGPlayerID)
	}
}

// --- riftbound_id derivation ----------------------------------------------

func TestTransformCard_RiftboundIDFromPublicCode(t *testing.T) {
	// The format is "SET-NUMBER/SETMAX" (uppercase set, slash separator).
	// We expect "ogn-011" — lowercase set, no setMax, no slash.
	card, err := scrape.TransformCard([]byte(buildCardJSON()), map[string]int{"OGN": 298})
	if err != nil {
		t.Fatalf("TransformCard: %v", err)
	}
	if card.RiftboundID != "ogn-011" {
		t.Errorf("RiftboundID = %q, want ogn-011", card.RiftboundID)
	}
}

func TestTransformCard_FallsBackToIDWhenPublicCodeMissing(t *testing.T) {
	// Some cards may not have a publicCode in the gallery; fall back to
	// stripping the trailing "-{setMax}" from the upstream id.
	json := strings.Replace(buildCardJSON(), `"publicCode": "OGN-011/298",`, `"publicCode": "",`, 1)
	card, err := scrape.TransformCard([]byte(json), map[string]int{"OGN": 298})
	if err != nil {
		t.Fatalf("TransformCard: %v", err)
	}
	if card.RiftboundID != "ogn-011" {
		t.Errorf("RiftboundID = %q, want ogn-011 (derived from id 'ogn-011-298')", card.RiftboundID)
	}
}

// --- metadata.alternate_art -----------------------------------------------

func TestTransformCard_AlternateArt(t *testing.T) {
	json := strings.Replace(buildCardJSON(), `"ogn-011-298"`, `"ogn-066a-298"`, 1)
	json = strings.Replace(json, `"collectorNumber": 11`, `"collectorNumber": 66`, 1)
	card, err := scrape.TransformCard([]byte(json), map[string]int{"OGN": 298})
	if err != nil {
		t.Fatalf("TransformCard: %v", err)
	}
	if !card.Metadata.AlternateArt {
		t.Errorf("AlternateArt = false, want true for riftbound id 'ogn-066a'")
	}
	if card.RiftboundID != "ogn-066a" {
		t.Errorf("RiftboundID = %q, want ogn-066a", card.RiftboundID)
	}
}

// --- metadata.overnumbered ------------------------------------------------

func TestTransformCard_Overnumbered(t *testing.T) {
	// collectorNumber (11) is less than setMax (298): not overnumbered.
	card, err := scrape.TransformCard([]byte(buildCardJSON()), map[string]int{"OGN": 298})
	if err != nil {
		t.Fatalf("TransformCard: %v", err)
	}
	if card.Metadata.Overnumbered {
		t.Errorf("Overnumbered = true, want false (11 <= 298)")
	}
}

func TestTransformCard_Overnumbered_WhenCollectorExceedsSetMax(t *testing.T) {
	// A card with collectorNumber 305 in a set whose max is 298 is
	// overnumbered. Use a different set to keep the rest of the test
	// card consistent.
	json := strings.Replace(buildCardJSON(),
		`"set": {"label": "Set", "value": {"id": "OGN", "label": "Origins"}}`,
		`"set": {"label": "Set", "value": {"id": "SFD", "label": "Spiritforged"}}`, 1)
	json = strings.Replace(json, `"collectorNumber": 11`, `"collectorNumber": 305`, 1)
	card, err := scrape.TransformCard([]byte(json), map[string]int{"SFD": 221})
	if err != nil {
		t.Fatalf("TransformCard: %v", err)
	}
	if !card.Metadata.Overnumbered {
		t.Errorf("Overnumbered = false, want true (305 > 221)")
	}
}

// --- metadata.signature ---------------------------------------------------

func TestTransformCard_Signature(t *testing.T) {
	json := strings.Replace(buildCardJSON(),
		`"superType": []`,
		`"superType": [{"id": "signature", "label": "Signature", "icon": "i"}]`, 1)
	card, err := scrape.TransformCard([]byte(json), map[string]int{"OGN": 298})
	if err != nil {
		t.Fatalf("TransformCard: %v", err)
	}
	if !card.Metadata.Signature {
		t.Errorf("Signature = false, want true for superType id 'signature'")
	}
}

func TestTransformCard_NoSuperType(t *testing.T) {
	card, err := scrape.TransformCard([]byte(buildCardJSON()), map[string]int{"OGN": 298})
	if err != nil {
		t.Fatalf("TransformCard: %v", err)
	}
	if card.Metadata.Signature {
		t.Errorf("Signature = true, want false for empty superType")
	}
	if card.Classification.Supertype != nil {
		t.Errorf("Supertype = %v, want nil for empty superType", *card.Classification.Supertype)
	}
}

// --- clean_name derivation ------------------------------------------------

func TestTransformCard_CleanNameWithPunctuation(t *testing.T) {
	json := strings.Replace(buildCardJSON(), `"name": "Abandon"`, `"name": "Jinx, Loose Cannon"`, 1)
	card, err := scrape.TransformCard([]byte(json), map[string]int{"OGN": 298})
	if err != nil {
		t.Fatalf("TransformCard: %v", err)
	}
	if card.Metadata.CleanName != "jinx loose cannon" {
		t.Errorf("CleanName = %q, want 'jinx loose cannon'", card.Metadata.CleanName)
	}
}

func TestTransformCard_CleanNameWithApostrophe(t *testing.T) {
	json := strings.Replace(buildCardJSON(), `"name": "Abandon"`, `"name": "Kai'Sa, Void"`, 1)
	card, err := scrape.TransformCard([]byte(json), map[string]int{"OGN": 298})
	if err != nil {
		t.Fatalf("TransformCard: %v", err)
	}
	if card.Metadata.CleanName != "kaisa void" {
		t.Errorf("CleanName = %q, want 'kaisa void'", card.Metadata.CleanName)
	}
}

// --- plain text -----------------------------------------------------------

func TestTransformCard_PlainTextStripsHTML(t *testing.T) {
	json := strings.Replace(buildCardJSON(),
		`<p>Counter a spell.</p>`,
		`<p><strong>Reaction</strong> (play any time): counter a spell.<br />Return it to its owner's hand.</p>`, 1)
	card, err := scrape.TransformCard([]byte(json), map[string]int{"OGN": 298})
	if err != nil {
		t.Fatalf("TransformCard: %v", err)
	}
	if strings.Contains(card.Text.Plain, "<") {
		t.Errorf("Plain = %q, contains HTML", card.Text.Plain)
	}
	if !strings.Contains(card.Text.Plain, "Reaction") {
		t.Errorf("Plain = %q, missing 'Reaction' keyword", card.Text.Plain)
	}
	if !strings.Contains(card.Text.Plain, "counter a spell") {
		t.Errorf("Plain = %q, missing 'counter a spell'", card.Text.Plain)
	}
}

// --- nullable fields ------------------------------------------------------

func TestTransformCard_NoEnergy(t *testing.T) {
	// The base fixture has energy=3, might/power=null. To test "no
	// energy" usefully (i.e. Attributes is still present because
	// might/power are populated), first inject might=2 and power=1,
	// then null out energy. The expected Attributes has Energy=nil,
	// Might=2, Power=1.
	json := strings.Replace(buildCardJSON(),
		`"might": null,
		"power": null,`,
		`"might": {"label": "Might", "value": {"id": "2", "label": "2"}},
		"power": {"label": "Power", "value": {"id": "1", "label": "1"}},`, 1)
	json = strings.Replace(json,
		`"energy": {"label": "Energy", "value": {"id": "3", "label": "3"}},`,
		`"energy": null,`, 1)
	card, err := scrape.TransformCard([]byte(json), map[string]int{"OGN": 298})
	if err != nil {
		t.Fatalf("TransformCard: %v", err)
	}
	if card.Attributes == nil {
		t.Fatalf("Attributes = nil, want non-nil (might/power should be populated)")
	}
	if card.Attributes.Energy != nil {
		t.Errorf("Energy = %v, want nil", *card.Attributes.Energy)
	}
	if card.Attributes.Might == nil || *card.Attributes.Might != 2 {
		t.Errorf("Might = %v, want 2", card.Attributes.Might)
	}
	if card.Attributes.Power == nil || *card.Attributes.Power != 1 {
		t.Errorf("Power = %v, want 1", card.Attributes.Power)
	}
}

func TestTransformCard_NoStatsAtAll(t *testing.T) {
	// The base fixture has might/power=null. Combined with energy=null,
	// every stat is missing. Attributes should be omitted from the
	// output (omitempty), so card.Attributes is nil.
	json := strings.Replace(buildCardJSON(), `"energy": {"label": "Energy", "value": {"id": "3", "label": "3"}},`, `"energy": null,`, 1)
	card, err := scrape.TransformCard([]byte(json), map[string]int{"OGN": 298})
	if err != nil {
		t.Fatalf("TransformCard: %v", err)
	}
	if card.Attributes != nil {
		t.Errorf("Attributes = %+v, want nil when all stats are null", card.Attributes)
	}
}

func TestTransformCard_NoTags(t *testing.T) {
	json := strings.Replace(buildCardJSON(), `"tags": {"label": "Tags", "tags": ["Freljord", "Noxus"]}`, `"tags": null`, 1)
	card, err := scrape.TransformCard([]byte(json), map[string]int{"OGN": 298})
	if err != nil {
		t.Fatalf("TransformCard: %v", err)
	}
	if card.Tags != nil {
		t.Errorf("Tags = %v, want nil when upstream tags is null", card.Tags)
	}
}

func TestTransformCard_NoArtist(t *testing.T) {
	json := strings.Replace(buildCardJSON(),
		`"illustrator": {"label": "Artist", "values": [{"id": "1", "label": "Artist Name", "icon": "i"}]}`,
		`"illustrator": {"label": "Artist", "values": []}`, 1)
	card, err := scrape.TransformCard([]byte(json), map[string]int{"OGN": 298})
	if err != nil {
		t.Fatalf("TransformCard: %v", err)
	}
	if card.Media.Artist != nil {
		t.Errorf("Artist = %v, want nil for empty illustrator.values", *card.Media.Artist)
	}
}

func TestTransformCard_NoAccessibilityText(t *testing.T) {
	json := strings.Replace(buildCardJSON(), `"accessibilityText": "Abandon"`, `"accessibilityText": ""`, 1)
	card, err := scrape.TransformCard([]byte(json), map[string]int{"OGN": 298})
	if err != nil {
		t.Fatalf("TransformCard: %v", err)
	}
	if card.Media.AccessibilityText != nil {
		t.Errorf("AccessibilityText = %v, want nil for empty upstream", *card.Media.AccessibilityText)
	}
}

// --- multiple domains -----------------------------------------------------

func TestTransformCard_MultipleDomains(t *testing.T) {
	json := strings.Replace(buildCardJSON(),
		`"domain": {"label": "Domain", "values": [{"id": "fury", "label": "Fury", "icon": "i"}]}`,
		`"domain": {"label": "Domain", "values": [{"id": "fury", "label": "Fury", "icon": "i"}, {"id": "chaos", "label": "Chaos", "icon": "i"}]}`, 1)
	card, err := scrape.TransformCard([]byte(json), map[string]int{"OGN": 298})
	if err != nil {
		t.Fatalf("TransformCard: %v", err)
	}
	if !slices.Equal(card.Classification.Domain, []string{"Fury", "Chaos"}) {
		t.Errorf("Domain = %v, want [Fury Chaos]", card.Classification.Domain)
	}
}

func TestTransformCard_NoDomains(t *testing.T) {
	json := strings.Replace(buildCardJSON(),
		`"domain": {"label": "Domain", "values": [{"id": "fury", "label": "Fury", "icon": "i"}]}`,
		`"domain": {"label": "Domain", "values": []}`, 1)
	card, err := scrape.TransformCard([]byte(json), map[string]int{"OGN": 298})
	if err != nil {
		t.Fatalf("TransformCard: %v", err)
	}
	if card.Classification.Domain == nil {
		t.Fatalf("Domain = nil, want non-nil (contract defaults to [])")
	}
	if len(card.Classification.Domain) != 0 {
		t.Errorf("Domain = %v, want []", card.Classification.Domain)
	}
}

// --- ADR-0001 null fields are always null --------------------------------

func TestTransformCard_FlavourIsAlwaysNull(t *testing.T) {
	card, err := scrape.TransformCard([]byte(buildCardJSON()), map[string]int{"OGN": 298})
	if err != nil {
		t.Fatalf("TransformCard: %v", err)
	}
	if card.Text.Flavour != nil {
		t.Errorf("Text.Flavour = %v, want nil per ADR-0001", *card.Text.Flavour)
	}
}

func TestTransformCard_UpdatedOnIsAlwaysNull(t *testing.T) {
	card, err := scrape.TransformCard([]byte(buildCardJSON()), map[string]int{"OGN": 298})
	if err != nil {
		t.Fatalf("TransformCard: %v", err)
	}
	if card.Metadata.UpdatedOn != nil {
		t.Errorf("Metadata.UpdatedOn = %v, want nil per ADR-0001", *card.Metadata.UpdatedOn)
	}
}

func TestTransformCard_TCGPlayerIDIsAlwaysNull(t *testing.T) {
	card, err := scrape.TransformCard([]byte(buildCardJSON()), map[string]int{"OGN": 298})
	if err != nil {
		t.Fatalf("TransformCard: %v", err)
	}
	if card.TCGPlayerID != nil {
		t.Errorf("TCGPlayerID = %v, want nil per ADR-0001", *card.TCGPlayerID)
	}
}

// --- public code -----------------------------------------------------------

func TestTransformCard_PopulatesPublicCode(t *testing.T) {
	card, err := scrape.TransformCard([]byte(buildCardJSON()), map[string]int{"OGN": 298})
	if err != nil {
		t.Fatalf("TransformCard: %v", err)
	}
	if card.PublicCode != "OGN-011/298" {
		t.Errorf("PublicCode = %q, want OGN-011/298", card.PublicCode)
	}
}

func TestTransformCard_PublicCodeExcludedFromJSON(t *testing.T) {
	// PublicCode is stored in the database but is not part of the
	// wire format. It must be excluded from any JSON the
	// API serves.
	card, err := scrape.TransformCard([]byte(buildCardJSON()), map[string]int{"OGN": 298})
	if err != nil {
		t.Fatalf("TransformCard: %v", err)
	}
	payload, err := json.Marshal(card)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(payload), "PublicCode") || strings.Contains(string(payload), "public_code") {
		t.Errorf("payload leaked the public_code field: %s", payload)
	}
}

// --- error cases ---------------------------------------------------------

func TestTransformCard_MissingID(t *testing.T) {
	json := strings.Replace(buildCardJSON(), `"id": "ogn-011-298",`, `"id": "",`, 1)
	_, err := scrape.TransformCard([]byte(json), map[string]int{"OGN": 298})
	if err == nil {
		t.Errorf("expected error for missing id, got nil")
	}
}

func TestTransformCard_MissingName(t *testing.T) {
	json := strings.Replace(buildCardJSON(), `"name": "Abandon",`, `"name": "",`, 1)
	_, err := scrape.TransformCard([]byte(json), map[string]int{"OGN": 298})
	if err == nil {
		t.Errorf("expected error for missing name, got nil")
	}
}

func TestTransformCard_MalformedJSON(t *testing.T) {
	_, err := scrape.TransformCard([]byte(`{not json`), map[string]int{"OGN": 298})
	if err == nil {
		t.Errorf("expected error for malformed JSON, got nil")
	}
}

// --- deriveRiftboundID: variant suffixes (tokens, runes) -----------------

// TestTransformCard_PreservesNonNumericSuffix covers the case where
// the upstream id has a non-numeric trailing segment (e.g. tokens
// "unl-t01" or runes "ven-r04"). These are real card variants and
// the trailing suffix must be preserved as part of the riftbound_id.
// Before the fix, deriveRiftboundID stripped every trailing segment,
// collapsing all 8 UNL tokens to "unl" and all 7 VEN runes/tokens
// to "ven". The upsert then kept only one of each, silently
// dropping 13 valid cards from the local store.
func TestTransformCard_PreservesNonNumericSuffix(t *testing.T) {
	tests := []struct {
		name           string
		upstreamID     string
		wantRiftbound  string
	}{
		{"token unl-t01", "unl-t01", "unl-t01"},
		{"token unl-t08", "unl-t08", "unl-t08"},
		{"rune ven-r01", "ven-r01", "ven-r01"},
		{"rune ven-r06", "ven-r06", "ven-r06"},
		{"ven token t04", "ven-t04", "ven-t04"},
		// Regression checks: numeric suffix is still stripped, and
		// alternate-art suffix is still preserved.
		{"base card ogn-011-298", "ogn-011-298", "ogn-011"},
		{"alt art ogn-066a-298", "ogn-066a-298", "ogn-066a"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			json := strings.Replace(buildCardJSON(), `"id": "ogn-011-298"`, `"id": "`+tc.upstreamID+`"`, 1)
			card, err := scrape.TransformCard([]byte(json), nil)
			if err != nil {
				t.Fatalf("TransformCard: %v", err)
			}
			if card.RiftboundID != tc.wantRiftbound {
				t.Errorf("RiftboundID = %q, want %q (upstream id was %q)", card.RiftboundID, tc.wantRiftbound, tc.upstreamID)
			}
		})
	}
}
