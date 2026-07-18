package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/xalevagre7/riftapi/internal/store"
)

// listCards handles GET /cards?sort=&dir=&set_id=&page=&size=.
// Returns a paginated list of cards. Response shape is the
// riftcodex SearchResponse: {items, total, page, size, pages}.
func (s *Server) listCards(w http.ResponseWriter, r *http.Request) {
	opts := parseListOptions(r)
	rows, total, err := s.store.Cards().List(r.Context(), opts)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeSearchResponse(w, rows, total, opts)
}

// searchCards handles GET /cards/search?query=&sort=&dir=&set_id=&page=&size=.
// Full-text search on text.plain. Response shape is the riftcodex
// SearchResponse.
func (s *Server) searchCards(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("query"))
	if query == "" {
		writeError(w, http.StatusBadRequest, "query is required")
		return
	}
	opts := parseListOptions(r)
	rows, total, err := s.store.Cards().SearchText(r.Context(), query, opts)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeSearchResponse(w, rows, total, opts)
}

// getCardByTcgPlayerID handles GET /cards/tcgplayer/{id}. The
// gallery does not expose tcgplayer_id (ADR-0001), so this
// endpoint is registered for API surface completeness but always
// returns 404.
func (s *Server) getCardByTcgPlayerID(w http.ResponseWriter, _ *http.Request) {
	writeError(w, http.StatusNotFound, "tcgplayer_id is not available in the upstream gallery (ADR-0001)")
}

// parseListOptions extracts the list/search query parameters into a
// store.ListCardsOptions. The Page/Size/Dir clamps happen inside
// the store layer, so this function only parses.
func parseListOptions(r *http.Request) store.ListCardsOptions {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	size, _ := strconv.Atoi(q.Get("size"))
	dir, _ := strconv.Atoi(q.Get("dir"))
	return store.ListCardsOptions{
		SetID: q.Get("set_id"),
		Sort:  q.Get("sort"),
		Dir:   dir,
		Page:  page,
		Size:  size,
	}
}

// writeSearchResponse writes the riftcodex SearchResponse shape
// (items, total, page, size, pages) with the supplied rows and
// total. The page/size in the response are normalised to the
// store's clamping (1, 50, 100) so the client can rely on the
// values it sees.
func writeSearchResponse(w http.ResponseWriter, rows []*store.CardRow, total int, opts store.ListCardsOptions) {
	items := make([]json.RawMessage, 0, len(rows))
	for _, row := range rows {
		items = append(items, row.Payload)
	}
	page := opts.Page
	if page < 1 {
		page = 1
	}
	size := opts.Size
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
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items,
		"total": total,
		"page":  page,
		"size":  size,
		"pages": pages,
	})
}
