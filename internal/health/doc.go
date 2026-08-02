// Package health owns shared sync-health decisions and the Telegram alert
// path that fires when a sync fails.
//
// Two entry points:
//
//   - check.go — decides whether a sync state and card count satisfy the
//     API's configured health threshold.
//   - alert.go — sends a one-line message to a Telegram chat when the
//     sync job's health check fails. The bot token and chat
//     id are read from the environment; the alert is a
//     no-op when alerts are disabled or unconfigured.
//
// Only the scraper passes the Telegram token to the alert sender. The API
// never uses it, which keeps the read-only API free of write-capable secrets.
package health
