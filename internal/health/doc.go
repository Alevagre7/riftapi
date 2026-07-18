// Package health owns the /health endpoint and the Telegram alert path
// that fires when a sync fails.
//
// Two entry points:
//
//   - check.go — reads sync_state from the store and returns it as a
//                typed value. Used by the /health handler in the api
//                package.
//   - alert.go — sends a one-line message to a Telegram chat when the
//                sync job's health check fails. The bot token and chat
//                id are read from the environment; the alert is a
//                no-op when alerts are disabled or unconfigured.
//
// Only the sync binary reads the Telegram token. The API binary never
// sees it, which keeps the read-only API free of write-capable secrets.
package health
