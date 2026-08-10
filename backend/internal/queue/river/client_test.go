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

func TestToRiverInsertOpts_BasicQueueAndTags(t *testing.T) {
	got := newTestClient().toRiverInsertOpts(queue.EnqueueOptions{
		Queue: "myqueue",
		Tags:  []string{"a", "b"},
	})

	require.NotNil(t, got)
	assert.Equal(t, "myqueue", got.Queue)
	assert.Equal(t, []string{"a", "b"}, got.Tags)
}

func TestToRiverInsertOpts_NonZeroPriority(t *testing.T) {
	got := newTestClient().toRiverInsertOpts(queue.EnqueueOptions{
		Queue:    "default",
		Priority: 1,
	})

	assert.Equal(t, 1, got.Priority)
}

func TestToRiverInsertOpts_ZeroPriority_NotSet(t *testing.T) {
	got := newTestClient().toRiverInsertOpts(queue.EnqueueOptions{
		Queue:    "default",
		Priority: 0,
	})

	assert.Equal(t, 0, got.Priority)
}

func TestToRiverInsertOpts_ScheduledAt_NonZero(t *testing.T) {
	scheduled := time.Now().Add(10 * time.Minute)
	got := newTestClient().toRiverInsertOpts(queue.EnqueueOptions{
		Queue:       "default",
		ScheduledAt: scheduled,
	})

	assert.Equal(t, scheduled, got.ScheduledAt)
}

func TestToRiverInsertOpts_ScheduledAt_Zero_NotSet(t *testing.T) {
	got := newTestClient().toRiverInsertOpts(queue.EnqueueOptions{
		Queue: "default",
	})

	assert.True(t, got.ScheduledAt.IsZero())
}

func TestToRiverInsertOpts_Metadata_NonNil(t *testing.T) {
	got := newTestClient().toRiverInsertOpts(queue.EnqueueOptions{
		Queue:    "default",
		Metadata: map[string]any{"user_id": "u1"},
	})

	require.NotNil(t, got.Metadata)
	var parsed map[string]any
	require.NoError(t, json.Unmarshal(got.Metadata, &parsed))
	assert.Equal(t, "u1", parsed["user_id"])
}

func TestToRiverInsertOpts_Metadata_Nil(t *testing.T) {
	got := newTestClient().toRiverInsertOpts(queue.EnqueueOptions{
		Queue:    "default",
		Metadata: nil,
	})

	assert.Nil(t, got.Metadata)
}

func TestToRiverInsertOpts_Unique_NonNil_SetsUniqueOpts(t *testing.T) {
	got := newTestClient().toRiverInsertOpts(queue.EnqueueOptions{
		Queue: "default",
		Unique: &queue.UniqueOpts{
			ByPeriod: 5 * time.Minute,
		},
	})

	assert.Equal(t, 5*time.Minute, got.UniqueOpts.ByPeriod)
}

func TestToRiverInsertOpts_Unique_Nil_NoUniqueOpts(t *testing.T) {
	got := newTestClient().toRiverInsertOpts(queue.EnqueueOptions{
		Queue:  "default",
		Unique: nil,
	})

	assert.Equal(t, time.Duration(0), got.UniqueOpts.ByPeriod)
	assert.False(t, got.UniqueOpts.ByArgs)
}

func TestToRiverUniqueOpts_ByPeriodOnly(t *testing.T) {
	got := newTestClient().toRiverUniqueOpts(&queue.UniqueOpts{
		ByPeriod: 10 * time.Minute,
	})

	assert.Equal(t, 10*time.Minute, got.ByPeriod)
	assert.False(t, got.ByArgs)
	assert.False(t, got.ByQueue)
	assert.Empty(t, got.ByState)
}

func TestToRiverUniqueOpts_ByArgs_True(t *testing.T) {
	got := newTestClient().toRiverUniqueOpts(&queue.UniqueOpts{
		ByArgs: true,
	})

	assert.True(t, got.ByArgs)
}

func TestToRiverUniqueOpts_ByQueue_True(t *testing.T) {
	got := newTestClient().toRiverUniqueOpts(&queue.UniqueOpts{
		ByQueue: true,
	})

	assert.True(t, got.ByQueue)
}

func TestToRiverUniqueOpts_ByState_Mapped(t *testing.T) {
	got := newTestClient().toRiverUniqueOpts(&queue.UniqueOpts{
		ByState: []queue.JobState{
			queue.JobStateAvailable,
			queue.JobStateRunning,
		},
	})

	require.Len(t, got.ByState, 2)
	assert.Equal(t, rivertype.JobState("available"), got.ByState[0])
	assert.Equal(t, rivertype.JobState("running"), got.ByState[1])
}

func TestToRiverUniqueOpts_ByState_Empty(t *testing.T) {
	got := newTestClient().toRiverUniqueOpts(&queue.UniqueOpts{})

	assert.Empty(t, got.ByState)
}

func TestToJobResult_MapsAllFields(t *testing.T) {
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

	got := newTestClient().toJobResult(result)

	require.NotNil(t, got)
	assert.Equal(t, int64(42), got.ID)
	assert.Equal(t, "my_job", got.Kind)
	assert.Equal(t, "default", got.Queue)
	assert.Equal(t, queue.JobState("available"), got.State)
	assert.Equal(t, scheduled, got.ScheduledAt)
	assert.True(t, got.UniqueSkipped)
}

func TestToJobResult_UniqueSkippedFalse(t *testing.T) {
	result := &rivertype.JobInsertResult{
		Job: &rivertype.JobRow{
			ID:    1,
			Kind:  "test",
			Queue: "default",
			State: rivertype.JobStateAvailable,
		},
		UniqueSkippedAsDuplicate: false,
	}

	got := newTestClient().toJobResult(result)

	assert.False(t, got.UniqueSkipped)
}
