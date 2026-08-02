package health_test

import (
	"testing"

	"github.com/xalevagre7/riftapi/internal/domain"
	"github.com/xalevagre7/riftapi/internal/health"
)

func TestIsHealthy(t *testing.T) {
	tests := []struct {
		name      string
		state     *domain.SyncState
		cardCount int
		minimum   int
		want      bool
	}{
		{name: "nil state", cardCount: 10, minimum: 1},
		{name: "failed state", state: &domain.SyncState{LastStatus: domain.SyncStatusFailed}, cardCount: 10, minimum: 1},
		{name: "below threshold", state: &domain.SyncState{LastStatus: domain.SyncStatusOK}, cardCount: 9, minimum: 10},
		{name: "at threshold", state: &domain.SyncState{LastStatus: domain.SyncStatusOK}, cardCount: 10, minimum: 10, want: true},
		{name: "zero threshold", state: &domain.SyncState{LastStatus: domain.SyncStatusOK}, cardCount: 0, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := health.IsHealthy(tt.state, tt.cardCount, tt.minimum); got != tt.want {
				t.Fatalf("IsHealthy() = %v, want %v", got, tt.want)
			}
		})
	}
}
