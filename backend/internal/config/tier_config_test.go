package config

import (
	"os"
	"sync"
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadTierConfig(t *testing.T) {
	// Reset singleton for testing
	globalTierConfig = nil
	configOnce = *new(sync.Once)

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
			assert.GreaterOrEqual(t, tier.Limits.Accounts, -1)
			assert.GreaterOrEqual(t, tier.Limits.TransactionHistoryDays, -1)
			assert.GreaterOrEqual(t, tier.Limits.ExecutionsPerDay, 0)
		})
	}
}

func TestLoadTierConfig_InvalidPath(t *testing.T) {
	// Set invalid config path
	os.Setenv("SUBSCRIPTION_TIERS_CONFIG", "/nonexistent/path/config.yaml")
	defer os.Unsetenv("SUBSCRIPTION_TIERS_CONFIG")

	// Reset singleton
	globalTierConfig = nil
	configOnce = *new(sync.Once)

	_, err := LoadTierConfig()
	assert.Error(t, err)
}

func TestTierConfig_Reload(t *testing.T) {
	// Reset environment and singleton
	os.Unsetenv("SUBSCRIPTION_TIERS_CONFIG")
	globalTierConfig = nil
	configOnce = *new(sync.Once)

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
		name                     string
		subscriptionTiersConfig  string
		paddleEnvironment        string
		expectedPathSuffix       string
	}{
		{
			name:                    "explicit config path overrides all",
			subscriptionTiersConfig: "config/custom.yaml",
			paddleEnvironment:       "sandbox",
			expectedPathSuffix:      "config/custom.yaml",
		},
		{
			name:                    "sandbox environment loads sandbox config",
			subscriptionTiersConfig: "",
			paddleEnvironment:       "sandbox",
			expectedPathSuffix:      "config/subscription_tiers.sandbox.yaml",
		},
		{
			name:                    "production environment loads production config",
			subscriptionTiersConfig: "",
			paddleEnvironment:       "production",
			expectedPathSuffix:      "config/subscription_tiers.production.yaml",
		},
		{
			name:                    "no environment falls back to default",
			subscriptionTiersConfig: "",
			paddleEnvironment:       "",
			expectedPathSuffix:      "config/subscription_tiers.yaml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment variables
			if tt.subscriptionTiersConfig != "" {
				os.Setenv("SUBSCRIPTION_TIERS_CONFIG", tt.subscriptionTiersConfig)
				defer os.Unsetenv("SUBSCRIPTION_TIERS_CONFIG")
			} else {
				os.Unsetenv("SUBSCRIPTION_TIERS_CONFIG")
			}

			if tt.paddleEnvironment != "" {
				os.Setenv("PADDLE_ENVIRONMENT", tt.paddleEnvironment)
				defer os.Unsetenv("PADDLE_ENVIRONMENT")
			} else {
				os.Unsetenv("PADDLE_ENVIRONMENT")
			}

			// Get config path
			configPath := getConfigPath()

			// Verify path ends with expected suffix
			assert.Contains(t, configPath, tt.expectedPathSuffix,
				"Config path %s should contain %s", configPath, tt.expectedPathSuffix)
		})
	}
}

func TestLoadTierConfig_EnvironmentSpecific(t *testing.T) {
	t.Run("loads sandbox config when PADDLE_ENVIRONMENT=sandbox", func(t *testing.T) {
		// Set environment
		os.Setenv("PADDLE_ENVIRONMENT", "sandbox")
		defer os.Unsetenv("PADDLE_ENVIRONMENT")

		// Reset singleton
		globalTierConfig = nil
		configOnce = *new(sync.Once)

		// Load config
		config, err := LoadTierConfig()
		require.NoError(t, err)
		assert.NotNil(t, config)

		// Verify it loaded successfully (sandbox config exists and is valid)
		assert.Len(t, config.Tiers, 5)
	})

	t.Run("loads production config when PADDLE_ENVIRONMENT=production", func(t *testing.T) {
		// Set environment
		os.Setenv("PADDLE_ENVIRONMENT", "production")
		defer os.Unsetenv("PADDLE_ENVIRONMENT")

		// Reset singleton
		globalTierConfig = nil
		configOnce = *new(sync.Once)

		// Load config
		config, err := LoadTierConfig()
		require.NoError(t, err)
		assert.NotNil(t, config)

		// Verify it loaded successfully (production config exists and is valid)
		assert.Len(t, config.Tiers, 5)
	})
}
