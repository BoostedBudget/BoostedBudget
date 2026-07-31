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
func (m *mockTrialRepository) SetRetentionDates(ctx context.Context, userID string, retentionMonths int) error {
	return m.Called(ctx, userID, retentionMonths).Error(0)
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

// setupTestTrialService constructs a TrialService for tests. Issue #1413 round-7: TrialService no
// longer has a Plaid dependency at all (ExpireTrial never calls Plaid — see that method's doc
// comment), so this helper only wires a TrialRepository and a permissive work-claims repo.
func setupTestTrialService(t *testing.T) (*TrialService, *mockTrialRepository) {
	t.Helper()

	// Load tier config for trial duration
	tierConfig, err := config.NewTierConfig("")
	require.NoError(t, err)

	repo := new(mockTrialRepository)

	service := NewTrialService(tierConfig, repo)
	// Issue #1413 round-4 finding H2: ExpireTrial now requires a work claims repo. Wired
	// permissively by default so every pre-existing test in this file exercising ExpireTrial's
	// OTHER behavior doesn't have to plumb claim expectations through — tests that DO exercise
	// the claim mechanism itself wire their own MockWorkClaimsRepository instead.
	service.SetWorkClaimsRepo(newAllowingTrialClaimsRepo())

	return service, repo
}

func TestTrialService_GrantTrial_Success(t *testing.T) {
	service, repo := setupTestTrialService(t)
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
	service, repo := setupTestTrialService(t)
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
	service, _ := setupTestTrialService(t)

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
	service, _ := setupTestTrialService(t)

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
	service, _ := setupTestTrialService(t)

	// User with active subscription (not trial)
	user := &domain.User{
		ID:                 "user-123",
		SubscriptionStatus: "active",
	}

	isActive := service.IsTrialActive(user)

	assert.False(t, isActive)
}

func TestTrialService_GetTrialDaysRemaining(t *testing.T) {
	service, _ := setupTestTrialService(t)

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
	service, _ := setupTestTrialService(t)

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

// TestTrialService_ExpireTrial_NeverCallsPlaid is the round-7 RED test (Issue #1413 round-7,
// Codex review): ExpireTrial must make NO Plaid call at all — the disconnect is deferred entirely
// to the grace-window repair pass (RetentionCleanupCheckWorker.repairPlaidDisconnects), because no
// ordering of the status flip and an irreversible Plaid call is safe: /subscription/checkout is
// deliberately whitelisted for read-only users (read_only.go), so flipping a user to read-only
// does NOT stop them becoming active again — a user can pay mid-cleanup and have their banks
// deleted afterward regardless of whether the disconnect runs before or after the flip.
//
// This REPLACES the round-6 versions of this test (formerly ...ExpireTrial_Success and
// ...ExpireTrial_DisconnectsPlaidItems), which wired a Plaid mock and asserted it WAS called —
// encoding exactly the contract round-7 removes. Post-refactor, "never calls Plaid" is enforced
// structurally: NewTrialService's signature no longer accepts a Plaid collaborator at all, so
// there is nothing left to stub or assert against — this test proves the ordinary happy path
// (status flip + retention clock) still succeeds using ONLY a TrialRepository.
//
// Falsifiability: on round-6 code (TrialService still took a PlaidCleanupForTrial and called
// DisconnectAllUserItems unconditionally after a successful CAS), a version of this test wired
// with a Plaid mock and asserting AssertNotCalled failed — see this task's report for the RED run.
func TestTrialService_ExpireTrial_NeverCallsPlaid(t *testing.T) {
	service, repo := setupTestTrialService(t)
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
	repo.On("SetRetentionDates", ctx, userID, mock.Anything).Return(nil)

	err := service.ExpireTrial(ctx, userID)

	require.NoError(t, err)
	repo.AssertExpectations(t)
}

// ─── #1367 — Phase 3: covered household members are exempt from trial expiry ──

type mockHouseholdCheckerForTrial struct{ mock.Mock }

func (m *mockHouseholdCheckerForTrial) IsCoveredByHousehold(ctx context.Context, userID string) (bool, error) {
	args := m.Called(ctx, userID)
	return args.Bool(0), args.Error(1)
}

func TestTrialService_ExpireTrial_CoveredByHousehold_Skipped(t *testing.T) {
	service, repo := setupTestTrialService(t)
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
}

func TestTrialService_ExpireTrial_NotCoveredByHousehold_Proceeds(t *testing.T) {
	service, repo := setupTestTrialService(t)
	checker := &mockHouseholdCheckerForTrial{}
	service.SetHouseholdChecker(checker)
	ctx := context.Background()
	userID := "user-123"

	trialEndsAt := time.Now().Add(-1 * time.Hour)
	user := &domain.User{ID: userID, SubscriptionStatus: "trial", TrialEndsAt: &trialEndsAt}
	repo.On("GetUserByID", ctx, userID).Return(user, nil)
	checker.On("IsCoveredByHousehold", ctx, userID).Return(false, nil)
	repo.On("ExpireTrialIfNoOverride", ctx, userID).Return(true, nil)
	repo.On("SetRetentionDates", ctx, userID, mock.Anything).Return(nil)

	err := service.ExpireTrial(ctx, userID)

	require.NoError(t, err)
	repo.AssertExpectations(t)
}

func TestTrialService_ExpireTrial_NoHouseholdCheckerConfigured_Proceeds(t *testing.T) {
	// Backward compatibility: no checker wired (nil) must behave exactly as before.
	service, repo := setupTestTrialService(t)
	ctx := context.Background()
	userID := "user-123"

	trialEndsAt := time.Now().Add(-1 * time.Hour)
	user := &domain.User{ID: userID, SubscriptionStatus: "trial", TrialEndsAt: &trialEndsAt}
	repo.On("GetUserByID", ctx, userID).Return(user, nil)
	repo.On("ExpireTrialIfNoOverride", ctx, userID).Return(true, nil)
	repo.On("SetRetentionDates", ctx, userID, mock.Anything).Return(nil)

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
	service, repo := setupTestTrialService(t)
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
}

// ─── #1413: comped users are exempt from trial-expiry destructive actions ──

// TestTrialService_ExpireTrial_ActiveOverride_Skipped proves the TOP-OF-FUNCTION comp guard
// (trial_service.go:237) alone stops ExpireTrial from proceeding. The GetUserByID stub carries
// .Once() so it can only satisfy the :237 read — if that guard were deleted, execution would fall
// through to the household check (skipped, no checker configured), win the permissive work claim,
// and then re-read the user a SECOND time at trial_service.go:295 for the mid-run re-check
// (:300). With no second stubbed response available, that second call panics on an unexpected
// mock invocation, failing this test for the right reason. (An earlier version of this test
// stubbed GetUserByID without .Once(), so deleting :237 silently re-read the SAME comped user,
// tripped :300 instead, and left every assertion below — including both AssertNotCalled calls —
// still passing: the test survived deleting the very gate it was named for.)
func TestTrialService_ExpireTrial_ActiveOverride_Skipped(t *testing.T) {
	service, repo := setupTestTrialService(t)
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
	repo.On("GetUserByID", ctx, userID).Return(user, nil).Once()

	err := service.ExpireTrial(ctx, userID)

	require.NoError(t, err)
	repo.AssertNumberOfCalls(t, "GetUserByID", 1)
	repo.AssertNotCalled(t, "ExpireTrialIfNoOverride", mock.Anything, mock.Anything)
	repo.AssertNotCalled(t, "SetRetentionDates", mock.Anything, mock.Anything, mock.Anything)
}

func TestTrialService_ExpireTrial_ExpiredOverride_Proceeds(t *testing.T) {
	service, repo := setupTestTrialService(t)
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
	repo.On("SetRetentionDates", ctx, userID, mock.Anything).Return(nil)

	err := service.ExpireTrial(ctx, userID)

	require.NoError(t, err)
	repo.AssertExpectations(t)
}

// TestTrialService_ExpireTrial_ConcurrentSubscribe_CASFails_ClaimReleasedNotCompleted proves a
// user who completes checkout concurrently (the webhook flips subscription_status to 'active'
// between the top-of-function read and the CAS) causes ExpireTrialIfNoOverride's WHERE clause
// (subscription_status = 'trial') to fail to match — a legitimate, non-error skip that releases
// (not completes) the work claim. Issue #1413 round-7: since ExpireTrial no longer calls Plaid at
// all, there is nothing further to protect here beyond the claim disposition itself — this test
// REPLACES the round-6 version (formerly ...CASFailsBeforeDisconnect_ClaimReleasedNotCompleted),
// which also asserted the Plaid collaborator was never called; that assertion is now structurally
// impossible to violate (TrialService has no Plaid collaborator at all) and so was dropped rather
// than kept as dead weight.
func TestTrialService_ExpireTrial_ConcurrentSubscribe_CASFails_ClaimReleasedNotCompleted(t *testing.T) {
	service, repo := setupTestTrialService(t)
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
	repo.AssertNotCalled(t, "SetRetentionDates", mock.Anything, mock.Anything, mock.Anything)
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
	svc := NewTrialService(tierConfig, repo) // SetWorkClaimsRepo never called

	ctx := context.Background()
	userID := "user-no-claims"
	trialEndsAt := time.Now().Add(-1 * time.Hour)
	user := &domain.User{ID: userID, SubscriptionStatus: "trial", TrialEndsAt: &trialEndsAt}
	repo.On("GetUserByID", ctx, userID).Return(user, nil)

	err = svc.ExpireTrial(ctx, userID)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "work claims repository not configured")
	repo.AssertNotCalled(t, "ExpireTrialIfNoOverride", mock.Anything, mock.Anything)
}

// TestTrialService_ExpireTrial_WorkClaimLost_CollaboratorNotCalled is the direct proof the brief
// requires: trial_service now takes a real work_claims flow claim, and losing it (another worker
// holds ActionTrialExpiration, or it was already completed for this user) must skip EVERYTHING
// below — including the CAS — rather than proceeding regardless.
func TestTrialService_ExpireTrial_WorkClaimLost_CollaboratorNotCalled(t *testing.T) {
	service, repo := setupTestTrialService(t)
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
	repo.AssertNotCalled(t, "SetRetentionDates", mock.Anything, mock.Anything, mock.Anything)
	mockClaims.AssertNotCalled(t, "Complete", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

// TestTrialService_ExpireTrial_WorkClaimWon_CompletedAfterSuccess proves the positive direction:
// a won claim runs the whole flow and is marked durably Complete afterward.
func TestTrialService_ExpireTrial_WorkClaimWon_CompletedAfterSuccess(t *testing.T) {
	service, repo := setupTestTrialService(t)
	ctx := context.Background()
	userID := "user-claim-won"

	trialEndsAt := time.Now().Add(-1 * time.Hour)
	user := &domain.User{ID: userID, SubscriptionStatus: "trial", TrialEndsAt: &trialEndsAt}
	repo.On("GetUserByID", ctx, userID).Return(user, nil)
	repo.On("ExpireTrialIfNoOverride", ctx, userID).Return(true, nil)
	repo.On("SetRetentionDates", ctx, userID, mock.Anything).Return(nil)

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

// TestTrialService_ExpireTrial_CompGrantedSinceClaim_SkipsFlip proves the mid-run comp re-check
// (Issue #1413 round-4 finding H2) survives round-7's removal of the Plaid disconnect: a comp
// granted in the window between winning the work claim and this recheck must skip the status
// flip. This REPLACES the round-6 version (formerly ...SkipsDisconnectAndFlip), which also
// asserted the Plaid collaborator was never called — dropped for the same structural reason as
// the concurrent-subscribe test above (TrialService has no Plaid collaborator to call at all).
//
// Wires an explicit MockWorkClaimsRepository (not the file's permissive default, whose Release and
// Complete stubs are both `.Maybe()`) so this test can assert Release — not Complete — was called
// with the specific "comp granted since claim" reason.
func TestTrialService_ExpireTrial_CompGrantedSinceClaim_SkipsFlip(t *testing.T) {
	service, repo := setupTestTrialService(t)
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
	repo.AssertNotCalled(t, "ExpireTrialIfNoOverride", mock.Anything, mock.Anything)
	repo.AssertNotCalled(t, "SetRetentionDates", mock.Anything, mock.Anything, mock.Anything)
	mockClaims.AssertCalled(t, "Release", mock.Anything, repository.ResourceTypeUser, userID, repository.ActionTrialExpiration, "comped-since-claim-token", "comp granted since claim")
	mockClaims.AssertNotCalled(t, "Complete", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestTrialService_CanGrantTrial_True(t *testing.T) {
	service, _ := setupTestTrialService(t)

	user := &domain.User{
		ID:       "user-123",
		HadTrial: false,
	}

	canGrant := service.CanGrantTrial(user)

	assert.True(t, canGrant)
}

func TestTrialService_CanGrantTrial_False(t *testing.T) {
	service, _ := setupTestTrialService(t)

	user := &domain.User{
		ID:       "user-123",
		HadTrial: true,
	}

	canGrant := service.CanGrantTrial(user)

	assert.False(t, canGrant)
}

// ─── Issue #1413 round-7: Plaid disconnect coverage moved out of trial_service_test.go ────
//
// The round-6 test suite here (TestTrialService_ExpireTrial_DisconnectFailsAfterFlip_*,
// ...DisconnectSucceeds_StatusFlipsAsBefore, ...StatusFlipRunsBeforeDisconnect,
// ...DisconnectFailsAfterFlip_RepairPassRetriesOnlyOutstandingItems) asserted ExpireTrial itself
// calls Plaid and pinned its internal CAS-then-disconnect ordering. That contract is exactly what
// round-7 removes — ExpireTrial no longer calls Plaid at all (see its doc comment for why no
// ordering of a status flip and an irreversible Plaid call can be safe). Per the round-7 brief,
// this coverage was MOVED, not dropped:
//   - Per-item eligibility recheck + partial-retry semantics now live in
//     plaid_item_cleanup_service_test.go (TestPlaidCleanup_DisconnectIfEligible_*,
//     TestPlaidCleanup_RetryAfterPartialFailure_OnlyOutstandingItemsAttempted).
//   - The repair pass's disconnect-on-schedule behavior (including the grace window and the
//     positive control — an ordinary expired trial eventually gets disconnected) now lives in
//     retention_cleanup_check_test.go and profile_repository_plaid_disconnect_repair_integration_test.go.
