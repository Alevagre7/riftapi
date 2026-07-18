package health

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// AlertSender is the contract a sync job uses to notify the maintainer
// when something has gone wrong. Implementations must be safe for
// concurrent use (a sync job is a oneshot process, but tests reuse
// senders across goroutines).
type AlertSender interface {
	// Send posts a one-line message to the configured destination. A
	// non-nil error means the message did not reach the destination;
	// the caller is expected to log it but not retry (the next sync
	// will alert again if the failure persists).
	Send(ctx context.Context, message string) error
}

// --- noop ------------------------------------------------------------------

// NoopSender is the AlertSender used when alerts are disabled or
// unconfigured. It is a sentinel value: construct it directly with
// the zero value, or use NewSender below which returns one
// automatically when configuration is missing.
type NoopSender struct{}

// Send on a NoopSender always returns nil.
func (NoopSender) Send(_ context.Context, _ string) error { return nil }

// --- telegram ---------------------------------------------------------------

// TelegramSender posts messages to the Telegram Bot API. One
// instance is cheap; the underlying http.Client has a 10s timeout
// and is safe for the few requests a sync job makes.
type TelegramSender struct {
	token   string
	chatID  string
	baseURL string
	client  *http.Client
}

// NewTelegramSender returns a TelegramSender that posts to the
// official api.telegram.org endpoint. Use NewTelegramSenderWithBaseURL
// in tests to point at a mock server.
func NewTelegramSender(token, chatID string) *TelegramSender {
	return NewTelegramSenderWithBaseURL(token, chatID, "https://api.telegram.org")
}

// NewTelegramSenderWithBaseURL is like NewTelegramSender but with a
// configurable base URL. Exported because tests in other packages
// (and the integration tests in this package) need to inject a
// httptest server's URL.
func NewTelegramSenderWithBaseURL(token, chatID, baseURL string) *TelegramSender {
	return &TelegramSender{
		token:   token,
		chatID:  chatID,
		baseURL: baseURL,
		client:  &http.Client{Timeout: 10 * time.Second},
	}
}

// Send posts the message to chatID via the configured bot. The
// Telegram Bot API expects a JSON body with chat_id and text. The
// response is checked for a 200 status; the body's `ok` field is
// not validated (Telegram returns 200 with `ok: false` for some
// error cases, and the maintainer can investigate from the logs).
func (s *TelegramSender) Send(ctx context.Context, message string) error {
	url := s.baseURL + "/bot" + s.token + "/sendMessage"
	body, err := json.Marshal(map[string]string{
		"chat_id": s.chatID,
		"text":    message,
	})
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("send: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("telegram returned status %d", resp.StatusCode)
	}
	return nil
}

// --- factory ---------------------------------------------------------------

// SenderConfig is the subset of config.Config that the alert
// factory reads. Defined locally so this package does not import
// the config package (the config package is imported by the binary
// entry points, not the library code).
type SenderConfig struct {
	Enabled     bool
	BotToken    string
	AdminChatID string
}

// NewSender returns the AlertSender the sync job should use, based
// on the supplied configuration. If alerts are disabled or either
// required field (token, chat id) is empty, returns a NoopSender.
// This is the only entry point the sync job needs.
func NewSender(cfg SenderConfig) AlertSender {
	if !cfg.Enabled || cfg.BotToken == "" || cfg.AdminChatID == "" {
		return NoopSender{}
	}
	return NewTelegramSender(cfg.BotToken, cfg.AdminChatID)
}
