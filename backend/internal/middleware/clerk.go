package middleware

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/clerk/clerk-sdk-go/v2"
	"github.com/clerk/clerk-sdk-go/v2/jwt"
	"github.com/labstack/echo/v4"
	"github.com/maxnussbaum/BoostedBudget/internal/auth"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
)

// ClerkMiddleware creates an Echo middleware function that validates Clerk JWT tokens
// and resolves Clerk identities to internal user UUIDs.
//
// Parameters:
//   - secretKey: The Clerk secret key (sk_test_... or sk_live_...)
//   - identityRepo: Repository for looking up user identities (maps Clerk ID -> user UUID)
//   - registrationService: Service for auto-registering new users
//
// The middleware:
//   - Extracts the JWT token from the Authorization header
//   - Validates the token using Clerk's SDK (handles JWKS automatically)
//   - Looks up the user identity from user_identities table
//   - Auto-registers new users if not found
//   - Stores the internal user UUID in context as "user_id"
//   - Stores the Clerk user ID in context as "clerk_user_id"
//   - Stores the session claims in context as "claims"
//   - Updates last_used_at timestamp for the identity
//   - Returns 401 Unauthorized if validation fails
func ClerkMiddleware(secretKey string, identityRepo repository.UserIdentityRepository, registrationService services.RegistrationService) echo.MiddlewareFunc {
	// Initialize Clerk SDK with secret key
	clerk.SetKey(secretKey)

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			// Extract token from Authorization header
			authHeader := c.Request().Header.Get("Authorization")
			if authHeader == "" {
				return echo.NewHTTPError(http.StatusUnauthorized, "Missing authorization header")
			}

			// Authorization header should be in format: "Bearer <token>"
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || parts[0] != "Bearer" {
				return echo.NewHTTPError(http.StatusUnauthorized, "Invalid authorization header format")
			}

			tokenString := parts[1]

			// Verify the JWT token using Clerk SDK
			// The SDK handles JWKS fetching and caching automatically
			sessionClaims, err := jwt.Verify(c.Request().Context(), &jwt.VerifyParams{
				Token: tokenString,
			})
			if err != nil {
				c.Logger().Warnf("JWT verification failed: %v", err)
				return echo.NewHTTPError(http.StatusUnauthorized, "Invalid or expired token")
			}

			// Extract Clerk user ID from Subject claim
			clerkUserID := sessionClaims.Subject
			if clerkUserID == "" {
				return echo.NewHTTPError(http.StatusUnauthorized, "Missing user ID in token")
			}

			// Look up user identity to get internal user UUID
			// Note: We use GetByClerkID which works for any provider ID format
			identity, err := identityRepo.GetByClerkID(c.Request().Context(), clerkUserID)
			if err != nil {
				if err == repository.ErrIdentityNotFound {
					// New user - auto-register them
					c.Logger().Infof("New user detected, auto-registering: %s", clerkUserID)

					// Extract email from custom claims or fetch from Clerk API
					email := auth.ExtractEmailFromClaims(sessionClaims)
					if email == "" {
						c.Logger().Errorf("Missing email in token claims for %s", clerkUserID)
						return echo.NewHTTPError(http.StatusUnauthorized, "Missing email in authentication token")
					}

					// For Clerk, emails are verified through their system
					// We trust the email if it's in the token
					emailVerified := true

					// Register the new user
					_, err := registrationService.RegisterUser(c.Request().Context(), clerkUserID, email, emailVerified)
					if err != nil {
						c.Logger().Errorf("Failed to register new user %s: %v", clerkUserID, err)
						return echo.NewHTTPError(http.StatusInternalServerError, "Failed to create user account")
					}

					// Lookup the identity again after registration
					identity, err = identityRepo.GetByClerkID(c.Request().Context(), clerkUserID)
					if err != nil {
						c.Logger().Errorf("Failed to lookup newly registered identity for %s: %v", clerkUserID, err)
						return echo.NewHTTPError(http.StatusInternalServerError, "Failed to complete registration")
					}

					c.Logger().Infof("Successfully registered new user: %s", clerkUserID)
				} else {
					c.Logger().Errorf("Failed to lookup user identity for %s: %v", clerkUserID, err)
					return echo.NewHTTPError(http.StatusInternalServerError, "Failed to authenticate user")
				}
			}

			// Store internal user UUID in context for downstream handlers
			c.Set("user_id", identity.UserID)
			c.Set("clerk_user_id", clerkUserID)
			c.Set("claims", sessionClaims)

			// Update last_used_at timestamp asynchronously (non-blocking)
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if err := identityRepo.UpdateLastUsed(ctx, clerkUserID); err != nil {
					c.Logger().Warnf("Failed to update last_used_at for %s: %v", clerkUserID, err)
				}
			}()

			// Continue to next handler
			return next(c)
		}
	}
}

// GetUserID is a helper function to extract the user_id from the Echo context.
// Returns the internal user UUID.
// Returns an error if the user_id is not found (user not authenticated).
func GetUserID(c echo.Context) (string, error) {
	userID := c.Get("user_id")
	if userID == nil {
		return "", errors.New("user not authenticated")
	}

	userIDStr, ok := userID.(string)
	if !ok {
		return "", errors.New("invalid user_id type in context")
	}

	return userIDStr, nil
}

// GetClerkUserID is a helper function to extract the Clerk user ID from the Echo context.
// Returns the Clerk user ID (e.g., "user_xxxxx").
// Use this only when you specifically need the Clerk ID.
// For most use cases, use GetUserID() which returns the internal user UUID.
func GetClerkUserID(c echo.Context) (string, error) {
	clerkUserID := c.Get("clerk_user_id")
	if clerkUserID == nil {
		return "", errors.New("clerk_user_id not found in context")
	}

	clerkUserIDStr, ok := clerkUserID.(string)
	if !ok {
		return "", errors.New("invalid clerk_user_id type in context")
	}

	return clerkUserIDStr, nil
}

// GetClerkClaims is a helper function to extract Clerk session claims from the Echo context.
// Returns an error if claims are not found (user not authenticated).
func GetClerkClaims(c echo.Context) (*clerk.SessionClaims, error) {
	claims := c.Get("claims")
	if claims == nil {
		return nil, errors.New("claims not found in context")
	}

	sessionClaims, ok := claims.(*clerk.SessionClaims)
	if !ok {
		return nil, errors.New("invalid claims type in context")
	}

	return sessionClaims, nil
}

// GetEmail extracts the email from Clerk claims.
// Returns the email from custom claims if configured in JWT template.
func GetEmail(c echo.Context) (string, error) {
	claims, err := GetClerkClaims(c)
	if err != nil {
		return "", err
	}

	email := auth.ExtractEmailFromClaims(claims)
	if email == "" {
		return "", errors.New("email not found in token claims")
	}

	return email, nil
}

// GetSubscriptionTier extracts the subscription tier from Clerk claims.
// Returns "free" if not found (default tier).
// The tier should be set in the user's public metadata in Clerk.
func GetSubscriptionTier(c echo.Context) string {
	claims, err := GetClerkClaims(c)
	if err != nil {
		return auth.DefaultSubscriptionTier
	}
	return auth.ExtractSubscriptionTier(claims)
}

// GetPrimaryRegion extracts the primary region from Clerk claims.
// Returns "iad" if not found (default region for MVP).
// The region should be set in the user's public metadata in Clerk.
func GetPrimaryRegion(c echo.Context) string {
	claims, err := GetClerkClaims(c)
	if err != nil {
		return auth.DefaultPrimaryRegion()
	}
	return auth.ExtractPrimaryRegion(claims)
}
