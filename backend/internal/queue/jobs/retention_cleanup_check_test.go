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

func (m *MockRetentionProfileRepository) ClaimRetentionNotice30dSent(ctx context.Context, userID string) (bool, error) {
	args := m.Called(ctx, userID)
	return args.Bool(0), args.Error(1)
}

func (m *MockRetentionProfileRepository) ClaimRetentionNotice7dSent(ctx context.Context, userID string) (bool, error) {
	args := m.Called(ctx, userID)
	return args.Bool(0), args.Error(1)
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
	mockRepo.On("ClaimRetentionNotice30dSent", ctx, "user-1").Return(true, nil)
	mockRepo.On("ClaimRetentionNotice30dSent", ctx, "user-2").Return(true, nil)
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
	mockRepo.On("ClaimRetentionNotice7dSent", ctx, "user-3").Return(true, nil)
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
	mockRepo.On("ClaimRetentionNotice30dSent", ctx, "user-1").Return(true, nil)
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

// ─── #1413: comped users are exempt from retention cleanup (belt-and-braces worker guard;
// Task 4 additionally excludes them at the SQL level) ──

// TestRetentionCleanupCheck_CompedUser_NotDeleted is the hard-delete regression test: a comped
// user present in the expired-retention candidate list must NOT be passed to ExecuteDeletion.
func TestRetentionCleanupCheck_CompedUser_NotDeleted(t *testing.T) {
	ctx := context.Background()

	retentionExpiresAt := time.Now().Add(-24 * time.Hour) // already expired
	override := "household_2"
	overrideEndsAt := time.Now().Add(24 * time.Hour) // still active
	users := []*domain.User{
		{
			ID: "user-comped", Email: "comped@example.com",
			RetentionExpiresAt: &retentionExpiresAt,
			ManualTierOverride: &override, ManualTierEndsAt: &overrideEndsAt,
		},
	}

	mockRepo := new(MockRetentionProfileRepository)
	mockDeletion := new(MockRetentionDeletionService)
	mockEmail := new(MockRetentionEmailService)

	mockRepo.On("GetUsersNeedingRetention30dNotice", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersNeedingRetention7dNotice", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersWithExpiredRetention", ctx).Return(users, nil)
	// ExecuteDeletion is deliberately left unstubbed on mockDeletion: if the comp guard is
	// missing, the real call would panic with "unexpected call" rather than silently pass.

	worker := newTestRetentionWorker(mockRepo, mockDeletion, mockEmail)
	job := newTestRetentionCleanupJob(RetentionCleanupCheckArgs{TriggerSource: "periodic"})

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockDeletion.AssertNotCalled(t, "ExecuteDeletion", mock.Anything, "user-comped")
}

// TestRetentionCleanupCheck_CompedUser_No30dNotice proves a comped user does not receive the
// 30-day pre-deletion warning. ClaimRetentionNotice30dSent is deliberately left unstubbed.
func TestRetentionCleanupCheck_CompedUser_No30dNotice(t *testing.T) {
	ctx := context.Background()

	retentionExpiresAt := time.Now().Add(25 * 24 * time.Hour)
	override := "household_2"
	overrideEndsAt := time.Now().Add(24 * time.Hour)
	users := []*domain.User{
		{
			ID: "user-comped-30d", Email: "comped30@example.com",
			RetentionExpiresAt: &retentionExpiresAt,
			ManualTierOverride: &override, ManualTierEndsAt: &overrideEndsAt,
		},
	}

	mockRepo := new(MockRetentionProfileRepository)
	mockDeletion := new(MockRetentionDeletionService)
	mockEmail := new(MockRetentionEmailService)

	mockRepo.On("GetUsersNeedingRetention30dNotice", ctx).Return(users, nil)
	mockRepo.On("GetUsersNeedingRetention7dNotice", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersWithExpiredRetention", ctx).Return([]*domain.User{}, nil)

	worker := newTestRetentionWorker(mockRepo, mockDeletion, mockEmail)
	job := newTestRetentionCleanupJob(RetentionCleanupCheckArgs{TriggerSource: "periodic"})

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockEmail.AssertNotCalled(t, "SendRetentionNoticeEmail", mock.Anything, "comped30@example.com", mock.Anything, mock.Anything)
	mockRepo.AssertNotCalled(t, "ClaimRetentionNotice30dSent", mock.Anything, "user-comped-30d")
}

// TestRetentionCleanupCheck_CompedUser_No7dNotice proves a comped user does not receive the
// 7-day final warning. ClaimRetentionNotice7dSent is deliberately left unstubbed.
func TestRetentionCleanupCheck_CompedUser_No7dNotice(t *testing.T) {
	ctx := context.Background()

	retentionExpiresAt := time.Now().Add(5 * 24 * time.Hour)
	override := "household_2"
	overrideEndsAt := time.Now().Add(24 * time.Hour)
	users := []*domain.User{
		{
			ID: "user-comped-7d", Email: "comped7@example.com",
			RetentionExpiresAt: &retentionExpiresAt,
			ManualTierOverride: &override, ManualTierEndsAt: &overrideEndsAt,
		},
	}

	mockRepo := new(MockRetentionProfileRepository)
	mockDeletion := new(MockRetentionDeletionService)
	mockEmail := new(MockRetentionEmailService)

	mockRepo.On("GetUsersNeedingRetention30dNotice", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersNeedingRetention7dNotice", ctx).Return(users, nil)
	mockRepo.On("GetUsersWithExpiredRetention", ctx).Return([]*domain.User{}, nil)

	worker := newTestRetentionWorker(mockRepo, mockDeletion, mockEmail)
	job := newTestRetentionCleanupJob(RetentionCleanupCheckArgs{TriggerSource: "periodic"})

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockEmail.AssertNotCalled(t, "SendRetentionNoticeEmail", mock.Anything, "comped7@example.com", mock.Anything, mock.Anything)
	mockRepo.AssertNotCalled(t, "ClaimRetentionNotice7dSent", mock.Anything, "user-comped-7d")
}

// TestRetentionCleanupCheck_ExpiredOverride_NotDeleted is the C1 regression proof (Issue #1413,
// third round of remediation) at the worker-guard level.
//
// This test previously asserted the OPPOSITE — that a user whose manual override has expired is
// "not exempt" and gets deleted normally — on the theory that only a currently-active comp
// should block auto-deletion. That was the C1 bug encoded as a test, not a documented rule: the
// belt-and-braces guard in deleteExpiredAccounts must agree with the retention queries (which
// exclude ANY override, live or lapsed, via noManualOverrideAtAllPredicate) and with the
// corrected claimForDeletionPredicate the real ExecuteDeletion claim now enforces. A lapsed
// override that the daily manual_tier_override_expiry_check job hasn't yet cleared must still
// delay deletion — deleting "normally" here is exactly the lapsed-comp hard-delete this branch
// exists to fix. See domain.User.HasAnyManualOverride and noManualOverrideAtAllPredicate for the
// full rationale shared with the SQL-level fix.
func TestRetentionCleanupCheck_ExpiredOverride_NotDeleted(t *testing.T) {
	ctx := context.Background()

	retentionExpiresAt := time.Now().Add(-24 * time.Hour)
	override := "household_2"
	overrideEndsAt := time.Now().Add(-24 * time.Hour) // expired, not yet cleared
	users := []*domain.User{
		{
			ID: "user-expired-override", Email: "expiredoverride@example.com",
			RetentionExpiresAt: &retentionExpiresAt,
			ManualTierOverride: &override, ManualTierEndsAt: &overrideEndsAt,
		},
	}

	mockRepo := new(MockRetentionProfileRepository)
	mockDeletion := new(MockRetentionDeletionService)
	mockEmail := new(MockRetentionEmailService)

	mockRepo.On("GetUsersNeedingRetention30dNotice", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersNeedingRetention7dNotice", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersWithExpiredRetention", ctx).Return(users, nil)
	// ExecuteDeletion is deliberately left unstubbed — an unstubbed testify mock call panics,
	// a stronger proof than AssertNotCalled alone that the guard actually short-circuits.

	worker := newTestRetentionWorker(mockRepo, mockDeletion, mockEmail)
	job := newTestRetentionCleanupJob(RetentionCleanupCheckArgs{TriggerSource: "periodic"})

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockDeletion.AssertNotCalled(t, "ExecuteDeletion", mock.Anything, "user-expired-override")
}

// TestRetentionCleanupCheck_NonComped_DeletedNormally is the positive-direction proof required
// alongside the guard above: an ordinary user with NO override at all must still be deleted.
// Without this, an over-tight guard that silently stopped all deletions would pass every
// "nothing bad happened" test.
func TestRetentionCleanupCheck_NonComped_DeletedNormally(t *testing.T) {
	ctx := context.Background()

	retentionExpiresAt := time.Now().Add(-24 * time.Hour)
	users := []*domain.User{
		{ID: "user-ordinary", Email: "ordinary@example.com", RetentionExpiresAt: &retentionExpiresAt},
	}

	mockRepo := new(MockRetentionProfileRepository)
	mockDeletion := new(MockRetentionDeletionService)
	mockEmail := new(MockRetentionEmailService)

	mockRepo.On("GetUsersNeedingRetention30dNotice", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersNeedingRetention7dNotice", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersWithExpiredRetention", ctx).Return(users, nil)
	mockDeletion.On("ExecuteDeletion", ctx, "user-ordinary").Return(nil)

	worker := newTestRetentionWorker(mockRepo, mockDeletion, mockEmail)
	job := newTestRetentionCleanupJob(RetentionCleanupCheckArgs{TriggerSource: "periodic"})

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockDeletion.AssertCalled(t, "ExecuteDeletion", ctx, "user-ordinary")
}

// ─── Issue #1413 Medium finding M9 (Codex review round 2): ClaimRetentionNotice30dSent/7dSent
// close the comp race AND the double-send window ──
//
// The belt-and-braces HasActiveManualOverride() check above only catches a LIVE comp visible
// on the candidate-list snapshot. These tests use a user with NO override visible on that
// snapshot (so the short-circuit does not fire), forcing execution through to the claim —
// proving the claim itself, not the earlier snapshot check, is what gates the email send. A
// lost claim represents either a comp granted since the query ran, a lapsed-but-uncleared
// override (which the belt-and-braces check cannot see), or a concurrent worker/retry that
// already claimed the notice.

// TestRetentionCleanupCheck_30dClaimLost_NoEmailSent proves the race is actually closed: even
// though the snapshot looked eligible, a lost claim must prevent the email send entirely.
// SendRetentionNoticeEmail is deliberately left unstubbed — an unstubbed testify mock call
// panics, a stronger proof than AssertNotCalled alone.
func TestRetentionCleanupCheck_30dClaimLost_NoEmailSent(t *testing.T) {
	ctx := context.Background()

	retentionExpiresAt := time.Now().Add(25 * 24 * time.Hour)
	users := []*domain.User{
		{ID: "user-race-30d", Email: "race30@example.com", RetentionExpiresAt: &retentionExpiresAt},
	}

	mockRepo := new(MockRetentionProfileRepository)
	mockDeletion := new(MockRetentionDeletionService)
	mockEmail := new(MockRetentionEmailService)

	mockRepo.On("GetUsersNeedingRetention30dNotice", ctx).Return(users, nil)
	mockRepo.On("ClaimRetentionNotice30dSent", ctx, "user-race-30d").Return(false, nil)
	mockRepo.On("GetUsersNeedingRetention7dNotice", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersWithExpiredRetention", ctx).Return([]*domain.User{}, nil)
	// SendRetentionNoticeEmail intentionally left unstubbed.

	worker := newTestRetentionWorker(mockRepo, mockDeletion, mockEmail)
	job := newTestRetentionCleanupJob(RetentionCleanupCheckArgs{TriggerSource: "periodic"})

	err := worker.Work(ctx, job)

	require.NoError(t, err, "a lost claim is a legitimate skip, not a job failure")
	mockEmail.AssertNotCalled(t, "SendRetentionNoticeEmail", mock.Anything, "race30@example.com", mock.Anything, mock.Anything)
}

// TestRetentionCleanupCheck_7dClaimLost_NoEmailSent mirrors the 30-day test for the 7-day
// notice.
func TestRetentionCleanupCheck_7dClaimLost_NoEmailSent(t *testing.T) {
	ctx := context.Background()

	retentionExpiresAt := time.Now().Add(5 * 24 * time.Hour)
	users := []*domain.User{
		{ID: "user-race-7d", Email: "race7@example.com", RetentionExpiresAt: &retentionExpiresAt},
	}

	mockRepo := new(MockRetentionProfileRepository)
	mockDeletion := new(MockRetentionDeletionService)
	mockEmail := new(MockRetentionEmailService)

	mockRepo.On("GetUsersNeedingRetention30dNotice", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersNeedingRetention7dNotice", ctx).Return(users, nil)
	mockRepo.On("ClaimRetentionNotice7dSent", ctx, "user-race-7d").Return(false, nil)
	mockRepo.On("GetUsersWithExpiredRetention", ctx).Return([]*domain.User{}, nil)
	// SendRetentionNoticeEmail intentionally left unstubbed.

	worker := newTestRetentionWorker(mockRepo, mockDeletion, mockEmail)
	job := newTestRetentionCleanupJob(RetentionCleanupCheckArgs{TriggerSource: "periodic"})

	err := worker.Work(ctx, job)

	require.NoError(t, err, "a lost claim is a legitimate skip, not a job failure")
	mockEmail.AssertNotCalled(t, "SendRetentionNoticeEmail", mock.Anything, "race7@example.com", mock.Anything, mock.Anything)
}

// TestRetentionCleanupCheck_30dClaimError_ContinuesProcessing proves a claim-level error (as
// opposed to a lost claim) is logged and counted, but does not stop the loop from processing
// remaining candidates or fail the overall job (notices are best-effort, like sends).
func TestRetentionCleanupCheck_30dClaimError_ContinuesProcessing(t *testing.T) {
	ctx := context.Background()

	retentionExpiresAt := time.Now().Add(25 * 24 * time.Hour)
	users := []*domain.User{
		{ID: "user-claimerr", Email: "claimerr@example.com", RetentionExpiresAt: &retentionExpiresAt},
		{ID: "user-claimok", Email: "claimok@example.com", RetentionExpiresAt: &retentionExpiresAt},
	}

	mockRepo := new(MockRetentionProfileRepository)
	mockDeletion := new(MockRetentionDeletionService)
	mockEmail := new(MockRetentionEmailService)

	mockRepo.On("GetUsersNeedingRetention30dNotice", ctx).Return(users, nil)
	mockRepo.On("ClaimRetentionNotice30dSent", ctx, "user-claimerr").Return(false, errors.New("db unavailable"))
	mockRepo.On("ClaimRetentionNotice30dSent", ctx, "user-claimok").Return(true, nil)
	mockRepo.On("GetUsersNeedingRetention7dNotice", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersWithExpiredRetention", ctx).Return([]*domain.User{}, nil)

	const subject30 = "Your BoostedBudget data will be deleted in 30 days"
	mockEmail.On("SendRetentionNoticeEmail", ctx, "claimok@example.com", subject30, mock.AnythingOfType("string")).Return(nil)

	worker := newTestRetentionWorker(mockRepo, mockDeletion, mockEmail)
	job := newTestRetentionCleanupJob(RetentionCleanupCheckArgs{TriggerSource: "periodic"})

	err := worker.Work(ctx, job)

	require.NoError(t, err, "a per-user claim error does not fail the overall job")
	mockEmail.AssertNotCalled(t, "SendRetentionNoticeEmail", mock.Anything, "claimerr@example.com", mock.Anything, mock.Anything)
	mockEmail.AssertCalled(t, "SendRetentionNoticeEmail", ctx, "claimok@example.com", subject30, mock.AnythingOfType("string"))
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
