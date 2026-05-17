// Package middleware provides Huma-compatible middleware for the BoostedBudget API.
package middleware

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/clerk/clerk-sdk-go/v2"
	"github.com/clerk/clerk-sdk-go/v2/jwt"
	"github.com/danielgtaylor/huma/v2"
	josejwt "github.com/go-jose/go-jose/v3/jwt"
	"github.com/maxnussbaum/BoostedBudget/internal/auth"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/maxnussbaum/BoostedBudget/internal/observability"
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

// SubscriptionStatusReader can look up a user's subscription status by internal user ID.
// Used by the Clerk middleware to store subscription status in context for read-only enforcement (#930).
type SubscriptionStatusReader interface {
	GetSubscriptionStatusByUserID(ctx context.Context, userID string) (string, error)
}

// NewClerkMiddleware creates a Huma middleware for Clerk JWT validation.
// It validates JWT tokens and resolves Clerk identities to internal user UUIDs.
//
// Parameters:
//   - secretKey: The Clerk secret key (sk_test_... or sk_live_...)
//   - identityRepo: Repository for looking up user identities
//   - registrationService: Service for auto-registering new users
//   - profileRepo: Repository for reading subscription status (nil = skip status check)
//
// The middleware stores:
//   - user_id: Internal user UUID (for most operations)
//   - clerk_user_id: Clerk user ID (for reference)
//   - claims: Clerk session claims (for additional info)
//   - subscription_status: User's subscription status (for read-only enforcement)
func NewClerkMiddleware(
	secretKey string,
	identityRepo repository.UserIdentityRepository,
	registrationService services.RegistrationService,
	logger *slog.Logger,
	profileRepo ...SubscriptionStatusReader,
) func(ctx huma.Context, next func(huma.Context)) {
	if logger == nil {
		logger = slog.Default()
	}

	// Extract optional profile repo for subscription status lookup
	var subStatusRepo SubscriptionStatusReader
	if len(profileRepo) > 0 {
		subStatusRepo = profileRepo[0]
	}

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
		// Use a fresh context with timeout to prevent JWKS fetch from being canceled
		// when the request context is closed prematurely
		verifyCtx, verifyCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer verifyCancel()

		// CustomClaimsConstructor is required to parse custom claims from JWT template
		sessionClaims, err := jwt.Verify(verifyCtx, &jwt.VerifyParams{
			Token:                   tokenString,
			CustomClaimsConstructor: auth.CustomClaimsConstructor,
		})

		if err != nil {
			logger.Warn("JWT verify failed", "error", err)
			if errors.Is(err, josejwt.ErrExpired) {
				observability.AuthAttempts.WithLabelValues("expired").Inc()
			} else {
				observability.AuthAttempts.WithLabelValues("invalid").Inc()
			}
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
		identity, err := identityRepo.GetByClerkID(ctx.Context(), clerkUserID)
		if err != nil {
			if err == repository.ErrIdentityNotFound {
				// Auto-register new user
				email := auth.ExtractEmailFromClaims(sessionClaims)

				// FALLBACK: If email not in JWT claims, fetch from Clerk API
				// This handles cases where JWT template claims aren't populated correctly
				if email == "" {
					var fetchErr error
					email, fetchErr = auth.FetchEmailFromClerkAPI(ctx.Context(), clerkUserID)
					if fetchErr != nil {
						logger.Error("failed to fetch email from Clerk API", "clerk_user_id", clerkUserID, "error", fetchErr)
						writeClerkJSONError(ctx, http.StatusInternalServerError, "Failed to retrieve user email")
						return
					}
				}

				emailVerified := true // Clerk verifies emails

				_, err = registrationService.RegisterUser(ctx.Context(), clerkUserID, email, emailVerified)
				if err != nil {
					logger.Error("failed to register user", "clerk_user_id", clerkUserID, "error", err)
					writeClerkJSONError(ctx, http.StatusInternalServerError, "Failed to create user account")
					return
				}
				observability.AuthRegistrations.Inc()

				identity, err = identityRepo.GetByClerkID(ctx.Context(), clerkUserID)
				if err != nil {
					logger.Error("failed to get identity after registration", "clerk_user_id", clerkUserID, "error", err)
					writeClerkJSONError(ctx, http.StatusInternalServerError, "Failed to complete registration")
					return
				}
			} else {
				logger.Error("identity lookup error", "clerk_user_id", clerkUserID, "error", err)
				writeClerkJSONError(ctx, http.StatusInternalServerError, "Failed to authenticate user")
				return
			}
		}

		// Store user info in context for resolvers
		newCtx := context.WithValue(ctx.Context(), inputs.UserIDKey, identity.UserID)
		newCtx = context.WithValue(newCtx, inputs.ClerkUserIDKey, clerkUserID)
		newCtx = context.WithValue(newCtx, inputs.ClerkClaimsKey, sessionClaims)
		newCtx = observability.WithUserID(newCtx, identity.UserID)

		// #930: Store subscription status for read-only enforcement middleware
		if subStatusRepo != nil {
			if subStatus, subErr := subStatusRepo.GetSubscriptionStatusByUserID(newCtx, identity.UserID); subErr == nil {
				newCtx = context.WithValue(newCtx, inputs.SubscriptionStatusKey, subStatus)
			} else {
				logger.Warn("failed to fetch subscription status", "user_id", identity.UserID, "error", subErr)
			}
		}

		// Update last_used_at asynchronously
		go func() {
			updateCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			identityRepo.UpdateLastUsed(updateCtx, clerkUserID)
		}()

		observability.AuthAttempts.WithLabelValues("success").Inc()
		next(huma.WithContext(ctx, newCtx))
	}
}
