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

// TestTrialService_ExpireTrial_ClaimLost_SkipsWithoutTouchingRetentionOrPlaid proves Issue #1413
// High finding H5 (Codex review round 3) is actually closed: the HasActiveManualOverride() guard
// at the top of ExpireTrial is a plain read and is NOT atomic with the status-flip write below
// it — an admin can grant/extend a comp in that exact window. Here the initial read shows no
// override (the guard passes), but ExpireTrialIfNoOverride reports claimed=false, simulating a
// comp landing between the guard and the atomic write. Retention and Plaid must NOT run.
//
// NOTE on naming: "Claim" here is the CAS (ExpireTrialIfNoOverride) losing, predating the
// round-4 H2 work_claims.Claim added below — see the "─── #1413 round-4 finding H2" section for
// the tests proving the NEW work-claims-layer Claim's own lost/won behavior.
func TestTrialService_ExpireTrial_ClaimLost_SkipsWithoutTouchingRetentionOrPlaid(t *testing.T) {
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
	repo.On("ExpireTrialIfNoOverride", ctx, userID).Return(false, nil)

	err := service.ExpireTrial(ctx, userID)

	require.NoError(t, err, "a lost claim is a normal skip, not a job failure")
	repo.AssertNotCalled(t, "SetRetentionDates", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	plaidCleanup.AssertNotCalled(t, "DisconnectAllUserItems", mock.Anything, mock.Anything)
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

// TestTrialService_ExpireTrial_CompGrantedAfterCAS_SkipsPlaidButKeepsStatusFlip documents the
// H2 residual behavior this task's brief explicitly asks for (not eliminate — narrow and
// document): a comp granted AFTER ExpireTrialIfNoOverride's CAS succeeds but BEFORE the
// late re-check immediately preceding the Plaid disconnect call must skip ONLY the Plaid
// disconnect. The status flip and retention-clock write already committed via the CAS are NOT
// undone — ComputeEffectiveStatus already treats a comped user as effectively active regardless
// of the raw subscription_status column, and the retention clock this run started is cleared the
// moment the comp is applied (ClearRetentionDates) — so the accepted outcome is "Plaid stays
// connected a little longer than a comp-free run would have", not any form of data loss.
func TestTrialService_ExpireTrial_CompGrantedAfterCAS_SkipsPlaidButKeepsStatusFlip(t *testing.T) {
	service, repo, plaidCleanup := setupTestTrialService(t)
	ctx := context.Background()
	userID := "user-comped-after-cas"

	trialEndsAt := time.Now().Add(-1 * time.Hour)
	uncompedUser := &domain.User{ID: userID, SubscriptionStatus: "trial", TrialEndsAt: &trialEndsAt}
	override := "household_2"
	overrideEndsAt := time.Now().Add(24 * time.Hour) // now active
	compedUser := &domain.User{
		ID:                 userID,
		SubscriptionStatus: "inactive", // already flipped by the CAS below
		TrialEndsAt:        &trialEndsAt,
		ManualTierOverride: &override,
		ManualTierEndsAt:   &overrideEndsAt,
	}

	// First read (top of ExpireTrial): no override. Second read (the late H2 re-check,
	// immediately before the Plaid disconnect call): a comp landed in between.
	repo.On("GetUserByID", ctx, userID).Return(uncompedUser, nil).Once()
	repo.On("ExpireTrialIfNoOverride", ctx, userID).Return(true, nil)
	repo.On("SetRetentionDates", ctx, userID, mock.Anything, mock.Anything).Return(nil)
	repo.On("GetUserByID", ctx, userID).Return(compedUser, nil).Once()

	err := service.ExpireTrial(ctx, userID)

	require.NoError(t, err)
	// The CAS and retention write already ran — they are NOT rolled back.
	repo.AssertCalled(t, "ExpireTrialIfNoOverride", ctx, userID)
	repo.AssertCalled(t, "SetRetentionDates", ctx, userID, mock.Anything, mock.Anything)
	// Only the Plaid disconnect is skipped — the documented, accepted residual.
	plaidCleanup.AssertNotCalled(t, "DisconnectAllUserItems", mock.Anything, mock.Anything)
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
