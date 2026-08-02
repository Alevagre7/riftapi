package config_test

import (
	"os"
	"strings"
	"testing"

	"github.com/xalevagre7/riftapi/internal/config"
)

func TestLoad_UsesSafeDefaults(t *testing.T) {
	for _, key := range []string{
		"RIFTAPI_API_PORT",
		"RIFTAPI_SCRAPE_TIMEOUT_SECS",
		"RIFTAPI_SCRAPE_MAX_RETRIES",
		"RIFTAPI_SYNC_MIN_CARD_COUNT",
		"RIFTAPI_SYNC_ENABLED",
		"RIFTAPI_TELEGRAM_ALERTS_ENABLED",
	} {
		t.Setenv(key, "")
	}
	oldRequired, hadRequired := os.LookupEnv("RIFTAPI_SYNC_REQUIRED_IDS")
	_ = os.Unsetenv("RIFTAPI_SYNC_REQUIRED_IDS")
	t.Cleanup(func() {
		if hadRequired {
			_ = os.Setenv("RIFTAPI_SYNC_REQUIRED_IDS", oldRequired)
		} else {
			_ = os.Unsetenv("RIFTAPI_SYNC_REQUIRED_IDS")
		}
	})
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate defaults: %v", err)
	}
	if cfg.APIPort != 8080 || cfg.ScrapeMaxRetries != 2 || cfg.SyncMinCardCount != 1100 {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	if len(cfg.SyncRequiredIDs) != 4 {
		t.Fatalf("SyncRequiredIDs = %v, want four defaults", cfg.SyncRequiredIDs)
	}
}

func TestLoad_RejectsMalformedInteger(t *testing.T) {
	t.Setenv("RIFTAPI_API_PORT", "eight-thousand")
	_, err := config.Load()
	if err == nil || !strings.Contains(err.Error(), "RIFTAPI_API_PORT") {
		t.Fatalf("Load error = %v, want malformed-port error", err)
	}
}

func TestLoad_RejectsMalformedBoolean(t *testing.T) {
	t.Setenv("RIFTAPI_SYNC_ENABLED", "sometimes")
	_, err := config.Load()
	if err == nil || !strings.Contains(err.Error(), "RIFTAPI_SYNC_ENABLED") {
		t.Fatalf("Load error = %v, want malformed-boolean error", err)
	}
}

func TestLoad_EmptyRequiredIDsDisablesOptionalCheck(t *testing.T) {
	t.Setenv("RIFTAPI_SYNC_REQUIRED_IDS", "")
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.SyncRequiredIDs) != 0 {
		t.Fatalf("SyncRequiredIDs = %v, want empty", cfg.SyncRequiredIDs)
	}
}

func TestValidate_RejectsRetryOverflow(t *testing.T) {
	cfg := &config.Config{APIPort: 8080, DatabasePath: "db", ScrapeTimeout: 1, ScrapeMaxRetries: 11, ScrapeUserAgent: "test", LogLevel: "info"}
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate returned nil for too many retries")
	}
}

func TestValidate_RejectsNilConfig(t *testing.T) {
	var cfg *config.Config
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate returned nil for a nil config")
	}
}
