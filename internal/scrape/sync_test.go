package scrape_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/xalevagre7/riftapi/internal/health"
	"github.com/xalevagre7/riftapi/internal/scrape"
	"github.com/xalevagre7/riftapi/internal/store"
)

// --- helpers ---------------------------------------------------------------

// newTestStore returns a Store backed by a fresh SQLite file in a
// per-test temp directory. Mirrors the helper in
// internal/store/store_test.go but kept local so the two test
// packages don't share scaffolding.
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

// recordingSender captures every message it is asked to send so tests
// can assert that the right alert fired (or didn't).
type recordingSender struct {
	mu   sync.Mutex
	msgs []string
}

func (s *recordingSender) Send(_ context.Context, msg string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.msgs = append(s.msgs, msg)
	return nil
}

func (s *recordingSender) messages() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.msgs))
	copy(out, s.msgs)
	return out
}

// upstreamFromFixture serves a saved gallery HTML page at /en-us/card-gallery/.
// The path is what NewClient constructs when BaseURL points at the
// httptest server.
func upstreamFromFixture(t *testing.T, body []byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/en-us/card-gallery/" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		ua := r.Header.Get("User-Agent")
		if ua == "" {
			http.Error(w, "missing User-Agent", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// --- happy path -----------------------------------------------------------

func TestSyncer_Run_HappyPath(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", "testdata", "gallery", "sample.html"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	upstream := upstreamFromFixture(t, body)
	st := newTestStore(t)
	alert := &recordingSender{}

	syn := &scrape.Syncer{
		Store:  st,
		Client: scrape.NewClient(scrape.ClientConfig{BaseURL: upstream.URL, UserAgent: "test", Timeout: 0, MaxRetries: 0}),
		Alert:  alert,
		// No MinCount / Required: the fixture has 2 cards; we want
		// the run to pass on the cards-present check, not on a hard
		// threshold that would only pass against the live upstream.
	}
	if err := syn.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// 1. Cards were written.
	repo := st.Cards()
	count, err := repo.Count(context.Background())
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count != 2 {
		t.Errorf("Count = %d, want 2", count)
	}

	// 2. A known card from the fixture is findable.
	got, err := repo.GetByRiftboundID(context.Background(), "ogn-011")
	if err != nil {
		t.Fatalf("GetByRiftboundID(ogn-011): %v", err)
	}
	if got.Name != "Abandon" {
		t.Errorf("Name = %q, want Abandon", got.Name)
	}
	if got.SetID != "OGN" {
		t.Errorf("SetID = %q, want OGN", got.SetID)
	}

	// 3. The alternate-art card in the fixture is also present.
	alt, err := repo.GetByRiftboundID(context.Background(), "ogn-066a")
	if err != nil {
		t.Fatalf("GetByRiftboundID(ogn-066a): %v", err)
	}
	if !strings.Contains(alt.Name, "Mystic Shot") {
		t.Errorf("alt.Name = %q, want one containing 'Mystic Shot'", alt.Name)
	}

	// 4. sync_state is "ok" with the right card count.
	state, err := st.SyncState().Get(context.Background())
	if err != nil {
		t.Fatalf("SyncState.Get: %v", err)
	}
	if state.LastStatus != "ok" {
		t.Errorf("LastStatus = %q, want ok", state.LastStatus)
	}
	if state.LastCardCount != 2 {
		t.Errorf("LastCardCount = %d, want 2", state.LastCardCount)
	}

	// 5. No alerts were sent on the success path.
	if msgs := alert.messages(); len(msgs) != 0 {
		t.Errorf("expected no alerts on success, got %v", msgs)
	}
}

// --- min count check ------------------------------------------------------

func TestSyncer_Run_FailsBelowMinCount(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", "testdata", "gallery", "sample.html"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	upstream := upstreamFromFixture(t, body)
	st := newTestStore(t)
	alert := &recordingSender{}

	syn := &scrape.Syncer{
		Store:    st,
		Client:   scrape.NewClient(scrape.ClientConfig{BaseURL: upstream.URL, UserAgent: "test", MaxRetries: 0}),
		Alert:    alert,
		MinCount: 1000, // fixture only has 2 cards → must fail
	}
	err = syn.Run(context.Background())
	if err == nil {
		t.Fatalf("expected error for below-min-count, got nil")
	}
	if !strings.Contains(err.Error(), "only 2 cards parsed") {
		t.Errorf("error = %v, want one containing 'only 2 cards parsed'", err)
	}

	// sync_state is "failed" with the error message.
	state, err := st.SyncState().Get(context.Background())
	if err != nil {
		t.Fatalf("SyncState.Get: %v", err)
	}
	if state.LastStatus != "failed" {
		t.Errorf("LastStatus = %q, want failed", state.LastStatus)
	}
	if !strings.Contains(state.LastError, "only 2 cards parsed") {
		t.Errorf("LastError = %q, want it to mention the min-count failure", state.LastError)
	}

	// Alert was sent.
	if msgs := alert.messages(); len(msgs) != 1 {
		t.Errorf("expected 1 alert, got %d: %v", len(msgs), msgs)
	} else if !strings.Contains(msgs[0], "riftapi sync failed") {
		t.Errorf("alert text = %q, want one mentioning 'riftapi sync failed'", msgs[0])
	}
}

// --- noop sender on success ----------------------------------------------

func TestSyncer_Run_NoopSenderDoesNothing(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", "testdata", "gallery", "sample.html"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	upstream := upstreamFromFixture(t, body)
	st := newTestStore(t)

	syn := &scrape.Syncer{
		Store:  st,
		Client: scrape.NewClient(scrape.ClientConfig{BaseURL: upstream.URL, UserAgent: "test", MaxRetries: 0}),
		Alert:  health.NoopSender{},
	}
	if err := syn.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
}

// --- sets table ---------------------------------------------------------

func TestSyncer_Run_PopulatesSetsTable(t *testing.T) {
	// The sample.html fixture has 2 cards (Abandon and Mystic Shot),
	// both in the OGN set. After the syncer runs, the sets table
	// should have one row for OGN with card_count=2.
	body, err := os.ReadFile(filepath.Join("..", "..", "testdata", "gallery", "sample.html"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	upstream := upstreamFromFixture(t, body)
	st := newTestStore(t)

	syn := &scrape.Syncer{
		Store:  st,
		Client: scrape.NewClient(scrape.ClientConfig{BaseURL: upstream.URL, UserAgent: "test", MaxRetries: 0}),
		Alert:  &recordingSender{},
	}
	if err := syn.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	set, err := st.Sets().GetByID(context.Background(), "OGN")
	if err != nil {
		t.Fatalf("Sets.GetByID(OGN): %v", err)
	}
	if set.SetID != "OGN" {
		t.Errorf("SetID = %q, want OGN (the upstream's casing is preserved)", set.SetID)
	}
	if set.CardCount != 2 {
		t.Errorf("CardCount = %d, want 2 (both sample cards are in OGN)", set.CardCount)
	}
}

// --- fetch failure --------------------------------------------------------

func TestSyncer_Run_FailsOnFetchError(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(upstream.Close)

	st := newTestStore(t)
	alert := &recordingSender{}

	syn := &scrape.Syncer{
		Store:  st,
		Client: scrape.NewClient(scrape.ClientConfig{BaseURL: upstream.URL, UserAgent: "test", MaxRetries: 1}),
		Alert:  alert,
	}
	err := syn.Run(context.Background())
	if err == nil {
		t.Fatalf("expected error for 5xx fetch, got nil")
	}
	if !strings.Contains(err.Error(), "fetch") {
		t.Errorf("error = %v, want one mentioning 'fetch'", err)
	}

	state, _ := st.SyncState().Get(context.Background())
	if state.LastStatus != "failed" {
		t.Errorf("LastStatus = %q, want failed", state.LastStatus)
	}
	if len(alert.messages()) != 1 {
		t.Errorf("expected 1 alert, got %d", len(alert.messages()))
	}
}
