package domain

import "time"

// SyncStatus is the outcome of the last sync run, as recorded in the
// sync_state table.
type SyncStatus string

const (
	// SyncStatusOK means the last sync wrote a valid snapshot.
	SyncStatusOK SyncStatus = "ok"
	// SyncStatusFailed means the last sync errored out and the previous
	// snapshot is still in place.
	SyncStatusFailed SyncStatus = "failed"
)

// SyncState is the metadata about the most recent sync run. The store
// has a single row keyed by id=1.
type SyncState struct {
	LastSyncAt    *time.Time
	LastStatus    SyncStatus
	LastSyncInputCount int
	LastBuildID   string
	LastError     string
}
