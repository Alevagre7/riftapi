package api

import (
	"net/http"

	"github.com/xalevagre7/riftapi/internal/domain"
)

// registerIndexRoutes mounts the /index/* handlers. Phase 4 ships
// /index/card-names (the one the bot uses for /random); Phase 5
// adds the other riftcodex index endpoints. /index/keywords is
// intentionally omitted — it would require text parsing of card
// text to extract [Keyword] tokens, which is out of scope.
func (s *Server) registerIndexRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /index/card-names", s.indexCardNames)

	mux.HandleFunc("GET /index/types", func(w http.ResponseWriter, r *http.Request) {
		s.indexStringField(w, r, "classification.type", "types")
	})
	mux.HandleFunc("GET /index/supertypes", func(w http.ResponseWriter, r *http.Request) {
		s.indexStringField(w, r, "classification.supertype", "supertypes")
	})
	mux.HandleFunc("GET /index/domains", func(w http.ResponseWriter, r *http.Request) {
		s.indexArrayField(w, r, "classification.domain", "domains")
	})
	mux.HandleFunc("GET /index/rarities", func(w http.ResponseWriter, r *http.Request) {
		s.indexStringField(w, r, "classification.rarity", "rarities")
	})
	mux.HandleFunc("GET /index/artists", func(w http.ResponseWriter, r *http.Request) {
		s.indexStringField(w, r, "media.artist", "artists")
	})
	mux.HandleFunc("GET /index/energy", func(w http.ResponseWriter, r *http.Request) {
		s.indexIntField(w, r, "attributes.energy", "energy")
	})
	mux.HandleFunc("GET /index/might", func(w http.ResponseWriter, r *http.Request) {
		s.indexIntField(w, r, "attributes.might", "might")
	})
	mux.HandleFunc("GET /index/power", func(w http.ResponseWriter, r *http.Request) {
		s.indexIntField(w, r, "attributes.power", "power")
	})
	mux.HandleFunc("GET /index/tags", func(w http.ResponseWriter, r *http.Request) {
		s.indexArrayField(w, r, "tags", "tags")
	})
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
	s.writeIndexStrings(w, "card-names", names)
}

// indexStringField returns the distinct values of a JSON string
// field across all cards, sorted. Used by /index/{types,
// supertypes, rarities, artists}.
func (s *Server) indexStringField(w http.ResponseWriter, r *http.Request, path, typeName string) {
	values, err := s.store.Cards().DistinctStringField(r.Context(), path)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeIndexStrings(w, typeName, values)
}

// indexIntField returns the distinct integer values of a JSON
// field across all cards, sorted. Used by /index/{energy, might, power}.
func (s *Server) indexIntField(w http.ResponseWriter, r *http.Request, path, typeName string) {
	values, err := s.store.Cards().DistinctIntField(r.Context(), path)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	ixValues := make([]domain.IndexValue, 0, len(values))
	for _, v := range values {
		ixValues = append(ixValues, domain.IntIndexValue(v))
	}
	writeJSON(w, http.StatusOK, domain.Index{
		Total:  len(values),
		Type:   typeName,
		Values: ixValues,
	})
}

// indexArrayField returns the distinct values of a JSON array
// field, unnested. Used by /index/{domains, tags}.
func (s *Server) indexArrayField(w http.ResponseWriter, r *http.Request, path, typeName string) {
	values, err := s.store.Cards().DistinctArrayValues(r.Context(), path)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeIndexStrings(w, typeName, values)
}

// writeIndexStrings writes an Index response whose values are all
// strings, in the supplied (already-sorted) order. The caller is
// responsible for sorting; the store's Distinct* methods do this.
func (s *Server) writeIndexStrings(w http.ResponseWriter, typeName string, values []string) {
	ixValues := make([]domain.IndexValue, 0, len(values))
	for _, v := range values {
		ixValues = append(ixValues, domain.StringIndexValue(v))
	}
	writeJSON(w, http.StatusOK, domain.Index{
		Total:  len(values),
		Type:   typeName,
		Values: ixValues,
	})
}
