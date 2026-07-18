package scrape_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xalevagre7/riftapi/internal/scrape"
)

func TestClient_Fetch_200(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("hello cards"))
	}))
	defer srv.Close()

	client := scrape.NewClient(scrape.ClientConfig{
		BaseURL:    srv.URL,
		UserAgent:  "test/1.0",
		Timeout:    5 * time.Second,
		MaxRetries: 0,
	})

	body, err := client.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if string(body) != "hello cards" {
		t.Errorf("body = %q, want %q", string(body), "hello cards")
	}
}

func TestClient_Fetch_RetrySuccess(t *testing.T) {
	t.Parallel()

	var callCount int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := atomic.AddInt32(&callCount, 1)
		if n == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("retry later"))
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok after retry"))
	}))
	defer srv.Close()

	client := scrape.NewClient(scrape.ClientConfig{
		BaseURL:    srv.URL,
		UserAgent:  "test/1.0",
		Timeout:    5 * time.Second,
		MaxRetries: 2,
	})

	body, err := client.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if string(body) != "ok after retry" {
		t.Errorf("body = %q, want %q", string(body), "ok after retry")
	}
	if n := atomic.LoadInt32(&callCount); n != 2 {
		t.Errorf("expected 2 calls, got %d", n)
	}
}

func TestClient_Fetch_RetryExhausted(t *testing.T) {
	t.Parallel()

	var callCount int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("always fails"))
	}))
	defer srv.Close()

	client := scrape.NewClient(scrape.ClientConfig{
		BaseURL:    srv.URL,
		UserAgent:  "test/1.0",
		Timeout:    5 * time.Second,
		MaxRetries: 2,
	})

	_, err := client.Fetch(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if want := "giving up after 3 attempts"; !strings.Contains(err.Error(), want) {
		t.Errorf("error = %q, want it to contain %q", err.Error(), want)
	}
	if n := atomic.LoadInt32(&callCount); n != 3 {
		t.Errorf("expected 3 calls, got %d", n)
	}
}

func TestClient_Fetch_UserAgent(t *testing.T) {
	t.Parallel()

	var gotUA string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	defer srv.Close()

	client := scrape.NewClient(scrape.ClientConfig{
		BaseURL:    srv.URL,
		UserAgent:  "riftapi-test/0.1",
		Timeout:    5 * time.Second,
		MaxRetries: 0,
	})

	if _, err := client.Fetch(context.Background()); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if gotUA != "riftapi-test/0.1" {
		t.Errorf("User-Agent = %q, want %q", gotUA, "riftapi-test/0.1")
	}
}

func TestClient_Fetch_Cancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // context is already cancelled

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	defer srv.Close()

	client := scrape.NewClient(scrape.ClientConfig{
		BaseURL:    srv.URL,
		UserAgent:  "test/1.0",
		Timeout:    5 * time.Second,
		MaxRetries: 0,
	})

	_, err := client.Fetch(ctx)
	if err == nil {
		t.Fatal("expected error due to cancelled context, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestClient_Fetch_CancellationDuringRetry(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())

	var callCount int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := atomic.AddInt32(&callCount, 1)
		if n == 1 {
			// First call fails with 500.
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("fail"))
			return
		}
		// Second call should never arrive because we cancel after the first.
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("should not reach"))
	}))
	defer srv.Close()

	client := scrape.NewClient(scrape.ClientConfig{
		BaseURL:    srv.URL,
		UserAgent:  "test/1.0",
		Timeout:    5 * time.Second,
		MaxRetries: 2,
	})

	// Use a goroutine to cancel the context during the retry backoff.
	go func() {
		// Wait a tiny bit to ensure the first request has been made.
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	_, err := client.Fetch(ctx)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	// We expect either context.Canceled or a context-related error.
	if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected context error, got %v", err)
	}
	// The server should have been called at most 2 times (first request + possibly
	// a second that got cancelled before it could complete).
	if n := atomic.LoadInt32(&callCount); n > 2 {
		t.Errorf("expected at most 2 calls, got %d", n)
	}
}

func TestClient_Fetch_4xxNoRetry(t *testing.T) {
	t.Parallel()

	var callCount int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("bad request body"))
	}))
	defer srv.Close()

	client := scrape.NewClient(scrape.ClientConfig{
		BaseURL:    srv.URL,
		UserAgent:  "test/1.0",
		Timeout:    5 * time.Second,
		MaxRetries: 2,
	})

	_, err := client.Fetch(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("error = %q, want it to contain '400'", err.Error())
	}
	if !strings.Contains(err.Error(), "bad request body") {
		t.Errorf("error = %q, want it to contain the response body", err.Error())
	}
	if n := atomic.LoadInt32(&callCount); n != 1 {
		t.Errorf("expected 1 call (no retry on 4xx), got %d", n)
	}
}
