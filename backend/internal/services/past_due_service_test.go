// Package services provides business logic and external service integrations.
// Tests for PastDueService — Issue #898: dunning grace-expiry logic.
package services

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// ---- Mocks ----

type mockPastDueProfileRepo struct {
	mock.Mock
}

func (m *mockPastDueProfileRepo) GetUsersInPastDue(ctx context.Context) ([]domain.User, error) {
	args := m.Called(ctx)
	return args.Get(0).([]domain.User), args.Error(1)
}

func (m *mockPastDueProfileRepo) GetUserByID(ctx context.Context, userID string) (*domain.User, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.User), args.Error(1)
}

// CancelIfPastDueOccurrenceWithRetention replaces the earlier separate CancelIfPastDue +
// SetRetentionDates pair (Issue #1413 round-3 finding H5 residual) and is itself the
// occurrence-scoped successor to CancelIfPastDueWithRetention (Issue #1413 round-4 finding H1) —
// see PastDueProfileRepository's doc comment.
func (m *mockPastDueProfileRepo) CancelIfPastDueOccurrenceWithRetention(ctx context.Context, userID string, expectedPastDueStartedAt, retentionStarted time.Time, retentionMonths int) (bool, error) {
	args := m.Called(ctx, userID, expectedPastDueStartedAt, retentionStarted, retentionMonths)
	return args.Bool(0), args.Error(1)
}

// ClearPastDueStartedAtIfMatches is ClearPastDueStartedAt's occurrence-scoped sibling (Issue
// #1413 round-4 finding H3) — see PastDueProfileRepository's doc comment.
func (m *mockPastDueProfileRepo) ClearPastDueStartedAtIfMatches(ctx context.Context, userID string, expected time.Time) (bool, error) {
	args := m.Called(ctx, userID, expected)
	return args.Bool(0), args.Error(1)
}

// ClearPlaidCleanupPendingIfMatches is Issue #1413 round-12 Fix 1's CAS-guarded marker clear,
// called after processGraceExpiry's synchronous Plaid disconnect (Step 1) plus CAS flip
// (Steps 3-4) both succeed — see PastDueProfileRepository's doc comment.
func (m *mockPastDueProfileRepo) ClearPlaidCleanupPendingIfMatches(ctx context.Context, userID string, expected time.Time) (bool, error) {
	args := m.Called(ctx, userID, expected)
	return args.Bool(0), args.Error(1)
}

type mockPastDuePlaidCleanup struct {
	mock.Mock
}

// DisconnectAllUserItemsIfEligible is the only entry point PastDuePlaidCleanup has left: its
// nil-recheck twin, DisconnectAllUserItems, was deleted outright by Issue #1500 once BOTH callers
// in this service (processGraceExpiry, then processPlaidDisconnectWindow) had been converted. It
// deliberately records itself under the "DisconnectAllUserItems"
// expectation name via MethodCalled: every pre-existing expectation and assertion in this file
// asserts "the flow does / does not disconnect this user's Plaid items", which is equally true of
// both entry points, and re-keying a dozen call sites would say nothing new. What this mock cannot
// show is the per-item behaviour the recheck exists for — a mock that treats a disconnect as one
// atomic call cannot — so THAT is asserted against a loop-simulating fake in
// past_due_grace_recheck_test.go instead.
func (m *mockPastDuePlaidCleanup) DisconnectAllUserItemsIfEligible(ctx context.Context, userID string, recheck func(ctx context.Context) (bool, error)) error {
	if recheck == nil {
		return fmt.Errorf("DisconnectAllUserItemsIfEligible: an eligibility recheck is required")
	}
	args := m.MethodCalled("DisconnectAllUserItems", ctx, userID)
	return args.Error(0)
}

type mockPastDuePaddleClient struct {
	mock.Mock
}

func (m *mockPastDuePaddleClient) CancelSubscription(ctx context.Context, subscriptionID string) error {
	args := m.Called(ctx, subscriptionID)
	return args.Error(0)
}

type mockPastDueClerkClient struct {
	mock.Mock
}

func (m *mockPastDueClerkClient) UpdateUserMetadata(ctx context.Context, clerkUserID string, metadata map[string]interface{}) error {
	args := m.Called(ctx, clerkUserID, metadata)
	return args.Error(0)
}

type mockPastDueNotificationRepo struct {
	mock.Mock
}

func (m *mockPastDueNotificationRepo) Create(ctx context.Context, n *domain.Notification) error {
	args := m.Called(ctx, n)
	return args.Error(0)
}

// ---- Helpers ----

const (
	testGraceDays           = 21
	testPlaidDisconnectDays = 14
)

func newTestPastDueService(
	repo *mockPastDueProfileRepo,
	plaid *mockPastDuePlaidCleanup,
	paddle *mockPastDuePaddleClient,
	clerk *mockPastDueClerkClient,
	notifications *mockPastDueNotificationRepo,
) *PastDueService {
	svc := NewPastDueService(repo, plaid, paddle, clerk, notifications, testGraceDays, testPlaidDisconnectDays)
	// #1413 H6a/H6b (Codex review round 4): PastDueService now requires a work claims repo for
	// EXCLUSIVITY before any irreversible Plaid/Paddle call. Wire a permissive default so tests
	// exercising OTHER behavior aren't forced to plumb claim expectations through; tests that
	// exercise the claim mechanism ITSELF override via svc.SetWorkClaimsRepo(customMock).
	svc.SetWorkClaimsRepo(newAllowingPastDueClaimsRepo())
	return svc
}

// testPastDueClaimToken is the fixed fencing token newAllowingPastDueClaimsRepo's Claim stub
// returns. Tests that only care about PastDueService's OTHER behavior can rely on it implicitly;
// tests exercising the claim mechanism itself supply their own MockWorkClaimsRepository instead.
const testPastDueClaimToken = "test-past-due-claim-token"

// newAllowingPastDueClaimsRepo returns a MockWorkClaimsRepository (defined in
// deletion_service_test.go — same package) stubbed to always grant and complete BOTH past_due
// work claims (ActionPlaidDisconnectPastDue, ActionPastDueGraceCancel) — for tests that exercise
// PastDueService's OTHER behavior and aren't themselves testing the claim mechanism. userID is
// wildcarded so one instance works across this file's whole fixture matrix. Mirrors
// deletion_service_test.go's newAllowingClaimsRepo.
func newAllowingPastDueClaimsRepo() *MockWorkClaimsRepository {
	m := new(MockWorkClaimsRepository)
	for _, action := range []string{repository.ActionPlaidDisconnectPastDue, repository.ActionPastDueGraceCancel} {
		m.On("Claim", mock.Anything, repository.ResourceTypeUser, mock.Anything, action, pastDueClaimLease).
			Return(testPastDueClaimToken, true, nil).Maybe()
		m.On("Complete", mock.Anything, repository.ResourceTypeUser, mock.Anything, action, testPastDueClaimToken).
			Return(true, nil).Maybe()
		m.On("Release", mock.Anything, repository.ResourceTypeUser, mock.Anything, action, testPastDueClaimToken, mock.AnythingOfType("string")).
			Return(true, nil).Maybe()
	}

	// Issue #1413 H1: processGraceExpiry's Paddle-cancel step nests ActionPaddleCancelSubscription
	// inside the ActionPastDueGraceCancel flow claim above — permissive so tests exercising
	// OTHER behavior aren't forced to plumb this through. Never Completed, only Released — see
	// claimSideEffectAndRun's doc comment (plaid_item_cleanup_service.go) for why.
	m.On("Claim", mock.Anything, repository.ResourceTypeUser, mock.Anything, repository.ActionPaddleCancelSubscription, paddleCancelSideEffectLease).
		Return(testPastDuePaddleCancelSideEffectToken, true, nil).Maybe()
	m.On("Release", mock.Anything, repository.ResourceTypeUser, mock.Anything, repository.ActionPaddleCancelSubscription, testPastDuePaddleCancelSideEffectToken, mock.AnythingOfType("string")).
		Return(true, nil).Maybe()
	return m
}

// testPastDuePaddleCancelSideEffectToken is the fixed fencing token newAllowingPastDueClaimsRepo's
// permissive ActionPaddleCancelSubscription stub returns.
const testPastDuePaddleCancelSideEffectToken = "test-past-due-paddle-cancel-side-effect-token"

// pastDueOccurrenceResourceID computes the composite work_claims.resource_id expected for a
// past_due-cycle occurrence (Issue #1413 P5) — mirrors production's
// repository.OccurrenceResourceID(userID, repository.FormatOccurrenceTimestamp(user.PastDueStartedAt)),
// which processGraceExpiry/processPlaidDisconnectWindow now use instead of the bare userID, so
// that a completed grace-cancel/Plaid-disconnect in ONE dunning cycle does not permanently block
// the SAME action in a later cycle.
func pastDueOccurrenceResourceID(t *testing.T, userID string, pastDueStartedAt time.Time) string {
	t.Helper()
	resourceID, err := repository.OccurrenceResourceID(userID, repository.FormatOccurrenceTimestamp(&pastDueStartedAt))
	require.NoError(t, err)
	return resourceID
}

func userWithPastDue(userID string, pastDueStartedAt time.Time) domain.User {
	subID := "sub_test123"
	return domain.User{
		ID:                   userID,
		ClerkUserID:          "clerk_" + userID,
		SubscriptionStatus:   string(domain.SubscriptionStatusPastDue),
		SubscriptionTier:     "premium",
		PaddleSubscriptionID: &subID,
		PastDueStartedAt:     &pastDueStartedAt,
	}
}

// mockHouseholdCheckerForPastDue mocks the household-coverage exemption hook (#1413),
// mirroring mockHouseholdCheckerForTrial in trial_service_test.go.
type mockHouseholdCheckerForPastDue struct{ mock.Mock }

func (m *mockHouseholdCheckerForPastDue) IsCoveredByHousehold(ctx context.Context, userID string) (bool, error) {
	args := m.Called(ctx, userID)
	return args.Bool(0), args.Error(1)
}

// userWithPastDueOverride returns a past_due user carrying a manual tier override ending at
// overrideEndsAt. A zero overrideEndsAt means "no end date" (override never expires).
func userWithPastDueOverride(userID string, pastDueStartedAt time.Time, overrideEndsAt time.Time) domain.User {
	u := userWithPastDue(userID, pastDueStartedAt)
	override := "household_2"
	u.ManualTierOverride = &override
	if !overrideEndsAt.IsZero() {
		u.ManualTierEndsAt = &overrideEndsAt
	}
	return u
}

// ---- Tests ----

// Test 1: Full grace expiry flow — all steps called in correct order
func TestPastDueService_ProcessUser_GraceExpiry_FullFlow(t *testing.T) {
	repo := &mockPastDueProfileRepo{}
	plaid := &mockPastDuePlaidCleanup{}
	paddle := &mockPastDuePaddleClient{}
	clerk := &mockPastDueClerkClient{}
	notif := &mockPastDueNotificationRepo{}

	svc := newTestPastDueService(repo, plaid, paddle, clerk, notif)

	userID := "user-grace-full"
	now := time.Now()
	pastDueStarted := now.Add(-time.Duration(testGraceDays+1) * 24 * time.Hour)
	user := userWithPastDue(userID, pastDueStarted)
	subID := *user.PaddleSubscriptionID

	// All steps should succeed; re-read confirms still past_due.
	repo.On("GetUserByID", mock.Anything, userID).Return(&user, nil)
	plaid.On("DisconnectAllUserItems", mock.Anything, userID).Return(nil)
	paddle.On("CancelSubscription", mock.Anything, subID).Return(nil)
	repo.On("CancelIfPastDueOccurrenceWithRetention", mock.Anything, userID, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time"), mock.AnythingOfType("int")).Return(true, nil)
	repo.On("ClearPastDueStartedAtIfMatches", mock.Anything, userID, mock.AnythingOfType("time.Time")).Return(true, nil)
	// #1413 G7: tightened from a subscription_status-only check to the full canonical payload
	// (subscription_tier, subscription_status, AND the orphaned-key "tier": nil cleanup),
	// matching the sibling assertion at admin_user_service_test.go's
	// TestAdminUserService_UpdateUserTier_PushesEffectiveOverrideTier_Household4. The previous
	// matcher would pass unchanged even if subscription_tier were wrong or the "tier" key were
	// missing entirely.
	clerk.On("UpdateUserMetadata", mock.Anything, user.ClerkUserID, mock.MatchedBy(func(m map[string]interface{}) bool {
		tierVal, hasTierKey := m["subscription_tier"]
		statusVal, hasStatusKey := m["subscription_status"]
		nilTierVal, hasNilTierKey := m["tier"]
		return hasTierKey && tierVal == "premium" &&
			hasStatusKey && statusVal == "cancelled" &&
			hasNilTierKey && nilTierVal == nil
	})).Return(nil)
	notif.On("Create", mock.Anything, mock.MatchedBy(func(n *domain.Notification) bool {
		return n.Type == "subscription_cancelled" && n.UserID == userID
	})).Return(nil)

	err := svc.ProcessPastDueUser(context.Background(), user, now)
	require.NoError(t, err)

	repo.AssertCalled(t, "GetUserByID", mock.Anything, userID)
	plaid.AssertCalled(t, "DisconnectAllUserItems", mock.Anything, userID)
	paddle.AssertCalled(t, "CancelSubscription", mock.Anything, subID)
	repo.AssertCalled(t, "CancelIfPastDueOccurrenceWithRetention", mock.Anything, userID, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time"), mock.AnythingOfType("int"))
	repo.AssertCalled(t, "ClearPastDueStartedAtIfMatches", mock.Anything, userID, mock.AnythingOfType("time.Time"))
	clerk.AssertCalled(t, "UpdateUserMetadata", mock.Anything, user.ClerkUserID, mock.Anything)
	notif.AssertCalled(t, "Create", mock.Anything, mock.Anything)
}

// TestPastDueService_ProcessUser_GraceExpiry_SuccessfulDisconnect_ClearsPlaidMarker is the Issue
// #1413 round-12 Fix 1 (Blocker) RED test for the past-due path. Ordering here is Plaid disconnect
// FIRST (Step 1), then the CAS flip that WRITES plaid_cleanup_pending_at (Steps 3-4) — so by the
// time the flip succeeds, a synchronous disconnect has ALREADY completed cleanup for this episode.
// Before this fix, the marker the flip just wrote was never cleared, leaving the invariant violated
// even though nothing is actually outstanding.
//
// Falsifiability: reverting processGraceExpiry to omit the post-flip clear step makes this test
// fail — ClearPlaidCleanupPendingIfMatches would never be called.
func TestPastDueService_ProcessUser_GraceExpiry_SuccessfulDisconnect_ClearsPlaidMarker(t *testing.T) {
	repo := &mockPastDueProfileRepo{}
	plaid := &mockPastDuePlaidCleanup{}
	paddle := &mockPastDuePaddleClient{}
	clerk := &mockPastDueClerkClient{}
	notif := &mockPastDueNotificationRepo{}

	svc := newTestPastDueService(repo, plaid, paddle, clerk, notif)

	userID := "user-grace-clears-marker"
	now := time.Now()
	pastDueStarted := now.Add(-time.Duration(testGraceDays+1) * 24 * time.Hour)
	user := userWithPastDue(userID, pastDueStarted)
	subID := *user.PaddleSubscriptionID

	pendingSince := time.Now().Add(-time.Minute)
	postFlipUser := userWithPastDue(userID, pastDueStarted)
	postFlipUser.SubscriptionStatus = string(domain.SubscriptionStatusCancelled)
	postFlipUser.PlaidCleanupPendingAt = &pendingSince

	// The THREE reReadStillPastDue calls (pre-claim, post-claim re-verify, and — since the past_due
	// sibling fix — one more immediately before the Paddle cancel, because Step 1 is a network
	// round-trip) must all see the user as STILL past_due to proceed; the clear step's OWN read
	// (after the CAS flip) sees the row as it stands AFTER
	// CancelIfPastDueOccurrenceWithRetention's real SQL would have written it.
	//
	// THE COUNT IS LOAD-BEARING, which is why this is Times(3) and not a single unbounded stub: the
	// stub ordering is what makes the post-flip read see the CANCELLED row. If a future change adds
	// or removes a pre-flip re-read, this test fails loudly rather than silently feeding the wrong
	// snapshot to a re-check.
	repo.On("GetUserByID", mock.Anything, userID).Return(&user, nil).Times(3)
	repo.On("GetUserByID", mock.Anything, userID).Return(&postFlipUser, nil)
	plaid.On("DisconnectAllUserItems", mock.Anything, userID).Return(nil)
	paddle.On("CancelSubscription", mock.Anything, subID).Return(nil)
	repo.On("CancelIfPastDueOccurrenceWithRetention", mock.Anything, userID, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time"), mock.AnythingOfType("int")).Return(true, nil)
	repo.On("ClearPlaidCleanupPendingIfMatches", mock.Anything, userID, pendingSince).Return(true, nil)
	repo.On("ClearPastDueStartedAtIfMatches", mock.Anything, userID, mock.AnythingOfType("time.Time")).Return(true, nil)
	clerk.On("UpdateUserMetadata", mock.Anything, user.ClerkUserID, mock.Anything).Return(nil)
	notif.On("Create", mock.Anything, mock.Anything).Return(nil)

	err := svc.ProcessPastDueUser(context.Background(), user, now)
	require.NoError(t, err)

	repo.AssertCalled(t, "ClearPlaidCleanupPendingIfMatches", mock.Anything, userID, pendingSince)
}

// TestPastDueService_ProcessUser_GraceExpiry_ClearMarkerFails_RetriesThenLogsErrorNotEscalated is
// the Issue #1413 #1453 (Codex review, PR #1443 follow-up) RED test: before this fix, a SINGLE
// failed ClearPlaidCleanupPendingIfMatches attempt here was logged at Warn and swallowed — a
// transient DB error at exactly this moment left plaid_cleanup_pending_at stuck non-NULL forever,
// since the disconnect that just succeeded already removed the live linked item
// GetUsersNeedingPlaidDisconnectRepair's own predicate requires to ever reselect this user again.
// This brings the past-due path into line with RetentionCleanupCheckWorker.repairPlaidDisconnects's
// established pattern (retention_cleanup_check.go): bounded retry, then an Error-level log (not
// Warn) plus the shared failure counter once every attempt is exhausted.
//
// Falsifiability: reverting processGraceExpiry's clear step to a single unretried attempt makes
// this test fail on the AssertNumberOfCalls assertion below (1 call, not
// plaidCleanupMarkerClearMaxAttempts) and on the log-level assertion (no "level=ERROR" line — the
// single-attempt code logs the failure at Warn only).
func TestPastDueService_ProcessUser_GraceExpiry_ClearMarkerFails_RetriesThenLogsErrorNotEscalated(t *testing.T) {
	repo := &mockPastDueProfileRepo{}
	plaid := &mockPastDuePlaidCleanup{}
	paddle := &mockPastDuePaddleClient{}
	clerk := &mockPastDueClerkClient{}
	notif := &mockPastDueNotificationRepo{}

	svc := newTestPastDueService(repo, plaid, paddle, clerk, notif)
	var logBuf bytes.Buffer
	svc.logger = slog.New(slog.NewTextHandler(&logBuf, nil))

	userID := "user-grace-clear-marker-fails"
	now := time.Now()
	pastDueStarted := now.Add(-time.Duration(testGraceDays+1) * 24 * time.Hour)
	user := userWithPastDue(userID, pastDueStarted)
	subID := *user.PaddleSubscriptionID

	pendingSince := time.Now().Add(-time.Minute)
	postFlipUser := userWithPastDue(userID, pastDueStarted)
	postFlipUser.SubscriptionStatus = string(domain.SubscriptionStatusCancelled)
	postFlipUser.PlaidCleanupPendingAt = &pendingSince

	// Times(3), not Twice: see the sibling test above for why the pre-flip re-read count is three.
	repo.On("GetUserByID", mock.Anything, userID).Return(&user, nil).Times(3)
	repo.On("GetUserByID", mock.Anything, userID).Return(&postFlipUser, nil)
	plaid.On("DisconnectAllUserItems", mock.Anything, userID).Return(nil)
	paddle.On("CancelSubscription", mock.Anything, subID).Return(nil)
	repo.On("CancelIfPastDueOccurrenceWithRetention", mock.Anything, userID, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time"), mock.AnythingOfType("int")).Return(true, nil)
	clearErr := errors.New("db unavailable")
	repo.On("ClearPlaidCleanupPendingIfMatches", mock.Anything, userID, pendingSince).Return(false, clearErr)
	repo.On("ClearPastDueStartedAtIfMatches", mock.Anything, userID, mock.AnythingOfType("time.Time")).Return(true, nil)
	clerk.On("UpdateUserMetadata", mock.Anything, user.ClerkUserID, mock.Anything).Return(nil)
	notif.On("Create", mock.Anything, mock.Anything).Return(nil)

	err := svc.ProcessPastDueUser(context.Background(), user, now)

	require.NoError(t, err, "a persistently-failing marker clear is best-effort tidying, not correctness-critical for THIS cycle — it must not fail the whole grace-expiry flow")
	repo.AssertNumberOfCalls(t, "ClearPlaidCleanupPendingIfMatches", plaidCleanupMarkerClearMaxAttempts)

	logOutput := logBuf.String()
	assert.Contains(t, logOutput, "level=ERROR", "exhausting every retry must be logged at ERROR, not swallowed at Warn")
	assert.Contains(t, logOutput, userID, "the ERROR log must carry the identifying user_id")
}

// Test 2: Plaid failure stops the flow — no cancel, no status flip
func TestPastDueService_ProcessUser_GraceExpiry_PlaidFails_DoesNotCancel(t *testing.T) {
	repo := &mockPastDueProfileRepo{}
	plaid := &mockPastDuePlaidCleanup{}
	paddle := &mockPastDuePaddleClient{}
	clerk := &mockPastDueClerkClient{}
	notif := &mockPastDueNotificationRepo{}

	svc := newTestPastDueService(repo, plaid, paddle, clerk, notif)

	userID := "user-grace-plaid-fail"
	now := time.Now()
	pastDueStarted := now.Add(-time.Duration(testGraceDays+1) * 24 * time.Hour)
	user := userWithPastDue(userID, pastDueStarted)

	repo.On("GetUserByID", mock.Anything, userID).Return(&user, nil)
	plaid.On("DisconnectAllUserItems", mock.Anything, userID).Return(errors.New("plaid API error"))

	err := svc.ProcessPastDueUser(context.Background(), user, now)
	require.Error(t, err, "should propagate Plaid error so the job retries tomorrow")

	repo.AssertCalled(t, "GetUserByID", mock.Anything, userID)
	paddle.AssertNotCalled(t, "CancelSubscription", mock.Anything, mock.Anything)
	repo.AssertNotCalled(t, "CancelIfPastDueOccurrenceWithRetention", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	repo.AssertNotCalled(t, "ClearPastDueStartedAtIfMatches", mock.Anything, mock.Anything, mock.Anything)
	clerk.AssertNotCalled(t, "UpdateUserMetadata", mock.Anything, mock.Anything, mock.Anything)
	notif.AssertNotCalled(t, "Create", mock.Anything, mock.Anything)
	// Issue #1413 round-12 Fix 1 positive control: a FAILED disconnect must leave
	// plaid_cleanup_pending_at untouched (still set, once the flip even runs) — the clear only ever
	// follows a SUCCESSFUL disconnect AND CAS flip, neither of which happen here.
	repo.AssertNotCalled(t, "ClearPlaidCleanupPendingIfMatches", mock.Anything, mock.Anything, mock.Anything)
}

// Test 3: Paddle already-cancelled error is tolerated — flow continues
func TestPastDueService_ProcessUser_GraceExpiry_PaddleCancelFails_Tolerated(t *testing.T) {
	repo := &mockPastDueProfileRepo{}
	plaid := &mockPastDuePlaidCleanup{}
	paddle := &mockPastDuePaddleClient{}
	clerk := &mockPastDueClerkClient{}
	notif := &mockPastDueNotificationRepo{}

	svc := newTestPastDueService(repo, plaid, paddle, clerk, notif)

	userID := "user-grace-paddle-already-cancelled"
	now := time.Now()
	pastDueStarted := now.Add(-time.Duration(testGraceDays+1) * 24 * time.Hour)
	user := userWithPastDue(userID, pastDueStarted)
	subID := *user.PaddleSubscriptionID

	repo.On("GetUserByID", mock.Anything, userID).Return(&user, nil)
	plaid.On("DisconnectAllUserItems", mock.Anything, userID).Return(nil)
	// Paddle returns already-cancelled error — should be tolerated
	paddle.On("CancelSubscription", mock.Anything, subID).Return(ErrPaddleAlreadyCancelled)
	repo.On("CancelIfPastDueOccurrenceWithRetention", mock.Anything, userID, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time"), mock.AnythingOfType("int")).Return(true, nil)
	repo.On("ClearPastDueStartedAtIfMatches", mock.Anything, userID, mock.AnythingOfType("time.Time")).Return(true, nil)
	clerk.On("UpdateUserMetadata", mock.Anything, user.ClerkUserID, mock.Anything).Return(nil)
	notif.On("Create", mock.Anything, mock.Anything).Return(nil)

	err := svc.ProcessPastDueUser(context.Background(), user, now)
	require.NoError(t, err, "already-cancelled Paddle error should be tolerated")

	repo.AssertCalled(t, "CancelIfPastDueOccurrenceWithRetention", mock.Anything, userID, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time"), mock.AnythingOfType("int"))
}

// TestPastDueService_ProcessUser_GraceExpiry_PaddleSideEffectClaimBusy_Aborts_ReleasesFlowClaim
// is the H1 gating test at the CALLER boundary: when the OPERATION-keyed
// ActionPaddleCancelSubscription claim is held by another flow (e.g. a concurrent account
// deletion), processGraceExpiry must never call the Paddle collaborator, must release its OWN
// ActionPastDueGraceCancel flow claim, and must propagate the error so the daily job logs it and
// retries tomorrow — exactly like any other Step 2 failure.
func TestPastDueService_ProcessUser_GraceExpiry_PaddleSideEffectClaimBusy_Aborts_ReleasesFlowClaim(t *testing.T) {
	repo := &mockPastDueProfileRepo{}
	plaid := &mockPastDuePlaidCleanup{}
	paddle := &mockPastDuePaddleClient{}
	clerk := &mockPastDueClerkClient{}
	notif := &mockPastDueNotificationRepo{}

	svc := newTestPastDueService(repo, plaid, paddle, clerk, notif)
	mockClaims := new(MockWorkClaimsRepository)
	svc.SetWorkClaimsRepo(mockClaims)

	userID := "user-grace-paddle-side-effect-busy"
	now := time.Now()
	pastDueStarted := now.Add(-time.Duration(testGraceDays+1) * 24 * time.Hour)
	user := userWithPastDue(userID, pastDueStarted)
	resourceID := pastDueOccurrenceResourceID(t, userID, pastDueStarted)

	repo.On("GetUserByID", mock.Anything, userID).Return(&user, nil)
	mockClaims.On("Claim", mock.Anything, repository.ResourceTypeUser, resourceID, repository.ActionPastDueGraceCancel, pastDueClaimLease).
		Return(testPastDueClaimToken, true, nil)
	plaid.On("DisconnectAllUserItems", mock.Anything, userID).Return(nil)
	// Another flow (e.g. account deletion) already holds the side-effect claim. Side-effect
	// claims stay keyed on the BARE userID (unchanged by #1413 P5) — see
	// plaid_item_cleanup_service.go's claimSideEffectAndRun.
	mockClaims.On("Claim", mock.Anything, repository.ResourceTypeUser, userID, repository.ActionPaddleCancelSubscription, paddleCancelSideEffectLease).
		Return("", false, nil)
	mockClaims.On("Release", mock.Anything, repository.ResourceTypeUser, resourceID, repository.ActionPastDueGraceCancel, testPastDueClaimToken, mock.AnythingOfType("string")).
		Return(true, nil)
	// paddle.CancelSubscription is deliberately left unstubbed — an unstubbed testify mock call
	// panics, proving the collaborator is never reached.

	err := svc.ProcessPastDueUser(context.Background(), user, now)

	require.Error(t, err, "a busy side-effect claim must propagate so the daily job retries")
	assert.ErrorIs(t, err, ErrSideEffectClaimBusy)
	paddle.AssertNotCalled(t, "CancelSubscription", mock.Anything, mock.Anything)
	repo.AssertNotCalled(t, "CancelIfPastDueOccurrenceWithRetention", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	mockClaims.AssertCalled(t, "Release", mock.Anything, repository.ResourceTypeUser, resourceID, repository.ActionPastDueGraceCancel, testPastDueClaimToken, mock.AnythingOfType("string"))
	mockClaims.AssertNotCalled(t, "Release", mock.Anything, repository.ResourceTypeUser, userID, repository.ActionPaddleCancelSubscription, mock.Anything, mock.Anything)
}

// Test 4: Plaid-only window (14 <= age < 21) — DisconnectAllUserItems called
func TestPastDueService_ProcessUser_PlaidDisconnect_AgeInWindow(t *testing.T) {
	repo := &mockPastDueProfileRepo{}
	plaid := &mockPastDuePlaidCleanup{}
	paddle := &mockPastDuePaddleClient{}
	clerk := &mockPastDueClerkClient{}
	notif := &mockPastDueNotificationRepo{}

	svc := newTestPastDueService(repo, plaid, paddle, clerk, notif)

	userID := "user-plaid-window"
	now := time.Now()
	// Age is 15 days: in the plaid-disconnect window but before grace expiry
	pastDueStarted := now.Add(-15 * 24 * time.Hour)
	user := userWithPastDue(userID, pastDueStarted)

	// Re-read confirms still past_due
	repo.On("GetUserByID", mock.Anything, userID).Return(&user, nil)
	plaid.On("DisconnectAllUserItems", mock.Anything, userID).Return(nil)
	notif.On("Create", mock.Anything, mock.Anything).Return(nil)

	err := svc.ProcessPastDueUser(context.Background(), user, now)
	require.NoError(t, err)

	plaid.AssertCalled(t, "DisconnectAllUserItems", mock.Anything, userID)
	notif.AssertCalled(t, "Create", mock.Anything, mock.Anything)
	paddle.AssertNotCalled(t, "CancelSubscription", mock.Anything, mock.Anything)
	repo.AssertNotCalled(t, "CancelIfPastDueOccurrenceWithRetention", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

// Test 4b (H6a): reReadStillPastDue passes, but the work claim itself is lost — e.g. an admin
// granted a comp, or the user recovered, in the narrow window between the re-read and this call,
// or another worker already holds/completed the claim. Proves the race reReadStillPastDue alone
// cannot close: DisconnectAllUserItems must NOT run.
func TestPastDueService_ProcessUser_PlaidDisconnect_ClaimLost_SkipsDisconnect(t *testing.T) {
	repo := &mockPastDueProfileRepo{}
	plaid := &mockPastDuePlaidCleanup{}
	paddle := &mockPastDuePaddleClient{}
	clerk := &mockPastDueClerkClient{}
	notif := &mockPastDueNotificationRepo{}

	svc := newTestPastDueService(repo, plaid, paddle, clerk, notif)
	mockClaims := new(MockWorkClaimsRepository)
	svc.SetWorkClaimsRepo(mockClaims)

	userID := "user-plaid-claim-lost"
	now := time.Now()
	pastDueStarted := now.Add(-15 * 24 * time.Hour)
	user := userWithPastDue(userID, pastDueStarted)
	resourceID := pastDueOccurrenceResourceID(t, userID, pastDueStarted)

	repo.On("GetUserByID", mock.Anything, userID).Return(&user, nil)
	// The claim reports false — another worker holds it, or it's already completed.
	mockClaims.On("Claim", mock.Anything, repository.ResourceTypeUser, resourceID, repository.ActionPlaidDisconnectPastDue, pastDueClaimLease).
		Return("", false, nil)

	err := svc.ProcessPastDueUser(context.Background(), user, now)
	require.NoError(t, err, "a lost claim is a normal skip, not a job failure")

	mockClaims.AssertCalled(t, "Claim", mock.Anything, repository.ResourceTypeUser, resourceID, repository.ActionPlaidDisconnectPastDue, pastDueClaimLease)
	plaid.AssertNotCalled(t, "DisconnectAllUserItems", mock.Anything, mock.Anything)
	notif.AssertNotCalled(t, "Create", mock.Anything, mock.Anything)
	mockClaims.AssertNotCalled(t, "Release", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

// TestPastDueService_ProcessUser_PlaidDisconnect_ClaimWon_PlaidFails_ReleasesClaim proves H6a's
// "claim won + external failure" direction: the claim must be Released (not left to expire, not
// silently swallowed) and the failure must propagate so the daily job retries. Regression target:
// the OLD processPlaidDisconnectWindow logged this error and continued — the exact swallowed-
// failure anti-pattern #1413 replaces the guard mechanism to fix.
func TestPastDueService_ProcessUser_PlaidDisconnect_ClaimWon_PlaidFails_ReleasesClaim(t *testing.T) {
	repo := &mockPastDueProfileRepo{}
	plaid := &mockPastDuePlaidCleanup{}
	paddle := &mockPastDuePaddleClient{}
	clerk := &mockPastDueClerkClient{}
	notif := &mockPastDueNotificationRepo{}

	svc := newTestPastDueService(repo, plaid, paddle, clerk, notif)
	mockClaims := new(MockWorkClaimsRepository)
	svc.SetWorkClaimsRepo(mockClaims)

	userID := "user-plaid-claim-fail"
	now := time.Now()
	pastDueStarted := now.Add(-15 * 24 * time.Hour)
	user := userWithPastDue(userID, pastDueStarted)
	resourceID := pastDueOccurrenceResourceID(t, userID, pastDueStarted)

	plaidErr := errors.New("plaid API error")
	repo.On("GetUserByID", mock.Anything, userID).Return(&user, nil)
	mockClaims.On("Claim", mock.Anything, repository.ResourceTypeUser, resourceID, repository.ActionPlaidDisconnectPastDue, pastDueClaimLease).
		Return(testPastDueClaimToken, true, nil)
	plaid.On("DisconnectAllUserItems", mock.Anything, userID).Return(plaidErr)
	mockClaims.On("Release", mock.Anything, repository.ResourceTypeUser, resourceID, repository.ActionPlaidDisconnectPastDue, testPastDueClaimToken, plaidErr.Error()).
		Return(true, nil)

	err := svc.ProcessPastDueUser(context.Background(), user, now)
	require.Error(t, err, "Plaid failure must propagate so the daily job retries")

	mockClaims.AssertCalled(t, "Release", mock.Anything, repository.ResourceTypeUser, resourceID, repository.ActionPlaidDisconnectPastDue, testPastDueClaimToken, plaidErr.Error())
	notif.AssertNotCalled(t, "Create", mock.Anything, mock.Anything)
	mockClaims.AssertNotCalled(t, "Complete", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

// TestPastDueService_ProcessUser_PlaidDisconnect_ClaimWon_Success_CompletesClaim proves H6a's
// positive direction end to end: a genuinely eligible user's claim is won, the disconnect runs,
// and Complete is called with the SAME token Claim returned.
func TestPastDueService_ProcessUser_PlaidDisconnect_ClaimWon_Success_CompletesClaim(t *testing.T) {
	repo := &mockPastDueProfileRepo{}
	plaid := &mockPastDuePlaidCleanup{}
	paddle := &mockPastDuePaddleClient{}
	clerk := &mockPastDueClerkClient{}
	notif := &mockPastDueNotificationRepo{}

	svc := newTestPastDueService(repo, plaid, paddle, clerk, notif)
	mockClaims := new(MockWorkClaimsRepository)
	svc.SetWorkClaimsRepo(mockClaims)

	userID := "user-plaid-claim-complete"
	now := time.Now()
	pastDueStarted := now.Add(-15 * 24 * time.Hour)
	user := userWithPastDue(userID, pastDueStarted)
	resourceID := pastDueOccurrenceResourceID(t, userID, pastDueStarted)

	repo.On("GetUserByID", mock.Anything, userID).Return(&user, nil)
	mockClaims.On("Claim", mock.Anything, repository.ResourceTypeUser, resourceID, repository.ActionPlaidDisconnectPastDue, pastDueClaimLease).
		Return(testPastDueClaimToken, true, nil)
	plaid.On("DisconnectAllUserItems", mock.Anything, userID).Return(nil)
	notif.On("Create", mock.Anything, mock.Anything).Return(nil)
	mockClaims.On("Complete", mock.Anything, repository.ResourceTypeUser, resourceID, repository.ActionPlaidDisconnectPastDue, testPastDueClaimToken).
		Return(true, nil)

	err := svc.ProcessPastDueUser(context.Background(), user, now)
	require.NoError(t, err)

	mockClaims.AssertCalled(t, "Complete", mock.Anything, repository.ResourceTypeUser, resourceID, repository.ActionPlaidDisconnectPastDue, testPastDueClaimToken)
	plaid.AssertCalled(t, "DisconnectAllUserItems", mock.Anything, userID)
}

// TestPastDueService_ProcessUser_PlaidDisconnect_CompleteReturnsFalse_LoggedNotSwallowed proves a
// lost lease at Complete time (another worker reclaimed this tuple while we were mid-flight) does
// not silently pass as success and is not reported as a processing failure — mirrors
// deletion_service_test.go's TestExecuteDeletion_CompleteReturnsFalse_LoggedNotSwallowed. Unlike
// that test, PastDueService's logger is a stored field (captured from slog.Default() at
// construction), so it's swapped for a buffer-backed logger here and the resulting ERROR-level log
// record from processPlaidDisconnectWindow's Complete handling is asserted directly, not merely
// inspected in the source.
func TestPastDueService_ProcessUser_PlaidDisconnect_CompleteReturnsFalse_LoggedNotSwallowed(t *testing.T) {
	repo := &mockPastDueProfileRepo{}
	plaid := &mockPastDuePlaidCleanup{}
	paddle := &mockPastDuePaddleClient{}
	clerk := &mockPastDueClerkClient{}
	notif := &mockPastDueNotificationRepo{}

	svc := newTestPastDueService(repo, plaid, paddle, clerk, notif)
	var logBuf bytes.Buffer
	svc.logger = slog.New(slog.NewTextHandler(&logBuf, nil))
	mockClaims := new(MockWorkClaimsRepository)
	svc.SetWorkClaimsRepo(mockClaims)

	userID := "user-plaid-complete-false"
	now := time.Now()
	pastDueStarted := now.Add(-15 * 24 * time.Hour)
	user := userWithPastDue(userID, pastDueStarted)
	resourceID := pastDueOccurrenceResourceID(t, userID, pastDueStarted)

	repo.On("GetUserByID", mock.Anything, userID).Return(&user, nil)
	mockClaims.On("Claim", mock.Anything, repository.ResourceTypeUser, resourceID, repository.ActionPlaidDisconnectPastDue, pastDueClaimLease).
		Return(testPastDueClaimToken, true, nil)
	plaid.On("DisconnectAllUserItems", mock.Anything, userID).Return(nil)
	notif.On("Create", mock.Anything, mock.Anything).Return(nil)
	// The lease was reclaimed by another worker before we could Complete it.
	mockClaims.On("Complete", mock.Anything, repository.ResourceTypeUser, resourceID, repository.ActionPlaidDisconnectPastDue, testPastDueClaimToken).
		Return(false, nil)

	err := svc.ProcessPastDueUser(context.Background(), user, now)

	assert.NoError(t, err, "the disconnect already ran; a lost lease at Complete time must not be reported as a failure")
	mockClaims.AssertCalled(t, "Complete", mock.Anything, repository.ResourceTypeUser, resourceID, repository.ActionPlaidDisconnectPastDue, testPastDueClaimToken)

	logOutput := logBuf.String()
	assert.Contains(t, logOutput, "level=ERROR", "a lost lease at Complete time must be logged at ERROR, not swallowed")
	assert.Contains(t, logOutput, userID, "the ERROR log must carry the identifying user_id")
}

// Test 5: Plaid-only window but re-read shows recovered — no-op
func TestPastDueService_ProcessUser_PlaidDisconnect_AlreadyRecovered(t *testing.T) {
	repo := &mockPastDueProfileRepo{}
	plaid := &mockPastDuePlaidCleanup{}
	paddle := &mockPastDuePaddleClient{}
	clerk := &mockPastDueClerkClient{}
	notif := &mockPastDueNotificationRepo{}

	svc := newTestPastDueService(repo, plaid, paddle, clerk, notif)

	userID := "user-recovered"
	now := time.Now()
	pastDueStarted := now.Add(-15 * 24 * time.Hour)
	user := userWithPastDue(userID, pastDueStarted)

	// Re-read shows user has recovered to active
	recoveredUser := user
	recoveredUser.SubscriptionStatus = string(domain.SubscriptionStatusActive)
	repo.On("GetUserByID", mock.Anything, userID).Return(&recoveredUser, nil)

	err := svc.ProcessPastDueUser(context.Background(), user, now)
	require.NoError(t, err)

	plaid.AssertNotCalled(t, "DisconnectAllUserItems", mock.Anything, mock.Anything)
}

// Test 6: Age below Plaid threshold — no action taken
func TestPastDueService_ProcessUser_TooEarly(t *testing.T) {
	repo := &mockPastDueProfileRepo{}
	plaid := &mockPastDuePlaidCleanup{}
	paddle := &mockPastDuePaddleClient{}
	clerk := &mockPastDueClerkClient{}
	notif := &mockPastDueNotificationRepo{}

	svc := newTestPastDueService(repo, plaid, paddle, clerk, notif)

	userID := "user-too-early"
	now := time.Now()
	// Age is 5 days: before Plaid threshold (14 days)
	pastDueStarted := now.Add(-5 * 24 * time.Hour)
	user := userWithPastDue(userID, pastDueStarted)

	err := svc.ProcessPastDueUser(context.Background(), user, now)
	require.NoError(t, err)

	plaid.AssertNotCalled(t, "DisconnectAllUserItems", mock.Anything, mock.Anything)
	paddle.AssertNotCalled(t, "CancelSubscription", mock.Anything, mock.Anything)
	repo.AssertNotCalled(t, "CancelIfPastDueOccurrenceWithRetention", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	repo.AssertNotCalled(t, "GetUserByID", mock.Anything, mock.Anything)
}

// Test 7: Grace expiry but CancelIfPastDueOccurrenceWithRetention returns false (user recovered between
// Plaid disconnect and the CAS flip) — no retention, no notification, returns nil.
func TestPastDueService_ProcessUser_GraceExpiry_UserRecovered_SkipsCancel(t *testing.T) {
	repo := &mockPastDueProfileRepo{}
	plaid := &mockPastDuePlaidCleanup{}
	paddle := &mockPastDuePaddleClient{}
	clerk := &mockPastDueClerkClient{}
	notif := &mockPastDueNotificationRepo{}

	svc := newTestPastDueService(repo, plaid, paddle, clerk, notif)

	userID := "user-grace-recovered"
	now := time.Now()
	pastDueStarted := now.Add(-time.Duration(testGraceDays+1) * 24 * time.Hour)
	user := userWithPastDue(userID, pastDueStarted)
	subID := *user.PaddleSubscriptionID

	repo.On("GetUserByID", mock.Anything, userID).Return(&user, nil)
	plaid.On("DisconnectAllUserItems", mock.Anything, userID).Return(nil)
	paddle.On("CancelSubscription", mock.Anything, subID).Return(nil)
	// CAS returns false — user paid between Plaid disconnect and this call
	repo.On("CancelIfPastDueOccurrenceWithRetention", mock.Anything, userID, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time"), mock.AnythingOfType("int")).Return(false, nil)

	err := svc.ProcessPastDueUser(context.Background(), user, now)
	require.NoError(t, err, "recovered user should not produce an error")

	repo.AssertCalled(t, "GetUserByID", mock.Anything, userID)
	repo.AssertCalled(t, "CancelIfPastDueOccurrenceWithRetention", mock.Anything, userID, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time"), mock.AnythingOfType("int"))
	repo.AssertNotCalled(t, "ClearPastDueStartedAtIfMatches", mock.Anything, mock.Anything, mock.Anything)
	clerk.AssertNotCalled(t, "UpdateUserMetadata", mock.Anything, mock.Anything, mock.Anything)
	notif.AssertNotCalled(t, "Create", mock.Anything, mock.Anything)
}

// Test 8 (GAP2): Grace expiry but user recovered between GetUsersInPastDue and processing —
// re-read guard must prevent destructive Plaid/Paddle/CAS calls.
// Regression: processGraceExpiry lacked the status re-read that processPlaidDisconnectWindow
// already had (lines 236-249). A user who paid between the batch-read and this call would lose
// bank connections and have Paddle cancelled before the CAS returned false.
func TestPastDueService_ProcessUser_GraceExpiry_RecoveredBeforeProcessing_SkipsDestructive(t *testing.T) {
	repo := &mockPastDueProfileRepo{}
	plaid := &mockPastDuePlaidCleanup{}
	paddle := &mockPastDuePaddleClient{}
	clerk := &mockPastDueClerkClient{}
	notif := &mockPastDueNotificationRepo{}

	svc := newTestPastDueService(repo, plaid, paddle, clerk, notif)

	userID := "user-recovered-before-grace"
	now := time.Now()
	pastDueStarted := now.Add(-time.Duration(testGraceDays+1) * 24 * time.Hour)
	user := userWithPastDue(userID, pastDueStarted)

	// Re-read shows the user paid and recovered to active.
	recoveredUser := user
	recoveredUser.SubscriptionStatus = string(domain.SubscriptionStatusActive)
	repo.On("GetUserByID", mock.Anything, userID).Return(&recoveredUser, nil)

	err := svc.ProcessPastDueUser(context.Background(), user, now)
	require.NoError(t, err, "recovered user should not produce an error")

	repo.AssertCalled(t, "GetUserByID", mock.Anything, userID)
	plaid.AssertNotCalled(t, "DisconnectAllUserItems", mock.Anything, mock.Anything)
	paddle.AssertNotCalled(t, "CancelSubscription", mock.Anything, mock.Anything)
	repo.AssertNotCalled(t, "CancelIfPastDueOccurrenceWithRetention", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	notif.AssertNotCalled(t, "Create", mock.Anything, mock.Anything)
}

// Test 8b (GAP3): Nil paddleClient — no panic, CAS flip still occurs, Paddle never called.
// Regression: main.go constructs PastDueService unconditionally with paddleService (nil when
// PADDLE_API_KEY unset). Before the nil guard, a user carrying a PaddleSubscriptionID would
// trigger a nil-pointer dereference panic inside processGraceExpiry step 2.
func TestPastDueService_ProcessUser_GraceExpiry_NilPaddleClient_NoPanic(t *testing.T) {
	repo := &mockPastDueProfileRepo{}
	plaid := &mockPastDuePlaidCleanup{}
	paddle := &mockPastDuePaddleClient{} // instantiated so AssertNotCalled works
	clerk := &mockPastDueClerkClient{}
	notif := &mockPastDueNotificationRepo{}

	// Pass interface nil for paddleClient (not a typed nil) so s.paddleClient == nil.
	svc := NewPastDueService(repo, plaid, nil, clerk, notif, testGraceDays, testPlaidDisconnectDays)
	svc.SetWorkClaimsRepo(newAllowingPastDueClaimsRepo())
	_ = paddle // used only for AssertNotCalled below

	userID := "user-nil-paddle"
	now := time.Now()
	pastDueStarted := now.Add(-time.Duration(testGraceDays+1) * 24 * time.Hour)
	user := userWithPastDue(userID, pastDueStarted) // has non-nil PaddleSubscriptionID

	repo.On("GetUserByID", mock.Anything, userID).Return(&user, nil)
	plaid.On("DisconnectAllUserItems", mock.Anything, userID).Return(nil)
	repo.On("CancelIfPastDueOccurrenceWithRetention", mock.Anything, userID, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time"), mock.AnythingOfType("int")).Return(true, nil)
	repo.On("ClearPastDueStartedAtIfMatches", mock.Anything, userID, mock.AnythingOfType("time.Time")).Return(true, nil)
	clerk.On("UpdateUserMetadata", mock.Anything, user.ClerkUserID, mock.Anything).Return(nil)
	notif.On("Create", mock.Anything, mock.Anything).Return(nil)

	// Must not panic even though user has a PaddleSubscriptionID and paddleClient is nil.
	assert.NotPanics(t, func() {
		err := svc.ProcessPastDueUser(context.Background(), user, now)
		assert.NoError(t, err)
	})
	// CAS flip still occurs; Paddle was correctly skipped (nil guard took effect).
	repo.AssertCalled(t, "CancelIfPastDueOccurrenceWithRetention", mock.Anything, userID, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time"), mock.AnythingOfType("int"))
	paddle.AssertNotCalled(t, "CancelSubscription", mock.Anything, mock.Anything)
}

// Test 9: GetUsersInPastDue — delegates to repo correctly
func TestPastDueService_GetUsersInPastDue(t *testing.T) {
	repo := &mockPastDueProfileRepo{}
	plaid := &mockPastDuePlaidCleanup{}
	paddle := &mockPastDuePaddleClient{}
	clerk := &mockPastDueClerkClient{}
	notif := &mockPastDueNotificationRepo{}

	svc := newTestPastDueService(repo, plaid, paddle, clerk, notif)

	now := time.Now()
	pastDueStarted := now.Add(-5 * 24 * time.Hour)
	expected := []domain.User{
		userWithPastDue("user-a", pastDueStarted),
		userWithPastDue("user-b", pastDueStarted),
	}
	repo.On("GetUsersInPastDue", mock.Anything).Return(expected, nil)

	result, err := svc.GetUsersInPastDue(context.Background())
	require.NoError(t, err)
	assert.Len(t, result, 2)
	assert.Equal(t, "user-a", result[0].ID)
	assert.Equal(t, "user-b", result[1].ID)
}

// ─── #1413: comped users are exempt from past_due destructive actions ──

// Test 10: comped user at the grace threshold — no Paddle cancel, no Plaid disconnect, no CAS flip.
func TestPastDueService_ProcessUser_GraceExpiry_ActiveOverride_Skipped(t *testing.T) {
	repo := &mockPastDueProfileRepo{}
	plaid := &mockPastDuePlaidCleanup{}
	paddle := &mockPastDuePaddleClient{}
	clerk := &mockPastDueClerkClient{}
	notif := &mockPastDueNotificationRepo{}

	svc := newTestPastDueService(repo, plaid, paddle, clerk, notif)

	userID := "user-comped-grace"
	now := time.Now()
	pastDueStarted := now.Add(-time.Duration(testGraceDays+1) * 24 * time.Hour)
	user := userWithPastDueOverride(userID, pastDueStarted, now.Add(24*time.Hour)) // active override

	err := svc.ProcessPastDueUser(context.Background(), user, now)
	require.NoError(t, err)

	repo.AssertNotCalled(t, "GetUserByID", mock.Anything, mock.Anything)
	plaid.AssertNotCalled(t, "DisconnectAllUserItems", mock.Anything, mock.Anything)
	paddle.AssertNotCalled(t, "CancelSubscription", mock.Anything, mock.Anything)
	repo.AssertNotCalled(t, "CancelIfPastDueOccurrenceWithRetention", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

// Test 11: comped user at the Plaid-disconnect threshold — no disconnect.
func TestPastDueService_ProcessUser_PlaidDisconnect_ActiveOverride_Skipped(t *testing.T) {
	repo := &mockPastDueProfileRepo{}
	plaid := &mockPastDuePlaidCleanup{}
	paddle := &mockPastDuePaddleClient{}
	clerk := &mockPastDueClerkClient{}
	notif := &mockPastDueNotificationRepo{}

	svc := newTestPastDueService(repo, plaid, paddle, clerk, notif)

	userID := "user-comped-plaid-window"
	now := time.Now()
	pastDueStarted := now.Add(-15 * 24 * time.Hour) // in plaid-disconnect window
	user := userWithPastDueOverride(userID, pastDueStarted, now.Add(24*time.Hour))

	err := svc.ProcessPastDueUser(context.Background(), user, now)
	require.NoError(t, err)

	plaid.AssertNotCalled(t, "DisconnectAllUserItems", mock.Anything, mock.Anything)
}

// Test 12: comp applied mid-run — the batch-read user was not comped, but reReadStillPastDue's
// re-read shows the user was comped between GetUsersInPastDue and this call. Grace expiry must abort.
func TestPastDueService_ProcessUser_GraceExpiry_CompedMidRun_Aborts(t *testing.T) {
	repo := &mockPastDueProfileRepo{}
	plaid := &mockPastDuePlaidCleanup{}
	paddle := &mockPastDuePaddleClient{}
	clerk := &mockPastDueClerkClient{}
	notif := &mockPastDueNotificationRepo{}

	svc := newTestPastDueService(repo, plaid, paddle, clerk, notif)

	userID := "user-comped-mid-run"
	now := time.Now()
	pastDueStarted := now.Add(-time.Duration(testGraceDays+1) * 24 * time.Hour)
	user := userWithPastDue(userID, pastDueStarted) // NOT comped at batch-read time

	// Re-read shows the user was comped in the meantime.
	compedUser := userWithPastDueOverride(userID, pastDueStarted, now.Add(24*time.Hour))
	repo.On("GetUserByID", mock.Anything, userID).Return(&compedUser, nil)

	err := svc.ProcessPastDueUser(context.Background(), user, now)
	require.NoError(t, err)

	plaid.AssertNotCalled(t, "DisconnectAllUserItems", mock.Anything, mock.Anything)
	paddle.AssertNotCalled(t, "CancelSubscription", mock.Anything, mock.Anything)
	repo.AssertNotCalled(t, "CancelIfPastDueOccurrenceWithRetention", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

// ─── Issue #1413 round-4 finding H1: a stale past-due occurrence must not act during a newer one ──
//
// reReadStillPastDue previously checked only current status and HasActiveManualOverride() — a
// worker holding cycle A's batch snapshot would pass both checks against cycle B's row (cycle B
// is ALSO past_due with no override) and proceed to disconnect Plaid / cancel Paddle / flip status
// during cycle B, before cycle B's own grace period had expired. The fix threads the CLAIMED
// occurrence (past_due_started_at) into the re-read check so a worker can only act on the exact
// cycle it actually claimed.

// TestPastDueService_ProcessUser_GraceExpiry_OccurrenceChangedMidRun_Aborts is the H1 regression
// proof: the batch snapshot captured cycle A; the re-read shows the SAME user, still past_due,
// still uncomped, but a DIFFERENT past_due_started_at (cycle B) — a stale worker must abort rather
// than act on cycle B under cycle A's authority.
func TestPastDueService_ProcessUser_GraceExpiry_OccurrenceChangedMidRun_Aborts(t *testing.T) {
	repo := &mockPastDueProfileRepo{}
	plaid := &mockPastDuePlaidCleanup{}
	paddle := &mockPastDuePaddleClient{}
	clerk := &mockPastDueClerkClient{}
	notif := &mockPastDueNotificationRepo{}

	svc := newTestPastDueService(repo, plaid, paddle, clerk, notif)

	userID := "user-grace-occurrence-changed"
	now := time.Now()
	cycleA := now.Add(-time.Duration(testGraceDays+1) * 24 * time.Hour) // the batch snapshot's cycle — at grace
	user := userWithPastDue(userID, cycleA)

	// The re-read shows a DIFFERENT, genuinely newer cycle (B) — still past_due, still uncomped,
	// but not yet at cycle A's grace age. A stale worker holding cycle A's claim must not treat
	// this as "still eligible".
	cycleB := now.Add(-time.Duration(testPlaidDisconnectDays) * 24 * time.Hour)
	freshUser := userWithPastDue(userID, cycleB)
	repo.On("GetUserByID", mock.Anything, userID).Return(&freshUser, nil)

	err := svc.ProcessPastDueUser(context.Background(), user, now)
	require.NoError(t, err, "an occurrence mismatch is a normal skip, not a job failure")

	plaid.AssertNotCalled(t, "DisconnectAllUserItems", mock.Anything, mock.Anything)
	paddle.AssertNotCalled(t, "CancelSubscription", mock.Anything, mock.Anything)
	repo.AssertNotCalled(t, "CancelIfPastDueOccurrenceWithRetention", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

// TestPastDueService_ProcessUser_PlaidDisconnect_OccurrenceChangedMidRun_Aborts mirrors the
// grace-expiry proof above for the Plaid-disconnect-window path: DisconnectAllUserItems must not
// be called for a stale cycle-A claim once the row has moved on to cycle B.
func TestPastDueService_ProcessUser_PlaidDisconnect_OccurrenceChangedMidRun_Aborts(t *testing.T) {
	repo := &mockPastDueProfileRepo{}
	plaid := &mockPastDuePlaidCleanup{}
	paddle := &mockPastDuePaddleClient{}
	clerk := &mockPastDueClerkClient{}
	notif := &mockPastDueNotificationRepo{}

	svc := newTestPastDueService(repo, plaid, paddle, clerk, notif)

	userID := "user-plaid-occurrence-changed"
	now := time.Now()
	cycleA := now.Add(-15 * 24 * time.Hour) // in the plaid-disconnect window
	user := userWithPastDue(userID, cycleA)

	cycleB := now.Add(-1 * time.Hour) // a different, much newer cycle
	freshUser := userWithPastDue(userID, cycleB)
	repo.On("GetUserByID", mock.Anything, userID).Return(&freshUser, nil)

	err := svc.ProcessPastDueUser(context.Background(), user, now)
	require.NoError(t, err, "an occurrence mismatch is a normal skip, not a job failure")

	plaid.AssertNotCalled(t, "DisconnectAllUserItems", mock.Anything, mock.Anything)
}

// TestPastDueService_ProcessUser_GraceExpiry_OccurrenceUnchanged_Proceeds is the required
// positive-direction proof: when the re-read's past_due_started_at matches the claimed occurrence
// exactly (the ordinary, overwhelmingly common case), processing proceeds exactly as before. An
// over-tight occurrence predicate that silently stopped ALL dunning (e.g. via a formatting/
// precision mismatch between the claimed and re-read timestamps) would fail this test even though
// every "nothing bad happened" test elsewhere would still pass.
func TestPastDueService_ProcessUser_GraceExpiry_OccurrenceUnchanged_Proceeds(t *testing.T) {
	repo := &mockPastDueProfileRepo{}
	plaid := &mockPastDuePlaidCleanup{}
	paddle := &mockPastDuePaddleClient{}
	clerk := &mockPastDueClerkClient{}
	notif := &mockPastDueNotificationRepo{}

	svc := newTestPastDueService(repo, plaid, paddle, clerk, notif)

	userID := "user-grace-occurrence-unchanged"
	now := time.Now()
	pastDueStarted := now.Add(-time.Duration(testGraceDays+1) * 24 * time.Hour)
	user := userWithPastDue(userID, pastDueStarted)
	subID := *user.PaddleSubscriptionID

	// The re-read returns a user with the EXACT SAME past_due_started_at — the ordinary case.
	repo.On("GetUserByID", mock.Anything, userID).Return(&user, nil)
	plaid.On("DisconnectAllUserItems", mock.Anything, userID).Return(nil)
	paddle.On("CancelSubscription", mock.Anything, subID).Return(nil)
	repo.On("CancelIfPastDueOccurrenceWithRetention", mock.Anything, userID, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time"), mock.AnythingOfType("int")).Return(true, nil)
	repo.On("ClearPastDueStartedAtIfMatches", mock.Anything, userID, mock.AnythingOfType("time.Time")).Return(true, nil)
	clerk.On("UpdateUserMetadata", mock.Anything, user.ClerkUserID, mock.Anything).Return(nil)
	notif.On("Create", mock.Anything, mock.Anything).Return(nil)

	err := svc.ProcessPastDueUser(context.Background(), user, now)
	require.NoError(t, err)

	repo.AssertCalled(t, "CancelIfPastDueOccurrenceWithRetention", mock.Anything, userID, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time"), mock.AnythingOfType("int"))
	plaid.AssertCalled(t, "DisconnectAllUserItems", mock.Anything, userID)
}

// Test 13: expired override — proceeds to grace expiry normally.
func TestPastDueService_ProcessUser_GraceExpiry_ExpiredOverride_Proceeds(t *testing.T) {
	repo := &mockPastDueProfileRepo{}
	plaid := &mockPastDuePlaidCleanup{}
	paddle := &mockPastDuePaddleClient{}
	clerk := &mockPastDueClerkClient{}
	notif := &mockPastDueNotificationRepo{}

	svc := newTestPastDueService(repo, plaid, paddle, clerk, notif)

	userID := "user-expired-override-grace"
	now := time.Now()
	pastDueStarted := now.Add(-time.Duration(testGraceDays+1) * 24 * time.Hour)
	user := userWithPastDueOverride(userID, pastDueStarted, now.Add(-24*time.Hour)) // expired override
	subID := *user.PaddleSubscriptionID

	repo.On("GetUserByID", mock.Anything, userID).Return(&user, nil)
	plaid.On("DisconnectAllUserItems", mock.Anything, userID).Return(nil)
	paddle.On("CancelSubscription", mock.Anything, subID).Return(nil)
	repo.On("CancelIfPastDueOccurrenceWithRetention", mock.Anything, userID, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time"), mock.AnythingOfType("int")).Return(true, nil)
	repo.On("ClearPastDueStartedAtIfMatches", mock.Anything, userID, mock.AnythingOfType("time.Time")).Return(true, nil)
	clerk.On("UpdateUserMetadata", mock.Anything, user.ClerkUserID, mock.Anything).Return(nil)
	notif.On("Create", mock.Anything, mock.Anything).Return(nil)

	err := svc.ProcessPastDueUser(context.Background(), user, now)
	require.NoError(t, err)

	repo.AssertCalled(t, "CancelIfPastDueOccurrenceWithRetention", mock.Anything, userID, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time"), mock.AnythingOfType("int"))
	plaid.AssertCalled(t, "DisconnectAllUserItems", mock.Anything, userID)
}

// Test 14: household-covered user is skipped entirely (the new hook, mirroring the deleted TrialService).
func TestPastDueService_ProcessUser_CoveredByHousehold_Skipped(t *testing.T) {
	repo := &mockPastDueProfileRepo{}
	plaid := &mockPastDuePlaidCleanup{}
	paddle := &mockPastDuePaddleClient{}
	clerk := &mockPastDueClerkClient{}
	notif := &mockPastDueNotificationRepo{}

	svc := newTestPastDueService(repo, plaid, paddle, clerk, notif)
	checker := &mockHouseholdCheckerForPastDue{}
	svc.SetHouseholdChecker(checker)

	userID := "user-household-covered"
	now := time.Now()
	pastDueStarted := now.Add(-time.Duration(testGraceDays+1) * 24 * time.Hour)
	user := userWithPastDue(userID, pastDueStarted)
	checker.On("IsCoveredByHousehold", mock.Anything, userID).Return(true, nil)

	err := svc.ProcessPastDueUser(context.Background(), user, now)
	require.NoError(t, err)

	repo.AssertNotCalled(t, "GetUserByID", mock.Anything, mock.Anything)
	plaid.AssertNotCalled(t, "DisconnectAllUserItems", mock.Anything, mock.Anything)
	paddle.AssertNotCalled(t, "CancelSubscription", mock.Anything, mock.Anything)
	repo.AssertNotCalled(t, "CancelIfPastDueOccurrenceWithRetention", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

// Test 15: household checker not covered — proceeds normally.
func TestPastDueService_ProcessUser_NotCoveredByHousehold_Proceeds(t *testing.T) {
	repo := &mockPastDueProfileRepo{}
	plaid := &mockPastDuePlaidCleanup{}
	paddle := &mockPastDuePaddleClient{}
	clerk := &mockPastDueClerkClient{}
	notif := &mockPastDueNotificationRepo{}

	svc := newTestPastDueService(repo, plaid, paddle, clerk, notif)
	checker := &mockHouseholdCheckerForPastDue{}
	svc.SetHouseholdChecker(checker)

	userID := "user-household-not-covered"
	now := time.Now()
	pastDueStarted := now.Add(-time.Duration(testGraceDays+1) * 24 * time.Hour)
	user := userWithPastDue(userID, pastDueStarted)
	subID := *user.PaddleSubscriptionID
	checker.On("IsCoveredByHousehold", mock.Anything, userID).Return(false, nil)

	repo.On("GetUserByID", mock.Anything, userID).Return(&user, nil)
	plaid.On("DisconnectAllUserItems", mock.Anything, userID).Return(nil)
	paddle.On("CancelSubscription", mock.Anything, subID).Return(nil)
	repo.On("CancelIfPastDueOccurrenceWithRetention", mock.Anything, userID, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time"), mock.AnythingOfType("int")).Return(true, nil)
	repo.On("ClearPastDueStartedAtIfMatches", mock.Anything, userID, mock.AnythingOfType("time.Time")).Return(true, nil)
	clerk.On("UpdateUserMetadata", mock.Anything, user.ClerkUserID, mock.Anything).Return(nil)
	notif.On("Create", mock.Anything, mock.Anything).Return(nil)

	err := svc.ProcessPastDueUser(context.Background(), user, now)
	require.NoError(t, err)

	repo.AssertCalled(t, "CancelIfPastDueOccurrenceWithRetention", mock.Anything, userID, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time"), mock.AnythingOfType("int"))
}

// Test 16: household checker fails — fail closed (skip destructive action) but propagate the
// error, mirroring the legacy trial-expiry flow's (deleted in #1652) fail-closed-with-error-propagation semantics
// (see the legacy trial service (deleted in #1652) for why swallowing would hide an operational failure).
func TestPastDueService_ProcessUser_HouseholdCheckFails_SkipsDefensivelyButPropagatesError(t *testing.T) {
	repo := &mockPastDueProfileRepo{}
	plaid := &mockPastDuePlaidCleanup{}
	paddle := &mockPastDuePaddleClient{}
	clerk := &mockPastDueClerkClient{}
	notif := &mockPastDueNotificationRepo{}

	svc := newTestPastDueService(repo, plaid, paddle, clerk, notif)
	checker := &mockHouseholdCheckerForPastDue{}
	svc.SetHouseholdChecker(checker)

	userID := "user-household-check-fails"
	now := time.Now()
	pastDueStarted := now.Add(-time.Duration(testGraceDays+1) * 24 * time.Hour)
	user := userWithPastDue(userID, pastDueStarted)
	checker.On("IsCoveredByHousehold", mock.Anything, userID).Return(false, errors.New("lookup failed"))

	err := svc.ProcessPastDueUser(context.Background(), user, now)
	require.Error(t, err, "the failure must be visible to the caller's error/retry accounting, not silently swallowed")

	plaid.AssertNotCalled(t, "DisconnectAllUserItems", mock.Anything, mock.Anything)
	repo.AssertNotCalled(t, "CancelIfPastDueOccurrenceWithRetention", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

// ─── #1413 H6b (Codex review round 4): the work claim must be won, and re-verified, BEFORE any
// Plaid/Paddle destructive work — the whole point of the finding is that ordering, not merely a
// wider predicate, closes the "CAS correctly refuses after the irreversible work already ran" bug. ──

// TestPastDueService_ProcessUser_GraceExpiry_ClaimWonBeforeDestructiveWork proves H6b's ordering
// fix directly by recording the actual call order via .Run() hooks — an ordering regression
// wouldn't change WHETHER each mock is called, only WHEN, so plain AssertCalled assertions
// (already covered by Test 1) cannot catch it.
func TestPastDueService_ProcessUser_GraceExpiry_ClaimWonBeforeDestructiveWork(t *testing.T) {
	repo := &mockPastDueProfileRepo{}
	plaid := &mockPastDuePlaidCleanup{}
	paddle := &mockPastDuePaddleClient{}
	clerk := &mockPastDueClerkClient{}
	notif := &mockPastDueNotificationRepo{}

	svc := newTestPastDueService(repo, plaid, paddle, clerk, notif)
	mockClaims := new(MockWorkClaimsRepository)
	svc.SetWorkClaimsRepo(mockClaims)

	userID := "user-grace-ordering"
	now := time.Now()
	pastDueStarted := now.Add(-time.Duration(testGraceDays+1) * 24 * time.Hour)
	user := userWithPastDue(userID, pastDueStarted)
	subID := *user.PaddleSubscriptionID
	resourceID := pastDueOccurrenceResourceID(t, userID, pastDueStarted)

	var callOrder []string
	repo.On("GetUserByID", mock.Anything, userID).Return(&user, nil)
	mockClaims.On("Claim", mock.Anything, repository.ResourceTypeUser, resourceID, repository.ActionPastDueGraceCancel, pastDueClaimLease).
		Run(func(mock.Arguments) { callOrder = append(callOrder, "claim") }).
		Return(testPastDueClaimToken, true, nil)
	plaid.On("DisconnectAllUserItems", mock.Anything, userID).
		Run(func(mock.Arguments) { callOrder = append(callOrder, "plaid_disconnect") }).
		Return(nil)
	// Issue #1413 H1: the Paddle-cancel step nests ActionPaddleCancelSubscription inside the flow
	// claim above. Side-effect claims stay keyed on the BARE userID (unchanged by #1413 P5).
	mockClaims.On("Claim", mock.Anything, repository.ResourceTypeUser, userID, repository.ActionPaddleCancelSubscription, paddleCancelSideEffectLease).
		Return(testPastDuePaddleCancelSideEffectToken, true, nil)
	mockClaims.On("Release", mock.Anything, repository.ResourceTypeUser, userID, repository.ActionPaddleCancelSubscription, testPastDuePaddleCancelSideEffectToken, mock.AnythingOfType("string")).
		Return(true, nil)
	paddle.On("CancelSubscription", mock.Anything, subID).
		Run(func(mock.Arguments) { callOrder = append(callOrder, "paddle_cancel") }).
		Return(nil)
	repo.On("CancelIfPastDueOccurrenceWithRetention", mock.Anything, userID, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time"), mock.AnythingOfType("int")).
		Run(func(mock.Arguments) { callOrder = append(callOrder, "cas_flip") }).
		Return(true, nil)
	mockClaims.On("Complete", mock.Anything, repository.ResourceTypeUser, resourceID, repository.ActionPastDueGraceCancel, testPastDueClaimToken).
		Return(true, nil)
	repo.On("ClearPastDueStartedAtIfMatches", mock.Anything, userID, mock.AnythingOfType("time.Time")).Return(true, nil)
	clerk.On("UpdateUserMetadata", mock.Anything, user.ClerkUserID, mock.Anything).Return(nil)
	notif.On("Create", mock.Anything, mock.Anything).Return(nil)

	err := svc.ProcessPastDueUser(context.Background(), user, now)
	require.NoError(t, err)

	require.Equal(t, []string{"claim", "plaid_disconnect", "paddle_cancel", "cas_flip"}, callOrder,
		"the claim must be won BEFORE any Paddle cancel or Plaid disconnect — H6b's ordering fix")
}

// TestPastDueService_ProcessUser_GraceExpiry_ClaimLost_SkipsDestructive proves H6b's "claim not
// won" direction: no external call of any kind, and no state transition.
func TestPastDueService_ProcessUser_GraceExpiry_ClaimLost_SkipsDestructive(t *testing.T) {
	repo := &mockPastDueProfileRepo{}
	plaid := &mockPastDuePlaidCleanup{}
	paddle := &mockPastDuePaddleClient{}
	clerk := &mockPastDueClerkClient{}
	notif := &mockPastDueNotificationRepo{}

	svc := newTestPastDueService(repo, plaid, paddle, clerk, notif)
	mockClaims := new(MockWorkClaimsRepository)
	svc.SetWorkClaimsRepo(mockClaims)

	userID := "user-grace-claim-lost"
	now := time.Now()
	pastDueStarted := now.Add(-time.Duration(testGraceDays+1) * 24 * time.Hour)
	user := userWithPastDue(userID, pastDueStarted)
	resourceID := pastDueOccurrenceResourceID(t, userID, pastDueStarted)

	repo.On("GetUserByID", mock.Anything, userID).Return(&user, nil)
	mockClaims.On("Claim", mock.Anything, repository.ResourceTypeUser, resourceID, repository.ActionPastDueGraceCancel, pastDueClaimLease).
		Return("", false, nil)

	err := svc.ProcessPastDueUser(context.Background(), user, now)
	require.NoError(t, err, "a lost claim is a normal skip, not a job failure")

	plaid.AssertNotCalled(t, "DisconnectAllUserItems", mock.Anything, mock.Anything)
	paddle.AssertNotCalled(t, "CancelSubscription", mock.Anything, mock.Anything)
	repo.AssertNotCalled(t, "CancelIfPastDueOccurrenceWithRetention", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	mockClaims.AssertNotCalled(t, "Release", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

// TestPastDueService_ProcessUser_GraceExpiry_ClaimWon_PlaidFails_ReleasesClaim proves H6b's "claim
// won + external failure" direction: Release is called with the reason, no state transition
// occurs, and the failure propagates so the daily job retries.
func TestPastDueService_ProcessUser_GraceExpiry_ClaimWon_PlaidFails_ReleasesClaim(t *testing.T) {
	repo := &mockPastDueProfileRepo{}
	plaid := &mockPastDuePlaidCleanup{}
	paddle := &mockPastDuePaddleClient{}
	clerk := &mockPastDueClerkClient{}
	notif := &mockPastDueNotificationRepo{}

	svc := newTestPastDueService(repo, plaid, paddle, clerk, notif)
	mockClaims := new(MockWorkClaimsRepository)
	svc.SetWorkClaimsRepo(mockClaims)

	userID := "user-grace-release-on-fail"
	now := time.Now()
	pastDueStarted := now.Add(-time.Duration(testGraceDays+1) * 24 * time.Hour)
	user := userWithPastDue(userID, pastDueStarted)
	resourceID := pastDueOccurrenceResourceID(t, userID, pastDueStarted)

	plaidErr := errors.New("plaid API error")
	repo.On("GetUserByID", mock.Anything, userID).Return(&user, nil)
	mockClaims.On("Claim", mock.Anything, repository.ResourceTypeUser, resourceID, repository.ActionPastDueGraceCancel, pastDueClaimLease).
		Return(testPastDueClaimToken, true, nil)
	plaid.On("DisconnectAllUserItems", mock.Anything, userID).Return(plaidErr)
	mockClaims.On("Release", mock.Anything, repository.ResourceTypeUser, resourceID, repository.ActionPastDueGraceCancel, testPastDueClaimToken, plaidErr.Error()).
		Return(true, nil)

	err := svc.ProcessPastDueUser(context.Background(), user, now)
	require.Error(t, err)

	mockClaims.AssertCalled(t, "Release", mock.Anything, repository.ResourceTypeUser, resourceID, repository.ActionPastDueGraceCancel, testPastDueClaimToken, plaidErr.Error())
	paddle.AssertNotCalled(t, "CancelSubscription", mock.Anything, mock.Anything)
	repo.AssertNotCalled(t, "CancelIfPastDueOccurrenceWithRetention", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	mockClaims.AssertNotCalled(t, "Complete", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

// TestPastDueService_ProcessUser_GraceExpiry_CASMiss_ReleasesNotCompletes proves that when the
// final CancelIfPastDueOccurrenceWithRetention CAS misses (comp/recovery landed during the Plaid/Paddle calls), the claim
// is Released — NOT Completed. Completing here would permanently block reclaiming per the
// work_claims contract ("a completed claim is never re-granted"), stranding a user who must still
// be cancelled once a mid-flight comp lapses.
func TestPastDueService_ProcessUser_GraceExpiry_CASMiss_ReleasesNotCompletes(t *testing.T) {
	repo := &mockPastDueProfileRepo{}
	plaid := &mockPastDuePlaidCleanup{}
	paddle := &mockPastDuePaddleClient{}
	clerk := &mockPastDueClerkClient{}
	notif := &mockPastDueNotificationRepo{}

	svc := newTestPastDueService(repo, plaid, paddle, clerk, notif)
	mockClaims := new(MockWorkClaimsRepository)
	svc.SetWorkClaimsRepo(mockClaims)

	userID := "user-grace-cas-miss"
	now := time.Now()
	pastDueStarted := now.Add(-time.Duration(testGraceDays+1) * 24 * time.Hour)
	user := userWithPastDue(userID, pastDueStarted)
	subID := *user.PaddleSubscriptionID
	resourceID := pastDueOccurrenceResourceID(t, userID, pastDueStarted)

	repo.On("GetUserByID", mock.Anything, userID).Return(&user, nil)
	mockClaims.On("Claim", mock.Anything, repository.ResourceTypeUser, resourceID, repository.ActionPastDueGraceCancel, pastDueClaimLease).
		Return(testPastDueClaimToken, true, nil)
	plaid.On("DisconnectAllUserItems", mock.Anything, userID).Return(nil)
	mockClaims.On("Claim", mock.Anything, repository.ResourceTypeUser, userID, repository.ActionPaddleCancelSubscription, paddleCancelSideEffectLease).
		Return(testPastDuePaddleCancelSideEffectToken, true, nil)
	mockClaims.On("Release", mock.Anything, repository.ResourceTypeUser, userID, repository.ActionPaddleCancelSubscription, testPastDuePaddleCancelSideEffectToken, mock.AnythingOfType("string")).
		Return(true, nil)
	paddle.On("CancelSubscription", mock.Anything, subID).Return(nil)
	// CAS misses — recovered or comped during the external calls above.
	repo.On("CancelIfPastDueOccurrenceWithRetention", mock.Anything, userID, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time"), mock.AnythingOfType("int")).Return(false, nil)
	mockClaims.On("Release", mock.Anything, repository.ResourceTypeUser, resourceID, repository.ActionPastDueGraceCancel, testPastDueClaimToken, mock.AnythingOfType("string")).
		Return(true, nil)

	err := svc.ProcessPastDueUser(context.Background(), user, now)
	require.NoError(t, err)

	mockClaims.AssertCalled(t, "Release", mock.Anything, repository.ResourceTypeUser, resourceID, repository.ActionPastDueGraceCancel, testPastDueClaimToken, mock.AnythingOfType("string"))
	mockClaims.AssertNotCalled(t, "Complete", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

// TestPastDueService_ProcessUser_GraceExpiry_CompleteReturnsFalse_LoggedNotSwallowed mirrors
// deletion_service_test.go's TestExecuteDeletion_CompleteReturnsFalse_LoggedNotSwallowed: a lost
// lease at Complete time must not be reported as a processing failure. PastDueService's logger is a
// stored field (captured from slog.Default() at construction), so it's swapped for a buffer-backed
// logger here and the resulting ERROR-level log record from processGraceExpiry's Complete handling
// is asserted directly, not merely inspected in the source.
func TestPastDueService_ProcessUser_GraceExpiry_CompleteReturnsFalse_LoggedNotSwallowed(t *testing.T) {
	repo := &mockPastDueProfileRepo{}
	plaid := &mockPastDuePlaidCleanup{}
	paddle := &mockPastDuePaddleClient{}
	clerk := &mockPastDueClerkClient{}
	notif := &mockPastDueNotificationRepo{}

	svc := newTestPastDueService(repo, plaid, paddle, clerk, notif)
	var logBuf bytes.Buffer
	svc.logger = slog.New(slog.NewTextHandler(&logBuf, nil))
	mockClaims := new(MockWorkClaimsRepository)
	svc.SetWorkClaimsRepo(mockClaims)

	userID := "user-grace-complete-false"
	now := time.Now()
	pastDueStarted := now.Add(-time.Duration(testGraceDays+1) * 24 * time.Hour)
	user := userWithPastDue(userID, pastDueStarted)
	subID := *user.PaddleSubscriptionID
	resourceID := pastDueOccurrenceResourceID(t, userID, pastDueStarted)

	repo.On("GetUserByID", mock.Anything, userID).Return(&user, nil)
	mockClaims.On("Claim", mock.Anything, repository.ResourceTypeUser, resourceID, repository.ActionPastDueGraceCancel, pastDueClaimLease).
		Return(testPastDueClaimToken, true, nil)
	plaid.On("DisconnectAllUserItems", mock.Anything, userID).Return(nil)
	mockClaims.On("Claim", mock.Anything, repository.ResourceTypeUser, userID, repository.ActionPaddleCancelSubscription, paddleCancelSideEffectLease).
		Return(testPastDuePaddleCancelSideEffectToken, true, nil)
	mockClaims.On("Release", mock.Anything, repository.ResourceTypeUser, userID, repository.ActionPaddleCancelSubscription, testPastDuePaddleCancelSideEffectToken, mock.AnythingOfType("string")).
		Return(true, nil)
	paddle.On("CancelSubscription", mock.Anything, subID).Return(nil)
	repo.On("CancelIfPastDueOccurrenceWithRetention", mock.Anything, userID, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time"), mock.AnythingOfType("int")).Return(true, nil)
	repo.On("ClearPastDueStartedAtIfMatches", mock.Anything, userID, mock.AnythingOfType("time.Time")).Return(true, nil)
	clerk.On("UpdateUserMetadata", mock.Anything, user.ClerkUserID, mock.Anything).Return(nil)
	notif.On("Create", mock.Anything, mock.Anything).Return(nil)
	// The lease was reclaimed by another worker before we could Complete it.
	mockClaims.On("Complete", mock.Anything, repository.ResourceTypeUser, resourceID, repository.ActionPastDueGraceCancel, testPastDueClaimToken).
		Return(false, nil)

	err := svc.ProcessPastDueUser(context.Background(), user, now)

	assert.NoError(t, err, "the cancellation already ran; a lost lease at Complete time must not be reported as a failure")
	mockClaims.AssertCalled(t, "Complete", mock.Anything, repository.ResourceTypeUser, resourceID, repository.ActionPastDueGraceCancel, testPastDueClaimToken)

	logOutput := logBuf.String()
	assert.Contains(t, logOutput, "level=ERROR", "a lost lease at Complete time must be logged at ERROR, not swallowed")
	assert.Contains(t, logOutput, userID, "the ERROR log must carry the identifying user_id")
}

// TestPastDueService_ProcessUser_GraceExpiry_ClearStep_TimestampMovedOn_NotAnError is the H3
// regression proof at the SERVICE (caller) level — Issue #1413 round-4 finding H3. By the time
// Step 5 runs, the CAS above has already flipped THIS cycle to 'cancelled'; if
// ClearPastDueStartedAtIfMatches reports (false, nil) — the column has already moved on to a
// NEWER cycle's timestamp, e.g. a recover -> re-enter-past-due race that landed during the
// Plaid/Paddle calls — that must be a logged skip, NOT a processing failure: the newer cycle's
// timestamp must be left alone so a future run can still process it, and this flow's OWN
// cancellation (which already succeeded) must still be reported as a success and still Complete
// its claim.
func TestPastDueService_ProcessUser_GraceExpiry_ClearStep_TimestampMovedOn_NotAnError(t *testing.T) {
	repo := &mockPastDueProfileRepo{}
	plaid := &mockPastDuePlaidCleanup{}
	paddle := &mockPastDuePaddleClient{}
	clerk := &mockPastDueClerkClient{}
	notif := &mockPastDueNotificationRepo{}

	svc := newTestPastDueService(repo, plaid, paddle, clerk, notif)
	mockClaims := new(MockWorkClaimsRepository)
	svc.SetWorkClaimsRepo(mockClaims)

	userID := "user-grace-clear-moved-on"
	now := time.Now()
	pastDueStarted := now.Add(-time.Duration(testGraceDays+1) * 24 * time.Hour)
	user := userWithPastDue(userID, pastDueStarted)
	subID := *user.PaddleSubscriptionID
	resourceID := pastDueOccurrenceResourceID(t, userID, pastDueStarted)

	repo.On("GetUserByID", mock.Anything, userID).Return(&user, nil)
	mockClaims.On("Claim", mock.Anything, repository.ResourceTypeUser, resourceID, repository.ActionPastDueGraceCancel, pastDueClaimLease).
		Return(testPastDueClaimToken, true, nil)
	plaid.On("DisconnectAllUserItems", mock.Anything, userID).Return(nil)
	mockClaims.On("Claim", mock.Anything, repository.ResourceTypeUser, userID, repository.ActionPaddleCancelSubscription, paddleCancelSideEffectLease).
		Return(testPastDuePaddleCancelSideEffectToken, true, nil)
	mockClaims.On("Release", mock.Anything, repository.ResourceTypeUser, userID, repository.ActionPaddleCancelSubscription, testPastDuePaddleCancelSideEffectToken, mock.AnythingOfType("string")).
		Return(true, nil)
	paddle.On("CancelSubscription", mock.Anything, subID).Return(nil)
	repo.On("CancelIfPastDueOccurrenceWithRetention", mock.Anything, userID, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time"), mock.AnythingOfType("int")).Return(true, nil)
	// The column has already moved on to a newer cycle by the time the clear step runs.
	repo.On("ClearPastDueStartedAtIfMatches", mock.Anything, userID, mock.AnythingOfType("time.Time")).Return(false, nil)
	clerk.On("UpdateUserMetadata", mock.Anything, user.ClerkUserID, mock.Anything).Return(nil)
	notif.On("Create", mock.Anything, mock.Anything).Return(nil)
	mockClaims.On("Complete", mock.Anything, repository.ResourceTypeUser, resourceID, repository.ActionPastDueGraceCancel, testPastDueClaimToken).
		Return(true, nil)

	err := svc.ProcessPastDueUser(context.Background(), user, now)

	assert.NoError(t, err, "a newer cycle's timestamp surviving the clear step is a legitimate skip, not a processing failure")
	mockClaims.AssertCalled(t, "Complete", mock.Anything, repository.ResourceTypeUser, resourceID, repository.ActionPastDueGraceCancel, testPastDueClaimToken)
	// This flow's own cancellation and notification still went through — the clear-step skip does
	// not undo or block anything that already succeeded.
	clerk.AssertCalled(t, "UpdateUserMetadata", mock.Anything, user.ClerkUserID, mock.Anything)
	notif.AssertCalled(t, "Create", mock.Anything, mock.Anything)
}

// TestPastDueService_ProcessUser_GraceExpiry_CancelIfPastDueOccurrenceWithRetentionError_Aborts_ReleasesClaim
// is the regression test for #1413 round-3 finding H5's residual: the CAS flip and the
// retention-clock write used to be two SEPARATE calls (CancelIfPastDue, then SetRetentionDates),
// so a lost/failed retention write after a successful CAS left the user cancelled with NO clock
// — and because GetUsersInPastDue only selects subscription_status = 'past_due', the daily job
// could never re-reach that user to retry. CancelIfPastDueOccurrenceWithRetention closes this STRUCTURALLY:
// both writes are now the SAME statement, so "CAS succeeds but retention fails" is no longer a
// reachable state — the only failure mode left is the WHOLE call erroring (e.g. a DB error),
// which this test proves still aborts (releases the claim, propagates the error) rather than
// silently completing.
func TestPastDueService_ProcessUser_GraceExpiry_CancelIfPastDueOccurrenceWithRetentionError_Aborts_ReleasesClaim(t *testing.T) {
	repo := &mockPastDueProfileRepo{}
	plaid := &mockPastDuePlaidCleanup{}
	paddle := &mockPastDuePaddleClient{}
	clerk := &mockPastDueClerkClient{}
	notif := &mockPastDueNotificationRepo{}

	svc := newTestPastDueService(repo, plaid, paddle, clerk, notif)
	mockClaims := new(MockWorkClaimsRepository)
	svc.SetWorkClaimsRepo(mockClaims)

	userID := "user-grace-retention-fail"
	now := time.Now()
	pastDueStarted := now.Add(-time.Duration(testGraceDays+1) * 24 * time.Hour)
	user := userWithPastDue(userID, pastDueStarted)
	subID := *user.PaddleSubscriptionID
	dbErr := errors.New("db unavailable")
	resourceID := pastDueOccurrenceResourceID(t, userID, pastDueStarted)

	repo.On("GetUserByID", mock.Anything, userID).Return(&user, nil)
	mockClaims.On("Claim", mock.Anything, repository.ResourceTypeUser, resourceID, repository.ActionPastDueGraceCancel, pastDueClaimLease).
		Return(testPastDueClaimToken, true, nil)
	plaid.On("DisconnectAllUserItems", mock.Anything, userID).Return(nil)
	mockClaims.On("Claim", mock.Anything, repository.ResourceTypeUser, userID, repository.ActionPaddleCancelSubscription, paddleCancelSideEffectLease).
		Return(testPastDuePaddleCancelSideEffectToken, true, nil)
	mockClaims.On("Release", mock.Anything, repository.ResourceTypeUser, userID, repository.ActionPaddleCancelSubscription, testPastDuePaddleCancelSideEffectToken, mock.AnythingOfType("string")).
		Return(true, nil)
	paddle.On("CancelSubscription", mock.Anything, subID).Return(nil)
	repo.On("CancelIfPastDueOccurrenceWithRetention", mock.Anything, userID, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time"), mock.AnythingOfType("int")).Return(false, dbErr)
	mockClaims.On("Release", mock.Anything, repository.ResourceTypeUser, resourceID, repository.ActionPastDueGraceCancel, testPastDueClaimToken, mock.AnythingOfType("string")).
		Return(true, nil)

	err := svc.ProcessPastDueUser(context.Background(), user, now)

	require.Error(t, err, "a failed CAS+retention write must abort processing so the failure is loud, not complete silently")
	mockClaims.AssertCalled(t, "Release", mock.Anything, repository.ResourceTypeUser, resourceID, repository.ActionPastDueGraceCancel, testPastDueClaimToken, mock.AnythingOfType("string"))
	mockClaims.AssertNotCalled(t, "Complete", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	repo.AssertNotCalled(t, "ClearPastDueStartedAtIfMatches", mock.Anything, mock.Anything, mock.Anything)
	clerk.AssertNotCalled(t, "UpdateUserMetadata", mock.Anything, mock.Anything, mock.Anything)
	notif.AssertNotCalled(t, "Create", mock.Anything, mock.Anything)
}

// --- Issue #1413 P5: NULL occurrence discriminator ---
//
// The normal entry point, ProcessPastDueUser, guards against a nil PastDueStartedAt at its very
// top (`if user.PastDueStartedAt == nil { return nil }`), so these two tests call
// processGraceExpiry/processPlaidDisconnectWindow directly to exercise the defensive path a
// caller bug (or a future code change removing that guard) could otherwise reach. The decision
// pinned here: refuse via repository.ErrMissingOccurrenceDiscriminator rather than silently
// collapsing to the bare userID — see work_claims_occurrence.go's doc comment for why a silent
// collapse would reintroduce the exact permanent-block bug this task fixes. mockClaims has NO
// expectations registered, so any Claim/Complete/Release call would panic — proving the claim
// layer is never reached at all on this path.

func TestPastDueService_ProcessGraceExpiry_NilPastDueStartedAt_ReturnsErrorWithoutClaiming(t *testing.T) {
	repo := &mockPastDueProfileRepo{}
	plaid := &mockPastDuePlaidCleanup{}
	paddle := &mockPastDuePaddleClient{}
	clerk := &mockPastDueClerkClient{}
	notif := &mockPastDueNotificationRepo{}

	svc := newTestPastDueService(repo, plaid, paddle, clerk, notif)
	mockClaims := new(MockWorkClaimsRepository)
	svc.SetWorkClaimsRepo(mockClaims)

	ctx := context.Background()
	user := domain.User{ID: "user-nil-discriminator-grace", SubscriptionStatus: string(domain.SubscriptionStatusPastDue)}
	// user.PastDueStartedAt is deliberately left nil.
	freshUser := user
	repo.On("GetUserByID", mock.Anything, user.ID).Return(&freshUser, nil)

	err := svc.processGraceExpiry(ctx, user, time.Now())

	require.Error(t, err, "a missing occurrence discriminator must fail loudly, not silently claim on the bare userID")
	assert.ErrorIs(t, err, repository.ErrMissingOccurrenceDiscriminator)
}

func TestPastDueService_ProcessPlaidDisconnectWindow_NilPastDueStartedAt_ReturnsErrorWithoutClaiming(t *testing.T) {
	repo := &mockPastDueProfileRepo{}
	plaid := &mockPastDuePlaidCleanup{}
	paddle := &mockPastDuePaddleClient{}
	clerk := &mockPastDueClerkClient{}
	notif := &mockPastDueNotificationRepo{}

	svc := newTestPastDueService(repo, plaid, paddle, clerk, notif)
	mockClaims := new(MockWorkClaimsRepository)
	svc.SetWorkClaimsRepo(mockClaims)

	ctx := context.Background()
	user := domain.User{ID: "user-nil-discriminator-plaid", SubscriptionStatus: string(domain.SubscriptionStatusPastDue)}
	// user.PastDueStartedAt is deliberately left nil.
	freshUser := user
	repo.On("GetUserByID", mock.Anything, user.ID).Return(&freshUser, nil)

	err := svc.processPlaidDisconnectWindow(ctx, user)

	require.Error(t, err, "a missing occurrence discriminator must fail loudly, not silently claim on the bare userID")
	assert.ErrorIs(t, err, repository.ErrMissingOccurrenceDiscriminator)
}

// ─────────────────────────────────────────────────────────────────────────────────────────────────
// THE SIBLING OF ExecuteDeletion'S DEFECT, in the past_due grace-cancel flow.
//
// processGraceExpiry re-verifies the dunning precondition immediately before Step 1 (the Plaid
// disconnect) — "already as late as the code can make this check", as its own comment says, and true
// of that call. But Step 2 (cancelling the Paddle subscription) is a SECOND irreversible external
// call, performed after Step 1's network round-trip, and nothing re-verified in between. A user who
// paid their invoice while we were disconnecting their banks therefore still had their subscription
// cancelled at Paddle, after which Step 3's CAS correctly refused to flip their local status —
// leaving a paid-up customer with no subscription at Paddle and a local row saying nothing happened.
//
// Same sentence as the deletion defect with different nouns, so it is fixed rather than deferred.
// What it is NOT is the same severity: nothing here hard-deletes, and no PII is destroyed.
// ─────────────────────────────────────────────────────────────────────────────────────────────────

// RED: recovery lands BETWEEN the Plaid disconnect and the Paddle cancel.
//
// A user who is already recovered at the pre-Step-1 re-read never reaches Step 1 at all, so seeding
// one would prove nothing — the recovery has to happen inside the window that actually exists. The
// Plaid mock's Run is what puts it there.
func TestPastDueService_ProcessUser_GraceExpiry_RecoveryDuringPlaidDisconnect_DoesNotCancelPaddle(t *testing.T) {
	repo := &mockPastDueProfileRepo{}
	plaid := &mockPastDuePlaidCleanup{}
	paddle := &mockPastDuePaddleClient{}
	clerk := &mockPastDueClerkClient{}
	notif := &mockPastDueNotificationRepo{}

	svc := newTestPastDueService(repo, plaid, paddle, clerk, notif)

	userID := "user-grace-recovers-mid-flight"
	now := time.Now()
	pastDueStarted := now.Add(-time.Duration(testGraceDays+1) * 24 * time.Hour)
	user := userWithPastDue(userID, pastDueStarted)

	// current is what every reReadStillPastDue re-read sees. It starts past_due — so the flow gets as
	// far as Step 1 — and the Plaid mock flips it to active, standing in for the invoice being paid
	// during that call.
	current := userWithPastDue(userID, pastDueStarted)
	repo.On("GetUserByID", mock.Anything, userID).Return(&current, nil)
	plaid.On("DisconnectAllUserItems", mock.Anything, userID).Return(nil).Run(func(mock.Arguments) {
		current.SubscriptionStatus = string(domain.SubscriptionStatusActive)
	})

	err := svc.ProcessPastDueUser(context.Background(), user, now)
	require.NoError(t, err, "a mid-flight recovery is a legitimate skip, not a failure")

	paddle.AssertNotCalled(t, "CancelSubscription", mock.Anything, mock.Anything)
	repo.AssertNotCalled(t, "CancelIfPastDueOccurrenceWithRetention", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	plaid.AssertCalled(t, "DisconnectAllUserItems", mock.Anything, userID)
}

// SIBLING ASSERTION, mandatory: the check above must not be satisfied by a flow that stopped
// cancelling subscriptions altogether. Same shape, no mid-flight recovery, and Step 2 plus the CAS
// still run.
//
// TestPastDueService_ProcessUser_GraceExpiry_FullFlow covers the same ground; this pairing exists so
// the RED test is falsifiable on its own terms rather than depending on a test written for another
// purpose. Verified by mutation: forcing the new pre-Step-2 re-check to report "not past due" reddens
// both this and FullFlow.
func TestPastDueService_ProcessUser_GraceExpiry_NoRecovery_StillCancelsPaddle(t *testing.T) {
	repo := &mockPastDueProfileRepo{}
	plaid := &mockPastDuePlaidCleanup{}
	paddle := &mockPastDuePaddleClient{}
	clerk := &mockPastDueClerkClient{}
	notif := &mockPastDueNotificationRepo{}

	svc := newTestPastDueService(repo, plaid, paddle, clerk, notif)

	userID := "user-grace-no-recovery"
	now := time.Now()
	pastDueStarted := now.Add(-time.Duration(testGraceDays+1) * 24 * time.Hour)
	user := userWithPastDue(userID, pastDueStarted)
	subID := *user.PaddleSubscriptionID

	current := userWithPastDue(userID, pastDueStarted)
	repo.On("GetUserByID", mock.Anything, userID).Return(&current, nil)
	plaid.On("DisconnectAllUserItems", mock.Anything, userID).Return(nil)
	paddle.On("CancelSubscription", mock.Anything, subID).Return(nil)
	repo.On("CancelIfPastDueOccurrenceWithRetention", mock.Anything, userID, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time"), mock.AnythingOfType("int")).Return(true, nil)
	repo.On("ClearPastDueStartedAtIfMatches", mock.Anything, userID, mock.AnythingOfType("time.Time")).Return(true, nil)
	clerk.On("UpdateUserMetadata", mock.Anything, current.ClerkUserID, mock.Anything).Return(nil)
	notif.On("Create", mock.Anything, mock.Anything).Return(nil)

	require.NoError(t, svc.ProcessPastDueUser(context.Background(), user, now))
	paddle.AssertCalled(t, "CancelSubscription", mock.Anything, subID)
	repo.AssertCalled(t, "CancelIfPastDueOccurrenceWithRetention", mock.Anything, userID, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time"), mock.AnythingOfType("int"))
}
