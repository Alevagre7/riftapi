package scrape

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strings"
	"time"
)

const (
	defaultClientTimeout = 30 * time.Second
	maxResponseBytes     = 32 << 20
)

// ClientConfig is the configuration for the HTTP client. Build it from
// env vars in the caller; the scrape package does not read the env
// directly.
type ClientConfig struct {
	BaseURL    string        // e.g. "https://playriftbound.com"
	UserAgent  string        // e.g. "riftapi/0.1 (+https://...)"
	Timeout    time.Duration // per-request; 30s is a good default
	MaxRetries int           // 0 means no retries; 2 is a good default
}

// Client fetches the upstream card gallery. One Client per process is
// sufficient; the underlying http.Client is safe for concurrent use.
type Client struct {
	cfg        ClientConfig
	httpClient *http.Client
}

// NewClient returns a Client that issues requests with the supplied
// configuration.
func NewClient(cfg ClientConfig) *Client {
	if cfg.Timeout <= 0 {
		cfg.Timeout = defaultClientTimeout
	}
	if cfg.MaxRetries < 0 {
		cfg.MaxRetries = 0
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	return &Client{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout: cfg.Timeout,
		},
	}
}

// Fetch GETs the card gallery and returns the response body. It
// retries on 5xx responses and network errors with exponential
// backoff (500ms, 1s, 2s, ...). It does NOT retry on 4xx (those
// are caller errors). Returns the body as bytes on success.
//
// On retry exhaustion, return the last error wrapped with a
// "giving up after N attempts" prefix.
func (c *Client) Fetch(ctx context.Context) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	url := c.URL()

	var lastErr error

	for attempt := 0; attempt <= c.cfg.MaxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff: 500ms, 1s, 2s, 4s, ... capped at
			// 16s so a malformed or unusually large retry setting cannot
			// overflow the duration calculation.
			shift := attempt - 1
			if shift > 5 {
				shift = 5
			}
			wait := 500 * time.Millisecond * time.Duration(1<<shift)
			// Small jitter of approximately ±10%.
			jitter := time.Duration(rand.Int63n(int64(wait)/5)) - wait/10
			wait += jitter

			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				return nil, ctx.Err()
			case <-timer.C:
			}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, fmt.Errorf("creating request: %w", err)
		}
		req.Header.Set("User-Agent", c.cfg.UserAgent)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil, err
			}
			lastErr = fmt.Errorf("request failed: %w", err)
			continue // retry on network / DNS / dial errors
		}

		body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
		closeErr := resp.Body.Close()
		if readErr != nil {
			lastErr = fmt.Errorf("reading body: %w", readErr)
			continue // retry on body read errors
		}
		if closeErr != nil {
			lastErr = fmt.Errorf("closing response body: %w", closeErr)
			continue
		}
		if len(body) > maxResponseBytes {
			return nil, fmt.Errorf("response body exceeds %d bytes", maxResponseBytes)
		}

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return body, nil
		}

		if resp.StatusCode >= 400 && resp.StatusCode < 500 {
			// 4xx: caller error, do not retry.
			return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, responseSummary(body))
		}

		// 5xx or other server-issue status: retry.
		lastErr = fmt.Errorf("unexpected status %d: %s", resp.StatusCode, responseSummary(body))
	}

	return nil, fmt.Errorf("giving up after %d attempts: %w", c.cfg.MaxRetries+1, lastErr)
}

// URL returns the gallery URL the client is configured to fetch.
// Used by tests.
func (c *Client) URL() string {
	return c.cfg.BaseURL + "/en-us/card-gallery/"
}

func responseSummary(body []byte) string {
	const maxSummaryBytes = 2048
	if len(body) > maxSummaryBytes {
		return string(body[:maxSummaryBytes]) + "…"
	}
	return string(body)
}
