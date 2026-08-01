// Package jobs contains River job definitions and workers for async processing.
// Issue #898: Past-due check job tests
package jobs

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// MockPastDueCheckProfileRepository mocks PastDueCheckProfileRepository for tests.
type MockPastDueCheckProfileRepository struct {
	mock.Mock
}

func (m *MockPastDueCheckProfileRepository) GetUsersInPastDue(ctx context.Context) ([]domain.User, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.User), args.Error(1)
}

// MockPastDueCheckService mocks PastDueCheckService for tests.
type MockPastDueCheckService struct {
	mock.Mock
}

func (m *MockPastDueCheckService) ProcessPastDueUser(ctx context.Context, user domain.User, now time.Time) error {
	args := m.Called(ctx, user, now)
	return args.Error(0)
}

func newTestPastDueCheckJob(args PastDueCheckArgs) *river.Job[PastDueCheckArgs] {
	return &river.Job[PastDueCheckArgs]{
		JobRow: &rivertype.JobRow{
			ID:      1,
			Attempt: 1,
		},
		Args: args,
	}
}

func TestPastDueCheckArgs_Kind(t *testing.T) {
	assert.Equal(t, "past_due_check", PastDueCheckArgs{}.Kind())
}

func TestPastDueCheckWorker_Work_NoUsers(t *testing.T) {
	ctx := context.Background()

	mockRepo := new(MockPastDueCheckProfileRepository)
	mockSvc := new(MockPastDueCheckService)

	mockRepo.On("GetUsersInPastDue", ctx).Return([]domain.User{}, nil)

	worker := NewPastDueCheckWorker(PastDueCheckWorkerDeps{
		ProfileRepo:    mockRepo,
		PastDueService: mockSvc,
		Logger:         slog.Default(),
	})

	err := worker.Work(ctx, newTestPastDueCheckJob(PastDueCheckArgs{TriggerSource: "periodic"}))

	require.NoError(t, err)
	mockRepo.AssertExpectations(t)
	mockSvc.AssertNotCalled(t, "ProcessPastDueUser")
}

func TestPastDueCheckWorker_Work_ProcessesEachUser(t *testing.T) {
	ctx := context.Background()

	mockRepo := new(MockPastDueCheckProfileRepository)
	mockSvc := new(MockPastDueCheckService)

	stamp := time.Now().Add(-22 * 24 * time.Hour)
	users := []domain.User{
		{ID: "user-1", SubscriptionStatus: "past_due", PastDueStartedAt: &stamp},
		{ID: "user-2", SubscriptionStatus: "past_due", PastDueStartedAt: &stamp},
	}

	mockRepo.On("GetUsersInPastDue", ctx).Return(users, nil)
	mockSvc.On("ProcessPastDueUser", ctx, users[0], mock.AnythingOfType("time.Time")).Return(nil)
	mockSvc.On("ProcessPastDueUser", ctx, users[1], mock.AnythingOfType("time.Time")).Return(nil)

	worker := NewPastDueCheckWorker(PastDueCheckWorkerDeps{
		ProfileRepo:    mockRepo,
		PastDueService: mockSvc,
		Logger:         slog.Default(),
	})

	err := worker.Work(ctx, newTestPastDueCheckJob(PastDueCheckArgs{TriggerSource: "periodic"}))

	require.NoError(t, err)
	mockRepo.AssertExpectations(t)
	mockSvc.AssertNumberOfCalls(t, "ProcessPastDueUser", 2)
}

func TestPastDueCheckWorker_Work_RepositoryError(t *testing.T) {
	ctx := context.Background()

	mockRepo := new(MockPastDueCheckProfileRepository)
	mockSvc := new(MockPastDueCheckService)

	mockRepo.On("GetUsersInPastDue", ctx).Return(nil, errors.New("db error"))

	worker := NewPastDueCheckWorker(PastDueCheckWorkerDeps{
		ProfileRepo:    mockRepo,
		PastDueService: mockSvc,
		Logger:         slog.Default(),
	})

	err := worker.Work(ctx, newTestPastDueCheckJob(PastDueCheckArgs{TriggerSource: "periodic"}))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to fetch past_due users")
	mockSvc.AssertNotCalled(t, "ProcessPastDueUser")
}

func TestPastDueCheckWorker_Work_ProcessErrorContinues(t *testing.T) {
	ctx := context.Background()

	mockRepo := new(MockPastDueCheckProfileRepository)
	mockSvc := new(MockPastDueCheckService)

	stamp := time.Now().Add(-22 * 24 * time.Hour)
	users := []domain.User{
		{ID: "user-1", SubscriptionStatus: "past_due", PastDueStartedAt: &stamp},
		{ID: "user-2", SubscriptionStatus: "past_due", PastDueStartedAt: &stamp},
	}

	mockRepo.On("GetUsersInPastDue", ctx).Return(users, nil)
	mockSvc.On("ProcessPastDueUser", ctx, users[0], mock.AnythingOfType("time.Time")).Return(errors.New("plaid error"))
	mockSvc.On("ProcessPastDueUser", ctx, users[1], mock.AnythingOfType("time.Time")).Return(nil)

	worker := NewPastDueCheckWorker(PastDueCheckWorkerDeps{
		ProfileRepo:    mockRepo,
		PastDueService: mockSvc,
		Logger:         slog.Default(),
	})

	err := worker.Work(ctx, newTestPastDueCheckJob(PastDueCheckArgs{TriggerSource: "periodic"}))

	require.NoError(t, err)
	mockSvc.AssertNumberOfCalls(t, "ProcessPastDueUser", 2)
}

func TestPastDueCheckUniqueOpts(t *testing.T) {
	opts := PastDueCheckUniqueOpts()

	assert.False(t, opts.ByArgs)
	assert.Equal(t, 1*time.Hour, opts.ByPeriod)
	assert.Contains(t, opts.ByState, queue.JobStateAvailable)
	assert.Contains(t, opts.ByState, queue.JobStateScheduled)
	assert.Contains(t, opts.ByState, queue.JobStateRunning)
	assert.Contains(t, opts.ByState, queue.JobStateRetryable)
	assert.Contains(t, opts.ByState, queue.JobStatePending)
}

// ─── Issue #1448: this job shipped RunOnStart: true with NO dedup guarantee at all. ──────

func TestPastDueCheckInsertOpts_Queue(t *testing.T) {
	opts := PastDueCheckInsertOpts()
	require.NotNil(t, opts)
	assert.Equal(t, "low", opts.Queue)
}

func TestPastDueCheckInsertOpts_CarriesUniqueOpts(t *testing.T) {
	opts := PastDueCheckInsertOpts()
	want := PastDueCheckUniqueOpts()

	assert.Equal(t, want.ByArgs, opts.UniqueOpts.ByArgs)
	assert.Equal(t, want.ByPeriod, opts.UniqueOpts.ByPeriod)
	require.NotEmpty(t, opts.UniqueOpts.ByState,
		"main.go's periodic registration previously carried NO UniqueOpts at all for this "+
			"job despite RunOnStart: true (Issue #1448)")
}

func TestPastDueCheckPeriodicOpts_RunOnStart(t *testing.T) {
	opts := PastDueCheckPeriodicOpts()
	require.NotNil(t, opts)
	assert.True(t, opts.RunOnStart)
}

func TestPastDueCheckPeriodicOpts_ID(t *testing.T) {
	opts := PastDueCheckPeriodicOpts()
	assert.Equal(t, "past_due_check", opts.ID)
}

func TestPastDueCheckPeriodicOpts_PairedWithUniqueOpts(t *testing.T) {
	opts := PastDueCheckPeriodicOpts()
	require.True(t, opts.RunOnStart)

	unique := PastDueCheckUniqueOpts()
	require.NotEmpty(t, unique.ByState)
}
