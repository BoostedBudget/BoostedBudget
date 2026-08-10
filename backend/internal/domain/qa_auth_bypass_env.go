package domain

import "strings"

// qaAuthBypassEnvironments is the explicit allowlist of ENVIRONMENT values in which the QA
// auth-bypass route (POST /api/v1/qa/auth/bypass) may exist at all.
//
// Issue #1432 (batch ED, finding B2-1). That route is the only UNAUTHENTICATED path to admin
// authority in this codebase: it carries no Clerk security annotation, it sits outside
// /api/v1/admin so the admin middleware (which keys purely on that path prefix) never sees it,
// and on role:"admin" it mints a five-minute Clerk sign-in token for QA_ADMIN_CLERK_USER_ID.
// Everything about how it is gated therefore has to fail SAFE.
//
// This is deliberately a SEPARATE, NARROWER allowlist than nonProductionEnvironments:
//
//   - IsNonProductionEnvironment also recognises "development", because the admin-email
//     allowlist legitimately loads its extra test entries on a developer's machine.
//   - This route needs pre-created Clerk QA users (QA_ADMIN_CLERK_USER_ID /
//     QA_PREMIUM_CLERK_USER_ID) and a shared secret, and only the two DEPLOYED non-production
//     environments have them: qa (docker-compose.qa.yml, .github/workflows/deploy-qa.yml) and
//     staging (backend/fly.staging.toml).
//
// Do not collapse the two functions — see the test that pins the relationship.
var qaAuthBypassEnvironments = map[string]struct{}{
	"qa":      {},
	"staging": {},
}

// IsQAAuthBypassEnvironment reports whether env is an environment in which the QA auth-bypass
// route may be served. Anything unrecognised — including the empty string, "development",
// "production", and any typo — returns false.
//
// It replaces the fail-OPEN `environment == "production"` guard the handler used to carry,
// under which every value that was not that exact lowercase string ("Production",
// " production ", "prod", "") fell through and minted a token. Matching here is case- and
// whitespace-insensitive so a stray "  QA  " is still recognised, while an unknown name still
// fails safe.
func IsQAAuthBypassEnvironment(env string) bool {
	_, ok := qaAuthBypassEnvironments[strings.ToLower(strings.TrimSpace(env))]
	return ok
}
