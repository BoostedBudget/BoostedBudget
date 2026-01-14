package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"gopkg.in/yaml.v3"
)

// Global admin email config instance (singleton)
var (
	globalAdminEmailConfig *domain.AdminEmailConfig
	adminConfigOnce        sync.Once
	adminConfigLoadError   error
)

// LoadAdminEmailConfig loads the admin email configuration from YAML file.
// Returns cached instance on subsequent calls (singleton pattern).
// Returns an empty config (no admin emails) if file not found (graceful degradation).
func LoadAdminEmailConfig() (*domain.AdminEmailConfig, error) {
	adminConfigOnce.Do(func() {
		globalAdminEmailConfig, adminConfigLoadError = loadAdminEmailConfigFromFile()
	})
	return globalAdminEmailConfig, adminConfigLoadError
}

// loadAdminEmailConfigFromFile reads and parses the admin emails YAML file.
func loadAdminEmailConfigFromFile() (*domain.AdminEmailConfig, error) {
	// Get config file path from environment or use default
	configPath := getEnv("ADMIN_EMAILS_CONFIG", "config/admin_emails.yaml")

	// Make path absolute if it's relative
	if !filepath.IsAbs(configPath) {
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
				// Graceful degradation: return empty config if file not found
				return &domain.AdminEmailConfig{
					AdminEmails: []string{},
				}, nil
			}
		}
	}

	// Read the YAML file
	data, err := os.ReadFile(configPath)
	if err != nil {
		// Graceful degradation: return empty config if file not found
		if os.IsNotExist(err) {
			return &domain.AdminEmailConfig{
				AdminEmails: []string{},
			}, nil
		}
		return nil, fmt.Errorf("failed to read admin emails config file %s: %w", configPath, err)
	}

	// Parse YAML
	var config domain.AdminEmailConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse admin emails config YAML: %w", err)
	}

	// Normalize emails to lowercase for case-insensitive matching
	for i, email := range config.AdminEmails {
		config.AdminEmails[i] = strings.ToLower(strings.TrimSpace(email))
	}

	return &config, nil
}

// ResetAdminEmailConfigForTesting resets the singleton for testing purposes.
// This should only be used in tests.
func ResetAdminEmailConfigForTesting() {
	adminConfigOnce = sync.Once{}
	globalAdminEmailConfig = nil
	adminConfigLoadError = nil
}
