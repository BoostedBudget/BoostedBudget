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
//  2. Authentication check: Returns 401 if no Clerk user ID in context
//  3. Hardcoded email check: Allows access if email is in hardcoded admin list
//  4. Role check: Returns 403 if user doesn't have admin role in Clerk privateMetadata
//     (and 500, i.e. refused, if the role lookup itself fails — fail closed)
//
// Issue #1432 (batch ED, finding B2-3): step 4 is a CLERK API lookup of privateMetadata, not a
// database check — this codebase has no roles table. Positive (role-granting) results are held
// in an in-process cache, so a role revoked in the Clerk dashboard keeps working until that
// entry expires; the window is bounded by AdminService's positiveRoleCacheTTLCap. Do not reason
// about this path as if revocation were immediate, and do not describe it as a database check.
//
// Issue #1499: there is deliberately NO environment check. Admin endpoints used to return
// 404 outside qa/staging, which made the admin dashboard — system health, user management,
// subscription admin, the dead-letter queue — unreachable in production for actual admins.
// Identity and role are now the whole gate, in every environment. The consequence worth
// keeping in mind: the hardcoded admin email list is now a live production ACL.
//
// Parameters:
//   - cfg: Application config (retained for callers and future per-environment policy;
//     no longer consulted for authorization)
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

		// Issue #638: Skip environment and role checks for check-role endpoint
		// This endpoint allows any authenticated user to check their admin status.
		// It's informational only and available in all environments.
		if path == "/api/v1/admin/check-role" {
			// Only require authentication, skip environment + role checks
			clerkUserID, ok := ctx.Context().Value(inputs.ClerkUserIDKey).(string)
			if !ok || clerkUserID == "" {
				writeAdminJSONError(ctx, http.StatusUnauthorized, "User not authenticated")
				return
			}
			next(ctx)
			return
		}

		// 1. Get Clerk user ID from context (set by Clerk middleware)
		clerkUserID, ok := ctx.Context().Value(inputs.ClerkUserIDKey).(string)
		if !ok || clerkUserID == "" {
			writeAdminJSONError(ctx, http.StatusUnauthorized, "User not authenticated")
			return
		}

		// 2. Get email from Clerk claims (with fallback to Clerk API if needed)
		email := extractEmailFromContext(ctx)
		if email == "" {
			// Fallback: Fetch from Clerk API if JWT claims don't include email
			if apiEmail, err := auth.FetchEmailFromClerkAPI(ctx.Context(), clerkUserID); err == nil {
				email = apiEmail
			}
		}

		// 3. Check hardcoded admin list first (bypasses Clerk API call for role).
		// Issue #1499: with the environment gate gone this list is a PRODUCTION ACL —
		// it is loaded from config/admin_emails.yaml, which ships inside the image.
		if email != "" && adminService.IsHardcodedAdmin(email) {
			next(ctx)
			return
		}

		// 4. Check admin role via Clerk privateMetadata
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
	_, _ = ctx.BodyWriter().Write([]byte(response))
}
