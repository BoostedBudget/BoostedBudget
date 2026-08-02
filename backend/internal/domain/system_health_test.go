package domain

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// Note: Status calculation logic is tested in service layer (TestSystemHealthService_CalculateOverallStatus)
// as the logic lives in systemHealthService.calculateOverallStatus()

func TestConnectionPoolStats_Validation(t *testing.T) {
	stats := ConnectionPoolStats{
		Active:       10,
		Idle:         5,
		Max:          100,
		Constructing: 2,
		Total:        17,
	}

	assert.Equal(t, int32(10), stats.Active)
	assert.Equal(t, int32(5), stats.Idle)
	assert.Equal(t, int32(100), stats.Max)
	assert.Equal(t, int32(2), stats.Constructing)
	assert.Equal(t, int32(17), stats.Total)
}

func TestDatabaseHealth_StatusCheck(t *testing.T) {
	tests := []struct {
		name           string
		health         DatabaseHealth
		expectedStatus string
	}{
		{
			name: "up when status is up",
			health: DatabaseHealth{
				Status:      "up",
				Connections: ConnectionPoolStats{Active: 5, Idle: 10, Max: 100},
				LatencyMs:   5,
			},
			expectedStatus: "up",
		},
		{
			name: "down when status is down",
			health: DatabaseHealth{
				Status:      "down",
				Connections: ConnectionPoolStats{},
				LatencyMs:   0,
			},
			expectedStatus: "down",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expectedStatus, tt.health.Status)
		})
	}
}

func TestQueueMetrics_Calculation(t *testing.T) {
	metrics := QueueMetrics{
		Pending:           10,
		Running:           5,
		Failed:            2,
		CompletedLastHour: 100,
	}

	assert.Equal(t, int64(10), metrics.Pending)
	assert.Equal(t, int64(5), metrics.Running)
	assert.Equal(t, int64(2), metrics.Failed)
	assert.Equal(t, int64(100), metrics.CompletedLastHour)
}

func TestSyncStatusMetrics_Validation(t *testing.T) {
	metrics := SyncStatusMetrics{
		ActiveSyncs:       3,
		FailedLastHour:    2,
		AverageDurationMs: 3500,
	}

	assert.Equal(t, int64(3), metrics.ActiveSyncs)
	assert.Equal(t, int64(2), metrics.FailedLastHour)
	assert.Equal(t, int64(3500), metrics.AverageDurationMs)
}

func TestDeadLetterMetrics_Validation(t *testing.T) {
	metrics := DeadLetterMetrics{
		Count:             5,
		OldestJobAgeHours: 12,
	}

	assert.Equal(t, int64(5), metrics.Count)
	assert.Equal(t, int64(12), metrics.OldestJobAgeHours)
}

// TestJobBacklogMetrics_Validation covers the three DB-derived backlog gauges surfaced on
// /admin/health (Issue #1458) — structurally identical to DeadLetterMetrics above, mirroring the
// same Paddle-metrics precedent (#762/#761).
//
// Batch R4 Task 4: before this fix, this test was a tautology — it assigned three literals to a
// struct and asserted the exact same three literals back, which cannot fail short of the Go
// compiler being broken, and never touched Available at all. Strengthened to actually exercise
// Available's documented fail-closed contract (see JobBacklogMetrics.Available's own doc comment,
// system_health.go): the zero value must read as "unavailable" (never a falsely-confident
// "available, verified zero"), and an explicitly-set true must read back as true.
func TestJobBacklogMetrics_Validation(t *testing.T) {
	var zeroValue JobBacklogMetrics
	assert.False(t, zeroValue.Available,
		"Available must default to false (the Go zero value) — any code path that forgets to explicitly report success must read as unavailable, never as a falsely-confident verified zero")

	metrics := JobBacklogMetrics{
		PlaidCleanupOutstandingUsers:                    3,
		PlaidCleanupOldestOutstandingSeconds:            7200,
		ManualTierOverrideOldestUnclearedExpiredSeconds: 3600,
		Available: true,
	}

	assert.Equal(t, int64(3), metrics.PlaidCleanupOutstandingUsers)
	assert.Equal(t, int64(7200), metrics.PlaidCleanupOldestOutstandingSeconds)
	assert.Equal(t, int64(3600), metrics.ManualTierOverrideOldestUnclearedExpiredSeconds)
	assert.True(t, metrics.Available, "Available must be true when a query has genuinely verified the backlog (possibly a real zero)")
}

func TestSystemHealthResponse_Timestamp(t *testing.T) {
	now := time.Now()
	response := SystemHealthResponse{
		Status:    "healthy",
		Timestamp: now,
	}

	assert.Equal(t, "healthy", response.Status)
	assert.WithinDuration(t, now, response.Timestamp, time.Second)
}
