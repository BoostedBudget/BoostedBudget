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
// Loaded from backend/config/subscription_tiers.yaml
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

// loadTierConfigFromFile reads and parses the subscription tiers YAML file
func loadTierConfigFromFile() (*TierConfig, error) {
	// Get config file path from environment or use default
	configPath := getEnv("SUBSCRIPTION_TIERS_CONFIG", "config/subscription_tiers.yaml")

	// Make path absolute if it's relative
	if !filepath.IsAbs(configPath) {
		// Try multiple paths to find the config file
		// 1. Relative to current working directory
		// 2. Relative to project root (go up to find it)
		wd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("failed to get working directory: %w", err)
		}

		// Try current directory first
		if _, err := os.Stat(filepath.Join(wd, configPath)); err == nil {
			configPath = filepath.Join(wd, configPath)
		} else {
			// Try going up directories to find project root
			dir := wd
			found := false
			for i := 0; i < 5; i++ { // Try up to 5 levels up
				testPath := filepath.Join(dir, configPath)
				if _, err := os.Stat(testPath); err == nil {
					configPath = testPath
					found = true
					break
				}
				dir = filepath.Dir(dir)
			}
			if !found {
				return nil, fmt.Errorf("config file not found at %s (searched from %s)", configPath, wd)
			}
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

	// Check that default tier exists
	if _, exists := tc.Tiers[tc.DefaultTier]; !exists {
		return fmt.Errorf("default tier '%s' not found in tier definitions", tc.DefaultTier)
	}

	// Validate each tier
	for name, tier := range tc.Tiers {
		if tier.DisplayName == "" {
			return fmt.Errorf("tier '%s' missing display name", name)
		}

		// Validate limits are non-negative (except unlimited = -1)
		if tier.Limits.Accounts < -1 {
			return fmt.Errorf("tier '%s' has invalid accounts limit: %d", name, tier.Limits.Accounts)
		}
		if tier.Limits.Rules < -1 {
			return fmt.Errorf("tier '%s' has invalid rules limit: %d", name, tier.Limits.Rules)
		}
		if tier.Limits.ExecutionsPerDay < 0 {
			return fmt.Errorf("tier '%s' has invalid executions_per_day: %d", name, tier.Limits.ExecutionsPerDay)
		}
		if tier.Limits.MinExecutionIntervalMinutes < 0 {
			return fmt.Errorf("tier '%s' has invalid min_execution_interval_minutes: %d", name, tier.Limits.MinExecutionIntervalMinutes)
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
