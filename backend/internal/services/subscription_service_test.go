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

// TestNewSubscriptionService tests service initialization
func TestNewSubscriptionService(t *testing.T) {
	// Load tier config
	tierConfig, err := config.LoadTierConfig()
	require.NoError(t, err, "Failed to load tier config")

	// Create service
	service := NewSubscriptionService(tierConfig, nil)
	assert.NotNil(t, service, "Service should not be nil")
}

// TestGetTierLimits_Premium tests premium tier limits
func TestGetTierLimits_Premium(t *testing.T) {
	tierConfig, err := config.LoadTierConfig()
	require.NoError(t, err)

	service := NewSubscriptionService(tierConfig, nil)
	ctx := context.Background()

	tier, err := service.GetTier(ctx, domain.TierPremium)
	require.NoError(t, err)

	assert.Equal(t, 730, tier.Limits.TransactionHistoryDays, "Premium should have 730 transaction history days")
	assert.True(t, tier.Features.PrioritySupport, "Premium should have priority support")
	assert.True(t, tier.Features.AdvancedReports, "Premium should have advanced reports")
}

// TestGetTierLimits_Household5 tests household_5 tier has highest limits
func TestGetTierLimits_Household5(t *testing.T) {
	tierConfig, err := config.LoadTierConfig()
	require.NoError(t, err)

	service := NewSubscriptionService(tierConfig, nil)
	ctx := context.Background()

	tier, err := service.GetTier(ctx, domain.TierHousehold5)
	require.NoError(t, err)

	assert.Equal(t, 730, tier.Limits.TransactionHistoryDays, "Household5 should have 730 transaction history days")
	assert.True(t, tier.Features.APIAccess, "Household5 should have API access")
}

// TestGetEffectiveSubscription_NoOverrides tests getting effective subscription without overrides
func TestGetEffectiveSubscription_NoOverrides(t *testing.T) {
	tierConfig, err := config.LoadTierConfig()
	require.NoError(t, err)

	service := NewSubscriptionService(tierConfig, nil)
	ctx := context.Background()

	user := &domain.User{
		ID:               "user-123",
		SubscriptionTier: string(domain.TierPremium),
	}

	effective, err := service.GetEffectiveSubscription(ctx, user)
	require.NoError(t, err)
	assert.Equal(t, domain.TierPremium, effective.Tier, "Tier should be premium")
	assert.False(t, effective.HasOverrides, "Should not have overrides")
	assert.Equal(t, 0, effective.ActiveOverridesCount, "Override count should be 0")
}

// mockSubscriptionOverrideRepository is a testify mock of
// repository.SubscriptionOverrideRepository, used by the two tests below now that
// the real repository exists and is wired into NewSubscriptionService — the stale
// "will implement after repository is created" skip reason no longer holds.
type mockSubscriptionOverrideRepository struct{ mock.Mock }

func (m *mockSubscriptionOverrideRepository) CreateOverride(ctx context.Context, override *domain.UserSubscriptionOverride) error {
	args := m.Called(ctx, override)
	return args.Error(0)
}

func (m *mockSubscriptionOverrideRepository) GetActiveOverrides(ctx context.Context, userID string, asOf time.Time) ([]domain.UserSubscriptionOverride, error) {
	args := m.Called(ctx, userID, asOf)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.UserSubscriptionOverride), args.Error(1)
}

func (m *mockSubscriptionOverrideRepository) DeleteOverride(ctx context.Context, userID, resourceType string) error {
	args := m.Called(ctx, userID, resourceType)
	return args.Error(0)
}

func (m *mockSubscriptionOverrideRepository) GetOverrideByID(ctx context.Context, id int64) (*domain.UserSubscriptionOverride, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.UserSubscriptionOverride), args.Error(1)
}

func (m *mockSubscriptionOverrideRepository) UpdateOverride(ctx context.Context, override *domain.UserSubscriptionOverride) error {
	args := m.Called(ctx, override)
	return args.Error(0)
}

// TestGetEffectiveSubscription_WithOverrides tests effective subscription with custom overrides
func TestGetEffectiveSubscription_WithOverrides(t *testing.T) {
	tierConfig, err := config.LoadTierConfig()
	require.NoError(t, err)

	overrideRepo := &mockSubscriptionOverrideRepository{}
	service := NewSubscriptionService(tierConfig, overrideRepo)
	ctx := context.Background()

	user := &domain.User{
		ID:               "user-123",
		SubscriptionTier: string(domain.TierPremium),
	}

	overrideRepo.On("GetActiveOverrides", mock.Anything, user.ID, mock.Anything).Return(
		[]domain.UserSubscriptionOverride{
			{
				UserID:       user.ID,
				ResourceType: string(domain.ResourceTransactionHistoryDays),
				LimitValue:   9999,
			},
		}, nil).Once()

	effective, err := service.GetEffectiveSubscription(ctx, user)
	require.NoError(t, err)
	assert.Equal(t, domain.TierPremium, effective.Tier)
	assert.True(t, effective.HasOverrides, "Should report that overrides are active")
	assert.Equal(t, 1, effective.ActiveOverridesCount, "Should count the one active override")
	assert.Equal(t, 9999, effective.Limits.TransactionHistoryDays, "Override value should replace the tier's base limit")
	overrideRepo.AssertExpectations(t)
}

// TestApplyOverride tests applying a custom override for enterprise deals
func TestApplyOverride(t *testing.T) {
	tierConfig, err := config.LoadTierConfig()
	require.NoError(t, err)

	overrideRepo := &mockSubscriptionOverrideRepository{}
	service := NewSubscriptionService(tierConfig, overrideRepo)
	ctx := context.Background()

	overrideRepo.On("CreateOverride", mock.Anything, mock.MatchedBy(func(o *domain.UserSubscriptionOverride) bool {
		return o.UserID == "user-123" &&
			o.ResourceType == string(domain.ResourceTransactionHistoryDays) &&
			o.LimitValue == 9999 &&
			o.Reason != nil && *o.Reason == "enterprise_deal"
	})).Return(nil).Once()

	err = service.ApplyOverride(ctx, "user-123", domain.ResourceTransactionHistoryDays, 9999, "enterprise_deal")
	require.NoError(t, err)
	overrideRepo.AssertExpectations(t)
}

// TestApplyOverride_RepositoryError verifies ApplyOverride surfaces a repository error
// instead of swallowing it, and TestApplyOverride_NilRepository verifies the nil-repo
// guard (s.overrideRepo == nil) that ApplyOverride falls back to when no repository is
// wired — both are new coverage for a method that had none in any environment.
func TestApplyOverride_RepositoryError(t *testing.T) {
	tierConfig, err := config.LoadTierConfig()
	require.NoError(t, err)

	overrideRepo := &mockSubscriptionOverrideRepository{}
	service := NewSubscriptionService(tierConfig, overrideRepo)
	ctx := context.Background()

	overrideRepo.On("CreateOverride", mock.Anything, mock.Anything).Return(errors.New("db unavailable")).Once()

	err = service.ApplyOverride(ctx, "user-123", domain.ResourceTransactionHistoryDays, 9999, "enterprise_deal")
	require.Error(t, err)
	overrideRepo.AssertExpectations(t)
}

func TestApplyOverride_NilRepository(t *testing.T) {
	tierConfig, err := config.LoadTierConfig()
	require.NoError(t, err)

	service := NewSubscriptionService(tierConfig, nil)
	ctx := context.Background()

	err = service.ApplyOverride(ctx, "user-123", domain.ResourceTransactionHistoryDays, 9999, "enterprise_deal")
	require.Error(t, err, "ApplyOverride must fail loudly, not silently no-op, when no repository is wired")
}

// TestListAllTiers tests listing all available tiers
func TestListAllTiers(t *testing.T) {
	tierConfig, err := config.LoadTierConfig()
	require.NoError(t, err)

	service := NewSubscriptionService(tierConfig, nil)
	ctx := context.Background()

	tiers, err := service.ListTiers(ctx)
	require.NoError(t, err)
	assert.Len(t, tiers, 5, "Should have 5 tiers (premium, household_2, household_3, household_4, household_5)")

	// Verify all expected tiers exist
	tierNames := make(map[domain.TierName]bool)
	for _, tier := range tiers {
		tierNames[tier.Name] = true
	}

	assert.True(t, tierNames[domain.TierPremium], "Should include premium tier")
	assert.True(t, tierNames[domain.TierHousehold2], "Should include household_2 tier")
	assert.True(t, tierNames[domain.TierHousehold3], "Should include household_3 tier")
	assert.True(t, tierNames[domain.TierHousehold4], "Should include household_4 tier")
	assert.True(t, tierNames[domain.TierHousehold5], "Should include household_5 tier")
}

// TestGetDefaultTier tests getting the default tier
func TestGetDefaultTier(t *testing.T) {
	tierConfig, err := config.LoadTierConfig()
	require.NoError(t, err)

	service := NewSubscriptionService(tierConfig, nil)
	ctx := context.Background()

	tier, err := service.GetDefaultTier(ctx)
	require.NoError(t, err)
	assert.Equal(t, domain.TierPremium, tier.Name, "Default tier should be premium")
}

// TestInvalidTier tests handling of invalid tier names
func TestInvalidTier(t *testing.T) {
	tierConfig, err := config.LoadTierConfig()
	require.NoError(t, err)

	service := NewSubscriptionService(tierConfig, nil)
	ctx := context.Background()

	_, err = service.GetTier(ctx, domain.TierName("invalid_tier"))
	assert.Error(t, err, "Should return error for invalid tier")
}

// TestGetEffectiveSubscription_ManualOverrideTakesPrecedence tests that a non-expired manual override
// overrides the base tier when computing the effective subscription.
func TestGetEffectiveSubscription_ManualOverrideTakesPrecedence(t *testing.T) {
	tierConfig, err := config.LoadTierConfig()
	require.NoError(t, err)

	service := NewSubscriptionService(tierConfig, nil)
	ctx := context.Background()

	overrideTier := string(domain.TierHousehold5)
	futureExpiry := time.Now().Add(30 * 24 * time.Hour)
	user := &domain.User{
		ID:                 "user-123",
		SubscriptionTier:   string(domain.TierPremium),
		ManualTierOverride: &overrideTier,
		ManualTierEndsAt:   &futureExpiry,
	}

	effective, err := service.GetEffectiveSubscription(ctx, user)
	require.NoError(t, err)
	assert.Equal(t, domain.TierHousehold5, effective.Tier, "Override tier should take precedence over base tier")
}

// TestGetEffectiveSubscription_ExpiredOverrideFallsBackToBase tests that an expired manual override
// falls back to the base subscription tier.
func TestGetEffectiveSubscription_ExpiredOverrideFallsBackToBase(t *testing.T) {
	tierConfig, err := config.LoadTierConfig()
	require.NoError(t, err)

	service := NewSubscriptionService(tierConfig, nil)
	ctx := context.Background()

	overrideTier := string(domain.TierHousehold5)
	pastExpiry := time.Now().Add(-24 * time.Hour)
	user := &domain.User{
		ID:                 "user-123",
		SubscriptionTier:   string(domain.TierPremium),
		ManualTierOverride: &overrideTier,
		ManualTierEndsAt:   &pastExpiry,
	}

	effective, err := service.GetEffectiveSubscription(ctx, user)
	require.NoError(t, err)
	assert.Equal(t, domain.TierPremium, effective.Tier, "Expired override should fall back to base tier")
}

// TestGetEffectiveSubscription_NilOverrideUsesBaseTier tests that a nil ManualTierOverride
// means the base tier is used.
func TestGetEffectiveSubscription_NilOverrideUsesBaseTier(t *testing.T) {
	tierConfig, err := config.LoadTierConfig()
	require.NoError(t, err)

	service := NewSubscriptionService(tierConfig, nil)
	ctx := context.Background()

	user := &domain.User{
		ID:                 "user-123",
		SubscriptionTier:   string(domain.TierPremium),
		ManualTierOverride: nil,
	}

	effective, err := service.GetEffectiveSubscription(ctx, user)
	require.NoError(t, err)
	assert.Equal(t, domain.TierPremium, effective.Tier, "Nil override should use base tier")
}

// TestGetHistoricalDepthLimit_Premium tests that premium tier returns 730 days of history
func TestGetHistoricalDepthLimit_Premium(t *testing.T) {
	tierConfig, err := config.LoadTierConfig()
	require.NoError(t, err)

	service := NewSubscriptionService(tierConfig, nil)
	ctx := context.Background()

	user := &domain.User{
		ID:               "user-123",
		SubscriptionTier: string(domain.TierPremium),
	}

	days, err := service.GetHistoricalDepthLimit(ctx, user)
	require.NoError(t, err)
	assert.Equal(t, 730, days, "Premium tier should return 730 days of history")
}

// TestGetHistoricalDepthLimit_Household5 tests that household tiers also return 730 days
func TestGetHistoricalDepthLimit_Household5(t *testing.T) {
	tierConfig, err := config.LoadTierConfig()
	require.NoError(t, err)

	service := NewSubscriptionService(tierConfig, nil)
	ctx := context.Background()

	user := &domain.User{
		ID:               "user-household",
		SubscriptionTier: string(domain.TierHousehold5),
	}

	days, err := service.GetHistoricalDepthLimit(ctx, user)
	require.NoError(t, err)
	assert.Equal(t, 730, days, "Household5 tier should return 730 days of history")
}

// TestGetHistoricalDepthLimit_ManualOverride tests that manual tier override is respected
func TestGetHistoricalDepthLimit_ManualOverride(t *testing.T) {
	tierConfig, err := config.LoadTierConfig()
	require.NoError(t, err)

	service := NewSubscriptionService(tierConfig, nil)
	ctx := context.Background()

	overrideTier := string(domain.TierHousehold5)
	futureExpiry := time.Now().Add(30 * 24 * time.Hour)
	user := &domain.User{
		ID:                 "user-123",
		SubscriptionTier:   string(domain.TierPremium),
		ManualTierOverride: &overrideTier,
		ManualTierEndsAt:   &futureExpiry,
	}

	days, err := service.GetHistoricalDepthLimit(ctx, user)
	require.NoError(t, err)
	assert.Equal(t, 730, days, "Manual override to household_5 should still return 730 days")
}

// TestGetEffectiveSubscription_PermanentOverride tests that a nil ManualTierEndsAt means
// the override never expires (permanent override).
func TestGetEffectiveSubscription_PermanentOverride(t *testing.T) {
	tierConfig, err := config.LoadTierConfig()
	require.NoError(t, err)

	service := NewSubscriptionService(tierConfig, nil)
	ctx := context.Background()

	overrideTier := string(domain.TierHousehold5)
	user := &domain.User{
		ID:                 "user-123",
		SubscriptionTier:   string(domain.TierPremium),
		ManualTierOverride: &overrideTier,
		ManualTierEndsAt:   nil, // nil = permanent (never expires)
	}

	effective, err := service.GetEffectiveSubscription(ctx, user)
	require.NoError(t, err)
	assert.Equal(t, domain.TierHousehold5, effective.Tier, "Permanent override (nil expiry) should always apply")
}

// mockResolver lets us test both the nil-resolver golden path and the
// production-mode golden path (non-nil resolver, no household row) explicitly —
// Codex flagged that a nil-resolver test alone doesn't cover the real prod wiring.
type mockResolver struct{ mock.Mock }

func (m *mockResolver) ResolveEffectiveEntitlement(ctx context.Context, user *domain.User) (domain.EffectiveEntitlement, error) {
	args := m.Called(ctx, user)
	return args.Get(0).(domain.EffectiveEntitlement), args.Error(1)
}

// ResolveEffectiveEntitlementAt (Batch S2 Task 1) is the method GetEffectiveSubscriptionAt
// actually calls now — see HouseholdEntitlementResolver's doc comment. Every existing test below
// that wires mockResolver registers expectations against THIS method, not the plain
// ResolveEffectiveEntitlement above (which no production or test call site here still invokes,
// but which stays on the mock/interface so IsCoveredByHousehold/GetEntitlementSummary's real
// no-arg callers keep working against HouseholdEntitlementService).
func (m *mockResolver) ResolveEffectiveEntitlementAt(ctx context.Context, user *domain.User, now time.Time) (domain.EffectiveEntitlement, error) {
	args := m.Called(ctx, user, now)
	return args.Get(0).(domain.EffectiveEntitlement), args.Error(1)
}

func TestGetEffectiveSubscription_NilResolver_GoldenUnchanged(t *testing.T) {
	tierConfig, err := config.LoadTierConfig()
	require.NoError(t, err)
	service := NewSubscriptionService(tierConfig, nil) // no SetEntitlementResolver call
	ctx := context.Background()

	user := &domain.User{ID: "user-123", SubscriptionTier: string(domain.TierPremium)}
	effective, err := service.GetEffectiveSubscription(ctx, user)
	require.NoError(t, err)
	assert.Equal(t, domain.TierPremium, effective.Tier)
	assert.Equal(t, 1, effective.UsersIncluded)
}

func TestGetEffectiveSubscription_ResolverPresentNoHouseholdRow_GoldenUnchanged(t *testing.T) {
	tierConfig, err := config.LoadTierConfig()
	require.NoError(t, err)
	service := NewSubscriptionService(tierConfig, nil)
	resolver := &mockResolver{}
	user := &domain.User{ID: "user-123", SubscriptionTier: string(domain.TierPremium)}
	resolver.On("ResolveEffectiveEntitlementAt", mock.Anything, user, mock.Anything).Return(domain.EffectiveEntitlement{
		Tier: "premium", Status: "active", Source: domain.EntitlementSourceOwn,
	}, nil).Once()
	service.SetEntitlementResolver(resolver)

	effective, err := service.GetEffectiveSubscription(context.Background(), user)
	require.NoError(t, err)
	assert.Equal(t, domain.TierPremium, effective.Tier, "production wiring with no household row must match solo behavior byte-for-byte")
	resolver.AssertExpectations(t)
}

func TestGetEffectiveSubscription_ResolverAppliesHouseholdTier(t *testing.T) {
	tierConfig, err := config.LoadTierConfig()
	require.NoError(t, err)
	service := NewSubscriptionService(tierConfig, nil)
	resolver := &mockResolver{}
	user := &domain.User{ID: "member-1", SubscriptionTier: string(domain.TierPremium)}
	resolver.On("ResolveEffectiveEntitlementAt", mock.Anything, user, mock.Anything).Return(domain.EffectiveEntitlement{
		Tier: "household_2", Status: "active", Source: domain.EntitlementSourceHousehold,
	}, nil).Once()
	service.SetEntitlementResolver(resolver)

	effective, err := service.GetEffectiveSubscription(context.Background(), user)
	require.NoError(t, err)
	assert.Equal(t, domain.TierHousehold2, effective.Tier)
	assert.Equal(t, 2, effective.UsersIncluded)
	resolver.AssertExpectations(t)
}

func TestGetEffectiveSubscription_ResolverError_DoesNotFallBackToLegacyTier(t *testing.T) {
	tierConfig, err := config.LoadTierConfig()
	require.NoError(t, err)
	service := NewSubscriptionService(tierConfig, nil)
	resolver := &mockResolver{}
	user := &domain.User{ID: "user-123", SubscriptionTier: string(domain.TierPremium)}
	resolver.On("ResolveEffectiveEntitlementAt", mock.Anything, user, mock.Anything).
		Return(domain.EffectiveEntitlement{}, errors.New("db timeout")).Once()
	service.SetEntitlementResolver(resolver)

	effective, err := service.GetEffectiveSubscription(context.Background(), user)
	require.ErrorContains(t, err, "db timeout")
	assert.Nil(t, effective, "an unconfirmed legacy tier must not authorize access during an authority outage")
	resolver.AssertExpectations(t)
}

// ─── Issue #1413 (this task) — tier axis: malformed manual_tier_override must not 500 ────────

// TestGetEffectiveSubscription_MalformedOverride_WithValidator_FallsBackToRawTier is the RED
// test: manual_tier_override is an unconstrained VARCHAR(50) with no CHECK constraint, so it can
// hold a value naming no configured tier. Before this fix, GetEffectiveSubscription called
// domain.User.ComputeEffectiveTier() unconditionally, so a malformed override reached
// tierConfig.GetTier() as an unrecognized name and returned an error — this is the root cause of
// the admin subscription-detail 500, the /households/me/entitlement 500, and the Plaid
// first-sync misclassification (all of which call through here). With a validator wired that
// rejects "not_a_tier", the malformed override must fall back to the raw (base) subscription
// tier instead of erroring.
func TestGetEffectiveSubscription_MalformedOverride_WithValidator_FallsBackToRawTier(t *testing.T) {
	tierConfig, err := config.LoadTierConfig()
	require.NoError(t, err)
	service := NewSubscriptionService(tierConfig, nil)
	service.SetOverrideTierValidator(func(tier string) bool {
		_, err := tierConfig.GetTier(domain.TierName(tier))
		return err == nil
	})

	override := "not_a_tier"
	futureEnd := time.Now().Add(24 * time.Hour)
	user := &domain.User{
		ID:                 "user-123",
		SubscriptionTier:   string(domain.TierPremium),
		SubscriptionStatus: "active", // actively paying — the worst-affected case
		ManualTierOverride: &override,
		ManualTierEndsAt:   &futureEnd,
	}

	effective, err := service.GetEffectiveSubscription(context.Background(), user)
	require.NoError(t, err, "a malformed override must fall back to the raw tier, not error")
	assert.Equal(t, domain.TierPremium, effective.Tier, "must fall back to the raw base tier, not the garbage override string")
}

// TestGetEffectiveSubscription_ValidOverride_WithValidator_StillAppliesOverrideTier is the
// POSITIVE CONTROL for the fix above: with the exact same validator wired, a VALID,
// currently-configured override must still resolve to the override tier. An over-tight fix that
// makes every comp fall back to the raw tier would silently un-comp every legitimately comped
// user, which is worse than the bug being fixed.
func TestGetEffectiveSubscription_ValidOverride_WithValidator_StillAppliesOverrideTier(t *testing.T) {
	tierConfig, err := config.LoadTierConfig()
	require.NoError(t, err)
	service := NewSubscriptionService(tierConfig, nil)
	service.SetOverrideTierValidator(func(tier string) bool {
		_, err := tierConfig.GetTier(domain.TierName(tier))
		return err == nil
	})

	override := string(domain.TierHousehold3)
	futureEnd := time.Now().Add(24 * time.Hour)
	user := &domain.User{
		ID:                 "user-123",
		SubscriptionTier:   string(domain.TierPremium),
		SubscriptionStatus: "inactive",
		ManualTierOverride: &override,
		ManualTierEndsAt:   &futureEnd,
	}

	effective, err := service.GetEffectiveSubscription(context.Background(), user)
	require.NoError(t, err)
	assert.Equal(t, domain.TierHousehold3, effective.Tier, "a valid override must still apply — over-tightening would silently un-comp legitimately comped users")
}

// TestGetEffectiveSubscription_MalformedOverride_NilValidator_PreFixBehaviorPreserved pins the
// documented nil-validator semantics (see SetOverrideTierValidator's doc comment once added): if
// the validator is never wired, a malformed override is trusted unconditionally (skip
// validation, not deny) — exactly the pre-fix behavior. main.go wires this validator for the
// entire process lifetime in production (os.Exit(1) if the tier config fails to load), so a nil
// validator is a test-only/wiring-gap scenario, never real production; failing closed here
// instead would turn a future wiring oversight into an instant outage for every currently-comped
// user.
func TestGetEffectiveSubscription_MalformedOverride_NilValidator_PreFixBehaviorPreserved(t *testing.T) {
	tierConfig, err := config.LoadTierConfig()
	require.NoError(t, err)
	service := NewSubscriptionService(tierConfig, nil) // SetOverrideTierValidator never called

	override := "not_a_tier"
	futureEnd := time.Now().Add(24 * time.Hour)
	user := &domain.User{
		ID:                 "user-123",
		SubscriptionTier:   string(domain.TierPremium),
		ManualTierOverride: &override,
		ManualTierEndsAt:   &futureEnd,
	}

	_, err = service.GetEffectiveSubscription(context.Background(), user)
	assert.Error(t, err, "nil validator must skip validation, not deny — matching this codebase's other SetOverrideTierValidator call sites")
}

// ─── Batch S2 Task 1 — GetEffectiveSubscriptionAt's asOf must survive the entitlement resolver ──

// TestGetEffectiveSubscriptionAt_ProductionWiring_RealResolver_TierDerivedFromAsOf_NotResolversOwnClock
// is the RED test for Batch S2 Task 1: GetEffectiveSubscriptionAt(ctx, user, asOf) computes
// tierName := effectiveTierNameAt(user, asOf) first (correctly derived from the caller's asOf),
// then — if an entitlement resolver is wired (main.go:500, SetEntitlementResolver) —
// UNCONDITIONALLY OVERWRITES tierName with
// s.entitlementResolver.ResolveEffectiveEntitlement(ctx, user)'s result. That call takes NO
// instant parameter at all, so it falls through to HouseholdEntitlementService's own internal
// `now := time.Now()` — a SECOND, independent clock read taken later than asOf.
//
// The existing regression test for this exact class of bug
// (TestGetEffectiveSubscription_ResolverPresentNoHouseholdRow_GoldenUnchanged and friends, above)
// only prove the mockResolver test double is invoked correctly — they never touch asOf/timing at
// all. TestGetUserSubscription_EffectiveTier_DerivedFromAsOf_NotLaterClockRead
// (admin_subscriptions_test.go) DOES test asOf-derivation, but builds the SubscriptionService with
// NO entitlement resolver wired at all (SetEntitlementResolver is never called on that test's
// subService) — a wiring shape production NEVER uses (main.go:500 always wires
// householdEntitlementService, unconditionally, for every request). This test instead wires a REAL
// *HouseholdEntitlementService (backed by minimal repo/loader test doubles), exactly like
// main.go's production wiring, for a SOLO user with no household entitlement row — the common
// case, and proof that this bug affects EVERY user, not just household members: even the
// "own"/no-household-row branch of ResolveEffectiveEntitlement reads its own now := time.Now().
//
// Uses a real time.Sleep, not an injected clock, the same way as
// TestAdminUserService_GetUserDetail_EffectiveStatus_DerivedFromPassedInstant_NotLaterClockRead
// (admin_user_service_test.go) and TestGetUserSubscription_EffectiveTier_DerivedFromAsOf_NotLaterClockRead
// (admin_subscriptions_test.go): asOf is captured FIRST while the override is still live, then an
// artificial sleep pushes wall-clock time past the override's expiry, and ONLY THEN is
// GetEffectiveSubscriptionAt called with the ORIGINAL asOf. A correct implementation must report
// the override tier (the override was live AS OF asOf); the pre-fix implementation instead
// re-reads time.Now() AFTER the sleep, inside the resolver, sees an EXPIRED override, and wrongly
// falls back to the raw tier — so this test fails on today's code and passes once
// GetEffectiveSubscriptionAt threads asOf all the way through the resolver
// (ResolveEffectiveEntitlementAt).
func TestGetEffectiveSubscriptionAt_ProductionWiring_RealResolver_TierDerivedFromAsOf_NotResolversOwnClock(t *testing.T) {
	tierConfig, err := config.LoadTierConfig()
	require.NoError(t, err)

	entRepo := new(mockEntitlementRepo)
	userLoader := new(mockUserLoader)
	householdRepo := new(mockHouseholdRepo)
	householdEntitlementService := NewHouseholdEntitlementService(entRepo, userLoader, householdRepo, tierConfig)

	subService := NewSubscriptionService(tierConfig, nil)
	// Exactly main.go:500's production wiring — unconditional, not gated on whether the user
	// actually belongs to a household.
	subService.SetEntitlementResolver(householdEntitlementService)

	userID := "user-solo-1"
	override := string(domain.TierHousehold3)

	asOf := time.Now()
	// Still live AT asOf, but will have expired by the time the sleep below elapses.
	overrideEndsAt := asOf.Add(20 * time.Millisecond)

	user := &domain.User{
		ID:                 userID,
		SubscriptionTier:   string(domain.TierPremium),
		SubscriptionStatus: "active",
		ManualTierOverride: &override,
		ManualTierEndsAt:   &overrideEndsAt,
	}

	// Solo user: no household entitlement row at all — the common case, and the one this bug's
	// own doc-comment note incorrectly scoped to "a household member's tier" (see
	// SubscriptionService.GetEffectiveSubscriptionAt's doc comment before this fix).
	entRepo.On("GetActiveEntitlement", mock.Anything, userID, mock.Anything).Return(nil, repository.ErrEntitlementNotFound)

	// Simulate request-processing time crossing the override's expiry boundary BETWEEN the
	// caller's asOf capture (above) and GetEffectiveSubscriptionAt's call into the resolver.
	//
	// This is a real time.Sleep against the wall clock, not an injected fake clock: the resolver
	// under test (HouseholdEntitlementService.ResolveEffectiveEntitlement) takes no instant
	// parameter and calls time.Now() internally (see the doc comment above), and reshaping it to
	// accept one is out of scope for this fix. The 20ms override lifetime vs. this 60ms sleep
	// leaves a 40ms margin, which makes the test unlikely to flake under normal load — it does not
	// make it deterministic.
	time.Sleep(60 * time.Millisecond)

	effective, err := subService.GetEffectiveSubscriptionAt(context.Background(), user, asOf)
	require.NoError(t, err)
	assert.Equal(t, domain.TierHousehold3, effective.Tier,
		"effective tier must be derived from asOf (when the override was still live), not the "+
			"entitlement resolver's own later time.Now() read taken after it had already expired")
}
