package river

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestClient returns a zero-value Client sufficient for calling pure helper
// methods that do not touch c.client or c.pool.
func newTestClient() *Client {
	return &Client{}
}

// --- toRiverInsertOpts ---

func TestToRiverInsertOpts_BasicQueueAndTags(t *testing.T) {
	c := newTestClient()
	opts := queue.EnqueueOptions{
		Queue: "myqueue",
		Tags:  []string{"a", "b"},
	}

	got := c.toRiverInsertOpts(opts)

	require.NotNil(t, got)
	assert.Equal(t, "myqueue", got.Queue)
	assert.Equal(t, []string{"a", "b"}, got.Tags)
}

func TestToRiverInsertOpts_NonZeroPriority(t *testing.T) {
	c := newTestClient()
	opts := queue.EnqueueOptions{
		Queue:    "default",
		Priority: 1,
	}

	got := c.toRiverInsertOpts(opts)

	assert.Equal(t, 1, got.Priority)
}

func TestToRiverInsertOpts_ZeroPriority_NotSet(t *testing.T) {
	c := newTestClient()
	opts := queue.EnqueueOptions{
		Queue:    "default",
		Priority: 0,
	}

	got := c.toRiverInsertOpts(opts)

	// Zero priority should remain zero (not overridden)
	assert.Equal(t, 0, got.Priority)
}

func TestToRiverInsertOpts_ScheduledAt_NonZero(t *testing.T) {
	c := newTestClient()
	scheduled := time.Now().Add(10 * time.Minute)
	opts := queue.EnqueueOptions{
		Queue:       "default",
		ScheduledAt: scheduled,
	}

	got := c.toRiverInsertOpts(opts)

	assert.Equal(t, scheduled, got.ScheduledAt)
}

func TestToRiverInsertOpts_ScheduledAt_Zero_NotSet(t *testing.T) {
	c := newTestClient()
	opts := queue.EnqueueOptions{
		Queue: "default",
	}

	got := c.toRiverInsertOpts(opts)

	assert.True(t, got.ScheduledAt.IsZero())
}

func TestToRiverInsertOpts_Metadata_NonNil(t *testing.T) {
	c := newTestClient()
	opts := queue.EnqueueOptions{
		Queue:    "default",
		Metadata: map[string]any{"user_id": "u1"},
	}

	got := c.toRiverInsertOpts(opts)

	require.NotNil(t, got.Metadata)
	var parsed map[string]any
	require.NoError(t, json.Unmarshal(got.Metadata, &parsed))
	assert.Equal(t, "u1", parsed["user_id"])
}

func TestToRiverInsertOpts_Metadata_Nil(t *testing.T) {
	c := newTestClient()
	opts := queue.EnqueueOptions{
		Queue:    "default",
		Metadata: nil,
	}

	got := c.toRiverInsertOpts(opts)

	assert.Nil(t, got.Metadata)
}

func TestToRiverInsertOpts_Unique_NonNil_SetsUniqueOpts(t *testing.T) {
	c := newTestClient()
	opts := queue.EnqueueOptions{
		Queue: "default",
		Unique: &queue.UniqueOpts{
			ByPeriod: 5 * time.Minute,
		},
	}

	got := c.toRiverInsertOpts(opts)

	assert.Equal(t, 5*time.Minute, got.UniqueOpts.ByPeriod)
}

func TestToRiverInsertOpts_Unique_Nil_NoUniqueOpts(t *testing.T) {
	c := newTestClient()
	opts := queue.EnqueueOptions{
		Queue:  "default",
		Unique: nil,
	}

	got := c.toRiverInsertOpts(opts)

	// UniqueOpts should be zero value (ByPeriod==0, ByArgs==false, etc.)
	assert.Equal(t, time.Duration(0), got.UniqueOpts.ByPeriod)
	assert.False(t, got.UniqueOpts.ByArgs)
}

// --- toRiverUniqueOpts ---

func TestToRiverUniqueOpts_ByPeriodOnly(t *testing.T) {
	c := newTestClient()
	unique := &queue.UniqueOpts{
		ByPeriod: 10 * time.Minute,
	}

	got := c.toRiverUniqueOpts(unique)

	assert.Equal(t, 10*time.Minute, got.ByPeriod)
	assert.False(t, got.ByArgs)
	assert.False(t, got.ByQueue)
	assert.Empty(t, got.ByState)
}

func TestToRiverUniqueOpts_ByArgs_True(t *testing.T) {
	c := newTestClient()
	unique := &queue.UniqueOpts{
		ByArgs: true,
	}

	got := c.toRiverUniqueOpts(unique)

	assert.True(t, got.ByArgs)
}

func TestToRiverUniqueOpts_ByQueue_True(t *testing.T) {
	c := newTestClient()
	unique := &queue.UniqueOpts{
		ByQueue: true,
	}

	got := c.toRiverUniqueOpts(unique)

	assert.True(t, got.ByQueue)
}

func TestToRiverUniqueOpts_ByState_Mapped(t *testing.T) {
	c := newTestClient()
	unique := &queue.UniqueOpts{
		ByState: []queue.JobState{
			queue.JobStateAvailable,
			queue.JobStateRunning,
		},
	}

	got := c.toRiverUniqueOpts(unique)

	require.Len(t, got.ByState, 2)
	assert.Equal(t, rivertype.JobState("available"), got.ByState[0])
	assert.Equal(t, rivertype.JobState("running"), got.ByState[1])
}

func TestToRiverUniqueOpts_ByState_Empty(t *testing.T) {
	c := newTestClient()
	unique := &queue.UniqueOpts{}

	got := c.toRiverUniqueOpts(unique)

	assert.Empty(t, got.ByState)
}

// --- toJobResult ---

func TestToJobResult_MapsAllFields(t *testing.T) {
	c := newTestClient()
	scheduled := time.Now().Add(5 * time.Minute).UTC().Truncate(time.Second)

	result := &rivertype.JobInsertResult{
		Job: &rivertype.JobRow{
			ID:          42,
			Kind:        "my_job",
			Queue:       "default",
			State:       rivertype.JobStateAvailable,
			ScheduledAt: scheduled,
		},
		UniqueSkippedAsDuplicate: true,
	}

	got := c.toJobResult(result)

	require.NotNil(t, got)
	assert.Equal(t, int64(42), got.ID)
	assert.Equal(t, "my_job", got.Kind)
	assert.Equal(t, "default", got.Queue)
	assert.Equal(t, queue.JobState("available"), got.State)
	assert.Equal(t, scheduled, got.ScheduledAt)
	assert.True(t, got.UniqueSkipped)
}

func TestToJobResult_UniqueSkippedFalse(t *testing.T) {
	c := newTestClient()
	result := &rivertype.JobInsertResult{
		Job: &rivertype.JobRow{
			ID:    1,
			Kind:  "test",
			Queue: "default",
			State: rivertype.JobStateAvailable,
		},
		UniqueSkippedAsDuplicate: false,
	}

	got := c.toJobResult(result)

	assert.False(t, got.UniqueSkipped)
}

// --- NewCustomRetryPolicy ---

func TestNewCustomRetryPolicy_NilPolicy_UsesDefault(t *testing.T) {
	p := NewCustomRetryPolicy(nil)

	require.NotNil(t, p)
	require.NotNil(t, p.policy)
	// Should use DefaultRetryPolicy values
	assert.Equal(t, DefaultRetryPolicy().MaxAttempts, p.policy.MaxAttempts)
	assert.Equal(t, DefaultRetryPolicy().InitialBackoff, p.policy.InitialBackoff)
}

func TestNewCustomRetryPolicy_NonNilPolicy_UsesProvided(t *testing.T) {
	custom := &RetryPolicy{
		MaxAttempts:       3,
		BackoffMultiplier: 2.0,
		InitialBackoff:    10 * time.Second,
		MaxBackoff:        1 * time.Minute,
	}

	p := NewCustomRetryPolicy(custom)

	require.NotNil(t, p)
	assert.Equal(t, 3, p.policy.MaxAttempts)
	assert.Equal(t, 10*time.Second, p.policy.InitialBackoff)
}

// --- (*CustomRetryPolicy).NextRetry ---

func TestCustomRetryPolicy_NextRetry_ReturnsTimeAfterNow(t *testing.T) {
	p := NewCustomRetryPolicy(nil)
	job := &rivertype.JobRow{
		ID:      1,
		Attempt: 1,
		Queue:   "default",
		Kind:    "test",
	}

	before := time.Now()
	result := p.NextRetry(job)

	assert.True(t, result.After(before),
		"NextRetry should return a time after now; got %v, before=%v", result, before)
}

func TestCustomRetryPolicy_NextRetry_HigherAttemptGivesLaterTime(t *testing.T) {
	p := NewCustomRetryPolicy(nil)
	job1 := &rivertype.JobRow{ID: 1, Attempt: 1, Queue: "default", Kind: "test"}
	job3 := &rivertype.JobRow{ID: 2, Attempt: 3, Queue: "default", Kind: "test"}

	t1 := p.NextRetry(job1)
	t3 := p.NextRetry(job3)

	assert.True(t, t3.After(t1),
		"attempt=3 should yield a later retry time than attempt=1")
}
