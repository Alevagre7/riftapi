// Package config loads riftapi's runtime configuration from environment
// variables. A config-file loader (YAML) is layered on top in Phase 7
// (see docs/IMPLEMENTATION_PLAN.md), but env-only is sufficient for the
// first five phases.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config is the in-memory shape of the runtime configuration. It is loaded
// once at startup by Load() and passed by value to the components that
// need it. No component is expected to mutate it.
type Config struct {
	// API server.
	APIBind string
	APIPort int

	// Database.
	DatabasePath string

	// Scraper (riftapi-sync only).
	ScrapeUserAgent  string
	ScrapeTimeout    time.Duration
	ScrapeMaxRetries int

	// Sync (riftapi-sync only).
	SyncEnabled      bool
	SyncMinCardCount int
	SyncRequiredIDs  []string

	// Telegram alerts (riftapi-sync only).
	TelegramAlertsEnabled bool
	TelegramBotToken      string
	TelegramAdminChatID   string

	// Logging.
	LogLevel string
}

// Load reads the environment and returns a fully-populated Config.
// Invalid values fall back to defaults; only structural problems
// (unparseable integers, etc.) are returned as errors.
func Load() (*Config, error) {
	cfg := &Config{
		APIBind:               getEnvDefault("RIFTAPI_API_BIND", "0.0.0.0"),
		APIPort:               getEnvInt("RIFTAPI_API_PORT", 8080),
		DatabasePath:          getEnvDefault("RIFTAPI_DATABASE_PATH", "/data/riftapi.db"),
		ScrapeUserAgent:       getEnvDefault("RIFTAPI_SCRAPE_USER_AGENT", "riftapi/0.1 (+https://github.com/xalevagre7/riftapi)"),
		ScrapeTimeout:         time.Duration(getEnvInt("RIFTAPI_SCRAPE_TIMEOUT_SECS", 30)) * time.Second,
		ScrapeMaxRetries:      getEnvInt("RIFTAPI_SCRAPE_MAX_RETRIES", 2),
		SyncEnabled:           getEnvBool("RIFTAPI_SYNC_ENABLED", false),
		SyncMinCardCount:      getEnvInt("RIFTAPI_SYNC_MIN_CARD_COUNT", 1100),
		SyncRequiredIDs:       splitCSV(getEnvDefault("RIFTAPI_SYNC_REQUIRED_IDS", "ogn-011,unl-001,sfd-001,ven-001")),
		TelegramAlertsEnabled: getEnvBool("RIFTAPI_TELEGRAM_ALERTS_ENABLED", true),
		TelegramBotToken:      getEnvDefault("RIFTAPI_TELEGRAM_BOT_TOKEN", ""),
		TelegramAdminChatID:   getEnvDefault("RIFTAPI_TELEGRAM_ADMIN_CHAT_ID", ""),
		LogLevel:              getEnvDefault("RIFTAPI_LOG_LEVEL", "info"),
	}
	return cfg, nil
}

// Validate checks the loaded config for hard problems (missing required
// values, out-of-range ports). Soft problems (unset Telegram token when
// alerts are enabled) are logged at startup time by the caller, not here.
func (c *Config) Validate() error {
	if c.APIPort <= 0 || c.APIPort > 65535 {
		return fmt.Errorf("invalid API port: %d", c.APIPort)
	}
	if c.DatabasePath == "" {
		return fmt.Errorf("database path is required (set RIFTAPI_DATABASE_PATH)")
	}
	if c.ScrapeTimeout <= 0 {
		return fmt.Errorf("invalid scrape timeout: %s", c.ScrapeTimeout)
	}
	if c.SyncMinCardCount < 0 {
		return fmt.Errorf("invalid sync min card count: %d", c.SyncMinCardCount)
	}
	return nil
}

func getEnvDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getEnvInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func getEnvBool(key string, def bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return def
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
