package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"gopkg.in/yaml.v3"
)

// TierConfig holds the subscription tier configuration
// Loaded from config/subscription_tiers.yaml (centralized config directory)
type TierConfig struct {
	Tiers       map[domain.TierName]domain.SubscriptionTier `yaml:"tiers"`
	DefaultTier domain.TierName                              `yaml:"default_tier"`
	Trial       TrialConfig                                   `yaml:"trial"`
	mu          sync.RWMutex                                 `yaml:"-"` // Protects concurrent access
}

// TrialConfig holds trial subscription configuration
type TrialConfig struct {
	Enabled      bool            `yaml:"enabled"`
	DurationDays int             `yaml:"duration_days"`
	Tier         domain.TierName `yaml:"tier"`
}

// Global tier config instance (singleton)
var (
	globalTierConfig *TierConfig
	configOnce       sync.Once
	configLoadError  error
)

// LoadTierConfig loads the subscription tier configuration from YAML file
// Returns cached instance on subsequent calls (singleton pattern)
func LoadTierConfig() (*TierConfig, error) {
	configOnce.Do(func() {
		globalTierConfig, configLoadError = loadTierConfigFromFile()
	})
	return globalTierConfig, configLoadError
}

// findConfigFile searches for a config file in multiple locations (current dir, parent dirs)
// Returns the first existing path found, or the original path if none exist
func findConfigFile(filename string) string {
	wd, _ := os.Getwd()
	testPaths := []string{filepath.Join(wd, filename), filename}

	// Try going up to 5 directory levels
	dir := wd
	for i := 0; i < 5; i++ {
		testPaths = append(testPaths, filepath.Join(dir, filename))
		dir = filepath.Dir(dir)
	}

	for _, path := range testPaths {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return filename // Return original if not found
}

// getConfigPath determines which config file to load based on environment
// Supports environment-specific configs for sandbox vs production Paddle IDs
//
// Priority:
// 1. SUBSCRIPTION_TIERS_CONFIG env var (explicit override)
// 2. config/subscription_tiers.{PADDLE_ENVIRONMENT}.yaml (e.g., sandbox, production)
// 3. config/subscription_tiers.yaml (default/template) — ONLY when PADDLE_ENVIRONMENT is unset
//
// Fails closed (returns an error) when PADDLE_ENVIRONMENT IS set but no matching
// config/subscription_tiers.<env>.yaml exists. This used to warn and silently fall back to
// the default/template catalogue — which can carry sandbox Paddle price IDs — meaning a
// production deploy with a typo'd PADDLE_ENVIRONMENT, or one where the environment-specific
// file was never created, could boot serving the wrong catalogue with no failure at all.
func getConfigPath() (string, error) {
	// Check for explicit config path override
	if explicitPath := getEnv("SUBSCRIPTION_TIERS_CONFIG", ""); explicitPath != "" {
		return explicitPath, nil
	}

	// Check for environment-specific config based on PADDLE_ENVIRONMENT
	if paddleEnv := getEnv("PADDLE_ENVIRONMENT", ""); paddleEnv != "" {
		envSpecificPath := fmt.Sprintf("config/subscription_tiers.%s.yaml", paddleEnv)
		found := findConfigFile(envSpecificPath)
		if found != envSpecificPath {
			fmt.Printf("Loading environment-specific config: %s\n", found)
			return found, nil
		}
		return "", fmt.Errorf(
			"PADDLE_ENVIRONMENT=%s requires %s, but it was not found. Refusing to silently fall back to the default/template catalogue (config/subscription_tiers.yaml), which may carry sandbox Paddle price IDs. Create the environment-specific file, or set SUBSCRIPTION_TIERS_CONFIG explicitly if this is intentional",
			paddleEnv, envSpecificPath)
	}

	// PADDLE_ENVIRONMENT unset: fall back to default config. This is the one case where
	// "no environment-specific file" is expected, not a misconfiguration.
	return "config/subscription_tiers.yaml", nil
}

// loadTierConfigFromFile reads and parses the subscription tiers YAML file
// Automatically selects environment-specific config based on PADDLE_ENVIRONMENT
func loadTierConfigFromFile() (*TierConfig, error) {
	configPath, err := getConfigPath()
	if err != nil {
		return nil, err
	}

	// Make path absolute if it's relative
	if !filepath.IsAbs(configPath) {
		configPath = findConfigFile(configPath)
		wd, _ := os.Getwd()
		if _, err := os.Stat(configPath); err != nil {
			return nil, fmt.Errorf("config file not found at %s (searched from %s)", configPath, wd)
		}
	}

	// Read and parse YAML file
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read tier config file %s: %w", configPath, err)
	}

	// Use TierConfig struct directly to avoid duplication
	config := &TierConfig{Tiers: make(map[domain.TierName]domain.SubscriptionTier)}

	// Temporary struct for parsing tiers with string keys
	var raw struct {
		Tiers       map[string]domain.SubscriptionTier `yaml:"tiers"`
		DefaultTier string                              `yaml:"default_tier"`
		Trial       TrialConfig                         `yaml:"trial"`
	}

	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse tier config YAML: %w", err)
	}

	// Convert tier keys to TierName type and populate
	config.DefaultTier = domain.TierName(raw.DefaultTier)
	config.Trial = raw.Trial

	for name, tier := range raw.Tiers {
		tier.Name = domain.TierName(name)
		config.Tiers[domain.TierName(name)] = tier
	}

	// Validate configuration
	if err := config.validate(); err != nil {
		return nil, fmt.Errorf("tier config validation failed: %w", err)
	}

	return config, nil
}

// canonicalTierUsersIncluded is the canonical tier name set (backend/internal/domain/subscription.go)
// mapped to the household seat count each tier's name advertises. Every subscription tier
// catalogue (config/subscription_tiers*.yaml) must define exactly this key set — validate()
// previously only checked len(tc.Tiers) == 5, which a catalogue with 5 WRONG tier names (e.g. a
// typo'd "household_6" swapped in for "household_5") would still have passed.
var canonicalTierUsersIncluded = map[domain.TierName]int{
	domain.TierPremium:    1,
	domain.TierHousehold2: 2,
	domain.TierHousehold3: 3,
	domain.TierHousehold4: 4,
	domain.TierHousehold5: 5,
}

// canonicalTierNamesSorted returns the canonical tier names in a stable, human-readable order
// for error messages.
func canonicalTierNamesSorted() []string {
	return []string{
		string(domain.TierPremium),
		string(domain.TierHousehold2),
		string(domain.TierHousehold3),
		string(domain.TierHousehold4),
		string(domain.TierHousehold5),
	}
}

// validate checks that the tier configuration is valid
func (tc *TierConfig) validate() error {
	if len(tc.Tiers) == 0 {
		return fmt.Errorf("no tiers defined in configuration")
	}

	if len(tc.Tiers) != len(canonicalTierUsersIncluded) {
		return fmt.Errorf("expected %d tiers (%v), got %d", len(canonicalTierUsersIncluded), canonicalTierNamesSorted(), len(tc.Tiers))
	}

	// Assert the EXACT canonical key set, not just the count: a catalogue with the right
	// number of tiers but a wrong/misspelled name would previously slip past the count-only
	// check with no other guard catching it before runtime lookups (e.g. GetTier) failed.
	for name := range tc.Tiers {
		if _, canonical := canonicalTierUsersIncluded[name]; !canonical {
			return fmt.Errorf("tier '%s' is not one of the canonical tier names %v", name, canonicalTierNamesSorted())
		}
	}
	for _, name := range canonicalTierNamesSorted() {
		if _, exists := tc.Tiers[domain.TierName(name)]; !exists {
			return fmt.Errorf("canonical tier '%s' is missing from configuration (expected %v)", name, canonicalTierNamesSorted())
		}
	}

	// Check that default tier exists
	if _, exists := tc.Tiers[tc.DefaultTier]; !exists {
		return fmt.Errorf("default tier '%s' not found in tier definitions", tc.DefaultTier)
	}

	// Validate each tier
	for name, tier := range tc.Tiers {
		if tier.DisplayName == "" {
			return fmt.Errorf("tier '%s' missing display name", name)
		}

		// Validate the only configurable limit is non-negative.
		// After simplification, only transaction_history_days is configurable.
		if tier.Limits.TransactionHistoryDays < 0 {
			return fmt.Errorf("tier '%s' has invalid transaction_history_days: %d", name, tier.Limits.TransactionHistoryDays)
		}

		// Advertised prices must be positive — a zero or negative price_monthly/yearly (or
		// mobile_price_yearly) would either give the tier away for free or reject valid
		// payments, and neither should ever reach production silently.
		if tier.Pricing.PriceMonthly <= 0 {
			return fmt.Errorf("tier '%s' has invalid price_monthly: %v (must be > 0)", name, tier.Pricing.PriceMonthly)
		}
		if tier.Pricing.PriceYearly <= 0 {
			return fmt.Errorf("tier '%s' has invalid price_yearly: %v (must be > 0)", name, tier.Pricing.PriceYearly)
		}
		if tier.Pricing.MobilePriceYearly <= 0 {
			return fmt.Errorf("tier '%s' has invalid mobile_price_yearly: %v (must be > 0)", name, tier.Pricing.MobilePriceYearly)
		}

		// users_included must match the tier's own name (household_3 must include 3 users,
		// etc.) — this is presentation-critical (mismatched seat counts are user-facing) and
		// has no legitimate reason to diverge from the tier's identity today.
		if expected, ok := canonicalTierUsersIncluded[name]; ok && tier.Pricing.UsersIncluded != expected {
			return fmt.Errorf("tier '%s' has users_included=%d, expected %d to match its tier name", name, tier.Pricing.UsersIncluded, expected)
		}
	}

	// Validate Trial config
	if err := tc.validateTrial(); err != nil {
		return err
	}

	return nil
}

// validateTrial checks that the trial configuration references a real tier with a sane duration.
func (tc *TierConfig) validateTrial() error {
	if tc.Trial.Tier == "" {
		return fmt.Errorf("trial.tier must be set")
	}
	if _, exists := tc.Tiers[tc.Trial.Tier]; !exists {
		return fmt.Errorf("trial.tier '%s' not found in tier definitions", tc.Trial.Tier)
	}
	if tc.Trial.DurationDays <= 0 {
		return fmt.Errorf("trial.duration_days must be > 0, got %d", tc.Trial.DurationDays)
	}
	return nil
}

// GetTier returns the tier configuration for a given tier name
// Thread-safe with read lock
func (tc *TierConfig) GetTier(tierName domain.TierName) (domain.SubscriptionTier, error) {
	tc.mu.RLock()
	defer tc.mu.RUnlock()

	tier, exists := tc.Tiers[tierName]
	if !exists {
		return domain.SubscriptionTier{}, fmt.Errorf("tier '%s' not found", tierName)
	}

	return tier, nil
}

// GetDefaultTier returns the default tier configuration
func (tc *TierConfig) GetDefaultTier() (domain.SubscriptionTier, error) {
	return tc.GetTier(tc.DefaultTier)
}

// ListTiers returns all available tiers
// Thread-safe with read lock
func (tc *TierConfig) ListTiers() []domain.SubscriptionTier {
	tc.mu.RLock()
	defer tc.mu.RUnlock()

	tiers := make([]domain.SubscriptionTier, 0, len(tc.Tiers))
	for _, tier := range tc.Tiers {
		tiers = append(tiers, tier)
	}

	return tiers
}

// GetTierNames returns all tier names
func (tc *TierConfig) GetTierNames() []domain.TierName {
	tc.mu.RLock()
	defer tc.mu.RUnlock()

	names := make([]domain.TierName, 0, len(tc.Tiers))
	for name := range tc.Tiers {
		names = append(names, name)
	}

	return names
}

// Reload reloads the tier configuration from disk
// Useful for configuration hot-reloading without restart
// Thread-safe with write lock
func (tc *TierConfig) Reload() error {
	newConfig, err := loadTierConfigFromFile()
	if err != nil {
		return err
	}

	tc.mu.Lock()
	defer tc.mu.Unlock()

	tc.Tiers = newConfig.Tiers
	tc.DefaultTier = newConfig.DefaultTier
	tc.Trial = newConfig.Trial

	return nil
}

// GetTrialConfig returns the trial configuration
// Issue #679: Trial service and auto-grant logic
func (tc *TierConfig) GetTrialConfig() TrialConfig {
	tc.mu.RLock()
	defer tc.mu.RUnlock()
	return tc.Trial
}

// GetTierByPriceID returns the tier name for a given Paddle price ID
// Searches both monthly and yearly price IDs across all tiers
// Thread-safe with read lock
// Issue #772: Webhook tier resolution (GAP 3)
func (tc *TierConfig) GetTierByPriceID(priceID string) (domain.TierName, error) {
	tc.mu.RLock()
	defer tc.mu.RUnlock()

	priceIDMatches := func(ptr *string) bool {
		return ptr != nil && *ptr == priceID
	}

	for name, tier := range tc.Tiers {
		if priceIDMatches(tier.Pricing.PaddlePriceIDMonthly) || priceIDMatches(tier.Pricing.PaddlePriceIDYearly) {
			return name, nil
		}
	}

	return "", fmt.Errorf("no tier found for price_id: %s", priceID)
}

// NewTierConfig creates a new TierConfig instance
// If configPath is empty, uses default path
func NewTierConfig(configPath string) (*TierConfig, error) {
	if configPath != "" {
		// Must not be ignored: if the override fails to stick, LoadTierConfig would
		// silently fall back to the default catalogue (possibly sandbox Paddle price IDs).
		//
		// ⚠ Two honest limits on that sentence, added in #1537 round 9 because the original
		// overstated what checking this error buys:
		//
		//  1. os.Setenv with a compile-time-constant name can only fail if the name contains '=' or
		//     NUL, so this branch is effectively unreachable. Checking it is correct but is hygiene,
		//     not a fix for anything observed.
		//  2. The REAL fail-open is larger and is not closed here: LoadTierConfig is sync.Once-gated
		//     (see configOnce above), so on any call after the first this env write is ignored
		//     entirely and the already-cached catalogue is returned regardless of configPath. A
		//     second caller passing a different path silently gets the first one's config. Every
		//     current caller passes "" or is a test, so nothing is broken today — but do not read the
		//     error check above as making this function's contract safe.
		if err := os.Setenv("SUBSCRIPTION_TIERS_CONFIG", configPath); err != nil {
			return nil, fmt.Errorf("set SUBSCRIPTION_TIERS_CONFIG=%s: %w", configPath, err)
		}
	}
	return LoadTierConfig()
}
