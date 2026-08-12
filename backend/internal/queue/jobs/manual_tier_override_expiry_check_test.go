// Package jobs contains River job definitions and workers for async processing.
// Issue #1413 Task 9: Manual tier override expiry check job tests.
package jobs

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	phclient "github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"
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
// AdminUserRepository.RemoveUserTierOverrideIfMatches method this job reuses instead of
// duplicating its CTE (Issue #1414 / Task 7; Issue #1413 High finding H4, Codex review round 3).
type mockManualTierOverrideRemover struct {
	mock.Mock
}

func (m *mockManualTierOverrideRemover) RemoveUserTierOverrideIfMatches(ctx context.Context, actorUserID, targetUserID, reason string, retentionMonths int, expectedOverride *string, expectedEndsAt *time.Time) (repository.RemoveTierOverrideRow, bool, error) {
	args := m.Called(ctx, actorUserID, targetUserID, reason, retentionMonths, expectedOverride, expectedEndsAt)
	if args.Get(0) == nil {
		return repository.RemoveTierOverrideRow{}, args.Bool(1), args.Error(2)
	}
	return args.Get(0).(repository.RemoveTierOverrideRow), args.Bool(1), args.Error(2)
}

// Test helpers

func newTestManualTierOverrideExpiryJob(args ManualTierOverrideExpiryCheckArgs) *river.Job[ManualTierOverrideExpiryCheckArgs] {
	return &river.Job[ManualTierOverrideExpiryCheckArgs]{
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
// passed to the shared RemoveUserTierOverrideIfMatches repository method with a system (empty)
// actor, along with the override/expiry values observed at select time.
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
	mockRemover.On("RemoveUserTierOverrideIfMatches", ctx, "", "user-expired", mock.AnythingOfType("string"), 6, &override, &endsAt).
		Return(repository.RemoveTierOverrideRow{Removed: true, RevertedTier: "premium", RevertedStatus: "cancelled"}, true, nil)

	worker := newTestManualTierOverrideExpiryWorker(mockRepo, mockRemover, nil)
	job := newTestManualTierOverrideExpiryJob(ManualTierOverrideExpiryCheckArgs{TriggerSource: "periodic"})

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockRepo.AssertExpectations(t)
	mockRemover.AssertExpectations(t)
}

// TestManualTierOverrideExpiryCheck_LiveOverride_LeftAlone proves the belt-and-braces guard:
// a candidate whose override is still active (per domain.User.HasActiveManualOverride) at the
// moment the worker processes it must NOT be passed to RemoveUserTierOverrideIfMatches —
// protects against a stale candidate list when an admin extends/re-applies a comp between the
// query and this loop. RemoveUserTierOverrideIfMatches is deliberately left unstubbed: a real
// call would panic.
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
	mockRemover.AssertNotCalled(t, "RemoveUserTierOverrideIfMatches", mock.Anything, mock.Anything, "user-live", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

// TestManualTierOverrideExpiryCheck_ClaimLost_SkipsWithoutError proves Issue #1413 High finding
// H4 (Codex review round 3) is actually closed: when RemoveUserTierOverrideIfMatches reports
// claimed=false (an admin extended/replaced the override between the select and this call), the
// worker must treat it as a normal skip — no error, no Clerk sync, no audit follow-up — rather
// than acting as if the removal succeeded. The mock's row is deliberately non-zero (unlike the
// real repository's actual claimed=false contract, which always returns a zero row) specifically
// to isolate the WORKER's own gating on `claimed`, so a missing check can't hide behind
// syncClerkMetadata's separate empty-ClerkUserID no-op.
func TestManualTierOverrideExpiryCheck_ClaimLost_SkipsWithoutError(t *testing.T) {
	ctx := context.Background()

	override := "household_2"
	endsAt := time.Now().Add(-1 * time.Hour) // expired at select time
	users := []*domain.User{
		{ID: "user-extended", ManualTierOverride: &override, ManualTierEndsAt: &endsAt},
	}

	mockRepo := new(mockManualTierOverrideProfileRepository)
	mockRemover := new(mockManualTierOverrideRemover)
	mockClerk := new(mockClerkService)

	mockRepo.On("GetUsersWithExpiredManualTierOverride", ctx).Return(users, nil)
	mockRemover.On("RemoveUserTierOverrideIfMatches", ctx, "", "user-extended", mock.AnythingOfType("string"), 6, &override, &endsAt).
		Return(repository.RemoveTierOverrideRow{ClerkUserID: "should-not-sync", RevertedTier: "premium", RevertedStatus: "cancelled"}, false, nil)
	mockClerk.On("UpdateUserMetadata", ctx, "should-not-sync", mock.Anything).Return(nil).Maybe()

	worker := newTestManualTierOverrideExpiryWorker(mockRepo, mockRemover, mockClerk)
	job := newTestManualTierOverrideExpiryJob(ManualTierOverrideExpiryCheckArgs{TriggerSource: "periodic"})

	err := worker.Work(ctx, job)

	require.NoError(t, err, "a lost claim is a normal skip, not a job failure")
	mockClerk.AssertNotCalled(t, "UpdateUserMetadata", mock.Anything, mock.Anything, mock.Anything)
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
// re-syncs Clerk using the REVERTED tier/status returned by RemoveUserTierOverrideIfMatches (not
// any stale value), mirroring AdminUserService.RemoveUserTierOverride's own sync semantics.
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
	mockRemover.On("RemoveUserTierOverrideIfMatches", ctx, "", "user-1", mock.AnythingOfType("string"), 6, &override, &endsAt).
		Return(repository.RemoveTierOverrideRow{
			Removed:        true,
			RevertedTier:   "premium",
			RevertedStatus: "cancelled",
			ClerkUserID:    "clerk_user_1",
		}, true, nil)
	// #1413 AC-15 (Task W, Codex review): assert the Clerk payload key-by-key, matching the
	// standard the three admin-removal tests use (admin_user_service_test.go) — mock.Anything or
	// a 2-field check would let a regression that re-emits a stale premium claim after an
	// override lapses (or drops the "tier": nil poisoned-user cleanup, or ADDS an extra key) pass
	// silently. This is the expiry path's twin of that same regression: the exact AC-15 failure
	// mode this test family exists to catch. len(m) == 3 pins the payload to exactly
	// {subscription_tier, subscription_status, tier} — domain.User.ClerkPublicMetadata's full key
	// set, no more, no less.
	mockClerk.On("UpdateUserMetadata", ctx, "clerk_user_1", mock.MatchedBy(func(m map[string]interface{}) bool {
		tierVal, hasTierKey := m["subscription_tier"]
		statusVal, hasStatusKey := m["subscription_status"]
		nilTierVal, hasNilTierKey := m["tier"]
		return len(m) == 3 &&
			hasTierKey && tierVal == "premium" &&
			hasStatusKey && statusVal == "cancelled" &&
			hasNilTierKey && nilTierVal == nil
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
	mockRemover.On("RemoveUserTierOverrideIfMatches", ctx, "", "user-1", mock.AnythingOfType("string"), 6, &override, &endsAt).
		Return(repository.RemoveTierOverrideRow{Removed: true, RevertedTier: "premium", RevertedStatus: "cancelled", ClerkUserID: "clerk_user_1"}, true, nil)
	// #1413 AC-15 (Task W, Codex review): assert key-by-key even though this call errors — the
	// payload actually sent must still be correct (a failed sync should not mask what would have
	// gone out), same standard as the test above and the admin-removal family
	// (admin_user_service_test.go). mock.Anything here was the last remaining instance of this
	// branch's persistent test-honesty defect in this file: it would pass regardless of what
	// payload the worker actually constructed.
	mockClerk.On("UpdateUserMetadata", ctx, "clerk_user_1", mock.MatchedBy(func(m map[string]interface{}) bool {
		tierVal, hasTierKey := m["subscription_tier"]
		statusVal, hasStatusKey := m["subscription_status"]
		nilTierVal, hasNilTierKey := m["tier"]
		return len(m) == 3 &&
			hasTierKey && tierVal == "premium" &&
			hasStatusKey && statusVal == "cancelled" &&
			hasNilTierKey && nilTierVal == nil
	})).Return(errors.New("clerk unavailable"))

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
	mockRemover.On("RemoveUserTierOverrideIfMatches", ctx, "", "user-1", mock.AnythingOfType("string"), 6, &override, &endsAt).
		Return(repository.RemoveTierOverrideRow{Removed: true, RevertedTier: "premium", RevertedStatus: "cancelled", ClerkUserID: "clerk_user_1"}, true, nil)

	worker := newTestManualTierOverrideExpiryWorker(mockRepo, mockRemover, nil)
	job := newTestManualTierOverrideExpiryJob(ManualTierOverrideExpiryCheckArgs{TriggerSource: "periodic"})

	require.NotPanics(t, func() {
		err := worker.Work(ctx, job)
		require.NoError(t, err)
	})
}

// --- Error resilience ---

// TestManualTierOverrideExpiryCheck_OneUserFails_OthersStillProcessed proves one user's
// RemoveUserTierOverrideIfMatches failure does not stall the batch — mirrors
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
	mockRemover.On("RemoveUserTierOverrideIfMatches", ctx, "", "user-fail", mock.AnythingOfType("string"), 6, &override, &endsAt).
		Return(nil, false, errors.New("db error"))
	mockRemover.On("RemoveUserTierOverrideIfMatches", ctx, "", "user-ok", mock.AnythingOfType("string"), 6, &override, &endsAt).
		Return(repository.RemoveTierOverrideRow{Removed: true, RevertedTier: "premium", RevertedStatus: "cancelled"}, true, nil)

	worker := newTestManualTierOverrideExpiryWorker(mockRepo, mockRemover, nil)
	job := newTestManualTierOverrideExpiryJob(ManualTierOverrideExpiryCheckArgs{TriggerSource: "periodic"})

	err := worker.Work(ctx, job)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "1 of 2")
	mockRemover.AssertNumberOfCalls(t, "RemoveUserTierOverrideIfMatches", 2)
}

// TestManualTierOverrideExpiryCheck_AggregateFailure_LoggedAtError is the observability proof
// (Issue #1413 C1 follow-up, "unbounded exclusion"): this job clearing manual_tier_override is the
// ONLY thing that bounds how long a lapsed comp can delay a retention notice/deletion. Even now
// that the job runs with RunOnStart: true (see ManualTierOverrideExpiryCheckPeriodicOpts — it was
// `false`, which meant the job could be enqueued zero times ever under a sub-daily deploy cadence),
// a failed run's retries still eventually land in the dead-letter queue with no automatic replay,
// so if this job silently stops working, that "delay" becomes
// indefinite. This test proves a run with per-user failures logs an explicit ERROR-level summary
// (on top of the per-user Error logs and the jobs_completed_total{status="failed"} metric the
// deferred func in Work() already increments — this repo's established River-job observability
// mechanism), so the failure is actually observable rather than only visible in Info-level logs.
func TestManualTierOverrideExpiryCheck_AggregateFailure_LoggedAtError(t *testing.T) {
	ctx := context.Background()

	override := "household_2"
	endsAt := time.Now().Add(-1 * time.Hour)
	users := []*domain.User{
		{ID: "user-fail-obs", ManualTierOverride: &override, ManualTierEndsAt: &endsAt},
	}

	mockRepo := new(mockManualTierOverrideProfileRepository)
	mockRemover := new(mockManualTierOverrideRemover)

	mockRepo.On("GetUsersWithExpiredManualTierOverride", ctx).Return(users, nil)
	mockRemover.On("RemoveUserTierOverrideIfMatches", ctx, "", "user-fail-obs", mock.AnythingOfType("string"), 6, &override, &endsAt).
		Return(nil, false, errors.New("db unavailable"))

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))

	worker := NewManualTierOverrideExpiryCheckWorker(ManualTierOverrideExpiryCheckWorkerDeps{
		ProfileRepo:     mockRepo,
		Remover:         mockRemover,
		RetentionMonths: 6,
		Logger:          logger,
	})
	job := newTestManualTierOverrideExpiryJob(ManualTierOverrideExpiryCheckArgs{TriggerSource: "periodic"})

	err := worker.Work(ctx, job)
	require.Error(t, err, "an aggregate per-user failure must still surface as a job error so River retries")

	// Note: a bare "level=ERROR" substring check would NOT be falsifiable here on its own — the
	// per-user "failed to expire manual tier override" log a few lines above is already ERROR
	// level, so that alone can't distinguish "the aggregate summary log exists" from "it doesn't".
	// The line below, tied to the exact aggregate message this fix adds, is what actually catches
	// its removal — and matched against "level=ERROR ... msg=" together, it also confirms the
	// summary is logged at ERROR (not Info/Warn), not merely present in the log at some level.
	logOutput := logBuf.String()
	assert.Regexp(t, `level=ERROR msg="manual tier override expiry check completed with failures"`, logOutput,
		"an aggregate expiry-check failure must be logged at ERROR, not just Info, so it is actually observable")
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
	mockRemover.AssertNotCalled(t, "RemoveUserTierOverrideIfMatches")
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
	mockRemover.AssertNotCalled(t, "RemoveUserTierOverrideIfMatches")
}

// --- Last-success heartbeat observability (Issue #1413 Task P4) ---

// TestManualTierOverrideExpiryCheck_Success_RecordsHeartbeat proves a successful run advances
// observability.JobLastSuccessTimestamp for this job's kind — the generic mechanism an alert reads
// to detect "override-expiry has not succeeded in > 48h", independent of (and stronger than)
// jobs_completed_total{status="failed"}, which never fires at all if the job silently stops
// running altogether.
func TestManualTierOverrideExpiryCheck_Success_RecordsHeartbeat(t *testing.T) {
	ctx := context.Background()

	mockRepo := new(mockManualTierOverrideProfileRepository)
	mockRemover := new(mockManualTierOverrideRemover)
	mockRepo.On("GetUsersWithExpiredManualTierOverride", ctx).Return([]*domain.User{}, nil)

	worker := newTestManualTierOverrideExpiryWorker(mockRepo, mockRemover, nil)
	job := newTestManualTierOverrideExpiryJob(ManualTierOverrideExpiryCheckArgs{TriggerSource: "periodic"})

	before := time.Now().Unix()
	err := worker.Work(ctx, job)
	require.NoError(t, err)

	got := testutil.ToFloat64(observability.JobLastSuccessTimestamp.WithLabelValues(job.Kind))
	assert.GreaterOrEqual(t, got, float64(before),
		"a successful run must advance the last-success heartbeat for this job's kind")
}

// TestManualTierOverrideExpiryCheck_FetchError_HeartbeatNotAdvanced proves a FAILED run does NOT
// advance the heartbeat — advancing it on failure would defeat the entire point of the metric
// (a job stuck failing every day would look identical to a healthy one).
func TestManualTierOverrideExpiryCheck_FetchError_HeartbeatNotAdvanced(t *testing.T) {
	ctx := context.Background()

	mockRepo := new(mockManualTierOverrideProfileRepository)
	mockRemover := new(mockManualTierOverrideRemover)
	mockRepo.On("GetUsersWithExpiredManualTierOverride", ctx).Return(nil, errors.New("connection refused"))

	worker := newTestManualTierOverrideExpiryWorker(mockRepo, mockRemover, nil)
	job := newTestManualTierOverrideExpiryJob(ManualTierOverrideExpiryCheckArgs{TriggerSource: "periodic"})

	before := testutil.ToFloat64(observability.JobLastSuccessTimestamp.WithLabelValues(job.Kind))
	err := worker.Work(ctx, job)
	require.Error(t, err)

	after := testutil.ToFloat64(observability.JobLastSuccessTimestamp.WithLabelValues(job.Kind))
	assert.Equal(t, before, after, "a failed run must NOT advance the last-success heartbeat")
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

	// Issue #1413 Task W (Codex review): ByState previously omitted `Completed`, which River's
	// own default (rivertype.UniqueOptsByStateDefault(), river@v0.43.0) includes. Per
	// UniqueOpts.ByState's doc comment (insert_opts.go): a completed job of the same kind, "not
	// yet cleaned out by the system," still blocks a duplicate unique insert — WITHOUT Completed
	// in this list, an already-successfully-completed run within the current 23h ByPeriod bucket
	// does NOT block a duplicate, so every RunOnStart catch-up insert on every process
	// restart/redeploy succeeds regardless of whether this job already ran this period. On a
	// repo that deploys several times a day, that turned a once-daily job — the ONLY thing
	// bounding how long noManualOverrideAtAllPredicate keeps a user out of the retention pipeline
	// — into a run-every-boot job.
	assert.Contains(t, opts.ByState, queue.JobStateCompleted,
		"ByState must include Completed — its absence is what let RunOnStart's catch-up insert "+
			"double-fire against an already-completed run within the same ByPeriod window "+
			"(Issue #1413 Task W)")

	// River requires these four states whenever ByState is customized (insert_opts.go: omitting
	// them "would put River in a difficult position when moving jobs between common parts of the
	// state machine and finding a unique conflict already there"). Asserted individually — not
	// merely via non-empty — so a future edit cannot silently drop one.
	assert.Contains(t, opts.ByState, queue.JobStateAvailable)
	assert.Contains(t, opts.ByState, queue.JobStatePending)
	assert.Contains(t, opts.ByState, queue.JobStateRunning)
	assert.Contains(t, opts.ByState, queue.JobStateScheduled)

	// Issue #1413 Task W: deliberately NOT Cancelled or Discarded. River's canonical default
	// (rivertype.UniqueOptsByStateDefault(), which this list is now brought into exact agreement
	// with) excludes both — its own doc comment states a new unique job "may be inserted even if
	// another job already exists, as long as that other job is set `cancelled` or `discarded`."
	// (This contradicts a looser prose aside in UniqueOpts.ByState's OWN doc comment in the same
	// package version, which calls completed-or-discarded jobs both blocking; the canonical
	// rivertype.UniqueOptsByStateDefault() implementation and ITS doc comment are what actually
	// executes, and they agree Discarded does not block.) Independently of that default, adding
	// Discarded here would be actively wrong for this specific job: Discarded means every retry
	// was exhausted without the job ever completing its expiry/re-arm/audit work — the opposite
	// of "already handled this period." Suppressing a RunOnStart catch-up insert because a prior
	// attempt discarded would let lapsed manual tier overrides go silently uncleared (and users
	// stay stuck out of the retention pipeline) for up to river's
	// DiscardedJobRetentionPeriodDefault (7 days) before another chance to run — precisely what
	// RunOnStart exists to prevent. Cancelled is excluded for the same reasoning (a cancelled run
	// also never completed the work) and is currently unreachable anyway: nothing in this
	// codebase cancels a manual_tier_override_expiry_check job.
	assert.NotContains(t, opts.ByState, queue.JobStateCancelled)
	assert.NotContains(t, opts.ByState, queue.JobStateDiscarded)
}

// ─── Issue #1413 G3: main.go must actually consume ManualTierOverrideExpiryCheckUniqueOpts ──
//
// main.go's periodic-job registration passed only &river.InsertOpts{Queue: "low"} — a
// hand-written literal that never called ManualTierOverrideExpiryCheckUniqueOpts() (tested
// above) at all, so the dedup it documents was never actually enforced in production: nothing
// stopped two overlapping daily runs (or a duplicate periodic-job registration from a
// redeployed/rolling River client) from both claiming the same lapsed overrides. That job is the
// ONLY thing bounding how long noManualOverrideAtAllPredicate keeps a user out of the retention
// pipeline (profile_repository.go), so duplicate or missed runs directly affect whether accounts
// are ever deleted.
//
// ManualTierOverrideExpiryCheckInsertOpts extracts the FULL *river.InsertOpts construction (not
// just the UniqueOpts) into this package so main.go can call a single function instead of
// hand-writing a literal — main.go is not unit-testable itself, so this extraction is what turns
// TestManualTierOverrideExpiryCheckInsertOpts_CarriesUniqueOpts below from an assertion about an
// unconsumed struct into a real guarantee about what main.go's periodic-job registration ships,
// once main.go calls this function directly (no more separate hand-copied literal to drift from
// it, unlike the sibling jobs' RetentionCleanupCheckUniqueOpts / HouseholdMemberCleanupCheckUniqueOpts
// / HouseholdOverCapacityCheckUniqueOpts, which main.go still hand-duplicates inline).

// TestManualTierOverrideExpiryCheckInsertOpts_Queue verifies the extracted InsertOpts helper
// sets Queue: "low", matching every other periodic maintenance job registered in main.go.
func TestManualTierOverrideExpiryCheckInsertOpts_Queue(t *testing.T) {
	opts := ManualTierOverrideExpiryCheckInsertOpts()

	assert.Equal(t, "low", opts.Queue)
}

// TestManualTierOverrideExpiryCheckInsertOpts_CarriesUniqueOpts is the RED test for G3: proves
// the InsertOpts this function returns — the value main.go's periodic-job registration will
// call directly once wired — carries river-native UniqueOpts matching
// ManualTierOverrideExpiryCheckUniqueOpts() field for field. Before this fix, main.go's
// &river.InsertOpts{Queue: "low"} literal had a zero-value UniqueOpts (ByArgs: false,
// ByPeriod: 0, ByState: nil), which River treats as "no uniqueness constraint at all" — this
// test would have failed against that literal.
func TestManualTierOverrideExpiryCheckInsertOpts_CarriesUniqueOpts(t *testing.T) {
	opts := ManualTierOverrideExpiryCheckInsertOpts()
	want := ManualTierOverrideExpiryCheckUniqueOpts()

	assert.Equal(t, want.ByArgs, opts.UniqueOpts.ByArgs)
	assert.Equal(t, want.ByPeriod, opts.UniqueOpts.ByPeriod)
	assert.Equal(t, want.ByQueue, opts.UniqueOpts.ByQueue)

	require.Len(t, opts.UniqueOpts.ByState, len(want.ByState),
		"ByState must carry every state ManualTierOverrideExpiryCheckUniqueOpts() documents — an "+
			"extra or missing entry changes which job states are deduplicated against")
	for i, s := range want.ByState {
		assert.Equal(t, rivertype.JobState(s), opts.UniqueOpts.ByState[i])
	}
}

// ─── Issue #1413 P0: RunOnStart must be true ───────────────────────────────────────────────
//
// River's periodic scheduler keeps only in-memory state (river.NewPeriodicJob's own doc
// comment): every process restart or leader election recomputes nextRunAt as ScheduleFunc(now)
// unless PeriodicJobOpts.ID is set (it isn't, here or anywhere in main.go), and skips the
// catch-up insert entirely when RunOnStart is false (river@v0.43.0
// internal/maintenance/periodic_job_enqueuer.go:409-426). This repo deploys several times a
// day, so RunOnStart: false let this daily job — the ONLY thing bounding how long
// noManualOverrideAtAllPredicate keeps a user out of the retention pipeline — be enqueued zero
// times, ever, silently leaving lapsed manual tier overrides uncleared forever.

// TestManualTierOverrideExpiryCheckPeriodicOpts_RunOnStart is the RED test for Issue #1413 P0.
func TestManualTierOverrideExpiryCheckPeriodicOpts_RunOnStart(t *testing.T) {
	opts := ManualTierOverrideExpiryCheckPeriodicOpts()

	require.NotNil(t, opts)
	assert.True(t, opts.RunOnStart,
		"RunOnStart must be true — without it, a process restart or leader election (this repo "+
			"deploys several times a day) can push this daily job's next-run time forward forever, "+
			"leaving lapsed manual tier overrides uncleared and users out of the retention pipeline "+
			"indefinitely (Issue #1413 P0)")
}

// TestManualTierOverrideExpiryCheckPeriodicOpts_PairedWithUniqueOpts proves RunOnStart: true is
// only safe here because ManualTierOverrideExpiryCheckInsertOpts()'s dedup (asserted above)
// exists alongside it — the two are only safe together. A future edit that weakens
// ManualTierOverrideExpiryCheckUniqueOpts() to a zero value while leaving RunOnStart: true in
// place would let a start-of-process catch-up run double-fire alongside an already-scheduled
// run; this test fails first.
func TestManualTierOverrideExpiryCheckPeriodicOpts_ID(t *testing.T) {
	opts := ManualTierOverrideExpiryCheckPeriodicOpts()

	require.NotNil(t, opts)
	assert.Equal(t, "manual_tier_override_expiry_check", opts.ID)
}

func TestManualTierOverrideExpiryCheckPeriodicOpts_PairedWithUniqueOpts(t *testing.T) {
	periodicOpts := ManualTierOverrideExpiryCheckPeriodicOpts()
	require.True(t, periodicOpts.RunOnStart, "this pairing guarantee only matters when RunOnStart is true")

	insertOpts := ManualTierOverrideExpiryCheckInsertOpts()
	require.NotNil(t, insertOpts)
	assert.NotZero(t, insertOpts.UniqueOpts.ByPeriod,
		"RunOnStart: true's catch-up insert is only safe when the paired InsertOpts carries a "+
			"non-zero ByPeriod window")
	require.NotEmpty(t, insertOpts.UniqueOpts.ByState,
		"RunOnStart: true's catch-up insert is only safe when the paired InsertOpts carries a "+
			"non-empty ByState constraint")
}

// --- Analytics wiring tests (Issue #1413 Task MB, hardened by Task NB) ---
//
// EventAdminSubscriptionOverrideExpired was a dead constant (posthog/events.go) with zero call
// sites until Task MB. It has no frontend twin (backend-only, system-initiated — Task NB's
// EventAdminSubscriptionOverride{Applied,Removed}Server rename doesn't apply to it) and stays on
// the raw phClient.Capture path (main.go's operational-event carve-out; see this worker's own
// analytics call site). These tests use MockAnalytics, already defined in this package by
// email_change_propagation_test.go (a testify mock.Mock, not a no-op stub) — reused rather than
// declaring a second identically-shaped type, per this package's existing convention
// (subscription_webhook_test.go's mockAnalyticsClient is a separate testify mock only because it
// needs its own doc comment; MockAnalytics is the general-purpose one already exported within
// this package for exactly this reuse).
//
// Several assertions below were strengthened per a second Codex round that found the ORIGINAL
// Task MB tests could not fail against specific wrong implementations (each documented at its own
// call site).

// actorUserIDIsPresentAndNil asserts that props carries the literal key "actor_user_id" with a
// nil value — NOT merely that indexing the map for that key returns nil. Go's map index
// expression returns the zero value (nil, for map[string]interface{}) both when a key is
// genuinely present with a nil value AND when the key is entirely absent from the map, so a bare
// `props["actor_user_id"] == nil` check cannot tell "no acting admin" (correct, expected shape)
// apart from "the key was silently dropped" (a real regression). The comma-ok form disambiguates.
func actorUserIDIsPresentAndNil(props map[string]interface{}) bool {
	v, ok := props["actor_user_id"]
	return ok && v == nil
}

// TestManualTierOverrideExpiryCheck_FiresAnalyticsEvent_ForClaimedUser proves a successfully
// expired (claimed) user fires EventAdminSubscriptionOverrideExpired with
// {tier, had_expiry, actor_user_id} — a DISTINCT event from EventAdminSubscriptionOverrideRemovedServer
// (never reused: an automatic expiry and an admin's deliberate removal are different events), with
// actor_user_id PRESENT and nil (no acting admin — system-initiated; see actorUserIDIsPresentAndNil's
// own doc comment for why a bare `== nil` check cannot be trusted here).
// MockGDPRAnalytics mocks phclient.GDPRAnalytics — the CONSENT-GATED client this worker moved to in
// Issue #1413 round 10. Deliberately a separate type from MockAnalytics (email_change_propagation_test.go),
// which mocks the raw phclient.Analytics interface: the whole point of the round-10 change is that this
// worker must call CaptureIfConsented and NOT Capture, so the two mocks must not be interchangeable.
// A test asserting on the wrong method name now fails to compile rather than silently passing.
type MockGDPRAnalytics struct {
	mock.Mock
}

func (m *MockGDPRAnalytics) CaptureIfConsented(ctx context.Context, userID, event string, properties map[string]interface{}) {
	m.Called(ctx, userID, event, properties)
}

func TestManualTierOverrideExpiryCheck_FiresAnalyticsEvent_ForClaimedUser(t *testing.T) {
	ctx := context.Background()

	override := "household_2"
	endsAt := time.Now().Add(-1 * time.Hour)
	users := []*domain.User{
		{ID: "user-expired", ManualTierOverride: &override, ManualTierEndsAt: &endsAt},
	}

	mockRepo := new(mockManualTierOverrideProfileRepository)
	mockRemover := new(mockManualTierOverrideRemover)
	mockGDPRAnalytics := new(MockGDPRAnalytics)

	mockRepo.On("GetUsersWithExpiredManualTierOverride", ctx).Return(users, nil)
	mockRemover.On("RemoveUserTierOverrideIfMatches", ctx, "", "user-expired", mock.AnythingOfType("string"), 6, &override, &endsAt).
		Return(repository.RemoveTierOverrideRow{Removed: true, RevertedTier: "premium", RevertedStatus: "cancelled"}, true, nil)
	mockGDPRAnalytics.On("CaptureIfConsented", mock.Anything, "user-expired", phclient.EventAdminSubscriptionOverrideExpired, mock.MatchedBy(func(props map[string]interface{}) bool {
		return len(props) == 3 &&
			props["tier"] == override &&
			props["had_expiry"] == true &&
			actorUserIDIsPresentAndNil(props)
	})).Return()

	worker := newTestManualTierOverrideExpiryWorker(mockRepo, mockRemover, nil)
	worker.SetGDPRAnalytics(mockGDPRAnalytics)
	job := newTestManualTierOverrideExpiryJob(ManualTierOverrideExpiryCheckArgs{TriggerSource: "periodic"})

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockGDPRAnalytics.AssertExpectations(t)
}

// TestManualTierOverrideExpiryCheck_FiresAnalyticsEvent_HadExpiryFalseWhenNoEndsAt proves
// had_expiry is DERIVED from user.ManualTierEndsAt (nil -> false), not a hardcoded literal true.
// Without this test, the sibling test above (which always observes a real, already-lapsed
// ManualTierEndsAt) cannot distinguish a correct `user.ManualTierEndsAt != nil` expression from a
// hardcoded `"had_expiry": true` — both produce identical output for every candidate this worker's
// real SQL query can ever actually return (GetUsersWithExpiredManualTierOverride's own doc
// comment: candidates always have a real, passed manual_tier_ends_at). This test drives the
// worker directly via the mocked repository with a synthetic candidate that has no override at
// all (ManualTierOverride nil) — deliberately outside what the real query would ever return — so
// that HasActiveManualOverride() returns false (not "skip: still active") and this iteration
// reaches the analytics call with a nil ManualTierEndsAt, isolating the had_expiry computation
// from the worker's own belt-and-braces guard.
func TestManualTierOverrideExpiryCheck_FiresAnalyticsEvent_HadExpiryFalseWhenNoEndsAt(t *testing.T) {
	ctx := context.Background()

	users := []*domain.User{
		{ID: "user-no-override", ManualTierOverride: nil, ManualTierEndsAt: nil},
	}

	mockRepo := new(mockManualTierOverrideProfileRepository)
	mockRemover := new(mockManualTierOverrideRemover)
	mockGDPRAnalytics := new(MockGDPRAnalytics)

	mockRepo.On("GetUsersWithExpiredManualTierOverride", ctx).Return(users, nil)
	mockRemover.On("RemoveUserTierOverrideIfMatches", ctx, "", "user-no-override", mock.AnythingOfType("string"), 6, (*string)(nil), (*time.Time)(nil)).
		Return(repository.RemoveTierOverrideRow{Removed: false, RevertedTier: "premium", RevertedStatus: "cancelled"}, true, nil)
	mockGDPRAnalytics.On("CaptureIfConsented", mock.Anything, "user-no-override", phclient.EventAdminSubscriptionOverrideExpired, mock.MatchedBy(func(props map[string]interface{}) bool {
		return len(props) == 3 &&
			props["tier"] == "" &&
			props["had_expiry"] == false &&
			actorUserIDIsPresentAndNil(props)
	})).Return()

	worker := newTestManualTierOverrideExpiryWorker(mockRepo, mockRemover, nil)
	worker.SetGDPRAnalytics(mockGDPRAnalytics)
	job := newTestManualTierOverrideExpiryJob(ManualTierOverrideExpiryCheckArgs{TriggerSource: "periodic"})

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockGDPRAnalytics.AssertExpectations(t)
}

// TestManualTierOverrideExpiryCheck_ClaimLost_NoAnalyticsEvent proves a lost claim (an admin
// changed the override concurrently — Issue #1413 High finding H4) fires no analytics event: the
// worker did not actually expire anything, so there is nothing to report.
//
// The mocked RemoveUserTierOverrideIfMatches return deliberately pairs claimed=false with a
// NON-zero row (Removed: true, populated Reverted* fields) — a combination the real repository
// implementation never actually produces (it always returns a zero-value row alongside
// claimed=false), but one this mock-driven unit test can construct on purpose specifically to
// prove the worker's "don't fire analytics" decision is keyed off the claimed bool, not off
// row.Removed. The original Task MB fixture returned a zero-value row for both, so an
// implementation that (wrongly) gated analytics on `!row.Removed` instead of `!claimed` would
// have still passed it — both being false/zero by coincidence of that fixture.
func TestManualTierOverrideExpiryCheck_ClaimLost_NoAnalyticsEvent(t *testing.T) {
	ctx := context.Background()

	override := "household_2"
	endsAt := time.Now().Add(-1 * time.Hour)
	users := []*domain.User{
		{ID: "user-extended", ManualTierOverride: &override, ManualTierEndsAt: &endsAt},
	}

	mockRepo := new(mockManualTierOverrideProfileRepository)
	mockRemover := new(mockManualTierOverrideRemover)
	mockGDPRAnalytics := new(MockGDPRAnalytics)

	mockRepo.On("GetUsersWithExpiredManualTierOverride", ctx).Return(users, nil)
	mockRemover.On("RemoveUserTierOverrideIfMatches", ctx, "", "user-extended", mock.AnythingOfType("string"), 6, &override, &endsAt).
		Return(repository.RemoveTierOverrideRow{Removed: true, RevertedTier: "premium", RevertedStatus: "cancelled"}, false, nil)

	worker := newTestManualTierOverrideExpiryWorker(mockRepo, mockRemover, nil)
	worker.SetGDPRAnalytics(mockGDPRAnalytics)
	job := newTestManualTierOverrideExpiryJob(ManualTierOverrideExpiryCheckArgs{TriggerSource: "periodic"})

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockGDPRAnalytics.AssertNotCalled(t, "CaptureIfConsented", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

// TestManualTierOverrideExpiryCheck_RemoveFails_NoAnalyticsEvent proves a per-user
// RemoveUserTierOverrideIfMatches failure fires no analytics event — the operation did not
// succeed for that user, so capture must not claim it did.
//
// As with the ClaimLost test above, the mocked return pairs a real error with a NON-zero row
// (Removed: true) — proving the worker's "don't fire" decision is keyed off the error, not off
// row.Removed (which the real repository implementation always zeroes on error anyway; this is a
// mock-only construction to isolate the worker's own branching logic).
func TestManualTierOverrideExpiryCheck_RemoveFails_NoAnalyticsEvent(t *testing.T) {
	ctx := context.Background()

	override := "household_2"
	endsAt := time.Now().Add(-1 * time.Hour)
	users := []*domain.User{
		{ID: "user-fail", ManualTierOverride: &override, ManualTierEndsAt: &endsAt},
	}

	mockRepo := new(mockManualTierOverrideProfileRepository)
	mockRemover := new(mockManualTierOverrideRemover)
	mockGDPRAnalytics := new(MockGDPRAnalytics)

	mockRepo.On("GetUsersWithExpiredManualTierOverride", ctx).Return(users, nil)
	mockRemover.On("RemoveUserTierOverrideIfMatches", ctx, "", "user-fail", mock.AnythingOfType("string"), 6, &override, &endsAt).
		Return(repository.RemoveTierOverrideRow{Removed: true, RevertedTier: "premium", RevertedStatus: "cancelled"}, false, errors.New("db error"))

	worker := newTestManualTierOverrideExpiryWorker(mockRepo, mockRemover, nil)
	worker.SetGDPRAnalytics(mockGDPRAnalytics)
	job := newTestManualTierOverrideExpiryJob(ManualTierOverrideExpiryCheckArgs{TriggerSource: "periodic"})

	err := worker.Work(ctx, job)

	require.Error(t, err)
	mockGDPRAnalytics.AssertNotCalled(t, "CaptureIfConsented", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

// TestManualTierOverrideExpiryCheck_NilAnalytics_NoPanic proves the worker is safe to use
// without ever calling SetGDPRAnalytics — the real-world state in local dev/QA where PostHog is
// unconfigured. A nil-pointer panic here would break the daily expiry sweep entirely.
//
// Paired positive assertion (second Codex round): the no-client phase above proves only "no
// panic" — an implementation that silently dropped every event, injected client or not, would
// still pass it. The second phase below, on a FRESH worker instance with a real mock client
// injected, proves the SAME Work() call still fires the event when a client IS present.
func TestManualTierOverrideExpiryCheck_NilAnalytics_NoPanic(t *testing.T) {
	ctx := context.Background()

	override := "household_2"
	endsAt := time.Now().Add(-1 * time.Hour)
	users := []*domain.User{
		{ID: "user-expired", ManualTierOverride: &override, ManualTierEndsAt: &endsAt},
	}

	mockRepo := new(mockManualTierOverrideProfileRepository)
	mockRemover := new(mockManualTierOverrideRemover)

	mockRepo.On("GetUsersWithExpiredManualTierOverride", ctx).Return(users, nil)
	mockRemover.On("RemoveUserTierOverrideIfMatches", ctx, "", "user-expired", mock.AnythingOfType("string"), 6, &override, &endsAt).
		Return(repository.RemoveTierOverrideRow{Removed: true, RevertedTier: "premium", RevertedStatus: "cancelled"}, true, nil)

	worker := newTestManualTierOverrideExpiryWorker(mockRepo, mockRemover, nil) // SetGDPRAnalytics never called

	job := newTestManualTierOverrideExpiryJob(ManualTierOverrideExpiryCheckArgs{TriggerSource: "periodic"})

	require.NotPanics(t, func() {
		err := worker.Work(ctx, job)
		require.NoError(t, err)
	})

	mockRepo.AssertExpectations(t)
	mockRemover.AssertExpectations(t)

	// Paired positive case: same candidate/call shape, but with a client injected on a fresh
	// worker instance.
	mockRepoWithClient := new(mockManualTierOverrideProfileRepository)
	mockRemoverWithClient := new(mockManualTierOverrideRemover)
	mockGDPRAnalytics := new(MockGDPRAnalytics)

	mockRepoWithClient.On("GetUsersWithExpiredManualTierOverride", ctx).Return(users, nil)
	mockRemoverWithClient.On("RemoveUserTierOverrideIfMatches", ctx, "", "user-expired", mock.AnythingOfType("string"), 6, &override, &endsAt).
		Return(repository.RemoveTierOverrideRow{Removed: true, RevertedTier: "premium", RevertedStatus: "cancelled"}, true, nil)
	mockGDPRAnalytics.On("CaptureIfConsented", mock.Anything, "user-expired", phclient.EventAdminSubscriptionOverrideExpired, mock.Anything).Return()

	workerWithClient := newTestManualTierOverrideExpiryWorker(mockRepoWithClient, mockRemoverWithClient, nil)
	workerWithClient.SetGDPRAnalytics(mockGDPRAnalytics)

	err := workerWithClient.Work(ctx, job)
	require.NoError(t, err)

	mockGDPRAnalytics.AssertExpectations(t)
}
