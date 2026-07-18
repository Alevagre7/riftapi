package health_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xalevagre7/riftapi/internal/health"
)

// --- noop ------------------------------------------------------------------

func TestNoopSender_AlwaysSucceeds(t *testing.T) {
	var s health.NoopSender
	if err := s.Send(context.Background(), "anything"); err != nil {
		t.Errorf("NoopSender.Send returned %v, want nil", err)
	}
}

func TestNewSender_ReturnsNoopWhenDisabled(t *testing.T) {
	s := health.NewSender(health.SenderConfig{
		Enabled:     false,
		BotToken:    "token",
		AdminChatID: "12345",
	})
	if _, ok := s.(health.NoopSender); !ok {
		t.Errorf("NewSender(disabled) = %T, want NoopSender", s)
	}
}

func TestNewSender_ReturnsNoopWhenTokenMissing(t *testing.T) {
	s := health.NewSender(health.SenderConfig{
		Enabled:     true,
		BotToken:    "",
		AdminChatID: "12345",
	})
	if _, ok := s.(health.NoopSender); !ok {
		t.Errorf("NewSender(no token) = %T, want NoopSender", s)
	}
}

func TestNewSender_ReturnsNoopWhenChatIDMissing(t *testing.T) {
	s := health.NewSender(health.SenderConfig{
		Enabled:     true,
		BotToken:    "token",
		AdminChatID: "",
	})
	if _, ok := s.(health.NoopSender); !ok {
		t.Errorf("NewSender(no chat id) = %T, want NoopSender", s)
	}
}

func TestNewSender_ReturnsTelegramWhenFullyConfigured(t *testing.T) {
	s := health.NewSender(health.SenderConfig{
		Enabled:     true,
		BotToken:    "token",
		AdminChatID: "12345",
	})
	if _, ok := s.(*health.TelegramSender); !ok {
		t.Errorf("NewSender(configured) = %T, want *TelegramSender", s)
	}
}

// --- telegram ---------------------------------------------------------------

func TestTelegramSender_PostsExpectedRequest(t *testing.T) {
	var (
		gotPath        atomic.Value // string
		gotBody        atomic.Value // string
		gotContentType atomic.Value // string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath.Store(r.URL.Path)
		gotContentType.Store(r.Header.Get("Content-Type"))
		body, _ := io.ReadAll(r.Body)
		gotBody.Store(string(body))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":1}}`))
	}))
	t.Cleanup(server.Close)

	sender := health.NewTelegramSenderWithBaseURL("my-token", "42", server.URL)
	if err := sender.Send(context.Background(), "hello world"); err != nil {
		t.Fatalf("Send: %v", err)
	}

	if path, _ := gotPath.Load().(string); path != "/botmy-token/sendMessage" {
		t.Errorf("path = %q, want /botmy-token/sendMessage", path)
	}
	if ct, _ := gotContentType.Load().(string); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	if body, _ := gotBody.Load().(string); !strings.Contains(body, `"chat_id":"42"`) {
		t.Errorf("body = %q, missing chat_id", body)
	}
	if body, _ := gotBody.Load().(string); !strings.Contains(body, `"text":"hello world"`) {
		t.Errorf("body = %q, missing text", body)
	}
}

func TestTelegramSender_ReturnsErrorOnNon200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"ok":false,"description":"chat not found"}`))
	}))
	t.Cleanup(server.Close)

	sender := health.NewTelegramSenderWithBaseURL("token", "42", server.URL)
	err := sender.Send(context.Background(), "x")
	if err == nil {
		t.Errorf("expected error for 500 response, got nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error = %v, want message containing 500", err)
	}
}

func TestTelegramSender_ReturnsErrorOnNetworkError(t *testing.T) {
	// A base URL that resolves but refuses connections.
	sender := health.NewTelegramSenderWithBaseURL("token", "42", "http://127.0.0.1:1")
	err := sender.Send(context.Background(), "x")
	if err == nil {
		t.Errorf("expected error for network failure, got nil")
	}
}

func TestTelegramSender_RespectsContextCancellation(t *testing.T) {
	// Server would normally take 5s to respond. The client's context
	// is cancelled well before that; Send must return promptly with
	// an error. The select in the handler ensures it can return on
	// either path — important because t.Cleanup(server.Close) waits
	// for the handler to return, and an unbreakable block would hang
	// the test forever.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(5 * time.Second):
			w.WriteHeader(http.StatusOK)
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(server.Close)

	sender := health.NewTelegramSenderWithBaseURL("token", "42", server.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := sender.Send(ctx, "x")
	elapsed := time.Since(start)

	if err == nil {
		t.Errorf("expected error from cancelled context, got nil")
	}
	if elapsed > 2*time.Second {
		t.Errorf("Send took %v after context cancellation; want < 2s", elapsed)
	}
}
