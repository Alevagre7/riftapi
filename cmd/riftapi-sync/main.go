// riftapi-sync pulls the latest card gallery from playriftbound.com,
// transforms it into the Riftcodex JSON shape, and writes a fresh
// SQLite snapshot. It is meant to be run by a host-level systemd
// timer during Spoiler Season, or manually with `riftapi-sync` for
// ad-hoc use.
//
// The binary exits non-zero on any failure so the systemd unit can
// be observed via `systemctl status riftapi-sync`. The Telegram
// alert is sent on top of that for the maintainer.
package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/xalevagre7/riftapi/internal/config"
	"github.com/xalevagre7/riftapi/internal/health"
	"github.com/xalevagre7/riftapi/internal/scrape"
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

	// The SyncEnabled flag is the master switch. When off, the
	// binary is a no-op — even if it's accidentally triggered
	// during a non-Spoiler-Season window, the store is left alone.
	// The systemd timer is the primary control; this flag is a
	// belt-and-braces guard that the maintainer can flip without
	// touching the timer.
	if !cfg.SyncEnabled {
		log.Println("riftapi-sync: SyncEnabled=false, exiting without changes")
		os.Exit(0)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	st, err := store.Open(ctx, cfg.DatabasePath)
	if err != nil {
		log.Fatalf("store.Open: %v", err)
	}
	defer func() { _ = st.Close() }()

	client := scrape.NewClient(scrape.ClientConfig{
		BaseURL:    "https://playriftbound.com",
		UserAgent:  cfg.ScrapeUserAgent,
		Timeout:    cfg.ScrapeTimeout,
		MaxRetries: cfg.ScrapeMaxRetries,
	})

	alert := health.NewSender(health.SenderConfig{
		Enabled:     cfg.TelegramAlertsEnabled,
		BotToken:    cfg.TelegramBotToken,
		AdminChatID: cfg.TelegramAdminChatID,
	})

	syn := &scrape.Syncer{
		Store:    st,
		Client:   client,
		Alert:    alert,
		MinCount: cfg.SyncMinCardCount,
		Required: cfg.SyncRequiredIDs,
	}

	if err := syn.Run(ctx); err != nil {
		log.Printf("riftapi-sync: failed: %v", err)
		os.Exit(1)
	}
	log.Println("riftapi-sync: done")
}
