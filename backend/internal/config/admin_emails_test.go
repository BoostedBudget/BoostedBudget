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
//
// ADMIN_EMAILS_CONFIG is cleared deliberately: the loader honours it, so without this an
// ambient override pointing at a compliant fixture would let every "shipped ACL" assertion
// below pass while config/admin_emails.yaml itself had regressed.
func loadShippedAdminEmailConfig(t *testing.T) *domain.AdminEmailConfig {
	t.Helper()
	t.Setenv("ADMIN_EMAILS_CONFIG", "")
	ResetAdminEmailConfigForTesting()
	t.Cleanup(ResetAdminEmailConfigForTesting)
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

// RED 4b — the fail-safe as it is actually reached at startup, not as called directly.
//
// This is the test that matters, and its absence was a real defect: RED 4 above calls
// EffectiveAdminEmails("") directly, but `Config.Environment` is NEVER "". getEnv
// (config.go:262) maps both unset AND empty to the "development" default, and development
// is on the non-production allowlist — so an operator who deploys production and simply
// forgets to set ENVIRONMENT would have loaded both CI test accounts as production admins,
// which is precisely the failure requirement 1 exists to prevent.
//
// The fix is that resolving the allowlist requires an EXPLICITLY set ENVIRONMENT. This test
// goes through Config.Load() so it exercises the real startup path end to end.
func TestConfigLoad_UnsetEnvironmentFailsSafeForAdminAllowlist(t *testing.T) {
	adminCfg := loadShippedAdminEmailConfig(t)

	t.Run("ENVIRONMENT unset", func(t *testing.T) {
		// Cleared to "" rather than unset: getEnv treats the two identically
		// (config.go:262-267), and t.Setenv restores any ambient export afterwards.
		t.Setenv("ENVIRONMENT", "")

		cfg, err := Load()
		require.NoError(t, err)
		// The effective environment still defaults to development — that behaviour is
		// relied on elsewhere and is not changed here.
		require.Equal(t, "development", cfg.Environment)
		// But the ADMIN ALLOWLIST must not trust a defaulted value.
		require.False(t, cfg.EnvironmentExplicitlySet,
			"an unset ENVIRONMENT must not count as explicitly configured")

		effective := adminCfg.EffectiveAdminEmails(cfg.AdminAllowlistEnvironment())
		for _, email := range testAccountAdminEmails {
			assert.NotContains(t, effective, email,
				"a deployment that forgets ENVIRONMENT must not grant admin to %s", email)
		}
		for _, email := range ownerAdminEmails {
			assert.Contains(t, effective, email,
				"failing safe must not lock owner %s out", email)
		}
	})

	// The sibling case, through Load() as well: an EXPLICIT non-production declaration still
	// works, so local dev and the E2E suite are unaffected. Without this, the assertions
	// above would also pass if AdminAllowlistEnvironment simply always returned "".
	//
	// Only development and qa go through Load() here — Validate() demands a full secret set
	// for staging and production (DATABASE_URL, CLERK_SECRET_KEY, ENCRYPTION_KEY,
	// CF_ORIGIN_SECRET, …), and stubbing all of them would test the validator, not this.
	// Those two names are covered by the table below instead.
	for _, env := range []string{"development", "qa"} {
		t.Run("ENVIRONMENT="+env, func(t *testing.T) {
			t.Setenv("ENVIRONMENT", env)

			cfg, err := Load()
			require.NoError(t, err)
			require.True(t, cfg.EnvironmentExplicitlySet)
			require.Equal(t, env, cfg.AdminAllowlistEnvironment())

			effective := adminCfg.EffectiveAdminEmails(cfg.AdminAllowlistEnvironment())
			for _, email := range testAccountAdminEmails {
				assert.Contains(t, effective, email,
					"an explicit %s must still grant admin to %s", env, email)
			}
		})
	}

	// AdminAllowlistEnvironment across every declared environment, plus the defaulted case.
	for _, tc := range []struct {
		name             string
		cfg              *Config
		wantAllowlistEnv string
		wantTestAdmins   bool
	}{
		{"explicit development", &Config{Environment: "development", EnvironmentExplicitlySet: true}, "development", true},
		{"explicit qa", &Config{Environment: "qa", EnvironmentExplicitlySet: true}, "qa", true},
		{"explicit staging", &Config{Environment: "staging", EnvironmentExplicitlySet: true}, "staging", true},
		{"explicit production", &Config{Environment: "production", EnvironmentExplicitlySet: true}, "production", false},
		// The defaulted case: Environment says development but nobody set ENVIRONMENT.
		{"defaulted development", &Config{Environment: "development", EnvironmentExplicitlySet: false}, "", false},
		{"nil config", nil, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.wantAllowlistEnv, tc.cfg.AdminAllowlistEnvironment())

			effective := adminCfg.EffectiveAdminEmails(tc.cfg.AdminAllowlistEnvironment())
			for _, email := range testAccountAdminEmails {
				if tc.wantTestAdmins {
					assert.Contains(t, effective, email)
				} else {
					assert.NotContains(t, effective, email)
				}
			}
			// Owners are admins in every one of these cases, without exception.
			for _, email := range ownerAdminEmails {
				assert.Contains(t, effective, email)
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
