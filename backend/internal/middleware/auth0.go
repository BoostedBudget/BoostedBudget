package middleware

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v4"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// JWKS represents the JSON Web Key Set from Auth0
type JWKS struct {
	Keys []JSONWebKey `json:"keys"`
}

// JSONWebKey represents a single key in the JWKS
type JSONWebKey struct {
	Kty string   `json:"kty"`
	Use string   `json:"use"`
	Kid string   `json:"kid"`
	N   string   `json:"n"`
	E   string   `json:"e"`
	X5c []string `json:"x5c"`
}

// jwksCache caches the fetched JWKS to avoid repeated network calls
type jwksCache struct {
	mu        sync.RWMutex
	keys      map[string]*rsa.PublicKey
	expiresAt time.Time
	issuer    string
}

var cache = &jwksCache{
	keys: make(map[string]*rsa.PublicKey),
}

// fetchJWKS fetches the JWKS from Auth0's well-known endpoint
func fetchJWKS(issuer string) (*JWKS, error) {
	// Ensure issuer ends with /
	if !strings.HasSuffix(issuer, "/") {
		issuer += "/"
	}

	jwksURL := issuer + ".well-known/jwks.json"

	resp, err := http.Get(jwksURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch JWKS: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("JWKS endpoint returned status %d", resp.StatusCode)
	}

	var jwks JWKS
	if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
		return nil, fmt.Errorf("failed to decode JWKS: %w", err)
	}

	return &jwks, nil
}

// convertJWKToPublicKey converts a JSON Web Key to an RSA public key
func convertJWKToPublicKey(jwk JSONWebKey) (*rsa.PublicKey, error) {
	// Decode the modulus
	nBytes, err := base64.RawURLEncoding.DecodeString(jwk.N)
	if err != nil {
		return nil, fmt.Errorf("failed to decode modulus: %w", err)
	}

	// Decode the exponent
	eBytes, err := base64.RawURLEncoding.DecodeString(jwk.E)
	if err != nil {
		return nil, fmt.Errorf("failed to decode exponent: %w", err)
	}

	// Convert bytes to big integers
	n := new(big.Int).SetBytes(nBytes)

	// Convert exponent bytes to int
	var e int
	for _, b := range eBytes {
		e = e<<8 + int(b)
	}

	return &rsa.PublicKey{
		N: n,
		E: e,
	}, nil
}

// getPublicKey retrieves the public key for the given kid, using cache when possible
func getPublicKey(kid string, issuer string) (*rsa.PublicKey, error) {
	cache.mu.RLock()
	// Check if we have a valid cached key
	if time.Now().Before(cache.expiresAt) && cache.issuer == issuer {
		if key, ok := cache.keys[kid]; ok {
			cache.mu.RUnlock()
			return key, nil
		}
	}
	cache.mu.RUnlock()

	// Fetch fresh JWKS
	cache.mu.Lock()
	defer cache.mu.Unlock()

	// Double-check after acquiring write lock
	if time.Now().Before(cache.expiresAt) && cache.issuer == issuer {
		if key, ok := cache.keys[kid]; ok {
			return key, nil
		}
	}

	// Fetch and cache keys
	jwks, err := fetchJWKS(issuer)
	if err != nil {
		return nil, err
	}

	newKeys := make(map[string]*rsa.PublicKey)
	for _, jwk := range jwks.Keys {
		if jwk.Kty != "RSA" || jwk.Use != "sig" {
			continue
		}

		pubKey, err := convertJWKToPublicKey(jwk)
		if err != nil {
			continue // Skip invalid keys
		}

		newKeys[jwk.Kid] = pubKey
	}

	// Update cache
	cache.keys = newKeys
	cache.issuer = issuer
	cache.expiresAt = time.Now().Add(24 * time.Hour) // Cache for 24 hours

	key, ok := newKeys[kid]
	if !ok {
		return nil, fmt.Errorf("public key with kid %s not found in JWKS", kid)
	}

	return key, nil
}

// validateAudience checks if the expected audience is in the token's audience claim
// Handles both string and array formats
func validateAudience(claims jwt.MapClaims, expectedAudience string) bool {
	aud := claims["aud"]

	// Handle string audience
	if audStr, ok := aud.(string); ok {
		return audStr == expectedAudience
	}

	// Handle array audience
	if audArray, ok := aud.([]interface{}); ok {
		for _, a := range audArray {
			if audStr, ok := a.(string); ok && audStr == expectedAudience {
				return true
			}
		}
	}

	return false
}

// Auth0Middleware creates an Echo middleware function that validates Auth0 JWT tokens using JWKS
// and resolves Auth0 identities to internal user UUIDs
//
// Parameters:
//   - issuer: The Auth0 issuer URL (e.g., "https://dev-evuy37ow1xcidsmw.us.auth0.com/")
//   - audience: The API audience/identifier (e.g., "https://api.boostedbudget.com")
//   - identityRepo: Repository for looking up user identities (maps Auth0 ID -> user UUID)
//
// The middleware:
//   - Extracts the JWT token from the Authorization header
//   - Validates the token signature using Auth0's public keys (RS256)
//   - Validates expiration, issuer, and audience
//   - Looks up the user identity from user_identities table
//   - Stores the internal user UUID in context as "user_id" (NOT the Auth0 ID)
//   - Stores the Auth0 user ID in context as "auth0_user_id" for reference
//   - Extracts all claims and stores them in the context as "claims"
//   - Updates last_used_at timestamp for the identity
//   - Returns 401 Unauthorized if validation fails or identity not found
//
// BREAKING CHANGE: GetUserID() now returns the internal user UUID, not the Auth0 user ID
//
// Usage:
//
//	e := echo.New()
//	identityRepo := repository.NewUserIdentityRepository(db)
//	e.Use(Auth0Middleware(os.Getenv("AUTH0_DOMAIN"), os.Getenv("AUTH0_AUDIENCE"), identityRepo))
//
//	e.GET("/protected", func(c echo.Context) error {
//	    userID := c.Get("user_id").(string)  // Internal UUID, NOT Auth0 ID
//	    claims := c.Get("claims").(jwt.MapClaims)
//	    return c.JSON(200, map[string]interface{}{"user_id": userID})
//	})
func Auth0Middleware(issuer, audience string, identityRepo repository.UserIdentityRepository) echo.MiddlewareFunc {
	// Ensure issuer has https:// prefix and trailing slash
	if !strings.HasPrefix(issuer, "http") {
		issuer = "https://" + issuer
	}
	if !strings.HasSuffix(issuer, "/") {
		issuer += "/"
	}

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

			// Parse the JWT token (without validation yet)
			token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
				// Verify the signing method is RSA
				if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
					return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
				}

				// Get the kid from token header
				kid, ok := token.Header["kid"].(string)
				if !ok {
					return nil, errors.New("missing kid in token header")
				}

				// Fetch the public key for this kid
				publicKey, err := getPublicKey(kid, issuer)
				if err != nil {
					return nil, fmt.Errorf("failed to get public key: %w", err)
				}

				return publicKey, nil
			})

			if err != nil {
				return echo.NewHTTPError(http.StatusUnauthorized, fmt.Sprintf("Invalid or expired token: %v", err))
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
				return echo.NewHTTPError(http.StatusUnauthorized, fmt.Sprintf("Invalid token issuer: expected %s, got %s", issuer, iss))
			}

			// Validate audience (handles both string and array)
			if !validateAudience(claims, audience) {
				return echo.NewHTTPError(http.StatusUnauthorized, fmt.Sprintf("Invalid token audience: %v does not contain %s", claims["aud"], audience))
			}

			// Extract Auth0 user ID from sub claim
			auth0UserID, ok := claims["sub"].(string)
			if !ok {
				return echo.NewHTTPError(http.StatusUnauthorized, "Missing user ID in token")
			}

			// Look up user identity to get internal user UUID
			identity, err := identityRepo.GetByAuth0ID(c.Request().Context(), auth0UserID)
			if err != nil {
				if err == repository.ErrIdentityNotFound {
					return echo.NewHTTPError(http.StatusUnauthorized, "User identity not found - please contact support")
				}
				c.Logger().Errorf("Failed to lookup user identity for %s: %v", auth0UserID, err)
				return echo.NewHTTPError(http.StatusInternalServerError, "Failed to authenticate user")
			}

			// Store internal user UUID (NOT Auth0 ID) in context for downstream handlers
			// BREAKING CHANGE: GetUserID() now returns UUID instead of Auth0 user ID
			c.Set("user_id", identity.UserID)
			c.Set("auth0_user_id", auth0UserID) // Store Auth0 ID for reference if needed
			c.Set("claims", claims)

			// Update last_used_at timestamp asynchronously (non-blocking)
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if err := identityRepo.UpdateLastUsed(ctx, auth0UserID); err != nil {
					// Log error but don't fail the request
					c.Logger().Warnf("Failed to update last_used_at for %s: %v", auth0UserID, err)
				}
			}()

			// Continue to next handler
			return next(c)
		}
	}
}

// Auth0MiddlewareWithJWKS is now an alias to Auth0Middleware since we implemented JWKS
// Deprecated: Use Auth0Middleware directly instead
func Auth0MiddlewareWithJWKS(ctx context.Context, issuer, audience string, identityRepo repository.UserIdentityRepository) (echo.MiddlewareFunc, error) {
	return Auth0Middleware(issuer, audience, identityRepo), nil
}

// GetUserID is a helper function to extract the user_id from the Echo context
// BREAKING CHANGE: Now returns the internal user UUID instead of Auth0 user ID
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

// GetAuth0UserID is a helper function to extract the Auth0 user ID from the Echo context
// Returns the original Auth0 user ID (e.g., "google-oauth2|123456")
// Use this only when you specifically need the Auth0 ID (rare)
// For most use cases, use GetUserID() which returns the internal user UUID
func GetAuth0UserID(c echo.Context) (string, error) {
	auth0UserID := c.Get("auth0_user_id")
	if auth0UserID == nil {
		return "", errors.New("auth0_user_id not found in context")
	}

	auth0UserIDStr, ok := auth0UserID.(string)
	if !ok {
		return "", errors.New("invalid auth0_user_id type in context")
	}

	return auth0UserIDStr, nil
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

// GetEmail extracts the email from JWT claims
// Tries custom claim first, then standard email claim
func GetEmail(c echo.Context) (string, error) {
	claims, err := GetClaims(c)
	if err != nil {
		return "", err
	}

	// Try custom namespaced claim first
	if email, ok := claims["https://boostedbudget.com/email"].(string); ok && email != "" {
		return email, nil
	}

	// Try standard email claim
	if email, ok := claims["email"].(string); ok && email != "" {
		return email, nil
	}

	return "", errors.New("email not found in token claims")
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
// Returns "iad" if not found (default region for MVP)
func GetPrimaryRegion(c echo.Context) string {
	claims, err := GetClaims(c)
	if err != nil {
		return "iad"
	}

	region, ok := claims["https://boostedbudget.com/primary_region"].(string)
	if !ok {
		return "iad"
	}

	return region
}
