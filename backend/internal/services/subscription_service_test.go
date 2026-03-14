package services

import (
	"context"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/config"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
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

	assert.Equal(t, -1, tier.Limits.Accounts, "Premium should have unlimited accounts (-1)")
	assert.Equal(t, 730, tier.Limits.TransactionHistoryDays, "Premium should have 730 transaction history days")
	assert.Equal(t, -1, tier.Limits.ExecutionsPerDay, "Premium should have unlimited executions per day (-1)")
	assert.True(t, tier.Features.PrioritySupport, "Premium should have priority support")
	assert.True(t, tier.Features.AdvancedReports, "Premium should have advanced reports")
	assert.False(t, tier.Features.AdsEnabled, "Premium should not have ads")
}

// TestGetTierLimits_Household5 tests household_5 tier has highest limits
func TestGetTierLimits_Household5(t *testing.T) {
	tierConfig, err := config.LoadTierConfig()
	require.NoError(t, err)

	service := NewSubscriptionService(tierConfig, nil)
	ctx := context.Background()

	tier, err := service.GetTier(ctx, domain.TierHousehold5)
	require.NoError(t, err)

	assert.Equal(t, -1, tier.Limits.Accounts, "Household5 should have unlimited accounts (-1)")
	assert.Equal(t, 730, tier.Limits.TransactionHistoryDays, "Household5 should have 730 transaction history days")
	assert.Equal(t, -1, tier.Limits.ExecutionsPerDay, "Household5 should have unlimited executions per day (-1)")
	assert.True(t, tier.Features.AIInsights, "Household5 should have AI insights")
	assert.True(t, tier.Features.APIAccess, "Household5 should have API access")
}

// TestCheckLimit_UnderLimit tests limit checking when user is under limit
func TestCheckLimit_UnderLimit(t *testing.T) {
	tierConfig, err := config.LoadTierConfig()
	require.NoError(t, err)

	service := NewSubscriptionService(tierConfig, nil)
	ctx := context.Background()

	// Create user with premium tier
	user := &domain.User{
		ID:               "user-123",
		SubscriptionTier: string(domain.TierPremium),
	}

	// All tiers have unlimited accounts (-1), so any usage is allowed
	result, err := service.CheckLimit(ctx, user, domain.ResourceAccounts, 5)
	require.NoError(t, err)
	assert.True(t, result.Allowed, "Should allow creation (unlimited)")
	assert.Equal(t, -1, result.Limit, "Premium tier account limit should be unlimited (-1)")
	assert.Equal(t, 5, result.CurrentUsage, "Current usage should be 5")
	assert.True(t, result.IsUnlimited, "Should be unlimited")
	assert.Equal(t, -1, result.RemainingUsage, "Remaining should be -1 for unlimited")
}

// TestCheckLimit_AtLimit tests limit checking when user is at the limit
func TestCheckLimit_AtLimit(t *testing.T) {
	tierConfig, err := config.LoadTierConfig()
	require.NoError(t, err)

	service := NewSubscriptionService(tierConfig, nil)
	ctx := context.Background()

	user := &domain.User{
		ID:               "user-123",
		SubscriptionTier: string(domain.TierPremium),
	}

	// All tiers have unlimited accounts (-1), so even "at 30" is still allowed
	result, err := service.CheckLimit(ctx, user, domain.ResourceAccounts, 30)
	require.NoError(t, err)
	assert.True(t, result.Allowed, "Should allow (unlimited accounts)")
	assert.Equal(t, -1, result.Limit, "Premium tier account limit should be unlimited (-1)")
	assert.Equal(t, 30, result.CurrentUsage, "Current usage should be 30")
	assert.True(t, result.IsUnlimited, "Should be unlimited")
}

// TestCheckLimit_OverLimit tests limit checking when user exceeds limit
func TestCheckLimit_OverLimit(t *testing.T) {
	tierConfig, err := config.LoadTierConfig()
	require.NoError(t, err)

	service := NewSubscriptionService(tierConfig, nil)
	ctx := context.Background()

	user := &domain.User{
		ID:               "user-123",
		SubscriptionTier: string(domain.TierPremium),
	}

	// All tiers have unlimited accounts (-1), so "over 30" is still allowed
	result, err := service.CheckLimit(ctx, user, domain.ResourceAccounts, 40)
	require.NoError(t, err)
	assert.True(t, result.Allowed, "Should allow (unlimited accounts)")
	assert.Equal(t, -1, result.Limit, "Premium tier account limit should be unlimited (-1)")
	assert.True(t, result.IsUnlimited, "Should be unlimited")
}

// TestCheckLimit_HouseholdTier tests limit checking for household tier with higher limits
func TestCheckLimit_HouseholdTier(t *testing.T) {
	tierConfig, err := config.LoadTierConfig()
	require.NoError(t, err)

	service := NewSubscriptionService(tierConfig, nil)
	ctx := context.Background()

	user := &domain.User{
		ID:               "user-household",
		SubscriptionTier: string(domain.TierHousehold5),
	}

	// Household5 also has unlimited accounts now
	result, err := service.CheckLimit(ctx, user, domain.ResourceAccounts, 100)
	require.NoError(t, err)
	assert.True(t, result.Allowed, "Household5 tier should allow 100 accounts (unlimited)")
	assert.Equal(t, -1, result.Limit, "Household5 account limit should be unlimited (-1)")
	assert.True(t, result.IsUnlimited, "Should be unlimited")
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
	assert.Equal(t, -1, effective.Limits.Accounts, "Should have premium account limits (unlimited)")
	assert.False(t, effective.HasOverrides, "Should not have overrides")
	assert.Equal(t, 0, effective.ActiveOverridesCount, "Override count should be 0")
}

// TestGetEffectiveSubscription_WithOverrides tests effective subscription with custom overrides
func TestGetEffectiveSubscription_WithOverrides(t *testing.T) {
	t.Skip("Requires database mock - will implement after repository is created")
}

// TestApplyOverride tests applying a custom override for enterprise deals
func TestApplyOverride(t *testing.T) {
	t.Skip("Requires database mock - will implement after repository is created")
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
	assert.Equal(t, -1, effective.Limits.Accounts, "Should use household_5 account limits (unlimited)")
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
	assert.Equal(t, -1, effective.Limits.Accounts, "Should use base premium account limits (unlimited)")
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
	assert.Equal(t, -1, effective.Limits.Accounts, "Should use household_5 account limits for permanent override (unlimited)")
}

// TestCheckLimit_AllResourceTypes tests all resource types
func TestCheckLimit_AllResourceTypes(t *testing.T) {
	tierConfig, err := config.LoadTierConfig()
	require.NoError(t, err)

	service := NewSubscriptionService(tierConfig, nil)
	ctx := context.Background()

	user := &domain.User{
		ID:               "user-123",
		SubscriptionTier: string(domain.TierPremium),
	}

	testCases := []struct {
		name          string
		resourceType  domain.ResourceType
		expectedLimit int
	}{
		{"accounts", domain.ResourceAccounts, -1},
		{"transaction_history_days", domain.ResourceTransactionHistoryDays, 730},
		{"executions_per_day", domain.ResourceExecutionsPerDay, -1},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := service.CheckLimit(ctx, user, tc.resourceType, 0)
			require.NoError(t, err)
			assert.Equal(t, tc.expectedLimit, result.Limit, "Limit should match expected for "+tc.name)
		})
	}
}
