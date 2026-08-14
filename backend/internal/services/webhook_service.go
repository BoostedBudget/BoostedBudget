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
	"net/http"
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

// ErrWebhookVerifierUnavailable indicates the webhook signature could not be verified because the
// verifier itself failed (e.g. fetching or parsing Plaid's JWK) — NOT because the signature was
// cryptographically wrong. Issue #1514: this must be treated as retryable (ask the caller to try
// again later), never as evidence of a forged request. Callers should check for this with
// errors.Is before treating a VerifyWebhook failure as a rejected/invalid signature.
var ErrWebhookVerifierUnavailable = errors.New("plaid webhook verifier temporarily unavailable")

// jwkFetcherFunc abstracts the Plaid API call used to fetch a JWK by kid. This is a seam so tests
// can simulate a verifier outage (network/API failure) or a malformed key response without a live
// Plaid client or network access — see webhookService.getJWKPublicKey.
//
// Issue #1514 (Codex round-1 finding): the returned bool reports whether a non-nil error is
// retryable (a genuine verifier outage) as opposed to a deterministic rejection. The kid comes
// from the JWT header, which is attacker-controlled and unverified at this point — a bogus kid
// makes Plaid's API return a definite "no such key" (4xx), which is NOT a verifier outage and
// must not be classified as ErrWebhookVerifierUnavailable, or a forged request would be
// misclassified as retryable and skip the invalid-attempt audit signal entirely.
type jwkFetcherFunc func(ctx context.Context, kid string) (jwk map[string]interface{}, retryable bool, err error)

// webhookService implements WebhookService
type webhookService struct {
	plaidClient *plaid.APIClient
	jwkCache    jwkCache
	fetchJWK    jwkFetcherFunc
}

// NewWebhookService creates a new WebhookService instance with JWK caching
func NewWebhookService(plaidClient *plaid.APIClient) WebhookService {
	return &webhookService{
		plaidClient: plaidClient,
		jwkCache:    newSyncMapJWKCache(),
		fetchJWK:    newPlaidJWKFetcher(plaidClient),
	}
}

// newPlaidJWKFetcher returns a jwkFetcherFunc backed by the real Plaid API.
func newPlaidJWKFetcher(plaidClient *plaid.APIClient) jwkFetcherFunc {
	return func(ctx context.Context, kid string) (map[string]interface{}, bool, error) {
		request := plaid.NewWebhookVerificationKeyGetRequest(kid)
		resp, httpResp, err := plaidClient.PlaidApi.WebhookVerificationKeyGet(ctx).
			WebhookVerificationKeyGetRequest(*request).Execute()
		if err != nil {
			// Issue #1514: only classify as retryable when there's no evidence Plaid actually
			// rejected the request outright — a missing response (network/timeout failure) or a
			// 5xx means the verifier itself is unhealthy. A 4xx (e.g. "no such verification key")
			// is Plaid deterministically telling us the attacker-controlled kid is bogus — that's
			// the request being invalid, not the verifier being unavailable.
			retryable := httpResp == nil || httpResp.StatusCode >= http.StatusInternalServerError
			return nil, retryable, fmt.Errorf("failed to fetch JWK from Plaid: %w", err)
		}

		jwk := resp.GetKey()
		return map[string]interface{}{
			"kty": jwk.GetKty(),
			"crv": jwk.GetCrv(),
			"x":   jwk.GetX(),
			"y":   jwk.GetY(),
			"kid": jwk.GetKid(),
			"use": jwk.GetUse(),
			"alg": jwk.GetAlg(),
		}, false, nil
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

	// Step 5: Validate iat claim (must be within 5 minutes of the server clock, in EITHER
	// direction).
	//
	// Issue #1514 Part B: symmetric with paddle_webhook_service.go's checkTimestampTolerance.
	// golang-jwt/v5's own iat check is opt-in via the WithIssuedAt() parser option (see
	// validator.go: verifyIat defaults to false), and the jwt.Parse call above does not pass it
	// — so nothing else in this verification path bounds iat. A stale iat is a replay risk, and
	// an unbounded future iat was previously accepted outright (this asymmetry was confirmed by
	// reading golang-jwt's validator source, not assumed).
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return false, errors.New("failed to extract claims")
	}

	iatInterface, ok := claims["iat"]
	if !ok {
		return false, errors.New("missing iat claim")
	}

	var iatUnix int64
	switch v := iatInterface.(type) {
	case float64:
		iatUnix = int64(v)
	case int64:
		iatUnix = v
	default:
		return false, fmt.Errorf("invalid iat type: %T", iatInterface)
	}

	if err := checkIatTolerance(iatUnix); err != nil {
		return false, err
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

// iatTolerance bounds how far a Plaid webhook JWT's iat claim may drift from the server clock,
// in either direction (issue #1514 Part B).
const iatTolerance = 5 * time.Minute

// minSaneIatUnixTimestamp is 2000-01-01T00:00:00Z in Unix seconds. checkIatTolerance uses it as a
// floor on iat well below any value this service should ever legitimately see, mirroring
// paddle_webhook_service.go's minSaneUnixTimestamp.
const minSaneIatUnixTimestamp int64 = 946684800

// iatEpochBoundSlackSeconds pads the upper sane-epoch bound past now+iatTolerance so the pre-check
// in checkIatTolerance never fights with the exact ±tolerance comparison that follows it — it only
// exists to keep issuedAt inside a range where time.Time arithmetic (Sub) cannot overflow, not to
// do the real tolerance decision itself. Mirrors paddle_webhook_service.go's
// epochBoundSlackSeconds.
const iatEpochBoundSlackSeconds int64 = 86400

// checkIatTolerance rejects iatUnix if it is farther than iatTolerance from time.Now() in EITHER
// direction. Symmetric by design (issue #1514 Part B), matching
// paddle_webhook_service.go's checkTimestampTolerance: a stale iat is a replay risk, and an
// unbounded future iat is just as dangerous (nothing else in this verification path bounds it —
// see the Step 5 comment in VerifyWebhook) and must be rejected too.
//
// Like checkTimestampTolerance, iatUnix is bounded against a sane epoch range in int64-seconds
// space BEFORE any time.Time/Duration arithmetic runs, so an extreme attacker-supplied iat cannot
// hit the same saturate-then-negate overflow that checkTimestampTolerance's doc comment describes
// (a time.Duration saturates instead of overflowing for deltas that don't fit in an int64
// nanosecond count, and negating math.MinInt64 does not produce a positive value).
func checkIatTolerance(iatUnix int64) error {
	now := time.Now()

	maxSaneUnixTimestamp := now.Unix() + int64(iatTolerance.Seconds()) + iatEpochBoundSlackSeconds
	if iatUnix < minSaneIatUnixTimestamp || iatUnix > maxSaneUnixTimestamp {
		return fmt.Errorf("JWT iat %d is outside the sane epoch range [%d, %d]",
			iatUnix, minSaneIatUnixTimestamp, maxSaneUnixTimestamp)
	}

	issuedAt := time.Unix(iatUnix, 0)
	age := now.Sub(issuedAt)

	if age > iatTolerance {
		return fmt.Errorf("JWT too old: issued %v ago (max %s)", age, iatTolerance)
	}
	if age < -iatTolerance {
		return fmt.Errorf("JWT issued in the future: issued %v from now (max %s)", -age, iatTolerance)
	}
	return nil
}

// getJWKPublicKey fetches JWK from Plaid API with caching.
//
// Issue #1514: only a genuine verifier outage (fetch marked retryable, or an unparsable key
// Plaid itself returned) is wrapped in ErrWebhookVerifierUnavailable. A deterministic fetch
// rejection (e.g. the attacker-controlled kid names no real key) is NOT wrapped — it surfaces as
// an ordinary error, which VerifyWebhook's caller correctly treats as "cryptographically
// invalid," preserving the invalid-attempt audit signal for genuinely forged requests.
func (s *webhookService) getJWKPublicKey(ctx context.Context, kid string) (*ecdsa.PublicKey, error) {
	// Check cache first
	if cachedKey, found := s.jwkCache.Get(kid); found {
		return cachedKey, nil
	}

	// Cache miss - fetch from Plaid API (or the test seam)
	jwkMap, retryable, err := s.fetchJWK(ctx, kid)
	if err != nil {
		if retryable {
			return nil, fmt.Errorf("%w: %v", ErrWebhookVerifierUnavailable, err)
		}
		return nil, err
	}

	publicKey, err := parseJWKToPublicKey(jwkMap)
	if err != nil {
		// Plaid returned a 200 we couldn't use — that's a verifier/schema problem, not evidence
		// the request is forged.
		return nil, fmt.Errorf("%w: failed to parse JWK: %v", ErrWebhookVerifierUnavailable, err)
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
