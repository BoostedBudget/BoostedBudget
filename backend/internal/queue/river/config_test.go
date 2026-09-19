package river_test

import (
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
	assert.Equal(t, 2, wc.RevenueCat)
}

// --- TotalWorkers ---

func TestTotalWorkers(t *testing.T) {
	tests := []struct {
		name     string
		wc       *river.WorkerConfig
		expected int
	}{
		{
			name:     "default config includes two RevenueCat workers",
			wc:       river.DefaultWorkerConfig(),
			expected: 40, // 10 + 20 + 5 + 3 + 2
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
	require.Len(t, queues, 5)
	assert.Equal(t, 2, queues["revenuecat"].MaxWorkers)

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
	assert.NotContains(t, queues, "revenuecat", "a custom zero limit omits the queue")
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
	assert.Equal(t, 40, cfg.Workers.TotalWorkers())

	// Timeouts should be non-zero
	assert.Equal(t, 30*time.Minute, cfg.JobTimeout)
	assert.Equal(t, 1*time.Hour, cfg.RescueStuckJobsAfter)

	// Poll timings should be non-zero
	assert.Equal(t, 200*time.Millisecond, cfg.FetchCooldown)
	assert.Equal(t, 500*time.Millisecond, cfg.FetchPollInterval)
}
