package api

import (
	"net/http"
)

// SyncMinCardCount is the minimum number of cards the store must
// contain for /health to report status "ok". If card_count is below
// this threshold the status is "degraded" (the sync ran but may have
// been partial). The default matches the riftapi-sync config default;
// API tests set it lower.
var SyncMinCardCount = 1100

// root handles GET /. It returns a tiny JSON page describing the API
// and including the Riot "Legal Jibber Jabber" attribution that
// ADR-0001 and the implementation plan require on any player-facing
// surface.
func (s *Server) root(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"name":     "riftapi",
		"version":  "0.1.0",
		"docs":     "https://github.com/xalevagre7/riftapi",
		"upstream": "playriftbound.com",
		"attribution": "This project was created under Riot Games' " +
			"'Legal Jibber Jabber' policy using assets owned by Riot Games. " +
			"Riot Games does not endorse or sponsor this project.",
	})
}

// health handles GET /health. It returns a tri-state status:
//
//   - "ok" (200)       — last sync succeeded and card_count >=
//                        SyncMinCardCount
//   - "degraded" (200) — no sync has run yet, or the last sync
//                        succeeded but card_count < SyncMinCardCount
//   - "error" (503)    — last sync failed or the store is unreachable
//
// ok and degraded both return 200 so Docker's healthcheck keeps the
// container up; degraded includes "degraded": true for monitoring.
func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	// 1. Card count — if the store is unreachable this is an error.
	cardCount, err := s.store.Cards().Count(r.Context())
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"status": "error",
			"error":  err.Error(),
		})
		return
	}

	// 2. Sync state.
	state, err := s.store.SyncState().Get(r.Context())
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"status": "error",
			"error":  err.Error(),
		})
		return
	}

	// 3. Determine the tri-state.
	var (
		status     string
		httpStatus int
		degraded   bool
	)

	switch {
	case state.LastStatus == "ok" && cardCount >= SyncMinCardCount:
		status = "ok"
		httpStatus = http.StatusOK
		degraded = false
	case state.LastStatus == "ok":
		// Sync succeeded but card count is below threshold.
		status = "degraded"
		httpStatus = http.StatusOK
		degraded = true
	case state.LastStatus == "":
		// No sync has run yet — bootstrapped row with zero values.
		status = "degraded"
		httpStatus = http.StatusOK
		degraded = true
	default:
		// LastStatus == "failed" or an unexpected value.
		status = "error"
		httpStatus = http.StatusServiceUnavailable
		degraded = false
	}

	writeJSON(w, httpStatus, map[string]any{
		"status":                status,
		"degraded":              degraded,
		"card_count":            cardCount,
		"last_sync_at":          state.LastSyncAt,
		"last_sync_input_count": state.LastSyncInputCount,
		"last_status":           string(state.LastStatus),
		"last_error":            state.LastError,
	})
}
