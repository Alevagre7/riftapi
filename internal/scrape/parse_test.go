package scrape_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/xalevagre7/riftapi/internal/scrape"
)

// fixturePath returns the path to the sample gallery HTML fixture relative
// to this package's directory when tests are run with `go test`.
const fixturePath = "../../testdata/gallery/sample.html"

// jsonBlade returns a minimal gallery HTML page whose __NEXT_DATA__ script
// tag contains the supplied JSON body.
func pageWithJSON(jsonBody string) []byte {
	return []byte(`<html><body><div id="__next"><script id="__NEXT_DATA__" type="application/json">` +
		jsonBody + `</script></div></body></html>`)
}

// --- happy path ------------------------------------------------------------

func TestParsePage_Valid(t *testing.T) {
	html, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("reading fixture %s: %v", fixturePath, err)
	}

	page, err := scrape.ParsePage(html)
	if err != nil {
		t.Fatalf("ParsePage: %v", err)
	}

	if len(page.CollectorMaxBySet) != 1 {
		t.Errorf("CollectorMaxBySet has %d entries, want 1", len(page.CollectorMaxBySet))
	}
	if max, ok := page.CollectorMaxBySet["OGN"]; !ok {
		t.Errorf("CollectorMaxBySet missing key OGN")
	} else if max != 298 {
		t.Errorf("CollectorMaxBySet[OGN] = %d, want 298", max)
	}

	if len(page.CardJSONs) != 2 {
		t.Fatalf("CardJSONs has %d entries, want 2", len(page.CardJSONs))
	}

	// Verify the first card can be decoded and contains the expected name.
	var card map[string]any
	if err := json.Unmarshal(page.CardJSONs[0], &card); err != nil {
		t.Fatalf("CardJSONs[0] is not valid JSON: %v", err)
	}
	if name, ok := card["name"].(string); !ok || name != "Abandon" {
		t.Errorf("CardJSONs[0].name = %v, want 'Abandon'", card["name"])
	}

	// Verify the second card has the alternate-art id.
	var card2 map[string]any
	if err := json.Unmarshal(page.CardJSONs[1], &card2); err != nil {
		t.Fatalf("CardJSONs[1] is not valid JSON: %v", err)
	}
	if id, ok := card2["id"].(string); !ok || id != "ogn-066a-298" {
		t.Errorf("CardJSONs[1].id = %v, want 'ogn-066a-298'", card2["id"])
	}
}

// --- script tag errors -----------------------------------------------------

func TestParsePage_NoScriptTag(t *testing.T) {
	html := []byte(`<html><body>no next data here</body></html>`)

	_, err := scrape.ParsePage(html)
	if err == nil {
		t.Fatal("expected error for missing __NEXT_DATA__ script tag, got nil")
	}
	if !strings.Contains(err.Error(), "__NEXT_DATA__") {
		t.Errorf("error = %q, want it to mention __NEXT_DATA__", err.Error())
	}
}

func TestParsePage_MalformedJSON(t *testing.T) {
	html := pageWithJSON(`{not valid json}`)

	_, err := scrape.ParsePage(html)
	if err == nil {
		t.Fatal("expected error for malformed JSON, got nil")
	}
}

// --- blade structure errors -----------------------------------------------

func TestParsePage_MissingCardsItems(t *testing.T) {
	// blades[2] exists but has no "cards" key at all.
	html := pageWithJSON(`{"props":{"pageProps":{"page":{"blades":[{},{},{"sets":{"items":[{"id":"OGN","collectorNumberMax":298}]}}]}}}}`)

	_, err := scrape.ParsePage(html)
	if err == nil {
		t.Fatal("expected error when blades[2] has no cards field, got nil")
	}
	if !strings.Contains(err.Error(), "no cards") {
		t.Errorf("error = %q, want it to mention 'no cards'", err.Error())
	}
}

func TestParsePage_ZeroCards(t *testing.T) {
	// blades[2] has cards but the items array is empty.
	html := pageWithJSON(`{"props":{"pageProps":{"page":{"blades":[{},{},{"sets":{"items":[{"id":"OGN","collectorNumberMax":298}]},"cards":{"items":[]}}]}}}}`)

	_, err := scrape.ParsePage(html)
	if err == nil {
		t.Fatal("expected error for zero cards, got nil")
	}
	if !strings.Contains(err.Error(), "no cards") {
		t.Errorf("error = %q, want it to mention 'no cards'", err.Error())
	}
}

func TestParsePage_SetsButNoCards(t *testing.T) {
	// Sets metadata is present but cards.items is empty — the caller can't
	// compute overnumbered without actual card data.
	html := pageWithJSON(`{"props":{"pageProps":{"page":{"blades":[{},{},{"sets":{"items":[{"id":"OGN","collectorNumberMax":298}]},"cards":{"items":[]}}]}}}}`)

	_, err := scrape.ParsePage(html)
	if err == nil {
		t.Fatal("expected error when cards are empty despite sets being present, got nil")
	}
}

// --- </script> in card field -----------------------------------------------

func TestParsePage_ScriptTagInCardField(t *testing.T) {
	// When a card's rich text body literally contains </script>, the
	// non-greedy regex stops at the first </script> it sees, which is
	// inside the card field. The truncated JSON is then malformed and
	// parsing fails. This is an accepted limitation because the real
	// gallery never embeds a literal </script> in card text.
	html := pageWithJSON(`{"props":{"pageProps":{"page":{"blades":[{},{},{"sets":{"items":[{"id":"X","collectorNumberMax":1}]},"cards":{"items":[{"id":"x-001-001","name":"Test","text":{"richText":{"body":"<p></script>evil</p>"}}}]}}]}}}}`)

	_, err := scrape.ParsePage(html)
	if err == nil {
		t.Fatal("expected error when card text contains </script>, got nil")
	}
}

// --- edge: too few blades --------------------------------------------------

func TestParsePage_TooFewBlades(t *testing.T) {
	html := pageWithJSON(`{"props":{"pageProps":{"page":{"blades":[{},{}]}}}}`)

	_, err := scrape.ParsePage(html)
	if err == nil {
		t.Fatal("expected error for fewer than 3 blades, got nil")
	}
	if !strings.Contains(err.Error(), "at least 3 blades") {
		t.Errorf("error = %q, want it to mention 'at least 3 blades'", err.Error())
	}
}

// --- URL helper ------------------------------------------------------------

func TestClient_URL(t *testing.T) {
	t.Parallel()

	client := scrape.NewClient(scrape.ClientConfig{
		BaseURL: "https://example.com",
	})
	want := "https://example.com/en-us/card-gallery/"
	if got := client.URL(); got != want {
		t.Errorf("URL() = %q, want %q", got, want)
	}
}
