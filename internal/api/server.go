// Package api implements the HTTP surface of riftapi. The handlers
// mirror the card data shape from the upstream gallery.
// (the source of truth for the wire format).
//
// The package is laid out by endpoint family:
//
//   - server.go     — Server type, NewServer, Routes. Owns the
//                     dependency on the store.
//   - middleware.go — CORS middleware.
//   - helpers.go    — writeJSON / writeError response helpers.
//   - health.go     — GET /, GET /health.
//   - cards.go      — GET /cards/* handlers (name, riftbound, by id,
//                     list, search).
//   - index.go      — GET /index/* handlers (card-names and friends).
//
// Handlers are thin: they read from the store, pass the stored JSON
// payload through, and apply the appropriate query parameters. There
// is no in-memory cache; SQLite's WAL mode is fast enough for the
// scale (≈1,200 rows) and removes a whole class of staleness bugs.
package api

import (
	"net/http"

	"github.com/xalevagre7/riftapi/internal/store"
)

// Server is the API's HTTP surface. One Server per process; it holds
// the store and the wired routes. There is no global state.
type Server struct {
	store *store.Store
}

// NewServer returns a Server backed by s. The store must already be
// Open()ed; the Server does not own its lifecycle (the binary
// entry-point closes it on shutdown).
func NewServer(s *store.Store) *Server {
	return &Server{store: s}
}

// Routes returns the http.Handler for the API. Use it as the
// http.Server.Handler; the Server has no other state. CORS is
// applied as the outermost layer so every response (including error
// responses from inner handlers) carries the headers.
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.root)
	mux.HandleFunc("GET /health", s.health)
	s.registerCardRoutes(mux)
	s.registerSetRoutes(mux)
	s.registerIndexRoutes(mux)
	return corsMiddleware(mux)
}
