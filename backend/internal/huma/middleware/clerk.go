// Package middleware provides Huma-compatible middleware for the BoostedBudget API.
package middleware

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/clerk/clerk-sdk-go/v2"
	"github.com/clerk/clerk-sdk-go/v2/jwt"
	"github.com/danielgtaylor/huma/v2"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
)

// writeJSONError writes an RFC 9457 Problem Details error response.
func writeClerkJSONError(ctx huma.Context, status int, message string) {
	ctx.SetStatus(status)
	ctx.SetHeader("Content-Type", "application/problem+json")
	// Write minimal RFC 9457 response
	_, _ = ctx.BodyWriter().Write([]byte(fmt.Sprintf(
		`{"status":%d,"title":"%s","detail":"%s"}`,
		status, http.StatusText(status), message,
	)))
}

// NewClerkMiddleware creates a Huma middleware for Clerk JWT validation.
// It validates JWT tokens and resolves Clerk identities to internal user UUIDs.
//
// Parameters:
//   - secretKey: The Clerk secret key (sk_test_... or sk_live_...)
//   - identityRepo: Repository for looking up user identities
//   - registrationService: Service for auto-registering new users
//
// The middleware stores:
//   - user_id: Internal user UUID (for most operations)
//   - clerk_user_id: Clerk user ID (for reference)
//   - claims: Clerk session claims (for additional info)
func NewClerkMiddleware(
	secretKey string,
	identityRepo repository.UserIdentityRepository,
	registrationService services.RegistrationService,
) func(ctx huma.Context, next func(huma.Context)) {
	// Initialize Clerk SDK with secret key
	clerk.SetKey(secretKey)

	return func(ctx huma.Context, next func(huma.Context)) {
		// Skip auth for routes without Security annotation (public routes)
		// This allows public routes like /health, /metrics, /docs to work without auth
		if len(ctx.Operation().Security) == 0 {
			next(ctx)
			return
		}

		// Extract Authorization header
		authHeader := ctx.Header("Authorization")
		if authHeader == "" {
			writeClerkJSONError(ctx, http.StatusUnauthorized, "Missing authorization header")
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || parts[0] != "Bearer" {
			writeClerkJSONError(ctx, http.StatusUnauthorized, "Invalid authorization header format")
			return
		}

		tokenString := parts[1]

		// Verify the JWT token using Clerk SDK
		sessionClaims, err := jwt.Verify(ctx.Context(), &jwt.VerifyParams{
			Token: tokenString,
		})
		if err != nil {
			writeClerkJSONError(ctx, http.StatusUnauthorized, "Invalid or expired token")
			return
		}

		// Extract Clerk user ID from Subject claim
		clerkUserID := sessionClaims.Subject
		if clerkUserID == "" {
			writeClerkJSONError(ctx, http.StatusUnauthorized, "Missing user ID in token")
			return
		}

		// Look up or register user identity
		identity, err := identityRepo.GetByAuth0ID(ctx.Context(), clerkUserID)
		if err != nil {
			if err == repository.ErrIdentityNotFound {
				// Auto-register new user
				email := extractEmailFromClerkClaims(sessionClaims)
				emailVerified := true // Clerk verifies emails

				_, err = registrationService.RegisterUser(ctx.Context(), clerkUserID, email, emailVerified)
				if err != nil {
					writeClerkJSONError(ctx, http.StatusInternalServerError, "Failed to create user account")
					return
				}

				identity, err = identityRepo.GetByAuth0ID(ctx.Context(), clerkUserID)
				if err != nil {
					writeClerkJSONError(ctx, http.StatusInternalServerError, "Failed to complete registration")
					return
				}
			} else {
				writeClerkJSONError(ctx, http.StatusInternalServerError, "Failed to authenticate user")
				return
			}
		}

		// Store user info in context for resolvers
		newCtx := context.WithValue(ctx.Context(), inputs.UserIDKey, identity.UserID)
		newCtx = context.WithValue(newCtx, inputs.ClerkUserIDKey, clerkUserID)
		newCtx = context.WithValue(newCtx, inputs.ClerkClaimsKey, sessionClaims)

		// Update last_used_at asynchronously
		go func() {
			updateCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			identityRepo.UpdateLastUsed(updateCtx, clerkUserID)
		}()

		// Continue with updated context
		next(huma.WithContext(ctx, newCtx))
	}
}

// extractEmailFromClerkClaims extracts email from Clerk session claims.
func extractEmailFromClerkClaims(claims *clerk.SessionClaims) string {
	if claims.Custom == nil {
		return ""
	}

	customClaims, ok := claims.Custom.(map[string]interface{})
	if !ok {
		return ""
	}

	if email, ok := customClaims["email"].(string); ok {
		return email
	}

	return ""
}
