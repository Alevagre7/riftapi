// riftapi is the read-only HTTP API server. It serves the Riftcodex
// JSON shape from a local SQLite snapshot, populated by riftapi-sync.
//
// The binary owns no secrets (no Telegram token, no upstream API key);
// the only inputs are a database path, a bind address, and a port.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/xalevagre7/riftapi/internal/api"
	"github.com/xalevagre7/riftapi/internal/config"
	"github.com/xalevagre7/riftapi/internal/store"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	if err := cfg.Validate(); err != nil {
		log.Fatalf("config invalid: %v", err)
	}

	// The --healthcheck flag is used by docker-compose's healthcheck
	// to test whether the server is up. It makes a request to the
	// local /health endpoint and exits 0 on 200, 1 on anything else.
	// This is a separate path from the server itself — we don't open
	// the store, we just probe the HTTP layer.
	if len(os.Args) > 1 && os.Args[1] == "--healthcheck" {
		runHealthcheck(cfg)
		return
	}

	st, err := store.Open(context.Background(), cfg.DatabasePath)
	if err != nil {
		log.Fatalf("store.Open: %v", err)
	}
	defer func() { _ = st.Close() }()

	addr := cfg.APIBind + ":" + strconv.Itoa(cfg.APIPort)
	srv := &http.Server{
		Addr:              addr,
		Handler:           api.NewServer(st).Routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	serverErr := make(chan error, 1)
	go func() {
		log.Printf("riftapi listening on %s (db=%s)", addr, cfg.DatabasePath)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		log.Printf("received %s, shutting down", sig)
	case err := <-serverErr:
		log.Printf("server error: %v", err)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown: %v", err)
	}
}

// runHealthcheck probes the local /health endpoint and exits 0 on
// 200, 1 otherwise. The caller is the docker-compose healthcheck
// (see docker-compose.yml); it is not an interactive command.
func runHealthcheck(cfg *config.Config) {
	url := fmt.Sprintf("http://127.0.0.1:%d/health", cfg.APIPort)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		log.Printf("healthcheck: %v", err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Printf("healthcheck: status %d", resp.StatusCode)
		os.Exit(1)
	}
	os.Exit(0)
}
