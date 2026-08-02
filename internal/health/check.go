package health

import "github.com/xalevagre7/riftapi/internal/domain"

// IsHealthy reports whether the latest sync succeeded and the current
// snapshot meets the configured card-count threshold. Keeping this predicate
// here makes the API's health decision use the same status vocabulary as the
// scraper without coupling the health package to storage.
func IsHealthy(s *domain.SyncState, cardCount, minCardCount int) bool {
	return s != nil && s.LastStatus == domain.SyncStatusOK && cardCount >= minCardCount
}
