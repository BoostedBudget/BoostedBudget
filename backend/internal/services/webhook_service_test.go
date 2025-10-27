package services

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TDD Phase 4: Webhook Service Tests - JWT Verification
// Issue #202: Plaid Webhook Integration
// Following RED-GREEN-REFACTOR: Write comprehensive tests first

// Test helper: Generate ECDSA key pair for testing
func generateTestKeyPair(t *testing.T) (*ecdsa.PrivateKey, *ecdsa.PublicKey) {
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	return privateKey, &privateKey.PublicKey
}

// Test helper: Create valid JWT with ES256
func createTestJWT(t *testing.T, privateKey *ecdsa.PrivateKey, kid string, bodyHash string, issuedAt time.Time) string {
	token := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.MapClaims{
		"iat":                   issuedAt.Unix(),
		"request_body_sha256":   bodyHash,
	})
	token.Header["kid"] = kid

	tokenString, err := token.SignedString(privateKey)
	require.NoError(t, err)
	return tokenString
}

// Test helper: Create JWK from ECDSA public key
func publicKeyToJWK(t *testing.T, pub *ecdsa.PublicKey, kid string) map[string]interface{} {
	// Convert coordinates to base64url
	xBytes := pub.X.Bytes()
	yBytes := pub.Y.Bytes()

	// Pad to 32 bytes for P-256
	xPadded := make([]byte, 32)
	yPadded := make([]byte, 32)
	copy(xPadded[32-len(xBytes):], xBytes)
	copy(yPadded[32-len(yBytes):], yBytes)

	return map[string]interface{}{
		"kty": "EC",
		"crv": "P-256",
		"x":   base64.RawURLEncoding.EncodeToString(xPadded),
		"y":   base64.RawURLEncoding.EncodeToString(yPadded),
		"kid": kid,
		"use": "sig",
		"alg": "ES256",
	}
}

func TestVerifyWebhook_ValidSignature(t *testing.T) {
	// Generate test key pair
	privateKey, publicKey := generateTestKeyPair(t)
	kid := "test-key-id-123"

	// Create test body and compute hash
	body := []byte(`{"webhook_type":"TRANSACTIONS","webhook_code":"DEFAULT_UPDATE"}`)
	bodyHash := sha256.Sum256(body)
	bodyHashHex := fmt.Sprintf("%x", bodyHash)

	// Create valid JWT
	jwtString := createTestJWT(t, privateKey, kid, bodyHashHex, time.Now())

	// Create service with mock JWK fetcher
	jwkCache := &mockJWKCache{
		jwks: map[string]*ecdsa.PublicKey{
			kid: publicKey,
		},
	}
	service := &webhookService{
		jwkCache: jwkCache,
	}

	// Execute
	valid, err := service.VerifyWebhook(context.Background(), body, jwtString)

	// Assert
	assert.NoError(t, err)
	assert.True(t, valid)
}

func TestVerifyWebhook_InvalidSignature(t *testing.T) {
	// Generate two different key pairs
	privateKey1, _ := generateTestKeyPair(t)
	_, publicKey2 := generateTestKeyPair(t)
	kid := "test-key-id"

	// Sign with key 1
	body := []byte(`{"webhook_type":"TRANSACTIONS"}`)
	bodyHash := sha256.Sum256(body)
	bodyHashHex := fmt.Sprintf("%x", bodyHash)
	jwtString := createTestJWT(t, privateKey1, kid, bodyHashHex, time.Now())

	// Verify with key 2 (should fail)
	jwkCache := &mockJWKCache{
		jwks: map[string]*ecdsa.PublicKey{
			kid: publicKey2,
		},
	}
	service := &webhookService{
		jwkCache: jwkCache,
	}

	// Execute
	valid, err := service.VerifyWebhook(context.Background(), body, jwtString)

	// Assert - signature verification should fail
	assert.Error(t, err)
	assert.False(t, valid)
	assert.Contains(t, err.Error(), "JWT verification failed")
}

func TestVerifyWebhook_MissingSignature(t *testing.T) {
	service := &webhookService{
		jwkCache: &mockJWKCache{},
	}
	body := []byte(`{"webhook_type":"TRANSACTIONS"}`)

	// Execute with empty signature
	valid, err := service.VerifyWebhook(context.Background(), body, "")

	// Assert
	assert.Error(t, err)
	assert.False(t, valid)
	assert.Contains(t, err.Error(), "signature is empty")
}

func TestVerifyWebhook_ExpiredJWT(t *testing.T) {
	privateKey, publicKey := generateTestKeyPair(t)
	kid := "test-key-id"

	body := []byte(`{"webhook_type":"TRANSACTIONS"}`)
	bodyHash := sha256.Sum256(body)
	bodyHashHex := fmt.Sprintf("%x", bodyHash)

	// Create JWT issued 10 minutes ago (expired)
	expiredTime := time.Now().Add(-10 * time.Minute)
	jwtString := createTestJWT(t, privateKey, kid, bodyHashHex, expiredTime)

	jwkCache := &mockJWKCache{
		jwks: map[string]*ecdsa.PublicKey{
			kid: publicKey,
		},
	}
	service := &webhookService{
		jwkCache: jwkCache,
	}

	// Execute
	valid, err := service.VerifyWebhook(context.Background(), body, jwtString)

	// Assert - should fail due to age
	assert.Error(t, err)
	assert.False(t, valid)
	assert.Contains(t, err.Error(), "JWT too old")
}

func TestVerifyWebhook_WrongAlgorithm(t *testing.T) {
	// Create JWT with HS256 (wrong algorithm)
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"iat":                 time.Now().Unix(),
		"request_body_sha256": "test",
	})
	token.Header["kid"] = "test-key-id"

	jwtString, err := token.SignedString([]byte("secret"))
	require.NoError(t, err)

	service := &webhookService{
		jwkCache: &mockJWKCache{},
	}
	body := []byte(`{"webhook_type":"TRANSACTIONS"}`)

	// Execute
	valid, err := service.VerifyWebhook(context.Background(), body, jwtString)

	// Assert
	assert.Error(t, err)
	assert.False(t, valid)
	assert.Contains(t, err.Error(), "invalid algorithm")
}

func TestVerifyWebhook_MismatchedBodyHash(t *testing.T) {
	privateKey, publicKey := generateTestKeyPair(t)
	kid := "test-key-id"

	// Body and hash don't match
	body := []byte(`{"webhook_type":"TRANSACTIONS","webhook_code":"DEFAULT_UPDATE"}`)
	wrongHash := "0000000000000000000000000000000000000000000000000000000000000000"

	jwtString := createTestJWT(t, privateKey, kid, wrongHash, time.Now())

	jwkCache := &mockJWKCache{
		jwks: map[string]*ecdsa.PublicKey{
			kid: publicKey,
		},
	}
	service := &webhookService{
		jwkCache: jwkCache,
	}

	// Execute
	valid, err := service.VerifyWebhook(context.Background(), body, jwtString)

	// Assert
	assert.Error(t, err)
	assert.False(t, valid)
	assert.Contains(t, err.Error(), "body hash mismatch")
}

func TestVerifyWebhook_MissingKid(t *testing.T) {
	privateKey, _ := generateTestKeyPair(t)

	// Create JWT without kid in header
	token := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.MapClaims{
		"iat":                 time.Now().Unix(),
		"request_body_sha256": "test",
	})
	// Don't set kid

	jwtString, err := token.SignedString(privateKey)
	require.NoError(t, err)

	service := &webhookService{
		jwkCache: &mockJWKCache{},
	}
	body := []byte(`{"webhook_type":"TRANSACTIONS"}`)

	// Execute
	valid, err := service.VerifyWebhook(context.Background(), body, jwtString)

	// Assert
	assert.Error(t, err)
	assert.False(t, valid)
	assert.Contains(t, err.Error(), "missing kid")
}

func TestVerifyWebhook_MalformedJWT(t *testing.T) {
	service := &webhookService{
		jwkCache: &mockJWKCache{},
	}
	body := []byte(`{"webhook_type":"TRANSACTIONS"}`)

	// Execute with malformed JWT
	valid, err := service.VerifyWebhook(context.Background(), body, "not.a.valid.jwt")

	// Assert
	assert.Error(t, err)
	assert.False(t, valid)
}

func TestParseJWK_ValidKey(t *testing.T) {
	_, publicKey := generateTestKeyPair(t)
	kid := "test-key-id"

	// Convert to JWK format
	jwk := publicKeyToJWK(t, publicKey, kid)

	// Execute
	parsedKey, err := parseJWKToPublicKey(jwk)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, parsedKey)
	assert.Equal(t, publicKey.X.Cmp(parsedKey.X), 0)
	assert.Equal(t, publicKey.Y.Cmp(parsedKey.Y), 0)
}

func TestParseJWK_InvalidCurve(t *testing.T) {
	jwk := map[string]interface{}{
		"kty": "EC",
		"crv": "P-384", // Wrong curve (should be P-256)
		"x":   "test",
		"y":   "test",
	}

	// Execute
	_, err := parseJWKToPublicKey(jwk)

	// Assert
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid curve")
}

func TestParseJWK_InvalidKeyType(t *testing.T) {
	jwk := map[string]interface{}{
		"kty": "RSA", // Wrong type (should be EC)
		"crv": "P-256",
		"x":   "test",
		"y":   "test",
	}

	// Execute
	_, err := parseJWKToPublicKey(jwk)

	// Assert
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid key type")
}

// Mock JWK cache for testing
type mockJWKCache struct {
	jwks map[string]*ecdsa.PublicKey
}

func (m *mockJWKCache) Get(kid string) (*ecdsa.PublicKey, bool) {
	key, found := m.jwks[kid]
	return key, found
}

func (m *mockJWKCache) Set(kid string, key *ecdsa.PublicKey) {
	if m.jwks == nil {
		m.jwks = make(map[string]*ecdsa.PublicKey)
	}
	m.jwks[kid] = key
}

func (m *mockJWKCache) Delete(kid string) {
	delete(m.jwks, kid)
}
