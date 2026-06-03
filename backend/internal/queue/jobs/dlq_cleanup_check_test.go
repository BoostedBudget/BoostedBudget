package jobs

import (
	"context"
	"errors"
	"testing"

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
