package scrape

import (
	"context"
	"fmt"
	"log"

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
// caller (the riftapi-sync binary, in our case).
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

	repo := s.Store.Cards()
	rows := make([]store.CardRow, 0, len(page.CardJSONs))
	for i, raw := range page.CardJSONs {
		card, err := TransformCard(raw, page.CollectorMaxBySet)
		if err != nil {
			// Don't abort the whole sync on one bad card; log and
			// continue. A persistent parser error will be caught by
			// MinCount at the end of the run.
			log.Printf("warn: transform card %d failed: %v", i, err)
			continue
		}
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
	}

	// SyncCards is a single transaction that upserts every row and
	// deletes any pre-existing card whose riftbound_id is not in the
	// new set. The result is that the store always contains exactly
	// the cards from the most recent successful sync — no stale
	// cards accumulate.
	if err := repo.SyncCards(ctx, rows); err != nil {
		return s.fail(ctx, fmt.Errorf("sync cards: %w", err))
	}
	count := len(rows)

	if s.MinCount > 0 && count < s.MinCount {
		return s.fail(ctx, fmt.Errorf("only %d cards parsed, want >= %d", count, s.MinCount))
	}
	for _, id := range s.Required {
		if _, err := repo.GetByRiftboundID(ctx, id); err != nil {
			return s.fail(ctx, fmt.Errorf("required card %s missing: %w", id, err))
		}
	}

	if err := s.Store.SyncState().MarkOK(ctx, count, s.BuildID); err != nil {
		return s.fail(ctx, fmt.Errorf("mark ok: %w", err))
	}
	log.Printf("sync ok: %d cards", count)
	return nil
}

// fail records the error in sync_state, sends a Telegram alert if one
// is configured, and returns the original error so the caller can
// decide what to do (typically: exit non-zero so systemd restarts on
// the next scheduled run).
func (s *Syncer) fail(ctx context.Context, err error) error {
	log.Printf("sync failed: %v", err)
	if markErr := s.Store.SyncState().MarkFailed(ctx, err); markErr != nil {
		log.Printf("mark failed: %v", markErr)
	}
	if alertErr := s.Alert.Send(ctx, fmt.Sprintf("riftapi sync failed: %v", err)); alertErr != nil {
		log.Printf("alert send failed: %v", alertErr)
	}
	return err
}
