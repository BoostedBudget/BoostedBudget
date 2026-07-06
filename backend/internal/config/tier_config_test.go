package config

import (
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// resetConfigSingleton resets the global tier config singleton for testing
func resetConfigSingleton() {
	globalTierConfig = nil
	configOnce = *new(sync.Once)
}

// setAndCleanupEnv sets environment variables and returns a cleanup function
func setAndCleanupEnv(envVars map[string]string) func() {
	original := make(map[string]string)
	for key, value := range envVars {
		original[key] = os.Getenv(key)
		if value != "" {
			os.Setenv(key, value)
		} else {
			os.Unsetenv(key)
		}
	}
	return func() {
		for key, originalValue := range original {
			if originalValue != "" {
				os.Setenv(key, originalValue)
			} else {
				os.Unsetenv(key)
			}
		}
	}
}

func TestLoadTierConfig(t *testing.T) {
	// Reset singleton for testing
	resetConfigSingleton()

	config, err := LoadTierConfig()
	require.NoError(t, err)
	assert.NotNil(t, config)

	// Verify tiers loaded
	assert.Len(t, config.Tiers, 5)
	assert.Equal(t, domain.TierPremium, config.DefaultTier)
}

func TestLoadTierConfig_Singleton(t *testing.T) {
	// First call
	config1, err := LoadTierConfig()
	require.NoError(t, err)

	// Second call should return same instance
	config2, err := LoadTierConfig()
	require.NoError(t, err)

	// Same pointer address
	assert.Equal(t, config1, config2)
}

func TestTierConfig_GetTier(t *testing.T) {
	config, err := LoadTierConfig()
	require.NoError(t, err)

	tier, err := config.GetTier(domain.TierPremium)
	require.NoError(t, err)
	assert.Equal(t, domain.TierPremium, tier.Name)
	assert.Equal(t, "Premium", tier.DisplayName)
}

func TestTierConfig_GetTier_NotFound(t *testing.T) {
	config, err := LoadTierConfig()
	require.NoError(t, err)

	_, err = config.GetTier(domain.TierName("invalid"))
	assert.Error(t, err)
}

func TestTierConfig_GetDefaultTier(t *testing.T) {
	config, err := LoadTierConfig()
	require.NoError(t, err)

	tier, err := config.GetDefaultTier()
	require.NoError(t, err)
	assert.Equal(t, domain.TierPremium, tier.Name)
}

func TestTierConfig_ListTiers(t *testing.T) {
	config, err := LoadTierConfig()
	require.NoError(t, err)

	tiers := config.ListTiers()
	assert.Len(t, tiers, 5)
}

func TestTierConfig_GetTierNames(t *testing.T) {
	config, err := LoadTierConfig()
	require.NoError(t, err)

	names := config.GetTierNames()
	assert.Len(t, names, 5)
	assert.Contains(t, names, domain.TierPremium)
	assert.Contains(t, names, domain.TierHousehold2)
	assert.Contains(t, names, domain.TierHousehold3)
	assert.Contains(t, names, domain.TierHousehold4)
	assert.Contains(t, names, domain.TierHousehold5)
}

func TestTierConfig_Validation(t *testing.T) {
	config, err := LoadTierConfig()
	require.NoError(t, err)

	// Check that validation passes
	err = config.validate()
	assert.NoError(t, err)

	// Verify each tier has valid limits
	for name, tier := range config.Tiers {
		t.Run(string(name), func(t *testing.T) {
			assert.NotEmpty(t, tier.DisplayName)
			assert.GreaterOrEqual(t, tier.Limits.TransactionHistoryDays, -1)
			assert.GreaterOrEqual(t, tier.Limits.ExecutionsPerDay, -1)
		})
	}
}

func TestLoadTierConfig_InvalidPath(t *testing.T) {
	defer setAndCleanupEnv(map[string]string{
		"SUBSCRIPTION_TIERS_CONFIG": "/nonexistent/path/config.yaml",
	})()

	resetConfigSingleton()

	_, err := LoadTierConfig()
	assert.Error(t, err)
}

func TestTierConfig_Reload(t *testing.T) {
	defer setAndCleanupEnv(map[string]string{
		"SUBSCRIPTION_TIERS_CONFIG": "",
	})()

	resetConfigSingleton()

	config, err := LoadTierConfig()
	require.NoError(t, err)

	// Reload should succeed
	err = config.Reload()
	assert.NoError(t, err)

	// Verify still has tiers
	assert.Len(t, config.Tiers, 5)
}

func TestTierConfig_GetTrialConfig(t *testing.T) {
	config, err := LoadTierConfig()
	require.NoError(t, err)

	trialConfig := config.GetTrialConfig()

	assert.Equal(t, 60, trialConfig.DurationDays)
	assert.Equal(t, domain.TierPremium, trialConfig.Tier)
}

func TestGetConfigPath_EnvironmentSpecific(t *testing.T) {
	tests := []struct {
		name               string
		subscriptionTiers  string
		paddleEnv          string
		expectedPathSuffix string
	}{
		{"explicit config path overrides all", "config/custom.yaml", "sandbox", "config/custom.yaml"},
		{"sandbox environment loads sandbox config", "", "sandbox", "config/subscription_tiers.sandbox.yaml"},
		{"production environment loads production config", "", "production", "config/subscription_tiers.production.yaml"},
		{"no environment falls back to default", "", "", "config/subscription_tiers.yaml"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer setAndCleanupEnv(map[string]string{
				"SUBSCRIPTION_TIERS_CONFIG": tt.subscriptionTiers,
				"PADDLE_ENVIRONMENT":        tt.paddleEnv,
			})()

			configPath := getConfigPath()
			assert.Contains(t, configPath, tt.expectedPathSuffix,
				"Config path %s should contain %s", configPath, tt.expectedPathSuffix)
		})
	}
}

func TestLoadTierConfig_EnvironmentSpecific(t *testing.T) {
	tests := []struct {
		name     string
		paddleEnv string
	}{
		{"loads sandbox config when PADDLE_ENVIRONMENT=sandbox", "sandbox"},
		{"loads production config when PADDLE_ENVIRONMENT=production", "production"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer setAndCleanupEnv(map[string]string{
				"PADDLE_ENVIRONMENT": tt.paddleEnv,
			})()

			resetConfigSingleton()

			config, err := LoadTierConfig()
			require.NoError(t, err)
			assert.NotNil(t, config)
			assert.Len(t, config.Tiers, 5)
		})
	}
}

// Issue #772: GetTierByPriceID tests (GAP 3)
func TestTierConfig_GetTierByPriceID(t *testing.T) {
	config, err := LoadTierConfig()
	require.NoError(t, err)

	skipIfNoPriceIDs := func(t *testing.T, priceID *string) {
		if priceID == nil {
			t.Skip("Paddle price IDs not configured in default config")
		}
	}

	t.Run("finds tier by monthly price ID", func(t *testing.T) {
		premiumTier, err := config.GetTier(domain.TierPremium)
		require.NoError(t, err)
		skipIfNoPriceIDs(t, premiumTier.Pricing.PaddlePriceIDMonthly)

		tier, err := config.GetTierByPriceID(*premiumTier.Pricing.PaddlePriceIDMonthly)
		require.NoError(t, err)
		assert.Equal(t, domain.TierPremium, tier)
	})

	t.Run("finds tier by yearly price ID", func(t *testing.T) {
		premiumTier, err := config.GetTier(domain.TierPremium)
		require.NoError(t, err)
		skipIfNoPriceIDs(t, premiumTier.Pricing.PaddlePriceIDYearly)

		tier, err := config.GetTierByPriceID(*premiumTier.Pricing.PaddlePriceIDYearly)
		require.NoError(t, err)
		assert.Equal(t, domain.TierPremium, tier)
	})

	t.Run("finds household tiers by price ID", func(t *testing.T) {
		household2, err := config.GetTier(domain.TierHousehold2)
		require.NoError(t, err)
		skipIfNoPriceIDs(t, household2.Pricing.PaddlePriceIDMonthly)

		tier, err := config.GetTierByPriceID(*household2.Pricing.PaddlePriceIDMonthly)
		require.NoError(t, err)
		assert.Equal(t, domain.TierHousehold2, tier)
	})

	t.Run("returns error for unknown price ID", func(t *testing.T) {
		unknownPriceID := "pri_unknown_12345"
		_, err := config.GetTierByPriceID(unknownPriceID)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no tier found for price_id")
		assert.Contains(t, err.Error(), unknownPriceID)
	})

	t.Run("thread-safe concurrent lookups", func(t *testing.T) {
		premiumTier, err := config.GetTier(domain.TierPremium)
		require.NoError(t, err)
		skipIfNoPriceIDs(t, premiumTier.Pricing.PaddlePriceIDMonthly)

		priceID := *premiumTier.Pricing.PaddlePriceIDMonthly
		done := make(chan bool, 10)

		for i := 0; i < 10; i++ {
			go func() {
				defer func() { done <- true }()
				tier, err := config.GetTierByPriceID(priceID)
				assert.NoError(t, err)
				assert.Equal(t, domain.TierPremium, tier)
			}()
		}

		for i := 0; i < 10; i++ {
			<-done
		}
	})
}

// TestSubscriptionTierYAMLHasNoAdsEnabled guards against the ads concept being reintroduced in
// the YAML config files (#1288 follow-on). Go's yaml.v3 silently ignores unknown keys, so
// ads_enabled could reappear in the YAML and be invisible to Go tests that only inspect the
// unmarshalled struct — this test reads the raw file bytes instead.
func TestSubscriptionTierYAMLHasNoAdsEnabled(t *testing.T) {
	yamlPaths := []string{
		"../../../config/subscription_tiers.yaml",
		"../../../config/subscription_tiers.production.yaml",
		"../../../config/subscription_tiers.sandbox.yaml",
	}
	for _, path := range yamlPaths {
		raw, err := os.ReadFile(path)
		require.NoError(t, err, "could not read %s", path)
		assert.NotContains(t, strings.ToLower(string(raw)), "ads_enabled",
			"%s must not contain ads_enabled (#1288 follow-on)", path)
	}
}

// Issue #898: Test GetPastDueConfig returns correct values
func TestTierConfig_GetPastDueConfig(t *testing.T) {
	config, err := LoadTierConfig()
	require.NoError(t, err)

	pastDueConfig := config.GetPastDueConfig()

	assert.Equal(t, 14, pastDueConfig.PlaidDisconnectDays)
	assert.Equal(t, 21, pastDueConfig.GraceDays)
}

// Issue #898: Test Reload copies PastDue
func TestTierConfig_Reload_CopiesPastDue(t *testing.T) {
	defer setAndCleanupEnv(map[string]string{
		"SUBSCRIPTION_TIERS_CONFIG": "",
	})()
	resetConfigSingleton()

	config, err := LoadTierConfig()
	require.NoError(t, err)

	// Store original values
	originalPastDue := config.GetPastDueConfig()

	// Reload
	err = config.Reload()
	assert.NoError(t, err)

	// Verify PastDue was copied
	reloadedPastDue := config.GetPastDueConfig()
	assert.Equal(t, originalPastDue.PlaidDisconnectDays, reloadedPastDue.PlaidDisconnectDays)
	assert.Equal(t, originalPastDue.GraceDays, reloadedPastDue.GraceDays)
}

// Issue #898: Test validation rejects invalid PastDue values
func TestTierConfig_Validation_InvalidPastDue(t *testing.T) {
	validTiers := map[domain.TierName]domain.SubscriptionTier{
		domain.TierPremium:    {Name: domain.TierPremium, DisplayName: "Premium"},
		domain.TierHousehold2: {Name: domain.TierHousehold2, DisplayName: "Household (2)"},
		domain.TierHousehold3: {Name: domain.TierHousehold3, DisplayName: "Household (3)"},
		domain.TierHousehold4: {Name: domain.TierHousehold4, DisplayName: "Household (4)"},
		domain.TierHousehold5: {Name: domain.TierHousehold5, DisplayName: "Household (5+)"},
	}

	newConfig := func(plaidDays, graceDays int) *TierConfig {
		return &TierConfig{
			Tiers:       validTiers,
			DefaultTier: domain.TierPremium,
			Trial:       TrialConfig{DurationDays: 60, Tier: domain.TierPremium},
			PastDue:     PastDueConfig{PlaidDisconnectDays: plaidDays, GraceDays: graceDays},
		}
	}

	tests := []struct {
		name        string
		plaidDays   int
		graceDays   int
		expectError bool
	}{
		{"valid past_due config", 14, 21, false},
		{"past_due with plaid_disconnect_days = 0 should fail", 0, 21, true},
		{"past_due with grace_days = 0 should fail", 14, 0, true},
		{"past_due with grace_days < plaid_disconnect_days should fail", 21, 14, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := newConfig(tt.plaidDays, tt.graceDays).validate()
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
