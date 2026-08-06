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

// defaultAdminEmailsConfigPath is the committed, code-reviewed admin ACL that ships inside
// the runtime image (backend/Dockerfile copies config/).
const defaultAdminEmailsConfigPath = "config/admin_emails.yaml"

// AdminEmailsConfigOverride reports the ADMIN_EMAILS_CONFIG value present in the process
// environment, and whether it is honoured when the allowlist resolves against allowlistEnv.
//
// Issue #1498: the override is honoured ONLY in a recognised non-production environment. The
// allowlist bypasses the role check entirely (middleware/admin.go returns from the
// IsHardcodedAdmin branch before HasRole is consulted), so an honoured override is an
// arbitrary, role-check-free set of admins. Its only real purpose is relocating the file for
// tests, which pass a non-production allowlistEnv to LoadAdminEmailConfig directly rather than
// going through ENVIRONMENT.
//
// "Is this production?" is answered by domain.IsNonProductionEnvironment(allowlistEnv) — the
// SAME predicate that decides which of the file's two lists load, and allowlistEnv is always
// Config.AdminAllowlistEnvironment(), which returns "" unless ENVIRONMENT was explicitly set.
// Two disagreeing notions of "production" in one authorization path is how this class of bug
// survives; do not add a second test here.
//
// The caller is expected to log a rejected override loudly (main.go does, at Error): silently
// reading a different file than the operator asked for would be its own trap.
func AdminEmailsConfigOverride(allowlistEnv string) (override string, honoured bool) {
	override = os.Getenv("ADMIN_EMAILS_CONFIG")
	return override, override != "" && domain.IsNonProductionEnvironment(allowlistEnv)
}

// adminEmailsConfigPath resolves which file supplies the admin allowlist. Outside a
// recognised non-production environment this is always the committed file.
func adminEmailsConfigPath(allowlistEnv string) string {
	if override, honoured := AdminEmailsConfigOverride(allowlistEnv); honoured {
		return override
	}
	return defaultAdminEmailsConfigPath
}

// LoadAdminEmailConfig loads the admin email configuration from YAML file.
// Returns cached instance on subsequent calls (singleton pattern).
// Returns an empty config (no admin emails) if file not found (graceful degradation).
//
// allowlistEnv is the environment the allowlist resolves against — always
// Config.AdminAllowlistEnvironment(), never Config.Environment. It decides whether the
// ADMIN_EMAILS_CONFIG relocation override is honoured; see adminEmailsConfigPath.
//
// ⚠️ allowlistEnv is only consulted on the FIRST call, because the result is cached by
// sync.Once. Today there is exactly one production caller (cmd/server/main.go), so that is
// safe. If a second call site is ever added, a caller passing a different allowlistEnv will
// silently receive the file the first caller selected — including, in the worst ordering, an
// override that would have been refused for the second caller's environment. Add a second
// caller only alongside either an explicit mismatch check here or the removal of the
// singleton. Tests must call ResetAdminEmailConfigForTesting between environments.
func LoadAdminEmailConfig(allowlistEnv string) (*domain.AdminEmailConfig, error) {
	adminConfigOnce.Do(func() {
		globalAdminEmailConfig, adminConfigLoadError = loadAdminEmailConfigFromFile(allowlistEnv)
	})
	return globalAdminEmailConfig, adminConfigLoadError
}

// loadAdminEmailConfigFromFile reads and parses the admin emails YAML file.
func loadAdminEmailConfigFromFile(allowlistEnv string) (*domain.AdminEmailConfig, error) {
	configPath := adminEmailsConfigPath(allowlistEnv)

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

	// Normalize emails to lowercase for case-insensitive matching.
	// Both lists get the same treatment (Issue #1498) — a stray capital in the
	// non-production list must not silently disable a QA/E2E admin.
	for i, email := range config.AdminEmails {
		config.AdminEmails[i] = strings.ToLower(strings.TrimSpace(email))
	}
	for i, email := range config.NonProductionAdminEmails {
		config.NonProductionAdminEmails[i] = strings.ToLower(strings.TrimSpace(email))
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
