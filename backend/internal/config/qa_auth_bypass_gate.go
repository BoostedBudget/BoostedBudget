package config

import (
	"strings"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// clerkLiveSecretKeyPrefix is Clerk's prefix for a PRODUCTION instance secret key. Clerk keys
// are sk_test_… for development/staging instances and sk_live_… for the production instance
// (docs/CLERK_SETUP.md; boosted-budget-fly-deployment.md gives staging sk_test_ and production
// sk_live_). This is not a label an operator types per deployment — it decides which real
// Clerk instance, holding which real users, the process talks to.
const clerkLiveSecretKeyPrefix = "sk_live_"

// paddleProductionEnvironment is the PADDLE_ENVIRONMENT value that selects the live Paddle
// catalogue and bills real money. backend/fly.toml sets "production"; backend/fly.staging.toml
// sets "sandbox"; the QA compose stack leaves it at the "sandbox" default.
const paddleProductionEnvironment = "production"

// QAAuthBypassRefusalReason reports whether the QA auth-bypass route
// (POST /api/v1/qa/auth/bypass) may be registered. It returns "" when registration is
// permitted, and otherwise a human-readable reason naming the specific setting that refused —
// suitable for logging so an operator can tell a deliberate refusal from a misconfiguration.
//
// Issue #1432 (batch ED, finding B2-1). This route is the ONLY unauthenticated path to admin
// authority in the codebase: it carries no Clerk security annotation, it sits outside
// /api/v1/admin so the admin middleware never sees it, and on role:"admin" it mints a
// five-minute Clerk sign-in token for QA_ADMIN_CLERK_USER_ID. Since #1499 removed the admin
// middleware's environment gate, whoever holds that token holds live production admin.
//
// Registration used to turn on ONE string — ENVIRONMENT == "qa" || "staging" — plus the three
// QA settings being non-empty. A production deployment mislabelled with exactly that string,
// or one whose secret store still carried QA values copied from staging, exposed the route.
//
// The gate is now a conjunction of four INDEPENDENT facts, in this order:
//
//  1. ENVIRONMENT was set explicitly. getEnv maps unset AND blank to "development", so
//     Environment alone cannot distinguish a dev box from a production deploy that forgot the
//     variable (same reasoning as AdminAllowlistEnvironment — see config.go).
//  2. ENVIRONMENT is on the two-value allowlist {qa, staging}
//     (domain.IsQAAuthBypassEnvironment).
//  3. QA_AUTH_BYPASS_ENABLED is true. An explicit opt-in that a production deployment never
//     carries, and that has to be set deliberately alongside the QA secrets.
//  4. No PRODUCTION-ONLY SIGNAL is present. Conditions 1-3 are all labels an operator sets, so
//     an operator who mislabels one may mislabel them all; these two are not labels. A Clerk
//     sk_live_ key means the sign-in token would be minted in the production Clerk instance
//     against real users, and PADDLE_ENVIRONMENT=production means the process is billing the
//     live catalogue. Either one means the deployment IS production whatever ENVIRONMENT says,
//     and the route must not exist.
//
// The three QA settings are still required, as before — an empty QA_BYPASS_SECRET would leave
// the handler comparing against "" and the service minting tokens for an empty Clerk user ID.
//
// Legitimate QA/staging use is unaffected: the only real consumer is the qa-login skill
// (.claude/skills/qa-login/SKILL.md), which curls this route against the QA and staging
// backends. The Playwright E2E suite does not use it — it authenticates through Clerk's test
// mode (apps/web/e2e/auth.setup.ts).
func (c *Config) QAAuthBypassRefusalReason() string {
	if c == nil {
		return "refusing the QA auth-bypass route: no configuration loaded"
	}

	if !c.EnvironmentExplicitlySet {
		return "refusing the QA auth-bypass route: ENVIRONMENT was not set explicitly, so the deployment cannot be distinguished from an unlabelled production one"
	}
	if !domain.IsQAAuthBypassEnvironment(c.Environment) {
		return "refusing the QA auth-bypass route: ENVIRONMENT=" + c.Environment + " is not a recognised QA-bypass environment (qa, staging)"
	}
	if !c.QAAuthBypassEnabled {
		return "refusing the QA auth-bypass route: QA_AUTH_BYPASS_ENABLED is not set to true"
	}

	// Production-only signals: checked AFTER the labels so the reason names the strongest
	// available evidence when several conditions fail at once.
	if strings.HasPrefix(strings.TrimSpace(c.ClerkSecretKey), clerkLiveSecretKeyPrefix) {
		return "refusing the QA auth-bypass route: CLERK_SECRET_KEY is a live production key (" + clerkLiveSecretKeyPrefix + "…), so a sign-in token would be minted against real users despite ENVIRONMENT=" + c.Environment
	}
	if strings.EqualFold(strings.TrimSpace(c.PaddleEnvironment), paddleProductionEnvironment) {
		return "refusing the QA auth-bypass route: PADDLE_ENVIRONMENT=production means this deployment bills the live catalogue despite ENVIRONMENT=" + c.Environment
	}

	if c.QABypassSecret == "" {
		return "refusing the QA auth-bypass route: QA_BYPASS_SECRET is empty"
	}
	if c.QAAdminClerkUserID == "" {
		return "refusing the QA auth-bypass route: QA_ADMIN_CLERK_USER_ID is empty"
	}
	if c.QAPremiumClerkUserID == "" {
		return "refusing the QA auth-bypass route: QA_PREMIUM_CLERK_USER_ID is empty"
	}

	return ""
}
