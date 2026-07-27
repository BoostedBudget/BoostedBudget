// Package services provides business logic and external service integrations.
// Tests for PastDueService — Issue #898: dunning grace-expiry logic.
package services

import (
	"context"
	"errors"
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

func (m *mockPastDueProfileRepo) CancelIfPastDue(ctx context.Context, userID string) (bool, error) {
	args := m.Called(ctx, userID)
	return args.Bool(0), args.Error(1)
}

func (m *mockPastDueProfileRepo) SetRetentionDates(ctx context.Context, userID string, started, expires time.Time) error {
	args := m.Called(ctx, userID, started, expires)
	return args.Error(0)
}

func (m *mockPastDueProfileRepo) ClearPastDueStartedAt(ctx context.Context, userID string) error {
	args := m.Called(ctx, userID)
	return args.Error(0)
}

type mockPastDuePlaidCleanup struct {
	mock.Mock
}

func (m *mockPastDuePlaidCleanup) DisconnectAllUserItems(ctx context.Context, userID string) error {
	args := m.Called(ctx, userID)
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
	testGraceDays          = 21
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
	return m
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
	repo.On("CancelIfPastDue", mock.Anything, userID).Return(true, nil)
	repo.On("SetRetentionDates", mock.Anything, userID, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time")).Return(nil)
	repo.On("ClearPastDueStartedAt", mock.Anything, userID).Return(nil)
	clerk.On("UpdateUserMetadata", mock.Anything, user.ClerkUserID, mock.MatchedBy(func(m map[string]interface{}) bool {
		return m["subscription_status"] == "cancelled"
	})).Return(nil)
	notif.On("Create", mock.Anything, mock.MatchedBy(func(n *domain.Notification) bool {
		return n.Type == "subscription_cancelled" && n.UserID == userID
	})).Return(nil)

	err := svc.ProcessPastDueUser(context.Background(), user, now)
	require.NoError(t, err)

	repo.AssertCalled(t, "GetUserByID", mock.Anything, userID)
	plaid.AssertCalled(t, "DisconnectAllUserItems", mock.Anything, userID)
	paddle.AssertCalled(t, "CancelSubscription", mock.Anything, subID)
	repo.AssertCalled(t, "CancelIfPastDue", mock.Anything, userID)
	repo.AssertCalled(t, "SetRetentionDates", mock.Anything, userID, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time"))
	repo.AssertCalled(t, "ClearPastDueStartedAt", mock.Anything, userID)
	clerk.AssertCalled(t, "UpdateUserMetadata", mock.Anything, user.ClerkUserID, mock.Anything)
	notif.AssertCalled(t, "Create", mock.Anything, mock.Anything)
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
	repo.AssertNotCalled(t, "CancelIfPastDue", mock.Anything, mock.Anything)
	repo.AssertNotCalled(t, "ClearPastDueStartedAt", mock.Anything, mock.Anything)
	clerk.AssertNotCalled(t, "UpdateUserMetadata", mock.Anything, mock.Anything, mock.Anything)
	notif.AssertNotCalled(t, "Create", mock.Anything, mock.Anything)
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
	repo.On("CancelIfPastDue", mock.Anything, userID).Return(true, nil)
	repo.On("SetRetentionDates", mock.Anything, userID, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time")).Return(nil)
	repo.On("ClearPastDueStartedAt", mock.Anything, userID).Return(nil)
	clerk.On("UpdateUserMetadata", mock.Anything, user.ClerkUserID, mock.Anything).Return(nil)
	notif.On("Create", mock.Anything, mock.Anything).Return(nil)

	err := svc.ProcessPastDueUser(context.Background(), user, now)
	require.NoError(t, err, "already-cancelled Paddle error should be tolerated")

	repo.AssertCalled(t, "CancelIfPastDue", mock.Anything, userID)
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
	repo.AssertNotCalled(t, "CancelIfPastDue", mock.Anything, mock.Anything)
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

	repo.On("GetUserByID", mock.Anything, userID).Return(&user, nil)
	// The claim reports false — another worker holds it, or it's already completed.
	mockClaims.On("Claim", mock.Anything, repository.ResourceTypeUser, userID, repository.ActionPlaidDisconnectPastDue, pastDueClaimLease).
		Return("", false, nil)

	err := svc.ProcessPastDueUser(context.Background(), user, now)
	require.NoError(t, err, "a lost claim is a normal skip, not a job failure")

	mockClaims.AssertCalled(t, "Claim", mock.Anything, repository.ResourceTypeUser, userID, repository.ActionPlaidDisconnectPastDue, pastDueClaimLease)
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

	plaidErr := errors.New("plaid API error")
	repo.On("GetUserByID", mock.Anything, userID).Return(&user, nil)
	mockClaims.On("Claim", mock.Anything, repository.ResourceTypeUser, userID, repository.ActionPlaidDisconnectPastDue, pastDueClaimLease).
		Return(testPastDueClaimToken, true, nil)
	plaid.On("DisconnectAllUserItems", mock.Anything, userID).Return(plaidErr)
	mockClaims.On("Release", mock.Anything, repository.ResourceTypeUser, userID, repository.ActionPlaidDisconnectPastDue, testPastDueClaimToken, plaidErr.Error()).
		Return(true, nil)

	err := svc.ProcessPastDueUser(context.Background(), user, now)
	require.Error(t, err, "Plaid failure must propagate so the daily job retries")

	mockClaims.AssertCalled(t, "Release", mock.Anything, repository.ResourceTypeUser, userID, repository.ActionPlaidDisconnectPastDue, testPastDueClaimToken, plaidErr.Error())
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

	repo.On("GetUserByID", mock.Anything, userID).Return(&user, nil)
	mockClaims.On("Claim", mock.Anything, repository.ResourceTypeUser, userID, repository.ActionPlaidDisconnectPastDue, pastDueClaimLease).
		Return(testPastDueClaimToken, true, nil)
	plaid.On("DisconnectAllUserItems", mock.Anything, userID).Return(nil)
	notif.On("Create", mock.Anything, mock.Anything).Return(nil)
	mockClaims.On("Complete", mock.Anything, repository.ResourceTypeUser, userID, repository.ActionPlaidDisconnectPastDue, testPastDueClaimToken).
		Return(true, nil)

	err := svc.ProcessPastDueUser(context.Background(), user, now)
	require.NoError(t, err)

	mockClaims.AssertCalled(t, "Complete", mock.Anything, repository.ResourceTypeUser, userID, repository.ActionPlaidDisconnectPastDue, testPastDueClaimToken)
	plaid.AssertCalled(t, "DisconnectAllUserItems", mock.Anything, userID)
}

// TestPastDueService_ProcessUser_PlaidDisconnect_CompleteReturnsFalse_LoggedNotSwallowed proves a
// lost lease at Complete time (another worker reclaimed this tuple while we were mid-flight) does
// not silently pass as success and is not reported as a processing failure — mirrors
// deletion_service_test.go's TestExecuteDeletion_CompleteReturnsFalse_LoggedNotSwallowed. The
// loud-logging behavior itself is inspected in the source (processPlaidDisconnectWindow's Complete
// handling) and is not independently mechanically asserted here for lack of an injectable log sink.
func TestPastDueService_ProcessUser_PlaidDisconnect_CompleteReturnsFalse_LoggedNotSwallowed(t *testing.T) {
	repo := &mockPastDueProfileRepo{}
	plaid := &mockPastDuePlaidCleanup{}
	paddle := &mockPastDuePaddleClient{}
	clerk := &mockPastDueClerkClient{}
	notif := &mockPastDueNotificationRepo{}

	svc := newTestPastDueService(repo, plaid, paddle, clerk, notif)
	mockClaims := new(MockWorkClaimsRepository)
	svc.SetWorkClaimsRepo(mockClaims)

	userID := "user-plaid-complete-false"
	now := time.Now()
	pastDueStarted := now.Add(-15 * 24 * time.Hour)
	user := userWithPastDue(userID, pastDueStarted)

	repo.On("GetUserByID", mock.Anything, userID).Return(&user, nil)
	mockClaims.On("Claim", mock.Anything, repository.ResourceTypeUser, userID, repository.ActionPlaidDisconnectPastDue, pastDueClaimLease).
		Return(testPastDueClaimToken, true, nil)
	plaid.On("DisconnectAllUserItems", mock.Anything, userID).Return(nil)
	notif.On("Create", mock.Anything, mock.Anything).Return(nil)
	// The lease was reclaimed by another worker before we could Complete it.
	mockClaims.On("Complete", mock.Anything, repository.ResourceTypeUser, userID, repository.ActionPlaidDisconnectPastDue, testPastDueClaimToken).
		Return(false, nil)

	err := svc.ProcessPastDueUser(context.Background(), user, now)

	assert.NoError(t, err, "the disconnect already ran; a lost lease at Complete time must not be reported as a failure")
	mockClaims.AssertCalled(t, "Complete", mock.Anything, repository.ResourceTypeUser, userID, repository.ActionPlaidDisconnectPastDue, testPastDueClaimToken)
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
	repo.AssertNotCalled(t, "CancelIfPastDue", mock.Anything, mock.Anything)
	repo.AssertNotCalled(t, "GetUserByID", mock.Anything, mock.Anything)
}

// Test 7: Grace expiry but CancelIfPastDue returns false (user recovered between Plaid
// disconnect and the CAS flip) — no retention, no notification, returns nil.
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
	repo.On("CancelIfPastDue", mock.Anything, userID).Return(false, nil)

	err := svc.ProcessPastDueUser(context.Background(), user, now)
	require.NoError(t, err, "recovered user should not produce an error")

	repo.AssertCalled(t, "GetUserByID", mock.Anything, userID)
	repo.AssertCalled(t, "CancelIfPastDue", mock.Anything, userID)
	repo.AssertNotCalled(t, "SetRetentionDates", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	repo.AssertNotCalled(t, "ClearPastDueStartedAt", mock.Anything, mock.Anything)
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
	repo.AssertNotCalled(t, "CancelIfPastDue", mock.Anything, mock.Anything)
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
	repo.On("CancelIfPastDue", mock.Anything, userID).Return(true, nil)
	repo.On("SetRetentionDates", mock.Anything, userID, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time")).Return(nil)
	repo.On("ClearPastDueStartedAt", mock.Anything, userID).Return(nil)
	clerk.On("UpdateUserMetadata", mock.Anything, user.ClerkUserID, mock.Anything).Return(nil)
	notif.On("Create", mock.Anything, mock.Anything).Return(nil)

	// Must not panic even though user has a PaddleSubscriptionID and paddleClient is nil.
	assert.NotPanics(t, func() {
		err := svc.ProcessPastDueUser(context.Background(), user, now)
		assert.NoError(t, err)
	})
	// CAS flip still occurs; Paddle was correctly skipped (nil guard took effect).
	repo.AssertCalled(t, "CancelIfPastDue", mock.Anything, userID)
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
	repo.AssertNotCalled(t, "CancelIfPastDue", mock.Anything, mock.Anything)
	repo.AssertNotCalled(t, "SetRetentionDates", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
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
	repo.AssertNotCalled(t, "CancelIfPastDue", mock.Anything, mock.Anything)
	repo.AssertNotCalled(t, "SetRetentionDates", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
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
	repo.On("CancelIfPastDue", mock.Anything, userID).Return(true, nil)
	repo.On("SetRetentionDates", mock.Anything, userID, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time")).Return(nil)
	repo.On("ClearPastDueStartedAt", mock.Anything, userID).Return(nil)
	clerk.On("UpdateUserMetadata", mock.Anything, user.ClerkUserID, mock.Anything).Return(nil)
	notif.On("Create", mock.Anything, mock.Anything).Return(nil)

	err := svc.ProcessPastDueUser(context.Background(), user, now)
	require.NoError(t, err)

	repo.AssertCalled(t, "CancelIfPastDue", mock.Anything, userID)
	plaid.AssertCalled(t, "DisconnectAllUserItems", mock.Anything, userID)
}

// Test 14: household-covered user is skipped entirely (the new hook, mirroring TrialService).
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
	repo.AssertNotCalled(t, "CancelIfPastDue", mock.Anything, mock.Anything)
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
	repo.On("CancelIfPastDue", mock.Anything, userID).Return(true, nil)
	repo.On("SetRetentionDates", mock.Anything, userID, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time")).Return(nil)
	repo.On("ClearPastDueStartedAt", mock.Anything, userID).Return(nil)
	clerk.On("UpdateUserMetadata", mock.Anything, user.ClerkUserID, mock.Anything).Return(nil)
	notif.On("Create", mock.Anything, mock.Anything).Return(nil)

	err := svc.ProcessPastDueUser(context.Background(), user, now)
	require.NoError(t, err)

	repo.AssertCalled(t, "CancelIfPastDue", mock.Anything, userID)
}

// Test 16: household checker fails — fail closed (skip destructive action) but propagate the
// error, mirroring TrialService.ExpireTrial's fail-closed-with-error-propagation semantics
// (see trial_service.go:197-204 for why swallowing would hide an operational failure).
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
	repo.AssertNotCalled(t, "CancelIfPastDue", mock.Anything, mock.Anything)
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

	var callOrder []string
	repo.On("GetUserByID", mock.Anything, userID).Return(&user, nil)
	mockClaims.On("Claim", mock.Anything, repository.ResourceTypeUser, userID, repository.ActionPastDueGraceCancel, pastDueClaimLease).
		Run(func(mock.Arguments) { callOrder = append(callOrder, "claim") }).
		Return(testPastDueClaimToken, true, nil)
	plaid.On("DisconnectAllUserItems", mock.Anything, userID).
		Run(func(mock.Arguments) { callOrder = append(callOrder, "plaid_disconnect") }).
		Return(nil)
	paddle.On("CancelSubscription", mock.Anything, subID).
		Run(func(mock.Arguments) { callOrder = append(callOrder, "paddle_cancel") }).
		Return(nil)
	repo.On("CancelIfPastDue", mock.Anything, userID).
		Run(func(mock.Arguments) { callOrder = append(callOrder, "cas_flip") }).
		Return(true, nil)
	mockClaims.On("Complete", mock.Anything, repository.ResourceTypeUser, userID, repository.ActionPastDueGraceCancel, testPastDueClaimToken).
		Return(true, nil)
	repo.On("SetRetentionDates", mock.Anything, userID, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time")).Return(nil)
	repo.On("ClearPastDueStartedAt", mock.Anything, userID).Return(nil)
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

	repo.On("GetUserByID", mock.Anything, userID).Return(&user, nil)
	mockClaims.On("Claim", mock.Anything, repository.ResourceTypeUser, userID, repository.ActionPastDueGraceCancel, pastDueClaimLease).
		Return("", false, nil)

	err := svc.ProcessPastDueUser(context.Background(), user, now)
	require.NoError(t, err, "a lost claim is a normal skip, not a job failure")

	plaid.AssertNotCalled(t, "DisconnectAllUserItems", mock.Anything, mock.Anything)
	paddle.AssertNotCalled(t, "CancelSubscription", mock.Anything, mock.Anything)
	repo.AssertNotCalled(t, "CancelIfPastDue", mock.Anything, mock.Anything)
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

	plaidErr := errors.New("plaid API error")
	repo.On("GetUserByID", mock.Anything, userID).Return(&user, nil)
	mockClaims.On("Claim", mock.Anything, repository.ResourceTypeUser, userID, repository.ActionPastDueGraceCancel, pastDueClaimLease).
		Return(testPastDueClaimToken, true, nil)
	plaid.On("DisconnectAllUserItems", mock.Anything, userID).Return(plaidErr)
	mockClaims.On("Release", mock.Anything, repository.ResourceTypeUser, userID, repository.ActionPastDueGraceCancel, testPastDueClaimToken, plaidErr.Error()).
		Return(true, nil)

	err := svc.ProcessPastDueUser(context.Background(), user, now)
	require.Error(t, err)

	mockClaims.AssertCalled(t, "Release", mock.Anything, repository.ResourceTypeUser, userID, repository.ActionPastDueGraceCancel, testPastDueClaimToken, plaidErr.Error())
	paddle.AssertNotCalled(t, "CancelSubscription", mock.Anything, mock.Anything)
	repo.AssertNotCalled(t, "CancelIfPastDue", mock.Anything, mock.Anything)
	mockClaims.AssertNotCalled(t, "Complete", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

// TestPastDueService_ProcessUser_GraceExpiry_CASMiss_ReleasesNotCompletes proves that when the
// final CancelIfPastDue CAS misses (comp/recovery landed during the Plaid/Paddle calls), the claim
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

	repo.On("GetUserByID", mock.Anything, userID).Return(&user, nil)
	mockClaims.On("Claim", mock.Anything, repository.ResourceTypeUser, userID, repository.ActionPastDueGraceCancel, pastDueClaimLease).
		Return(testPastDueClaimToken, true, nil)
	plaid.On("DisconnectAllUserItems", mock.Anything, userID).Return(nil)
	paddle.On("CancelSubscription", mock.Anything, subID).Return(nil)
	// CAS misses — recovered or comped during the external calls above.
	repo.On("CancelIfPastDue", mock.Anything, userID).Return(false, nil)
	mockClaims.On("Release", mock.Anything, repository.ResourceTypeUser, userID, repository.ActionPastDueGraceCancel, testPastDueClaimToken, mock.AnythingOfType("string")).
		Return(true, nil)

	err := svc.ProcessPastDueUser(context.Background(), user, now)
	require.NoError(t, err)

	mockClaims.AssertCalled(t, "Release", mock.Anything, repository.ResourceTypeUser, userID, repository.ActionPastDueGraceCancel, testPastDueClaimToken, mock.AnythingOfType("string"))
	mockClaims.AssertNotCalled(t, "Complete", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	repo.AssertNotCalled(t, "SetRetentionDates", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

// TestPastDueService_ProcessUser_GraceExpiry_CompleteReturnsFalse_LoggedNotSwallowed mirrors
// deletion_service_test.go's TestExecuteDeletion_CompleteReturnsFalse_LoggedNotSwallowed: a lost
// lease at Complete time must not be reported as a processing failure. The loud-logging behavior
// itself is inspected in the source (processGraceExpiry's Complete handling) and is not
// independently mechanically asserted here for lack of an injectable log sink.
func TestPastDueService_ProcessUser_GraceExpiry_CompleteReturnsFalse_LoggedNotSwallowed(t *testing.T) {
	repo := &mockPastDueProfileRepo{}
	plaid := &mockPastDuePlaidCleanup{}
	paddle := &mockPastDuePaddleClient{}
	clerk := &mockPastDueClerkClient{}
	notif := &mockPastDueNotificationRepo{}

	svc := newTestPastDueService(repo, plaid, paddle, clerk, notif)
	mockClaims := new(MockWorkClaimsRepository)
	svc.SetWorkClaimsRepo(mockClaims)

	userID := "user-grace-complete-false"
	now := time.Now()
	pastDueStarted := now.Add(-time.Duration(testGraceDays+1) * 24 * time.Hour)
	user := userWithPastDue(userID, pastDueStarted)
	subID := *user.PaddleSubscriptionID

	repo.On("GetUserByID", mock.Anything, userID).Return(&user, nil)
	mockClaims.On("Claim", mock.Anything, repository.ResourceTypeUser, userID, repository.ActionPastDueGraceCancel, pastDueClaimLease).
		Return(testPastDueClaimToken, true, nil)
	plaid.On("DisconnectAllUserItems", mock.Anything, userID).Return(nil)
	paddle.On("CancelSubscription", mock.Anything, subID).Return(nil)
	repo.On("CancelIfPastDue", mock.Anything, userID).Return(true, nil)
	repo.On("SetRetentionDates", mock.Anything, userID, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time")).Return(nil)
	repo.On("ClearPastDueStartedAt", mock.Anything, userID).Return(nil)
	clerk.On("UpdateUserMetadata", mock.Anything, user.ClerkUserID, mock.Anything).Return(nil)
	notif.On("Create", mock.Anything, mock.Anything).Return(nil)
	// The lease was reclaimed by another worker before we could Complete it.
	mockClaims.On("Complete", mock.Anything, repository.ResourceTypeUser, userID, repository.ActionPastDueGraceCancel, testPastDueClaimToken).
		Return(false, nil)

	err := svc.ProcessPastDueUser(context.Background(), user, now)

	assert.NoError(t, err, "the cancellation already ran; a lost lease at Complete time must not be reported as a failure")
	mockClaims.AssertCalled(t, "Complete", mock.Anything, repository.ResourceTypeUser, userID, repository.ActionPastDueGraceCancel, testPastDueClaimToken)
}
