package health

import "github.com/xalevagre7/riftapi/internal/domain"

// IsHealthy reports whether the store has a successful sync on
// record with at least one card. The card-count guard catches the
// edge case where a sync "succeeds" with an empty payload (e.g. an
// upstream change that broke the parser but returned 200). The API
// will report unhealthy in that state, which is what the maintainer
// wants.
func IsHealthy(s *domain.SyncState) bool {
	return s != nil && s.LastStatus == domain.SyncStatusOK && s.LastCardCount > 0
}
