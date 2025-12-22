// Package inputs provides common Huma input types for API operations.
package inputs

import (
	"github.com/danielgtaylor/huma/v2"
	"github.com/golang-jwt/jwt/v5"
)

// ContextKey is a custom type for context keys to avoid collisions.
type ContextKey string

const (
	// UserIDKey is the context key for the internal user UUID.
	UserIDKey ContextKey = "user_id"
	// Auth0UserIDKey is the context key for the Auth0 user ID.
	Auth0UserIDKey ContextKey = "auth0_user_id"
	// ClaimsKey is the context key for JWT claims.
	ClaimsKey ContextKey = "claims"
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
	// This is NOT the Auth0 user ID, but the internal BoostedBudget user UUID.
	// Populated by the Resolve method from context.
	UserID string `json:"-" doc:"Internal user UUID (resolved from JWT)"`
}

// Resolve implements huma.Resolver to extract user_id from context.
// The Auth0 middleware must set the user_id in context before this resolver runs.
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

// AuthenticatedWithEmailInput extends AuthenticatedInput to also extract email from JWT claims.
// Use this for profile-related endpoints that need the user's email address.
type AuthenticatedWithEmailInput struct {
	AuthenticatedInput
	// Email is the user's email address from the JWT claims.
	Email string `json:"-" doc:"User email from JWT claims"`
}

// Resolve implements huma.Resolver to extract user_id and email from context.
func (a *AuthenticatedWithEmailInput) Resolve(ctx huma.Context) []error {
	// First resolve the UserID
	if errs := a.AuthenticatedInput.Resolve(ctx); len(errs) > 0 {
		return errs
	}

	// Then extract email from claims
	// Note: Must use jwt.MapClaims (not map[string]interface{}) because Go type assertions are strict
	claims, ok := ctx.Context().Value(ClaimsKey).(jwt.MapClaims)
	if !ok {
		return []error{huma.Error401Unauthorized("Claims not found in context")}
	}

	email, ok := claims["email"].(string)
	if !ok || email == "" {
		return []error{huma.Error401Unauthorized("Email not found in token")}
	}
	a.Email = email
	return nil
}

// Ensure AuthenticatedWithEmailInput implements huma.Resolver at compile time.
var _ huma.Resolver = (*AuthenticatedWithEmailInput)(nil)
