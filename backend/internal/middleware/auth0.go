package middleware

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v4"
)

// Auth0Middleware creates an Echo middleware function that validates Auth0 JWT tokens
//
// Parameters:
//   - issuer: The Auth0 issuer URL (e.g., "https://your-tenant.auth0.com/")
//   - audience: The API audience/identifier (e.g., "https://api.boostedbudget.com")
//
// The middleware:
//   - Extracts the JWT token from the Authorization header
//   - Validates the token signature, expiration, issuer, and audience
//   - Extracts the user_id (sub claim) and stores it in the context as "user_id"
//   - Extracts all claims and stores them in the context as "claims"
//   - Returns 401 Unauthorized if validation fails
//
// Usage:
//
//	e := echo.New()
//	e.Use(Auth0Middleware(os.Getenv("AUTH0_DOMAIN"), os.Getenv("AUTH0_AUDIENCE")))
//
//	e.GET("/protected", func(c echo.Context) error {
//	    userID := c.Get("user_id").(string)
//	    claims := c.Get("claims").(jwt.MapClaims)
//	    return c.JSON(200, map[string]interface{}{"user_id": userID})
//	})
func Auth0Middleware(issuer, audience string) echo.MiddlewareFunc {
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

			// Parse and validate the JWT token
			token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
				// Verify the signing method
				if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
					return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
				}

				// For testing purposes, we use a simple secret
				// In production with Auth0 RS256, you would fetch the JWKS and verify with public key
				// TODO: Implement JWKS-based verification for RS256 tokens
				return []byte("test-secret-key"), nil
			})

			if err != nil {
				return echo.NewHTTPError(http.StatusUnauthorized, "Invalid or expired token")
			}

			if !token.Valid {
				return echo.NewHTTPError(http.StatusUnauthorized, "Invalid token")
			}

			// Extract claims
			claims, ok := token.Claims.(jwt.MapClaims)
			if !ok {
				return echo.NewHTTPError(http.StatusUnauthorized, "Invalid token claims")
			}

			// Validate issuer
			iss, ok := claims["iss"].(string)
			if !ok || iss != issuer {
				return echo.NewHTTPError(http.StatusUnauthorized, "Invalid token issuer")
			}

			// Validate audience
			aud, ok := claims["aud"].(string)
			if !ok || aud != audience {
				return echo.NewHTTPError(http.StatusUnauthorized, "Invalid token audience")
			}

			// Extract user_id from sub claim
			sub, ok := claims["sub"].(string)
			if !ok {
				return echo.NewHTTPError(http.StatusUnauthorized, "Missing user ID in token")
			}

			// Store user_id and claims in context for downstream handlers
			c.Set("user_id", sub)
			c.Set("claims", claims)

			// Continue to next handler
			return next(c)
		}
	}
}

// Auth0MiddlewareWithJWKS creates an Echo middleware that validates Auth0 JWT tokens using JWKS
// This is the production-ready version that fetches Auth0's public keys
//
// TODO: Implement this for production use with RS256 tokens
// For now, we use the simpler HS256 version for testing
func Auth0MiddlewareWithJWKS(ctx context.Context, issuer, audience string) (echo.MiddlewareFunc, error) {
	// TODO: Fetch JWKS from issuer's .well-known/jwks.json endpoint
	// TODO: Cache the public keys
	// TODO: Verify RS256 signatures using the public key
	return nil, errors.New("not implemented yet - use Auth0Middleware for testing")
}

// GetUserID is a helper function to extract the user_id from the Echo context
// Returns an error if the user_id is not found (user not authenticated)
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

// GetClaims is a helper function to extract JWT claims from the Echo context
// Returns an error if claims are not found (user not authenticated)
func GetClaims(c echo.Context) (jwt.MapClaims, error) {
	claims := c.Get("claims")
	if claims == nil {
		return nil, errors.New("claims not found in context")
	}

	claimsMap, ok := claims.(jwt.MapClaims)
	if !ok {
		return nil, errors.New("invalid claims type in context")
	}

	return claimsMap, nil
}

// GetSubscriptionTier extracts the subscription tier from JWT claims
// Returns "free" if not found (default tier)
func GetSubscriptionTier(c echo.Context) string {
	claims, err := GetClaims(c)
	if err != nil {
		return "free"
	}

	tier, ok := claims["https://boostedbudget.com/subscription_tier"].(string)
	if !ok {
		return "free"
	}

	return tier
}

// GetPrimaryRegion extracts the primary region from JWT claims
// Returns "us-east-1" if not found (default region for MVP)
func GetPrimaryRegion(c echo.Context) string {
	claims, err := GetClaims(c)
	if err != nil {
		return "us-east-1"
	}

	region, ok := claims["https://boostedbudget.com/primary_region"].(string)
	if !ok {
		return "us-east-1"
	}

	return region
}
