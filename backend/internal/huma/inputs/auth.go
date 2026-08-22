// Package inputs provides common Huma input types for API operations.
package inputs

import (
	"github.com/clerk/clerk-sdk-go/v2"
	"github.com/danielgtaylor/huma/v2"
	"github.com/maxnussbaum/BoostedBudget/internal/auth"
)

// ContextKey is a custom type for context keys to avoid collisions.
type ContextKey string

const (
	// UserIDKey is the context key for the internal user UUID.
	UserIDKey ContextKey = "user_id"
	// ClerkUserIDKey is the context key for the Clerk user ID.
	ClerkUserIDKey ContextKey = "clerk_user_id"
	// ClerkClaimsKey is the context key for Clerk session claims.
	ClerkClaimsKey ContextKey = "clerk_claims"
	// EffectiveSubscriptionStatusKey is the context key for the user's EFFECTIVE subscription
	// status — "active" when a non-expired manual tier override (admin comp) is in effect,
	// otherwise the raw subscription status (Issue #1413). Set by Clerk middleware; read by
	// read-only enforcement middleware (#930).
	EffectiveSubscriptionStatusKey ContextKey = "subscription_status"

	// SubscriptionStatusUnresolvedKey is the context key set to true when the Clerk
	// middleware's effective-subscription-status lookup FAILED (e.g. a DB error) — the
	// status is unknown, not merely absent. This is distinct from the key simply being
	// unset (no status reader configured), which legitimately means "cannot determine
	// restriction, allow through." Read by the read-only enforcement middleware to fail
	// closed on mutating requests rather than silently granting write access during an
	// outage (Issue #1413).
	SubscriptionStatusUnresolvedKey ContextKey = "subscription_status_unresolved"

	// ClerkUserIDHeader is the HTTP header used by test helpers to supply a user ID
	// without going through JWT validation. In production, the Clerk middleware always
	// sets UserIDKey in context first, so this header is never read.
	ClerkUserIDHeader = "X-Clerk-User-Id"
)

// AuthenticatedInput provides user authentication resolution for all protected endpoints.
// Embed this in your input structs to automatically resolve the user ID from JWT.
//
// Example:
//
//	type GetTagsInput struct {
//	    inputs.AuthenticatedInput
//	}
//
//	func (h *Handler) GetTags(ctx context.Context, input *GetTagsInput) (*GetTagsOutput, error) {
//	    userID := input.UserID // Already resolved from JWT
//	    ...
//	}
type AuthenticatedInput struct {
	// UserID is the internal user UUID resolved from the JWT token.
	// This is NOT the Clerk user ID, but the internal BoostedBudget user UUID.
	// Populated by the Resolve method from context.
	UserID string `json:"-" doc:"Internal user UUID (resolved from JWT)"`
}

// Resolve implements huma.Resolver to extract user_id from context.
// The Clerk middleware must set the user_id in context before this resolver runs.
func (a *AuthenticatedInput) Resolve(ctx huma.Context) []error {
	userID, ok := ctx.Context().Value(UserIDKey).(string)
	if !ok || userID == "" {
		return []error{huma.Error401Unauthorized("User not authenticated")}
	}
	a.UserID = userID
	return nil
}

// Ensure AuthenticatedInput implements huma.Resolver at compile time.
var _ huma.Resolver = (*AuthenticatedInput)(nil)

// AuthenticatedWithEmailInput extends AuthenticatedInput to also extract email from Clerk claims.
// Use this for profile-related endpoints that need the user's email address.
type AuthenticatedWithEmailInput struct {
	AuthenticatedInput
	// Email is the user's email address from the Clerk claims.
	Email string `json:"-" doc:"User email from Clerk claims"`
}

// Resolve implements huma.Resolver to extract user_id and email from context.
func (a *AuthenticatedWithEmailInput) Resolve(ctx huma.Context) []error {
	// First resolve the UserID
	if errs := a.AuthenticatedInput.Resolve(ctx); len(errs) > 0 {
		return errs
	}

	// Then extract email from Clerk SESSION claims.
	//
	// #1560 gap analysis: this is also the boundary that refuses MCP-origin callers, and the
	// refusal is deliberate. middleware/clerk.go's MCP branch sets UserIDKey and ClerkUserIDKey but
	// NOT ClerkClaimsKey ("claims is nil for the MCP path (OAuth token, not a session JWT)"), so
	// every operation using this input — today profile.go and deletion.go — is session-only. That is
	// the right default: the email resolved here is the session-verified address a GDPR deletion
	// cancellation link is sent to, and an OAuth-scoped agent token does not establish it.
	//
	// The message must SAY so. It previously read "Claims not found in context", which is
	// indistinguishable from a malformed JWT — and that ambiguity is exactly how a second 401 cause
	// stayed hidden behind the registration-order bug #1560 fixed. Pinned by
	// TestAuthenticatedWithEmailInput_Resolve_MCPOriginIsRefusedDiagnostically.
	//
	// ⚠ NOTE the consequence for the Clerk-API fallback below: it is keyed on ClerkUserIDKey, which
	// the MCP path DOES set, so it could serve MCP callers — but this guard returns first, making
	// that fallback unreachable for them. Whether MCP should reach profile/deletion at all is a
	// product decision, deliberately NOT taken here; see the #1537 gap analysis.
	claims, ok := ctx.Context().Value(ClerkClaimsKey).(*clerk.SessionClaims)
	if !ok || claims == nil {
		return []error{huma.Error401Unauthorized(
			"Clerk session claims not found in context: this operation requires a session JWT and " +
				"cannot be served from an OAuth/MCP-origin token")}
	}

	// Extract email from custom claims (set via JWT template in Clerk)
	email := auth.ExtractEmailFromClaims(claims)

	// FALLBACK: If email not in JWT claims, fetch from Clerk API
	// This handles cases where JWT template claims aren't populated correctly
	// (e.g., no JWT template configured, or using default Clerk token)
	if email == "" {
		clerkUserID, ok := ctx.Context().Value(ClerkUserIDKey).(string)
		if ok && clerkUserID != "" {
			var fetchErr error
			email, fetchErr = auth.FetchEmailFromClerkAPI(ctx.Context(), clerkUserID)
			if fetchErr != nil {
				return []error{huma.Error401Unauthorized("Email not found in token and failed to fetch from Clerk API")}
			}
		}
	}

	if email == "" {
		return []error{huma.Error401Unauthorized("Email not found in token")}
	}
	a.Email = email
	return nil
}

// Ensure AuthenticatedWithEmailInput implements huma.Resolver at compile time.
var _ huma.Resolver = (*AuthenticatedWithEmailInput)(nil)
