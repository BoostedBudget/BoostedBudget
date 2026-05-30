package jobs

import (
	"context"
	"errors"
	"testing"

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
