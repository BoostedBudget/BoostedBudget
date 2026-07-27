// Package jobs contains River job definitions and workers for async processing.
// Issue #1413 Task 9: Manual tier override expiry check job tests.
package jobs

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// mockManualTierOverrideProfileRepository mocks ManualTierOverrideProfileRepository.
type mockManualTierOverrideProfileRepository struct {
	mock.Mock
}

func (m *mockManualTierOverrideProfileRepository) GetUsersWithExpiredManualTierOverride(ctx context.Context) ([]*domain.User, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.User), args.Error(1)
}

// mockManualTierOverrideRemover mocks ManualTierOverrideRemover — the single
// AdminUserRepository.RemoveUserTierOverride method this job reuses instead of duplicating its
// CTE (Issue #1414 / Task 7).
type mockManualTierOverrideRemover struct {
	mock.Mock
}

func (m *mockManualTierOverrideRemover) RemoveUserTierOverride(ctx context.Context, actorUserID, targetUserID, reason string, retentionMonths int) (repository.RemoveTierOverrideRow, error) {
	args := m.Called(ctx, actorUserID, targetUserID, reason, retentionMonths)
	if args.Get(0) == nil {
		return repository.RemoveTierOverrideRow{}, args.Error(1)
	}
	return args.Get(0).(repository.RemoveTierOverrideRow), args.Error(1)
}

// Test helpers

func newTestManualTierOverrideExpiryJob(args ManualTierOverrideExpiryCheckArgs) *river.Job[ManualTierOverrideExpiryCheckArgs] {
	return &river.Job[ManualTierOverrideExpiryCheckArgs]{
		JobRow: &rivertype.JobRow{
			ID:      1,
			Attempt: 1,
		},
		Args: args,
	}
}

func newTestManualTierOverrideExpiryWorker(
	repo ManualTierOverrideProfileRepository,
	remover ManualTierOverrideRemover,
	clerkSvc ClerkService,
) *ManualTierOverrideExpiryCheckWorker {
	return NewManualTierOverrideExpiryCheckWorker(ManualTierOverrideExpiryCheckWorkerDeps{
		ProfileRepo:     repo,
		Remover:         remover,
		ClerkService:    clerkSvc,
		RetentionMonths: 6,
		Logger:          slog.Default(),
	})
}

// --- Kind ---

func TestManualTierOverrideExpiryCheckArgs_Kind(t *testing.T) {
	args := ManualTierOverrideExpiryCheckArgs{}
	assert.Equal(t, "manual_tier_override_expiry_check", args.Kind())
}

// --- Core expiry behavior ---

// TestManualTierOverrideExpiryCheck_ExpiredOverride_Cleared proves an expired override is
// passed to the shared RemoveUserTierOverride repository method with a system (empty) actor.
func TestManualTierOverrideExpiryCheck_ExpiredOverride_Cleared(t *testing.T) {
	ctx := context.Background()

	override := "household_2"
	endsAt := time.Now().Add(-1 * time.Hour) // expired
	users := []*domain.User{
		{ID: "user-expired", ManualTierOverride: &override, ManualTierEndsAt: &endsAt},
	}

	mockRepo := new(mockManualTierOverrideProfileRepository)
	mockRemover := new(mockManualTierOverrideRemover)

	mockRepo.On("GetUsersWithExpiredManualTierOverride", ctx).Return(users, nil)
	mockRemover.On("RemoveUserTierOverride", ctx, "", "user-expired", mock.AnythingOfType("string"), 6).
		Return(repository.RemoveTierOverrideRow{Removed: true, RevertedTier: "premium", RevertedStatus: "cancelled"}, nil)

	worker := newTestManualTierOverrideExpiryWorker(mockRepo, mockRemover, nil)
	job := newTestManualTierOverrideExpiryJob(ManualTierOverrideExpiryCheckArgs{TriggerSource: "periodic"})

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockRepo.AssertExpectations(t)
	mockRemover.AssertExpectations(t)
}

// TestManualTierOverrideExpiryCheck_LiveOverride_LeftAlone proves the belt-and-braces guard:
// a candidate whose override is still active (per domain.User.HasActiveManualOverride) at the
// moment the worker processes it must NOT be passed to RemoveUserTierOverride — protects
// against a stale candidate list when an admin extends/re-applies a comp between the query and
// this loop. RemoveUserTierOverride is deliberately left unstubbed: a real call would panic.
func TestManualTierOverrideExpiryCheck_LiveOverride_LeftAlone(t *testing.T) {
	ctx := context.Background()

	override := "household_2"
	endsAt := time.Now().Add(1 * time.Hour) // still live
	users := []*domain.User{
		{ID: "user-live", ManualTierOverride: &override, ManualTierEndsAt: &endsAt},
	}

	mockRepo := new(mockManualTierOverrideProfileRepository)
	mockRemover := new(mockManualTierOverrideRemover)

	mockRepo.On("GetUsersWithExpiredManualTierOverride", ctx).Return(users, nil)

	worker := newTestManualTierOverrideExpiryWorker(mockRepo, mockRemover, nil)
	job := newTestManualTierOverrideExpiryJob(ManualTierOverrideExpiryCheckArgs{TriggerSource: "periodic"})

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockRemover.AssertNotCalled(t, "RemoveUserTierOverride", mock.Anything, mock.Anything, "user-live", mock.Anything, mock.Anything)
}

// Note on the "override ending exactly now" boundary: a literal, deterministic worker-level
// test for this tie is not achievable black-box. The guard calls
// domain.User.HasActiveManualOverride(), which always reads a FRESH time.Now() internally with
// no clock-injection point (domain/user.go is out of this job's scope fence to add one) — by
// the time Work() reaches the guard, real wall-clock time has unavoidably advanced past any
// ManualTierEndsAt fixed at test-setup, so it is always judged expired, never "exactly tied".
// The worker guard is a pure passthrough to HasActiveManualOverride() with no boundary logic of
// its own, so the exact-tie semantics are proven deterministically at the two layers that CAN
// pin a clock:
//   - domain/user_test.go's TestHasActiveManualOverrideAt_BoundarySemantics (calls the
//     unexported, now-injectable hasActiveManualOverrideAt(now) directly)
//   - TestGetUsersWithExpiredManualTierOverride_Integration_ExactExpiryBoundary (this package's
//     sibling repository test — evaluates the SQL predicate in a transaction sharing a single
//     pinned NOW())
//
// TestManualTierOverrideExpiryCheck_LiveOverride_LeftAlone below covers the guard's only
// deterministically-testable behavior: a comfortably-future ManualTierEndsAt is left alone.

// --- Clerk sync ---

// TestManualTierOverrideExpiryCheck_ClerkSyncAttempted_WithRevertedValues proves the worker
// re-syncs Clerk using the REVERTED tier/status returned by RemoveUserTierOverride (not any
// stale value), mirroring AdminUserService.RemoveUserTierOverride's own sync semantics.
func TestManualTierOverrideExpiryCheck_ClerkSyncAttempted_WithRevertedValues(t *testing.T) {
	ctx := context.Background()

	override := "household_2"
	endsAt := time.Now().Add(-1 * time.Hour)
	users := []*domain.User{
		{ID: "user-1", ManualTierOverride: &override, ManualTierEndsAt: &endsAt},
	}

	mockRepo := new(mockManualTierOverrideProfileRepository)
	mockRemover := new(mockManualTierOverrideRemover)
	mockClerk := new(mockClerkService)

	mockRepo.On("GetUsersWithExpiredManualTierOverride", ctx).Return(users, nil)
	mockRemover.On("RemoveUserTierOverride", ctx, "", "user-1", mock.AnythingOfType("string"), 6).
		Return(repository.RemoveTierOverrideRow{
			Removed:        true,
			RevertedTier:   "premium",
			RevertedStatus: "cancelled",
			ClerkUserID:    "clerk_user_1",
		}, nil)
	mockClerk.On("UpdateUserMetadata", ctx, "clerk_user_1", mock.MatchedBy(func(metadata map[string]interface{}) bool {
		return metadata["subscription_tier"] == "premium" && metadata["subscription_status"] == "cancelled"
	})).Return(nil)

	worker := newTestManualTierOverrideExpiryWorker(mockRepo, mockRemover, mockClerk)
	job := newTestManualTierOverrideExpiryJob(ManualTierOverrideExpiryCheckArgs{TriggerSource: "periodic"})

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockClerk.AssertExpectations(t)
}

// TestManualTierOverrideExpiryCheck_ClerkFailure_DoesNotFailJob proves a Clerk sync failure is
// best-effort: logged and swallowed, never propagated as a job error.
func TestManualTierOverrideExpiryCheck_ClerkFailure_DoesNotFailJob(t *testing.T) {
	ctx := context.Background()

	override := "household_2"
	endsAt := time.Now().Add(-1 * time.Hour)
	users := []*domain.User{
		{ID: "user-1", ManualTierOverride: &override, ManualTierEndsAt: &endsAt},
	}

	mockRepo := new(mockManualTierOverrideProfileRepository)
	mockRemover := new(mockManualTierOverrideRemover)
	mockClerk := new(mockClerkService)

	mockRepo.On("GetUsersWithExpiredManualTierOverride", ctx).Return(users, nil)
	mockRemover.On("RemoveUserTierOverride", ctx, "", "user-1", mock.AnythingOfType("string"), 6).
		Return(repository.RemoveTierOverrideRow{Removed: true, RevertedTier: "premium", RevertedStatus: "cancelled", ClerkUserID: "clerk_user_1"}, nil)
	mockClerk.On("UpdateUserMetadata", ctx, "clerk_user_1", mock.Anything).Return(errors.New("clerk unavailable"))

	worker := newTestManualTierOverrideExpiryWorker(mockRepo, mockRemover, mockClerk)
	job := newTestManualTierOverrideExpiryJob(ManualTierOverrideExpiryCheckArgs{TriggerSource: "periodic"})

	err := worker.Work(ctx, job)

	require.NoError(t, err)
}

// TestManualTierOverrideExpiryCheck_NilClerkService_NoPanic proves a nil ClerkService is safe
// (matches the nil-safe convention used throughout this package).
func TestManualTierOverrideExpiryCheck_NilClerkService_NoPanic(t *testing.T) {
	ctx := context.Background()

	override := "household_2"
	endsAt := time.Now().Add(-1 * time.Hour)
	users := []*domain.User{
		{ID: "user-1", ManualTierOverride: &override, ManualTierEndsAt: &endsAt},
	}

	mockRepo := new(mockManualTierOverrideProfileRepository)
	mockRemover := new(mockManualTierOverrideRemover)

	mockRepo.On("GetUsersWithExpiredManualTierOverride", ctx).Return(users, nil)
	mockRemover.On("RemoveUserTierOverride", ctx, "", "user-1", mock.AnythingOfType("string"), 6).
		Return(repository.RemoveTierOverrideRow{Removed: true, RevertedTier: "premium", RevertedStatus: "cancelled", ClerkUserID: "clerk_user_1"}, nil)

	worker := newTestManualTierOverrideExpiryWorker(mockRepo, mockRemover, nil)
	job := newTestManualTierOverrideExpiryJob(ManualTierOverrideExpiryCheckArgs{TriggerSource: "periodic"})

	require.NotPanics(t, func() {
		err := worker.Work(ctx, job)
		require.NoError(t, err)
	})
}

// --- Error resilience ---

// TestManualTierOverrideExpiryCheck_OneUserFails_OthersStillProcessed proves one user's
// RemoveUserTierOverride failure does not stall the batch — mirrors
// retention_cleanup_check.go's deleteExpiredAccounts log-and-continue pattern.
func TestManualTierOverrideExpiryCheck_OneUserFails_OthersStillProcessed(t *testing.T) {
	ctx := context.Background()

	override := "household_2"
	endsAt := time.Now().Add(-1 * time.Hour)
	users := []*domain.User{
		{ID: "user-fail", ManualTierOverride: &override, ManualTierEndsAt: &endsAt},
		{ID: "user-ok", ManualTierOverride: &override, ManualTierEndsAt: &endsAt},
	}

	mockRepo := new(mockManualTierOverrideProfileRepository)
	mockRemover := new(mockManualTierOverrideRemover)

	mockRepo.On("GetUsersWithExpiredManualTierOverride", ctx).Return(users, nil)
	mockRemover.On("RemoveUserTierOverride", ctx, "", "user-fail", mock.AnythingOfType("string"), 6).
		Return(nil, errors.New("db error"))
	mockRemover.On("RemoveUserTierOverride", ctx, "", "user-ok", mock.AnythingOfType("string"), 6).
		Return(repository.RemoveTierOverrideRow{Removed: true, RevertedTier: "premium", RevertedStatus: "cancelled"}, nil)

	worker := newTestManualTierOverrideExpiryWorker(mockRepo, mockRemover, nil)
	job := newTestManualTierOverrideExpiryJob(ManualTierOverrideExpiryCheckArgs{TriggerSource: "periodic"})

	err := worker.Work(ctx, job)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "1 of 2")
	mockRemover.AssertNumberOfCalls(t, "RemoveUserTierOverride", 2)
}

// TestManualTierOverrideExpiryCheck_NoUsers_NoOp tests the job succeeds when there's nothing to do.
func TestManualTierOverrideExpiryCheck_NoUsers_NoOp(t *testing.T) {
	ctx := context.Background()

	mockRepo := new(mockManualTierOverrideProfileRepository)
	mockRemover := new(mockManualTierOverrideRemover)

	mockRepo.On("GetUsersWithExpiredManualTierOverride", ctx).Return([]*domain.User{}, nil)

	worker := newTestManualTierOverrideExpiryWorker(mockRepo, mockRemover, nil)
	job := newTestManualTierOverrideExpiryJob(ManualTierOverrideExpiryCheckArgs{TriggerSource: "periodic"})

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockRemover.AssertNotCalled(t, "RemoveUserTierOverride")
}

// TestManualTierOverrideExpiryCheck_FetchError_ReturnsError proves a query failure surfaces as
// a job error (so River retries), matching the retention job's fetch-error handling.
func TestManualTierOverrideExpiryCheck_FetchError_ReturnsError(t *testing.T) {
	ctx := context.Background()

	mockRepo := new(mockManualTierOverrideProfileRepository)
	mockRemover := new(mockManualTierOverrideRemover)

	mockRepo.On("GetUsersWithExpiredManualTierOverride", ctx).Return(nil, errors.New("connection refused"))

	worker := newTestManualTierOverrideExpiryWorker(mockRepo, mockRemover, nil)
	job := newTestManualTierOverrideExpiryJob(ManualTierOverrideExpiryCheckArgs{TriggerSource: "periodic"})

	err := worker.Work(ctx, job)

	require.Error(t, err)
	mockRemover.AssertNotCalled(t, "RemoveUserTierOverride")
}

// TestNewManualTierOverrideExpiryCheckWorker_DefaultLogger tests nil logger uses default.
func TestNewManualTierOverrideExpiryCheckWorker_DefaultLogger(t *testing.T) {
	mockRepo := new(mockManualTierOverrideProfileRepository)
	mockRemover := new(mockManualTierOverrideRemover)

	worker := NewManualTierOverrideExpiryCheckWorker(ManualTierOverrideExpiryCheckWorkerDeps{
		ProfileRepo: mockRepo,
		Remover:     mockRemover,
		// Logger nil — should use default
	})

	assert.NotNil(t, worker.logger)
}

// TestNewManualTierOverrideExpiryCheckWorker_DefaultRetentionMonths tests 0 months uses default (6).
func TestNewManualTierOverrideExpiryCheckWorker_DefaultRetentionMonths(t *testing.T) {
	mockRepo := new(mockManualTierOverrideProfileRepository)
	mockRemover := new(mockManualTierOverrideRemover)

	worker := NewManualTierOverrideExpiryCheckWorker(ManualTierOverrideExpiryCheckWorkerDeps{
		ProfileRepo:     mockRepo,
		Remover:         mockRemover,
		RetentionMonths: 0, // Should default to 6
	})

	assert.Equal(t, 6, worker.retentionMonths)
}

// TestManualTierOverrideExpiryCheckUniqueOpts tests the unique opts configuration.
func TestManualTierOverrideExpiryCheckUniqueOpts(t *testing.T) {
	opts := ManualTierOverrideExpiryCheckUniqueOpts()

	assert.False(t, opts.ByArgs)
	assert.Equal(t, 23*time.Hour, opts.ByPeriod)
}
