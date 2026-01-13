package inputs

import (
	"github.com/danielgtaylor/huma/v2"
)

// AdminAuthenticatedInput provides admin authentication resolution for admin-only endpoints.
// Embeds AuthenticatedInput for UserID and adds ClerkUserID extraction.
//
// Note: The actual admin role validation is done by the AdminMiddleware.
// This resolver only extracts the user identifiers from context.
// By the time this resolver runs, the middleware has already validated admin access.
type AdminAuthenticatedInput struct {
	AuthenticatedInput
	// ClerkUserID is the Clerk user ID extracted from context.
	// Useful for audit logging and Clerk API operations.
	ClerkUserID string `json:"-" doc:"Clerk user ID (resolved from JWT)"`
}

// Resolve implements huma.Resolver to extract user_id and clerk_user_id from context.
// The AdminMiddleware must validate admin access before this resolver runs.
func (a *AdminAuthenticatedInput) Resolve(ctx huma.Context) []error {
	// First resolve the UserID from AuthenticatedInput
	if errs := a.AuthenticatedInput.Resolve(ctx); len(errs) > 0 {
		return errs
	}

	// Extract Clerk user ID from context
	clerkUserID, ok := ctx.Context().Value(ClerkUserIDKey).(string)
	if !ok || clerkUserID == "" {
		return []error{huma.Error401Unauthorized("Clerk user ID not found in context")}
	}
	a.ClerkUserID = clerkUserID

	return nil
}

// Ensure AdminAuthenticatedInput implements huma.Resolver at compile time.
var _ huma.Resolver = (*AdminAuthenticatedInput)(nil)
