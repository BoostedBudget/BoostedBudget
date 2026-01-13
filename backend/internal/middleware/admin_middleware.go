package middleware

import (
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/maxnussbaum/BoostedBudget/internal/config"
)

// AdminMiddleware creates middleware that restricts admin endpoints to QA environment only.
//
// Deprecated: Use humamiddleware.NewAdminMiddleware for Huma routes instead.
// This Echo middleware is kept for backward compatibility but should not be used for new code.
// Issue #309 implemented role-based access control in the Huma middleware.
//
// Security Strategy: Returns 404 Not Found in production instead of 403 Forbidden to hide
// endpoint existence (security through obscurity). This prevents attackers from discovering
// admin endpoints by probing for 403 responses.
//
// Usage (DEPRECATED - use Huma middleware instead):
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
// Note: This middleware does NOT perform role-based access control.
// For admin routes, use the Huma AdminMiddleware which validates admin role via Clerk privateMetadata.
func AdminMiddleware(cfg *config.Config) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			// Environment check: Only allow qa
			// Returns 404 instead of 403 for security through obscurity
			// (prevents revealing endpoint existence to unauthorized users)
			if cfg.Environment != "qa" {
				return echo.NewHTTPError(http.StatusNotFound, "Endpoint not available")
			}

			// Note: Role-based auth is NOT implemented here.
			// Use the Huma AdminMiddleware for admin routes (Issue #309).

			// All checks passed, proceed to handler
			return next(c)
		}
	}
}
