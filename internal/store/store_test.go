package store_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/xalevagre7/riftapi/internal/domain"
	"github.com/xalevagre7/riftapi/internal/store"
)

// --- helpers ---------------------------------------------------------------

// newTestStore returns a Store backed by a fresh SQLite file in a
// per-test temp directory. The store is closed automatically when the
// test ends.
func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "riftapi.db")
	s, err := store.Open(context.Background(), path)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func sampleCard(t *testing.T, id, name, setID string, n int) (store.CardRow, []byte) {
	t.Helper()
	card := domain.Card{
		ID:              id,
		Name:            name,
		RiftboundID:     id,
		CollectorNumber: n,
		Classification:  domain.Classification{Type: "Unit", Rarity: "Common", Domain: []string{"Fury"}},
		Text:            domain.Text{Rich: "<p>rules</p>", Plain: "rules"},
		Set:             domain.CardSet{SetID: setID, Label: setID},
		Media:           domain.Media{ImageURL: "https://example/" + id + ".png"},
		Metadata:        domain.Metadata{CleanName: name, AlternateArt: false, Overnumbered: false, Signature: false},
		Orientation:     "portrait",
	}
	payload, err := json.Marshal(&card)
	if err != nil {
		t.Fatalf("marshal card: %v", err)
	}
	return store.CardRow{
		RiftboundID:     id,
		PublicCode:      id + "/" + "298",
		SetID:           setID,
		CollectorNumber: n,
		Name:            name,
		CleanName:       strings.ToLower(name),
		Payload:         payload,
	}, payload
}

func sampleSet(t *testing.T, setID string, count int) (store.SetRow, []byte) {
	t.Helper()
	set := domain.Set{
		ID:        setID,
		Name:      setID,
		SetID:     setID,
		CardCount: &count,
	}
	payload, err := json.Marshal(&set)
	if err != nil {
		t.Fatalf("marshal set: %v", err)
	}
	return store.SetRow{
		SetID:     setID,
		CardCount: count,
		Payload:   payload,
	}, payload
}

// --- Open + WAL ------------------------------------------------------------

func TestOpen_SetsWALMode(t *testing.T) {
	s := newTestStore(t)
	mode, err := s.JournalMode(context.Background())
	if err != nil {
		t.Fatalf("JournalMode: %v", err)
	}
	if mode != "wal" {
		t.Errorf("expected journal_mode=wal, got %q", mode)
	}
}

func TestOpen_PathIsAccessible(t *testing.T) {
	s := newTestStore(t)
	if s.Path() == "" {
		t.Errorf("expected non-empty path")
	}
}

func TestMigrate_IsIdempotent(t *testing.T) {
	s := newTestStore(t)
	// The store already ran Migrate() inside Open. Calling Migrate
	// again must succeed and leave the schema_migrations table
	// untouched.
	if err := store.Migrate(context.Background(), s.DB()); err != nil {
		t.Fatalf("Migrate on already-migrated DB: %v", err)
	}
	var n int
	if err := s.DB().QueryRow(`SELECT COUNT(1) FROM schema_migrations`).Scan(&n); err != nil {
		t.Fatalf("count schema_migrations: %v", err)
	}
	if n != 1 {
		t.Errorf("expected exactly 1 applied migration, got %d", n)
	}
}

func TestMigrate_CreatesAllTables(t *testing.T) {
	s := newTestStore(t)
	required := []string{"cards", "sets", "sync_state", "schema_migrations"}
	for _, tbl := range required {
		var n int
		err := s.DB().QueryRow(
			`SELECT COUNT(1) FROM sqlite_master WHERE type='table' AND name=?`, tbl,
		).Scan(&n)
		if err != nil {
			t.Fatalf("check table %s: %v", tbl, err)
		}
		if n != 1 {
			t.Errorf("expected table %q to exist", tbl)
		}
	}
}

// --- CardRepo --------------------------------------------------------------

func TestCardRepo_UpsertAndGetByRiftboundID(t *testing.T) {
	s := newTestStore(t)
	repo := store.NewCardRepo(s.DB())
	row, payload := sampleCard(t, "ogn-011", "Abandon", "OGN", 11)

	ctx := context.Background()
	if err := repo.Upsert(ctx, row); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	got, err := repo.GetByRiftboundID(ctx, "ogn-011")
	if err != nil {
		t.Fatalf("GetByRiftboundID: %v", err)
	}
	if got.RiftboundID != "ogn-011" {
		t.Errorf("RiftboundID = %q, want ogn-011", got.RiftboundID)
	}
	if got.Name != "Abandon" {
		t.Errorf("Name = %q, want Abandon", got.Name)
	}
	if got.CleanName != "abandon" {
		t.Errorf("CleanName = %q, want abandon", got.CleanName)
	}
	if got.SetID != "OGN" {
		t.Errorf("SetID = %q, want OGN", got.SetID)
	}
	if got.CollectorNumber != 11 {
		t.Errorf("CollectorNumber = %d, want 11", got.CollectorNumber)
	}
	if string(got.Payload) != string(payload) {
		t.Errorf("payload mismatch: got %s, want %s", got.Payload, payload)
	}
}

func TestCardRepo_UpsertReplacesExisting(t *testing.T) {
	s := newTestStore(t)
	repo := store.NewCardRepo(s.DB())
	ctx := context.Background()

	row1, _ := sampleCard(t, "ogn-011", "Abandon", "OGN", 11)
	if err := repo.Upsert(ctx, row1); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	// Replace the same riftbound_id with updated payload.
	row2, _ := sampleCard(t, "ogn-011", "Abandon (Updated)", "OGN", 11)
	if err := repo.Upsert(ctx, row2); err != nil {
		t.Fatalf("Upsert replace: %v", err)
	}

	got, err := repo.GetByRiftboundID(ctx, "ogn-011")
	if err != nil {
		t.Fatalf("GetByRiftboundID: %v", err)
	}
	if got.Name != "Abandon (Updated)" {
		t.Errorf("Name = %q, want updated value (replacement did not happen)", got.Name)
	}
	if n, _ := repo.Count(ctx); n != 1 {
		t.Errorf("Count = %d, want 1 (replacement should not duplicate)", n)
	}
}

func TestCardRepo_GetByRiftboundID_NotFound(t *testing.T) {
	s := newTestStore(t)
	repo := store.NewCardRepo(s.DB())
	_, err := repo.GetByRiftboundID(context.Background(), "missing")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected sql.ErrNoRows, got %v", err)
	}
}

func TestCardRepo_GetByRiftboundID_CaseInsensitive(t *testing.T) {
	s := newTestStore(t)
	repo := store.NewCardRepo(s.DB())
	row, _ := sampleCard(t, "ogn-011", "Abandon", "OGN", 11)
	if err := repo.Upsert(context.Background(), row); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if _, err := repo.GetByRiftboundID(context.Background(), "OGN-011"); err != nil {
		t.Errorf("case-insensitive lookup failed: %v", err)
	}
}

func TestCardRepo_GetByName(t *testing.T) {
	s := newTestStore(t)
	repo := store.NewCardRepo(s.DB())
	ctx := context.Background()
	row, _ := sampleCard(t, "ogn-011", "Abandon", "OGN", 11)
	if err := repo.Upsert(ctx, row); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	got, err := repo.GetByName(ctx, "Abandon")
	if err != nil {
		t.Fatalf("GetByName: %v", err)
	}
	if got.RiftboundID != "ogn-011" {
		t.Errorf("RiftboundID = %q, want ogn-011", got.RiftboundID)
	}
}

func TestCardRepo_SearchByName(t *testing.T) {
	s := newTestStore(t)
	repo := store.NewCardRepo(s.DB())
	ctx := context.Background()
	for _, c := range []struct{ id, name string }{
		{"ogn-001", "Abandon"},
		{"ogn-002", "Abyssal"},
		{"ogn-003", "Master Yi"},
		{"ogn-004", "Fury Charger"},
	} {
		row, _ := sampleCard(t, c.id, c.name, "OGN", 0)
		if err := repo.Upsert(ctx, row); err != nil {
			t.Fatalf("Upsert %s: %v", c.id, err)
		}
	}

	// "ab" should match "Abandon" and "Abyssal" but not the others.
	got, err := repo.SearchByName(ctx, "ab", 0)
	if err != nil {
		t.Fatalf("SearchByName: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("expected 2 matches for 'ab', got %d: %+v", len(got), names(got))
	}
}

func TestCardRepo_SearchByName_AlsoMatchesCleanName(t *testing.T) {
	// "Jinx, Loose Cannon" is stored with name="Jinx, Loose Cannon" and
	// clean_name="jinx loose cannon". A search for "loose" should
	// match the clean_name even though the literal name has a comma.
	s := newTestStore(t)
	repo := store.NewCardRepo(s.DB())
	row, _ := sampleCard(t, "ogn-066", "Jinx, Loose Cannon", "OGN", 66)
	if err := repo.Upsert(context.Background(), row); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	got, err := repo.SearchByName(context.Background(), "loose", 0)
	if err != nil {
		t.Fatalf("SearchByName: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("expected 1 match for 'loose' via clean_name, got %d", len(got))
	}
}

func TestCardRepo_SearchByName_RespectsLimit(t *testing.T) {
	s := newTestStore(t)
	repo := store.NewCardRepo(s.DB())
	ctx := context.Background()
	for i := 1; i <= 5; i++ {
		row, _ := sampleCard(t, "ogn-00"+strconv.Itoa(i), "Aether "+strconv.Itoa(i), "OGN", i)
		if err := repo.Upsert(ctx, row); err != nil {
			t.Fatalf("Upsert: %v", err)
		}
	}
	got, err := repo.SearchByName(ctx, "Aether", 2)
	if err != nil {
		t.Fatalf("SearchByName: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("expected limit=2 to return 2, got %d", len(got))
	}
}

func TestCardRepo_ListNames_Sorted(t *testing.T) {
	s := newTestStore(t)
	repo := store.NewCardRepo(s.DB())
	ctx := context.Background()
	for _, c := range []struct{ id, name string }{
		{"ogn-001", "Zephyr"},
		{"ogn-002", "Abandon"},
		{"ogn-003", "Master Yi"},
	} {
		row, _ := sampleCard(t, c.id, c.name, "OGN", 0)
		if err := repo.Upsert(ctx, row); err != nil {
			t.Fatalf("Upsert: %v", err)
		}
	}
	names, err := repo.ListNames(ctx)
	if err != nil {
		t.Fatalf("ListNames: %v", err)
	}
	want := []string{"Abandon", "Master Yi", "Zephyr"}
	if !slices.Equal(names, want) {
		t.Errorf("ListNames = %v, want %v", names, want)
	}
}

func TestCardRepo_Count(t *testing.T) {
	s := newTestStore(t)
	repo := store.NewCardRepo(s.DB())
	ctx := context.Background()
	if n, _ := repo.Count(ctx); n != 0 {
		t.Errorf("empty Count = %d, want 0", n)
	}
	for i := 1; i <= 3; i++ {
		row, _ := sampleCard(t, "ogn-00"+strconv.Itoa(i), "X"+strconv.Itoa(i), "OGN", i)
		if err := repo.Upsert(ctx, row); err != nil {
			t.Fatalf("Upsert: %v", err)
		}
	}
	if n, _ := repo.Count(ctx); n != 3 {
		t.Errorf("Count = %d, want 3", n)
	}
}

func TestCardRepo_GetRandomCard(t *testing.T) {
	s := newTestStore(t)
	repo := store.NewCardRepo(s.DB())
	ctx := context.Background()

	// Empty store: ErrNoRows, not a panic.
	if _, err := repo.GetRandomCard(ctx); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("empty store: GetRandomCard = %v, want sql.ErrNoRows", err)
	}

	// Seed three cards and call repeatedly. Across N calls we
	// expect to see at least two distinct ids — strict-uniformity
	// tests are flaky and meaningless; we only assert that the
	// function is actually picking from the seed.
	ids := []string{"ogn-001", "ogn-002", "ogn-003"}
	for i, id := range ids {
		row, _ := sampleCard(t, id, "Name"+strconv.Itoa(i+1), "OGN", i+1)
		if err := repo.Upsert(ctx, row); err != nil {
			t.Fatalf("Upsert %s: %v", id, err)
		}
	}
	seen := make(map[string]bool)
	for i := 0; i < 50; i++ {
		row, err := repo.GetRandomCard(ctx)
		if err != nil {
			t.Fatalf("GetRandomCard: %v", err)
		}
		seen[row.RiftboundID] = true
	}
	if len(seen) < 2 {
		t.Errorf("GetRandomCard returned only %d distinct ids in 50 calls, want at least 2", len(seen))
	}
	for id := range seen {
		if !slices.Contains(ids, id) {
			t.Errorf("GetRandomCard returned unknown id %q", id)
		}
	}
}

// --- SyncCards (transactional replace) ------------------------------------

func TestCardRepo_SyncCards_ReplacesSet(t *testing.T) {
	s := newTestStore(t)
	repo := store.NewCardRepo(s.DB())
	ctx := context.Background()

	// Seed: three cards.
	seed := []string{"ogn-001", "ogn-002", "ogn-003"}
	for _, id := range seed {
		row, _ := sampleCard(t, id, id, "OGN", 0)
		if err := repo.Upsert(ctx, row); err != nil {
			t.Fatalf("seed Upsert: %v", err)
		}
	}

	// SyncCards with a subset + a new one. The replaced set should
	// contain exactly the new IDs, no others.
	newRows := []store.CardRow{}
	for i, id := range []string{"ogn-002", "ogn-003", "ogn-099"} {
		row, _ := sampleCard(t, id, id, "OGN", i)
		newRows = append(newRows, row)
	}
	if err := repo.SyncCards(ctx, newRows); err != nil {
		t.Fatalf("SyncCards: %v", err)
	}

	got, err := repo.All(ctx)
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	ids := make([]string, 0, len(got))
	for _, r := range got {
		ids = append(ids, r.RiftboundID)
	}
	want := []string{"ogn-002", "ogn-003", "ogn-099"}
	if !slices.Equal(ids, want) {
		t.Errorf("after SyncCards, ids = %v, want %v", ids, want)
	}
}

func TestCardRepo_SyncCards_EmptyClearsStore(t *testing.T) {
	s := newTestStore(t)
	repo := store.NewCardRepo(s.DB())
	ctx := context.Background()
	row, _ := sampleCard(t, "ogn-011", "Abandon", "OGN", 11)
	if err := repo.Upsert(ctx, row); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := repo.SyncCards(ctx, nil); err != nil {
		t.Fatalf("SyncCards(nil): %v", err)
	}
	if n, _ := repo.Count(ctx); n != 0 {
		t.Errorf("Count = %d, want 0 after SyncCards(nil)", n)
	}
}

// --- SetRepo ---------------------------------------------------------------

func TestSetRepo_UpsertAndGetByID(t *testing.T) {
	s := newTestStore(t)
	repo := store.NewSetRepo(s.DB())
	row, payload := sampleSet(t, "ogn", 298)
	ctx := context.Background()
	if err := repo.Upsert(ctx, row); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	got, err := repo.GetByID(ctx, "ogn")
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.SetID != "ogn" {
		t.Errorf("SetID = %q, want ogn", got.SetID)
	}
	if got.CardCount != 298 {
		t.Errorf("CardCount = %d, want 298", got.CardCount)
	}
	if string(got.Payload) != string(payload) {
		t.Errorf("payload mismatch")
	}
}

func TestSetRepo_GetByID_NotFound(t *testing.T) {
	s := newTestStore(t)
	repo := store.NewSetRepo(s.DB())
	_, err := repo.GetByID(context.Background(), "missing")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected sql.ErrNoRows, got %v", err)
	}
}

// --- SyncStateRepo ---------------------------------------------------------

func TestSyncStateRepo_InitialStateIsEmpty(t *testing.T) {
	s := newTestStore(t)
	repo := store.NewSyncStateRepo(s.DB())
	got, err := repo.Get(context.Background())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.LastSyncAt != nil {
		t.Errorf("LastSyncAt = %v, want nil on initial state", got.LastSyncAt)
	}
	if got.LastStatus != "" {
		t.Errorf("LastStatus = %q, want empty", got.LastStatus)
	}
	if got.LastCardCount != 0 {
		t.Errorf("LastCardCount = %d, want 0", got.LastCardCount)
	}
	if got.LastError != "" {
		t.Errorf("LastError = %q, want empty", got.LastError)
	}
}

func TestSyncStateRepo_MarkOK(t *testing.T) {
	s := newTestStore(t)
	repo := store.NewSyncStateRepo(s.DB())
	ctx := context.Background()
	if err := repo.MarkOK(ctx, 1178, "build-abc"); err != nil {
		t.Fatalf("MarkOK: %v", err)
	}
	got, err := repo.Get(ctx)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.LastStatus != domain.SyncStatusOK {
		t.Errorf("LastStatus = %q, want ok", got.LastStatus)
	}
	if got.LastCardCount != 1178 {
		t.Errorf("LastCardCount = %d, want 1178", got.LastCardCount)
	}
	if got.LastBuildID != "build-abc" {
		t.Errorf("LastBuildID = %q, want build-abc", got.LastBuildID)
	}
	if got.LastSyncAt == nil {
		t.Errorf("LastSyncAt = nil, want non-nil after MarkOK")
	}
	if got.LastError != "" {
		t.Errorf("LastError = %q, want empty after MarkOK", got.LastError)
	}
}

func TestSyncStateRepo_MarkFailed(t *testing.T) {
	s := newTestStore(t)
	repo := store.NewSyncStateRepo(s.DB())
	ctx := context.Background()
	if err := repo.MarkFailed(ctx, errors.New("upstream returned 503")); err != nil {
		t.Fatalf("MarkFailed: %v", err)
	}
	got, err := repo.Get(ctx)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.LastStatus != domain.SyncStatusFailed {
		t.Errorf("LastStatus = %q, want failed", got.LastStatus)
	}
	if got.LastError != "upstream returned 503" {
		t.Errorf("LastError = %q, want upstream returned 503", got.LastError)
	}
	if got.LastSyncAt == nil {
		t.Errorf("LastSyncAt = nil, want non-nil after MarkFailed")
	}
}

func TestSyncStateRepo_LastSyncAtIsRecent(t *testing.T) {
	s := newTestStore(t)
	repo := store.NewSyncStateRepo(s.DB())
	before := time.Now().UTC().Add(-time.Second)
	if err := repo.MarkOK(context.Background(), 0, ""); err != nil {
		t.Fatalf("MarkOK: %v", err)
	}
	after := time.Now().UTC().Add(time.Second)
	got, _ := repo.Get(context.Background())
	if got.LastSyncAt == nil {
		t.Fatalf("LastSyncAt = nil")
	}
	if got.LastSyncAt.Before(before) || got.LastSyncAt.After(after) {
		t.Errorf("LastSyncAt = %v, want between %v and %v", got.LastSyncAt, before, after)
	}
}

// --- utilities -------------------------------------------------------------

func names(rows []*store.CardRow) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Name)
	}
	return out
}
