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
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func newTestDLQCleanupJob(args DLQCleanupCheckArgs) *river.Job[DLQCleanupCheckArgs] {
	return &river.Job[DLQCleanupCheckArgs]{
		JobRow: &rivertype.JobRow{
			ID:      1,
			Attempt: 1,
		},
		Args: args,
	}
}

// mockDLQCleanupRepo mocks DLQCleanupRepository for worker tests.
type mockDLQCleanupRepo struct {
	mock.Mock
}

func (m *mockDLQCleanupRepo) CleanupOldJobs(ctx context.Context, retentionDays int) (int64, error) {
	args := m.Called(ctx, retentionDays)
	return args.Get(0).(int64), args.Error(1)
}

func TestDLQCleanupCheckArgs_Kind(t *testing.T) {
	args := DLQCleanupCheckArgs{}
	assert.Equal(t, "dlq_cleanup_check", args.Kind())
}

func TestDLQCleanupCheckWorker_Work_Success(t *testing.T) {
	repo := &mockDLQCleanupRepo{}
	worker := NewDLQCleanupCheckWorker(DLQCleanupCheckWorkerDeps{
		Repo:          repo,
		RetentionDays: 30,
	})

	ctx := context.Background()
	repo.On("CleanupOldJobs", ctx, 30).Return(int64(5), nil)

	job := newTestDLQCleanupJob(DLQCleanupCheckArgs{TriggerSource: "periodic"})
	err := worker.Work(ctx, job)

	require.NoError(t, err)
	repo.AssertExpectations(t)
}

func TestDLQCleanupCheckWorker_Work_NoJobs(t *testing.T) {
	repo := &mockDLQCleanupRepo{}
	worker := NewDLQCleanupCheckWorker(DLQCleanupCheckWorkerDeps{
		Repo:          repo,
		RetentionDays: 30,
	})

	ctx := context.Background()
	repo.On("CleanupOldJobs", ctx, 30).Return(int64(0), nil)

	job := newTestDLQCleanupJob(DLQCleanupCheckArgs{})
	err := worker.Work(ctx, job)

	require.NoError(t, err)
	repo.AssertExpectations(t)
}

func TestDLQCleanupCheckWorker_Work_RepoError(t *testing.T) {
	repo := &mockDLQCleanupRepo{}
	worker := NewDLQCleanupCheckWorker(DLQCleanupCheckWorkerDeps{
		Repo:          repo,
		RetentionDays: 30,
	})

	ctx := context.Background()
	repo.On("CleanupOldJobs", ctx, 30).Return(int64(0), errors.New("db error"))

	job := newTestDLQCleanupJob(DLQCleanupCheckArgs{})
	err := worker.Work(ctx, job)

	require.Error(t, err)
	repo.AssertExpectations(t)
}

func TestDLQCleanupCheckWorker_DefaultRetentionDays(t *testing.T) {
	repo := &mockDLQCleanupRepo{}
	// RetentionDays = 0 → should default to 30
	worker := NewDLQCleanupCheckWorker(DLQCleanupCheckWorkerDeps{
		Repo:          repo,
		RetentionDays: 0,
	})

	ctx := context.Background()
	repo.On("CleanupOldJobs", ctx, 30).Return(int64(0), nil)

	job := newTestDLQCleanupJob(DLQCleanupCheckArgs{})
	err := worker.Work(ctx, job)

	require.NoError(t, err)
	repo.AssertExpectations(t)
}

func TestDLQCleanupUniqueOpts(t *testing.T) {
	opts := DLQCleanupUniqueOpts()

	assert.False(t, opts.ByArgs)
	assert.Equal(t, 23*time.Hour, opts.ByPeriod)
	assert.Contains(t, opts.ByState, queue.JobStateAvailable)
	assert.Contains(t, opts.ByState, queue.JobStateScheduled)
	assert.Contains(t, opts.ByState, queue.JobStateRunning)
	assert.Contains(t, opts.ByState, queue.JobStateRetryable)
	assert.Contains(t, opts.ByState, queue.JobStatePending)
}

// ─── Issue #1448: DLQCleanupUniqueOpts existed but was never wired. ──────────────────────

func TestDLQCleanupInsertOpts_Queue(t *testing.T) {
	opts := DLQCleanupInsertOpts()
	require.NotNil(t, opts)
	assert.Equal(t, "low", opts.Queue)
}

func TestDLQCleanupInsertOpts_CarriesUniqueOpts(t *testing.T) {
	opts := DLQCleanupInsertOpts()
	want := DLQCleanupUniqueOpts()

	assert.Equal(t, want.ByArgs, opts.UniqueOpts.ByArgs)
	assert.Equal(t, want.ByPeriod, opts.UniqueOpts.ByPeriod)
	assert.False(t, opts.UniqueOpts.ByQueue, "uniqueness must stay global across queues")

	require.Len(t, opts.UniqueOpts.ByState, len(want.ByState),
		"main.go's periodic registration must carry DLQCleanupUniqueOpts' dedup (Issue #1448) "+
			"— ByState must match exactly, not just be non-empty")
	for i, s := range want.ByState {
		assert.Equal(t, rivertype.JobState(s), opts.UniqueOpts.ByState[i])
	}
}

func TestDLQCleanupPeriodicOpts_RunOnStart(t *testing.T) {
	opts := DLQCleanupPeriodicOpts()
	require.NotNil(t, opts)
	assert.True(t, opts.RunOnStart)
}

func TestDLQCleanupPeriodicOpts_ID(t *testing.T) {
	opts := DLQCleanupPeriodicOpts()
	assert.Equal(t, "dlq_cleanup_check", opts.ID)
}

func TestDLQCleanupPeriodicOpts_PairedWithUniqueOpts(t *testing.T) {
	opts := DLQCleanupPeriodicOpts()
	require.True(t, opts.RunOnStart)

	unique := DLQCleanupUniqueOpts()
	require.NotEmpty(t, unique.ByState)
}
