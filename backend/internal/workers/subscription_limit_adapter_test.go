// Package workers provides background job processing for automation rules.
// Issue #37: Rule Scheduler - Tests for SubscriptionLimitAdapter.
package workers

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSubscriptionLimitAdapter_DefaultLimits(t *testing.T) {
	limits := DefaultDailyLimits()

	assert.Equal(t, 1, limits["free"])
	assert.Equal(t, 10, limits["premium_lite"])
	assert.Equal(t, 50, limits["premium"])
	assert.Equal(t, 1000, limits["premium_plus"])
}

func TestSubscriptionLimitAdapter_NewAdapter(t *testing.T) {
	adapter := NewSubscriptionLimitAdapter()

	assert.NotNil(t, adapter)
	assert.NotNil(t, adapter.dailyExecutions)
	assert.NotNil(t, adapter.dailyLimits)
}

func TestSubscriptionLimitAdapter_NewAdapterWithLimits(t *testing.T) {
	customLimits := map[string]int{
		"test": 100,
	}
	adapter := NewSubscriptionLimitAdapterWithLimits(customLimits)

	assert.NotNil(t, adapter)
	assert.Equal(t, 100, adapter.dailyLimits["test"])
}

func TestSubscriptionLimitAdapter_CheckDailyExecutionLimit(t *testing.T) {
	adapter := NewSubscriptionLimitAdapter()
	ctx := context.Background()

	// First execution should be allowed
	allowed, remaining, err := adapter.CheckDailyExecutionLimit(ctx, "user-1")
	require.NoError(t, err)
	assert.True(t, allowed)
	assert.Equal(t, 10, remaining) // premium_lite default

	// Increment to limit
	for i := 0; i < 10; i++ {
		err := adapter.IncrementDailyExecutions(ctx, "user-1")
		require.NoError(t, err)
	}

	// Should now be at limit
	allowed, remaining, err = adapter.CheckDailyExecutionLimit(ctx, "user-1")
	require.NoError(t, err)
	assert.False(t, allowed)
	assert.Equal(t, 0, remaining)
}

func TestSubscriptionLimitAdapter_GetUserExecutionCount(t *testing.T) {
	adapter := NewSubscriptionLimitAdapter()
	ctx := context.Background()

	// Initially zero
	count := adapter.GetUserExecutionCount("user-1")
	assert.Equal(t, 0, count)

	// After increment
	_ = adapter.IncrementDailyExecutions(ctx, "user-1")
	count = adapter.GetUserExecutionCount("user-1")
	assert.Equal(t, 1, count)
}

func TestSubscriptionLimitAdapter_SetUserLimit(t *testing.T) {
	adapter := NewSubscriptionLimitAdapter()

	// Should not panic
	adapter.SetUserLimit("user-1", 999)
	// Currently a no-op, just verify it doesn't crash
}

func TestSubscriptionLimitAdapter_ResetIfNewDay(t *testing.T) {
	adapter := NewSubscriptionLimitAdapter()
	ctx := context.Background()

	// Add some executions
	_ = adapter.IncrementDailyExecutions(ctx, "user-1")
	_ = adapter.IncrementDailyExecutions(ctx, "user-1")

	count := adapter.GetUserExecutionCount("user-1")
	assert.Equal(t, 2, count)

	// Manually set lastReset to yesterday
	adapter.mu.Lock()
	adapter.lastReset = time.Now().UTC().Add(-25 * time.Hour)
	adapter.mu.Unlock()

	// Next check should reset the count
	_, _, _ = adapter.CheckDailyExecutionLimit(ctx, "user-1")

	count = adapter.GetUserExecutionCount("user-1")
	assert.Equal(t, 0, count)
}
