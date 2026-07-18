package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/xalevagre7/riftapi/internal/store"
)

// registerCardRoutes mounts the /cards/* handlers. Phase 4 wires up
// the four endpoints the bot currently uses; Phase 5 adds the
// remaining riftcodex endpoints (list, search, tcgplayer).
func (s *Server) registerCardRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /cards/name", s.listCardsByName)
	mux.HandleFunc("GET /cards/riftbound/{id}", s.getCardsByRiftboundID)
	mux.HandleFunc("GET /cards/{id}", s.getCardByID)
}

// listCardsByName handles GET /cards/name?fuzzy=X or ?exact=X.
// Returns the riftcodex SearchResponse shape: {items, total, page,
// size, pages}. The {page, size, pages} fields are always 1 here
// because the local store is small and pagination is not exposed
// (Phase 5's /cards list endpoint will add it).
func (s *Server) listCardsByName(w http.ResponseWriter, r *http.Request) {
	repo := s.store.Cards()
	ctx := r.Context()

	var rows []*store.CardRow
	if exact := strings.TrimSpace(r.URL.Query().Get("exact")); exact != "" {
		row, err := repo.GetByName(ctx, exact)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				// Exact lookup with no match: return an empty
				// search response (the riftcodex API returns
				// total=0 in this case).
				writeJSON(w, http.StatusOK, map[string]any{
					"items": []json.RawMessage{},
					"total": 0,
					"page":  1,
					"size":  0,
					"pages": 0,
				})
				return
			}
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		rows = []*store.CardRow{row}
	} else if fuzzy := strings.TrimSpace(r.URL.Query().Get("fuzzy")); fuzzy != "" {
		var err error
		rows, err = repo.SearchByName(ctx, fuzzy, 0)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	} else {
		writeError(w, http.StatusBadRequest, "either fuzzy or exact query parameter is required")
		return
	}

	items := make([]json.RawMessage, 0, len(rows))
	for _, row := range rows {
		items = append(items, row.Payload)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"items": items,
		"total": len(items),
		"page":  1,
		"size":  len(items),
		"pages": 1,
	})
}

// getCardByID handles GET /cards/{id}. The path parameter is matched
// against the local store's riftbound_id (we don't have riftcodex
// UUIDs). Returns the Card JSON directly on success, 404 if not
// found.
func (s *Server) getCardByID(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "id is required")
		return
	}

	row, err := s.store.Cards().GetByRiftboundID(r.Context(), id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "card not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeRawJSON(w, http.StatusOK, row.Payload)
}

// getCardsByRiftboundID handles GET /cards/riftbound/{id}. The
// riftcodex contract returns an array (alternate arts may share a
// base id; we use the trailing letter in the riftbound_id to
// distinguish them, so in practice a single id matches at most one
// row — but the array shape is preserved for forward compatibility).
// Returns [] (200) when no card matches.
func (s *Server) getCardsByRiftboundID(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "id is required")
		return
	}

	row, err := s.store.Cards().GetByRiftboundID(r.Context(), id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeRawJSON(w, http.StatusOK, []byte(`[]`))
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeRawJSON(w, http.StatusOK, []byte(`[`+string(row.Payload)+`]`))
}
