package services

import (
	"context"
	"testing"

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

// TestGetTierLimits_FreeTier tests that free tier has correct limits
func TestGetTierLimits_FreeTier(t *testing.T) {
	tierConfig, err := config.LoadTierConfig()
	require.NoError(t, err)

	service := NewSubscriptionService(tierConfig, nil)
	ctx := context.Background()

	// Get free tier limits
	tier, err := service.GetTier(ctx, domain.TierFree)
	require.NoError(t, err)

	// Assert free tier limits match configuration
	assert.Equal(t, 8, tier.Limits.Accounts, "Free tier should have 8 accounts")
	assert.Equal(t, 2, tier.Limits.Rules, "Free tier should have 2 rules")
	assert.Equal(t, 1, tier.Limits.ExecutionsPerDay, "Free tier should have 1 execution per day")
	assert.Equal(t, 1440, tier.Limits.MinExecutionIntervalMinutes, "Free tier should have 24-hour interval")
	assert.True(t, tier.Features.AdsEnabled, "Free tier should have ads enabled")
	assert.False(t, tier.Features.AIInsights, "Free tier should not have AI insights")
	assert.False(t, tier.Features.APIAccess, "Free tier should not have API access")
}

// TestGetTierLimits_PremiumLite tests premium lite tier limits
func TestGetTierLimits_PremiumLite(t *testing.T) {
	tierConfig, err := config.LoadTierConfig()
	require.NoError(t, err)

	service := NewSubscriptionService(tierConfig, nil)
	ctx := context.Background()

	tier, err := service.GetTier(ctx, domain.TierPremiumLite)
	require.NoError(t, err)

	assert.Equal(t, 15, tier.Limits.Accounts, "Premium Lite should have 15 accounts")
	assert.Equal(t, 5, tier.Limits.Rules, "Premium Lite should have 5 rules")
	assert.Equal(t, 2, tier.Limits.ExecutionsPerDay, "Premium Lite should have 2 executions per day")
	assert.Equal(t, 720, tier.Limits.MinExecutionIntervalMinutes, "Premium Lite should have 12-hour interval")
	assert.False(t, tier.Features.AdsEnabled, "Premium Lite should not have ads")
}

// TestGetTierLimits_Premium tests premium tier limits
func TestGetTierLimits_Premium(t *testing.T) {
	tierConfig, err := config.LoadTierConfig()
	require.NoError(t, err)

	service := NewSubscriptionService(tierConfig, nil)
	ctx := context.Background()

	tier, err := service.GetTier(ctx, domain.TierPremium)
	require.NoError(t, err)

	assert.Equal(t, 30, tier.Limits.Accounts, "Premium should have 30 accounts")
	assert.Equal(t, 15, tier.Limits.Rules, "Premium should have 15 rules")
	assert.Equal(t, 6, tier.Limits.ExecutionsPerDay, "Premium should have 6 executions per day")
	assert.Equal(t, 240, tier.Limits.MinExecutionIntervalMinutes, "Premium should have 4-hour interval")
	assert.True(t, tier.Features.PrioritySupport, "Premium should have priority support")
	assert.True(t, tier.Features.AdvancedReports, "Premium should have advanced reports")
}

// TestGetTierLimits_PremiumPlus tests premium+ tier has unlimited limits
func TestGetTierLimits_PremiumPlus(t *testing.T) {
	tierConfig, err := config.LoadTierConfig()
	require.NoError(t, err)

	service := NewSubscriptionService(tierConfig, nil)
	ctx := context.Background()

	tier, err := service.GetTier(ctx, domain.TierPremiumPlus)
	require.NoError(t, err)

	// Check for unlimited (-1)
	assert.Equal(t, domain.Unlimited, tier.Limits.Accounts, "Premium+ should have unlimited accounts")
	assert.Equal(t, domain.Unlimited, tier.Limits.Rules, "Premium+ should have unlimited rules")
	assert.Equal(t, 144, tier.Limits.ExecutionsPerDay, "Premium+ should have 144 executions per day (every 10min)")
	assert.Equal(t, 10, tier.Limits.MinExecutionIntervalMinutes, "Premium+ should have 10-minute interval")
	assert.True(t, tier.Features.AIInsights, "Premium+ should have AI insights")
	assert.True(t, tier.Features.APIAccess, "Premium+ should have API access")
}

// TestCheckLimit_UnderLimit tests limit checking when user is under limit
func TestCheckLimit_UnderLimit(t *testing.T) {
	tierConfig, err := config.LoadTierConfig()
	require.NoError(t, err)

	service := NewSubscriptionService(tierConfig, nil)
	ctx := context.Background()

	// Create mock user with free tier
	user := &domain.User{
		ID:               "user-123",
		SubscriptionTier: string(domain.TierFree),
	}

	// Check if user can create 1st rule (under limit of 2)
	result, err := service.CheckLimit(ctx, user, domain.ResourceRules, 1)
	require.NoError(t, err)
	assert.True(t, result.Allowed, "Should allow creation under limit")
	assert.Equal(t, 2, result.Limit, "Free tier limit should be 2")
	assert.Equal(t, 1, result.CurrentUsage, "Current usage should be 1")
	assert.Equal(t, 1, result.RemainingUsage, "Should have 1 remaining")
}

// TestCheckLimit_AtLimit tests limit checking when user is at the limit
func TestCheckLimit_AtLimit(t *testing.T) {
	tierConfig, err := config.LoadTierConfig()
	require.NoError(t, err)

	service := NewSubscriptionService(tierConfig, nil)
	ctx := context.Background()

	user := &domain.User{
		ID:               "user-123",
		SubscriptionTier: string(domain.TierFree),
	}

	// Check if user can create 3rd rule (at limit of 2, should fail)
	result, err := service.CheckLimit(ctx, user, domain.ResourceRules, 2)
	require.NoError(t, err)
	assert.False(t, result.Allowed, "Should NOT allow creation at limit")
	assert.Equal(t, 2, result.Limit, "Free tier limit should be 2")
	assert.Equal(t, 2, result.CurrentUsage, "Current usage should be 2")
	assert.Equal(t, 0, result.RemainingUsage, "Should have 0 remaining")
}

// TestCheckLimit_OverLimit tests limit checking when user exceeds limit
func TestCheckLimit_OverLimit(t *testing.T) {
	tierConfig, err := config.LoadTierConfig()
	require.NoError(t, err)

	service := NewSubscriptionService(tierConfig, nil)
	ctx := context.Background()

	user := &domain.User{
		ID:               "user-123",
		SubscriptionTier: string(domain.TierFree),
	}

	// Try to check with 10 accounts (over limit of 8)
	result, err := service.CheckLimit(ctx, user, domain.ResourceAccounts, 10)
	require.NoError(t, err)
	assert.False(t, result.Allowed, "Should NOT allow over limit")
	assert.Equal(t, 8, result.Limit, "Free tier account limit should be 8")
}

// TestCheckLimit_Unlimited tests limit checking for unlimited tier
func TestCheckLimit_Unlimited(t *testing.T) {
	tierConfig, err := config.LoadTierConfig()
	require.NoError(t, err)

	service := NewSubscriptionService(tierConfig, nil)
	ctx := context.Background()

	user := &domain.User{
		ID:               "user-premium-plus",
		SubscriptionTier: string(domain.TierPremiumPlus),
	}

	// Check with large number for unlimited tier
	result, err := service.CheckLimit(ctx, user, domain.ResourceRules, 1000)
	require.NoError(t, err)
	assert.True(t, result.Allowed, "Unlimited tier should allow any usage")
	assert.True(t, result.IsUnlimited, "Should be marked as unlimited")
	assert.Equal(t, domain.Unlimited, result.Limit, "Limit should be -1 (unlimited)")
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
	assert.Equal(t, 30, effective.Limits.Accounts, "Should have premium account limits")
	assert.False(t, effective.HasOverrides, "Should not have overrides")
	assert.Equal(t, 0, effective.ActiveOverridesCount, "Override count should be 0")
}

// TestGetEffectiveSubscription_WithOverrides tests effective subscription with custom overrides
// This will test the database override functionality
func TestGetEffectiveSubscription_WithOverrides(t *testing.T) {
	t.Skip("Requires database mock - will implement after repository is created")
	// TODO: Implement after creating override repository
	// Should test that overrides increase limits beyond base tier
}

// TestApplyOverride tests applying a custom override for enterprise deals
func TestApplyOverride(t *testing.T) {
	t.Skip("Requires database mock - will implement after repository is created")
	// TODO: Implement after creating override repository
	// Should test creating custom limits for specific users
}

// TestListAllTiers tests listing all available tiers
func TestListAllTiers(t *testing.T) {
	tierConfig, err := config.LoadTierConfig()
	require.NoError(t, err)

	service := NewSubscriptionService(tierConfig, nil)
	ctx := context.Background()

	tiers, err := service.ListTiers(ctx)
	require.NoError(t, err)
	assert.Len(t, tiers, 4, "Should have 4 tiers (free, premium_lite, premium, premium_plus)")

	// Verify all expected tiers exist
	tierNames := make(map[domain.TierName]bool)
	for _, tier := range tiers {
		tierNames[tier.Name] = true
	}

	assert.True(t, tierNames[domain.TierFree], "Should include free tier")
	assert.True(t, tierNames[domain.TierPremiumLite], "Should include premium lite tier")
	assert.True(t, tierNames[domain.TierPremium], "Should include premium tier")
	assert.True(t, tierNames[domain.TierPremiumPlus], "Should include premium plus tier")
}

// TestGetDefaultTier tests getting the default tier
func TestGetDefaultTier(t *testing.T) {
	tierConfig, err := config.LoadTierConfig()
	require.NoError(t, err)

	service := NewSubscriptionService(tierConfig, nil)
	ctx := context.Background()

	tier, err := service.GetDefaultTier(ctx)
	require.NoError(t, err)
	assert.Equal(t, domain.TierFree, tier.Name, "Default tier should be free")
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

// TestCheckLimit_AllResourceTypes tests all resource types
func TestCheckLimit_AllResourceTypes(t *testing.T) {
	tierConfig, err := config.LoadTierConfig()
	require.NoError(t, err)

	service := NewSubscriptionService(tierConfig, nil)
	ctx := context.Background()

	user := &domain.User{
		ID:               "user-123",
		SubscriptionTier: string(domain.TierFree),
	}

	testCases := []struct {
		name         string
		resourceType domain.ResourceType
		expectedLimit int
	}{
		{"accounts", domain.ResourceAccounts, 8},
		{"rules", domain.ResourceRules, 2},
		{"executions_per_day", domain.ResourceExecutionsPerDay, 1},
		{"api_calls_per_month", domain.ResourceAPICallsPerMonth, 0},
		{"data_export_per_month", domain.ResourceDataExportPerMonth, 1},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := service.CheckLimit(ctx, user, tc.resourceType, 0)
			require.NoError(t, err)
			assert.Equal(t, tc.expectedLimit, result.Limit, "Limit should match expected for "+tc.name)
		})
	}
}
