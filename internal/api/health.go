package api

import (
	"net/http"

	"github.com/xalevagre7/riftapi/internal/health"
)

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

// health handles GET /health. It returns 200 with a status of "ok"
// when the local store has a successful sync on record with at least
// one card; 503 with a status of "unhealthy" otherwise (failed sync,
// or no sync has run yet). The 503 is the signal that triggers a
// docker healthcheck failure in Phase 7.
func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	state, err := s.store.SyncState().Get(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"status": "error",
			"error":  err.Error(),
		})
		return
	}

	status := "ok"
	httpStatus := http.StatusOK
	if !health.IsHealthy(state) {
		status = "unhealthy"
		httpStatus = http.StatusServiceUnavailable
	}

	writeJSON(w, httpStatus, map[string]any{
		"status":          status,
		"last_sync_at":    state.LastSyncAt,
		"last_card_count": state.LastCardCount,
		"last_status":     string(state.LastStatus),
		"last_error":      state.LastError,
	})
}
