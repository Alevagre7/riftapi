package scrape

import (
	"encoding/json"
	"fmt"
	"regexp"
)

// Page is the parsed result of a gallery HTML page. It is a thin
// intermediate shape: the per-card JSON is kept as raw messages
// (so the transformer can decode each one independently and surface
// per-card parse errors) and the set metadata is pre-extracted into
// a map for the transformer's overnumbered check.
type Page struct {
	// CollectorMaxBySet maps a set_id (e.g. "OGN") to the set's
	// collectorNumberMax (e.g. 298). Sourced from
	// blades[2].sets.items[].collectorNumberMax.
	CollectorMaxBySet map[string]int

	// CardJSONs is the raw JSON of each card, in upstream order.
	// Sourced from blades[2].cards.items[].
	CardJSONs []json.RawMessage
}

var nextDataRe = regexp.MustCompile(
	`(?s)<script[^>]*id="__NEXT_DATA__"[^>]*type="application/json"[^>]*>(.*?)</script>`,
)

// ParsePage extracts the __NEXT_DATA__ JSON from the gallery HTML
// and returns a Page. The expected shape is:
//
//	<script id="__NEXT_DATA__" type="application/json">
//	  {"props":{"pageProps":{"page":{"blades":[
//	    ...3 blades...,
//	    {"type":"...","sets":{"items":[
//	      {"id":"OGN","collectorNumberMax":298,...},
//	      ...
//	    ]},"cards":{"items":[
//	      {"id":"ogn-011-298","name":"Abandon",...},
//	      ...
//	    ]}}
//	  ]}}}}
//	</script>
//
// The page property's blades array is 3 elements long; the third
// (index 2) is the one with sets and cards. The first two may be
// filter / header blades and their shape is not interesting to us.
//
// Returns an error when:
//   - the __NEXT_DATA__ script tag is missing,
//   - the embedded JSON is malformed,
//   - the page.blades[2] shape is missing the expected fields,
//   - the page is structurally valid but contains no cards.
func ParsePage(html []byte) (*Page, error) {
	match := nextDataRe.FindSubmatch(html)
	if match == nil {
		return nil, fmt.Errorf("__NEXT_DATA__ script tag not found")
	}

	// Unmarshal the outer shape to reach the blades array.
	var outer struct {
		Props struct {
			PageProps struct {
				Page struct {
					Blades []json.RawMessage `json:"blades"`
				} `json:"page"`
			} `json:"pageProps"`
		} `json:"props"`
	}
	if err := json.Unmarshal(match[1], &outer); err != nil {
		return nil, fmt.Errorf("decoding __NEXT_DATA__ JSON: %w", err)
	}

	if len(outer.Props.PageProps.Page.Blades) < 3 {
		return nil, fmt.Errorf(
			"expected at least 3 blades in page, got %d",
			len(outer.Props.PageProps.Page.Blades),
		)
	}

	// Decode the gallery blade (index 2).
	var blade struct {
		Sets *struct {
			Items []struct {
				ID                 string `json:"id"`
				CollectorNumberMax int    `json:"collectorNumberMax"`
			} `json:"items"`
		} `json:"sets"`
		Cards *struct {
			Items []json.RawMessage `json:"items"`
		} `json:"cards"`
	}
	if err := json.Unmarshal(outer.Props.PageProps.Page.Blades[2], &blade); err != nil {
		return nil, fmt.Errorf("decoding gallery blade (blades[2]): %w", err)
	}

	if blade.Cards == nil || len(blade.Cards.Items) == 0 {
		return nil, fmt.Errorf("gallery blade (blades[2]) contains no cards")
	}

	page := &Page{
		CardJSONs: blade.Cards.Items,
	}

	if blade.Sets != nil {
		page.CollectorMaxBySet = make(map[string]int, len(blade.Sets.Items))
		for _, s := range blade.Sets.Items {
			page.CollectorMaxBySet[s.ID] = s.CollectorNumberMax
		}
	} else {
		page.CollectorMaxBySet = make(map[string]int)
	}

	return page, nil
}
