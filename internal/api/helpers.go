package api

import (
	"encoding/json"
	"net/http"
)

// writeJSON serialises body to JSON and writes it with the given
// status. Errors are swallowed; the response has already been
// committed.
func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// writeRawJSON writes pre-serialised JSON bytes (e.g. a CardRow's
// payload column) directly. Used when the data is already valid JSON
// in the store and we don't want a round-trip through encoding/json.
func writeRawJSON(w http.ResponseWriter, status int, body []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// writeError sends a structured error response. The shape matches
// the standard {error, message} shape so any consumer can
// can parse it uniformly.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{
		"error":   http.StatusText(status),
		"message": message,
	})
}
