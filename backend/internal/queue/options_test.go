package queue_test

import (
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- DefaultEnqueueOptions ---

func TestDefaultEnqueueOptions(t *testing.T) {
	opts := queue.DefaultEnqueueOptions()
	assert.Equal(t, "default", opts.Queue)
	assert.Equal(t, 2, opts.Priority)
	assert.Equal(t, 5, opts.MaxAttempts)
	assert.Nil(t, opts.Unique)
	assert.Nil(t, opts.Tags)
	assert.Nil(t, opts.Metadata)
}

// --- ApplyOptions ---

func TestApplyOptions_NoOptions_ReturnsDefaults(t *testing.T) {
	opts := queue.ApplyOptions(nil)
	assert.Equal(t, "default", opts.Queue)
	assert.Equal(t, 2, opts.Priority)
	assert.Equal(t, 5, opts.MaxAttempts)
}

func TestApplyOptions_MultipleOptions_AllApplied(t *testing.T) {
	opts := queue.ApplyOptions([]queue.EnqueueOption{
		queue.WithQueue("critical"),
		queue.WithPriority(1),
		queue.WithMaxAttempts(3),
	})
	assert.Equal(t, "critical", opts.Queue)
	assert.Equal(t, 1, opts.Priority)
	assert.Equal(t, 3, opts.MaxAttempts)
}

// --- WithQueue ---

func TestWithQueue(t *testing.T) {
	tests := []struct {
		name     string
		queue    string
		expected string
	}{
		{"critical queue", queue.QueueCritical, "critical"},
		{"default queue", queue.QueueDefault, "default"},
		{"notifications queue", queue.QueueNotifications, "notifications"},
		{"low queue", queue.QueueLow, "low"},
		{"custom queue name", "my-custom-queue", "my-custom-queue"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			opts := queue.ApplyOptions([]queue.EnqueueOption{queue.WithQueue(tc.queue)})
			assert.Equal(t, tc.expected, opts.Queue)
		})
	}
}

// --- WithPriority ---

func TestWithPriority(t *testing.T) {
	tests := []struct {
		name     string
		input    int
		expected int
	}{
		{"priority 1 (high)", 1, 1},
		{"priority 2 (normal)", 2, 2},
		{"priority 3", 3, 3},
		{"priority 4 (low)", 4, 4},
		{"below minimum clamped to 1", 0, 1},
		{"negative clamped to 1", -5, 1},
		{"above maximum clamped to 4", 5, 4},
		{"way above maximum clamped to 4", 100, 4},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			opts := queue.ApplyOptions([]queue.EnqueueOption{queue.WithPriority(tc.input)})
			assert.Equal(t, tc.expected, opts.Priority)
		})
	}
}

// --- WithHighPriority / WithLowPriority ---

func TestWithHighPriority(t *testing.T) {
	opts := queue.ApplyOptions([]queue.EnqueueOption{queue.WithHighPriority()})
	assert.Equal(t, 1, opts.Priority)
}

func TestWithLowPriority(t *testing.T) {
	opts := queue.ApplyOptions([]queue.EnqueueOption{queue.WithLowPriority()})
	assert.Equal(t, 4, opts.Priority)
}

// --- WithScheduledAt ---

func TestWithScheduledAt(t *testing.T) {
	future := time.Now().Add(1 * time.Hour)
	opts := queue.ApplyOptions([]queue.EnqueueOption{queue.WithScheduledAt(future)})
	assert.Equal(t, future, opts.ScheduledAt)
}

// --- WithDelay ---

func TestWithDelay(t *testing.T) {
	delay := 5 * time.Minute
	before := time.Now()
	opts := queue.ApplyOptions([]queue.EnqueueOption{queue.WithDelay(delay)})
	after := time.Now()

	// ScheduledAt should be between now+delay-epsilon and now+delay+epsilon
	assert.True(t, opts.ScheduledAt.After(before.Add(delay-time.Second)),
		"ScheduledAt should be after now+delay")
	assert.True(t, opts.ScheduledAt.Before(after.Add(delay+time.Second)),
		"ScheduledAt should be before now+delay+1s")
}

// --- WithMaxAttempts ---

func TestWithMaxAttempts(t *testing.T) {
	tests := []struct {
		name     string
		input    int
		expected int
	}{
		{"normal attempts", 3, 3},
		{"one attempt (no retry)", 1, 1},
		{"max attempts 10", 10, 10},
		{"zero clamped to 1", 0, 1},
		{"negative clamped to 1", -1, 1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			opts := queue.ApplyOptions([]queue.EnqueueOption{queue.WithMaxAttempts(tc.input)})
			assert.Equal(t, tc.expected, opts.MaxAttempts)
		})
	}
}

// --- WithUnique ---

func TestWithUnique(t *testing.T) {
	unique := queue.UniqueOpts{
		ByArgs:   true,
		ByPeriod: 10 * time.Minute,
		ByQueue:  true,
	}
	opts := queue.ApplyOptions([]queue.EnqueueOption{queue.WithUnique(unique)})
	require.NotNil(t, opts.Unique)
	assert.True(t, opts.Unique.ByArgs)
	assert.Equal(t, 10*time.Minute, opts.Unique.ByPeriod)
	assert.True(t, opts.Unique.ByQueue)
}

// --- WithUniqueByArgs ---

func TestWithUniqueByArgs(t *testing.T) {
	opts := queue.ApplyOptions([]queue.EnqueueOption{queue.WithUniqueByArgs()})
	require.NotNil(t, opts.Unique)
	assert.True(t, opts.Unique.ByArgs)
}

func TestWithUniqueByArgs_PreservesExistingUnique(t *testing.T) {
	existing := queue.UniqueOpts{ByPeriod: 5 * time.Minute}
	opts := queue.ApplyOptions([]queue.EnqueueOption{
		queue.WithUnique(existing),
		queue.WithUniqueByArgs(),
	})
	require.NotNil(t, opts.Unique)
	assert.True(t, opts.Unique.ByArgs)
	assert.Equal(t, 5*time.Minute, opts.Unique.ByPeriod, "existing period should be preserved")
}

// --- WithUniqueByPeriod ---

func TestWithUniqueByPeriod(t *testing.T) {
	opts := queue.ApplyOptions([]queue.EnqueueOption{queue.WithUniqueByPeriod(15 * time.Minute)})
	require.NotNil(t, opts.Unique)
	assert.Equal(t, 15*time.Minute, opts.Unique.ByPeriod)
}

// --- WithUniqueByArgsAndPeriod ---

func TestWithUniqueByArgsAndPeriod(t *testing.T) {
	opts := queue.ApplyOptions([]queue.EnqueueOption{queue.WithUniqueByArgsAndPeriod(30 * time.Minute)})
	require.NotNil(t, opts.Unique)
	assert.True(t, opts.Unique.ByArgs)
	assert.Equal(t, 30*time.Minute, opts.Unique.ByPeriod)
}

// --- WithTags ---

func TestWithTags(t *testing.T) {
	opts := queue.ApplyOptions([]queue.EnqueueOption{queue.WithTags("tag1", "tag2", "tag3")})
	assert.Equal(t, []string{"tag1", "tag2", "tag3"}, opts.Tags)
}

func TestWithTags_MultipleCallsAppend(t *testing.T) {
	opts := queue.ApplyOptions([]queue.EnqueueOption{
		queue.WithTags("tag1"),
		queue.WithTags("tag2", "tag3"),
	})
	assert.Equal(t, []string{"tag1", "tag2", "tag3"}, opts.Tags)
}

// --- WithMetadata ---

func TestWithMetadata(t *testing.T) {
	opts := queue.ApplyOptions([]queue.EnqueueOption{
		queue.WithMetadata("key1", "value1"),
		queue.WithMetadata("key2", 42),
	})
	require.NotNil(t, opts.Metadata)
	assert.Equal(t, "value1", opts.Metadata["key1"])
	assert.Equal(t, 42, opts.Metadata["key2"])
}

// --- WithUserID ---

func TestWithUserID(t *testing.T) {
	opts := queue.ApplyOptions([]queue.EnqueueOption{queue.WithUserID("user-abc-123")})
	require.NotNil(t, opts.Metadata)
	assert.Equal(t, "user-abc-123", opts.Metadata["user_id"])
}

// --- Queue name constants ---

func TestQueueNameConstants(t *testing.T) {
	assert.Equal(t, "critical", queue.QueueCritical)
	assert.Equal(t, "default", queue.QueueDefault)
	assert.Equal(t, "notifications", queue.QueueNotifications)
	assert.Equal(t, "low", queue.QueueLow)
}
