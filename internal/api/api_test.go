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

// sampleCard is the data used to seed the test store.
type sampleCard struct {
	riftboundID     string
	name            string
	setID           string
	collectorNumber int
}

var sampleCards = []sampleCard{
	{"ogn-011", "Abandon", "OGN", 11},
	{"ogn-066a", "Mystic Shot", "OGN", 66},
	{"unl-001", "Jinx, Loose Cannon", "UNL", 1},
	{"ven-001", "Vengeful Spirit", "VEN", 1},
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
	card := domain.Card{
		ID:              c.riftboundID,
		Name:            c.name,
		RiftboundID:     c.riftboundID,
		CollectorNumber: c.collectorNumber,
		PublicCode:      c.riftboundID + "-298",
		Set:             domain.CardSet{SetID: c.setID, Label: c.setID},
		Classification:  domain.Classification{Type: "Unit", Rarity: "Common", Domain: []string{}},
		Text:            domain.Text{Rich: "rules", Plain: "rules"},
		Media:           domain.Media{ImageURL: "https://example.com/" + c.riftboundID + ".png"},
		Metadata: domain.Metadata{
			CleanName:    strings.ToLower(c.name),
			AlternateArt: strings.HasSuffix(c.riftboundID, "a"),
		},
		Orientation: "portrait",
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
