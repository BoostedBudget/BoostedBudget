package middleware

import (
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/maxnussbaum/BoostedBudget/internal/config"
)

// AdminMiddleware creates middleware that restricts admin endpoints to staging environment only.
//
// Security Strategy: Returns 404 Not Found in production instead of 403 Forbidden to hide
// endpoint existence (security through obscurity). This prevents attackers from discovering
// admin endpoints by probing for 403 responses.
//
// Usage:
//
//	admin := e.Group("/api/v1/admin")
//	admin.Use(middleware.AdminMiddleware(cfg))
//	admin.POST("/plaid/fire-webhook", adminHandler.FireWebhook)
//
// Environment Behavior:
//   - staging: Allows request to proceed (200 OK with handler response)
//   - test: Allows request to proceed (for E2E testing, Issue #331)
//   - production: Returns 404 Not Found (hides endpoint existence)
//   - development: Returns 404 Not Found (non-production admin endpoint)
//
// Future Enhancement (Issue #309): Add role-based access control to check for "admin" role
// in Auth0 JWT custom claims. Current implementation only validates environment.
func AdminMiddleware(cfg *config.Config) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			// DEBUG: Log environment value and comparison results (Issue #331)
			log.Printf("🔍 AdminMiddleware DEBUG:")
			log.Printf("  cfg.Environment = '%s'", cfg.Environment)
			log.Printf("  len(cfg.Environment) = %d", len(cfg.Environment))
			log.Printf("  cfg.Environment == \"test\" = %v", cfg.Environment == "test")
			log.Printf("  cfg.Environment == \"staging\" = %v", cfg.Environment == "staging")

			// Environment check: Only allow staging and test (Issue #331: E2E testing)
			// Returns 404 instead of 403 for security through obscurity
			// (prevents revealing endpoint existence to unauthorized users)
			if cfg.Environment != "staging" && cfg.Environment != "test" {
				log.Printf("❌ AdminMiddleware: BLOCKING request - environment '%s' not allowed", cfg.Environment)
				return echo.NewHTTPError(http.StatusNotFound, "Endpoint not available")
			}

			log.Println("✅ AdminMiddleware: Allowing request to proceed")

			// TODO (Issue #309): Add role-based auth check
			// if !HasRole(c, "admin") {
			//     return echo.NewHTTPError(http.StatusForbidden, "Admin access required")
			// }

			// All checks passed, proceed to handler
			return next(c)
		}
	}
}
