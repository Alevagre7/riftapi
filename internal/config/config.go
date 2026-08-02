// Package config loads riftapi's runtime configuration from environment
// variables. The deployment examples use environment files so the API and
// scraper share one documented configuration surface.
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

	// Scraper (riftapi-scraper only).
	ScrapeUserAgent  string
	ScrapeTimeout    time.Duration
	ScrapeMaxRetries int

	// Sync (riftapi-scraper only).
	SyncEnabled      bool
	SyncMinCardCount int
	SyncRequiredIDs  []string

	// Telegram alerts (riftapi-scraper only).
	TelegramAlertsEnabled bool
	TelegramBotToken      string
	TelegramAdminChatID   string

	// Logging.
	LogLevel string
}

// Load reads the environment and returns a fully-populated Config. Explicitly
// malformed values are errors instead of silently turning into defaults; a
// typo in a port or retry count should stop startup loudly.
func Load() (*Config, error) {
	apiPort, err := getEnvInt("RIFTAPI_API_PORT", 8080)
	if err != nil {
		return nil, err
	}
	timeoutSecs, err := getEnvInt("RIFTAPI_SCRAPE_TIMEOUT_SECS", 30)
	if err != nil {
		return nil, err
	}
	const maxTimeoutSecs = int64(1<<63-1) / int64(time.Second)
	if int64(timeoutSecs) > maxTimeoutSecs {
		return nil, fmt.Errorf("RIFTAPI_SCRAPE_TIMEOUT_SECS is too large: %d", timeoutSecs)
	}
	maxRetries, err := getEnvInt("RIFTAPI_SCRAPE_MAX_RETRIES", 2)
	if err != nil {
		return nil, err
	}
	minCardCount, err := getEnvInt("RIFTAPI_SYNC_MIN_CARD_COUNT", 1100)
	if err != nil {
		return nil, err
	}
	syncEnabled, err := getEnvBool("RIFTAPI_SYNC_ENABLED", false)
	if err != nil {
		return nil, err
	}
	alertsEnabled, err := getEnvBool("RIFTAPI_TELEGRAM_ALERTS_ENABLED", true)
	if err != nil {
		return nil, err
	}
	requiredIDs := getEnvDefault("RIFTAPI_SYNC_REQUIRED_IDS", "ogn-011,unl-001,sfd-001,ven-001")
	if _, ok := os.LookupEnv("RIFTAPI_SYNC_REQUIRED_IDS"); ok {
		// Unlike most string settings, an explicitly empty required-ID list
		// is meaningful: it disables that optional sanity check.
		requiredIDs = os.Getenv("RIFTAPI_SYNC_REQUIRED_IDS")
	}
	cfg := &Config{
		APIBind:               strings.TrimSpace(getEnvDefault("RIFTAPI_API_BIND", "0.0.0.0")),
		APIPort:               apiPort,
		DatabasePath:          strings.TrimSpace(getEnvDefault("RIFTAPI_DATABASE_PATH", "/data/riftapi.db")),
		ScrapeUserAgent:       strings.TrimSpace(getEnvDefault("RIFTAPI_SCRAPE_USER_AGENT", "riftapi/0.1 (+https://github.com/xalevagre7/riftapi)")),
		ScrapeTimeout:         time.Duration(timeoutSecs) * time.Second,
		ScrapeMaxRetries:      maxRetries,
		SyncEnabled:           syncEnabled,
		SyncMinCardCount:      minCardCount,
		SyncRequiredIDs:       splitCSV(requiredIDs),
		TelegramAlertsEnabled: alertsEnabled,
		TelegramBotToken:      getEnvDefault("RIFTAPI_TELEGRAM_BOT_TOKEN", ""),
		TelegramAdminChatID:   getEnvDefault("RIFTAPI_TELEGRAM_ADMIN_CHAT_ID", ""),
		LogLevel:              strings.ToLower(strings.TrimSpace(getEnvDefault("RIFTAPI_LOG_LEVEL", "info"))),
	}
	return cfg, nil
}

// Validate checks the loaded config for hard problems (missing required
// values, out-of-range ports). Soft problems (unset Telegram token when
// alerts are enabled) are logged at startup time by the caller, not here.
func (c *Config) Validate() error {
	if c == nil {
		return fmt.Errorf("config is nil")
	}
	if c.APIPort <= 0 || c.APIPort > 65535 {
		return fmt.Errorf("invalid API port: %d", c.APIPort)
	}
	if strings.TrimSpace(c.DatabasePath) == "" {
		return fmt.Errorf("database path is required (set RIFTAPI_DATABASE_PATH)")
	}
	if strings.TrimSpace(c.APIBind) == "" {
		return fmt.Errorf("API bind address cannot be empty")
	}
	if strings.TrimSpace(c.ScrapeUserAgent) == "" {
		return fmt.Errorf("scrape user-agent cannot be empty")
	}
	if c.ScrapeTimeout <= 0 {
		return fmt.Errorf("invalid scrape timeout: %s", c.ScrapeTimeout)
	}
	if c.ScrapeMaxRetries < 0 || c.ScrapeMaxRetries > 10 {
		return fmt.Errorf("invalid scrape max retries: %d (want 0-10)", c.ScrapeMaxRetries)
	}
	if c.SyncMinCardCount < 0 {
		return fmt.Errorf("invalid sync min card count: %d", c.SyncMinCardCount)
	}
	switch strings.ToLower(strings.TrimSpace(c.LogLevel)) {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("invalid log level: %q", c.LogLevel)
	}
	return nil
}

func getEnvDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getEnvInt(key string, def int) (int, error) {
	v, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(v) == "" {
		return def, nil
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer, got %q: %w", key, v, err)
	}
	return n, nil
}

func getEnvBool(key string, def bool) (bool, error) {
	v, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(v) == "" {
		return def, nil
	}
	b, err := strconv.ParseBool(strings.TrimSpace(v))
	if err != nil {
		return false, fmt.Errorf("%s must be a boolean, got %q: %w", key, v, err)
	}
	return b, nil
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
