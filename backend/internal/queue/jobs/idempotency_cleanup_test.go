package jobs

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestIdempotencyCleanupJob(args IdempotencyCleanupArgs) *river.Job[IdempotencyCleanupArgs] {
	return &river.Job[IdempotencyCleanupArgs]{
		JobRow: &rivertype.JobRow{ID: 1, Attempt: 1},
		Args:   args,
	}
}

type mockIdempotencyCleanupRepo struct {
	deleted int64
	err     error
}

func (m *mockIdempotencyCleanupRepo) DeleteExpired(_ context.Context) (int64, error) {
	return m.deleted, m.err
}

func TestIdempotencyCleanupWorker_Work(t *testing.T) {
	ctx := context.Background()

	t.Run("R4-7: deletes expired keys and returns nil", func(t *testing.T) {
		repo := &mockIdempotencyCleanupRepo{deleted: 7}
		worker := NewIdempotencyCleanupWorker(IdempotencyCleanupWorkerDeps{Repo: repo})

		job := newTestIdempotencyCleanupJob(IdempotencyCleanupArgs{TriggerSource: "periodic"})
		err := worker.Work(ctx, job)
		require.NoError(t, err)
	})

	t.Run("R4-7: propagates repository error so River retries", func(t *testing.T) {
		repo := &mockIdempotencyCleanupRepo{err: errors.New("db error")}
		worker := NewIdempotencyCleanupWorker(IdempotencyCleanupWorkerDeps{Repo: repo})

		job := newTestIdempotencyCleanupJob(IdempotencyCleanupArgs{TriggerSource: "periodic"})
		err := worker.Work(ctx, job)
		assert.ErrorContains(t, err, "db error")
	})

	t.Run("R4-7: Kind() returns idempotency_cleanup", func(t *testing.T) {
		assert.Equal(t, "idempotency_cleanup", IdempotencyCleanupArgs{}.Kind())
	})
}

// ─── Issue #1448: IdempotencyCleanupUniqueOpts did not exist at all, and main.go's periodic
// registration had no dedup and RunOnStart: false. ────────────────────────────────────────

func TestIdempotencyCleanupUniqueOpts(t *testing.T) {
	opts := IdempotencyCleanupUniqueOpts()

	assert.False(t, opts.ByArgs)
	assert.Equal(t, 23*time.Hour, opts.ByPeriod)
	assert.Contains(t, opts.ByState, queue.JobStateAvailable)
	assert.Contains(t, opts.ByState, queue.JobStateScheduled)
	assert.Contains(t, opts.ByState, queue.JobStateRunning)
	assert.Contains(t, opts.ByState, queue.JobStateRetryable)
	assert.Contains(t, opts.ByState, queue.JobStatePending)
}

func TestIdempotencyCleanupInsertOpts_Queue(t *testing.T) {
	opts := IdempotencyCleanupInsertOpts()
	require.NotNil(t, opts)
	assert.Equal(t, "low", opts.Queue)
}

func TestIdempotencyCleanupInsertOpts_CarriesUniqueOpts(t *testing.T) {
	opts := IdempotencyCleanupInsertOpts()
	want := IdempotencyCleanupUniqueOpts()

	assert.Equal(t, want.ByArgs, opts.UniqueOpts.ByArgs)
	assert.Equal(t, want.ByPeriod, opts.UniqueOpts.ByPeriod)
	assert.False(t, opts.UniqueOpts.ByQueue, "uniqueness must stay global across queues")

	require.Len(t, opts.UniqueOpts.ByState, len(want.ByState),
		"ByState must match IdempotencyCleanupUniqueOpts() exactly, not just be non-empty")
	for i, s := range want.ByState {
		assert.Equal(t, rivertype.JobState(s), opts.UniqueOpts.ByState[i])
	}
}

func TestIdempotencyCleanupPeriodicOpts_RunOnStart(t *testing.T) {
	opts := IdempotencyCleanupPeriodicOpts()
	require.NotNil(t, opts)
	assert.True(t, opts.RunOnStart)
}

func TestIdempotencyCleanupPeriodicOpts_ID(t *testing.T) {
	opts := IdempotencyCleanupPeriodicOpts()
	assert.Equal(t, "idempotency_cleanup", opts.ID)
}

func TestIdempotencyCleanupPeriodicOpts_PairedWithUniqueOpts(t *testing.T) {
	opts := IdempotencyCleanupPeriodicOpts()
	require.True(t, opts.RunOnStart)

	unique := IdempotencyCleanupUniqueOpts()
	require.NotEmpty(t, unique.ByState)
}
