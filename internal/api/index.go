package api

import (
	"net/http"

	"github.com/xalevagre7/riftapi/internal/domain"
)

// registerIndexRoutes mounts the /index/* handlers. Phase 4 only
// ships /index/card-names (the one the bot uses for /random);
// Phase 5 adds /index/{keywords,types,supertypes,domains,rarities,
// artists,energy,might,power,tags}.
func (s *Server) registerIndexRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /index/card-names", s.indexCardNames)
}

// indexCardNames handles GET /index/card-names. Returns the
// riftcodex Index shape: {total, type, values}. The values are
// sorted alphabetically and are strings.
func (s *Server) indexCardNames(w http.ResponseWriter, r *http.Request) {
	names, err := s.store.Cards().ListNames(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	values := make([]domain.IndexValue, 0, len(names))
	for _, n := range names {
		values = append(values, domain.StringIndexValue(n))
	}
	writeJSON(w, http.StatusOK, domain.Index{
		Total:  len(names),
		Type:   "card-names",
		Values: values,
	})
}
