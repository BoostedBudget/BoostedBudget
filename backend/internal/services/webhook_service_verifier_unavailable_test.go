package services

import (
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// Issue #1514: distinguish "verifier temporarily unavailable" (e.g. the Plaid JWK fetch failed —
// retryable, must NOT be treated as a forged request) from "signature is cryptographically
// invalid" (reject). These tests exercise the fetchJWK seam directly, without a live Plaid client
// or network access.

var errFakeJWKFetchDown = errors.New("plaid api unreachable")
var errFakeJWKNotFound = errors.New("no verification key found for this kid")

func TestVerifyWebhook_JWKFetchFails_ReturnsVerifierUnavailable(t *testing.T) {
	privateKey, _ := generateTestKeyPair(t)
	kid := "unavailable-key"
	body := []byte(`{"webhook_type":"TRANSACTIONS"}`)
	bodyHash := sha256.Sum256(body)
	bodyHashHex := fmt.Sprintf("%x", bodyHash)

	jwtString := createTestJWT(t, privateKey, kid, bodyHashHex, time.Now())

	service := &webhookService{
		jwkCache: &mockJWKCache{}, // cache miss forces a fetch
		fetchJWK: func(ctx context.Context, kid string) (map[string]interface{}, bool, error) {
			return nil, true, errFakeJWKFetchDown // retryable: e.g. a network/5xx failure
		},
	}

	valid, err := service.VerifyWebhook(context.Background(), body, jwtString)

	assert.False(t, valid)
	assert.ErrorIs(t, err, ErrWebhookVerifierUnavailable, "a retryable JWK fetch failure must surface as verifier-unavailable, not a forgery")
}

// TestVerifyWebhook_JWKFetchRejected_IsNotVerifierUnavailable is Issue #1514 Codex round-1
// finding (Medium): the kid in the JWT header is attacker-controlled and unverified at this
// point. When Plaid deterministically rejects it (e.g. "no such verification key" — a 4xx, not a
// network/5xx failure), that is the request being invalid, not the verifier being unavailable.
// Misclassifying this as retryable would let a forged request skip the invalid-attempt audit
// signal (recordInvalidWebhookAttempt) entirely.
func TestVerifyWebhook_JWKFetchRejected_IsNotVerifierUnavailable(t *testing.T) {
	privateKey, _ := generateTestKeyPair(t)
	kid := "attacker-supplied-bogus-kid"
	body := []byte(`{"webhook_type":"TRANSACTIONS"}`)
	bodyHash := sha256.Sum256(body)
	bodyHashHex := fmt.Sprintf("%x", bodyHash)
	jwtString := createTestJWT(t, privateKey, kid, bodyHashHex, time.Now())

	service := &webhookService{
		jwkCache: &mockJWKCache{},
		fetchJWK: func(ctx context.Context, kid string) (map[string]interface{}, bool, error) {
			return nil, false, errFakeJWKNotFound // not retryable: Plaid returned 4xx "no such key"
		},
	}

	valid, err := service.VerifyWebhook(context.Background(), body, jwtString)

	assert.False(t, valid)
	assert.NotErrorIs(t, err, ErrWebhookVerifierUnavailable, "a deterministic fetch rejection (bogus kid) must NOT be classified as verifier-unavailable")
}

func TestVerifyWebhook_JWKParseFails_ReturnsVerifierUnavailable(t *testing.T) {
	privateKey, _ := generateTestKeyPair(t)
	kid := "malformed-key"
	body := []byte(`{"webhook_type":"ITEM"}`)
	bodyHash := sha256.Sum256(body)
	bodyHashHex := fmt.Sprintf("%x", bodyHash)
	jwtString := createTestJWT(t, privateKey, kid, bodyHashHex, time.Now())

	service := &webhookService{
		jwkCache: &mockJWKCache{},
		fetchJWK: func(ctx context.Context, kid string) (map[string]interface{}, bool, error) {
			// Missing required x/y coordinates -> parseJWKToPublicKey fails.
			return map[string]interface{}{"kty": "EC", "crv": "P-256"}, false, nil
		},
	}

	valid, err := service.VerifyWebhook(context.Background(), body, jwtString)

	assert.False(t, valid)
	assert.ErrorIs(t, err, ErrWebhookVerifierUnavailable, "an unparsable JWK must surface as verifier-unavailable, not a forgery")
}

func TestVerifyWebhook_BadSignature_IsNotVerifierUnavailable(t *testing.T) {
	// Sanity check: a genuinely bad signature (existing TestVerifyWebhook_InvalidSignature
	// scenario) must NOT be classified as verifier-unavailable — only fetch/parse failures are.
	privateKey1, _ := generateTestKeyPair(t)
	_, publicKey2 := generateTestKeyPair(t)
	kid := "mismatched-key"

	body := []byte(`{"webhook_type":"TRANSACTIONS"}`)
	bodyHash := sha256.Sum256(body)
	bodyHashHex := fmt.Sprintf("%x", bodyHash)
	jwtString := createTestJWT(t, privateKey1, kid, bodyHashHex, time.Now())

	svc := &webhookService{
		jwkCache: &mockJWKCache{jwks: map[string]*ecdsa.PublicKey{kid: publicKey2}},
	}

	valid, err := svc.VerifyWebhook(context.Background(), body, jwtString)

	assert.False(t, valid)
	assert.NotErrorIs(t, err, ErrWebhookVerifierUnavailable)
}
