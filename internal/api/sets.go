package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/xalevagre7/riftapi/internal/store"
)

// registerSetRoutes mounts the /sets/* handlers. The riftcodex
// surface includes five set endpoints; the gallery does not expose
// UUIDs, tcgplayer_id, or cardmarket_id, so the lookup-by-id and
// the two id-based lookups are always 404. /sets and
// /sets/set-id/{set_id} are functional.
func (s *Server) registerSetRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /sets", s.listSets)
	mux.HandleFunc("GET /sets/set-id/{set_id}", s.getSetBySetID)
	// Always-404 handlers, kept for surface completeness.
	mux.HandleFunc("GET /sets/tcgplayer/{id}", s.getSetByTcgPlayerID)
	mux.HandleFunc("GET /sets/cardmarket/{id}", s.getSetByCardmarketID)
	mux.HandleFunc("GET /sets/{id}", s.getSetByID)
}

// listSets handles GET /sets?page=&size=. Response is the
// riftcodex SearchResponse shape: {items, total, page, size, pages}.
func (s *Server) listSets(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	size, _ := strconv.Atoi(q.Get("size"))
	rows, total, err := s.store.Sets().List(r.Context(), store.ListSetsOptions{Page: page, Size: size})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	items := make([]json.RawMessage, 0, len(rows))
	for _, row := range rows {
		items = append(items, row.Payload)
	}
	page, size, pages := clampPagination(page, size, total)
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items,
		"total": total,
		"page":  page,
		"size":  size,
		"pages": pages,
	})
}

// getSetBySetID handles GET /sets/set-id/{set_id}. The set_id is
// the lowercased code from upstream (e.g. "ogn", "unl", "ven").
// Returns the Set JSON on success, 404 if not found.
func (s *Server) getSetBySetID(w http.ResponseWriter, r *http.Request) {
	setID := r.PathValue("set_id")
	if setID == "" {
		writeError(w, http.StatusBadRequest, "set_id is required")
		return
	}
	row, err := s.store.Sets().GetByID(r.Context(), setID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "set not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeRawJSON(w, http.StatusOK, row.Payload)
}

// getSetByID handles GET /sets/{id}. The riftcodex id is a UUID;
// we don't have those (the gallery does not expose them), so the
// endpoint always 404s.
func (s *Server) getSetByID(w http.ResponseWriter, _ *http.Request) {
	writeError(w, http.StatusNotFound, "set UUID lookup not supported (no upstream UUIDs)")
}

// getSetByTcgPlayerID handles GET /sets/tcgplayer/{id}. The
// gallery does not expose tcgplayer_id, so this is always 404.
func (s *Server) getSetByTcgPlayerID(w http.ResponseWriter, _ *http.Request) {
	writeError(w, http.StatusNotFound, "tcgplayer_id is not available in the upstream gallery (ADR-0001)")
}

// getSetByCardmarketID handles GET /sets/cardmarket/{id}. The
// gallery does not expose cardmarket_id, so this is always 404.
func (s *Server) getSetByCardmarketID(w http.ResponseWriter, _ *http.Request) {
	writeError(w, http.StatusNotFound, "cardmarket_id is not available in the upstream gallery (ADR-0001)")
}

// clampPagination normalises a (page, size) pair to the store's
// defaults (1, 50) and returns the (clamped_page, clamped_size,
// pages) tuple. Shared between the cards and sets list endpoints.
func clampPagination(page, size, total int) (int, int, int) {
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 50
	}
	if size > 100 {
		size = 100
	}
	pages := 0
	if size > 0 {
		pages = (total + size - 1) / size
	}
	return page, size, pages
}
