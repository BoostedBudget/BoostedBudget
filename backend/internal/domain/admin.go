// Package domain contains domain models and types for the BoostedBudget application.
package domain

import "strings"

// AdminRole represents a role that can be assigned to users for admin access.
type AdminRole string

const (
	// RoleAdmin is the admin role that grants access to admin endpoints.
	RoleAdmin AdminRole = "admin"
)

// AdminPrivateMetadata represents the structure of Clerk user privateMetadata
// that contains role information.
// Expected format in Clerk Dashboard: {"roles": ["admin", "support"]}
type AdminPrivateMetadata struct {
	Roles []string `json:"roles"`
}

// AdminEmailConfig holds the configuration for hardcoded admin emails.
// These emails are loaded from config/admin_emails.yaml and grant admin access
// regardless of Clerk privateMetadata settings.
//
// Issue #1498: this list bypasses the role check entirely, and since #1499 removed the
// admin middleware's environment gate it is a live PRODUCTION ACL. The list is therefore
// split in two so that CI/QA test accounts can never become production admins:
//
//   - AdminEmails grant admin access in EVERY environment, production included.
//   - NonProductionAdminEmails grant admin access only in a known non-production
//     environment (see IsNonProductionEnvironment).
//
// Resolve the two with EffectiveAdminEmails; never read AdminEmails directly when
// building the ACL.
type AdminEmailConfig struct {
	AdminEmails              []string `yaml:"admin_emails"`
	NonProductionAdminEmails []string `yaml:"non_production_admin_emails"`
}

// nonProductionEnvironments is the explicit allowlist of ENVIRONMENT values that are known
// to be non-production, and is the ONLY way the extra admin entries get loaded.
//
// It is deliberately an allowlist rather than `env != "production"`: an unset, misspelled,
// or newly-introduced ENVIRONMENT value must fail SAFE (production treatment, test
// accounts excluded) rather than silently granting admin access to CI credentials.
//
// The values are every non-production name this codebase actually produces:
//   - "development" — the default in Config.Load (getEnv("ENVIRONMENT", "development"))
//     and docker-compose.yml
//   - "qa" — docker-compose.qa.yml, .github/workflows/deploy-qa.yml, and the E2E backend
//     in apps/web/playwright.config.ts
//   - "staging" — backend/fly.staging.toml
//
// backend/fly.toml sets "production". Adding a new deployment environment means adding it
// here too; forgetting to costs test-admin access in that environment, never production
// exposure.
var nonProductionEnvironments = map[string]struct{}{
	"development": {},
	"qa":          {},
	"staging":     {},
}

// IsNonProductionEnvironment reports whether env is a recognised non-production
// environment. Anything unrecognised — including the empty string — returns false, i.e.
// gets the production treatment.
//
// Matching is case- and whitespace-insensitive so that a stray "  QA  " is still
// recognised as the qa environment; a value that is not a known name still fails safe.
func IsNonProductionEnvironment(env string) bool {
	_, ok := nonProductionEnvironments[strings.ToLower(strings.TrimSpace(env))]
	return ok
}

// EffectiveAdminEmails returns the hardcoded-admin ACL that applies in env.
//
// In a recognised non-production environment this is AdminEmails plus
// NonProductionAdminEmails. Everywhere else — production, and any unrecognised value — it
// is AdminEmails alone.
func (c *AdminEmailConfig) EffectiveAdminEmails(env string) []string {
	if c == nil {
		return []string{}
	}

	effective := make([]string, 0, len(c.AdminEmails)+len(c.NonProductionAdminEmails))
	effective = append(effective, c.AdminEmails...)
	if IsNonProductionEnvironment(env) {
		effective = append(effective, c.NonProductionAdminEmails...)
	}
	return effective
}

// CachedRole stores cached role information for a Clerk user.
// Used by AdminService to avoid repeated Clerk API calls.
type CachedRole struct {
	ClerkUserID string
	Roles       []AdminRole
	CachedAt    int64 // Unix timestamp in seconds
}
