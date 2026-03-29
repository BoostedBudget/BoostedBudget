package jobs

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	phclient "github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// MockDeletionService mocks the DeletionService interface
type MockDeletionService struct {
	mock.Mock
}

func (m *MockDeletionService) RequestDeletion(ctx context.Context, userID, email string) (string, time.Time, error) {
	args := m.Called(ctx, userID, email)
	return args.String(0), args.Get(1).(time.Time), args.Error(2)
}

func (m *MockDeletionService) CancelDeletion(ctx context.Context, token string) error {
	args := m.Called(ctx, token)
	return args.Error(0)
}

func (m *MockDeletionService) ExecuteDeletion(ctx context.Context, userID string) error {
	args := m.Called(ctx, userID)
	return args.Error(0)
}

func (m *MockDeletionService) SetPlaidCleanupService(svc services.PlaidItemCleanupService) {
	m.Called(svc)
}

func (m *MockDeletionService) SetAnalytics(a phclient.Analytics) {
	m.Called(a)
}

func newTestAccountDeletionJob(args AccountDeletionArgs) *river.Job[AccountDeletionArgs] {
	return &river.Job[AccountDeletionArgs]{
		JobRow: &rivertype.JobRow{
			ID:      1,
			Attempt: 1,
		},
		Args: args,
	}
}

func TestAccountDeletionArgs_Kind(t *testing.T) {
	args := AccountDeletionArgs{}
	assert.Equal(t, "account_deletion", args.Kind())
}

func TestAccountDeletionWorker_Work_Success(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New().String()
	requestID := uuid.New().String()

	mockService := new(MockDeletionService)
	mockService.On("ExecuteDeletion", ctx, userID).Return(nil)

	worker := NewAccountDeletionWorker(AccountDeletionWorkerDeps{
		DeletionService: mockService,
		Logger:          slog.Default(),
	})

	job := newTestAccountDeletionJob(AccountDeletionArgs{
		UserID:    userID,
		RequestID: requestID,
	})

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockService.AssertExpectations(t)
}

func TestAccountDeletionWorker_Work_DeletionError(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New().String()

	mockService := new(MockDeletionService)
	mockService.On("ExecuteDeletion", ctx, userID).
		Return(errors.New("database constraint violation"))

	worker := NewAccountDeletionWorker(AccountDeletionWorkerDeps{
		DeletionService: mockService,
		Logger:          slog.Default(),
	})

	job := newTestAccountDeletionJob(AccountDeletionArgs{
		UserID: userID,
	})

	err := worker.Work(ctx, job)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to delete account")
	mockService.AssertExpectations(t)
}

func TestAccountDeletionWorker_Work_WithoutRequestID(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New().String()

	mockService := new(MockDeletionService)
	mockService.On("ExecuteDeletion", ctx, userID).Return(nil)

	worker := NewAccountDeletionWorker(AccountDeletionWorkerDeps{
		DeletionService: mockService,
		Logger:          slog.Default(),
	})

	// No RequestID - should still work
	job := newTestAccountDeletionJob(AccountDeletionArgs{
		UserID: userID,
	})

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockService.AssertExpectations(t)
}

func TestAccountDeletionUniqueOpts(t *testing.T) {
	opts := AccountDeletionUniqueOpts()

	assert.True(t, opts.ByArgs)
	assert.Equal(t, time.Duration(0), opts.ByPeriod) // No time period - permanent uniqueness
	assert.Contains(t, opts.ByState, queue.JobStateAvailable)
	assert.Contains(t, opts.ByState, queue.JobStateRunning)
}

func TestNewAccountDeletionWorker_DefaultLogger(t *testing.T) {
	mockService := new(MockDeletionService)

	worker := NewAccountDeletionWorker(AccountDeletionWorkerDeps{
		DeletionService: mockService,
		// Logger is nil
	})

	assert.NotNil(t, worker.logger)
}
