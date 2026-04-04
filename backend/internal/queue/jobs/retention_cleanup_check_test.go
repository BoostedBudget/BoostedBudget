// Package jobs contains River job definitions and workers for async processing.
// Issue #930: Retention cleanup check job tests.
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

// MockRetentionProfileRepository mocks the RetentionProfileRepository interface.
type MockRetentionProfileRepository struct {
	mock.Mock
}

func (m *MockRetentionProfileRepository) GetUsersNeedingRetention30dNotice(ctx context.Context) ([]*domain.User, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.User), args.Error(1)
}

func (m *MockRetentionProfileRepository) GetUsersNeedingRetention7dNotice(ctx context.Context) ([]*domain.User, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.User), args.Error(1)
}

func (m *MockRetentionProfileRepository) GetUsersWithExpiredRetention(ctx context.Context) ([]*domain.User, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.User), args.Error(1)
}

func (m *MockRetentionProfileRepository) SetRetentionNotice30dSent(ctx context.Context, userID string) error {
	args := m.Called(ctx, userID)
	return args.Error(0)
}

func (m *MockRetentionProfileRepository) SetRetentionNotice7dSent(ctx context.Context, userID string) error {
	args := m.Called(ctx, userID)
	return args.Error(0)
}

// MockRetentionDeletionService mocks the RetentionDeletionService interface.
type MockRetentionDeletionService struct {
	mock.Mock
}

func (m *MockRetentionDeletionService) ExecuteDeletion(ctx context.Context, userID string) error {
	args := m.Called(ctx, userID)
	return args.Error(0)
}

// MockRetentionEmailService mocks the RetentionEmailService interface.
type MockRetentionEmailService struct {
	mock.Mock
}

func (m *MockRetentionEmailService) SendRetentionNoticeEmail(ctx context.Context, toEmail, subject, body string) error {
	args := m.Called(ctx, toEmail, subject, body)
	return args.Error(0)
}

// Test helpers

func newTestRetentionCleanupJob(args RetentionCleanupCheckArgs) *river.Job[RetentionCleanupCheckArgs] {
	return &river.Job[RetentionCleanupCheckArgs]{
		JobRow: &rivertype.JobRow{
			ID:      1,
			Attempt: 1,
		},
		Args: args,
	}
}

func newTestRetentionWorker(repo RetentionProfileRepository, deletionSvc RetentionDeletionService, emailSvc RetentionEmailService) *RetentionCleanupCheckWorker {
	return NewRetentionCleanupCheckWorker(RetentionCleanupCheckWorkerDeps{
		ProfileRepo:     repo,
		DeletionService: deletionSvc,
		EmailService:    emailSvc,
		RetentionMonths: 6,
		Logger:          slog.Default(),
	})
}

// --- Kind ---

func TestRetentionCleanupCheckArgs_Kind(t *testing.T) {
	args := RetentionCleanupCheckArgs{}
	assert.Equal(t, "retention_cleanup_check", args.Kind())
}

// --- 30-day notice tests ---

func TestRetentionCleanupCheck_30dNotice_SentToUsers(t *testing.T) {
	ctx := context.Background()

	retentionExpiresAt := time.Now().Add(25 * 24 * time.Hour)
	users := []*domain.User{
		{ID: "user-1", Email: "user1@example.com", RetentionExpiresAt: &retentionExpiresAt},
		{ID: "user-2", Email: "user2@example.com", RetentionExpiresAt: &retentionExpiresAt},
	}

	mockRepo := new(MockRetentionProfileRepository)
	mockDeletion := new(MockRetentionDeletionService)
	mockEmail := new(MockRetentionEmailService)

	mockRepo.On("GetUsersNeedingRetention30dNotice", ctx).Return(users, nil)
	mockRepo.On("SetRetentionNotice30dSent", ctx, "user-1").Return(nil)
	mockRepo.On("SetRetentionNotice30dSent", ctx, "user-2").Return(nil)
	mockRepo.On("GetUsersNeedingRetention7dNotice", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersWithExpiredRetention", ctx).Return([]*domain.User{}, nil)

	const subject30 = "Your BoostedBudget data will be deleted in 30 days"
	mockEmail.On("SendRetentionNoticeEmail", ctx, "user1@example.com", subject30, mock.AnythingOfType("string")).Return(nil)
	mockEmail.On("SendRetentionNoticeEmail", ctx, "user2@example.com", subject30, mock.AnythingOfType("string")).Return(nil)

	worker := newTestRetentionWorker(mockRepo, mockDeletion, mockEmail)
	job := newTestRetentionCleanupJob(RetentionCleanupCheckArgs{TriggerSource: "periodic"})

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockRepo.AssertExpectations(t)
	mockEmail.AssertExpectations(t)
	mockDeletion.AssertNotCalled(t, "ExecuteDeletion")
}

// --- 7-day notice tests ---

func TestRetentionCleanupCheck_7dNotice_SentToUsers(t *testing.T) {
	ctx := context.Background()

	retentionExpiresAt := time.Now().Add(5 * 24 * time.Hour)
	users := []*domain.User{
		{ID: "user-3", Email: "user3@example.com", RetentionExpiresAt: &retentionExpiresAt},
	}

	mockRepo := new(MockRetentionProfileRepository)
	mockDeletion := new(MockRetentionDeletionService)
	mockEmail := new(MockRetentionEmailService)

	mockRepo.On("GetUsersNeedingRetention30dNotice", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersNeedingRetention7dNotice", ctx).Return(users, nil)
	mockRepo.On("SetRetentionNotice7dSent", ctx, "user-3").Return(nil)
	mockRepo.On("GetUsersWithExpiredRetention", ctx).Return([]*domain.User{}, nil)

	const subject7 = "Final notice: BoostedBudget data deletion in 7 days"
	mockEmail.On("SendRetentionNoticeEmail", ctx, "user3@example.com", subject7, mock.AnythingOfType("string")).Return(nil)

	worker := newTestRetentionWorker(mockRepo, mockDeletion, mockEmail)
	job := newTestRetentionCleanupJob(RetentionCleanupCheckArgs{TriggerSource: "periodic"})

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockRepo.AssertExpectations(t)
	mockEmail.AssertExpectations(t)
}

// --- Auto-deletion tests ---

func TestRetentionCleanupCheck_AutoDeletion_ExecutedForExpiredUsers(t *testing.T) {
	ctx := context.Background()

	retentionExpiresAt := time.Now().Add(-24 * time.Hour) // Already expired
	users := []*domain.User{
		{ID: "user-exp-1", Email: "expired1@example.com", RetentionExpiresAt: &retentionExpiresAt},
		{ID: "user-exp-2", Email: "expired2@example.com", RetentionExpiresAt: &retentionExpiresAt},
	}

	mockRepo := new(MockRetentionProfileRepository)
	mockDeletion := new(MockRetentionDeletionService)
	mockEmail := new(MockRetentionEmailService)

	mockRepo.On("GetUsersNeedingRetention30dNotice", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersNeedingRetention7dNotice", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersWithExpiredRetention", ctx).Return(users, nil)
	mockDeletion.On("ExecuteDeletion", ctx, "user-exp-1").Return(nil)
	mockDeletion.On("ExecuteDeletion", ctx, "user-exp-2").Return(nil)

	worker := newTestRetentionWorker(mockRepo, mockDeletion, mockEmail)
	job := newTestRetentionCleanupJob(RetentionCleanupCheckArgs{TriggerSource: "periodic"})

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockRepo.AssertExpectations(t)
	mockDeletion.AssertNumberOfCalls(t, "ExecuteDeletion", 2)
	mockEmail.AssertNotCalled(t, "SendRetentionNoticeEmail")
}

// --- Error resilience tests ---

func TestRetentionCleanupCheck_DeletionError_ContinuesProcessing(t *testing.T) {
	ctx := context.Background()

	retentionExpiresAt := time.Now().Add(-24 * time.Hour)
	users := []*domain.User{
		{ID: "user-fail", Email: "fail@example.com", RetentionExpiresAt: &retentionExpiresAt},
		{ID: "user-ok", Email: "ok@example.com", RetentionExpiresAt: &retentionExpiresAt},
	}

	mockRepo := new(MockRetentionProfileRepository)
	mockDeletion := new(MockRetentionDeletionService)
	mockEmail := new(MockRetentionEmailService)

	mockRepo.On("GetUsersNeedingRetention30dNotice", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersNeedingRetention7dNotice", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersWithExpiredRetention", ctx).Return(users, nil)
	mockDeletion.On("ExecuteDeletion", ctx, "user-fail").Return(errors.New("deletion failed"))
	mockDeletion.On("ExecuteDeletion", ctx, "user-ok").Return(nil)

	worker := newTestRetentionWorker(mockRepo, mockDeletion, mockEmail)
	job := newTestRetentionCleanupJob(RetentionCleanupCheckArgs{TriggerSource: "periodic"})

	// Job continues processing all users even when one fails, then returns an error so River retries
	err := worker.Work(ctx, job)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "1 of 2 deletions failed")
	mockDeletion.AssertNumberOfCalls(t, "ExecuteDeletion", 2)
}

// TestRetentionCleanupCheck_NoUsers_NoOp tests that the job succeeds when there's nothing to do.
func TestRetentionCleanupCheck_NoUsers_NoOp(t *testing.T) {
	ctx := context.Background()

	mockRepo := new(MockRetentionProfileRepository)
	mockDeletion := new(MockRetentionDeletionService)
	mockEmail := new(MockRetentionEmailService)

	mockRepo.On("GetUsersNeedingRetention30dNotice", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersNeedingRetention7dNotice", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersWithExpiredRetention", ctx).Return([]*domain.User{}, nil)

	worker := newTestRetentionWorker(mockRepo, mockDeletion, mockEmail)
	job := newTestRetentionCleanupJob(RetentionCleanupCheckArgs{TriggerSource: "periodic"})

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockDeletion.AssertNotCalled(t, "ExecuteDeletion")
	mockEmail.AssertNotCalled(t, "SendRetentionNoticeEmail")
}

// TestRetentionCleanupCheck_EmailServiceNil_SkipsSending tests that nil email service is safe.
func TestRetentionCleanupCheck_EmailServiceNil_SkipsSending(t *testing.T) {
	ctx := context.Background()

	retentionExpiresAt := time.Now().Add(25 * 24 * time.Hour)
	users := []*domain.User{
		{ID: "user-1", Email: "user1@example.com", RetentionExpiresAt: &retentionExpiresAt},
	}

	mockRepo := new(MockRetentionProfileRepository)
	mockDeletion := new(MockRetentionDeletionService)

	mockRepo.On("GetUsersNeedingRetention30dNotice", ctx).Return(users, nil)
	mockRepo.On("SetRetentionNotice30dSent", ctx, "user-1").Return(nil)
	mockRepo.On("GetUsersNeedingRetention7dNotice", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersWithExpiredRetention", ctx).Return([]*domain.User{}, nil)

	// nil email service
	worker := newTestRetentionWorker(mockRepo, mockDeletion, nil)
	job := newTestRetentionCleanupJob(RetentionCleanupCheckArgs{TriggerSource: "periodic"})

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockRepo.AssertExpectations(t)
}

// TestRetentionCleanupCheckUniqueOpts tests the unique opts configuration.
func TestRetentionCleanupCheckUniqueOpts(t *testing.T) {
	opts := RetentionCleanupCheckUniqueOpts()

	assert.False(t, opts.ByArgs)
	assert.Equal(t, 23*time.Hour, opts.ByPeriod)
	assert.Contains(t, opts.ByState, queue.JobStateAvailable)
	assert.Contains(t, opts.ByState, queue.JobStateRunning)
}

// TestNewRetentionCleanupCheckWorker_DefaultLogger tests that nil logger uses default.
func TestNewRetentionCleanupCheckWorker_DefaultLogger(t *testing.T) {
	mockRepo := new(MockRetentionProfileRepository)
	mockDeletion := new(MockRetentionDeletionService)

	worker := NewRetentionCleanupCheckWorker(RetentionCleanupCheckWorkerDeps{
		ProfileRepo:     mockRepo,
		DeletionService: mockDeletion,
		// Logger nil — should use default
	})

	assert.NotNil(t, worker.logger)
}

// TestNewRetentionCleanupCheckWorker_DefaultRetentionMonths tests that 0 months uses default (6).
func TestNewRetentionCleanupCheckWorker_DefaultRetentionMonths(t *testing.T) {
	mockRepo := new(MockRetentionProfileRepository)
	mockDeletion := new(MockRetentionDeletionService)

	worker := NewRetentionCleanupCheckWorker(RetentionCleanupCheckWorkerDeps{
		ProfileRepo:     mockRepo,
		DeletionService: mockDeletion,
		RetentionMonths: 0, // Should default to 6
	})

	assert.Equal(t, 6, worker.retentionMonths)
}
