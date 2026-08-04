package domain

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

// TestJobBacklogMetrics_FieldsRoundTrip is a plain struct-literal smoke test — matches the
// sibling DeadLetterMetrics/SyncStatusMetrics tests above/below in this file. It is NOT a
// behavioral test: constructing a struct and reading back the literals just assigned cannot fail
// short of the Go compiler being broken, and this specifically does NOT exercise
// SystemHealthService.getJobBacklogMetrics — the actual producer of a real JobBacklogMetrics
// value — at all.
//
// Batch S2 Task 5 correction: this test previously (as TestJobBacklogMetrics_Validation) also
// asserted `assert.False(t, zeroValue.Available, ...)` against a bare Go zero value and claimed,
// in its own comment, to have been "strengthened to actually exercise Available's documented
// fail-closed contract" — that claim was FALSE. Asserting a zero-value bool defaults to false is
// a property of the Go language, not of this type's contract, and cannot fail either. Both
// tautological assertions are deleted here, not merely restated.
//
// The producer's actual fail-closed contract — Available: false on either query erroring,
// Available: true only when both succeed — is exercised at the SERVICE layer, where the producer
// (getJobBacklogMetrics, an unexported method of systemHealthService) actually lives and can be
// called: see TestGetSystemHealth_JobBacklogMetricsPopulated (success path),
// TestGetSystemHealth_JobBacklogMetricsError_UsesZeroValues (first query fails),
// TestGetSystemHealth_JobBacklogMetrics_SecondQueryError_DiscardsFirstQuerysGoodData (second query
// fails — this ALREADY existed before this task; it is not a gap this task closes), and
// TestGetSystemHealth_JobBacklogMetrics_UnavailableVsRealZero_AreDistinguishable (all in
// system_health_service_test.go). This domain-package test cannot reach that unexported method at
// all — the domain package only defines the wire type — so "test the real producer instead" means
// pointing at those existing tests, not duplicating them here.
func TestJobBacklogMetrics_FieldsRoundTrip(t *testing.T) {
	metrics := JobBacklogMetrics{
		PlaidCleanupOutstandingUsers:                    3,
		PlaidCleanupOldestOutstandingSeconds:            7200,
		ManualTierOverrideOldestUnclearedExpiredSeconds: 3600,
		Available: true,
	}

	assert.Equal(t, int64(3), metrics.PlaidCleanupOutstandingUsers)
	assert.Equal(t, int64(7200), metrics.PlaidCleanupOldestOutstandingSeconds)
	assert.Equal(t, int64(3600), metrics.ManualTierOverrideOldestUnclearedExpiredSeconds)
	assert.True(t, metrics.Available)
}

// TestJobBacklogMetrics_Available_SerializesEvenWhenFalse is a genuinely falsifiable
// domain-level test (Batch S2 Task 5): it exercises the actual `available` JSON tag on the wire
// type, not a hand-constructed literal. Available's own doc comment (system_health.go) says it
// "defaults to the Go zero value (false) — the fail-closed direction" specifically so a caller
// that forgot to set it reads as unavailable, NOT as an absent/undefined field. That guarantee
// would silently break if a future edit added `,omitempty` to the `available` json tag: Go's
// encoding/json omits a `false` bool with omitempty, so the key would vanish from the response
// entirely for the "never queried, zero value" case — indistinguishable on the wire from a field
// that was never added at all, rather than explicit `false`.
//
// Falsified by adding `,omitempty` to Available's json tag in system_health.go: this test then
// fails because `"available"` no longer appears in the marshaled output at all.
func TestJobBacklogMetrics_Available_SerializesEvenWhenFalse(t *testing.T) {
	var zeroValue JobBacklogMetrics // Available is false, the Go zero value — never explicitly set

	b, err := json.Marshal(zeroValue)
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(b, &decoded))

	availableValue, present := decoded["available"]
	require.True(t, present,
		"the \"available\" key must be present in the marshaled JSON even when Available is the "+
			"Go zero value (false) — an omitempty tag would drop it, making a genuinely-unavailable "+
			"gauge indistinguishable on the wire from a field that was never added")
	assert.Equal(t, false, availableValue)
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
