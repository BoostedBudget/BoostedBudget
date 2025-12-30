package middleware

import (
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/maxnussbaum/BoostedBudget/internal/config"
)

// AdminMiddleware creates middleware that restricts admin endpoints to QA environment only.
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
//   - qa: Allows request to proceed (200 OK with handler response)
//   - production: Returns 404 Not Found (hides endpoint existence)
//   - development: Returns 404 Not Found (qa-only endpoint)
//
// Future Enhancement (Issue #309): Add role-based access control to check for "admin" role
// in Clerk JWT custom claims. Current implementation only validates environment.
func AdminMiddleware(cfg *config.Config) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			// Environment check: Only allow qa
			// Returns 404 instead of 403 for security through obscurity
			// (prevents revealing endpoint existence to unauthorized users)
			if cfg.Environment != "qa" {
				return echo.NewHTTPError(http.StatusNotFound, "Endpoint not available")
			}

			// TODO (Issue #309): Add role-based auth check
			// if !HasRole(c, "admin") {
			//     return echo.NewHTTPError(http.StatusForbidden, "Admin access required")
			// }

			// All checks passed, proceed to handler
			return next(c)
		}
	}
}
