package middleware

import (
	"fmt"
	"net/http"
	"strings"

	echomiddleware "github.com/labstack/echo/v4/middleware"
)

// corsPermissiveEnvironments is the explicit allowlist of ENVIRONMENT values in which a
// wildcard/empty CORS_ALLOWED_ORIGINS is tolerated. Deliberately an allowlist rather than a
// blocklist of "production": an unset, misspelled, or newly-introduced ENVIRONMENT value must
// fail SAFE (no wildcard CORS) rather than silently falling through a switch's default case,
// matching the fail-safe allowlist pattern in
// backend/internal/config/qa_auth_bypass_gate.go and domain.IsNonProductionEnvironment
// (issue #1514, F10).
//
//   - "development" is the local dev default (config.Load's getEnv("ENVIRONMENT",
//     "development")) and docker-compose.yml — needed so `make dev` keeps working without
//     CORS_ALLOWED_ORIGINS set.
//   - "qa" is the deployed QA environment (docker-compose.qa.yml,
//     .github/workflows/deploy-qa.yml, and the Playwright E2E backend in
//     apps/web/playwright.config.ts) — needed so the E2E/QA flows keep working.
//
// "staging" and "production" are deliberately NOT here: both fail closed on wildcard/empty
// origins. Staging used to only warn (the bug this fix closes) — a permissive staging config
// is exactly how a bad value gets copy-pasted into production.
var corsPermissiveEnvironments = map[string]struct{}{
	"development": {},
	"qa":          {},
}

// ValidateCORSConfig checks that the CORS configuration is safe for the given environment.
// Returns an error if wildcard/empty origins are used anywhere except the environments in
// corsPermissiveEnvironments (development, qa) — this includes production, staging, and any
// unrecognised/unset environment value.
// For non-wildcard origins, validates each is a valid http(s) URL.
func ValidateCORSConfig(allowedOrigins, environment string) error {
	isWildcard := allowedOrigins == "" || allowedOrigins == "*"

	if isWildcard {
		if _, ok := corsPermissiveEnvironments[environment]; !ok {
			return fmt.Errorf("CORS_ALLOWED_ORIGINS cannot be '*' or empty in environment %q: set explicit allowed origins", environment)
		}
		return nil
	}

	// Validate each non-wildcard origin is a valid URL
	for _, origin := range strings.Split(allowedOrigins, ",") {
		trimmed := strings.TrimSpace(origin)
		if trimmed == "" {
			continue
		}
		if !strings.HasPrefix(trimmed, "http://") && !strings.HasPrefix(trimmed, "https://") {
			return fmt.Errorf("invalid CORS origin %q: must start with http:// or https://", trimmed)
		}
	}
	return nil
}

// parseCORSAllowedOrigins is the single place that turns the CORS_ALLOWED_ORIGINS config
// string into an origin list. Both GetCORSConfig and CORSErrorHeaders (cors_error_headers.go)
// must use this so the two allowlists cannot drift (issue #1609): the header-only middleware
// registered before the Cloudflare/rate-limit gate needs the exact same allowlist semantics as
// the real echomiddleware.CORSWithConfig instance registered after it.
//
// If allowedOrigins is empty or "*", it returns []string{"*"} (dev/test only, wildcard).
// Otherwise it splits on commas and trims whitespace, dropping empty entries.
func parseCORSAllowedOrigins(allowedOrigins string) []string {
	if allowedOrigins == "" || allowedOrigins == "*" {
		return []string{"*"}
	}

	var origins []string
	for _, origin := range strings.Split(allowedOrigins, ",") {
		if trimmed := strings.TrimSpace(origin); trimmed != "" {
			origins = append(origins, trimmed)
		}
	}
	return origins
}

// GetCORSConfig returns an Echo CORS middleware configuration based on allowed origins
// If allowedOrigins is empty or "*", it allows all origins (dev/test only)
// For production, should be a comma-separated list of allowed origins
func GetCORSConfig(allowedOrigins string) echomiddleware.CORSConfig {
	return echomiddleware.CORSConfig{
		AllowOrigins:     parseCORSAllowedOrigins(allowedOrigins),
		AllowMethods:     []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions},
		AllowHeaders:     []string{"Authorization", "Content-Type", "X-Requested-With"},
		// ExposeHeaders makes Retry-After readable cross-origin. Without it a browser client
		// cannot read the Retry-After header IPRateLimiter sets on a 429 (rate_limit.go), even
		// once CORSErrorHeaders lets it read the response at all (issue #1609).
		ExposeHeaders:    []string{"Retry-After"},
		AllowCredentials: true,
		MaxAge:           86400,
	}
}
