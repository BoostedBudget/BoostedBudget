package river_test

import (
	"errors"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/queue/river"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- DefaultWorkerConfig ---

func TestDefaultWorkerConfig(t *testing.T) {
	wc := river.DefaultWorkerConfig()
	require.NotNil(t, wc)
	assert.Equal(t, 10, wc.Critical)
	assert.Equal(t, 20, wc.Default)
	assert.Equal(t, 5, wc.Notifications)
	assert.Equal(t, 3, wc.Low)
}

// --- TotalWorkers ---

func TestTotalWorkers(t *testing.T) {
	tests := []struct {
		name     string
		wc       *river.WorkerConfig
		expected int
	}{
		{
			name:     "default config sums to 38",
			wc:       river.DefaultWorkerConfig(),
			expected: 38, // 10 + 20 + 5 + 3
		},
		{
			name: "custom config sums correctly",
			wc: &river.WorkerConfig{
				Critical:      2,
				Default:       4,
				Notifications: 1,
				Low:           1,
			},
			expected: 8,
		},
		{
			name: "all zeros gives zero",
			wc: &river.WorkerConfig{
				Critical:      0,
				Default:       0,
				Notifications: 0,
				Low:           0,
			},
			expected: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.wc.TotalWorkers()
			assert.Equal(t, tc.expected, got)
		})
	}
}

// --- ToRiverQueues ---

func TestToRiverQueues(t *testing.T) {
	wc := river.DefaultWorkerConfig()
	queues := wc.ToRiverQueues()

	require.NotNil(t, queues)
	require.Len(t, queues, 4)

	assert.Equal(t, 10, queues["critical"].MaxWorkers)
	assert.Equal(t, 20, queues["default"].MaxWorkers)
	assert.Equal(t, 5, queues["notifications"].MaxWorkers)
	assert.Equal(t, 3, queues["low"].MaxWorkers)
}

func TestToRiverQueues_ContainsAllExpectedQueues(t *testing.T) {
	wc := &river.WorkerConfig{
		Critical:      1,
		Default:       2,
		Notifications: 3,
		Low:           4,
	}
	queues := wc.ToRiverQueues()

	expectedKeys := []string{"critical", "default", "notifications", "low"}
	for _, key := range expectedKeys {
		_, ok := queues[key]
		assert.True(t, ok, "expected queue %q to be present", key)
	}

	assert.Equal(t, 1, queues["critical"].MaxWorkers)
	assert.Equal(t, 2, queues["default"].MaxWorkers)
	assert.Equal(t, 3, queues["notifications"].MaxWorkers)
	assert.Equal(t, 4, queues["low"].MaxWorkers)
}

// --- DefaultConfig ---

func TestDefaultConfig(t *testing.T) {
	cfg := river.DefaultConfig()

	// Workers should be populated
	require.NotNil(t, cfg.Workers)
	assert.Equal(t, 38, cfg.Workers.TotalWorkers())

	// Timeouts should be non-zero
	assert.Equal(t, 30*time.Minute, cfg.JobTimeout)
	assert.Equal(t, 1*time.Hour, cfg.RescueStuckJobsAfter)

	// Poll timings should be non-zero
	assert.Equal(t, 200*time.Millisecond, cfg.FetchCooldown)
	assert.Equal(t, 500*time.Millisecond, cfg.FetchPollInterval)

	// RetryPolicy should be set
	require.NotNil(t, cfg.RetryPolicy)
}

// --- DefaultRetryPolicy ---

func TestDefaultRetryPolicy(t *testing.T) {
	rp := river.DefaultRetryPolicy()
	require.NotNil(t, rp)
	assert.Equal(t, 5, rp.MaxAttempts)
	assert.Equal(t, 4.0, rp.BackoffMultiplier)
	assert.Equal(t, 30*time.Second, rp.InitialBackoff)
	assert.Equal(t, 2*time.Hour, rp.MaxBackoff)
}

// --- RetryPolicy.NextRetry ---

func TestRetryPolicy_NextRetry(t *testing.T) {
	rp := river.DefaultRetryPolicy()
	_ = errors.New("some error") // unused but documents intent

	tests := []struct {
		name            string
		attempt         int
		expectedAtLeast time.Duration
		expectedAtMost  time.Duration
	}{
		{
			name:            "attempt 0 returns initial backoff",
			attempt:         0,
			expectedAtLeast: 30 * time.Second,
			expectedAtMost:  30 * time.Second,
		},
		{
			name:            "attempt 1 returns initial backoff",
			attempt:         1,
			expectedAtLeast: 30 * time.Second,
			expectedAtMost:  30 * time.Second,
		},
		{
			name:            "attempt 2 returns initial * multiplier = 2 min",
			attempt:         2,
			expectedAtLeast: 2 * time.Minute,
			expectedAtMost:  2 * time.Minute,
		},
		{
			name:            "attempt 3 returns initial * multiplier^2 = 8 min",
			attempt:         3,
			expectedAtLeast: 8 * time.Minute,
			expectedAtMost:  8 * time.Minute,
		},
		{
			name:            "attempt 4 returns initial * multiplier^3 = 32 min",
			attempt:         4,
			expectedAtLeast: 32 * time.Minute,
			expectedAtMost:  32 * time.Minute,
		},
		{
			name:            "attempt 5 returns initial * multiplier^4 = 2 hr (max)",
			attempt:         5,
			expectedAtLeast: 2 * time.Hour,
			expectedAtMost:  2 * time.Hour,
		},
		{
			name:            "large attempt capped at MaxBackoff",
			attempt:         100,
			expectedAtLeast: 2 * time.Hour,
			expectedAtMost:  2 * time.Hour,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := rp.NextRetry(tc.attempt)
			assert.GreaterOrEqual(t, got, tc.expectedAtLeast,
				"retry for attempt %d should be >= %v", tc.attempt, tc.expectedAtLeast)
			assert.LessOrEqual(t, got, tc.expectedAtMost,
				"retry for attempt %d should be <= %v", tc.attempt, tc.expectedAtMost)
		})
	}
}

func TestRetryPolicy_NextRetry_ReturnsPositiveDuration(t *testing.T) {
	rp := river.DefaultRetryPolicy()
	for attempt := 0; attempt <= 10; attempt++ {
		got := rp.NextRetry(attempt)
		assert.Greater(t, got, time.Duration(0),
			"NextRetry(attempt=%d) should return a positive duration", attempt)
	}
}

func TestRetryPolicy_NextRetry_CustomPolicy(t *testing.T) {
	rp := &river.RetryPolicy{
		MaxAttempts:       3,
		BackoffMultiplier: 2,
		InitialBackoff:    10 * time.Second,
		MaxBackoff:        1 * time.Minute,
	}

	assert.Equal(t, 10*time.Second, rp.NextRetry(1))   // initial
	assert.Equal(t, 20*time.Second, rp.NextRetry(2))   // initial * 2
	assert.Equal(t, 40*time.Second, rp.NextRetry(3))   // initial * 2^2
	assert.Equal(t, 1*time.Minute, rp.NextRetry(4))    // capped at MaxBackoff
	assert.Equal(t, 1*time.Minute, rp.NextRetry(100))  // still capped
}
