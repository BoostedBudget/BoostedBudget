package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/config"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// mockTrialRepository is a mock implementation of TrialRepository for testing
type mockTrialRepository struct {
	mock.Mock
}

func (m *mockTrialRepository) GetUserByID(ctx context.Context, id string) (*domain.User, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.User), args.Error(1)
}

func (m *mockTrialRepository) UpdateTrialStatus(ctx context.Context, userID string, status string, trialStartedAt, trialEndsAt *time.Time, hadTrial bool) error {
	args := m.Called(ctx, userID, status, trialStartedAt, trialEndsAt, hadTrial)
	return args.Error(0)
}

func (m *mockTrialRepository) ExpireTrialIfNoOverride(ctx context.Context, userID string) (bool, error) {
	args := m.Called(ctx, userID)
	return args.Bool(0), args.Error(1)
}

func (m *mockTrialRepository) GetUsersWithExpiredTrials(ctx context.Context) ([]*domain.User, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.User), args.Error(1)
}

func (m *mockTrialRepository) GetUsersWithExpiringTrials(ctx context.Context, withinDays int) ([]*domain.User, error) {
	args := m.Called(ctx, withinDays)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.User), args.Error(1)
}

// #930: SetRetentionDates stub — allows mock to satisfy updated TrialRepository interface
func (m *mockTrialRepository) SetRetentionDates(ctx context.Context, userID string, startedAt, expiresAt time.Time) error {
	return m.Called(ctx, userID, startedAt, expiresAt).Error(0)
}

// mockPlaidCleanupService is a mock for Plaid cleanup
type mockPlaidCleanupService struct {
	mock.Mock
}

func (m *mockPlaidCleanupService) DisconnectAllUserItems(ctx context.Context, userID string) error {
	args := m.Called(ctx, userID)
	return args.Error(0)
}

// testTrialExpirationClaimToken is the fixed fencing token newAllowingTrialClaimsRepo's Claim
// stub returns. Tests that only care about ExpireTrial's OTHER behavior can assert Complete was
// called with this exact value without plumbing a real per-test token through.
const testTrialExpirationClaimToken = "test-trial-expiration-claim-token"

// newAllowingTrialClaimsRepo returns a MockWorkClaimsRepository stubbed to always grant the
// ActionTrialExpiration claim and always complete it successfully — Issue #1413 round-4 finding
// H2. For tests that exercise ExpireTrial's OTHER behavior and aren't themselves testing the
// claim mechanism. userID is wildcarded (mock.Anything) so one instance works across this file's
// whole matrix of fixture users.
func newAllowingTrialClaimsRepo() *MockWorkClaimsRepository {
	m := new(MockWorkClaimsRepository)
	m.On("Claim", mock.Anything, repository.ResourceTypeUser, mock.Anything, repository.ActionTrialExpiration, trialExpirationClaimLease).
		Return(testTrialExpirationClaimToken, true, nil).Maybe()
	m.On("Complete", mock.Anything, repository.ResourceTypeUser, mock.Anything, repository.ActionTrialExpiration, testTrialExpirationClaimToken).
		Return(true, nil).Maybe()
	m.On("Release", mock.Anything, repository.ResourceTypeUser, mock.Anything, repository.ActionTrialExpiration, testTrialExpirationClaimToken, mock.AnythingOfType("string")).
		Return(true, nil).Maybe()
	return m
}

func setupTestTrialService(t *testing.T) (*TrialService, *mockTrialRepository, *mockPlaidCleanupService) {
	t.Helper()

	// Load tier config for trial duration
	tierConfig, err := config.NewTierConfig("")
	require.NoError(t, err)

	repo := new(mockTrialRepository)
	plaidCleanup := new(mockPlaidCleanupService)

	service := NewTrialService(tierConfig, repo, plaidCleanup)
	// Issue #1413 round-4 finding H2: ExpireTrial now requires a work claims repo. Wired
	// permissively by default so every pre-existing test in this file exercising ExpireTrial's
	// OTHER behavior doesn't have to plumb claim expectations through — tests that DO exercise
	// the claim mechanism itself wire their own MockWorkClaimsRepository instead.
	service.SetWorkClaimsRepo(newAllowingTrialClaimsRepo())

	return service, repo, plaidCleanup
}

func TestTrialService_GrantTrial_Success(t *testing.T) {
	service, repo, _ := setupTestTrialService(t)
	ctx := context.Background()
	userID := "user-123"

	// User who hasn't had a trial
	user := &domain.User{
		ID:                 userID,
		SubscriptionTier:   "premium",
		SubscriptionStatus: "active",
		HadTrial:           false,
	}

	repo.On("GetUserByID", ctx, userID).Return(user, nil)
	repo.On("UpdateTrialStatus", ctx, userID, "trial", mock.AnythingOfType("*time.Time"), mock.AnythingOfType("*time.Time"), true).Return(nil)

	err := service.GrantTrial(ctx, userID)

	require.NoError(t, err)
	repo.AssertExpectations(t)
}

func TestTrialService_GrantTrial_AlreadyHadTrial(t *testing.T) {
	service, repo, _ := setupTestTrialService(t)
	ctx := context.Background()
	userID := "user-123"

	// User who already had a trial
	user := &domain.User{
		ID:                 userID,
		SubscriptionTier:   "premium",
		SubscriptionStatus: "inactive",
		HadTrial:           true,
	}

	repo.On("GetUserByID", ctx, userID).Return(user, nil)

	err := service.GrantTrial(ctx, userID)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "already had a trial")
	repo.AssertExpectations(t)
}

func TestTrialService_IsTrialActive_Active(t *testing.T) {
	service, _, _ := setupTestTrialService(t)

	// User with active trial (ends in the future)
	trialEndsAt := time.Now().Add(30 * 24 * time.Hour)
	user := &domain.User{
		ID:                 "user-123",
		SubscriptionStatus: "trial",
		TrialEndsAt:        &trialEndsAt,
	}

	isActive := service.IsTrialActive(user)

	assert.True(t, isActive)
}

func TestTrialService_IsTrialActive_Expired(t *testing.T) {
	service, _, _ := setupTestTrialService(t)

	// User with expired trial (ended in the past)
	trialEndsAt := time.Now().Add(-1 * time.Hour)
	user := &domain.User{
		ID:                 "user-123",
		SubscriptionStatus: "trial",
		TrialEndsAt:        &trialEndsAt,
	}

	isActive := service.IsTrialActive(user)

	assert.False(t, isActive)
}

func TestTrialService_IsTrialActive_NotTrial(t *testing.T) {
	service, _, _ := setupTestTrialService(t)

	// User with active subscription (not trial)
	user := &domain.User{
		ID:                 "user-123",
		SubscriptionStatus: "active",
	}

	isActive := service.IsTrialActive(user)

	assert.False(t, isActive)
}

func TestTrialService_GetTrialDaysRemaining(t *testing.T) {
	service, _, _ := setupTestTrialService(t)

	// User with 30 days remaining
	trialEndsAt := time.Now().Add(30 * 24 * time.Hour)
	user := &domain.User{
		ID:                 "user-123",
		SubscriptionStatus: "trial",
		TrialEndsAt:        &trialEndsAt,
	}

	days := service.GetTrialDaysRemaining(user)

	// Should be approximately 30 days (allow for test execution time)
	assert.GreaterOrEqual(t, days, 29)
	assert.LessOrEqual(t, days, 30)
}

func TestTrialService_GetTrialDaysRemaining_Expired(t *testing.T) {
	service, _, _ := setupTestTrialService(t)

	// User with expired trial
	trialEndsAt := time.Now().Add(-5 * 24 * time.Hour)
	user := &domain.User{
		ID:                 "user-123",
		SubscriptionStatus: "trial",
		TrialEndsAt:        &trialEndsAt,
	}

	days := service.GetTrialDaysRemaining(user)

	assert.Equal(t, 0, days)
}

func TestTrialService_ExpireTrial_Success(t *testing.T) {
	service, repo, plaidCleanup := setupTestTrialService(t)
	ctx := context.Background()
	userID := "user-123"

	// User with expired trial
	trialEndsAt := time.Now().Add(-1 * time.Hour)
	user := &domain.User{
		ID:                 userID,
		SubscriptionStatus: "trial",
		TrialEndsAt:        &trialEndsAt,
	}

	repo.On("GetUserByID", ctx, userID).Return(user, nil)
	repo.On("ExpireTrialIfNoOverride", ctx, userID).Return(true, nil)
	repo.On("SetRetentionDates", ctx, userID, mock.Anything, mock.Anything).Return(nil)
	plaidCleanup.On("DisconnectAllUserItems", ctx, userID).Return(nil)

	err := service.ExpireTrial(ctx, userID)

	require.NoError(t, err)
	repo.AssertExpectations(t)
	plaidCleanup.AssertExpectations(t)
}

func TestTrialService_ExpireTrial_DisconnectsPlaidItems(t *testing.T) {
	service, repo, plaidCleanup := setupTestTrialService(t)
	ctx := context.Background()
	userID := "user-123"

	trialEndsAt := time.Now().Add(-1 * time.Hour)
	user := &domain.User{
		ID:                 userID,
		SubscriptionStatus: "trial",
		TrialEndsAt:        &trialEndsAt,
	}

	repo.On("GetUserByID", ctx, userID).Return(user, nil)
	repo.On("ExpireTrialIfNoOverride", ctx, userID).Return(true, nil)
	repo.On("SetRetentionDates", ctx, userID, mock.Anything, mock.Anything).Return(nil)
	plaidCleanup.On("DisconnectAllUserItems", ctx, userID).Return(nil)

	err := service.ExpireTrial(ctx, userID)

	require.NoError(t, err)
	// Verify Plaid cleanup was called
	plaidCleanup.AssertCalled(t, "DisconnectAllUserItems", ctx, userID)
}

// ─── #1367 — Phase 3: covered household members are exempt from trial expiry ──

type mockHouseholdCheckerForTrial struct{ mock.Mock }

func (m *mockHouseholdCheckerForTrial) IsCoveredByHousehold(ctx context.Context, userID string) (bool, error) {
	args := m.Called(ctx, userID)
	return args.Bool(0), args.Error(1)
}

func TestTrialService_ExpireTrial_CoveredByHousehold_Skipped(t *testing.T) {
	service, repo, plaidCleanup := setupTestTrialService(t)
	checker := &mockHouseholdCheckerForTrial{}
	service.SetHouseholdChecker(checker)
	ctx := context.Background()
	userID := "user-123"

	trialEndsAt := time.Now().Add(-1 * time.Hour)
	user := &domain.User{ID: userID, SubscriptionStatus: "trial", TrialEndsAt: &trialEndsAt}
	repo.On("GetUserByID", ctx, userID).Return(user, nil)
	checker.On("IsCoveredByHousehold", ctx, userID).Return(true, nil)

	err := service.ExpireTrial(ctx, userID)

	require.NoError(t, err)
	repo.AssertNotCalled(t, "ExpireTrialIfNoOverride", mock.Anything, mock.Anything)
	plaidCleanup.AssertNotCalled(t, "DisconnectAllUserItems", mock.Anything, mock.Anything)
}

func TestTrialService_ExpireTrial_NotCoveredByHousehold_Proceeds(t *testing.T) {
	service, repo, plaidCleanup := setupTestTrialService(t)
	checker := &mockHouseholdCheckerForTrial{}
	service.SetHouseholdChecker(checker)
	ctx := context.Background()
	userID := "user-123"

	trialEndsAt := time.Now().Add(-1 * time.Hour)
	user := &domain.User{ID: userID, SubscriptionStatus: "trial", TrialEndsAt: &trialEndsAt}
	repo.On("GetUserByID", ctx, userID).Return(user, nil)
	checker.On("IsCoveredByHousehold", ctx, userID).Return(false, nil)
	repo.On("ExpireTrialIfNoOverride", ctx, userID).Return(true, nil)
	repo.On("SetRetentionDates", ctx, userID, mock.Anything, mock.Anything).Return(nil)
	plaidCleanup.On("DisconnectAllUserItems", ctx, userID).Return(nil)

	err := service.ExpireTrial(ctx, userID)

	require.NoError(t, err)
	repo.AssertExpectations(t)
}

func TestTrialService_ExpireTrial_NoHouseholdCheckerConfigured_Proceeds(t *testing.T) {
	// Backward compatibility: no checker wired (nil) must behave exactly as before.
	service, repo, plaidCleanup := setupTestTrialService(t)
	ctx := context.Background()
	userID := "user-123"

	trialEndsAt := time.Now().Add(-1 * time.Hour)
	user := &domain.User{ID: userID, SubscriptionStatus: "trial", TrialEndsAt: &trialEndsAt}
	repo.On("GetUserByID", ctx, userID).Return(user, nil)
	repo.On("ExpireTrialIfNoOverride", ctx, userID).Return(true, nil)
	repo.On("SetRetentionDates", ctx, userID, mock.Anything, mock.Anything).Return(nil)
	plaidCleanup.On("DisconnectAllUserItems", ctx, userID).Return(nil)

	err := service.ExpireTrial(ctx, userID)
	require.NoError(t, err)
	repo.AssertExpectations(t)
}

func TestTrialService_ExpireTrial_HouseholdCheckFails_SkipsDefensivelyButPropagatesError(t *testing.T) {
	// On an uncertain coverage check, fail closed on the destructive action (skip expiry
	// this run) rather than risk wrongly locking out an actually-covered member — but
	// DO propagate the error (Codex review, Medium finding 6) so the daily job's
	// error/retry counting reflects reality instead of silently logging success for a
	// user whose trial was never actually processed. The job retries, so this self-heals
	// once the coverage check starts working again.
	service, repo, plaidCleanup := setupTestTrialService(t)
	checker := &mockHouseholdCheckerForTrial{}
	service.SetHouseholdChecker(checker)
	ctx := context.Background()
	userID := "user-123"

	trialEndsAt := time.Now().Add(-1 * time.Hour)
	user := &domain.User{ID: userID, SubscriptionStatus: "trial", TrialEndsAt: &trialEndsAt}
	repo.On("GetUserByID", ctx, userID).Return(user, nil)
	checker.On("IsCoveredByHousehold", ctx, userID).Return(false, errors.New("lookup failed"))

	err := service.ExpireTrial(ctx, userID)

	require.Error(t, err, "the failure must be visible to the caller's error/retry accounting, not silently swallowed")
	repo.AssertNotCalled(t, "ExpireTrialIfNoOverride", mock.Anything, mock.Anything)
	plaidCleanup.AssertNotCalled(t, "DisconnectAllUserItems", mock.Anything, mock.Anything)
}

// ─── #1413: comped users are exempt from trial-expiry destructive actions ──

func TestTrialService_ExpireTrial_ActiveOverride_Skipped(t *testing.T) {
	service, repo, plaidCleanup := setupTestTrialService(t)
	ctx := context.Background()
	userID := "user-123"

	trialEndsAt := time.Now().Add(-1 * time.Hour)
	override := "household_2"
	overrideEndsAt := time.Now().Add(24 * time.Hour) // still active
	user := &domain.User{
		ID:                 userID,
		SubscriptionStatus: "trial",
		TrialEndsAt:        &trialEndsAt,
		ManualTierOverride: &override,
		ManualTierEndsAt:   &overrideEndsAt,
	}
	repo.On("GetUserByID", ctx, userID).Return(user, nil)

	err := service.ExpireTrial(ctx, userID)

	require.NoError(t, err)
	repo.AssertNotCalled(t, "ExpireTrialIfNoOverride", mock.Anything, mock.Anything)
	repo.AssertNotCalled(t, "SetRetentionDates", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	plaidCleanup.AssertNotCalled(t, "DisconnectAllUserItems", mock.Anything, mock.Anything)
}

func TestTrialService_ExpireTrial_ExpiredOverride_Proceeds(t *testing.T) {
	service, repo, plaidCleanup := setupTestTrialService(t)
	ctx := context.Background()
	userID := "user-123"

	trialEndsAt := time.Now().Add(-1 * time.Hour)
	override := "household_2"
	overrideEndsAt := time.Now().Add(-24 * time.Hour) // expired
	user := &domain.User{
		ID:                 userID,
		SubscriptionStatus: "trial",
		TrialEndsAt:        &trialEndsAt,
		ManualTierOverride: &override,
		ManualTierEndsAt:   &overrideEndsAt,
	}
	repo.On("GetUserByID", ctx, userID).Return(user, nil)
	repo.On("ExpireTrialIfNoOverride", ctx, userID).Return(true, nil)
	repo.On("SetRetentionDates", ctx, userID, mock.Anything, mock.Anything).Return(nil)
	plaidCleanup.On("DisconnectAllUserItems", ctx, userID).Return(nil)

	err := service.ExpireTrial(ctx, userID)

	require.NoError(t, err)
	repo.AssertExpectations(t)
	plaidCleanup.AssertExpectations(t)
}

// TestTrialService_ExpireTrial_ConcurrentSubscribe_CASFailsBeforeDisconnect_ClaimReleasedNotCompleted
// is the round-6 RED test for the round-5 regression's Harm #1: a user who completes checkout
// mid-run (the webhook flips subscription_status to 'active' — see subscription_webhook.go:622-629)
// must NOT have every Plaid item removed after they already paid.
//
// Simulated here by ExpireTrialIfNoOverride reporting flipped=false: its WHERE clause requires
// subscription_status = 'trial' at the moment of the write, so a concurrent subscribe (or an admin
// comp landing after the mid-run recheck) makes it fail for exactly this reason. Because the CAS
// now runs BEFORE the Plaid disconnect (round-6's fix), a lost CAS means the disconnect
// collaborator must NEVER be invoked at all — proven with an explicit MockWorkClaimsRepository
// (not the file's permissive default) asserting Release, not Complete, was called: the shared
// permissive mock makes both `.Maybe()`, so an assertion against it alone cannot tell a correct
// Release apart from an incorrectly-called Complete (this was the exact gap identified in a
// subsequent review — the previous version of this test used the permissive default and asserted
// neither).
//
// This REPLACES the round-5 version of this test (formerly
// ...CASLostAfterDisconnect_ReleasesWithoutRetention), which asserted the OPPOSITE — that Plaid
// WAS called even though the CAS lost — because under round-5's ordering the disconnect ran BEFORE
// the CAS could ever see the concurrent write. That assertion encoded the very regression this
// fix reverts.
//
// Falsifiability: on today's (round-5) code, this test fails — plaidCleanup.AssertNotCalled below
// fails because the disconnect unconditionally runs BEFORE the CAS is ever consulted, regardless
// of what ExpireTrialIfNoOverride would have reported.
func TestTrialService_ExpireTrial_ConcurrentSubscribe_CASFailsBeforeDisconnect_ClaimReleasedNotCompleted(t *testing.T) {
	service, repo, plaidCleanup := setupTestTrialService(t)
	ctx := context.Background()
	userID := "user-123"

	trialEndsAt := time.Now().Add(-1 * time.Hour)
	user := &domain.User{
		ID:                 userID,
		SubscriptionStatus: "trial",
		TrialEndsAt:        &trialEndsAt,
	}
	repo.On("GetUserByID", ctx, userID).Return(user, nil)
	// The checkout webhook already flipped subscription_status to 'active' since the top-of-function
	// read — ExpireTrialIfNoOverride's WHERE clause (subscription_status = 'trial') fails to match.
	repo.On("ExpireTrialIfNoOverride", ctx, userID).Return(false, nil)

	mockClaims := new(MockWorkClaimsRepository)
	mockClaims.On("Claim", ctx, repository.ResourceTypeUser, userID, repository.ActionTrialExpiration, trialExpirationClaimLease).
		Return("subscribe-race-token", true, nil)
	mockClaims.On("Release", mock.Anything, repository.ResourceTypeUser, userID, repository.ActionTrialExpiration, "subscribe-race-token",
		"precondition lost — comp granted or subscription status moved on concurrently").
		Return(true, nil)
	service.SetWorkClaimsRepo(mockClaims)

	err := service.ExpireTrial(ctx, userID)

	require.NoError(t, err, "a lost CAS is a normal skip, not a job failure")
	plaidCleanup.AssertNotCalled(t, "DisconnectAllUserItems", mock.Anything, mock.Anything,
		"a paying customer's Plaid items must never be touched — the CAS failing BEFORE the disconnect is what the round-6 fix guarantees")
	repo.AssertNotCalled(t, "SetRetentionDates", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	mockClaims.AssertCalled(t, "Release", mock.Anything, repository.ResourceTypeUser, userID, repository.ActionTrialExpiration, "subscribe-race-token",
		"precondition lost — comp granted or subscription status moved on concurrently")
	mockClaims.AssertNotCalled(t, "Complete", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

// ─── #1413 round-4 finding H2: trial_service gains a work_claims flow claim ────

// TestTrialService_ExpireTrial_NoClaimsRepoConfigured_FailsLoudly proves this is a required
// capability, not an optional one — mirroring DeletionService/PastDueService/
// PlaidItemCleanupService's nil-claims-repo behavior. Constructs the service directly (bypassing
// setupTestTrialService's permissive wiring) so claimsRepo is genuinely nil.
func TestTrialService_ExpireTrial_NoClaimsRepoConfigured_FailsLoudly(t *testing.T) {
	tierConfig, err := config.NewTierConfig("")
	require.NoError(t, err)
	repo := new(mockTrialRepository)
	plaidCleanup := new(mockPlaidCleanupService)
	svc := NewTrialService(tierConfig, repo, plaidCleanup) // SetWorkClaimsRepo never called

	ctx := context.Background()
	userID := "user-no-claims"
	trialEndsAt := time.Now().Add(-1 * time.Hour)
	user := &domain.User{ID: userID, SubscriptionStatus: "trial", TrialEndsAt: &trialEndsAt}
	repo.On("GetUserByID", ctx, userID).Return(user, nil)

	err = svc.ExpireTrial(ctx, userID)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "work claims repository not configured")
	repo.AssertNotCalled(t, "ExpireTrialIfNoOverride", mock.Anything, mock.Anything)
	plaidCleanup.AssertNotCalled(t, "DisconnectAllUserItems", mock.Anything, mock.Anything)
}

// TestTrialService_ExpireTrial_WorkClaimLost_CollaboratorNotCalled is the direct proof the brief
// requires: trial_service now takes a real work_claims flow claim, and losing it (another worker
// holds ActionTrialExpiration, or it was already completed for this user) must skip EVERYTHING
// below — including the CAS and the Plaid collaborator — rather than proceeding regardless.
func TestTrialService_ExpireTrial_WorkClaimLost_CollaboratorNotCalled(t *testing.T) {
	service, repo, plaidCleanup := setupTestTrialService(t)
	ctx := context.Background()
	userID := "user-claim-busy"

	trialEndsAt := time.Now().Add(-1 * time.Hour)
	user := &domain.User{ID: userID, SubscriptionStatus: "trial", TrialEndsAt: &trialEndsAt}
	repo.On("GetUserByID", ctx, userID).Return(user, nil)

	// Override the permissive default: this specific claim attempt loses.
	mockClaims := new(MockWorkClaimsRepository)
	mockClaims.On("Claim", ctx, repository.ResourceTypeUser, userID, repository.ActionTrialExpiration, trialExpirationClaimLease).
		Return("", false, nil)
	service.SetWorkClaimsRepo(mockClaims)

	err := service.ExpireTrial(ctx, userID)

	require.NoError(t, err, "a lost work claim is a normal skip, not a job failure")
	repo.AssertNotCalled(t, "ExpireTrialIfNoOverride", mock.Anything, mock.Anything)
	repo.AssertNotCalled(t, "SetRetentionDates", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	plaidCleanup.AssertNotCalled(t, "DisconnectAllUserItems", mock.Anything, mock.Anything)
	mockClaims.AssertNotCalled(t, "Complete", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

// TestTrialService_ExpireTrial_WorkClaimWon_CompletedAfterSuccess proves the positive direction:
// a won claim runs the whole flow and is marked durably Complete afterward.
func TestTrialService_ExpireTrial_WorkClaimWon_CompletedAfterSuccess(t *testing.T) {
	service, repo, plaidCleanup := setupTestTrialService(t)
	ctx := context.Background()
	userID := "user-claim-won"

	trialEndsAt := time.Now().Add(-1 * time.Hour)
	user := &domain.User{ID: userID, SubscriptionStatus: "trial", TrialEndsAt: &trialEndsAt}
	repo.On("GetUserByID", ctx, userID).Return(user, nil)
	repo.On("ExpireTrialIfNoOverride", ctx, userID).Return(true, nil)
	repo.On("SetRetentionDates", ctx, userID, mock.Anything, mock.Anything).Return(nil)
	plaidCleanup.On("DisconnectAllUserItems", ctx, userID).Return(nil)

	mockClaims := new(MockWorkClaimsRepository)
	mockClaims.On("Claim", ctx, repository.ResourceTypeUser, userID, repository.ActionTrialExpiration, trialExpirationClaimLease).
		Return("won-token", true, nil)
	mockClaims.On("Complete", mock.Anything, repository.ResourceTypeUser, userID, repository.ActionTrialExpiration, "won-token").
		Return(true, nil)
	service.SetWorkClaimsRepo(mockClaims)

	err := service.ExpireTrial(ctx, userID)

	require.NoError(t, err)
	mockClaims.AssertCalled(t, "Complete", mock.Anything, repository.ResourceTypeUser, userID, repository.ActionTrialExpiration, "won-token")
	mockClaims.AssertNotCalled(t, "Release", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

// TestTrialService_ExpireTrial_CompGrantedSinceClaim_SkipsDisconnectAndFlip proves the mid-run
// comp re-check (Issue #1413 round-4 finding H2) survives the round-6 reordering: it now guards
// the CAS flip directly (not the Plaid disconnect, which round-6 moved to run AFTER the CAS) — a
// comp granted in the window between winning the work claim and this recheck must skip BOTH the
// status flip AND the Plaid disconnect, since neither has run yet at this point in the new order.
//
// Wires an explicit MockWorkClaimsRepository (not the file's permissive default, whose Release and
// Complete stubs are both `.Maybe()`) so this test can assert Release — not Complete — was called
// with the specific "comp granted since claim" reason. A prior version of this test used the
// permissive default and asserted neither the Release call nor Complete's absence: the shared
// mock's `.Maybe()` stubs mean swapping Release for Complete in the implementation would still
// have passed silently, leaving a comped user's trial-expiration claim wrongly marked Complete —
// as work_claims never re-grants a completed claim, that would permanently strand the user's trial
// row (Complete forecloses any FUTURE ExpireTrial run from ever processing them again once the
// comp eventually lapses).
//
// Falsifiability: if the mid-run recheck were removed (or downgraded to skip only the disconnect),
// plaidCleanup.AssertNotCalled and/or repo.AssertNotCalled("ExpireTrialIfNoOverride") below would
// fail. If Complete were called instead of Release, mockClaims.AssertNotCalled("Complete") below
// would fail.
func TestTrialService_ExpireTrial_CompGrantedSinceClaim_SkipsDisconnectAndFlip(t *testing.T) {
	service, repo, plaidCleanup := setupTestTrialService(t)
	ctx := context.Background()
	userID := "user-comped-since-claim"

	trialEndsAt := time.Now().Add(-1 * time.Hour)
	uncompedUser := &domain.User{ID: userID, SubscriptionStatus: "trial", TrialEndsAt: &trialEndsAt}
	override := "household_2"
	overrideEndsAt := time.Now().Add(24 * time.Hour) // now active
	compedUser := &domain.User{
		ID:                 userID,
		SubscriptionStatus: "trial", // unchanged — no CAS write has happened yet
		TrialEndsAt:        &trialEndsAt,
		ManualTierOverride: &override,
		ManualTierEndsAt:   &overrideEndsAt,
	}

	// First read (top of ExpireTrial): no override. Second read (the mid-run recheck, now
	// immediately after the work claim and BEFORE the CAS flip): a comp landed since.
	repo.On("GetUserByID", ctx, userID).Return(uncompedUser, nil).Once()
	repo.On("GetUserByID", ctx, userID).Return(compedUser, nil).Once()

	mockClaims := new(MockWorkClaimsRepository)
	mockClaims.On("Claim", ctx, repository.ResourceTypeUser, userID, repository.ActionTrialExpiration, trialExpirationClaimLease).
		Return("comped-since-claim-token", true, nil)
	mockClaims.On("Release", mock.Anything, repository.ResourceTypeUser, userID, repository.ActionTrialExpiration, "comped-since-claim-token", "comp granted since claim").
		Return(true, nil)
	service.SetWorkClaimsRepo(mockClaims)

	err := service.ExpireTrial(ctx, userID)

	require.NoError(t, err)
	plaidCleanup.AssertNotCalled(t, "DisconnectAllUserItems", mock.Anything, mock.Anything)
	repo.AssertNotCalled(t, "ExpireTrialIfNoOverride", mock.Anything, mock.Anything)
	repo.AssertNotCalled(t, "SetRetentionDates", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	mockClaims.AssertCalled(t, "Release", mock.Anything, repository.ResourceTypeUser, userID, repository.ActionTrialExpiration, "comped-since-claim-token", "comp granted since claim")
	mockClaims.AssertNotCalled(t, "Complete", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestTrialService_CanGrantTrial_True(t *testing.T) {
	service, _, _ := setupTestTrialService(t)

	user := &domain.User{
		ID:       "user-123",
		HadTrial: false,
	}

	canGrant := service.CanGrantTrial(user)

	assert.True(t, canGrant)
}

func TestTrialService_CanGrantTrial_False(t *testing.T) {
	service, _, _ := setupTestTrialService(t)

	user := &domain.User{
		ID:       "user-123",
		HadTrial: true,
	}

	canGrant := service.CanGrantTrial(user)

	assert.False(t, canGrant)
}

// Issue #1015: TrialService must be constructable with nil plaidCleanup and wirable via setter.
// This enables River initialization to be decoupled from Plaid configuration.

func TestTrialService_SetPlaidCleanup(t *testing.T) {
	tierConfig, err := config.NewTierConfig("")
	require.NoError(t, err)
	repo := new(mockTrialRepository)

	// Create with nil plaidCleanup — valid when Plaid not configured
	svc := NewTrialService(tierConfig, repo, nil)

	// Wire cleanup after Plaid initializes via setter
	cleanup := new(mockPlaidCleanupService)
	svc.SetPlaidCleanup(cleanup)
	assert.Equal(t, cleanup, svc.plaidCleanup, "plaidCleanup should be set after SetPlaidCleanup")
}

func TestTrialService_ExpireTrial_NilPlaidCleanup(t *testing.T) {
	tierConfig, err := config.NewTierConfig("")
	require.NoError(t, err)
	repo := new(mockTrialRepository)

	// Create with nil plaidCleanup (no Plaid configured)
	svc := NewTrialService(tierConfig, repo, nil)
	svc.SetWorkClaimsRepo(newAllowingTrialClaimsRepo())

	ctx := context.Background()
	userID := "user-456"
	trialEndsAt := time.Now().Add(-1 * time.Hour)
	user := &domain.User{
		ID:                 userID,
		SubscriptionStatus: "trial",
		TrialEndsAt:        &trialEndsAt,
	}

	repo.On("GetUserByID", ctx, userID).Return(user, nil)
	repo.On("ExpireTrialIfNoOverride", ctx, userID).Return(true, nil)
	repo.On("SetRetentionDates", ctx, userID, mock.Anything, mock.Anything).Return(nil)

	// Should not panic and should succeed — plaidCleanup nil-guard in ExpireTrial
	err = svc.ExpireTrial(ctx, userID)

	require.NoError(t, err)
	repo.AssertExpectations(t)
	// No DisconnectAllUserItems call expected since plaidCleanup is nil
}

// ─── #1413 round-6 (two independent reviewers): revert round-5's Plaid-disconnect reorder ────
//
// Round-5's High finding was genuine — ExpireTrial used to flip subscription_status to 'inactive'
// BEFORE disconnecting Plaid, and swallowed a disconnect failure with a log-and-continue, so a
// failed disconnect could never be retried via GetUsersWithExpiredTrials (which selects on raw
// status = 'trial'). But round-5's FIX — disconnect FIRST, flip SECOND — was itself a regression:
// its mid-run re-check only ever verified HasActiveManualOverride(), never SubscriptionStatus, and
// it ran BEFORE the CAS, leaving 'trial' (a WRITABLE status, unlike past_due) writable for the
// disconnect's entire duration. A user who completed checkout mid-run had every Plaid item removed
// after already paying; a user who linked a new item mid-run could have it orphaned.
//
// Round-6 restores CAS-FIRST ordering (claim -> comp re-check -> CAS flip -> disconnect ->
// retention -> complete) — eliminating the writable window instead of narrowing it — and replaces
// round-5's retry mechanism (a released work claim) with a dedicated repair path:
// GetUsersNeedingPlaidDisconnectRepair (profile_repository.go), consumed by
// RetentionCleanupCheckWorker's periodic pass (retention_cleanup_check.go), mirroring
// GetUsersNeedingRetentionClockRepair's existing shape for the retention clock.

// TestTrialService_ExpireTrial_DisconnectFailsAfterFlip_ClaimCompleted_ErrorPropagated is the
// round-6 replacement for the round-5 test formerly named
// ...DisconnectFails_StatusNotFlipped_ClaimReleasedForRetry, whose premise (a disconnect failure
// prevents the CAS from ever running) encoded the regression this reverts. Under the restored
// ordering, the CAS has ALREADY committed by the time the disconnect can fail — this proves the
// new contract: the flip and retention-clock start both still happen, the flow claim is marked
// Complete (not Released — the state transition succeeded; retrying THIS claim would accomplish
// nothing further), and the error is still propagated so the caller's error/retry accounting
// reflects the problem, even though the ACTUAL retry now happens via the Plaid-disconnect repair
// pass rather than a released ActionTrialExpiration claim.
//
// Falsifiability: on the round-5 ordering, this test fails — ExpireTrialIfNoOverride would never
// be called (repo.AssertCalled fails) because the disconnect runs first and its failure aborts the
// flow before the CAS is ever reached.
func TestTrialService_ExpireTrial_DisconnectFailsAfterFlip_ClaimCompleted_ErrorPropagated(t *testing.T) {
	service, repo, plaidCleanup := setupTestTrialService(t)
	ctx := context.Background()
	userID := "user-disconnect-fails"

	trialEndsAt := time.Now().Add(-1 * time.Hour)
	user := &domain.User{ID: userID, SubscriptionStatus: "trial", TrialEndsAt: &trialEndsAt}
	repo.On("GetUserByID", ctx, userID).Return(user, nil)
	repo.On("ExpireTrialIfNoOverride", ctx, userID).Return(true, nil)
	repo.On("SetRetentionDates", ctx, userID, mock.Anything, mock.Anything).Return(nil)

	disconnectErr := errors.New("plaid: transient 500")
	plaidCleanup.On("DisconnectAllUserItems", ctx, userID).Return(disconnectErr)

	mockClaims := new(MockWorkClaimsRepository)
	mockClaims.On("Claim", ctx, repository.ResourceTypeUser, userID, repository.ActionTrialExpiration, trialExpirationClaimLease).
		Return("disconnect-fail-token", true, nil)
	mockClaims.On("Complete", mock.Anything, repository.ResourceTypeUser, userID, repository.ActionTrialExpiration, "disconnect-fail-token").
		Return(true, nil)
	service.SetWorkClaimsRepo(mockClaims)

	err := service.ExpireTrial(ctx, userID)

	require.Error(t, err, "a disconnect failure must still be visible to the caller's error accounting, not silently swallowed")
	assert.ErrorIs(t, err, disconnectErr)
	repo.AssertCalled(t, "ExpireTrialIfNoOverride", ctx, userID)
	repo.AssertCalled(t, "SetRetentionDates", ctx, userID, mock.Anything, mock.Anything)
	mockClaims.AssertCalled(t, "Complete", mock.Anything, repository.ResourceTypeUser, userID, repository.ActionTrialExpiration, "disconnect-fail-token")
	mockClaims.AssertNotCalled(t, "Release", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

// TestTrialService_ExpireTrial_DisconnectSucceeds_StatusFlipsAsBefore is the positive control:
// an ordinary expired trial with a successful Plaid disconnect must be fully processed regardless
// of internal ordering — flipped to inactive, retention started, claim completed. An over-tight
// guard reacting to the round-6 reorder by refusing to flip status, or refusing to disconnect,
// would fail this test (repo/plaidCleanup.AssertExpectations below require every collaborator to
// have actually been called).
func TestTrialService_ExpireTrial_DisconnectSucceeds_StatusFlipsAsBefore(t *testing.T) {
	service, repo, plaidCleanup := setupTestTrialService(t)
	ctx := context.Background()
	userID := "user-disconnect-succeeds"

	trialEndsAt := time.Now().Add(-1 * time.Hour)
	user := &domain.User{ID: userID, SubscriptionStatus: "trial", TrialEndsAt: &trialEndsAt}
	repo.On("GetUserByID", ctx, userID).Return(user, nil)
	plaidCleanup.On("DisconnectAllUserItems", ctx, userID).Return(nil)
	repo.On("ExpireTrialIfNoOverride", ctx, userID).Return(true, nil)
	repo.On("SetRetentionDates", ctx, userID, mock.Anything, mock.Anything).Return(nil)

	err := service.ExpireTrial(ctx, userID)

	require.NoError(t, err)
	repo.AssertExpectations(t)
	plaidCleanup.AssertExpectations(t)
}

// TestTrialService_ExpireTrial_StatusFlipRunsBeforeDisconnect pins the ORDERING itself, not just
// the outcome: ExpireTrialIfNoOverride must be observed to run strictly before
// DisconnectAllUserItems. Records call order via .Run callbacks (mirroring
// TestPlaidCleanup_C2_ItemListReadAfterClaimNotBefore's approach in
// plaid_item_cleanup_service_test.go) so a regression that silently restores round-5's ordering is
// caught even if some other assertion would otherwise still pass.
//
// This is also the structural proof for round-5 regression Harm #2 (an item linked during the
// disconnect window is orphaned): once the CAS flip commits BEFORE the disconnect call below, the
// user is already read-only for the disconnect's entire duration — the read-only middleware
// (internal/huma/middleware/read_only.go) rejects any further account-linking request for a
// read-only-status user, so no new item can be created in the window this test proves does not
// exist between the flip and the disconnect. (This unit test cannot itself drive the HTTP layer;
// it proves the ordering precondition the read-only middleware's existing, separately-tested
// behavior depends on.)
//
// Falsifiability: on today's (round-5) code, this test fails — the actual recorded order is
// ["disconnect", "cas"], not ["cas", "disconnect"].
func TestTrialService_ExpireTrial_StatusFlipRunsBeforeDisconnect(t *testing.T) {
	service, repo, plaidCleanup := setupTestTrialService(t)
	ctx := context.Background()
	userID := "user-order-pin"

	trialEndsAt := time.Now().Add(-1 * time.Hour)
	user := &domain.User{ID: userID, SubscriptionStatus: "trial", TrialEndsAt: &trialEndsAt}
	repo.On("GetUserByID", ctx, userID).Return(user, nil)

	var order []string
	repo.On("ExpireTrialIfNoOverride", ctx, userID).Run(func(mock.Arguments) {
		order = append(order, "cas")
	}).Return(true, nil)
	plaidCleanup.On("DisconnectAllUserItems", ctx, userID).Run(func(mock.Arguments) {
		order = append(order, "disconnect")
	}).Return(nil)
	repo.On("SetRetentionDates", ctx, userID, mock.Anything, mock.Anything).Return(nil)

	err := service.ExpireTrial(ctx, userID)

	require.NoError(t, err)
	require.Equal(t, []string{"cas", "disconnect"}, order,
		"the status-flip CAS must run strictly before the Plaid disconnect — the entire point of the round-6 fix")
}

// TestTrialService_ExpireTrial_DisconnectFailsAfterFlip_RepairPassRetriesOnlyOutstandingItems
// proves the round-6 reordering and Issue #1413 H2's per-item durable cleanup state compose
// correctly end-to-end — not merely assumed from
// TestPlaidCleanup_RetryAfterPartialFailure_OnlyOutstandingItemsAttempted (the lower-level proof
// in plaid_item_cleanup_service_test.go that this test builds on). Wires a REAL
// *plaidItemCleanupService (not a bare test double) as TrialService's Plaid collaborator, backed
// by mocked linked-item-repo/Plaid-client collaborators:
//
//  1. ExpireTrial's own pass: the CAS succeeds (round-6 — the flip no longer depends on the
//     disconnect outcome) while item-1 fails and item-2 succeeds. cleanupItems aggregates this
//     into a non-nil error, which ExpireTrial still propagates for visibility even though the
//     flip and retention clock both already committed.
//  2. The retry: unlike the round-5 test this replaces, the retry is NOT a second ExpireTrial
//     call — GetUsersWithExpiredTrials can no longer re-select this user, since their
//     subscription_status is already 'inactive'. Instead, this simulates
//     RetentionCleanupCheckWorker's Plaid-disconnect repair pass, which calls
//     DisconnectAllUserItems directly for a GetUsersNeedingPlaidDisconnectRepair candidate. item-2
//     is now 'disconnected' in the repo's returned state (standing in for MarkItemsDisconnected's
//     real persistence), so cleanupItems must NOT send it to Plaid again — only item-1 (still
//     outstanding) is attempted, and succeeds.
func TestTrialService_ExpireTrial_DisconnectFailsAfterFlip_RepairPassRetriesOnlyOutstandingItems(t *testing.T) {
	tierConfig, err := config.NewTierConfig("")
	require.NoError(t, err)

	ctx := context.Background()
	userID := "user-partial-retry"
	trialEndsAt := time.Now().Add(-1 * time.Hour)
	user := &domain.User{ID: userID, SubscriptionStatus: "trial", TrialEndsAt: &trialEndsAt}

	repo := new(mockTrialRepository)
	repo.On("GetUserByID", ctx, userID).Return(user, nil)
	repo.On("ExpireTrialIfNoOverride", ctx, userID).Return(true, nil)
	repo.On("SetRetentionDates", ctx, userID, mock.Anything, mock.Anything).Return(nil)

	mockLinkedItems := new(MockLinkedItemRepoForCleanup)
	mockPlaid := new(MockPlaidServiceForCleanup)
	plaidCleanup := NewPlaidItemCleanupService(mockLinkedItems, mockPlaid)
	plaidCleanup.SetWorkClaimsRepo(newAllowingPlaidCleanupClaimsRepo())

	svc := NewTrialService(tierConfig, repo, plaidCleanup)
	svc.SetWorkClaimsRepo(newAllowingTrialClaimsRepo())

	// ExpireTrial's own pass: item-1 fails, item-2 succeeds.
	firstPassItems := []domain.LinkedItem{
		{ID: "item-1", UserID: userID, AccessToken: "tok-1"},
		{ID: "item-2", UserID: userID, AccessToken: "tok-2"},
	}
	mockLinkedItems.On("GetActiveItemsByUserID", ctx, userID).Return(firstPassItems, nil).Once()
	plaidErr := errors.New("transient plaid error")
	mockPlaid.On("RemoveItem", ctx, "tok-1").Return(plaidErr).Once()
	mockPlaid.On("RemoveItem", ctx, "tok-2").Return(nil).Once()
	mockLinkedItems.On("MarkItemsDisconnected", ctx, []string{"item-2"}).Return(nil).Once()

	err = svc.ExpireTrial(ctx, userID)
	require.Error(t, err, "a genuine per-item Plaid failure must still surface for operational visibility")
	repo.AssertCalled(t, "ExpireTrialIfNoOverride", ctx, userID)
	repo.AssertCalled(t, "SetRetentionDates", ctx, userID, mock.Anything, mock.Anything)

	// The repair pass (retention_cleanup_check.go's repairPlaidDisconnects, driven by
	// GetUsersNeedingPlaidDisconnectRepair) calls DisconnectAllUserItems directly — item-2 is now
	// durably 'disconnected'; only item-1 is outstanding.
	secondPassItems := []domain.LinkedItem{
		{ID: "item-1", UserID: userID, AccessToken: "tok-1"},
		{ID: "item-2", UserID: userID, AccessToken: "tok-2", Status: domain.LinkedItemStatusDisconnected},
	}
	mockLinkedItems.On("GetActiveItemsByUserID", ctx, userID).Return(secondPassItems, nil).Once()
	mockPlaid.On("RemoveItem", ctx, "tok-1").Return(nil).Once()
	mockLinkedItems.On("MarkItemsDisconnected", ctx, []string{"item-1"}).Return(nil).Once()

	repairErr := plaidCleanup.DisconnectAllUserItems(ctx, userID)
	require.NoError(t, repairErr, "the repair pass must succeed once only the outstanding item remains")

	// Across BOTH passes tok-1 was attempted twice (fail, then succeed) but tok-2 only once — the
	// repair retry must not call Plaid again for the item already marked disconnected.
	mockPlaid.AssertNumberOfCalls(t, "RemoveItem", 3)
	repo.AssertExpectations(t)
	mockLinkedItems.AssertExpectations(t)
}
