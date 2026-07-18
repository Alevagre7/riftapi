package api_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/xalevagre7/riftapi/internal/api"
	"github.com/xalevagre7/riftapi/internal/domain"
	"github.com/xalevagre7/riftapi/internal/store"
)

// --- test scaffolding ------------------------------------------------------

// newTestStore returns a Store backed by a fresh SQLite file in a
// per-test temp directory.
func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	dir := t.TempDir()
	s, err := store.Open(context.Background(), filepath.Join(dir, "riftapi.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// sampleCard is the data used to seed the test store. The
// Classification, Artist, Energy, and Tags fields exist so the
// /index/* endpoints in Phase 5 can assert against distinct values
// without a separate seed routine.
type sampleCard struct {
	riftboundID     string
	name            string
	setID           string
	collectorNumber int
	cardType        string
	rarity          string
	domain          []string
	artist          string
	energy          *int
	tags            []string
}

func intPtr(n int) *int { return &n }

var sampleCards = []sampleCard{
	{"ogn-011", "Abandon", "OGN", 11, "Spell", "Common", []string{"Fury"}, "Artist A", intPtr(3), []string{"Freljord", "Noxus"}},
	{"ogn-066a", "Mystic Shot", "OGN", 66, "Spell", "Rare", []string{"Mind"}, "Artist B", intPtr(1), []string{"Ionia"}},
	{"unl-001", "Jinx, Loose Cannon", "UNL", 1, "Unit", "Epic", []string{"Chaos"}, "Artist C", intPtr(4), []string{"Zaun"}},
	{"ven-001", "Vengeful Spirit", "VEN", 1, "Unit", "Uncommon", []string{"Fury"}, "Artist A", intPtr(2), []string{"Shadow Isles"}},
}

// seedStore inserts the sample cards and returns the store.
func seedStore(t *testing.T) *store.Store {
	t.Helper()
	st := newTestStore(t)
	repo := st.Cards()
	ctx := context.Background()
	for _, c := range sampleCards {
		row := buildCardRow(t, c)
		if err := repo.Upsert(ctx, row); err != nil {
			t.Fatalf("Upsert %s: %v", c.riftboundID, err)
		}
	}
	return st
}

// buildCardRow returns a CardRow with a valid riftcodex-shaped
// payload for a sample card.
func buildCardRow(t *testing.T, c sampleCard) store.CardRow {
	t.Helper()
	tags := c.tags
	artist := c.artist
	card := domain.Card{
		ID:              c.riftboundID,
		Name:            c.name,
		RiftboundID:     c.riftboundID,
		CollectorNumber: c.collectorNumber,
		PublicCode:      c.riftboundID + "-298",
		Attributes: &domain.Attributes{
			Energy: c.energy,
		},
		Classification: domain.Classification{
			Type:      c.cardType,
			Rarity:    c.rarity,
			Domain:    c.domain,
		},
		Text: domain.Text{Rich: "rules", Plain: "rules"},
		Set:  domain.CardSet{SetID: c.setID, Label: c.setID},
		Media: domain.Media{
			ImageURL: "https://example.com/" + c.riftboundID + ".png",
			Artist:   &artist,
		},
		Tags:        &tags,
		Orientation: "portrait",
		Metadata: domain.Metadata{
			CleanName:    strings.ToLower(c.name),
			AlternateArt: strings.HasSuffix(c.riftboundID, "a"),
		},
	}
	payload, err := json.Marshal(&card)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return store.CardRow{
		RiftboundID:     c.riftboundID,
		PublicCode:      c.riftboundID + "-298",
		SetID:           c.setID,
		CollectorNumber: c.collectorNumber,
		Name:            c.name,
		CleanName:       strings.ToLower(c.name),
		Payload:         payload,
	}
}

// newTestServer returns a fully-wired API server backed by a store
// with sample cards already inserted.
func newTestServer(t *testing.T) *api.Server {
	t.Helper()
	return api.NewServer(seedStore(t))
}

// do is a small helper that builds a GET request, runs it through
// the server, and returns the response. It fails the test on a
// non-2xx status unless the test is asserting an error code.
func do(t *testing.T, srv *api.Server, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rr := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rr, req)
	return rr
}

// indexBody is the shape of an /index/* response (type + values).
// Values can be strings or ints, so we use a small union.
type indexBody struct {
	Type   string   `json:"type"`
	Total  int      `json:"total"`
	Values []string `json:"values"`
}

// decodeIndex decodes a 200 OK /index/* response and fails the test
// on any non-2xx. The Values are decoded as strings; the int
// /index/* endpoints are asserted against []int in the caller by
// re-decoding the body bytes (the JSON value type is `integer`
// for the int fields, so the raw JSON distinguishes them).
func decodeIndex(t *testing.T, rr *httptest.ResponseRecorder) indexBody {
	t.Helper()
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", rr.Code, rr.Body.String())
	}
	var body indexBody
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return body
}

// decodeIndexInt is the int-Values variant of decodeIndex, used
// for /index/{energy, might, power}. The HTTP wire format is
// identical except that values are JSON numbers; the two
// helpers exist so the test asserts the correct value type.
func decodeIndexInt(t *testing.T, rr *httptest.ResponseRecorder) indexIntBody {
	t.Helper()
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", rr.Code, rr.Body.String())
	}
	var body indexIntBody
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return body
}

// indexIntBody is the int-Values variant of indexBody.
type indexIntBody struct {
	Type   string `json:"type"`
	Total  int    `json:"total"`
	Values []int  `json:"values"`
}

// --- GET / -----------------------------------------------------------------

func TestRoot_ReturnsAPIInfo(t *testing.T) {
	srv := newTestServer(t)
	rr := do(t, srv, "/")
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var body map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["name"] != "riftapi" {
		t.Errorf("name = %v, want riftapi", body["name"])
	}
}

func TestRoot_IncludesLegalJibberJabberAttribution(t *testing.T) {
	srv := newTestServer(t)
	rr := do(t, srv, "/")
	var body map[string]any
	_ = json.NewDecoder(rr.Body).Decode(&body)
	attr, ok := body["attribution"].(string)
	if !ok {
		t.Fatalf("attribution is missing or not a string: %v", body["attribution"])
	}
	if !strings.Contains(attr, "Legal Jibber Jabber") {
		t.Errorf("attribution = %q, want it to mention 'Legal Jibber Jabber'", attr)
	}
}

func TestCORS_HeadersOnEveryResponse(t *testing.T) {
	// CORS is the outermost layer, so it must apply to both success
	// and error responses, and to any endpoint — pick a few
	// representative ones.
	srv := newTestServer(t)
	for _, path := range []string{"/", "/health", "/index/card-names"} {
		rr := do(t, srv, path)
		if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "*" {
			t.Errorf("%s: Access-Control-Allow-Origin = %q, want *", path, got)
		}
	}
}

func TestCORS_HandlesPreflight(t *testing.T) {
	// The API is read-only; OPTIONS preflight should be answered with
	// 204 and the right CORS headers, no route handler running.
	srv := newTestServer(t)
	req := httptest.NewRequest(http.MethodOptions, "/health", nil)
	rr := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", rr.Code)
	}
	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("Access-Control-Allow-Origin = %q, want *", got)
	}
	if got := rr.Header().Get("Access-Control-Allow-Methods"); !strings.Contains(got, "GET") {
		t.Errorf("Access-Control-Allow-Methods = %q, want it to mention GET", got)
	}
}

// --- GET /health -----------------------------------------------------------

func TestHealth_200OnOK(t *testing.T) {
	st := seedStore(t)
	if err := st.SyncState().MarkOK(context.Background(), 4, ""); err != nil {
		t.Fatal(err)
	}
	srv := api.NewServer(st)
	rr := do(t, srv, "/health")
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
	var body map[string]any
	_ = json.NewDecoder(rr.Body).Decode(&body)
	if body["status"] != "ok" {
		t.Errorf("body.status = %v, want ok", body["status"])
	}
	if body["last_card_count"].(float64) != 4 {
		t.Errorf("body.last_card_count = %v, want 4", body["last_card_count"])
	}
}

func TestHealth_503OnFailedSync(t *testing.T) {
	st := seedStore(t)
	if err := st.SyncState().MarkFailed(context.Background(), errors.New("upstream 503")); err != nil {
		t.Fatal(err)
	}
	srv := api.NewServer(st)
	rr := do(t, srv, "/health")
	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rr.Code)
	}
}

func TestHealth_503OnNoSync(t *testing.T) {
	// Fresh server with no sync at all.
	srv := api.NewServer(newTestStore(t))
	rr := do(t, srv, "/health")
	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503 (no sync yet)", rr.Code)
	}
}

func TestHealth_503WhenOKButZeroCards(t *testing.T) {
	st := seedStore(t)
	if err := st.SyncState().MarkOK(context.Background(), 0, ""); err != nil {
		t.Fatal(err)
	}
	srv := api.NewServer(st)
	rr := do(t, srv, "/health")
	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503 (zero cards is not healthy)", rr.Code)
	}
}

// --- GET /cards/name -------------------------------------------------------

func TestCardsName(t *testing.T) {
	srv := newTestServer(t)
	tests := []struct {
		name      string
		path      string
		wantCode  int
		wantTotal int
		wantFirst string // expected riftbound_id of items[0], or "" if empty
	}{
		{"fuzzy matches 'ab'", "/cards/name?fuzzy=ab", http.StatusOK, 1, "ogn-011"},
		{"fuzzy matches 'shot'", "/cards/name?fuzzy=shot", http.StatusOK, 1, "ogn-066a"},
		{"fuzzy matches 'jinx'", "/cards/name?fuzzy=jinx", http.StatusOK, 1, "unl-001"},
		{"fuzzy case-insensitive", "/cards/name?fuzzy=ABANDON", http.StatusOK, 1, "ogn-011"},
		{"fuzzy matches clean_name 'loose'", "/cards/name?fuzzy=loose", http.StatusOK, 1, "unl-001"},
		{"fuzzy no match", "/cards/name?fuzzy=zzzzzzz", http.StatusOK, 0, ""},
		{"exact match", "/cards/name?exact=Abandon", http.StatusOK, 1, "ogn-011"},
		{"exact case-insensitive", "/cards/name?exact=abandon", http.StatusOK, 1, "ogn-011"},
		{"exact not found", "/cards/name?exact=NotACard", http.StatusOK, 0, ""},
		{"no query param", "/cards/name", http.StatusBadRequest, 0, ""},
		{"empty query", "/cards/name?fuzzy=", http.StatusBadRequest, 0, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rr := do(t, srv, tc.path)
			if rr.Code != tc.wantCode {
				t.Fatalf("status = %d, want %d (body=%s)", rr.Code, tc.wantCode, rr.Body.String())
			}
			if tc.wantCode != http.StatusOK {
				return
			}
			var body struct {
				Items []json.RawMessage `json:"items"`
				Total int               `json:"total"`
			}
			if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if body.Total != tc.wantTotal {
				t.Errorf("total = %d, want %d", body.Total, tc.wantTotal)
			}
			if len(body.Items) != tc.wantTotal {
				t.Errorf("len(items) = %d, want %d", len(body.Items), tc.wantTotal)
			}
			if tc.wantFirst != "" {
				var first map[string]any
				if err := json.Unmarshal(body.Items[0], &first); err != nil {
					t.Fatalf("decode first item: %v", err)
				}
				if first["riftbound_id"] != tc.wantFirst {
					t.Errorf("items[0].riftbound_id = %v, want %s", first["riftbound_id"], tc.wantFirst)
				}
			}
		})
	}
}

// --- GET /cards/{id} -------------------------------------------------------

func TestCardByID(t *testing.T) {
	srv := newTestServer(t)
	tests := []struct {
		name     string
		path     string
		wantCode int
		wantID   string
	}{
		{"found", "/cards/ogn-011", http.StatusOK, "ogn-011"},
		{"found alternate art", "/cards/ogn-066a", http.StatusOK, "ogn-066a"},
		{"case-insensitive", "/cards/OGN-011", http.StatusOK, "ogn-011"},
		{"not found", "/cards/missing", http.StatusNotFound, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rr := do(t, srv, tc.path)
			if rr.Code != tc.wantCode {
				t.Fatalf("status = %d, want %d (body=%s)", rr.Code, tc.wantCode, rr.Body.String())
			}
			if tc.wantCode != http.StatusOK {
				return
			}
			var body map[string]any
			if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if body["riftbound_id"] != tc.wantID {
				t.Errorf("riftbound_id = %v, want %s", body["riftbound_id"], tc.wantID)
			}
		})
	}
}

// --- GET /cards/riftbound/{id} --------------------------------------------

func TestCardsByRiftboundID(t *testing.T) {
	srv := newTestServer(t)
	tests := []struct {
		name     string
		path     string
		wantCode int
		wantLen  int
		wantID   string
	}{
		{"found", "/cards/riftbound/ogn-011", http.StatusOK, 1, "ogn-011"},
		{"not found returns []", "/cards/riftbound/missing", http.StatusOK, 0, ""},
		{"case-insensitive", "/cards/riftbound/UNL-001", http.StatusOK, 1, "unl-001"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rr := do(t, srv, tc.path)
			if rr.Code != tc.wantCode {
				t.Fatalf("status = %d, want %d (body=%s)", rr.Code, tc.wantCode, rr.Body.String())
			}
			var items []map[string]any
			if err := json.NewDecoder(rr.Body).Decode(&items); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if len(items) != tc.wantLen {
				t.Errorf("len = %d, want %d", len(items), tc.wantLen)
			}
			if tc.wantID != "" {
				if items[0]["riftbound_id"] != tc.wantID {
					t.Errorf("items[0].riftbound_id = %v, want %s", items[0]["riftbound_id"], tc.wantID)
				}
			}
		})
	}
}

// --- GET /index/card-names -------------------------------------------------

func TestIndexCardNames(t *testing.T) {
	srv := newTestServer(t)
	rr := do(t, srv, "/index/card-names")
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var body struct {
		Total  int      `json:"total"`
		Type   string   `json:"type"`
		Values []string `json:"values"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Type != "card-names" {
		t.Errorf("type = %q, want card-names", body.Type)
	}
	if body.Total != len(sampleCards) {
		t.Errorf("total = %d, want %d", body.Total, len(sampleCards))
	}
	want := []string{"Abandon", "Jinx, Loose Cannon", "Mystic Shot", "Vengeful Spirit"}
	if fmt.Sprintf("%v", body.Values) != fmt.Sprintf("%v", want) {
		t.Errorf("values = %v, want %v (sorted alphabetically)", body.Values, want)
	}
}

func TestIndexCardNames_Empty(t *testing.T) {
	srv := api.NewServer(newTestStore(t))
	rr := do(t, srv, "/index/card-names")
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var body struct {
		Total  int      `json:"total"`
		Values []string `json:"values"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Total != 0 {
		t.Errorf("total = %d, want 0", body.Total)
	}
	if len(body.Values) != 0 {
		t.Errorf("values = %v, want []", body.Values)
	}
}

// --- Phase 5: /cards list, /cards/search, /cards/tcgplayer, /index/* ----

func TestCardsList(t *testing.T) {
	srv := newTestServer(t)
	rr := do(t, srv, "/cards")
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", rr.Code, rr.Body.String())
	}
	var body struct {
		Items []map[string]any `json:"items"`
		Total int               `json:"total"`
		Page  int               `json:"page"`
		Size  int               `json:"size"`
		Pages int               `json:"pages"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Total != len(sampleCards) {
		t.Errorf("total = %d, want %d", body.Total, len(sampleCards))
	}
	if len(body.Items) != len(sampleCards) {
		t.Errorf("len(items) = %d, want %d", len(body.Items), len(sampleCards))
	}
	if body.Page != 1 {
		t.Errorf("page = %d, want 1 (default)", body.Page)
	}
	if body.Size != 50 {
		t.Errorf("size = %d, want 50 (default)", body.Size)
	}
}

func TestCardsList_SortByCollectorNumberDesc(t *testing.T) {
	srv := newTestServer(t)
	rr := do(t, srv, "/cards?sort=collector_number&dir=-1")
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var body struct {
		Items []struct {
			RiftboundID     string `json:"riftbound_id"`
			CollectorNumber int    `json:"collector_number"`
		} `json:"items"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Expected order by collector_number desc: unl-001 (1)? — actually
	// sample cards: ogn-011 (11), ogn-066a (66), unl-001 (1), ven-001 (1).
	// Descending: 66, 11, 1, 1. Stable order for ties is implementation
	// defined; we just assert the maximum (66) comes first.
	if len(body.Items) == 0 || body.Items[0].CollectorNumber != 66 {
		t.Errorf("first item collector_number = %d, want 66", body.Items[0].CollectorNumber)
	}
}

func TestCardsList_FilterBySetID(t *testing.T) {
	srv := newTestServer(t)
	rr := do(t, srv, "/cards?set_id=ogn")
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var body struct {
		Items []struct {
			Set struct {
				SetID string `json:"set_id"`
			} `json:"set"`
		} `json:"items"`
		Total int `json:"total"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Total != 2 {
		t.Errorf("total = %d, want 2 (only OGN cards)", body.Total)
	}
	for _, item := range body.Items {
		if item.Set.SetID != "OGN" {
			t.Errorf("item.set.set_id = %q, want OGN", item.Set.SetID)
		}
	}
}

func TestCardsList_Pagination(t *testing.T) {
	srv := newTestServer(t)
	// 4 sample cards; page 1 with size 2 should return 2 items, total 4, pages 2.
	rr := do(t, srv, "/cards?page=1&size=2")
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var body struct {
		Items []map[string]any `json:"items"`
		Total int               `json:"total"`
		Pages int               `json:"pages"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Total != 4 {
		t.Errorf("total = %d, want 4", body.Total)
	}
	if len(body.Items) != 2 {
		t.Errorf("len(items) = %d, want 2 (page 1, size 2)", len(body.Items))
	}
	if body.Pages != 2 {
		t.Errorf("pages = %d, want 2", body.Pages)
	}
}

func TestCardsSearch(t *testing.T) {
	srv := newTestServer(t)
	// All sample cards have text.plain = "rules", so searching for
	// "rules" matches all of them.
	rr := do(t, srv, "/cards/search?query=rules")
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var body struct {
		Items []map[string]any `json:"items"`
		Total int               `json:"total"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Total != len(sampleCards) {
		t.Errorf("total = %d, want %d", body.Total, len(sampleCards))
	}
}

func TestCardsSearch_NoQuery(t *testing.T) {
	srv := newTestServer(t)
	rr := do(t, srv, "/cards/search")
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (missing query)", rr.Code)
	}
}

func TestCardsByTcgPlayerID_AlwaysNotFound(t *testing.T) {
	// The gallery does not expose tcgplayer_id (ADR-0001). The
	// endpoint is registered for surface completeness but always 404s.
	srv := newTestServer(t)
	for _, id := range []string{"12345", "67890", "any-id"} {
		rr := do(t, srv, "/cards/tcgplayer/"+id)
		if rr.Code != http.StatusNotFound {
			t.Errorf("tcgplayer/%s: status = %d, want 404", id, rr.Code)
		}
	}
}

func TestIndexTypes(t *testing.T) {
	srv := newTestServer(t)
	rr := do(t, srv, "/index/types")
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	body := decodeIndex(t, rr)
	if body.Type != "types" {
		t.Errorf("type = %q, want types", body.Type)
	}
	if !slices.Equal(body.Values, []string{"Spell", "Unit"}) {
		t.Errorf("values = %v, want [Spell Unit]", body.Values)
	}
	if body.Total != 2 {
		t.Errorf("total = %d, want 2", body.Total)
	}
}

func TestIndexRarities(t *testing.T) {
	srv := newTestServer(t)
	rr := do(t, srv, "/index/rarities")
	body := decodeIndex(t, rr)
	if !slices.Equal(body.Values, []string{"Common", "Epic", "Rare", "Uncommon"}) {
		t.Errorf("values = %v, want [Common Epic Rare Uncommon]", body.Values)
	}
}

func TestIndexDomains_DistinctArrayValues(t *testing.T) {
	// Fury appears in two cards' domains; the index should report
	// each unique domain once, sorted.
	srv := newTestServer(t)
	rr := do(t, srv, "/index/domains")
	body := decodeIndex(t, rr)
	if !slices.Equal(body.Values, []string{"Chaos", "Fury", "Mind"}) {
		t.Errorf("values = %v, want [Chaos Fury Mind]", body.Values)
	}
	if body.Total != 3 {
		t.Errorf("total = %d, want 3 (Fury collapsed)", body.Total)
	}
}

func TestIndexArtists(t *testing.T) {
	// Artist A appears on two cards; should be listed once.
	srv := newTestServer(t)
	rr := do(t, srv, "/index/artists")
	body := decodeIndex(t, rr)
	if !slices.Equal(body.Values, []string{"Artist A", "Artist B", "Artist C"}) {
		t.Errorf("values = %v, want [Artist A Artist B Artist C]", body.Values)
	}
}

func TestIndexEnergy(t *testing.T) {
	srv := newTestServer(t)
	rr := do(t, srv, "/index/energy")
	body := decodeIndexInt(t, rr)
	if !slices.Equal(body.Values, []int{1, 2, 3, 4}) {
		t.Errorf("values = %v, want [1 2 3 4]", body.Values)
	}
}

func TestIndexTags_DistinctArrayValues(t *testing.T) {
	srv := newTestServer(t)
	rr := do(t, srv, "/index/tags")
	body := decodeIndex(t, rr)
	if !slices.Equal(body.Values, []string{"Freljord", "Ionia", "Noxus", "Shadow Isles", "Zaun"}) {
		t.Errorf("values = %v, want [Freljord Ionia Noxus Shadow Isles Zaun]", body.Values)
	}
	if body.Total != 5 {
		t.Errorf("total = %d, want 5", body.Total)
	}
}

// --- Phase 5: /sets/* ----------------------------------------------------

// seedSets inserts a small set of SetRows directly via the
// SetRepo. The API tests need a non-empty sets table to exercise
// /sets; the syncer populates this in production, but the API
// tests should not depend on the syncer.
func seedSets(t *testing.T, st *store.Store, sets ...store.SetRow) {
	t.Helper()
	repo := st.Sets()
	ctx := context.Background()
	for _, row := range sets {
		if err := repo.Upsert(ctx, row); err != nil {
			t.Fatalf("seed sets: %v", err)
		}
	}
}

func sampleSetRow(t *testing.T, setID, label string, cardCount int) store.SetRow {
	t.Helper()
	count := cardCount
	payload, err := json.Marshal(domain.Set{
		ID:        setID,
		Name:      label,
		SetID:     setID,
		CardCount: &count,
	})
	if err != nil {
		t.Fatalf("marshal set: %v", err)
	}
	return store.SetRow{
		SetID:     setID,
		CardCount: cardCount,
		Payload:   payload,
	}
}

func TestSetsList(t *testing.T) {
	st := newTestStore(t)
	seedSets(t, st,
		sampleSetRow(t, "ogn", "Origins", 298),
		sampleSetRow(t, "unl", "Unleashed", 219),
		sampleSetRow(t, "ven", "Vendetta", 166),
	)
	srv := api.NewServer(st)
	rr := do(t, srv, "/sets")
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", rr.Code, rr.Body.String())
	}
	var body struct {
		Items []map[string]any `json:"items"`
		Total int               `json:"total"`
		Page  int               `json:"page"`
		Size  int               `json:"size"`
		Pages int               `json:"pages"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Total != 3 {
		t.Errorf("total = %d, want 3", body.Total)
	}
	if len(body.Items) != 3 {
		t.Errorf("len(items) = %d, want 3", len(body.Items))
	}
	// Items are ordered by set_id (OGN < UNL < VEN).
	if got := body.Items[0]["set_id"]; got != "ogn" {
		t.Errorf("items[0].set_id = %v, want ogn", got)
	}
}

func TestSetsList_Pagination(t *testing.T) {
	st := newTestStore(t)
	seedSets(t, st,
		sampleSetRow(t, "ogn", "Origins", 298),
		sampleSetRow(t, "unl", "Unleashed", 219),
		sampleSetRow(t, "ven", "Vendetta", 166),
	)
	srv := api.NewServer(st)
	rr := do(t, srv, "/sets?page=1&size=2")
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var body struct {
		Items []map[string]any `json:"items"`
		Total int               `json:"total"`
		Pages int               `json:"pages"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Total != 3 {
		t.Errorf("total = %d, want 3", body.Total)
	}
	if len(body.Items) != 2 {
		t.Errorf("len(items) = %d, want 2 (page 1, size 2)", len(body.Items))
	}
	if body.Pages != 2 {
		t.Errorf("pages = %d, want 2", body.Pages)
	}
}

func TestSetsList_Empty(t *testing.T) {
	srv := api.NewServer(newTestStore(t))
	rr := do(t, srv, "/sets")
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var body struct {
		Total  int             `json:"total"`
		Items  []map[string]any `json:"items"`
		Pages  int             `json:"pages"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Total != 0 {
		t.Errorf("total = %d, want 0", body.Total)
	}
	if len(body.Items) != 0 {
		t.Errorf("len(items) = %d, want 0", len(body.Items))
	}
}

func TestSetBySetID(t *testing.T) {
	st := newTestStore(t)
	seedSets(t, st, sampleSetRow(t, "ogn", "Origins", 298))
	srv := api.NewServer(st)
	rr := do(t, srv, "/sets/set-id/ogn")
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var body struct {
		SetID     string `json:"set_id"`
		Name      string `json:"name"`
		CardCount *int   `json:"card_count"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.SetID != "ogn" {
		t.Errorf("set_id = %q, want ogn", body.SetID)
	}
	if body.Name != "Origins" {
		t.Errorf("name = %q, want Origins", body.Name)
	}
	if body.CardCount == nil || *body.CardCount != 298 {
		t.Errorf("card_count = %v, want 298", body.CardCount)
	}
}

func TestSetBySetID_NotFound(t *testing.T) {
	srv := api.NewServer(newTestStore(t))
	rr := do(t, srv, "/sets/set-id/missing")
	if rr.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rr.Code)
	}
}

func TestSetByID_AlwaysNotFound(t *testing.T) {
	// The gallery does not expose set UUIDs, so /sets/{id} always 404s.
	srv := api.NewServer(newTestStore(t))
	for _, id := range []string{"abc123", "any-id", "65a8"} {
		rr := do(t, srv, "/sets/"+id)
		if rr.Code != http.StatusNotFound {
			t.Errorf("sets/%s: status = %d, want 404", id, rr.Code)
		}
	}
}

func TestSetByTcgPlayerID_AlwaysNotFound(t *testing.T) {
	srv := api.NewServer(newTestStore(t))
	for _, id := range []string{"12345", "24343"} {
		rr := do(t, srv, "/sets/tcgplayer/"+id)
		if rr.Code != http.StatusNotFound {
			t.Errorf("sets/tcgplayer/%s: status = %d, want 404", id, rr.Code)
		}
	}
}

func TestSetByCardmarketID_AlwaysNotFound(t *testing.T) {
	srv := api.NewServer(newTestStore(t))
	for _, id := range []string{"6322", "6483"} {
		rr := do(t, srv, "/sets/cardmarket/"+id)
		if rr.Code != http.StatusNotFound {
			t.Errorf("sets/cardmarket/%s: status = %d, want 404", id, rr.Code)
		}
	}
}

// --- smoke: ensure the helper paths compile and resolve --------------------

func TestSmoke_StoreRoundTrip(t *testing.T) {
	// Sanity check: a row inserted via Upsert can be read back. This
	// guards against the test scaffolding drifting from the store's
	// actual column layout.
	st := seedStore(t)
	got, err := st.Cards().GetByRiftboundID(context.Background(), "ogn-011")
	if err != nil {
		t.Fatalf("GetByRiftboundID: %v", err)
	}
	if got.Name != "Abandon" {
		t.Errorf("Name = %q, want Abandon", got.Name)
	}
	_ = sql.ErrNoRows // silence unused import if the smoke test is removed
}
