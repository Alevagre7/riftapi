package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/xalevagre7/riftapi/internal/store"
)

// listCards handles GET /cards?sort=&dir=&set_id=&page=&size=.
// Returns a paginated list of cards. Response shape is the
// search-response shape: {items, total, page, size, pages}.
func (s *Server) listCards(w http.ResponseWriter, r *http.Request) {
	opts, err := parseListOptions(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	rows, total, err := s.store.Cards().List(r.Context(), opts)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeSearchResponse(w, rows, total, opts)
}

// searchCards handles GET /cards/search?query=&sort=&dir=&set_id=&page=&size=.
// Full-text search on text.plain. Response shape is the search-response
// SearchResponse.
func (s *Server) searchCards(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("query"))
	if query == "" {
		writeError(w, http.StatusBadRequest, "query is required")
		return
	}
	opts, err := parseListOptions(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
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

// parseListOptions extracts and validates the list/search query parameters.
// Page and size zero retain their store defaults; negative values and unknown
// sort directions are caller errors rather than silently changing the query.
func parseListOptions(r *http.Request) (store.ListCardsOptions, error) {
	q := r.URL.Query()
	page, err := parseOptionalInt(q.Get("page"), "page")
	if err != nil {
		return store.ListCardsOptions{}, err
	}
	size, err := parseOptionalInt(q.Get("size"), "size")
	if err != nil {
		return store.ListCardsOptions{}, err
	}
	dir, err := parseOptionalInt(q.Get("dir"), "dir")
	if err != nil {
		return store.ListCardsOptions{}, err
	}
	if page < 0 || size < 0 {
		return store.ListCardsOptions{}, fmt.Errorf("page and size must not be negative")
	}
	if dir != -1 && dir != 0 && dir != 1 {
		return store.ListCardsOptions{}, fmt.Errorf("dir must be -1, 0, or 1")
	}
	sort := q.Get("sort")
	switch sort {
	case "", "name", "collector_number", "set_id":
	default:
		return store.ListCardsOptions{}, fmt.Errorf("unsupported sort %q", sort)
	}
	return store.ListCardsOptions{
		SetID: q.Get("set_id"),
		Sort:  sort,
		Dir:   dir,
		Page:  page,
		Size:  size,
	}, nil
}

func parseOptionalInt(value, name string) (int, error) {
	if value == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer", name)
	}
	return n, nil
}

// writeSearchResponse writes the search-response shape
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
