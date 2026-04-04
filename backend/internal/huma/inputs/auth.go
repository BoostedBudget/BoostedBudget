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
	// SubscriptionStatusKey is the context key for the user's subscription status.
	// Set by Clerk middleware; read by read-only enforcement middleware (#930).
	SubscriptionStatusKey ContextKey = "subscription_status"
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

	// Then extract email from Clerk claims
	claims, ok := ctx.Context().Value(ClerkClaimsKey).(*clerk.SessionClaims)
	if !ok || claims == nil {
		return []error{huma.Error401Unauthorized("Claims not found in context")}
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
