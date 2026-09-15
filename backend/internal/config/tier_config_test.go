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

// setAndCleanupEnv sets environment variables and returns a cleanup function.
// Deliberately not t.Setenv: callers rely on the returned cleanup running when *they*
// choose (some invoke it when a helper returns, before the test ends), and on ""
// meaning a genuine unset rather than an empty value.
func setAndCleanupEnv(t *testing.T, envVars map[string]string) func() {
	t.Helper()
	setOrUnset := func(key, value string) {
		if value != "" {
			require.NoError(t, os.Setenv(key, value))
		} else {
			require.NoError(t, os.Unsetenv(key))
		}
	}
	original := make(map[string]string)
	for key, value := range envVars {
		original[key] = os.Getenv(key)
		setOrUnset(key, value)
	}
	return func() {
		for key, originalValue := range original {
			setOrUnset(key, originalValue)
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
		})
	}
}

// validSubscriptionTier is a helper for building a minimal, individually-valid tier fixture
// for validate() tests below, so each test only needs to override what it's actually testing.
func validSubscriptionTier(name domain.TierName, displayName string, usersIncluded int) domain.SubscriptionTier {
	return domain.SubscriptionTier{
		Name:        name,
		DisplayName: displayName,
		Pricing: domain.SubscriptionPricing{
			PriceMonthly:      9.99,
			PriceYearly:       99,
			MobilePriceYearly: 109.99,
			UsersIncluded:     usersIncluded,
		},
	}
}

func validTierConfigForValidationTests() *TierConfig {
	return &TierConfig{
		Tiers: map[domain.TierName]domain.SubscriptionTier{
			domain.TierPremium:    validSubscriptionTier(domain.TierPremium, "Premium", 1),
			domain.TierHousehold2: validSubscriptionTier(domain.TierHousehold2, "Household (2)", 2),
			domain.TierHousehold3: validSubscriptionTier(domain.TierHousehold3, "Household (3)", 3),
			domain.TierHousehold4: validSubscriptionTier(domain.TierHousehold4, "Household (4)", 4),
			domain.TierHousehold5: validSubscriptionTier(domain.TierHousehold5, "Household (5)", 5),
		},
		DefaultTier: domain.TierPremium,
		Trial:       TrialConfig{Enabled: true, DurationDays: 60, Tier: domain.TierPremium},
	}
}

// Issue #1432 followup: validate() previously only checked len(tc.Tiers) == 5. A catalogue
// with exactly 5 tiers but the WRONG names (e.g. a typo swapping "household_5" for
// "household_6") passed that check silently. It must now be rejected by name, not just count.
func TestTierConfig_Validation_WrongCanonicalTierNames(t *testing.T) {
	tc := validTierConfigForValidationTests()
	delete(tc.Tiers, domain.TierHousehold5)
	tc.Tiers["household_6"] = validSubscriptionTier("household_6", "Household (6)", 6)

	err := tc.validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "household_6")
}

// Issue #1432 followup: prices must be positive.
func TestTierConfig_Validation_NonPositivePrices(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(tier domain.SubscriptionTier) domain.SubscriptionTier
	}{
		{"zero price_monthly", func(t domain.SubscriptionTier) domain.SubscriptionTier { t.Pricing.PriceMonthly = 0; return t }},
		{"negative price_yearly", func(t domain.SubscriptionTier) domain.SubscriptionTier { t.Pricing.PriceYearly = -1; return t }},
		{"zero mobile_price_yearly", func(t domain.SubscriptionTier) domain.SubscriptionTier { t.Pricing.MobilePriceYearly = 0; return t }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := validTierConfigForValidationTests()
			tc.Tiers[domain.TierPremium] = tt.mutate(tc.Tiers[domain.TierPremium])

			err := tc.validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "premium")
		})
	}
}

// Issue #1432 followup: users_included must match the tier's own identity (household_3 must
// include 3 users, etc.) — a mismatch here is user-facing (wrong seat count advertised).
func TestTierConfig_Validation_UsersIncludedMismatch(t *testing.T) {
	tc := validTierConfigForValidationTests()
	tier := tc.Tiers[domain.TierHousehold3]
	tier.Pricing.UsersIncluded = 4
	tc.Tiers[domain.TierHousehold3] = tier

	err := tc.validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "household_3")
	assert.Contains(t, err.Error(), "users_included=4")
}

// Issue #1432 followup: trial.tier must reference a real tier and have a positive duration.
func TestTierConfig_Validation_Trial(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(tc *TierConfig)
	}{
		{"trial.tier unset", func(tc *TierConfig) { tc.Trial.Tier = "" }},
		{"trial.tier references unknown tier", func(tc *TierConfig) { tc.Trial.Tier = "not_a_real_tier" }},
		{"trial.duration_days zero", func(tc *TierConfig) { tc.Trial.DurationDays = 0 }},
		{"trial.duration_days negative", func(tc *TierConfig) { tc.Trial.DurationDays = -5 }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := validTierConfigForValidationTests()
			tt.mutate(tc)

			err := tc.validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "trial")
		})
	}
}

func TestLoadTierConfig_InvalidPath(t *testing.T) {
	defer setAndCleanupEnv(t, map[string]string{
		"SUBSCRIPTION_TIERS_CONFIG": "/nonexistent/path/config.yaml",
	})()

	resetConfigSingleton()

	_, err := LoadTierConfig()
	assert.Error(t, err)
}

func TestTierConfig_Reload(t *testing.T) {
	defer setAndCleanupEnv(t, map[string]string{
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
			defer setAndCleanupEnv(t, map[string]string{
				"SUBSCRIPTION_TIERS_CONFIG": tt.subscriptionTiers,
				"PADDLE_ENVIRONMENT":        tt.paddleEnv,
			})()

			configPath, err := getConfigPath()
			require.NoError(t, err)
			assert.Contains(t, configPath, tt.expectedPathSuffix,
				"Config path %s should contain %s", configPath, tt.expectedPathSuffix)
		})
	}
}

// Issue #1432 followup: getConfigPath must FAIL CLOSED when PADDLE_ENVIRONMENT is set but no
// matching config/subscription_tiers.<env>.yaml exists, rather than silently falling back to
// the default/template catalogue (which may carry sandbox Paddle price IDs).
func TestGetConfigPath_UnknownEnvironmentFailsClosed(t *testing.T) {
	defer setAndCleanupEnv(t, map[string]string{
		"SUBSCRIPTION_TIERS_CONFIG": "",
		"PADDLE_ENVIRONMENT":        "staging-does-not-exist",
	})()

	_, err := getConfigPath()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "staging-does-not-exist")
	assert.Contains(t, err.Error(), "config/subscription_tiers.staging-does-not-exist.yaml")
}

// Same failure mode, exercised through the public LoadTierConfig entry point rather than the
// unexported getConfigPath directly.
func TestLoadTierConfig_UnknownEnvironmentFailsClosed(t *testing.T) {
	defer setAndCleanupEnv(t, map[string]string{
		"SUBSCRIPTION_TIERS_CONFIG": "",
		"PADDLE_ENVIRONMENT":        "staging-does-not-exist",
	})()
	resetConfigSingleton()

	_, err := LoadTierConfig()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "staging-does-not-exist")
}

func TestLoadTierConfig_EnvironmentSpecific(t *testing.T) {
	tests := []struct {
		name      string
		paddleEnv string
	}{
		{"loads sandbox config when PADDLE_ENVIRONMENT=sandbox", "sandbox"},
		{"loads production config when PADDLE_ENVIRONMENT=production", "production"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer setAndCleanupEnv(t, map[string]string{
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

func TestLoadTierConfig_ProductionHasCompletePaddleCatalog(t *testing.T) {
	cleanup := setAndCleanupEnv(t, map[string]string{
		"SUBSCRIPTION_TIERS_CONFIG": "",
		"PADDLE_ENVIRONMENT":        "production",
	})
	t.Cleanup(func() {
		cleanup()
		resetConfigSingleton()
	})
	resetConfigSingleton()

	tierConfig, err := LoadTierConfig()
	require.NoError(t, err)
	householdFive, err := tierConfig.GetTier(domain.TierHousehold5)
	require.NoError(t, err)
	assert.Equal(t, "Household (5)", householdFive.DisplayName,
		"production must not advertise more than the enforced five household seats")

	seen := make(map[string]domain.TierName)
	for tierName, tier := range tierConfig.Tiers {
		priceIDs := map[string]*string{
			"monthly": tier.Pricing.PaddlePriceIDMonthly,
			"yearly":  tier.Pricing.PaddlePriceIDYearly,
		}

		for interval, priceID := range priceIDs {
			require.NotNil(t, priceID,
				"tier %s must configure a production Paddle %s price", tierName, interval)
			require.NotEmpty(t, *priceID,
				"tier %s must configure a non-empty production Paddle %s price", tierName, interval)
			require.True(t, strings.HasPrefix(*priceID, "pri_"),
				"tier %s production Paddle %s price must use a pri_ ID", tierName, interval)

			previousTier, duplicate := seen[*priceID]
			require.False(t, duplicate,
				"production Paddle price ID %s is shared by tiers %s and %s", *priceID, previousTier, tierName)
			seen[*priceID] = tierName
		}
	}

	assert.Len(t, seen, 10, "five production tiers must configure monthly and yearly Paddle prices")
}

func TestProductionPaddleCatalogDoesNotReuseSandboxPrices(t *testing.T) {
	sandboxPriceIDs := loadPaddlePriceIDsForEnvironment(t, "sandbox")
	productionPriceIDs := loadPaddlePriceIDsForEnvironment(t, "production")

	for priceID, productionTier := range productionPriceIDs {
		sandboxTier, reused := sandboxPriceIDs[priceID]
		require.False(t, reused,
			"production tier %s reuses sandbox Paddle price %s from tier %s", productionTier, priceID, sandboxTier)
	}
}

// Sandbox's counterpart to TestLoadTierConfig_ProductionHasCompletePaddleCatalog: every tier
// must configure a non-empty, uniquely-assigned pri_-prefixed Paddle price for both intervals.
// NOTE (scope, per issue #1432 followup): this proves the sandbox catalogue is INTERNALLY
// well-formed (non-empty, unique, correctly-prefixed IDs) — it does not, and cannot from here,
// prove these price IDs actually exist in Paddle's sandbox dashboard.
func TestLoadTierConfig_SandboxHasCompletePaddleCatalog(t *testing.T) {
	tierConfig := loadTierConfigForEnvironment(t, "sandbox")

	seen := make(map[string]domain.TierName)
	for tierName, tier := range tierConfig.Tiers {
		priceIDs := map[string]*string{
			"monthly": tier.Pricing.PaddlePriceIDMonthly,
			"yearly":  tier.Pricing.PaddlePriceIDYearly,
		}

		for interval, priceID := range priceIDs {
			require.NotNil(t, priceID,
				"tier %s must configure a sandbox Paddle %s price", tierName, interval)
			require.NotEmpty(t, *priceID,
				"tier %s must configure a non-empty sandbox Paddle %s price", tierName, interval)
			require.True(t, strings.HasPrefix(*priceID, "pri_"),
				"tier %s sandbox Paddle %s price must use a pri_ ID", tierName, interval)

			previousTier, duplicate := seen[*priceID]
			require.False(t, duplicate,
				"sandbox Paddle price ID %s is shared by tiers %s and %s", *priceID, previousTier, tierName)
			seen[*priceID] = tierName
		}
	}

	assert.Len(t, seen, 10, "five sandbox tiers must configure monthly and yearly Paddle prices")
}

// ─── Catalogue semantic parity (dev vs production) ──────────────────────────────────────────
//
// Issue #1432 followup: once each catalogue independently enforces the canonical key set
// (validate(), above), a bare "same tier keys" comparison between catalogues is too weak — it
// would miss a tier that exists in both but has different prices, limits, features, or seat
// counts. This compares a SEMANTIC PROJECTION instead: everything that should be identical
// across environments. Deliberately EXCLUDED from the projection:
//   - Paddle price IDs (expected to differ — sandbox vs. production Paddle dashboards)
//   - display_name / description (documented presentation differences are allowed — e.g.
//     production's household_5 reads "Household (5)" / "Premium features for up to 5 family
//     members" where dev/sandbox read "Household (5+)" / "...5+ family members...". Verified by
//     direct diff of the two YAML files: this is the ONLY value-level difference between them
//     besides Paddle IDs — every price, limit, feature flag, and users_included value is
//     otherwise identical.)

// tierSemanticProjection is the subset of a tier's definition that must agree across every
// environment's catalogue.
type tierSemanticProjection struct {
	UsersIncluded     int
	Limits            domain.SubscriptionLimits
	Features          domain.SubscriptionFeatures
	PriceMonthly      float64
	PriceYearly       float64
	MobilePriceYearly float64
}

// catalogueSemanticProjection is the subset of an entire catalogue that must agree across
// every environment.
type catalogueSemanticProjection struct {
	Tiers        map[domain.TierName]tierSemanticProjection
	DefaultTier  domain.TierName
	TrialTier    domain.TierName
	TrialDays    int
	TrialEnabled bool
}

func projectCatalogue(tc *TierConfig) catalogueSemanticProjection {
	tiers := make(map[domain.TierName]tierSemanticProjection, len(tc.Tiers))
	for name, tier := range tc.Tiers {
		tiers[name] = tierSemanticProjection{
			UsersIncluded:     tier.Pricing.UsersIncluded,
			Limits:            tier.Limits,
			Features:          tier.Features,
			PriceMonthly:      tier.Pricing.PriceMonthly,
			PriceYearly:       tier.Pricing.PriceYearly,
			MobilePriceYearly: tier.Pricing.MobilePriceYearly,
		}
	}
	return catalogueSemanticProjection{
		Tiers:        tiers,
		DefaultTier:  tc.DefaultTier,
		TrialTier:    tc.Trial.Tier,
		TrialDays:    tc.Trial.DurationDays,
		TrialEnabled: tc.Trial.Enabled,
	}
}

func TestCatalogueSemanticParity_DevVsProduction(t *testing.T) {
	dev := loadTierConfigForEnvironment(t, "")
	prod := loadTierConfigForEnvironment(t, "production")

	assert.Equal(t, projectCatalogue(dev), projectCatalogue(prod),
		"dev (config/subscription_tiers.yaml) and production catalogues must agree on canonical "+
			"keys, users_included, limits, features, prices, default_tier, and trial — "+
			"only Paddle price IDs and documented display_name/description differences are allowed")
}

func TestCatalogueSemanticParity_DevVsSandbox(t *testing.T) {
	dev := loadTierConfigForEnvironment(t, "")
	sandbox := loadTierConfigForEnvironment(t, "sandbox")

	assert.Equal(t, projectCatalogue(dev), projectCatalogue(sandbox),
		"dev (config/subscription_tiers.yaml) and sandbox catalogues must agree on canonical "+
			"keys, users_included, limits, features, prices, default_tier, and trial")
}

// loadTierConfigForEnvironment loads the tier config for the given PADDLE_ENVIRONMENT value
// ("" means unset, i.e. the default/template catalogue).
func loadTierConfigForEnvironment(t *testing.T, paddleEnvironment string) *TierConfig {
	t.Helper()

	cleanup := setAndCleanupEnv(t, map[string]string{
		"SUBSCRIPTION_TIERS_CONFIG": "",
		"PADDLE_ENVIRONMENT":        paddleEnvironment,
	})
	t.Cleanup(func() {
		cleanup()
		resetConfigSingleton()
	})
	resetConfigSingleton()

	tierConfig, err := LoadTierConfig()
	require.NoError(t, err)
	return tierConfig
}

func loadPaddlePriceIDsForEnvironment(t *testing.T, paddleEnvironment string) map[string]domain.TierName {
	t.Helper()

	cleanup := setAndCleanupEnv(t, map[string]string{
		"SUBSCRIPTION_TIERS_CONFIG": "",
		"PADDLE_ENVIRONMENT":        paddleEnvironment,
	})
	defer cleanup()
	defer resetConfigSingleton()
	resetConfigSingleton()

	tierConfig, err := LoadTierConfig()
	require.NoError(t, err)

	priceIDs := make(map[string]domain.TierName)
	for tierName, tier := range tierConfig.Tiers {
		for _, priceID := range []*string{
			tier.Pricing.PaddlePriceIDMonthly,
			tier.Pricing.PaddlePriceIDYearly,
		} {
			if priceID != nil && *priceID != "" {
				priceIDs[*priceID] = tierName
			}
		}
	}

	return priceIDs
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

// #1652 todo 6: the past_due config block was removed entirely (its only consumer, PastDueService,
// was deleted). This is the config-load acceptance criterion: the catalogue must still parse and
// validate with no past_due key present, and the key must be gone from every catalogue file.
func TestTierConfig_LoadsWithoutPastDueBlock(t *testing.T) {
	for _, path := range []string{
		"../../../config/subscription_tiers.yaml",
		"../../../config/subscription_tiers.sandbox.yaml",
		"../../../config/subscription_tiers.production.yaml",
	} {
		raw, err := os.ReadFile(path)
		require.NoError(t, err, "could not read %s", path)
		assert.NotContains(t, string(raw), "past_due",
			"%s must no longer carry the removed past_due block (#1652)", path)
	}

	cfg := loadTierConfigForEnvironment(t, "")
	require.NotNil(t, cfg)
	require.NoError(t, cfg.validate(), "the catalogue must validate with no past_due block")
}

// Issue #1569 Task 20 (S10) — the census claim, RE-DERIVED AND NOT REPRODUCED, then made loud.
//
// THE CLAIM: "daysRequested applies only when cursor == "", so a tier upgrade never deepens
// history and no upgrade-triggered re-sync exists."
//
// The mechanism is real. transactionSyncService's sync loop computes daysRequested only on the
// FIRST sync (`if cursor == ""`) and passes 0 on every re-sync, and plaidService only calls
// SetDaysRequested when the cursor is empty. Nothing in the tree re-syncs an item when a user's
// tier changes.
//
// The CONSEQUENCE does not exist, because there is no upgrade that would deepen anything: every
// tier in config/subscription_tiers.yaml declares transaction_history_days: 730, and there is no
// free tier (the config's own header says so — "Removed free tier entirely", "Only 1 configurable
// limit remains"). GetHistoricalDepthLimit returns the same 730 for premium and for household_5,
// so moving between tiers changes the depth by nothing and there is nothing for a re-sync to
// fetch. The item is deleted rather than fixed.
//
// What survives is a LATENT trap, and this test is what converts it into a loud one: the moment
// the tiers stop agreeing, the missing upgrade re-sync becomes a real defect — a user who paid for
// deeper history would keep the shallower window until they re-linked the bank, with no error and
// nothing to tell them. Differentiating the depths is a legitimate product decision; doing it
// WITHOUT the re-sync is the trap. This test fails on the former so the latter has to be thought
// about at the same time.
func TestTierConfig_HistoricalDepthIsUniform_OrTheUpgradeResyncMustExistFirst(t *testing.T) {
	config, err := LoadTierConfig()
	require.NoError(t, err)

	tiers := config.Tiers
	require.NotEmpty(t, tiers, "no tiers loaded — a vacuous pass here would read as protection")

	depths := map[int][]string{}
	for name, tier := range tiers {
		depths[tier.Limits.TransactionHistoryDays] = append(depths[tier.Limits.TransactionHistoryDays], string(name))
	}

	if len(depths) > 1 {
		t.Fatalf("tiers now declare %d different transaction_history_days values (%v).\n\n"+
			"That makes an upgrade able to DEEPEN a user's history — and nothing fetches the extra "+
			"days. transactionSyncService's sync loop computes daysRequested only when the cursor is "+
			"empty (its `if cursor == \"\"` branch) and passes 0 on every re-sync, plaidService "+
			"calls SetDaysRequested only when the cursor is empty, and no code path re-syncs an item "+
			"on a tier change. A user who upgraded would keep the shallower window until they "+
			"re-linked the bank, silently.\n\n"+
			"If differentiating the depths is the intent, land the upgrade-triggered re-sync in the "+
			"same change and then delete this test. Issue #1569 task 20 (S10).", len(depths), depths)
	}
}
