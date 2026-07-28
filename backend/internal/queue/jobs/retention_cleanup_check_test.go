// Package jobs contains River job definitions and workers for async processing.
// Issue #930: Retention cleanup check job tests.
package jobs

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/observability"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/prometheus/client_golang/prometheus/testutil"
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

func (m *MockRetentionProfileRepository) IsRetentionNotice30dEligible(ctx context.Context, userID string) (bool, error) {
	args := m.Called(ctx, userID)
	return args.Bool(0), args.Error(1)
}

func (m *MockRetentionProfileRepository) IsRetentionNotice7dEligible(ctx context.Context, userID string) (bool, error) {
	args := m.Called(ctx, userID)
	return args.Bool(0), args.Error(1)
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
			ID: 1,
			// Kind mirrors what River itself sets at dequeue time (Args.Kind()) — needed so
			// tests asserting on the observability.JobLastSuccessTimestamp{kind=...} heartbeat
			// gauge exercise the REAL label, not an empty string.
			Kind:    args.Kind(),
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

// testRetentionClaimToken is the fixed fencing token newAllowingRetentionClaimsRepo's Claim stub
// returns. Tests that only care about the worker's OTHER behavior can rely on it implicitly;
// tests exercising the claim mechanism itself supply their own mockWorkClaimsRepository via
// worker.SetWorkClaimsRepo(customMock). Mirrors subscription_webhook_test.go's
// testWebhookClaimToken / deletion_service_test.go's testDeletionClaimToken (same package here:
// mockWorkClaimsRepository is defined once, in subscription_webhook_test.go, and reused).
const testRetentionClaimToken = "test-retention-claim-token"

// newAllowingRetentionClaimsRepo returns a mockWorkClaimsRepository stubbed to always grant and
// complete BOTH retention-notice work claims (ActionRetentionNotice30d, ActionRetentionNotice7d)
// — for tests that exercise the worker's OTHER behavior and aren't themselves testing the claim
// mechanism. userID is wildcarded so one instance works across this file's whole fixture matrix.
// Mirrors past_due_service_test.go's newAllowingPastDueClaimsRepo.
func newAllowingRetentionClaimsRepo() *mockWorkClaimsRepository {
	m := new(mockWorkClaimsRepository)
	for _, action := range []string{repository.ActionRetentionNotice30d, repository.ActionRetentionNotice7d} {
		m.On("Claim", mock.Anything, repository.ResourceTypeUser, mock.Anything, action, retentionNoticeClaimLease).
			Return(testRetentionClaimToken, true, nil).Maybe()
		m.On("Complete", mock.Anything, repository.ResourceTypeUser, mock.Anything, action, testRetentionClaimToken).
			Return(true, nil).Maybe()
		m.On("Release", mock.Anything, repository.ResourceTypeUser, mock.Anything, action, testRetentionClaimToken, mock.AnythingOfType("string")).
			Return(true, nil).Maybe()
	}
	return m
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
	mockRepo.On("IsRetentionNotice30dEligible", ctx, "user-1").Return(true, nil)
	mockRepo.On("IsRetentionNotice30dEligible", ctx, "user-2").Return(true, nil)
	mockRepo.On("SetRetentionNotice30dSent", ctx, "user-1").Return(nil)
	mockRepo.On("SetRetentionNotice30dSent", ctx, "user-2").Return(nil)
	mockRepo.On("GetUsersNeedingRetention7dNotice", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersWithExpiredRetention", ctx).Return([]*domain.User{}, nil)

	const subject30 = "Your BoostedBudget data will be deleted in 30 days"
	mockEmail.On("SendRetentionNoticeEmail", ctx, "user1@example.com", subject30, mock.AnythingOfType("string")).Return(nil)
	mockEmail.On("SendRetentionNoticeEmail", ctx, "user2@example.com", subject30, mock.AnythingOfType("string")).Return(nil)

	worker := newTestRetentionWorker(mockRepo, mockDeletion, mockEmail)
	worker.SetWorkClaimsRepo(newAllowingRetentionClaimsRepo())
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
	mockRepo.On("IsRetentionNotice7dEligible", ctx, "user-3").Return(true, nil)
	mockRepo.On("SetRetentionNotice7dSent", ctx, "user-3").Return(nil)
	mockRepo.On("GetUsersWithExpiredRetention", ctx).Return([]*domain.User{}, nil)

	const subject7 = "Final notice: BoostedBudget data deletion in 7 days"
	mockEmail.On("SendRetentionNoticeEmail", ctx, "user3@example.com", subject7, mock.AnythingOfType("string")).Return(nil)

	worker := newTestRetentionWorker(mockRepo, mockDeletion, mockEmail)
	worker.SetWorkClaimsRepo(newAllowingRetentionClaimsRepo())
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

// TestRetentionCleanupCheck_EmailServiceNil_SkipsSending tests that nil email service is safe, AND
// (Issue #1413 M9) that a demonstrably-unsendable notice never touches work_claims or the profile
// repo's eligibility/mark methods at all — "prefer not to claim when there is nothing to send".
func TestRetentionCleanupCheck_EmailServiceNil_SkipsSending(t *testing.T) {
	ctx := context.Background()

	retentionExpiresAt := time.Now().Add(25 * 24 * time.Hour)
	users := []*domain.User{
		{ID: "user-1", Email: "user1@example.com", RetentionExpiresAt: &retentionExpiresAt},
	}

	mockRepo := new(MockRetentionProfileRepository)
	mockDeletion := new(MockRetentionDeletionService)

	mockRepo.On("GetUsersNeedingRetention30dNotice", ctx).Return(users, nil)
	mockRepo.On("GetUsersNeedingRetention7dNotice", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersWithExpiredRetention", ctx).Return([]*domain.User{}, nil)
	// IsRetentionNotice30dEligible / SetRetentionNotice30dSent are deliberately left unstubbed:
	// with no email service configured, the skip must happen BEFORE claiming or touching the
	// profile repo at all. An unstubbed testify mock call panics, a stronger proof than
	// AssertExpectations alone.

	// nil email service, but claimsRepo IS wired to a WORKING (allowing) mock — this is the part
	// that makes the test actually falsifiable: if the "skip before claiming" check were removed,
	// Claim would succeed and the code would proceed to call the unstubbed
	// IsRetentionNotice30dEligible, panicking. A nil claimsRepo here would let that same broken
	// code path silently no-op via the (unrelated) "claims repo not configured" guard instead,
	// masking the bug this test exists to catch.
	worker := newTestRetentionWorker(mockRepo, mockDeletion, nil)
	worker.SetWorkClaimsRepo(newAllowingRetentionClaimsRepo())
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
// 30-day pre-deletion warning. IsRetentionNotice30dEligible is deliberately left unstubbed.
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
	mockRepo.AssertNotCalled(t, "IsRetentionNotice30dEligible", mock.Anything, "user-comped-30d")
}

// TestRetentionCleanupCheck_CompedUser_No7dNotice proves a comped user does not receive the
// 7-day final warning. IsRetentionNotice7dEligible is deliberately left unstubbed.
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
	mockRepo.AssertNotCalled(t, "IsRetentionNotice7dEligible", mock.Anything, "user-comped-7d")
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

// ─── Issue #1413 Medium finding M9 (Codex review round 3): exclusivity now lives in work_claims
// (Claim/Complete/Release), and the *_sent_at marker is stamped ONLY after a successful send. ──
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
// SendRetentionNoticeEmail and IsRetentionNotice30dEligible are deliberately left unstubbed — an
// unstubbed testify mock call panics, a stronger proof than AssertNotCalled alone.
func TestRetentionCleanupCheck_30dClaimLost_NoEmailSent(t *testing.T) {
	ctx := context.Background()

	retentionExpiresAt := time.Now().Add(25 * 24 * time.Hour)
	users := []*domain.User{
		{ID: "user-race-30d", Email: "race30@example.com", RetentionExpiresAt: &retentionExpiresAt},
	}

	mockRepo := new(MockRetentionProfileRepository)
	mockDeletion := new(MockRetentionDeletionService)
	mockEmail := new(MockRetentionEmailService)
	mockClaims := new(mockWorkClaimsRepository)

	mockRepo.On("GetUsersNeedingRetention30dNotice", ctx).Return(users, nil)
	mockRepo.On("GetUsersNeedingRetention7dNotice", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersWithExpiredRetention", ctx).Return([]*domain.User{}, nil)
	mockClaims.On("Claim", ctx, repository.ResourceTypeUser, "user-race-30d", repository.ActionRetentionNotice30d, retentionNoticeClaimLease).
		Return("", false, nil)

	worker := newTestRetentionWorker(mockRepo, mockDeletion, mockEmail)
	worker.SetWorkClaimsRepo(mockClaims)
	job := newTestRetentionCleanupJob(RetentionCleanupCheckArgs{TriggerSource: "periodic"})

	err := worker.Work(ctx, job)

	require.NoError(t, err, "a lost claim is a legitimate skip, not a job failure")
	mockEmail.AssertNotCalled(t, "SendRetentionNoticeEmail", mock.Anything, "race30@example.com", mock.Anything, mock.Anything)
	mockClaims.AssertExpectations(t)
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
	mockClaims := new(mockWorkClaimsRepository)

	mockRepo.On("GetUsersNeedingRetention30dNotice", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersNeedingRetention7dNotice", ctx).Return(users, nil)
	mockRepo.On("GetUsersWithExpiredRetention", ctx).Return([]*domain.User{}, nil)
	mockClaims.On("Claim", ctx, repository.ResourceTypeUser, "user-race-7d", repository.ActionRetentionNotice7d, retentionNoticeClaimLease).
		Return("", false, nil)

	worker := newTestRetentionWorker(mockRepo, mockDeletion, mockEmail)
	worker.SetWorkClaimsRepo(mockClaims)
	job := newTestRetentionCleanupJob(RetentionCleanupCheckArgs{TriggerSource: "periodic"})

	err := worker.Work(ctx, job)

	require.NoError(t, err, "a lost claim is a legitimate skip, not a job failure")
	mockEmail.AssertNotCalled(t, "SendRetentionNoticeEmail", mock.Anything, "race7@example.com", mock.Anything, mock.Anything)
	mockClaims.AssertExpectations(t)
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
	mockClaims := new(mockWorkClaimsRepository)

	mockRepo.On("GetUsersNeedingRetention30dNotice", ctx).Return(users, nil)
	mockRepo.On("GetUsersNeedingRetention7dNotice", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersWithExpiredRetention", ctx).Return([]*domain.User{}, nil)

	mockClaims.On("Claim", ctx, repository.ResourceTypeUser, "user-claimerr", repository.ActionRetentionNotice30d, retentionNoticeClaimLease).
		Return("", false, errors.New("db unavailable"))
	mockClaims.On("Claim", ctx, repository.ResourceTypeUser, "user-claimok", repository.ActionRetentionNotice30d, retentionNoticeClaimLease).
		Return(testRetentionClaimToken, true, nil)
	mockClaims.On("Complete", ctx, repository.ResourceTypeUser, "user-claimok", repository.ActionRetentionNotice30d, testRetentionClaimToken).
		Return(true, nil)

	mockRepo.On("IsRetentionNotice30dEligible", ctx, "user-claimok").Return(true, nil)
	mockRepo.On("SetRetentionNotice30dSent", ctx, "user-claimok").Return(nil)

	const subject30 = "Your BoostedBudget data will be deleted in 30 days"
	mockEmail.On("SendRetentionNoticeEmail", ctx, "claimok@example.com", subject30, mock.AnythingOfType("string")).Return(nil)

	worker := newTestRetentionWorker(mockRepo, mockDeletion, mockEmail)
	worker.SetWorkClaimsRepo(mockClaims)
	job := newTestRetentionCleanupJob(RetentionCleanupCheckArgs{TriggerSource: "periodic"})

	err := worker.Work(ctx, job)

	require.NoError(t, err, "a per-user claim error does not fail the overall job")
	mockEmail.AssertNotCalled(t, "SendRetentionNoticeEmail", mock.Anything, "claimerr@example.com", mock.Anything, mock.Anything)
	mockEmail.AssertCalled(t, "SendRetentionNoticeEmail", ctx, "claimok@example.com", subject30, mock.AnythingOfType("string"))
}

// TestRetentionCleanupCheck_30dIneligibleAfterClaim_ReleasesClaim_SkipsSend proves the
// re-verification step actually gates the send: even though the claim was won, an eligibility
// check reporting false (comp granted, window closed, or already sent since the candidate query
// ran) must Release the claim and skip the send. SendRetentionNoticeEmail is deliberately left
// unstubbed.
func TestRetentionCleanupCheck_30dIneligibleAfterClaim_ReleasesClaim_SkipsSend(t *testing.T) {
	ctx := context.Background()

	retentionExpiresAt := time.Now().Add(25 * 24 * time.Hour)
	users := []*domain.User{
		{ID: "user-ineligible-30d", Email: "ineligible30@example.com", RetentionExpiresAt: &retentionExpiresAt},
	}

	mockRepo := new(MockRetentionProfileRepository)
	mockDeletion := new(MockRetentionDeletionService)
	mockEmail := new(MockRetentionEmailService)
	mockClaims := new(mockWorkClaimsRepository)

	mockRepo.On("GetUsersNeedingRetention30dNotice", ctx).Return(users, nil)
	mockRepo.On("GetUsersNeedingRetention7dNotice", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersWithExpiredRetention", ctx).Return([]*domain.User{}, nil)

	mockClaims.On("Claim", ctx, repository.ResourceTypeUser, "user-ineligible-30d", repository.ActionRetentionNotice30d, retentionNoticeClaimLease).
		Return(testRetentionClaimToken, true, nil)
	mockRepo.On("IsRetentionNotice30dEligible", ctx, "user-ineligible-30d").Return(false, nil)
	mockClaims.On("Release", ctx, repository.ResourceTypeUser, "user-ineligible-30d", repository.ActionRetentionNotice30d, testRetentionClaimToken, "precondition lost since claim").
		Return(true, nil)

	worker := newTestRetentionWorker(mockRepo, mockDeletion, mockEmail)
	worker.SetWorkClaimsRepo(mockClaims)
	job := newTestRetentionCleanupJob(RetentionCleanupCheckArgs{TriggerSource: "periodic"})

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockClaims.AssertCalled(t, "Release", ctx, repository.ResourceTypeUser, "user-ineligible-30d", repository.ActionRetentionNotice30d, testRetentionClaimToken, "precondition lost since claim")
}

// TestRetentionCleanupCheck_30dSendFails_ReleasesClaim_NextRunRetries proves the M9 fix directly:
// a failed send must Release the claim (with the failure reason) rather than stamp the marker or
// Complete — and the NEXT daily run (a second worker.Work() call, since the candidate query would
// again return the still-unsent user) must retry the whole sequence. SetRetentionNotice30dSent
// and Complete are deliberately left unstubbed for the first attempt: an unstubbed call panics, a
// stronger proof than AssertNotCalled alone that a failed send never reaches either.
func TestRetentionCleanupCheck_30dSendFails_ReleasesClaim_NextRunRetries(t *testing.T) {
	ctx := context.Background()

	retentionExpiresAt := time.Now().Add(25 * 24 * time.Hour)
	users := []*domain.User{
		{ID: "user-retry-30d", Email: "retry30@example.com", RetentionExpiresAt: &retentionExpiresAt},
	}

	mockRepo := new(MockRetentionProfileRepository)
	mockDeletion := new(MockRetentionDeletionService)
	mockEmail := new(MockRetentionEmailService)
	mockClaims := new(mockWorkClaimsRepository)

	// The candidate query returns the SAME still-unsent user on both runs — proving the marker
	// really did stay NULL after the failed send (the repository-level integration test proves
	// the SQL side directly; this proves the WORKER's retry behavior end to end).
	mockRepo.On("GetUsersNeedingRetention30dNotice", ctx).Return(users, nil).Twice()
	mockRepo.On("GetUsersNeedingRetention7dNotice", ctx).Return([]*domain.User{}, nil).Twice()
	mockRepo.On("GetUsersWithExpiredRetention", ctx).Return([]*domain.User{}, nil).Twice()
	mockRepo.On("IsRetentionNotice30dEligible", ctx, "user-retry-30d").Return(true, nil).Twice()

	const token1 = "token-attempt-1"
	const token2 = "token-attempt-2"
	sendErr := errors.New("smtp: connection refused")

	// First run: claim wins, but the send fails.
	mockClaims.On("Claim", ctx, repository.ResourceTypeUser, "user-retry-30d", repository.ActionRetentionNotice30d, retentionNoticeClaimLease).
		Return(token1, true, nil).Once()
	mockEmail.On("SendRetentionNoticeEmail", ctx, "retry30@example.com", mock.Anything, mock.Anything).
		Return(sendErr).Once()
	mockClaims.On("Release", ctx, repository.ResourceTypeUser, "user-retry-30d", repository.ActionRetentionNotice30d, token1, sendErr.Error()).
		Return(true, nil).Once()

	// Second run ("the next day"): a fresh claim (Release made it immediately reclaimable), and
	// this time the send succeeds.
	mockClaims.On("Claim", ctx, repository.ResourceTypeUser, "user-retry-30d", repository.ActionRetentionNotice30d, retentionNoticeClaimLease).
		Return(token2, true, nil).Once()
	mockEmail.On("SendRetentionNoticeEmail", ctx, "retry30@example.com", mock.Anything, mock.Anything).
		Return(nil).Once()
	mockRepo.On("SetRetentionNotice30dSent", ctx, "user-retry-30d").Return(nil).Once()
	mockClaims.On("Complete", ctx, repository.ResourceTypeUser, "user-retry-30d", repository.ActionRetentionNotice30d, token2).
		Return(true, nil).Once()

	worker := newTestRetentionWorker(mockRepo, mockDeletion, mockEmail)
	worker.SetWorkClaimsRepo(mockClaims)
	job := newTestRetentionCleanupJob(RetentionCleanupCheckArgs{TriggerSource: "periodic"})

	err := worker.Work(ctx, job)
	require.NoError(t, err, "first run: a per-user send failure does not fail the overall job")

	err = worker.Work(ctx, job)
	require.NoError(t, err, "second run: succeeds after the first run released the claim and left the marker unset")

	mockClaims.AssertExpectations(t)
	mockRepo.AssertExpectations(t)
	mockEmail.AssertExpectations(t)
}

// TestRetentionCleanupCheck_7dSendFails_ReleasesClaim_NextRunRetries mirrors the 30-day retry
// proof for the 7-day notice — Issue #1413 M9's "both 30d and 7d independently" requirement: this
// exercises IsRetentionNotice7dEligible / SetRetentionNotice7dSent / ActionRetentionNotice7d, not
// merely the shared processRetentionNotices control flow already proven above.
func TestRetentionCleanupCheck_7dSendFails_ReleasesClaim_NextRunRetries(t *testing.T) {
	ctx := context.Background()

	retentionExpiresAt := time.Now().Add(5 * 24 * time.Hour)
	users := []*domain.User{
		{ID: "user-retry-7d", Email: "retry7@example.com", RetentionExpiresAt: &retentionExpiresAt},
	}

	mockRepo := new(MockRetentionProfileRepository)
	mockDeletion := new(MockRetentionDeletionService)
	mockEmail := new(MockRetentionEmailService)
	mockClaims := new(mockWorkClaimsRepository)

	mockRepo.On("GetUsersNeedingRetention30dNotice", ctx).Return([]*domain.User{}, nil).Twice()
	mockRepo.On("GetUsersNeedingRetention7dNotice", ctx).Return(users, nil).Twice()
	mockRepo.On("GetUsersWithExpiredRetention", ctx).Return([]*domain.User{}, nil).Twice()
	mockRepo.On("IsRetentionNotice7dEligible", ctx, "user-retry-7d").Return(true, nil).Twice()

	const token1 = "token-attempt-1-7d"
	const token2 = "token-attempt-2-7d"
	sendErr := errors.New("smtp: connection refused")

	mockClaims.On("Claim", ctx, repository.ResourceTypeUser, "user-retry-7d", repository.ActionRetentionNotice7d, retentionNoticeClaimLease).
		Return(token1, true, nil).Once()
	mockEmail.On("SendRetentionNoticeEmail", ctx, "retry7@example.com", mock.Anything, mock.Anything).
		Return(sendErr).Once()
	mockClaims.On("Release", ctx, repository.ResourceTypeUser, "user-retry-7d", repository.ActionRetentionNotice7d, token1, sendErr.Error()).
		Return(true, nil).Once()

	mockClaims.On("Claim", ctx, repository.ResourceTypeUser, "user-retry-7d", repository.ActionRetentionNotice7d, retentionNoticeClaimLease).
		Return(token2, true, nil).Once()
	mockEmail.On("SendRetentionNoticeEmail", ctx, "retry7@example.com", mock.Anything, mock.Anything).
		Return(nil).Once()
	mockRepo.On("SetRetentionNotice7dSent", ctx, "user-retry-7d").Return(nil).Once()
	mockClaims.On("Complete", ctx, repository.ResourceTypeUser, "user-retry-7d", repository.ActionRetentionNotice7d, token2).
		Return(true, nil).Once()

	worker := newTestRetentionWorker(mockRepo, mockDeletion, mockEmail)
	worker.SetWorkClaimsRepo(mockClaims)
	job := newTestRetentionCleanupJob(RetentionCleanupCheckArgs{TriggerSource: "periodic"})

	err := worker.Work(ctx, job)
	require.NoError(t, err, "first run: a per-user send failure does not fail the overall job")

	err = worker.Work(ctx, job)
	require.NoError(t, err, "second run: succeeds after the first run released the claim and left the marker unset")

	mockClaims.AssertExpectations(t)
	mockRepo.AssertExpectations(t)
	mockEmail.AssertExpectations(t)
}

// TestRetentionCleanupCheck_30dSendSucceeds_OrderingProof proves M9's core claim directly by
// recording the actual call order via .Run() hooks — an ordering regression (e.g. stamping the
// marker before sending, the original bug) wouldn't change WHETHER each mock is called, only
// WHEN, so plain AssertCalled assertions (already covered elsewhere) cannot catch it. Mirrors
// past_due_service_test.go's H6b ordering proof.
func TestRetentionCleanupCheck_30dSendSucceeds_OrderingProof(t *testing.T) {
	ctx := context.Background()

	retentionExpiresAt := time.Now().Add(25 * 24 * time.Hour)
	users := []*domain.User{
		{ID: "user-order-30d", Email: "order30@example.com", RetentionExpiresAt: &retentionExpiresAt},
	}

	mockRepo := new(MockRetentionProfileRepository)
	mockDeletion := new(MockRetentionDeletionService)
	mockEmail := new(MockRetentionEmailService)
	mockClaims := new(mockWorkClaimsRepository)

	mockRepo.On("GetUsersNeedingRetention30dNotice", ctx).Return(users, nil)
	mockRepo.On("GetUsersNeedingRetention7dNotice", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersWithExpiredRetention", ctx).Return([]*domain.User{}, nil)

	var callOrder []string
	mockClaims.On("Claim", ctx, repository.ResourceTypeUser, "user-order-30d", repository.ActionRetentionNotice30d, retentionNoticeClaimLease).
		Run(func(mock.Arguments) { callOrder = append(callOrder, "claim") }).
		Return(testRetentionClaimToken, true, nil)
	mockRepo.On("IsRetentionNotice30dEligible", ctx, "user-order-30d").
		Run(func(mock.Arguments) { callOrder = append(callOrder, "eligible") }).
		Return(true, nil)
	mockEmail.On("SendRetentionNoticeEmail", ctx, "order30@example.com", mock.Anything, mock.Anything).
		Run(func(mock.Arguments) { callOrder = append(callOrder, "send") }).
		Return(nil)
	mockRepo.On("SetRetentionNotice30dSent", ctx, "user-order-30d").
		Run(func(mock.Arguments) { callOrder = append(callOrder, "stamp") }).
		Return(nil)
	mockClaims.On("Complete", ctx, repository.ResourceTypeUser, "user-order-30d", repository.ActionRetentionNotice30d, testRetentionClaimToken).
		Run(func(mock.Arguments) { callOrder = append(callOrder, "complete") }).
		Return(true, nil)

	worker := newTestRetentionWorker(mockRepo, mockDeletion, mockEmail)
	worker.SetWorkClaimsRepo(mockClaims)
	job := newTestRetentionCleanupJob(RetentionCleanupCheckArgs{TriggerSource: "periodic"})

	err := worker.Work(ctx, job)
	require.NoError(t, err)

	require.Equal(t, []string{"claim", "eligible", "send", "stamp", "complete"}, callOrder,
		"M9: the *_sent_at marker must be stamped only AFTER a successful send, and Complete only after the stamp")
}

// TestRetentionCleanupCheck_30dCompleteReturnsFalse_LoggedAtError proves the lost-lease-at-
// Complete-time case (the fencing-token scenario — another worker reclaimed the tuple while this
// one was mid-flight) is logged at ERROR, not swallowed, per the #1413 ambiguity resolution: it
// means the notice may have gone out twice. Uses an injectable *slog.Logger (unlike
// DeletionService, this worker takes one via Deps) so the ERROR level is asserted directly rather
// than only inspected in source.
func TestRetentionCleanupCheck_30dCompleteReturnsFalse_LoggedAtError(t *testing.T) {
	ctx := context.Background()

	retentionExpiresAt := time.Now().Add(25 * 24 * time.Hour)
	users := []*domain.User{
		{ID: "user-lease-lost", Email: "leaselost@example.com", RetentionExpiresAt: &retentionExpiresAt},
	}

	mockRepo := new(MockRetentionProfileRepository)
	mockDeletion := new(MockRetentionDeletionService)
	mockEmail := new(MockRetentionEmailService)
	mockClaims := new(mockWorkClaimsRepository)

	mockRepo.On("GetUsersNeedingRetention30dNotice", ctx).Return(users, nil)
	mockRepo.On("GetUsersNeedingRetention7dNotice", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersWithExpiredRetention", ctx).Return([]*domain.User{}, nil)

	mockClaims.On("Claim", ctx, repository.ResourceTypeUser, "user-lease-lost", repository.ActionRetentionNotice30d, retentionNoticeClaimLease).
		Return(testRetentionClaimToken, true, nil)
	mockRepo.On("IsRetentionNotice30dEligible", ctx, "user-lease-lost").Return(true, nil)
	mockEmail.On("SendRetentionNoticeEmail", ctx, "leaselost@example.com", mock.Anything, mock.Anything).Return(nil)
	mockRepo.On("SetRetentionNotice30dSent", ctx, "user-lease-lost").Return(nil)
	// The lease was reclaimed by another worker before we could Complete it.
	mockClaims.On("Complete", ctx, repository.ResourceTypeUser, "user-lease-lost", repository.ActionRetentionNotice30d, testRetentionClaimToken).
		Return(false, nil)

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))

	worker := NewRetentionCleanupCheckWorker(RetentionCleanupCheckWorkerDeps{
		ProfileRepo:     mockRepo,
		DeletionService: mockDeletion,
		EmailService:    mockEmail,
		RetentionMonths: 6,
		Logger:          logger,
	})
	worker.SetWorkClaimsRepo(mockClaims)
	job := newTestRetentionCleanupJob(RetentionCleanupCheckArgs{TriggerSource: "periodic"})

	err := worker.Work(ctx, job)
	require.NoError(t, err, "the email genuinely sent; a lost lease at Complete time is not itself a job failure")

	logOutput := logBuf.String()
	assert.Contains(t, logOutput, "level=ERROR", "a lost lease at Complete time must be logged at ERROR, not swallowed")
	assert.Contains(t, logOutput, "user-lease-lost")
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

// --- Last-success heartbeat observability (Issue #1413 Task P4) ---

// TestRetentionCleanupCheck_Success_RecordsHeartbeat proves a successful run advances
// observability.JobLastSuccessTimestamp for this job's kind — the generic mechanism an alert
// reads to detect this job has gone silent, independent of (and stronger than)
// jobs_completed_total{status="failed"}, which never fires at all if the job stops running
// entirely rather than merely failing.
func TestRetentionCleanupCheck_Success_RecordsHeartbeat(t *testing.T) {
	ctx := context.Background()

	mockRepo := new(MockRetentionProfileRepository)
	mockDeletion := new(MockRetentionDeletionService)
	mockEmail := new(MockRetentionEmailService)

	mockRepo.On("GetUsersNeedingRetention30dNotice", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersNeedingRetention7dNotice", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersWithExpiredRetention", ctx).Return([]*domain.User{}, nil)

	worker := newTestRetentionWorker(mockRepo, mockDeletion, mockEmail)
	job := newTestRetentionCleanupJob(RetentionCleanupCheckArgs{TriggerSource: "periodic"})

	before := time.Now().Unix()
	err := worker.Work(ctx, job)
	require.NoError(t, err)

	got := testutil.ToFloat64(observability.JobLastSuccessTimestamp.WithLabelValues(job.Kind))
	assert.GreaterOrEqual(t, got, float64(before),
		"a successful run must advance the last-success heartbeat for this job's kind")
}

// TestRetentionCleanupCheck_DeletionFailure_HeartbeatNotAdvanced proves a run that returns an
// error (a partial deletion failure) does NOT advance the heartbeat — advancing it on failure
// would defeat the entire point of the metric — AND that the failure is independently visible via
// an ERROR-level log line (the pre-existing "failed to process expired retention deletions" log),
// not merely a silently-unmoved gauge.
func TestRetentionCleanupCheck_DeletionFailure_HeartbeatNotAdvanced(t *testing.T) {
	ctx := context.Background()

	retentionExpiresAt := time.Now().Add(-24 * time.Hour)
	users := []*domain.User{
		{ID: "user-fail-hb", Email: "fail-hb@example.com", RetentionExpiresAt: &retentionExpiresAt},
	}

	mockRepo := new(MockRetentionProfileRepository)
	mockDeletion := new(MockRetentionDeletionService)
	mockEmail := new(MockRetentionEmailService)

	mockRepo.On("GetUsersNeedingRetention30dNotice", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersNeedingRetention7dNotice", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersWithExpiredRetention", ctx).Return(users, nil)
	mockDeletion.On("ExecuteDeletion", ctx, "user-fail-hb").Return(errors.New("deletion failed"))

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))
	worker := NewRetentionCleanupCheckWorker(RetentionCleanupCheckWorkerDeps{
		ProfileRepo:     mockRepo,
		DeletionService: mockDeletion,
		EmailService:    mockEmail,
		RetentionMonths: 6,
		Logger:          logger,
	})
	job := newTestRetentionCleanupJob(RetentionCleanupCheckArgs{TriggerSource: "periodic"})

	before := testutil.ToFloat64(observability.JobLastSuccessTimestamp.WithLabelValues(job.Kind))
	err := worker.Work(ctx, job)
	require.Error(t, err)

	after := testutil.ToFloat64(observability.JobLastSuccessTimestamp.WithLabelValues(job.Kind))
	assert.Equal(t, before, after, "a failed run must NOT advance the last-success heartbeat")

	logOutput := logBuf.String()
	assert.Contains(t, logOutput, "level=ERROR", "a deletion failure must be logged at ERROR, not merely reflected in an unmoved gauge")
	assert.Contains(t, logOutput, "failed to process expired retention deletions")
}
