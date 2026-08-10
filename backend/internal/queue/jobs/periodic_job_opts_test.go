package jobs

import (
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRiverUniqueOpts_ConvertsAllFields is the RED test for the shared conversion helper
// every periodic job's *InsertOpts() function (Issue #1448) relies on: a bug here would
// silently corrupt dedup for every job that uses it, not just one.
func TestRiverUniqueOpts_ConvertsAllFields(t *testing.T) {
	got := riverUniqueOpts(&queue.UniqueOpts{
		ByArgs:   true,
		ByPeriod: 23 * time.Hour,
		ByQueue:  true,
		ByState:  []queue.JobState{queue.JobStateAvailable, queue.JobStateCompleted},
	})

	assert.True(t, got.ByArgs)
	assert.Equal(t, 23*time.Hour, got.ByPeriod)
	assert.True(t, got.ByQueue)
	require.Len(t, got.ByState, 2)
	assert.Equal(t, rivertype.JobStateAvailable, got.ByState[0])
	assert.Equal(t, rivertype.JobStateCompleted, got.ByState[1])
}

// TestRiverUniqueOpts_EmptyByState proves an unset (nil) source ByState converts to a NIL
// river ByState, not a non-nil empty slice. This distinction is load-bearing: river's own
// UniqueOpts.isEmpty() (insert_opts.go) checks `ByState == nil` specifically when a
// completely zero-valued UniqueOpts{} should fall back to a job's own default InsertOpts —
// make([]rivertype.JobState, 0) would produce a non-nil empty slice that breaks that
// fallback. None of the 14 periodic jobs hit this today (every one sets a non-zero ByPeriod
// or non-empty ByState), but the helper itself must still get this right for future callers.
func TestRiverUniqueOpts_EmptyByState(t *testing.T) {
	got := riverUniqueOpts(&queue.UniqueOpts{})
	assert.Nil(t, got.ByState, "an unset source ByState must convert to nil, not a non-nil empty slice")
	assert.False(t, got.ByArgs)
	assert.False(t, got.ByQueue)
	assert.Zero(t, got.ByPeriod)
}

// TestRiverInsertOptsFromUnique_CarriesQueueAndUniqueOpts proves the InsertOpts builder
// carries both the queue name and the converted UniqueOpts through untouched.
func TestRiverInsertOptsFromUnique_CarriesQueueAndUniqueOpts(t *testing.T) {
	unique := &queue.UniqueOpts{
		ByPeriod: 1 * time.Hour,
		ByState:  []queue.JobState{queue.JobStateAvailable},
	}
	opts := riverInsertOptsFromUnique("low", unique)

	require.NotNil(t, opts)
	assert.Equal(t, "low", opts.Queue)
	assert.Equal(t, 1*time.Hour, opts.UniqueOpts.ByPeriod)
	require.Len(t, opts.UniqueOpts.ByState, 1)
	assert.Equal(t, rivertype.JobStateAvailable, opts.UniqueOpts.ByState[0])
}
