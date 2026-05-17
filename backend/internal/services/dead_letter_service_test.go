package services

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// --- Mocks ---

type mockDLQRepo struct {
	mock.Mock
}

func (m *mockDLQRepo) GetByID(ctx context.Context, id int64) (*domain.DeadLetterJob, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.DeadLetterJob), args.Error(1)
}

func (m *mockDLQRepo) ListAll(ctx context.Context, filters domain.DeadLetterJobFilters, limit, offset int) ([]*domain.DeadLetterJob, int, error) {
	args := m.Called(ctx, filters, limit, offset)
	return args.Get(0).([]*domain.DeadLetterJob), args.Int(1), args.Error(2)
}

func (m *mockDLQRepo) AtomicMarkRetried(ctx context.Context, id, retryJobID int64) (bool, error) {
	args := m.Called(ctx, id, retryJobID)
	return args.Bool(0), args.Error(1)
}

func (m *mockDLQRepo) MarkRetried(ctx context.Context, id int64, retryJobID int64) error {
	args := m.Called(ctx, id, retryJobID)
	return args.Error(0)
}

func (m *mockDLQRepo) AddNotes(ctx context.Context, id int64, notes string) error {
	args := m.Called(ctx, id, notes)
	return args.Error(0)
}

func (m *mockDLQRepo) DeleteByID(ctx context.Context, id int64) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *mockDLQRepo) DeleteByFilters(ctx context.Context, filters domain.DeadLetterJobFilters) (int64, error) {
	args := m.Called(ctx, filters)
	return args.Get(0).(int64), args.Error(1)
}

func (m *mockDLQRepo) GetStats(ctx context.Context) (*repository.DeadLetterStats, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*repository.DeadLetterStats), args.Error(1)
}

func (m *mockDLQRepo) GetStatsByKind(ctx context.Context) (map[string]int64, error) {
	args := m.Called(ctx)
	return args.Get(0).(map[string]int64), args.Error(1)
}

func (m *mockDLQRepo) DeleteOlderThan(ctx context.Context, before time.Time, skipWithNotes bool) (int64, error) {
	args := m.Called(ctx, before, skipWithNotes)
	return args.Get(0).(int64), args.Error(1)
}

type mockJobQueue struct {
	mock.Mock
}

func (m *mockJobQueue) Enqueue(ctx context.Context, job queue.Job, opts ...queue.EnqueueOption) (*queue.JobResult, error) {
	args := m.Called(ctx, job)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*queue.JobResult), args.Error(1)
}

func (m *mockJobQueue) EnqueueTx(ctx context.Context, tx pgx.Tx, job queue.Job, opts ...queue.EnqueueOption) (*queue.JobResult, error) {
	return nil, nil
}

func (m *mockJobQueue) EnqueueMany(ctx context.Context, jobs []queue.Job, opts ...queue.EnqueueOption) ([]*queue.JobResult, error) {
	return nil, nil
}

func (m *mockJobQueue) EnqueueManyTx(ctx context.Context, tx pgx.Tx, jobs []queue.Job, opts ...queue.EnqueueOption) ([]*queue.JobResult, error) {
	return nil, nil
}

// makeDLQJob creates a test dead letter job.
func makeDLQJob(id int64, kind, q string, retriedAt *time.Time) *domain.DeadLetterJob {
	return &domain.DeadLetterJob{
		ID:          id,
		Kind:        kind,
		Queue:       q,
		Args:        json.RawMessage(`{"user_id":"u1"}`),
		Errors:      json.RawMessage(`[]`),
		MaxAttempts: 5,
		CreatedAt:   time.Now(),
		FailedAt:    time.Now(),
		RetriedAt:   retriedAt,
	}
}

// --- List ---

func TestDeadLetterService_List_Success(t *testing.T) {
	repo := &mockDLQRepo{}
	svc := NewDeadLetterService(repo, nil)

	ctx := context.Background()
	jobs := []*domain.DeadLetterJob{makeDLQJob(1, "plaid_sync", "default", nil)}
	filters := domain.DeadLetterJobFilters{}

	repo.On("ListAll", ctx, filters, 20, 0).Return(jobs, 1, nil)

	resp, err := svc.List(ctx, filters, 1, 20)
	require.NoError(t, err)
	assert.Equal(t, 1, resp.Total)
	assert.Len(t, resp.Jobs, 1)
	repo.AssertExpectations(t)
}

func TestDeadLetterService_List_RepoError(t *testing.T) {
	repo := &mockDLQRepo{}
	svc := NewDeadLetterService(repo, nil)

	ctx := context.Background()
	filters := domain.DeadLetterJobFilters{}

	repo.On("ListAll", ctx, filters, 20, 0).Return([]*domain.DeadLetterJob{}, 0, errors.New("db error"))

	_, err := svc.List(ctx, filters, 1, 20)
	require.Error(t, err)
	repo.AssertExpectations(t)
}

// --- GetByID ---

func TestDeadLetterService_GetByID_Success(t *testing.T) {
	repo := &mockDLQRepo{}
	svc := NewDeadLetterService(repo, nil)

	ctx := context.Background()
	job := makeDLQJob(1, "plaid_sync", "default", nil)

	repo.On("GetByID", ctx, int64(1)).Return(job, nil)

	result, err := svc.GetByID(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, int64(1), result.ID)
	repo.AssertExpectations(t)
}

func TestDeadLetterService_GetByID_NotFound(t *testing.T) {
	repo := &mockDLQRepo{}
	svc := NewDeadLetterService(repo, nil)

	ctx := context.Background()

	repo.On("GetByID", ctx, int64(99)).Return(nil, ErrDeadLetterJobNotFound)

	_, err := svc.GetByID(ctx, 99)
	require.ErrorIs(t, err, ErrDeadLetterJobNotFound)
	repo.AssertExpectations(t)
}

// --- Retry ---

func TestDeadLetterService_Retry_Success(t *testing.T) {
	repo := &mockDLQRepo{}
	jobQ := &mockJobQueue{}
	svc := NewDeadLetterService(repo, jobQ)

	ctx := context.Background()
	job := makeDLQJob(1, "plaid_sync", "default", nil)
	newJobID := int64(42)

	repo.On("GetByID", ctx, int64(1)).Return(job, nil)
	// Claim-first: AtomicMarkRetried with placeholder 0
	repo.On("AtomicMarkRetried", ctx, int64(1), int64(0)).Return(true, nil)
	// Enqueue returns real job ID
	jobQ.On("Enqueue", ctx, mock.AnythingOfType("queue.RawJobArgs")).
		Return(&queue.JobResult{ID: newJobID}, nil)
	// Update with real job ID (best-effort)
	repo.On("MarkRetried", ctx, int64(1), newJobID).Return(nil)

	resp, err := svc.Retry(ctx, 1)
	require.NoError(t, err)
	assert.True(t, resp.Success)
	assert.Equal(t, int64(1), resp.DeadLetterID)
	repo.AssertExpectations(t)
}

func TestDeadLetterService_Retry_AlreadyRetried(t *testing.T) {
	repo := &mockDLQRepo{}
	svc := NewDeadLetterService(repo, nil)

	ctx := context.Background()
	retriedAt := time.Now().Add(-1 * time.Hour)
	job := makeDLQJob(1, "plaid_sync", "default", &retriedAt)

	repo.On("GetByID", ctx, int64(1)).Return(job, nil)

	_, err := svc.Retry(ctx, 1)
	require.ErrorIs(t, err, ErrDeadLetterJobAlreadyRetried)
	repo.AssertExpectations(t)
}

func TestDeadLetterService_Retry_NotFound(t *testing.T) {
	repo := &mockDLQRepo{}
	svc := NewDeadLetterService(repo, nil)

	ctx := context.Background()
	repo.On("GetByID", ctx, int64(99)).Return(nil, ErrDeadLetterJobNotFound)

	_, err := svc.Retry(ctx, 99)
	require.ErrorIs(t, err, ErrDeadLetterJobNotFound)
}

// --- AddNotes ---

func TestDeadLetterService_AddNotes_Success(t *testing.T) {
	repo := &mockDLQRepo{}
	svc := NewDeadLetterService(repo, nil)

	ctx := context.Background()
	repo.On("AddNotes", ctx, int64(1), "investigating plaid timeout").Return(nil)

	err := svc.AddNotes(ctx, 1, "investigating plaid timeout")
	require.NoError(t, err)
	repo.AssertExpectations(t)
}

// --- Delete ---

func TestDeadLetterService_Delete_Success(t *testing.T) {
	repo := &mockDLQRepo{}
	svc := NewDeadLetterService(repo, nil)

	ctx := context.Background()
	repo.On("DeleteByID", ctx, int64(1)).Return(nil)

	err := svc.Delete(ctx, 1)
	require.NoError(t, err)
	repo.AssertExpectations(t)
}

// --- GetStats ---

func TestDeadLetterService_GetStats_Success(t *testing.T) {
	repo := &mockDLQRepo{}
	svc := NewDeadLetterService(repo, nil)

	ctx := context.Background()
	stats := &repository.DeadLetterStats{Total: 5, Unnotified: 3, Retried: 1, Last24Hours: 2}
	byKind := map[string]int64{"plaid_sync": 3, "rule_evaluation": 2}

	repo.On("GetStats", ctx).Return(stats, nil)
	repo.On("GetStatsByKind", ctx).Return(byKind, nil)

	resp, err := svc.GetStats(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(5), resp.Total)
	assert.Equal(t, int64(3), resp.Unnotified)
	assert.Equal(t, byKind, resp.ByKind)
}

// --- Cleanup ---

func TestDeadLetterService_Cleanup_Success(t *testing.T) {
	repo := &mockDLQRepo{}
	svc := NewDeadLetterService(repo, nil)

	ctx := context.Background()
	repo.On("DeleteOlderThan", ctx, mock.AnythingOfType("time.Time"), true).Return(int64(5), nil)

	count, err := svc.Cleanup(ctx, 30)
	require.NoError(t, err)
	assert.Equal(t, int64(5), count)
	repo.AssertExpectations(t)
}
