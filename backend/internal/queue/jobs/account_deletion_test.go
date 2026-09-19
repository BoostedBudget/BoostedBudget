package jobs

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/cache"
	phclient "github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
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
	// LastKind records the repository.DeletionClaimKind the last ExecuteDeletion call passed
	// (Issue #1440, T0). Asserted by TestAccountDeletionWorker_Work_ClaimsAsUserRequested.
	LastKind repository.DeletionClaimKind
}

func (m *MockDeletionService) RequestDeletion(ctx context.Context, userID, email string) (string, time.Time, error) {
	args := m.Called(ctx, userID, email)
	return args.String(0), args.Get(1).(time.Time), args.Error(2)
}

func (m *MockDeletionService) CancelDeletion(ctx context.Context, token string) error {
	args := m.Called(ctx, token)
	return args.Error(0)
}

// ExecuteDeletion records the caller-declared authorisation kind in LastKind rather than folding it
// into m.Called: the kind is asserted directly by
// TestAccountDeletionWorker_Work_ClaimsAsUserRequested, which keeps every other expectation in this
// package free of an argument it does not care about (Issue #1440, T0).
func (m *MockDeletionService) ExecuteDeletion(ctx context.Context, userID string, kind repository.DeletionClaimKind) error {
	m.LastKind = kind
	args := m.Called(ctx, userID)
	return args.Error(0)
}

func (m *MockDeletionService) SetPlaidCleanupService(svc services.PlaidItemCleanupService) {
	m.Called(svc)
}

func (m *MockDeletionService) SetPaddleCleanupService(svc services.PaddleDeletionCleaner) {}

// SetHouseholdReader/SetHouseholdInviteRevoker (#1367 — Phase 3) are not exercised by
// these account-deletion job tests; stubbed to satisfy the expanded interface.
func (m *MockDeletionService) SetHouseholdReader(r services.HouseholdReader)               {}
func (m *MockDeletionService) SetHouseholdInviteRevoker(r services.HouseholdInviteRevoker) {}
func (m *MockDeletionService) SetDB(db repository.DB)                                      {}

// SetWorkClaimsRepo (Issue #1413 Critical finding C2) is not exercised by these
// account-deletion job tests; stubbed to satisfy the expanded interface.
func (m *MockDeletionService) SetWorkClaimsRepo(repo repository.WorkClaimsRepository)         {}
func (m *MockDeletionService) SetSubscriptionCleanupGuard(services.SubscriptionCleanupGuard)  {}
func (m *MockDeletionService) SetRevenueCatCleanupService(services.RevenueCatDeletionCleaner) {}

func (m *MockDeletionService) SetAnalytics(a phclient.Analytics) {
	m.Called(a)
}

// SetCache (Issue #1531 Task 10b) is not exercised by these account-deletion job tests; stubbed to
// satisfy the expanded interface.
func (m *MockDeletionService) SetCache(purger cache.Purger, gens *cache.Generations, jq queue.JobQueue) {
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

// TestAccountDeletionWorker_Work_ClaimsAsUserRequested pins the authorisation this worker declares
// (Issue #1440, T0). This job is only ever enqueued by ScheduledDeletionCheckWorker from
// GetUsersScheduledForDeletionPage, i.e. always on the strength of a user's own deletion request
// whose grace period elapsed. Declaring DeletionKindRetention instead would authorise deleting a
// user who never asked and whose retention clock has not expired — and would skip the
// deletion_scheduled_at check that is the entire reason a stale, cancelled, or re-requested job now
// no-ops.
func TestAccountDeletionWorker_Work_ClaimsAsUserRequested(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New().String()

	mockService := new(MockDeletionService)
	mockService.On("ExecuteDeletion", ctx, userID).Return(nil)

	worker := NewAccountDeletionWorker(AccountDeletionWorkerDeps{
		DeletionService: mockService,
		Logger:          slog.Default(),
	})

	require.NoError(t, worker.Work(ctx, newTestAccountDeletionJob(AccountDeletionArgs{UserID: userID})))
	assert.Equal(t, repository.DeletionKindUserRequested, mockService.LastKind,
		"the account_deletion job is a user-requested deletion and must claim under that authorisation")
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
