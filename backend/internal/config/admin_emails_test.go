package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Issue #1498: config/admin_emails.yaml grants admin access by email, bypassing the
// database/Clerk role check entirely (internal/huma/middleware/admin.go, the
// IsHardcodedAdmin branch returns before HasRole is consulted). Since #1499 removed the
// admin middleware's environment gate, that file is a live PRODUCTION ACL — and two of
// its entries were CI/QA test accounts.
//
// These tests exercise the SHIPPED config file, not a fixture, because the exposure was in
// the data as much as in the code. They are deliberately four assertions, not one:
// asserting only "the test accounts are absent in production" would also pass if config
// loading broke entirely and every list came back empty.

// ownerAdminEmails are the platform-owner addresses that must hold admin access in every
// environment, production included.
var ownerAdminEmails = []string{
	"mbnuss27@gmail.com",
	"maxnussbaum@live.com",
	"yingyuemaggieliu@gmail.com",
}

// testAccountAdminEmails are the CI/QA accounts that must NOT be admins in production.
var testAccountAdminEmails = []string{
	"e2e+clerk_test@boostedbudget.com",
	"qa-admin@boostedbudget.com",
}

// loadShippedAdminEmailConfig loads the real config/admin_emails.yaml from the repository.
func loadShippedAdminEmailConfig(t *testing.T) *domain.AdminEmailConfig {
	t.Helper()
	ResetAdminEmailConfigForTesting()
	cfg, err := LoadAdminEmailConfig()
	require.NoError(t, err, "the shipped admin_emails.yaml must parse")
	require.NotNil(t, cfg)
	return cfg
}

// RED 1: in production, neither test account resolves to an admin.
func TestEffectiveAdminEmails_ProductionExcludesTestAccounts(t *testing.T) {
	cfg := loadShippedAdminEmailConfig(t)

	effective := cfg.EffectiveAdminEmails("production")

	for _, email := range testAccountAdminEmails {
		assert.NotContains(t, effective, email,
			"%s is a test account and must not be a production admin", email)
	}
}

// RED 2: in production, the owner accounts DO resolve to admins. Without this, RED 1 would
// pass for the wrong reason (config loading broken, empty list).
func TestEffectiveAdminEmails_ProductionIncludesOwners(t *testing.T) {
	cfg := loadShippedAdminEmailConfig(t)

	effective := cfg.EffectiveAdminEmails("production")

	for _, email := range ownerAdminEmails {
		assert.Contains(t, effective, email,
			"%s is a platform owner and must keep production admin access", email)
	}
	assert.Len(t, effective, len(ownerAdminEmails),
		"production must resolve to exactly the owner addresses")
}

// RED 3: in the non-production environments the test accounts ARE admins — this is what
// keeps the E2E suite (which runs the backend with ENVIRONMENT=qa, see
// apps/web/playwright.config.ts) and the QA admin user working.
func TestEffectiveAdminEmails_NonProductionIncludesTestAccounts(t *testing.T) {
	cfg := loadShippedAdminEmailConfig(t)

	// Every environment name this codebase actually produces for ENVIRONMENT, other than
	// production: the config default (development), docker-compose.qa.yml + deploy-qa.yml +
	// playwright.config.ts (qa), and backend/fly.staging.toml (staging).
	for _, env := range []string{"development", "qa", "staging"} {
		t.Run(env, func(t *testing.T) {
			effective := cfg.EffectiveAdminEmails(env)
			for _, email := range testAccountAdminEmails {
				assert.Contains(t, effective, email,
					"%s must be an admin in %s", email, env)
			}
			for _, email := range ownerAdminEmails {
				assert.Contains(t, effective, email,
					"%s must be an admin in %s too", email, env)
			}
		})
	}
}

// RED 4: the fail-safe. An unset, misspelled, or otherwise unrecognised ENVIRONMENT must
// get the PRODUCTION treatment. This is why the predicate is an allowlist of known
// non-production names rather than `if env == "production"`.
func TestEffectiveAdminEmails_UnrecognisedEnvironmentFailsSafe(t *testing.T) {
	cfg := loadShippedAdminEmailConfig(t)

	for _, env := range []string{
		"",            // ENVIRONMENT unset and no default applied
		"   ",         // whitespace only
		"prod",        // plausible abbreviation that is not a known name
		"produciton",  // misspelling of production
		"stagng",      // misspelling of staging
		"q a",         // mangled qa
		"preprod",     // an environment nobody has declared
		"unknown-env", // anything else
	} {
		t.Run("env="+env, func(t *testing.T) {
			effective := cfg.EffectiveAdminEmails(env)
			for _, email := range testAccountAdminEmails {
				assert.NotContains(t, effective, email,
					"unrecognised environment %q must not load test admin %s", env, email)
			}
			// Owners still work — failing safe must not lock the platform owners out.
			for _, email := range ownerAdminEmails {
				assert.Contains(t, effective, email,
					"unrecognised environment %q must still grant owner %s", env, email)
			}
		})
	}
}

// The ADMIN_EMAILS_CONFIG override relocates the file; nothing currently sets it, so it is
// easy to break silently. This also pins that BOTH yaml keys are parsed.
func TestLoadAdminEmailConfig_HonoursADMIN_EMAILS_CONFIGAndParsesBothLists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "admin_emails.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`admin_emails:
  - "Owner@Example.com  "
non_production_admin_emails:
  - " Tester@Example.com"
`), 0o600))

	t.Setenv("ADMIN_EMAILS_CONFIG", path)
	ResetAdminEmailConfigForTesting()
	t.Cleanup(ResetAdminEmailConfigForTesting)

	cfg, err := LoadAdminEmailConfig()
	require.NoError(t, err)

	// Both lists are normalised to lowercase and trimmed, as the file's own comment promises.
	assert.Equal(t, []string{"owner@example.com"}, cfg.AdminEmails)
	assert.Equal(t, []string{"tester@example.com"}, cfg.NonProductionAdminEmails)

	assert.Equal(t, []string{"owner@example.com"}, cfg.EffectiveAdminEmails("production"))
	assert.ElementsMatch(t, []string{"owner@example.com", "tester@example.com"},
		cfg.EffectiveAdminEmails("qa"))
}
