// Package jobs contains River job definitions and workers for async processing.
// Issue #930: Retention cleanup check job tests.
package jobs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/observability"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
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

// GetUsersNeedingRetentionClockRepair, RepairRetentionClock (Issue #1413 round-3 finding H5
// residual) back the Work() Step 0 repair pass added alongside the notice/deletion steps.
func (m *MockRetentionProfileRepository) GetUsersNeedingRetentionClockRepair(ctx context.Context) ([]*domain.User, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.User), args.Error(1)
}

func (m *MockRetentionProfileRepository) RepairRetentionClock(ctx context.Context, userID string, startedAt, expiresAt time.Time) (bool, error) {
	args := m.Called(ctx, userID, startedAt, expiresAt)
	return args.Bool(0), args.Error(1)
}

// GetUsersNeedingPlaidDisconnectRepair, GetUserByID (Issue #1413 round-7 grace-window repair
// path) back the Work() Step 0b repair pass added alongside the retention-clock repair (Step 0)
// and notice/deletion steps.
func (m *MockRetentionProfileRepository) GetUsersNeedingPlaidDisconnectRepair(ctx context.Context, gracePeriod time.Duration) ([]*domain.User, error) {
	args := m.Called(ctx, gracePeriod)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.User), args.Error(1)
}

func (m *MockRetentionProfileRepository) GetUserByID(ctx context.Context, userID string) (*domain.User, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.User), args.Error(1)
}

// ClearPlaidCleanupPendingIfMatches is the Issue #1413 round-11 Fix 3 occurrence-scoped
// marker-clear called once a repair actually completes successfully — replaces the round-10
// unconditional ClearPlaidCleanupPending.
func (m *MockRetentionProfileRepository) ClearPlaidCleanupPendingIfMatches(ctx context.Context, userID string, expected time.Time) (bool, error) {
	args := m.Called(ctx, userID, expected)
	return args.Bool(0), args.Error(1)
}

// IsRetentionNotice30dEligibleForOccurrence is IsRetentionNotice30dEligible's occurrence-scoped
// successor (Issue #1413 round-4 finding M1) — see RetentionProfileRepository's doc comment.
func (m *MockRetentionProfileRepository) IsRetentionNotice30dEligibleForOccurrence(ctx context.Context, userID string, expectedRetentionExpiresAt time.Time) (bool, error) {
	args := m.Called(ctx, userID, expectedRetentionExpiresAt)
	return args.Bool(0), args.Error(1)
}

// IsRetentionNotice7dEligibleForOccurrence is IsRetentionNotice30dEligibleForOccurrence's 7-day
// sibling.
func (m *MockRetentionProfileRepository) IsRetentionNotice7dEligibleForOccurrence(ctx context.Context, userID string, expectedRetentionExpiresAt time.Time) (bool, error) {
	args := m.Called(ctx, userID, expectedRetentionExpiresAt)
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

// MockRetentionPlaidCleanup mocks the RetentionPlaidCleanup interface (Issue #1413 round-7
// grace-window repair path).
type MockRetentionPlaidCleanup struct {
	mock.Mock
	// capturedRecheck, when non-nil (a test-provided pointer), receives the recheck callback
	// DisconnectAllUserItemsIfEligible was invoked with — lets a test call it directly to prove
	// repairPlaidDisconnects wired a REAL per-item eligibility check through (re-reads the user via
	// GetUserByID), not a no-op stand-in. See
	// TestRetentionCleanupCheck_PlaidRepair_RecheckWiredToGetUserByID.
	capturedRecheck *func(ctx context.Context) (bool, error)
}

func (m *MockRetentionPlaidCleanup) DisconnectAllUserItemsIfEligible(ctx context.Context, userID string, recheck func(ctx context.Context) (bool, error)) error {
	args := m.Called(ctx, userID, recheck)
	if m.capturedRecheck != nil {
		*m.capturedRecheck = recheck
	}
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

// newTestRetentionWorker constructs a worker for tests exercising the notice/deletion steps.
// repo must be a *MockRetentionProfileRepository (not just any RetentionProfileRepository) so
// this helper can register permissive defaults for GetUsersNeedingRetentionClockRepair (Issue
// #1413 round-3 finding H5 residual, the Step 0 repair pass) and GetUsersNeedingPlaidDisconnectRepair
// (Issue #1413 round-7 grace-window repair path, the Step 0b repair pass) — none of the existing
// notice/deletion tests care about either repair step, and without these defaults every one of them
// would panic on an unregistered mock call now that Work() invokes both unconditionally. Tests that
// DO want to exercise a repair step itself construct their own worker directly (see the repair-step
// tests below) rather than going through this helper, so there is no conflict between this catch-all
// and a test-specific expectation for the same method.
//
// Issue #1413 round-10 (Fix 1): repairPlaidDisconnects now treats a nil plaidCleanup as a hard
// configuration error (errPlaidCleanupNotConfigured) that fails the whole job — matching real
// production, where Plaid is expected to always be configured. So this helper also wires an inert
// MockRetentionPlaidCleanup via SetPlaidCleanup: the permissive empty-candidate-list default above
// means DisconnectAllUserItemsIfEligible is never actually invoked for any test using this helper,
// but the worker must see a NON-nil collaborator or every one of the 20 notice/deletion tests below
// would now fail on that account alone, despite not caring about Plaid-repair mechanics at all.
func newTestRetentionWorker(repo *MockRetentionProfileRepository, deletionSvc RetentionDeletionService, emailSvc RetentionEmailService) *RetentionCleanupCheckWorker {
	repo.On("GetUsersNeedingRetentionClockRepair", mock.Anything).Return([]*domain.User{}, nil).Maybe()
	repo.On("GetUsersNeedingPlaidDisconnectRepair", mock.Anything, mock.Anything).Return([]*domain.User{}, nil).Maybe()
	repo.On("ClearPlaidCleanupPendingIfMatches", mock.Anything, mock.Anything, mock.Anything).Return(true, nil).Maybe()
	worker := NewRetentionCleanupCheckWorker(RetentionCleanupCheckWorkerDeps{
		ProfileRepo:     repo,
		DeletionService: deletionSvc,
		EmailService:    emailSvc,
		RetentionMonths: 6,
		Logger:          slog.Default(),
	})
	worker.SetPlaidCleanup(new(MockRetentionPlaidCleanup))
	return worker
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

// retentionOccurrenceResourceID computes the composite work_claims.resource_id expected for a
// retention-cycle occurrence (Issue #1413 P5) — mirrors production's
// repository.OccurrenceResourceID(userID, repository.FormatOccurrenceTimestamp(user.RetentionExpiresAt)),
// which processRetentionNotices now uses instead of the bare userID, so that a re-armed retention
// cycle (resubscribe then cancel again) is treated as a NEW occurrence rather than colliding with
// an already-completed notice from the FIRST cycle.
func retentionOccurrenceResourceID(t *testing.T, userID string, retentionExpiresAt time.Time) string {
	t.Helper()
	resourceID, err := repository.OccurrenceResourceID(userID, repository.FormatOccurrenceTimestamp(&retentionExpiresAt))
	require.NoError(t, err)
	return resourceID
}

// --- Kind ---

func TestRetentionCleanupCheckArgs_Kind(t *testing.T) {
	args := RetentionCleanupCheckArgs{}
	assert.Equal(t, "retention_cleanup_check", args.Kind())
}

// --- Retention clock repair tests (Issue #1413 round-3 finding H5 residual) ---

// TestRetentionCleanupCheck_RepairsRetentionClock proves the positive direction of the Step 0
// repair pass: a candidate returned by GetUsersNeedingRetentionClockRepair is repaired via
// RepairRetentionClock, and the job still succeeds.
func TestRetentionCleanupCheck_RepairsRetentionClock(t *testing.T) {
	ctx := context.Background()

	users := []*domain.User{
		{ID: "user-needs-repair", Email: "needsrepair@example.com", SubscriptionStatus: "cancelled"},
	}

	mockRepo := new(MockRetentionProfileRepository)
	mockDeletion := new(MockRetentionDeletionService)
	mockEmail := new(MockRetentionEmailService)

	mockRepo.On("GetUsersNeedingRetentionClockRepair", ctx).Return(users, nil)
	mockRepo.On("RepairRetentionClock", ctx, "user-needs-repair", mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time")).Return(true, nil)
	mockRepo.On("GetUsersNeedingPlaidDisconnectRepair", ctx, plaidDisconnectRepairGracePeriod).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersNeedingRetention30dNotice", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersNeedingRetention7dNotice", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersWithExpiredRetention", ctx).Return([]*domain.User{}, nil)

	worker := NewRetentionCleanupCheckWorker(RetentionCleanupCheckWorkerDeps{
		ProfileRepo:     mockRepo,
		DeletionService: mockDeletion,
		EmailService:    mockEmail,
		RetentionMonths: 6,
		Logger:          slog.Default(),
	})
	worker.SetPlaidCleanup(new(MockRetentionPlaidCleanup))
	job := newTestRetentionCleanupJob(RetentionCleanupCheckArgs{TriggerSource: "periodic"})

	err := worker.Work(ctx, job)
	require.NoError(t, err)

	mockRepo.AssertCalled(t, "RepairRetentionClock", ctx, "user-needs-repair", mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time"))
}

// TestRetentionCleanupCheck_RepairSkipsCompedUser proves the belt-and-braces comp guard: even
// though GetUsersNeedingRetentionClockRepair already excludes any override (live or lapsed) at
// the SQL level, a comped user surfaced by a stale candidate list must never have
// RepairRetentionClock called for them.
func TestRetentionCleanupCheck_RepairSkipsCompedUser(t *testing.T) {
	ctx := context.Background()

	override := "household_2"
	overrideEndsAt := time.Now().Add(24 * time.Hour)
	users := []*domain.User{
		{ID: "user-comped-repair", Email: "compedrepair@example.com", SubscriptionStatus: "cancelled",
			ManualTierOverride: &override, ManualTierEndsAt: &overrideEndsAt},
	}

	mockRepo := new(MockRetentionProfileRepository)
	mockDeletion := new(MockRetentionDeletionService)
	mockEmail := new(MockRetentionEmailService)

	mockRepo.On("GetUsersNeedingRetentionClockRepair", ctx).Return(users, nil)
	mockRepo.On("GetUsersNeedingPlaidDisconnectRepair", ctx, plaidDisconnectRepairGracePeriod).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersNeedingRetention30dNotice", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersNeedingRetention7dNotice", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersWithExpiredRetention", ctx).Return([]*domain.User{}, nil)
	// RepairRetentionClock is deliberately left unstubbed: an unexpected call panics, proving the
	// comp guard runs BEFORE the write, not merely alongside it.

	worker := NewRetentionCleanupCheckWorker(RetentionCleanupCheckWorkerDeps{
		ProfileRepo:     mockRepo,
		DeletionService: mockDeletion,
		EmailService:    mockEmail,
		RetentionMonths: 6,
		Logger:          slog.Default(),
	})
	worker.SetPlaidCleanup(new(MockRetentionPlaidCleanup))
	job := newTestRetentionCleanupJob(RetentionCleanupCheckArgs{TriggerSource: "periodic"})

	err := worker.Work(ctx, job)
	require.NoError(t, err)

	mockRepo.AssertNotCalled(t, "RepairRetentionClock", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

// TestRetentionCleanupCheck_RepairFailure_NonFatal_ContinuesToNoticesAndDeletion proves the
// repair step is non-fatal, like steps 1/2: a repair failure must not prevent the notice and
// deletion steps from still running in the SAME pass.
func TestRetentionCleanupCheck_RepairFailure_NonFatal_ContinuesToNoticesAndDeletion(t *testing.T) {
	ctx := context.Background()

	retentionExpiresAt := time.Now().Add(-24 * time.Hour)
	repairUsers := []*domain.User{
		{ID: "user-repair-fail", Email: "repairfail@example.com", SubscriptionStatus: "cancelled"},
	}
	expiredUsers := []*domain.User{
		{ID: "user-still-deleted", Email: "stilldeleted@example.com", RetentionExpiresAt: &retentionExpiresAt},
	}

	mockRepo := new(MockRetentionProfileRepository)
	mockDeletion := new(MockRetentionDeletionService)
	mockEmail := new(MockRetentionEmailService)

	mockRepo.On("GetUsersNeedingRetentionClockRepair", ctx).Return(repairUsers, nil)
	mockRepo.On("RepairRetentionClock", ctx, "user-repair-fail", mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time")).
		Return(false, errors.New("db unavailable"))
	mockRepo.On("GetUsersNeedingPlaidDisconnectRepair", ctx, plaidDisconnectRepairGracePeriod).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersNeedingRetention30dNotice", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersNeedingRetention7dNotice", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersWithExpiredRetention", ctx).Return(expiredUsers, nil)
	mockDeletion.On("ExecuteDeletion", ctx, "user-still-deleted").Return(nil)

	worker := NewRetentionCleanupCheckWorker(RetentionCleanupCheckWorkerDeps{
		ProfileRepo:     mockRepo,
		DeletionService: mockDeletion,
		EmailService:    mockEmail,
		RetentionMonths: 6,
		Logger:          slog.Default(),
	})
	worker.SetPlaidCleanup(new(MockRetentionPlaidCleanup))
	job := newTestRetentionCleanupJob(RetentionCleanupCheckArgs{TriggerSource: "periodic"})

	err := worker.Work(ctx, job)
	require.NoError(t, err, "a repair failure must not fail the whole job — deletion still ran and succeeded")

	mockDeletion.AssertCalled(t, "ExecuteDeletion", ctx, "user-still-deleted")
}

// TestRetentionCleanupCheck_RepairSkip_PreconditionLostSinceQuery proves RepairRetentionClock
// returning (false, nil) — e.g. a concurrent run already repaired this user, or their status/clock
// changed since the candidate query ran — is treated as a legitimate skip, not an error.
func TestRetentionCleanupCheck_RepairSkip_PreconditionLostSinceQuery(t *testing.T) {
	ctx := context.Background()

	users := []*domain.User{
		{ID: "user-repair-raced", Email: "repairraced@example.com", SubscriptionStatus: "cancelled"},
	}

	mockRepo := new(MockRetentionProfileRepository)
	mockDeletion := new(MockRetentionDeletionService)
	mockEmail := new(MockRetentionEmailService)

	mockRepo.On("GetUsersNeedingRetentionClockRepair", ctx).Return(users, nil)
	mockRepo.On("RepairRetentionClock", ctx, "user-repair-raced", mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time")).Return(false, nil)
	mockRepo.On("GetUsersNeedingPlaidDisconnectRepair", ctx, plaidDisconnectRepairGracePeriod).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersNeedingRetention30dNotice", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersNeedingRetention7dNotice", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersWithExpiredRetention", ctx).Return([]*domain.User{}, nil)

	worker := NewRetentionCleanupCheckWorker(RetentionCleanupCheckWorkerDeps{
		ProfileRepo:     mockRepo,
		DeletionService: mockDeletion,
		EmailService:    mockEmail,
		RetentionMonths: 6,
		Logger:          slog.Default(),
	})
	worker.SetPlaidCleanup(new(MockRetentionPlaidCleanup))
	job := newTestRetentionCleanupJob(RetentionCleanupCheckArgs{TriggerSource: "periodic"})

	err := worker.Work(ctx, job)
	assert.NoError(t, err, "a lost repair precondition must not be reported as a job failure")
}

// ─── Step 0b: Plaid-disconnect grace-window repair pass (Issue #1413 round-7) ────

// TestRetentionCleanupCheck_RepairsPlaidDisconnect proves the positive direction: a candidate
// returned by GetUsersNeedingPlaidDisconnectRepair, still read-only-status and uncomped on
// re-read, has DisconnectAllUserItemsIfEligible called for them, and the job still succeeds.
func TestRetentionCleanupCheck_RepairsPlaidDisconnect(t *testing.T) {
	ctx := context.Background()

	pendingSince := time.Now().Add(-100 * time.Hour)
	user := &domain.User{ID: "user-needs-plaid-repair", Email: "needsplaidrepair@example.com", SubscriptionStatus: "inactive", PlaidCleanupPendingAt: &pendingSince}
	users := []*domain.User{user}

	mockRepo := new(MockRetentionProfileRepository)
	mockDeletion := new(MockRetentionDeletionService)
	mockEmail := new(MockRetentionEmailService)
	mockPlaid := new(MockRetentionPlaidCleanup)

	mockRepo.On("GetUsersNeedingRetentionClockRepair", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersNeedingPlaidDisconnectRepair", ctx, plaidDisconnectRepairGracePeriod).Return(users, nil)
	mockRepo.On("GetUserByID", ctx, "user-needs-plaid-repair").Return(user, nil)
	mockPlaid.On("DisconnectAllUserItemsIfEligible", ctx, "user-needs-plaid-repair", mock.Anything).Return(nil)
	mockRepo.On("ClearPlaidCleanupPendingIfMatches", ctx, "user-needs-plaid-repair", pendingSince).Return(true, nil)
	mockRepo.On("GetUsersNeedingRetention30dNotice", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersNeedingRetention7dNotice", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersWithExpiredRetention", ctx).Return([]*domain.User{}, nil)

	worker := NewRetentionCleanupCheckWorker(RetentionCleanupCheckWorkerDeps{
		ProfileRepo:     mockRepo,
		DeletionService: mockDeletion,
		EmailService:    mockEmail,
		RetentionMonths: 6,
		Logger:          slog.Default(),
	})
	worker.SetPlaidCleanup(mockPlaid)
	job := newTestRetentionCleanupJob(RetentionCleanupCheckArgs{TriggerSource: "periodic"})

	err := worker.Work(ctx, job)
	require.NoError(t, err)

	mockPlaid.AssertCalled(t, "DisconnectAllUserItemsIfEligible", ctx, "user-needs-plaid-repair", mock.Anything)
	mockRepo.AssertCalled(t, "ClearPlaidCleanupPendingIfMatches", ctx, "user-needs-plaid-repair", pendingSince)
}

// TestRetentionCleanupCheck_PlaidRepair_QueriesWithConfiguredGracePeriod proves
// repairPlaidDisconnects passes the round-7 grace constant through to the query, not an
// unconstrained/zero value — the grace window is the entire mechanism that turns "resubscribed
// before disconnect" into a non-event.
func TestRetentionCleanupCheck_PlaidRepair_QueriesWithConfiguredGracePeriod(t *testing.T) {
	ctx := context.Background()

	mockRepo := new(MockRetentionProfileRepository)
	mockDeletion := new(MockRetentionDeletionService)
	mockEmail := new(MockRetentionEmailService)

	mockRepo.On("GetUsersNeedingRetentionClockRepair", ctx).Return([]*domain.User{}, nil)
	// Exact-match on the constant (not mock.Anything) — an unregistered call with any other value
	// panics, proving the worker doesn't invent its own grace duration.
	mockRepo.On("GetUsersNeedingPlaidDisconnectRepair", ctx, plaidDisconnectRepairGracePeriod).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersNeedingRetention30dNotice", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersNeedingRetention7dNotice", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersWithExpiredRetention", ctx).Return([]*domain.User{}, nil)

	worker := NewRetentionCleanupCheckWorker(RetentionCleanupCheckWorkerDeps{
		ProfileRepo:     mockRepo,
		DeletionService: mockDeletion,
		EmailService:    mockEmail,
		RetentionMonths: 6,
		Logger:          slog.Default(),
	})
	worker.SetPlaidCleanup(new(MockRetentionPlaidCleanup))
	job := newTestRetentionCleanupJob(RetentionCleanupCheckArgs{TriggerSource: "periodic"})

	err := worker.Work(ctx, job)
	require.NoError(t, err)

	mockRepo.AssertCalled(t, "GetUsersNeedingPlaidDisconnectRepair", ctx, plaidDisconnectRepairGracePeriod)
}

// TestRetentionCleanupCheck_PlaidRepair_RecheckWiredToGetUserByID proves the recheck callback
// passed to DisconnectAllUserItemsIfEligible is a REAL per-item eligibility check — not a
// pass-through no-op — by invoking the captured callback directly and confirming it re-reads the
// user via GetUserByID and reflects a status change. This is the round-7 "re-verify per item, not
// once per user" fix: without it, DisconnectAllUserItemsIfEligible's recheck parameter could be
// wired to a function that always returns (true, nil), silently defeating the per-item guard while
// every other assertion in this file still passes.
func TestRetentionCleanupCheck_PlaidRepair_RecheckWiredToGetUserByID(t *testing.T) {
	ctx := context.Background()

	pendingSince := time.Now().Add(-100 * time.Hour)
	candidate := &domain.User{ID: "user-recheck-wiring", Email: "recheckwiring@example.com", SubscriptionStatus: "inactive", PlaidCleanupPendingAt: &pendingSince}
	users := []*domain.User{candidate}

	mockRepo := new(MockRetentionProfileRepository)
	mockDeletion := new(MockRetentionDeletionService)
	mockEmail := new(MockRetentionEmailService)
	var capturedRecheck func(context.Context) (bool, error)
	mockPlaid := &MockRetentionPlaidCleanup{capturedRecheck: &capturedRecheck}

	mockRepo.On("GetUsersNeedingRetentionClockRepair", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersNeedingPlaidDisconnectRepair", ctx, plaidDisconnectRepairGracePeriod).Return(users, nil)
	// First re-read (the frontline pre-filter check): still read-only. Second re-read (this test
	// invoking the captured recheck callback directly, simulating a resubscribe that happened
	// between item 1 and item 2 of a real disconnect loop): now active.
	mockRepo.On("GetUserByID", ctx, "user-recheck-wiring").Return(candidate, nil).Once()
	activeNow := &domain.User{ID: "user-recheck-wiring", Email: "recheckwiring@example.com", SubscriptionStatus: "active"}
	mockRepo.On("GetUserByID", ctx, "user-recheck-wiring").Return(activeNow, nil).Once()
	mockPlaid.On("DisconnectAllUserItemsIfEligible", ctx, "user-recheck-wiring", mock.Anything).Return(nil)
	mockRepo.On("ClearPlaidCleanupPendingIfMatches", ctx, "user-recheck-wiring", pendingSince).Return(true, nil)
	mockRepo.On("GetUsersNeedingRetention30dNotice", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersNeedingRetention7dNotice", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersWithExpiredRetention", ctx).Return([]*domain.User{}, nil)

	worker := NewRetentionCleanupCheckWorker(RetentionCleanupCheckWorkerDeps{
		ProfileRepo:     mockRepo,
		DeletionService: mockDeletion,
		EmailService:    mockEmail,
		RetentionMonths: 6,
		Logger:          slog.Default(),
	})
	worker.SetPlaidCleanup(mockPlaid)
	job := newTestRetentionCleanupJob(RetentionCleanupCheckArgs{TriggerSource: "periodic"})

	err := worker.Work(ctx, job)
	require.NoError(t, err)

	require.NotNil(t, capturedRecheck, "DisconnectAllUserItemsIfEligible must have been called with a non-nil recheck")
	eligible, err := capturedRecheck(ctx)
	require.NoError(t, err)
	assert.False(t, eligible, "the captured recheck must re-read the user and report ineligible once they're no longer read-only-status")
	mockRepo.AssertNumberOfCalls(t, "GetUserByID", 2)
}

// TestRetentionCleanupCheck_PlaidRepairSkipsCompedUser proves the belt-and-braces comp guard: even
// though GetUsersNeedingPlaidDisconnectRepair already excludes a currently-ACTIVE override at the
// SQL level (noActiveManualOverridePredicate), a comped user surfaced by a stale candidate list
// must never have DisconnectAllUserItemsIfEligible called for them. Unlike the retention/deletion
// queries — which exclude ANY override, live or lapsed, via noManualOverrideAtAllPredicate,
// because deletion is irreversible and must fail safe by staying excluded — this Plaid-repair
// query and this belt-and-braces guard deliberately use the live-only rule: a Plaid disconnect is
// recoverable (the user relinks), so a LAPSED override must not permanently exempt a user here.
// See TestRetentionCleanupCheck_PlaidRepairProceedsForLapsedComp immediately below, which proves
// exactly that lapsed-override case proceeds rather than being skipped.
func TestRetentionCleanupCheck_PlaidRepairSkipsCompedUser(t *testing.T) {
	ctx := context.Background()

	override := "household_2"
	overrideEndsAt := time.Now().Add(24 * time.Hour)
	users := []*domain.User{
		{ID: "user-comped-plaid-repair", Email: "compedplaidrepair@example.com", SubscriptionStatus: "cancelled",
			ManualTierOverride: &override, ManualTierEndsAt: &overrideEndsAt},
	}

	mockRepo := new(MockRetentionProfileRepository)
	mockDeletion := new(MockRetentionDeletionService)
	mockEmail := new(MockRetentionEmailService)
	mockPlaid := new(MockRetentionPlaidCleanup)

	mockRepo.On("GetUsersNeedingRetentionClockRepair", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersNeedingPlaidDisconnectRepair", ctx, plaidDisconnectRepairGracePeriod).Return(users, nil)
	mockRepo.On("GetUsersNeedingRetention30dNotice", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersNeedingRetention7dNotice", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersWithExpiredRetention", ctx).Return([]*domain.User{}, nil)
	// GetUserByID / DisconnectAllUserItemsIfEligible are deliberately left unstubbed: an
	// unexpected call panics, proving the comp guard runs BEFORE even the re-read, not merely
	// alongside it.

	worker := NewRetentionCleanupCheckWorker(RetentionCleanupCheckWorkerDeps{
		ProfileRepo:     mockRepo,
		DeletionService: mockDeletion,
		EmailService:    mockEmail,
		RetentionMonths: 6,
		Logger:          slog.Default(),
	})
	worker.SetPlaidCleanup(mockPlaid)
	job := newTestRetentionCleanupJob(RetentionCleanupCheckArgs{TriggerSource: "periodic"})

	err := worker.Work(ctx, job)
	require.NoError(t, err)

	mockPlaid.AssertNotCalled(t, "DisconnectAllUserItemsIfEligible", mock.Anything, mock.Anything, mock.Anything)
}

// TestRetentionCleanupCheck_PlaidRepairProceedsForLapsedComp is the Issue #1413 round-12 Fix 2 RED
// test at the worker's belt-and-braces layer: a candidate surfaced with a LAPSED (expired, never
// cleared) manual_tier_override must still proceed to DisconnectAllUserItemsIfEligible — unlike
// TestRetentionCleanupCheck_PlaidRepairSkipsCompedUser's LIVE override, which must still skip. A
// Plaid disconnect is recoverable (the user relinks), so this belt-and-braces guard must use
// HasActiveManualOverride() (live-only), not HasAnyManualOverride() (live-or-lapsed) — the same
// choice GetUsersNeedingPlaidDisconnectRepair's SQL predicate now makes.
//
// Falsifiability: reverting the guard to HasAnyManualOverride() makes this test fail —
// DisconnectAllUserItemsIfEligible would never be called for this candidate.
func TestRetentionCleanupCheck_PlaidRepairProceedsForLapsedComp(t *testing.T) {
	ctx := context.Background()

	pendingSince := time.Now().Add(-100 * time.Hour)
	lapsedOverride := "household_2"
	lapsedEndsAt := time.Now().Add(-24 * time.Hour) // lapsed, not cleared
	user := &domain.User{
		ID: "user-lapsed-comp-plaid-repair", Email: "lapsedcompplaidrepair@example.com",
		SubscriptionStatus: "cancelled", PlaidCleanupPendingAt: &pendingSince,
		ManualTierOverride: &lapsedOverride, ManualTierEndsAt: &lapsedEndsAt,
	}
	users := []*domain.User{user}

	mockRepo := new(MockRetentionProfileRepository)
	mockDeletion := new(MockRetentionDeletionService)
	mockEmail := new(MockRetentionEmailService)
	mockPlaid := new(MockRetentionPlaidCleanup)

	mockRepo.On("GetUsersNeedingRetentionClockRepair", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersNeedingPlaidDisconnectRepair", ctx, plaidDisconnectRepairGracePeriod).Return(users, nil)
	mockRepo.On("GetUserByID", ctx, "user-lapsed-comp-plaid-repair").Return(user, nil)
	mockPlaid.On("DisconnectAllUserItemsIfEligible", ctx, "user-lapsed-comp-plaid-repair", mock.Anything).Return(nil)
	mockRepo.On("ClearPlaidCleanupPendingIfMatches", ctx, "user-lapsed-comp-plaid-repair", pendingSince).Return(true, nil)
	mockRepo.On("GetUsersNeedingRetention30dNotice", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersNeedingRetention7dNotice", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersWithExpiredRetention", ctx).Return([]*domain.User{}, nil)

	worker := NewRetentionCleanupCheckWorker(RetentionCleanupCheckWorkerDeps{
		ProfileRepo:     mockRepo,
		DeletionService: mockDeletion,
		EmailService:    mockEmail,
		RetentionMonths: 6,
		Logger:          slog.Default(),
	})
	worker.SetPlaidCleanup(mockPlaid)
	job := newTestRetentionCleanupJob(RetentionCleanupCheckArgs{TriggerSource: "periodic"})

	err := worker.Work(ctx, job)
	require.NoError(t, err)

	mockPlaid.AssertCalled(t, "DisconnectAllUserItemsIfEligible", ctx, "user-lapsed-comp-plaid-repair", mock.Anything)
}

// TestRetentionCleanupCheck_PlaidRepairSkipsIfResubscribedSinceQuery proves the re-read guard:
// unlike RepairRetentionClock's conditional UPDATE (which gets this for free from its own WHERE
// clause), DisconnectAllUserItemsIfEligible is an external Plaid call with no atomic DB guard — a
// stale candidate who resubscribed (no longer read-only-status) since
// GetUsersNeedingPlaidDisconnectRepair ran must be caught by the fresh GetUserByID re-read, or
// this repair pass would reproduce the exact harm it exists to fix (disconnecting a
// genuinely-active user's live Plaid items) via a different code path.
func TestRetentionCleanupCheck_PlaidRepairSkipsIfResubscribedSinceQuery(t *testing.T) {
	ctx := context.Background()

	stalePendingSince := time.Now().Add(-100 * time.Hour)
	staleUser := &domain.User{ID: "user-resubscribed", Email: "resubscribed@example.com", SubscriptionStatus: "inactive", PlaidCleanupPendingAt: &stalePendingSince}
	users := []*domain.User{staleUser}
	currentUser := &domain.User{ID: "user-resubscribed", Email: "resubscribed@example.com", SubscriptionStatus: "active"}

	mockRepo := new(MockRetentionProfileRepository)
	mockDeletion := new(MockRetentionDeletionService)
	mockEmail := new(MockRetentionEmailService)
	mockPlaid := new(MockRetentionPlaidCleanup)

	mockRepo.On("GetUsersNeedingRetentionClockRepair", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersNeedingPlaidDisconnectRepair", ctx, plaidDisconnectRepairGracePeriod).Return(users, nil)
	mockRepo.On("GetUserByID", ctx, "user-resubscribed").Return(currentUser, nil)
	mockRepo.On("GetUsersNeedingRetention30dNotice", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersNeedingRetention7dNotice", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersWithExpiredRetention", ctx).Return([]*domain.User{}, nil)
	// DisconnectAllUserItemsIfEligible is deliberately left unstubbed: an unexpected call panics.

	worker := NewRetentionCleanupCheckWorker(RetentionCleanupCheckWorkerDeps{
		ProfileRepo:     mockRepo,
		DeletionService: mockDeletion,
		EmailService:    mockEmail,
		RetentionMonths: 6,
		Logger:          slog.Default(),
	})
	worker.SetPlaidCleanup(mockPlaid)
	job := newTestRetentionCleanupJob(RetentionCleanupCheckArgs{TriggerSource: "periodic"})

	err := worker.Work(ctx, job)
	require.NoError(t, err)

	mockPlaid.AssertNotCalled(t, "DisconnectAllUserItemsIfEligible", mock.Anything, mock.Anything, mock.Anything)
}

// TestRetentionCleanupCheck_PlaidRepairFailure_NonFatal_ContinuesToNoticesAndDeletion proves the
// Plaid-disconnect repair step is non-fatal, like the retention-clock repair and notice/deletion
// steps: a repair failure must not prevent the notice and deletion steps from still running in the
// SAME pass.
func TestRetentionCleanupCheck_PlaidRepairFailure_NonFatal_ContinuesToNoticesAndDeletion(t *testing.T) {
	ctx := context.Background()

	pendingSince := time.Now().Add(-100 * time.Hour)
	repairUser := &domain.User{ID: "user-plaid-repair-fail", Email: "plaidrepairfail@example.com", SubscriptionStatus: "inactive", PlaidCleanupPendingAt: &pendingSince}
	retentionExpiresAt := time.Now().Add(-24 * time.Hour)
	expiredUsers := []*domain.User{
		{ID: "user-still-deleted-2", Email: "stilldeleted2@example.com", RetentionExpiresAt: &retentionExpiresAt},
	}

	mockRepo := new(MockRetentionProfileRepository)
	mockDeletion := new(MockRetentionDeletionService)
	mockEmail := new(MockRetentionEmailService)
	mockPlaid := new(MockRetentionPlaidCleanup)

	mockRepo.On("GetUsersNeedingRetentionClockRepair", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersNeedingPlaidDisconnectRepair", ctx, plaidDisconnectRepairGracePeriod).Return([]*domain.User{repairUser}, nil)
	mockRepo.On("GetUserByID", ctx, "user-plaid-repair-fail").Return(repairUser, nil)
	mockPlaid.On("DisconnectAllUserItemsIfEligible", ctx, "user-plaid-repair-fail", mock.Anything).Return(errors.New("plaid: transient 500"))
	mockRepo.On("GetUsersNeedingRetention30dNotice", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersNeedingRetention7dNotice", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersWithExpiredRetention", ctx).Return(expiredUsers, nil)
	mockDeletion.On("ExecuteDeletion", ctx, "user-still-deleted-2").Return(nil)

	worker := NewRetentionCleanupCheckWorker(RetentionCleanupCheckWorkerDeps{
		ProfileRepo:     mockRepo,
		DeletionService: mockDeletion,
		EmailService:    mockEmail,
		RetentionMonths: 6,
		Logger:          slog.Default(),
	})
	worker.SetPlaidCleanup(mockPlaid)
	job := newTestRetentionCleanupJob(RetentionCleanupCheckArgs{TriggerSource: "periodic"})

	err := worker.Work(ctx, job)
	require.NoError(t, err, "a Plaid-disconnect repair failure must not fail the whole job — deletion still ran and succeeded")

	mockDeletion.AssertCalled(t, "ExecuteDeletion", ctx, "user-still-deleted-2")
}

// ─── Issue #1413: per-user Plaid-disconnect-repair failure visibility ──
//
// repairPlaidDisconnects's per-user error isolation (errorCount++; continue, proven non-fatal by
// the test above) is deliberate — one user's Plaid failure must never block the GDPR notice/
// deletion steps the same run performs for everyone else. But before this metric, that isolation
// was also completely SILENT: nothing outside a log line distinguished "one user failed once" from
// "the same user has failed every run for weeks". observability.PlaidDisconnectRepairUserFailuresTotal
// is the counter that closes that gap — these tests prove it increments on exactly the failure
// paths inside the per-user loop (excluding the benign ErrSideEffectClaimBusy contention case,
// which is expected concurrent-worker contention, not a failure) and stays flat on success.

// TestRetentionCleanupCheck_PlaidRepairFailure_IncrementsUserFailureCounter proves
// DisconnectAllUserItemsIfEligible failing for one user increments the failure-visibility counter.
//
// Falsifiability: removing the Inc() call at the disconnect-failure site in repairPlaidDisconnects
// makes this test fail (counter stays flat despite a real per-user failure).
func TestRetentionCleanupCheck_PlaidRepairFailure_IncrementsUserFailureCounter(t *testing.T) {
	ctx := context.Background()

	pendingSince := time.Now().Add(-100 * time.Hour)
	repairUser := &domain.User{ID: "user-plaid-repair-fail-counter", Email: "plaidrepairfailcounter@example.com", SubscriptionStatus: "inactive", PlaidCleanupPendingAt: &pendingSince}

	mockRepo := new(MockRetentionProfileRepository)
	mockDeletion := new(MockRetentionDeletionService)
	mockEmail := new(MockRetentionEmailService)
	mockPlaid := new(MockRetentionPlaidCleanup)

	mockRepo.On("GetUsersNeedingRetentionClockRepair", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersNeedingPlaidDisconnectRepair", ctx, plaidDisconnectRepairGracePeriod).Return([]*domain.User{repairUser}, nil)
	mockRepo.On("GetUserByID", ctx, "user-plaid-repair-fail-counter").Return(repairUser, nil)
	mockPlaid.On("DisconnectAllUserItemsIfEligible", ctx, "user-plaid-repair-fail-counter", mock.Anything).Return(errors.New("plaid: transient 500"))
	mockRepo.On("GetUsersNeedingRetention30dNotice", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersNeedingRetention7dNotice", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersWithExpiredRetention", ctx).Return([]*domain.User{}, nil)

	worker := NewRetentionCleanupCheckWorker(RetentionCleanupCheckWorkerDeps{
		ProfileRepo:     mockRepo,
		DeletionService: mockDeletion,
		EmailService:    mockEmail,
		RetentionMonths: 6,
		Logger:          slog.Default(),
	})
	worker.SetPlaidCleanup(mockPlaid)
	job := newTestRetentionCleanupJob(RetentionCleanupCheckArgs{TriggerSource: "periodic"})

	before := testutil.ToFloat64(observability.PlaidDisconnectRepairUserFailuresTotal)
	err := worker.Work(ctx, job)
	require.NoError(t, err, "the deliberate per-user error isolation must still hold — the job succeeds even though the counter recorded a failure")

	after := testutil.ToFloat64(observability.PlaidDisconnectRepairUserFailuresTotal)
	assert.Equal(t, before+1, after, "a per-user Plaid disconnect failure must increment the failure-visibility counter")
}

// TestRetentionCleanupCheck_PlaidRepairPreCheckFailure_IncrementsUserFailureCounter proves the
// OTHER per-user failure site — the pre-disconnect isStillEligible re-read (GetUserByID) erroring
// before DisconnectAllUserItemsIfEligible is ever called — also increments the same counter.
func TestRetentionCleanupCheck_PlaidRepairPreCheckFailure_IncrementsUserFailureCounter(t *testing.T) {
	ctx := context.Background()

	pendingSince := time.Now().Add(-100 * time.Hour)
	repairUser := &domain.User{ID: "user-plaid-precheck-fail", Email: "plaidprecheckfail@example.com", SubscriptionStatus: "inactive", PlaidCleanupPendingAt: &pendingSince}

	mockRepo := new(MockRetentionProfileRepository)
	mockDeletion := new(MockRetentionDeletionService)
	mockEmail := new(MockRetentionEmailService)
	mockPlaid := new(MockRetentionPlaidCleanup)

	mockRepo.On("GetUsersNeedingRetentionClockRepair", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersNeedingPlaidDisconnectRepair", ctx, plaidDisconnectRepairGracePeriod).Return([]*domain.User{repairUser}, nil)
	mockRepo.On("GetUserByID", ctx, "user-plaid-precheck-fail").Return(nil, errors.New("db: connection reset"))
	mockRepo.On("GetUsersNeedingRetention30dNotice", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersNeedingRetention7dNotice", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersWithExpiredRetention", ctx).Return([]*domain.User{}, nil)

	worker := NewRetentionCleanupCheckWorker(RetentionCleanupCheckWorkerDeps{
		ProfileRepo:     mockRepo,
		DeletionService: mockDeletion,
		EmailService:    mockEmail,
		RetentionMonths: 6,
		Logger:          slog.Default(),
	})
	worker.SetPlaidCleanup(mockPlaid)
	job := newTestRetentionCleanupJob(RetentionCleanupCheckArgs{TriggerSource: "periodic"})

	before := testutil.ToFloat64(observability.PlaidDisconnectRepairUserFailuresTotal)
	err := worker.Work(ctx, job)
	require.NoError(t, err)

	after := testutil.ToFloat64(observability.PlaidDisconnectRepairUserFailuresTotal)
	assert.Equal(t, before+1, after, "a pre-disconnect re-eligibility read failure must also increment the failure-visibility counter")
	mockPlaid.AssertNotCalled(t, "DisconnectAllUserItemsIfEligible", mock.Anything, mock.Anything, mock.Anything)
}

// TestRetentionCleanupCheck_PlaidRepairSuccess_DoesNotIncrementUserFailureCounter is the negative
// control: a successful per-user repair must leave the counter untouched.
func TestRetentionCleanupCheck_PlaidRepairSuccess_DoesNotIncrementUserFailureCounter(t *testing.T) {
	ctx := context.Background()

	pendingSince := time.Now().Add(-100 * time.Hour)
	user := &domain.User{ID: "user-plaid-repair-success-counter", Email: "plaidrepairsuccesscounter@example.com", SubscriptionStatus: "inactive", PlaidCleanupPendingAt: &pendingSince}

	mockRepo := new(MockRetentionProfileRepository)
	mockDeletion := new(MockRetentionDeletionService)
	mockEmail := new(MockRetentionEmailService)
	mockPlaid := new(MockRetentionPlaidCleanup)

	mockRepo.On("GetUsersNeedingRetentionClockRepair", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersNeedingPlaidDisconnectRepair", ctx, plaidDisconnectRepairGracePeriod).Return([]*domain.User{user}, nil)
	mockRepo.On("GetUserByID", ctx, "user-plaid-repair-success-counter").Return(user, nil)
	mockPlaid.On("DisconnectAllUserItemsIfEligible", ctx, "user-plaid-repair-success-counter", mock.Anything).Return(nil)
	mockRepo.On("ClearPlaidCleanupPendingIfMatches", ctx, "user-plaid-repair-success-counter", pendingSince).Return(true, nil)
	mockRepo.On("GetUsersNeedingRetention30dNotice", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersNeedingRetention7dNotice", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersWithExpiredRetention", ctx).Return([]*domain.User{}, nil)

	worker := NewRetentionCleanupCheckWorker(RetentionCleanupCheckWorkerDeps{
		ProfileRepo:     mockRepo,
		DeletionService: mockDeletion,
		EmailService:    mockEmail,
		RetentionMonths: 6,
		Logger:          slog.Default(),
	})
	worker.SetPlaidCleanup(mockPlaid)
	job := newTestRetentionCleanupJob(RetentionCleanupCheckArgs{TriggerSource: "periodic"})

	before := testutil.ToFloat64(observability.PlaidDisconnectRepairUserFailuresTotal)
	err := worker.Work(ctx, job)
	require.NoError(t, err)

	after := testutil.ToFloat64(observability.PlaidDisconnectRepairUserFailuresTotal)
	assert.Equal(t, before, after, "a successful per-user Plaid disconnect repair must NOT increment the failure-visibility counter")
}

// ─── Issue #1413 round-14 H1 (Codex review): a mid-loop abort must not be treated as completed
// cleanup ──
//
// plaid_item_cleanup_service.go's cleanupItems now returns services.ErrCleanupAborted (not nil)
// when the per-item eligibility recheck reports the user is no longer eligible partway through the
// loop — e.g. an admin granted a comp mid-run. Before this fix, that outcome returned nil, which
// this worker's `if disconnectErr != nil` branch never entered, so it fell straight through to
// ClearPlaidCleanupPendingIfMatches. Round-13 (36455e78) is what made that bite: it deliberately
// stopped a comp grant from touching the marker, so the CAS guard on that clear call still MATCHED
// and the clear SUCCEEDED — permanently hiding a user with still-live linked items from the only
// code path that ever disconnects them, resurrecting the exact leak round 13 had just closed.

// TestRetentionCleanupCheck_PlaidRepair_AbortedMidRun_MarkerLeftSetAndNotCountedAsFailure is the
// round-14 H1 RED test. A user with multiple live items whose eligibility flips to false partway
// through (modeled here by the collaborator returning ErrCleanupAborted, exactly as the real
// service now does — see plaid_item_cleanup_service_test.go's
// TestPlaidCleanup_DisconnectIfEligible_BecomesIneligibleMidLoop_RemainingItemsSpared for the
// service-level proof that this outcome is genuinely reachable) must end the run with:
//   - plaid_cleanup_pending_at left SET — ClearPlaidCleanupPendingIfMatches is never called;
//   - the job still succeeding (this is a legitimate, expected outcome, not a job failure);
//   - the failure-visibility counter untouched — an abort is not a Plaid failure; and
//   - nothing logged at WARN — only Info, mirroring the ErrSideEffectClaimBusy precedent.
//
// Falsifiability: on retention_cleanup_check.go as it stood before this fix (no
// errors.Is(disconnectErr, services.ErrCleanupAborted) branch), this error falls into the generic
// `else` branch — it is Warn-logged and DOES increment PlaidDisconnectRepairUserFailuresTotal. The
// counter and log-level assertions below fail against that code; ClearPlaidCleanupPendingIfMatches
// is left unstubbed specifically so an accidental call (which the OLD code's nil-return version of
// this bug would have caused, had the service itself not been fixed first) panics the test instead
// of silently passing.
func TestRetentionCleanupCheck_PlaidRepair_AbortedMidRun_MarkerLeftSetAndNotCountedAsFailure(t *testing.T) {
	ctx := context.Background()

	pendingSince := time.Now().Add(-100 * time.Hour)
	user := &domain.User{ID: "user-aborted-mid-run", Email: "abortedmidrun@example.com", SubscriptionStatus: "inactive", PlaidCleanupPendingAt: &pendingSince}

	mockRepo := new(MockRetentionProfileRepository)
	mockDeletion := new(MockRetentionDeletionService)
	mockEmail := new(MockRetentionEmailService)
	mockPlaid := new(MockRetentionPlaidCleanup)

	mockRepo.On("GetUsersNeedingRetentionClockRepair", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersNeedingPlaidDisconnectRepair", ctx, plaidDisconnectRepairGracePeriod).Return([]*domain.User{user}, nil)
	mockRepo.On("GetUserByID", ctx, "user-aborted-mid-run").Return(user, nil)
	mockPlaid.On("DisconnectAllUserItemsIfEligible", ctx, "user-aborted-mid-run", mock.Anything).
		Return(services.ErrCleanupAborted)
	mockRepo.On("GetUsersNeedingRetention30dNotice", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersNeedingRetention7dNotice", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersWithExpiredRetention", ctx).Return([]*domain.User{}, nil)
	// ClearPlaidCleanupPendingIfMatches is deliberately left unstubbed: an unexpected call panics —
	// proving the marker is NOT cleared on a mid-run abort.

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))

	worker := NewRetentionCleanupCheckWorker(RetentionCleanupCheckWorkerDeps{
		ProfileRepo:     mockRepo,
		DeletionService: mockDeletion,
		EmailService:    mockEmail,
		RetentionMonths: 6,
		Logger:          logger,
	})
	worker.SetPlaidCleanup(mockPlaid)
	job := newTestRetentionCleanupJob(RetentionCleanupCheckArgs{TriggerSource: "periodic"})

	before := testutil.ToFloat64(observability.PlaidDisconnectRepairUserFailuresTotal)
	err := worker.Work(ctx, job)
	require.NoError(t, err, "a mid-loop abort is a legitimate, expected outcome — it must not fail the job")

	after := testutil.ToFloat64(observability.PlaidDisconnectRepairUserFailuresTotal)
	assert.Equal(t, before, after, "a mid-loop abort must NOT increment the failure-visibility counter — it is not a Plaid failure")

	mockRepo.AssertNotCalled(t, "ClearPlaidCleanupPendingIfMatches", mock.Anything, mock.Anything, mock.Anything)

	logOutput := logBuf.String()
	assert.NotContains(t, logOutput, "level=WARN", "a mid-loop abort must be logged at Info, not Warn — it is a correct, expected outcome, mirroring the ErrSideEffectClaimBusy precedent")
	assert.Contains(t, logOutput, "aborted mid-run", "the abort must be visible in the logs, not silently swallowed")
}

// TestRetentionCleanupCheck_PlaidRepair_AbortedMidRun_JoinedError proves the sentinel is still
// detected via errors.Is when DisconnectAllUserItemsIfEligible wraps it (as the real service does,
// via fmt.Errorf("%w: ...", ErrCleanupAborted)) rather than returning the bare sentinel.
func TestRetentionCleanupCheck_PlaidRepair_AbortedMidRun_JoinedError(t *testing.T) {
	ctx := context.Background()

	pendingSince := time.Now().Add(-100 * time.Hour)
	user := &domain.User{ID: "user-aborted-wrapped", Email: "abortedwrapped@example.com", SubscriptionStatus: "cancelled", PlaidCleanupPendingAt: &pendingSince}

	mockRepo := new(MockRetentionProfileRepository)
	mockDeletion := new(MockRetentionDeletionService)
	mockEmail := new(MockRetentionEmailService)
	mockPlaid := new(MockRetentionPlaidCleanup)

	mockRepo.On("GetUsersNeedingRetentionClockRepair", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersNeedingPlaidDisconnectRepair", ctx, plaidDisconnectRepairGracePeriod).Return([]*domain.User{user}, nil)
	mockRepo.On("GetUserByID", ctx, "user-aborted-wrapped").Return(user, nil)
	mockPlaid.On("DisconnectAllUserItemsIfEligible", ctx, "user-aborted-wrapped", mock.Anything).
		Return(fmt.Errorf("cleanupItems: %w for user user-aborted-wrapped (1 of 2 item(s) not attempted)", services.ErrCleanupAborted))
	mockRepo.On("GetUsersNeedingRetention30dNotice", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersNeedingRetention7dNotice", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersWithExpiredRetention", ctx).Return([]*domain.User{}, nil)
	// ClearPlaidCleanupPendingIfMatches deliberately left unstubbed — an unexpected call panics.

	worker := NewRetentionCleanupCheckWorker(RetentionCleanupCheckWorkerDeps{
		ProfileRepo:     mockRepo,
		DeletionService: mockDeletion,
		EmailService:    mockEmail,
		RetentionMonths: 6,
		Logger:          slog.Default(),
	})
	worker.SetPlaidCleanup(mockPlaid)
	job := newTestRetentionCleanupJob(RetentionCleanupCheckArgs{TriggerSource: "periodic"})

	before := testutil.ToFloat64(observability.PlaidDisconnectRepairUserFailuresTotal)
	err := worker.Work(ctx, job)
	require.NoError(t, err)

	after := testutil.ToFloat64(observability.PlaidDisconnectRepairUserFailuresTotal)
	assert.Equal(t, before, after, "a wrapped ErrCleanupAborted must still be recognized via errors.Is and excluded from the failure counter")
}

// TestRetentionCleanupCheck_PlaidRepairNilCleanup_FailsJob_HeartbeatNotAdvanced is the round-10
// (Fix 1 / round-9 review Blocker) replacement for the old
// TestRetentionCleanupCheck_PlaidRepairNilCleanup_LogsAndSkips, which asserted the BROKEN contract:
// a nil Plaid cleanup collaborator used to be logged and swallowed, so a deployment with bad Plaid
// credentials ran this job daily, disconnected nobody, and reported healthy forever — the exact
// "silently not disconnect while reporting success" failure mode this whole task exists to close.
// A missing Plaid cleanup dependency is a CONFIGURATION ERROR: the job must fail loudly, and the
// RecordJobSuccess heartbeat must NOT advance, so an alert can fire on "this job hasn't succeeded
// in > 48h" instead of everything looking fine.
func TestRetentionCleanupCheck_PlaidRepairNilCleanup_FailsJob_HeartbeatNotAdvanced(t *testing.T) {
	ctx := context.Background()

	mockRepo := new(MockRetentionProfileRepository)
	mockDeletion := new(MockRetentionDeletionService)
	mockEmail := new(MockRetentionEmailService)

	mockRepo.On("GetUsersNeedingRetentionClockRepair", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersNeedingRetention30dNotice", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersNeedingRetention7dNotice", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersWithExpiredRetention", ctx).Return([]*domain.User{}, nil)
	// GetUsersNeedingPlaidDisconnectRepair / GetUserByID are deliberately left unstubbed: an
	// unexpected call panics, proving the nil-dependency check runs BEFORE the candidate query even
	// fires — matching this worker's other required-dependency checks (e.g.
	// processRetentionNotices's claimsRepo nil check), which fail fast rather than doing partial
	// work first.

	// worker.plaidCleanup is deliberately left nil (SetPlaidCleanup never called).
	worker := NewRetentionCleanupCheckWorker(RetentionCleanupCheckWorkerDeps{
		ProfileRepo:     mockRepo,
		DeletionService: mockDeletion,
		EmailService:    mockEmail,
		RetentionMonths: 6,
		Logger:          slog.Default(),
	})
	job := newTestRetentionCleanupJob(RetentionCleanupCheckArgs{TriggerSource: "periodic"})

	before := testutil.ToFloat64(observability.JobLastSuccessTimestamp.WithLabelValues(job.Kind))

	err := worker.Work(ctx, job)
	require.Error(t, err, "a nil Plaid cleanup collaborator is a configuration error and must fail the job")
	assert.ErrorIs(t, err, errPlaidCleanupNotConfigured)

	after := testutil.ToFloat64(observability.JobLastSuccessTimestamp.WithLabelValues(job.Kind))
	assert.Equal(t, before, after, "the last-success heartbeat must NOT advance when the job fails")

	mockRepo.AssertNotCalled(t, "GetUsersNeedingPlaidDisconnectRepair", mock.Anything, mock.Anything)
	mockRepo.AssertNotCalled(t, "GetUserByID", mock.Anything, mock.Anything)
}

// ─── Issue #1413 round-8 High 4 (Codex review): the per-item recheck must use the EXACT SAME
// status set as the repair SQL, not a wider "read-only" notion ──

// TestRetentionCleanupCheck_PlaidRepairSkipsIfTransitionedToPastDueSinceQuery is the RED test for
// round-8 High 4. The repair SQL (GetUsersNeedingPlaidDisconnectRepair) selects only 'inactive' and
// 'cancelled' candidates, but the per-item recheck used domain.SubscriptionStatus.
// IsReadOnlySubscription(), which ALSO includes 'past_due'. An old inactive/cancelled candidate
// that transitions into a NEW past_due cycle (e.g. a resubscribe followed by a failed payment)
// before this re-read still passed the recheck and could be disconnected immediately — bypassing
// past_due's own dunning timeline (past_due_service.go, PastDueConfig.PlaidDisconnectDays), which
// exists precisely to give a paying-but-behind user time before Plaid access is touched.
//
// Falsifiability: reverting the recheck to IsReadOnlySubscription() makes this test fail —
// DisconnectAllUserItemsIfEligible would be called for a user who is now 'past_due', not
// inactive/cancelled.
func TestRetentionCleanupCheck_PlaidRepairSkipsIfTransitionedToPastDueSinceQuery(t *testing.T) {
	ctx := context.Background()

	stalePendingSince := time.Now().Add(-100 * time.Hour)
	staleUser := &domain.User{ID: "user-now-pastdue", Email: "nowpastdue@example.com", SubscriptionStatus: "inactive", PlaidCleanupPendingAt: &stalePendingSince}
	users := []*domain.User{staleUser}
	currentUser := &domain.User{ID: "user-now-pastdue", Email: "nowpastdue@example.com", SubscriptionStatus: "past_due"}

	mockRepo := new(MockRetentionProfileRepository)
	mockDeletion := new(MockRetentionDeletionService)
	mockEmail := new(MockRetentionEmailService)
	mockPlaid := new(MockRetentionPlaidCleanup)

	mockRepo.On("GetUsersNeedingRetentionClockRepair", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersNeedingPlaidDisconnectRepair", ctx, plaidDisconnectRepairGracePeriod).Return(users, nil)
	mockRepo.On("GetUserByID", ctx, "user-now-pastdue").Return(currentUser, nil)
	mockRepo.On("GetUsersNeedingRetention30dNotice", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersNeedingRetention7dNotice", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersWithExpiredRetention", ctx).Return([]*domain.User{}, nil)
	// DisconnectAllUserItemsIfEligible is deliberately left unstubbed: an unexpected call panics.

	worker := NewRetentionCleanupCheckWorker(RetentionCleanupCheckWorkerDeps{
		ProfileRepo:     mockRepo,
		DeletionService: mockDeletion,
		EmailService:    mockEmail,
		RetentionMonths: 6,
		Logger:          slog.Default(),
	})
	worker.SetPlaidCleanup(mockPlaid)
	job := newTestRetentionCleanupJob(RetentionCleanupCheckArgs{TriggerSource: "periodic"})

	err := worker.Work(ctx, job)
	require.NoError(t, err)

	mockPlaid.AssertNotCalled(t, "DisconnectAllUserItemsIfEligible", mock.Anything, mock.Anything, mock.Anything)
}

// ─── Issue #1413 round-11 Fix 3: the recheck must validate the marker's IDENTITY, not merely its
// presence ──

// TestRetentionCleanupCheck_PlaidRepairSkipsIfMarkerReplacedByNewEpisodeSinceQuery is the round-11
// Fix 3 RED test. Sequence modeled: a stale candidate is selected with an AGED marker; before this
// worker's recheck runs, an admin grants a comp (clearing the marker) and then removes it (setting
// a FRESH marker for a brand-new ineligibility episode) — status and override-absence both still
// look "eligible" to a presence-only check, but the marker itself is now a DIFFERENT (much
// younger) episode than the one this worker selected on. Before the fix, isStillEligible only
// checked "PlaidCleanupPendingAt != nil", which this fresh marker satisfies — the stale worker
// would disconnect immediately, then clear the NEW episode's marker too, losing its entire grace
// window. The fix requires the CURRENT marker to still equal the SELECTED marker.
//
// Falsifiability: reverting isStillEligible's marker check to "current.PlaidCleanupPendingAt !=
// nil" (dropping the FormatOccurrenceTimestamp equality) makes this test fail —
// DisconnectAllUserItemsIfEligible would be called despite the marker having moved on.
func TestRetentionCleanupCheck_PlaidRepairSkipsIfMarkerReplacedByNewEpisodeSinceQuery(t *testing.T) {
	ctx := context.Background()

	agedMarker := time.Now().Add(-200 * time.Hour) // the episode this worker selected on
	staleUser := &domain.User{ID: "user-marker-replaced", Email: "markerreplaced@example.com", SubscriptionStatus: "cancelled", PlaidCleanupPendingAt: &agedMarker}
	users := []*domain.User{staleUser}

	freshMarker := time.Now().Add(-time.Minute) // the NEW episode (comp granted then removed)
	currentUser := &domain.User{ID: "user-marker-replaced", Email: "markerreplaced@example.com", SubscriptionStatus: "cancelled", PlaidCleanupPendingAt: &freshMarker}

	mockRepo := new(MockRetentionProfileRepository)
	mockDeletion := new(MockRetentionDeletionService)
	mockEmail := new(MockRetentionEmailService)
	mockPlaid := new(MockRetentionPlaidCleanup)

	mockRepo.On("GetUsersNeedingRetentionClockRepair", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersNeedingPlaidDisconnectRepair", ctx, plaidDisconnectRepairGracePeriod).Return(users, nil)
	mockRepo.On("GetUserByID", ctx, "user-marker-replaced").Return(currentUser, nil)
	mockRepo.On("GetUsersNeedingRetention30dNotice", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersNeedingRetention7dNotice", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersWithExpiredRetention", ctx).Return([]*domain.User{}, nil)
	// DisconnectAllUserItemsIfEligible and ClearPlaidCleanupPendingIfMatches are deliberately left
	// unstubbed: an unexpected call panics, proving neither the disconnect nor a clear of the NEW
	// episode's marker ever happens.

	worker := NewRetentionCleanupCheckWorker(RetentionCleanupCheckWorkerDeps{
		ProfileRepo:     mockRepo,
		DeletionService: mockDeletion,
		EmailService:    mockEmail,
		RetentionMonths: 6,
		Logger:          slog.Default(),
	})
	worker.SetPlaidCleanup(mockPlaid)
	job := newTestRetentionCleanupJob(RetentionCleanupCheckArgs{TriggerSource: "periodic"})

	err := worker.Work(ctx, job)
	require.NoError(t, err)

	mockPlaid.AssertNotCalled(t, "DisconnectAllUserItemsIfEligible", mock.Anything, mock.Anything, mock.Anything)
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
	mockRepo.On("IsRetentionNotice30dEligibleForOccurrence", ctx, "user-1", retentionExpiresAt).Return(true, nil)
	mockRepo.On("IsRetentionNotice30dEligibleForOccurrence", ctx, "user-2", retentionExpiresAt).Return(true, nil)
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
	mockRepo.On("IsRetentionNotice7dEligibleForOccurrence", ctx, "user-3", retentionExpiresAt).Return(true, nil)
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
	// IsRetentionNotice30dEligibleForOccurrence / SetRetentionNotice30dSent are deliberately left
	// unstubbed: with no email service configured, the skip must happen BEFORE claiming or
	// touching the profile repo at all. An unstubbed testify mock call panics, a stronger proof
	// than AssertExpectations alone.

	// nil email service, but claimsRepo IS wired to a WORKING (allowing) mock — this is the part
	// that makes the test actually falsifiable: if the "skip before claiming" check were removed,
	// Claim would succeed and the code would proceed to call the unstubbed
	// IsRetentionNotice30dEligibleForOccurrence, panicking. A nil claimsRepo here would let that same broken
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

// ─── Issue #1413 P0: RunOnStart must be true ───────────────────────────────────────────────
//
// River's periodic scheduler keeps only in-memory state (river.NewPeriodicJob's own doc
// comment): every process restart or leader election recomputes nextRunAt as ScheduleFunc(now)
// unless PeriodicJobOpts.ID is set (it isn't, here or anywhere in main.go), and skips the
// catch-up insert entirely when RunOnStart is false (river@v0.38.0
// internal/maintenance/periodic_job_enqueuer.go:409-426). This repo deploys several times a
// day, so RunOnStart: false let this daily job — the only place that runs retention-clock
// repair, the 30-day/7-day GDPR pre-deletion notices, deleteExpiredAccounts, and the
// Plaid-disconnect repair pass — be enqueued zero times, ever.

// TestRetentionCleanupCheckPeriodicOpts_RunOnStart is the RED test for Issue #1413 P0.
func TestRetentionCleanupCheckPeriodicOpts_RunOnStart(t *testing.T) {
	opts := RetentionCleanupCheckPeriodicOpts()

	require.NotNil(t, opts)
	assert.True(t, opts.RunOnStart,
		"RunOnStart must be true — without it, a process restart or leader election (this repo "+
			"deploys several times a day) can push this daily job's next-run time forward forever, "+
			"silently disabling GDPR pre-deletion notices, account deletion, retention-clock repair, "+
			"and Plaid-disconnect repair (Issue #1413 P0)")
}

// TestRetentionCleanupCheckPeriodicOpts_PairedWithUniqueOpts proves RunOnStart: true is only
// safe here because RetentionCleanupCheckUniqueOpts()'s dedup exists alongside it — the two
// are only safe together. A future edit that weakens UniqueOpts to a zero value (no ByPeriod,
// no ByState — which River treats as "no uniqueness constraint at all") while leaving
// RunOnStart: true in place would let a start-of-process catch-up run double-fire alongside an
// already-scheduled run; this test fails first.
func TestRetentionCleanupCheckPeriodicOpts_PairedWithUniqueOpts(t *testing.T) {
	opts := RetentionCleanupCheckPeriodicOpts()
	require.True(t, opts.RunOnStart, "this pairing guarantee only matters when RunOnStart is true")

	unique := RetentionCleanupCheckUniqueOpts()
	require.NotNil(t, unique)
	assert.NotZero(t, unique.ByPeriod,
		"RunOnStart: true's catch-up insert is only safe when deduped against an "+
			"already-scheduled run by a non-zero ByPeriod window")
	require.NotEmpty(t, unique.ByState,
		"RunOnStart: true's catch-up insert is only safe when deduped against an "+
			"already-scheduled run by a non-empty ByState constraint")
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

// ─── Issue #1413 round-8 Blocker 2 (Codex review): the repair pass must not depend on
// DELETION_TOKEN_SECRET ──

// TestRetentionCleanupCheck_NilDeletionService_SkipsAutoDeleteButRepairStepsStillRun is the RED
// test for round-8 Blocker 2: RetentionCleanupCheckWorker used to be constructed ONLY inside
// `if cfg.DeletionTokenSecret != ""` in main.go, coupling repairPlaidDisconnects — the ONLY place
// a read-only user's Plaid items are ever disconnected — to an unrelated secret that production
// validation didn't even check. A deployment with good Plaid credentials but no
// DELETION_TOKEN_SECRET would silently never disconnect anyone. The fix constructs the worker
// unconditionally, so DeletionService can genuinely be nil here; deleteExpiredAccounts (Step 3)
// must degrade gracefully (log and skip) rather than panic on a nil interface method call, while
// the retention-clock repair (Step 0) and Plaid-disconnect repair (Step 0b) — neither of which
// touches DeletionService at all — must still run in the SAME pass.
//
// Falsifiability: calling w.deletionService.ExecuteDeletion without a nil guard panics
// ("invalid memory address or nil pointer dereference") the moment GetUsersWithExpiredRetention
// returns a non-empty list against a nil DeletionService — this test's expired-user fixture
// exists specifically to trigger that path.
func TestRetentionCleanupCheck_NilDeletionService_SkipsAutoDeleteButRepairStepsStillRun(t *testing.T) {
	ctx := context.Background()

	retentionExpiresAt := time.Now().Add(-24 * time.Hour)
	expiredUsers := []*domain.User{
		{ID: "user-would-be-deleted", Email: "wouldbedeleted@example.com", RetentionExpiresAt: &retentionExpiresAt},
	}
	plaidPendingSince := time.Now().Add(-100 * time.Hour)
	plaidRepairUser := &domain.User{ID: "user-needs-plaid-repair-nodeletion", Email: "plaidrepairnodeletion@example.com", SubscriptionStatus: "inactive", PlaidCleanupPendingAt: &plaidPendingSince}

	mockRepo := new(MockRetentionProfileRepository)
	mockEmail := new(MockRetentionEmailService)
	mockPlaid := new(MockRetentionPlaidCleanup)

	mockRepo.On("GetUsersNeedingRetentionClockRepair", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersNeedingPlaidDisconnectRepair", ctx, plaidDisconnectRepairGracePeriod).Return([]*domain.User{plaidRepairUser}, nil)
	mockRepo.On("GetUserByID", ctx, "user-needs-plaid-repair-nodeletion").Return(plaidRepairUser, nil)
	mockPlaid.On("DisconnectAllUserItemsIfEligible", ctx, "user-needs-plaid-repair-nodeletion", mock.Anything).Return(nil)
	mockRepo.On("ClearPlaidCleanupPendingIfMatches", ctx, "user-needs-plaid-repair-nodeletion", plaidPendingSince).Return(true, nil)
	mockRepo.On("GetUsersNeedingRetention30dNotice", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersNeedingRetention7dNotice", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersWithExpiredRetention", ctx).Return(expiredUsers, nil)
	// DeletionService is nil — there is no mock to call ExecuteDeletion on; an unguarded call
	// panics instead of erroring, which is why this test asserts via require.NoError plus
	// completing without a panic, not via an AssertNotCalled expectation.

	worker := NewRetentionCleanupCheckWorker(RetentionCleanupCheckWorkerDeps{
		ProfileRepo:     mockRepo,
		DeletionService: nil,
		EmailService:    mockEmail,
		RetentionMonths: 6,
		Logger:          slog.Default(),
	})
	worker.SetPlaidCleanup(mockPlaid)
	job := newTestRetentionCleanupJob(RetentionCleanupCheckArgs{TriggerSource: "periodic"})

	err := worker.Work(ctx, job)

	require.NoError(t, err, "a nil DeletionService must not fail (or panic) the whole job — it only disables Step 3 (auto-deletion)")
	mockPlaid.AssertCalled(t, "DisconnectAllUserItemsIfEligible", ctx, "user-needs-plaid-repair-nodeletion", mock.Anything)
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
// 30-day pre-deletion warning. IsRetentionNotice30dEligibleForOccurrence is deliberately left unstubbed.
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
	mockRepo.AssertNotCalled(t, "IsRetentionNotice30dEligibleForOccurrence", mock.Anything, "user-comped-30d", mock.Anything)
}

// TestRetentionCleanupCheck_CompedUser_No7dNotice proves a comped user does not receive the
// 7-day final warning. IsRetentionNotice7dEligibleForOccurrence is deliberately left unstubbed.
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
	mockRepo.AssertNotCalled(t, "IsRetentionNotice7dEligibleForOccurrence", mock.Anything, "user-comped-7d", mock.Anything)
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
// SendRetentionNoticeEmail and IsRetentionNotice30dEligibleForOccurrence are deliberately left unstubbed — an
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
	resourceID := retentionOccurrenceResourceID(t, "user-race-30d", retentionExpiresAt)
	mockClaims.On("Claim", ctx, repository.ResourceTypeUser, resourceID, repository.ActionRetentionNotice30d, retentionNoticeClaimLease).
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
	resourceID := retentionOccurrenceResourceID(t, "user-race-7d", retentionExpiresAt)
	mockClaims.On("Claim", ctx, repository.ResourceTypeUser, resourceID, repository.ActionRetentionNotice7d, retentionNoticeClaimLease).
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

	claimErrResourceID := retentionOccurrenceResourceID(t, "user-claimerr", retentionExpiresAt)
	claimOKResourceID := retentionOccurrenceResourceID(t, "user-claimok", retentionExpiresAt)
	mockClaims.On("Claim", ctx, repository.ResourceTypeUser, claimErrResourceID, repository.ActionRetentionNotice30d, retentionNoticeClaimLease).
		Return("", false, errors.New("db unavailable"))
	mockClaims.On("Claim", ctx, repository.ResourceTypeUser, claimOKResourceID, repository.ActionRetentionNotice30d, retentionNoticeClaimLease).
		Return(testRetentionClaimToken, true, nil)
	mockClaims.On("Complete", ctx, repository.ResourceTypeUser, claimOKResourceID, repository.ActionRetentionNotice30d, testRetentionClaimToken).
		Return(true, nil)

	mockRepo.On("IsRetentionNotice30dEligibleForOccurrence", ctx, "user-claimok", retentionExpiresAt).Return(true, nil)
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

	resourceID := retentionOccurrenceResourceID(t, "user-ineligible-30d", retentionExpiresAt)
	mockClaims.On("Claim", ctx, repository.ResourceTypeUser, resourceID, repository.ActionRetentionNotice30d, retentionNoticeClaimLease).
		Return(testRetentionClaimToken, true, nil)
	mockRepo.On("IsRetentionNotice30dEligibleForOccurrence", ctx, "user-ineligible-30d", retentionExpiresAt).Return(false, nil)
	mockClaims.On("Release", ctx, repository.ResourceTypeUser, resourceID, repository.ActionRetentionNotice30d, testRetentionClaimToken, "precondition lost since claim").
		Return(true, nil)

	worker := newTestRetentionWorker(mockRepo, mockDeletion, mockEmail)
	worker.SetWorkClaimsRepo(mockClaims)
	job := newTestRetentionCleanupJob(RetentionCleanupCheckArgs{TriggerSource: "periodic"})

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockClaims.AssertCalled(t, "Release", ctx, repository.ResourceTypeUser, resourceID, repository.ActionRetentionNotice30d, testRetentionClaimToken, "precondition lost since claim")
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
	mockRepo.On("IsRetentionNotice30dEligibleForOccurrence", ctx, "user-retry-30d", retentionExpiresAt).Return(true, nil).Twice()

	const token1 = "token-attempt-1"
	const token2 = "token-attempt-2"
	sendErr := errors.New("smtp: connection refused")
	// Same occurrence across both runs (retentionExpiresAt is the SAME value both times, since
	// GetUsersNeedingRetention30dNotice returns the SAME *domain.User both times) — proving the
	// composite key collides for a retry of the SAME retention cycle, exactly as intended.
	resourceID := retentionOccurrenceResourceID(t, "user-retry-30d", retentionExpiresAt)

	// First run: claim wins, but the send fails.
	mockClaims.On("Claim", ctx, repository.ResourceTypeUser, resourceID, repository.ActionRetentionNotice30d, retentionNoticeClaimLease).
		Return(token1, true, nil).Once()
	mockEmail.On("SendRetentionNoticeEmail", ctx, "retry30@example.com", mock.Anything, mock.Anything).
		Return(sendErr).Once()
	mockClaims.On("Release", ctx, repository.ResourceTypeUser, resourceID, repository.ActionRetentionNotice30d, token1, sendErr.Error()).
		Return(true, nil).Once()

	// Second run ("the next day"): a fresh claim (Release made it immediately reclaimable), and
	// this time the send succeeds.
	mockClaims.On("Claim", ctx, repository.ResourceTypeUser, resourceID, repository.ActionRetentionNotice30d, retentionNoticeClaimLease).
		Return(token2, true, nil).Once()
	mockEmail.On("SendRetentionNoticeEmail", ctx, "retry30@example.com", mock.Anything, mock.Anything).
		Return(nil).Once()
	mockRepo.On("SetRetentionNotice30dSent", ctx, "user-retry-30d").Return(nil).Once()
	mockClaims.On("Complete", ctx, repository.ResourceTypeUser, resourceID, repository.ActionRetentionNotice30d, token2).
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
// exercises IsRetentionNotice7dEligibleForOccurrence / SetRetentionNotice7dSent / ActionRetentionNotice7d, not
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
	mockRepo.On("IsRetentionNotice7dEligibleForOccurrence", ctx, "user-retry-7d", retentionExpiresAt).Return(true, nil).Twice()

	const token1 = "token-attempt-1-7d"
	const token2 = "token-attempt-2-7d"
	sendErr := errors.New("smtp: connection refused")
	resourceID := retentionOccurrenceResourceID(t, "user-retry-7d", retentionExpiresAt)

	mockClaims.On("Claim", ctx, repository.ResourceTypeUser, resourceID, repository.ActionRetentionNotice7d, retentionNoticeClaimLease).
		Return(token1, true, nil).Once()
	mockEmail.On("SendRetentionNoticeEmail", ctx, "retry7@example.com", mock.Anything, mock.Anything).
		Return(sendErr).Once()
	mockClaims.On("Release", ctx, repository.ResourceTypeUser, resourceID, repository.ActionRetentionNotice7d, token1, sendErr.Error()).
		Return(true, nil).Once()

	mockClaims.On("Claim", ctx, repository.ResourceTypeUser, resourceID, repository.ActionRetentionNotice7d, retentionNoticeClaimLease).
		Return(token2, true, nil).Once()
	mockEmail.On("SendRetentionNoticeEmail", ctx, "retry7@example.com", mock.Anything, mock.Anything).
		Return(nil).Once()
	mockRepo.On("SetRetentionNotice7dSent", ctx, "user-retry-7d").Return(nil).Once()
	mockClaims.On("Complete", ctx, repository.ResourceTypeUser, resourceID, repository.ActionRetentionNotice7d, token2).
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

	resourceID := retentionOccurrenceResourceID(t, "user-order-30d", retentionExpiresAt)
	var callOrder []string
	mockClaims.On("Claim", ctx, repository.ResourceTypeUser, resourceID, repository.ActionRetentionNotice30d, retentionNoticeClaimLease).
		Run(func(mock.Arguments) { callOrder = append(callOrder, "claim") }).
		Return(testRetentionClaimToken, true, nil)
	mockRepo.On("IsRetentionNotice30dEligibleForOccurrence", ctx, "user-order-30d", retentionExpiresAt).
		Run(func(mock.Arguments) { callOrder = append(callOrder, "eligible") }).
		Return(true, nil)
	mockEmail.On("SendRetentionNoticeEmail", ctx, "order30@example.com", mock.Anything, mock.Anything).
		Run(func(mock.Arguments) { callOrder = append(callOrder, "send") }).
		Return(nil)
	mockRepo.On("SetRetentionNotice30dSent", ctx, "user-order-30d").
		Run(func(mock.Arguments) { callOrder = append(callOrder, "stamp") }).
		Return(nil)
	mockClaims.On("Complete", ctx, repository.ResourceTypeUser, resourceID, repository.ActionRetentionNotice30d, testRetentionClaimToken).
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

	mockRepo.On("GetUsersNeedingRetentionClockRepair", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersNeedingPlaidDisconnectRepair", ctx, plaidDisconnectRepairGracePeriod).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersNeedingRetention30dNotice", ctx).Return(users, nil)
	mockRepo.On("GetUsersNeedingRetention7dNotice", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersWithExpiredRetention", ctx).Return([]*domain.User{}, nil)

	resourceID := retentionOccurrenceResourceID(t, "user-lease-lost", retentionExpiresAt)
	mockClaims.On("Claim", ctx, repository.ResourceTypeUser, resourceID, repository.ActionRetentionNotice30d, retentionNoticeClaimLease).
		Return(testRetentionClaimToken, true, nil)
	mockRepo.On("IsRetentionNotice30dEligibleForOccurrence", ctx, "user-lease-lost", retentionExpiresAt).Return(true, nil)
	mockEmail.On("SendRetentionNoticeEmail", ctx, "leaselost@example.com", mock.Anything, mock.Anything).Return(nil)
	mockRepo.On("SetRetentionNotice30dSent", ctx, "user-lease-lost").Return(nil)
	// The lease was reclaimed by another worker before we could Complete it.
	mockClaims.On("Complete", ctx, repository.ResourceTypeUser, resourceID, repository.ActionRetentionNotice30d, testRetentionClaimToken).
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
	worker.SetPlaidCleanup(new(MockRetentionPlaidCleanup))
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

	mockRepo.On("GetUsersNeedingRetentionClockRepair", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersNeedingPlaidDisconnectRepair", ctx, plaidDisconnectRepairGracePeriod).Return([]*domain.User{}, nil)
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

// TestRetentionCleanupCheck_ProcessRetentionNotices_NilRetentionExpiresAt_ReturnsErrorWithoutClaiming
// pins the Issue #1413 P5 NULL-discriminator decision for retention notices: if
// retention_expires_at is ever unset for a notice candidate (should be impossible in practice —
// GetUsersNeedingRetention30dNotice/7dNotice both filter on it being non-null — but this test
// calls processRetentionNotices directly to exercise the defensive path), the composite-key
// builder must refuse rather than silently collapse to the bare userID. mockClaims has NO
// expectations registered, so any Claim/Complete/Release call would panic — proving the claim
// layer is never reached at all.
func TestRetentionCleanupCheck_ProcessRetentionNotices_NilRetentionExpiresAt_ReturnsErrorWithoutClaiming(t *testing.T) {
	ctx := context.Background()

	mockRepo := new(MockRetentionProfileRepository)
	mockDeletion := new(MockRetentionDeletionService)
	mockEmail := new(MockRetentionEmailService)
	mockClaims := new(mockWorkClaimsRepository)

	user := &domain.User{ID: "user-nil-retention-discriminator", Email: "nildiscriminator@example.com"}
	// user.RetentionExpiresAt is deliberately left nil.

	worker := NewRetentionCleanupCheckWorker(RetentionCleanupCheckWorkerDeps{
		ProfileRepo:     mockRepo,
		DeletionService: mockDeletion,
		EmailService:    mockEmail,
		RetentionMonths: 6,
		Logger:          slog.Default(),
	})
	worker.SetWorkClaimsRepo(mockClaims)

	sent, errCount, skipped := worker.processRetentionNotices(ctx, 1, []*domain.User{user}, retentionNoticeSpec{
		action:     repository.ActionRetentionNotice30d,
		noticeName: "30d",
		subject:    "test subject",
		body:       "test body",
		isEligible: mockRepo.IsRetentionNotice30dEligibleForOccurrence,
		markSent:   mockRepo.SetRetentionNotice30dSent,
	})

	assert.Equal(t, 0, sent)
	assert.Equal(t, 1, errCount, "a missing occurrence discriminator must be counted as an error, not silently skipped as ineligible")
	assert.Equal(t, 0, skipped)
	mockEmail.AssertNotCalled(t, "SendRetentionNoticeEmail", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}
