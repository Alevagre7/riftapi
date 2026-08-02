package scrape

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/xalevagre7/riftapi/internal/domain"
	"github.com/xalevagre7/riftapi/internal/health"
	"github.com/xalevagre7/riftapi/internal/store"
)

// Syncer orchestrates a full sync: fetch the upstream gallery, parse
// it, transform each card, and write the result to the store. On any
// failure it updates sync_state and (if configured) sends a Telegram
// alert. A successful Run replaces the store's snapshot with the new
// one in place; the API continues to read the same SQLite file with
// WAL mode, so reads see the new data as it lands.
//
// The Syncer owns no goroutines and is safe to run from a single
// caller (the riftapi-scraper binary, in our case).
type Syncer struct {
	// Store is the local SQLite store the syncer writes to. Required.
	Store *store.Store

	// Client fetches the upstream gallery HTML. Required.
	Client *Client

	// Alert is the destination for failure notifications. The Noop
	// sender is a fine default.
	Alert health.AlertSender

	// MinCount is the minimum number of cards the parsed page must
	// contain for the sync to be considered successful. Below this
	// the syncer treats the run as a failure (typically a sign that
	// the upstream page structure changed). The default in the
	// riftapi config is 1100.
	MinCount int

	// Required is the list of riftbound_ids that must be present in
	// the parsed page for the sync to be considered successful. Acts
	// as a stronger sanity check than MinCount alone. Empty means
	// "no required cards."
	Required []string

	// BuildID is an opaque upstream identifier (the Next.js build
	// id, if available) recorded in sync_state on success. Empty is
	// fine.
	BuildID string
}

// setMeta is the per-set accumulator the syncer builds while
// processing cards. The label is taken from the first card's set
// reference; the count is the number of cards seen for the set in
// the current sync.
type setMeta struct {
	label string
	count int
}

// Run executes one sync. The returned error is also reflected in the
// store's sync_state and (if Alert is configured) sent to Telegram.
// On success, the store's card and sync_state tables reflect the
// freshly-parsed page.
func (s *Syncer) Run(ctx context.Context) error {
	if s.Store == nil {
		return fmt.Errorf("syncer: Store is required")
	}
	if s.Client == nil {
		return fmt.Errorf("syncer: Client is required")
	}
	if s.Alert == nil {
		s.Alert = health.NoopSender{}
	}

	body, err := s.Client.Fetch(ctx)
	if err != nil {
		return s.fail(ctx, fmt.Errorf("fetch: %w", err))
	}

	page, err := ParsePage(body)
	if err != nil {
		return s.fail(ctx, fmt.Errorf("parse: %w", err))
	}

	rows := make([]store.CardRow, 0, len(page.CardJSONs))
	seenIDs := make(map[string]struct{}, len(page.CardJSONs))
	// setsByID accumulates the (label, card_count) per set_id as we
	// process each card. The label is taken from the first card's
	// set reference; the upstream's blades[2].sets.items[] may not
	// carry a label in every fixture. card_count is the actual
	// number of cards transformed for the set (not the collector
	// number max — variants push it above the max).
	setsByID := make(map[string]*setMeta)
	for i, raw := range page.CardJSONs {
		card, err := TransformCard(raw, page.CollectorMaxBySet)
		if err != nil {
			// A partially transformed snapshot is more dangerous than a
			// failed sync: MinCount could still pass while silently
			// dropping a real card. Keep the previous snapshot instead.
			return s.fail(ctx, fmt.Errorf("transform card %d: %w", i, err))
		}
		idKey := strings.ToLower(card.RiftboundID)
		if _, exists := seenIDs[idKey]; exists {
			return s.fail(ctx, fmt.Errorf("duplicate card id %q in upstream payload", card.RiftboundID))
		}
		seenIDs[idKey] = struct{}{}
		payload, err := store.EncodeCard(card)
		if err != nil {
			return s.fail(ctx, fmt.Errorf("encode card %s: %w", card.RiftboundID, err))
		}
		rows = append(rows, store.CardRow{
			RiftboundID:     card.RiftboundID,
			PublicCode:      card.PublicCode,
			SetID:           card.Set.SetID,
			CollectorNumber: card.CollectorNumber,
			Name:            card.Name,
			CleanName:       card.Metadata.CleanName,
			Payload:         payload,
		})

		m, ok := setsByID[card.Set.SetID]
		if !ok {
			m = &setMeta{label: card.Set.Label}
			setsByID[card.Set.SetID] = m
		}
		m.count++
	}

	count := len(rows)

	if s.MinCount > 0 && count < s.MinCount {
		return s.fail(ctx, fmt.Errorf("only %d cards parsed, want >= %d", count, s.MinCount))
	}
	for _, id := range s.Required {
		if _, ok := seenIDs[strings.ToLower(id)]; !ok {
			return s.fail(ctx, fmt.Errorf("required card %s missing", id))
		}
	}

	setRows, err := buildSetRows(setsByID)
	if err != nil {
		return s.fail(ctx, err)
	}
	// Cards, sets, and the successful sync state are committed together.
	// Validation happens before this call so a partial upstream payload can
	// never replace the last known-good snapshot.
	if err := s.Store.SyncSnapshot(ctx, rows, setRows, count, s.BuildID); err != nil {
		return s.fail(ctx, fmt.Errorf("sync snapshot: %w", err))
	}
	log.Printf("sync ok: %d cards", count)
	return nil
}

// fail records the error in sync_state, sends a Telegram alert if one
// is configured, and returns the original error so the caller can
// decide what to do (typically: exit non-zero so systemd restarts on
// the next scheduled run).
func (s *Syncer) fail(_ context.Context, err error) error {
	log.Printf("sync failed: %v", err)
	// The caller's context is often the one that timed out. Use a short,
	// independent context so the failure state and alert still have a
	// chance to be recorded after an upstream timeout or cancellation.
	failureCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if markErr := s.Store.SyncState().MarkFailed(failureCtx, err); markErr != nil {
		log.Printf("mark failed: %v", markErr)
	}
	message := fmt.Sprintf("riftapi sync failed: %v", err)
	if len(message) > 4000 {
		message = message[:4000] + "…"
	}
	if alertErr := s.Alert.Send(failureCtx, message); alertErr != nil {
		log.Printf("alert send failed: %v", alertErr)
	}
	return err
}

// buildSetRows creates one row per set seen in this run. TCGPlayerID,
// CardmarketID, and PublishedOn are always nil — the gallery does
// not provide them (ADR-0001) and the Set's ID is the set_id (we
// don't have opaque internal UUIDs).
func buildSetRows(sets map[string]*setMeta) ([]store.SetRow, error) {
	setIDs := make([]string, 0, len(sets))
	for setID := range sets {
		setIDs = append(setIDs, setID)
	}
	sort.Strings(setIDs)
	rows := make([]store.SetRow, 0, len(setIDs))
	for _, setID := range setIDs {
		m := sets[setID]
		payload, err := encodeSetPayload(setID, m.label, m.count)
		if err != nil {
			return nil, fmt.Errorf("encode set %s: %w", setID, err)
		}
		rows = append(rows, store.SetRow{
			SetID:     setID,
			CardCount: m.count,
			Payload:   payload,
		})
	}
	return rows, nil
}

// encodeSetPayload serialises a domain.Set to the JSON blob stored
// in the sets.payload column. TCGPlayerID, CardmarketID, and
// PublishedOn are nil per ADR-0001.
func encodeSetPayload(setID, label string, cardCount int) ([]byte, error) {
	count := cardCount
	return json.Marshal(domain.Set{
		ID:        setID,
		Name:      label,
		SetID:     setID,
		CardCount: &count,
	})
}
