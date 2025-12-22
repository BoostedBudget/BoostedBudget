// Package middleware provides Huma-compatible middleware for the BoostedBudget API.
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

	"github.com/danielgtaylor/huma/v2"
	"github.com/golang-jwt/jwt/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
)

// writeJSONError writes an RFC 9457 Problem Details error response.
func writeJSONError(ctx huma.Context, status int, message string) {
	ctx.SetStatus(status)
	ctx.SetHeader("Content-Type", "application/problem+json")
	// Write minimal RFC 9457 response
	_, _ = ctx.BodyWriter().Write([]byte(fmt.Sprintf(
		`{"status":%d,"title":"%s","detail":"%s"}`,
		status, http.StatusText(status), message,
	)))
}

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
	nBytes, err := base64.RawURLEncoding.DecodeString(jwk.N)
	if err != nil {
		return nil, fmt.Errorf("failed to decode modulus: %w", err)
	}

	eBytes, err := base64.RawURLEncoding.DecodeString(jwk.E)
	if err != nil {
		return nil, fmt.Errorf("failed to decode exponent: %w", err)
	}

	n := new(big.Int).SetBytes(nBytes)

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
	if time.Now().Before(cache.expiresAt) && cache.issuer == issuer {
		if key, ok := cache.keys[kid]; ok {
			cache.mu.RUnlock()
			return key, nil
		}
	}
	cache.mu.RUnlock()

	cache.mu.Lock()
	defer cache.mu.Unlock()

	if time.Now().Before(cache.expiresAt) && cache.issuer == issuer {
		if key, ok := cache.keys[kid]; ok {
			return key, nil
		}
	}

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
			continue
		}

		newKeys[jwk.Kid] = pubKey
	}

	cache.keys = newKeys
	cache.issuer = issuer
	cache.expiresAt = time.Now().Add(24 * time.Hour)

	key, ok := newKeys[kid]
	if !ok {
		return nil, fmt.Errorf("public key with kid %s not found in JWKS", kid)
	}

	return key, nil
}

// validateAudience checks if the expected audience is in the token's audience claim
func validateAudience(claims jwt.MapClaims, expectedAudience string) bool {
	aud := claims["aud"]

	if audStr, ok := aud.(string); ok {
		return audStr == expectedAudience
	}

	if audArray, ok := aud.([]interface{}); ok {
		for _, a := range audArray {
			if audStr, ok := a.(string); ok && audStr == expectedAudience {
				return true
			}
		}
	}

	return false
}

// NewAuth0Middleware creates a Huma middleware for Auth0 JWT validation.
// It validates JWT tokens and resolves Auth0 identities to internal user UUIDs.
//
// Parameters:
//   - issuer: The Auth0 issuer URL (e.g., "https://dev-xxx.us.auth0.com/")
//   - audience: The API audience/identifier (e.g., "https://api.boostedbudget.com")
//   - identityRepo: Repository for looking up user identities
//   - registrationService: Service for auto-registering new users
//
// The middleware stores:
//   - user_id: Internal user UUID (for most operations)
//   - auth0_user_id: Auth0 user ID (for reference)
//   - claims: JWT claims (for additional info)
func NewAuth0Middleware(
	issuer, audience string,
	identityRepo repository.UserIdentityRepository,
	registrationService services.RegistrationService,
) func(ctx huma.Context, next func(huma.Context)) {
	// Normalize issuer
	if !strings.HasPrefix(issuer, "http") {
		issuer = "https://" + issuer
	}
	if !strings.HasSuffix(issuer, "/") {
		issuer += "/"
	}

	return func(ctx huma.Context, next func(huma.Context)) {
		// Extract Authorization header
		authHeader := ctx.Header("Authorization")
		if authHeader == "" {
			writeJSONError(ctx, http.StatusUnauthorized, "Missing authorization header")
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || parts[0] != "Bearer" {
			writeJSONError(ctx, http.StatusUnauthorized, "Invalid authorization header format")
			return
		}

		tokenString := parts[1]

		// Parse and validate JWT
		token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
			if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
			}

			kid, ok := token.Header["kid"].(string)
			if !ok {
				return nil, errors.New("missing kid in token header")
			}

			return getPublicKey(kid, issuer)
		})

		if err != nil || !token.Valid {
			writeJSONError(ctx, http.StatusUnauthorized, "Invalid or expired token")
			return
		}

		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			writeJSONError(ctx, http.StatusUnauthorized, "Invalid token claims")
			return
		}

		// Validate issuer
		if iss, _ := claims["iss"].(string); iss != issuer {
			writeJSONError(ctx, http.StatusUnauthorized, "Invalid token issuer")
			return
		}

		// Validate audience
		if !validateAudience(claims, audience) {
			writeJSONError(ctx, http.StatusUnauthorized, "Invalid token audience")
			return
		}

		// Extract Auth0 user ID
		auth0UserID, ok := claims["sub"].(string)
		if !ok {
			writeJSONError(ctx, http.StatusUnauthorized, "Missing user ID in token")
			return
		}

		// Look up or register user identity
		identity, err := identityRepo.GetByAuth0ID(ctx.Context(), auth0UserID)
		if err != nil {
			if err == repository.ErrIdentityNotFound {
				// Auto-register new user
				email, _ := claims["email"].(string)
				emailVerified, _ := claims["email_verified"].(bool)

				_, err = registrationService.RegisterUser(ctx.Context(), auth0UserID, email, emailVerified)
				if err != nil {
					writeJSONError(ctx, http.StatusInternalServerError, "Failed to create user account")
					return
				}

				identity, err = identityRepo.GetByAuth0ID(ctx.Context(), auth0UserID)
				if err != nil {
					writeJSONError(ctx, http.StatusInternalServerError, "Failed to complete registration")
					return
				}
			} else {
				writeJSONError(ctx, http.StatusInternalServerError, "Failed to authenticate user")
				return
			}
		}

		// Store user info in context for resolvers
		newCtx := context.WithValue(ctx.Context(), inputs.UserIDKey, identity.UserID)
		newCtx = context.WithValue(newCtx, inputs.Auth0UserIDKey, auth0UserID)
		newCtx = context.WithValue(newCtx, inputs.ClaimsKey, claims)

		// Update last_used_at asynchronously
		go func() {
			updateCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			identityRepo.UpdateLastUsed(updateCtx, auth0UserID)
		}()

		// Continue with updated context
		next(huma.WithContext(ctx, newCtx))
	}
}
