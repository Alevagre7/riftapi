package api

import "net/http"

// corsMiddleware sets the CORS headers on every response. The API
// is read-only, so we allow any origin and the GET method (and
// OPTIONS for preflight). The "open by default" policy comes from
// docs/IMPLEMENTATION_PLAN.md §4.
func corsMiddleware(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		h.ServeHTTP(w, r)
	})
}
