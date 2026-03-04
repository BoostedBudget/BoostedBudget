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
// 3. config/subscription_tiers.yaml (default/template)
func getConfigPath() string {
	// Check for explicit config path override
	if explicitPath := getEnv("SUBSCRIPTION_TIERS_CONFIG", ""); explicitPath != "" {
		return explicitPath
	}

	// Check for environment-specific config based on PADDLE_ENVIRONMENT
	if paddleEnv := getEnv("PADDLE_ENVIRONMENT", ""); paddleEnv != "" {
		envSpecificPath := fmt.Sprintf("config/subscription_tiers.%s.yaml", paddleEnv)
		found := findConfigFile(envSpecificPath)
		if found != envSpecificPath {
			fmt.Printf("Loading environment-specific config: %s\n", found)
			return found
		}
		fmt.Printf("Warning: PADDLE_ENVIRONMENT=%s but %s not found, using default config\n",
			paddleEnv, envSpecificPath)
	}

	// Fall back to default config
	return "config/subscription_tiers.yaml"
}

// loadTierConfigFromFile reads and parses the subscription tiers YAML file
// Automatically selects environment-specific config based on PADDLE_ENVIRONMENT
func loadTierConfigFromFile() (*TierConfig, error) {
	configPath := getConfigPath()

	// Make path absolute if it's relative
	if !filepath.IsAbs(configPath) {
		configPath = findConfigFile(configPath)
		wd, _ := os.Getwd()
		if _, err := os.Stat(configPath); err != nil {
			return nil, fmt.Errorf("config file not found at %s (searched from %s)", configPath, wd)
		}
	}

	// Read the YAML file
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read tier config file %s: %w", configPath, err)
	}

	// Parse YAML structure
	var rawConfig struct {
		Tiers       map[string]domain.SubscriptionTier `yaml:"tiers"`
		DefaultTier string                              `yaml:"default_tier"`
		Trial       TrialConfig                         `yaml:"trial"`
	}

	if err := yaml.Unmarshal(data, &rawConfig); err != nil {
		return nil, fmt.Errorf("failed to parse tier config YAML: %w", err)
	}

	// Convert map keys to TierName type and populate tier names
	config := &TierConfig{
		Tiers:       make(map[domain.TierName]domain.SubscriptionTier),
		DefaultTier: domain.TierName(rawConfig.DefaultTier),
		Trial:       rawConfig.Trial,
	}

	for name, tier := range rawConfig.Tiers {
		tier.Name = domain.TierName(name)
		config.Tiers[domain.TierName(name)] = tier
	}

	// Validate configuration
	if err := config.validate(); err != nil {
		return nil, fmt.Errorf("tier config validation failed: %w", err)
	}

	return config, nil
}

// validate checks that the tier configuration is valid
func (tc *TierConfig) validate() error {
	if len(tc.Tiers) == 0 {
		return fmt.Errorf("no tiers defined in configuration")
	}

	// Expect 5 tiers: premium, household_2, household_3, household_4, household_5
	if len(tc.Tiers) != 5 {
		return fmt.Errorf("expected 5 tiers (premium + 4 household tiers), got %d", len(tc.Tiers))
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

		// Validate the 3 configurable limits are non-negative (except unlimited = -1)
		// After simplification, only accounts, transaction_history_days, and executions_per_day are configurable
		if tier.Limits.Accounts < -1 {
			return fmt.Errorf("tier '%s' has invalid accounts limit: %d", name, tier.Limits.Accounts)
		}
		if tier.Limits.TransactionHistoryDays < 0 {
			return fmt.Errorf("tier '%s' has invalid transaction_history_days: %d", name, tier.Limits.TransactionHistoryDays)
		}
		if tier.Limits.ExecutionsPerDay < -1 {
			return fmt.Errorf("tier '%s' has invalid executions_per_day: %d", name, tier.Limits.ExecutionsPerDay)
		}
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

	for name, tier := range tc.Tiers {
		// Check monthly price ID
		if tier.Pricing.PaddlePriceIDMonthly != nil && *tier.Pricing.PaddlePriceIDMonthly == priceID {
			return name, nil
		}
		// Check yearly price ID
		if tier.Pricing.PaddlePriceIDYearly != nil && *tier.Pricing.PaddlePriceIDYearly == priceID {
			return name, nil
		}
	}

	return "", fmt.Errorf("no tier found for price_id: %s", priceID)
}

// NewTierConfig creates a new TierConfig instance
// If configPath is empty, uses default path
func NewTierConfig(configPath string) (*TierConfig, error) {
	if configPath != "" {
		os.Setenv("SUBSCRIPTION_TIERS_CONFIG", configPath)
	}
	return LoadTierConfig()
}
