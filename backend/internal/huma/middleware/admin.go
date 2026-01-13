package middleware

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/clerk/clerk-sdk-go/v2"
	"github.com/danielgtaylor/huma/v2"
	"github.com/maxnussbaum/BoostedBudget/internal/auth"
	"github.com/maxnussbaum/BoostedBudget/internal/config"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
)

// NewAdminMiddleware creates a Huma middleware for admin authentication.
//
// Security Strategy:
//  1. Only applies to routes starting with /api/v1/admin
//  2. Environment check FIRST: Returns 404 in non-qa environments (hides endpoint existence)
//  3. Authentication check: Returns 401 if no Clerk user ID in context
//  4. Hardcoded email check: Allows access if email is in hardcoded admin list
//  5. Role check: Returns 403 if user doesn't have admin role in Clerk privateMetadata
//
// Parameters:
//   - cfg: Application config (for environment check)
//   - adminService: Service for role verification
func NewAdminMiddleware(
	cfg *config.Config,
	adminService services.AdminService,
) func(ctx huma.Context, next func(huma.Context)) {
	return func(ctx huma.Context, next func(huma.Context)) {
		// Only apply to admin routes
		path := ctx.Operation().Path
		if !strings.HasPrefix(path, "/api/v1/admin") {
			next(ctx)
			return
		}

		// 1. Environment check FIRST: Only allow qa environment
		// Returns 404 instead of 403 for security through obscurity
		// (prevents revealing endpoint existence to unauthorized users)
		if cfg.Environment != "qa" {
			writeAdminJSONError(ctx, http.StatusNotFound, "Endpoint not available")
			return
		}

		// 2. Get Clerk user ID from context (set by Clerk middleware)
		clerkUserID, ok := ctx.Context().Value(inputs.ClerkUserIDKey).(string)
		if !ok || clerkUserID == "" {
			writeAdminJSONError(ctx, http.StatusUnauthorized, "User not authenticated")
			return
		}

		// 3. Get email from Clerk claims for hardcoded admin check
		email := extractEmailFromContext(ctx)

		// 4. Check hardcoded admin list first (bypasses Clerk API call)
		if email != "" && adminService.IsHardcodedAdmin(email) {
			next(ctx)
			return
		}

		// 5. Check admin role via Clerk privateMetadata
		hasAdminRole, err := adminService.HasRole(ctx.Context(), clerkUserID, domain.RoleAdmin)
		if err != nil {
			writeAdminJSONError(ctx, http.StatusInternalServerError, "Failed to verify admin role")
			return
		}

		if !hasAdminRole {
			writeAdminJSONError(ctx, http.StatusForbidden, "Admin access required")
			return
		}

		// All checks passed, proceed to handler
		next(ctx)
	}
}

// extractEmailFromContext extracts the user's email from Clerk session claims.
func extractEmailFromContext(ctx huma.Context) string {
	claims, ok := ctx.Context().Value(inputs.ClerkClaimsKey).(*clerk.SessionClaims)
	if !ok || claims == nil {
		return ""
	}
	return auth.ExtractEmailFromClaims(claims)
}

// writeAdminJSONError writes an RFC 9457 Problem Details error response.
func writeAdminJSONError(ctx huma.Context, status int, message string) {
	ctx.SetStatus(status)
	ctx.SetHeader("Content-Type", "application/problem+json")
	response := fmt.Sprintf(`{"status":%d,"title":"%s","detail":"%s"}`, status, http.StatusText(status), message)
	ctx.BodyWriter().Write([]byte(response))
}
