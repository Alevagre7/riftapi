package scrape

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"time"
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
	url := c.URL()

	var lastErr error

	for attempt := 0; attempt <= c.cfg.MaxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff: 500ms, 1s, 2s, 4s, ...
			wait := time.Duration(500*(1<<(attempt-1))) * time.Millisecond
			// Small jitter of ±20%
			jitter := time.Duration(rand.Int63n(int64(wait)/5)) - wait/10
			wait += jitter

			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(wait):
			}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, fmt.Errorf("creating request: %w", err)
		}
		req.Header.Set("User-Agent", c.cfg.UserAgent)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("request failed: %w", err)
			continue // retry on network / DNS / dial errors
		}

		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			lastErr = fmt.Errorf("reading body: %w", readErr)
			continue // retry on body read errors
		}

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return body, nil
		}

		if resp.StatusCode >= 400 && resp.StatusCode < 500 {
			// 4xx: caller error, do not retry.
			return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
		}

		// 5xx or other server-issue status: retry.
		lastErr = fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}

	return nil, fmt.Errorf("giving up after %d attempts: %w", c.cfg.MaxRetries+1, lastErr)
}

// URL returns the gallery URL the client is configured to fetch.
// Used by tests.
func (c *Client) URL() string {
	return c.cfg.BaseURL + "/en-us/card-gallery/"
}
