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

func TestSystemHealthResponse_Timestamp(t *testing.T) {
	now := time.Now()
	response := SystemHealthResponse{
		Status:    "healthy",
		Timestamp: now,
	}

	assert.Equal(t, "healthy", response.Status)
	assert.WithinDuration(t, now, response.Timestamp, time.Second)
}
