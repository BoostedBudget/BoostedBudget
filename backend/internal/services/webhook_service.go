package services

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/plaid/plaid-go/v23/plaid"
)

// WebhookService provides webhook signature verification for Plaid webhooks.
// Implements JWT verification with ES256 (ECDSA P-256 + SHA-256) per Plaid specification.
//
// Issue #202: Plaid Webhook Integration
// Verification Process (6 steps):
//  1. Decode JWT header (unverified) to extract kid and alg
//  2. Validate algorithm is ES256
//  3. Fetch JWK from Plaid API (with caching)
//  4. Parse JWK to ECDSA public key
//  5. Verify JWT signature using golang-jwt
//  6. Validate iat claim (<5 minutes old)
//  7. Verify body SHA-256 hash matches claim
type WebhookService interface {
	// VerifyWebhook verifies a Plaid webhook signature using JWT verification.
	// Returns true if signature is valid, false otherwise.
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeout
	//   - rawBody: Raw request body bytes (must preserve original whitespace for hashing)
	//   - signature: JWT from Plaid-Verification header
	//
	// Returns:
	//   - bool: true if signature valid, false otherwise
	//   - error: verification error details, nil on success
	VerifyWebhook(ctx context.Context, rawBody []byte, signature string) (bool, error)
}

// jwkCache provides thread-safe caching of JWK public keys
type jwkCache interface {
	Get(kid string) (*ecdsa.PublicKey, bool)
	Set(kid string, key *ecdsa.PublicKey)
	Delete(kid string)
}

// webhookService implements WebhookService
type webhookService struct {
	plaidClient *plaid.APIClient
	jwkCache    jwkCache
}

// NewWebhookService creates a new WebhookService instance with JWK caching
func NewWebhookService(plaidClient *plaid.APIClient) WebhookService {
	return &webhookService{
		plaidClient: plaidClient,
		jwkCache:    newSyncMapJWKCache(),
	}
}

// VerifyWebhook implements 6-step JWT verification process
func (s *webhookService) VerifyWebhook(ctx context.Context, rawBody []byte, signature string) (bool, error) {
	// Validate signature is not empty
	if signature == "" {
		return false, errors.New("signature is empty")
	}

	// Step 1: Decode JWT header (unverified) to extract kid and alg
	token, err := jwt.Parse(signature, func(token *jwt.Token) (interface{}, error) {
		// Step 2: Verify algorithm is ES256
		if token.Method.Alg() != "ES256" {
			return nil, fmt.Errorf("invalid algorithm: %s (expected ES256)", token.Method.Alg())
		}

		// Extract kid from header
		kidInterface, ok := token.Header["kid"]
		if !ok {
			return nil, errors.New("missing kid in JWT header")
		}
		kid, ok := kidInterface.(string)
		if !ok {
			return nil, errors.New("kid is not a string")
		}

		// Step 3: Get JWK public key (with caching)
		publicKey, err := s.getJWKPublicKey(ctx, kid)
		if err != nil {
			return nil, fmt.Errorf("failed to get JWK: %w", err)
		}

		return publicKey, nil
	})

	// Step 4: Verify JWT signature
	if err != nil {
		return false, fmt.Errorf("JWT verification failed: %w", err)
	}

	if !token.Valid {
		return false, errors.New("JWT is invalid")
	}

	// Step 5: Validate iat claim (must be within 5 minutes)
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return false, errors.New("failed to extract claims")
	}

	iatInterface, ok := claims["iat"]
	if !ok {
		return false, errors.New("missing iat claim")
	}

	var issuedAt time.Time
	switch v := iatInterface.(type) {
	case float64:
		issuedAt = time.Unix(int64(v), 0)
	case int64:
		issuedAt = time.Unix(v, 0)
	default:
		return false, fmt.Errorf("invalid iat type: %T", iatInterface)
	}

	if time.Since(issuedAt) > 5*time.Minute {
		return false, fmt.Errorf("JWT too old: issued %v ago (max 5 minutes)", time.Since(issuedAt))
	}

	// Step 6: Verify body hash
	expectedHashInterface, ok := claims["request_body_sha256"]
	if !ok {
		return false, errors.New("missing request_body_sha256 claim")
	}

	expectedHash, ok := expectedHashInterface.(string)
	if !ok {
		return false, errors.New("request_body_sha256 is not a string")
	}

	// Compute SHA-256 of raw body
	bodyHash := sha256.Sum256(rawBody)
	bodyHashHex := fmt.Sprintf("%x", bodyHash)

	if bodyHashHex != expectedHash {
		return false, fmt.Errorf("body hash mismatch: got %s, expected %s", bodyHashHex, expectedHash)
	}

	return true, nil
}

// getJWKPublicKey fetches JWK from Plaid API with caching
func (s *webhookService) getJWKPublicKey(ctx context.Context, kid string) (*ecdsa.PublicKey, error) {
	// Check cache first
	if cachedKey, found := s.jwkCache.Get(kid); found {
		return cachedKey, nil
	}

	// Cache miss - fetch from Plaid API
	request := plaid.NewWebhookVerificationKeyGetRequest(kid)
	resp, _, err := s.plaidClient.PlaidApi.WebhookVerificationKeyGet(ctx).
		WebhookVerificationKeyGetRequest(*request).Execute()
	if err != nil {
		return nil, fmt.Errorf("failed to fetch JWK from Plaid: %w", err)
	}

	// Parse JWK to ECDSA public key
	jwk := resp.GetKey()
	jwkMap := map[string]interface{}{
		"kty": jwk.GetKty(),
		"crv": jwk.GetCrv(),
		"x":   jwk.GetX(),
		"y":   jwk.GetY(),
		"kid": jwk.GetKid(),
		"use": jwk.GetUse(),
		"alg": jwk.GetAlg(),
	}

	publicKey, err := parseJWKToPublicKey(jwkMap)
	if err != nil {
		return nil, fmt.Errorf("failed to parse JWK: %w", err)
	}

	// Cache for future use
	s.jwkCache.Set(kid, publicKey)

	return publicKey, nil
}

// parseJWKToPublicKey converts a JWK map to an ECDSA public key
func parseJWKToPublicKey(jwk map[string]interface{}) (*ecdsa.PublicKey, error) {
	// Validate key type
	kty, ok := jwk["kty"].(string)
	if !ok || kty != "EC" {
		return nil, fmt.Errorf("invalid key type: %v (expected EC)", jwk["kty"])
	}

	// Validate curve
	crv, ok := jwk["crv"].(string)
	if !ok || crv != "P-256" {
		return nil, fmt.Errorf("invalid curve: %v (expected P-256)", jwk["crv"])
	}

	// Extract and decode x coordinate
	xStr, ok := jwk["x"].(string)
	if !ok {
		return nil, errors.New("missing or invalid x coordinate")
	}
	xBytes, err := base64.RawURLEncoding.DecodeString(xStr)
	if err != nil {
		return nil, fmt.Errorf("failed to decode x coordinate: %w", err)
	}

	// Extract and decode y coordinate
	yStr, ok := jwk["y"].(string)
	if !ok {
		return nil, errors.New("missing or invalid y coordinate")
	}
	yBytes, err := base64.RawURLEncoding.DecodeString(yStr)
	if err != nil {
		return nil, fmt.Errorf("failed to decode y coordinate: %w", err)
	}

	// Create ECDSA public key
	return &ecdsa.PublicKey{
		Curve: elliptic.P256(),
		X:     new(big.Int).SetBytes(xBytes),
		Y:     new(big.Int).SetBytes(yBytes),
	}, nil
}

// syncMapJWKCache implements jwkCache using sync.Map
type syncMapJWKCache struct {
	cache sync.Map
}

// newSyncMapJWKCache creates a new thread-safe JWK cache
func newSyncMapJWKCache() jwkCache {
	return &syncMapJWKCache{}
}

func (c *syncMapJWKCache) Get(kid string) (*ecdsa.PublicKey, bool) {
	value, ok := c.cache.Load(kid)
	if !ok {
		return nil, false
	}
	key, ok := value.(*ecdsa.PublicKey)
	return key, ok
}

func (c *syncMapJWKCache) Set(kid string, key *ecdsa.PublicKey) {
	c.cache.Store(kid, key)
}

func (c *syncMapJWKCache) Delete(kid string) {
	c.cache.Delete(kid)
}
