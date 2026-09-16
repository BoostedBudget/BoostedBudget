// Package middleware provides Huma-compatible middleware for the BoostedBudget API.
package middleware

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
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
	_, _ = fmt.Fprintf(ctx.BodyWriter(),
		`{"status":%d,"title":"%s","detail":"%s"}`,
		status, http.StatusText(status), message,
	)
}

// SubscriptionStatusReader can look up a user's EFFECTIVE subscription status (override-aware;
// Issue #1413) by internal user ID. Used by the Clerk middleware to store subscription status
// in context for read-only enforcement (#930).
type SubscriptionStatusReader interface {
	GetEffectiveSubscriptionStatusByUserID(ctx context.Context, userID string) (string, error)
}

// OAuthTokenVerifier verifies a Clerk OAuth access token (opaque or JWT) with Clerk
// and returns the authenticated Clerk user ID (the token "subject"). Implementations
// must return an error for missing, invalid, expired, or unverifiable tokens.
type OAuthTokenVerifier interface {
	VerifyAccessToken(ctx context.Context, token string) (clerkUserID string, err error)
}

// MCPAuthConfig enables a service-to-service auth branch for the MCP Worker (#1247).
//
// Defense-in-depth — BOTH factors are required:
//  1. A valid X-MCP-Worker-Secret proves the request came from our trusted Worker
//     (and selects this branch).
//  2. The forwarded Clerk OAuth access token is verified WITH Clerk to prove the end
//     user actually authorized.
//
// A leaked WorkerSecret alone therefore cannot impersonate a user — the caller must
// also possess that user's valid, Clerk-verifiable OAuth token.
type MCPAuthConfig struct {
	WorkerSecret string
	Verifier     OAuthTokenVerifier
}

func (m *MCPAuthConfig) enabled() bool {
	return m != nil && m.WorkerSecret != "" && m.Verifier != nil
}

// matchesWorkerSecret constant-time compares the presented secret. Fail-closed: returns
// false when MCP auth is not fully configured or the presented secret is empty.
func (m *MCPAuthConfig) matchesWorkerSecret(presented string) bool {
	if !m.enabled() || presented == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(presented), []byte(m.WorkerSecret)) == 1
}

// clerkOAuthVerifyURL is Clerk's REST endpoint for verifying OAuth access tokens
// (handles both opaque and JWT formats). Auth: Bearer <secret key>.
const clerkOAuthVerifyURL = "https://api.clerk.com/oauth_applications/access_tokens/verify"

// clerkOAuthVerifier is the production OAuthTokenVerifier. It calls Clerk's verify
// endpoint and caches token→userID for a short TTL to avoid a Clerk round-trip on
// every MCP API request (opaque tokens are stable for the session).
type clerkOAuthVerifier struct {
	secretKey  string
	httpClient *http.Client
	ttl        time.Duration

	mu    sync.Mutex
	cache map[string]oauthCacheEntry
}

type oauthCacheEntry struct {
	userID string
	expiry time.Time
}

// oauthCacheMaxEntries bounds the verifier cache so it cannot grow without limit
// (e.g. under token spraying). When exceeded, expired entries are pruned and, if
// still over the cap, the cache is reset — it is only a latency optimization.
const oauthCacheMaxEntries = 10000

// tokenCacheKey returns a non-reversible key so raw bearer tokens are never
// retained in memory as map keys.
func tokenCacheKey(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// NewClerkOAuthVerifier builds the production verifier using the Clerk secret key.
func NewClerkOAuthVerifier(secretKey string) *clerkOAuthVerifier {
	return &clerkOAuthVerifier{
		secretKey:  secretKey,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		ttl:        60 * time.Second,
		cache:      make(map[string]oauthCacheEntry),
	}
}

// VerifyAccessToken verifies the token with Clerk and returns the subject (Clerk user ID).
func (v *clerkOAuthVerifier) VerifyAccessToken(ctx context.Context, token string) (string, error) {
	if token == "" {
		return "", errors.New("empty access token")
	}

	now := time.Now()
	key := tokenCacheKey(token)
	v.mu.Lock()
	if e, ok := v.cache[key]; ok && now.Before(e.expiry) {
		v.mu.Unlock()
		return e.userID, nil
	}
	v.mu.Unlock()

	reqBody, _ := json.Marshal(map[string]string{"access_token": token})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, clerkOAuthVerifyURL, bytes.NewReader(reqBody))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+v.secretKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := v.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("clerk oauth verify request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("clerk oauth verify: unexpected status %d", resp.StatusCode)
	}

	// Clerk uses "subject" for the principal user ID across its token APIs
	// (M2MToken, APIKey); accept "user_id" as a defensive fallback.
	var out struct {
		Subject string `json:"subject"`
		UserID  string `json:"user_id"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return "", fmt.Errorf("clerk oauth verify: decode response: %w", err)
	}
	userID := out.Subject
	if userID == "" {
		userID = out.UserID
	}
	if userID == "" {
		return "", errors.New("clerk oauth verify: no subject in response")
	}

	v.mu.Lock()
	if len(v.cache) >= oauthCacheMaxEntries {
		for k, e := range v.cache {
			if !now.Before(e.expiry) {
				delete(v.cache, k)
			}
		}
		if len(v.cache) >= oauthCacheMaxEntries {
			v.cache = make(map[string]oauthCacheEntry)
		}
	}
	v.cache[key] = oauthCacheEntry{userID: userID, expiry: now.Add(v.ttl)}
	v.mu.Unlock()

	return userID, nil
}

// NewClerkMiddleware creates a Huma middleware for Clerk authentication.
//
// It validates Clerk session JWTs and resolves Clerk identities to internal user UUIDs.
// When mcpAuth is configured (non-nil with a WorkerSecret and Verifier), requests that
// carry a valid X-MCP-Worker-Secret are authenticated via the MCP-origin branch instead:
// the forwarded Clerk OAuth access token is verified with Clerk (see MCPAuthConfig).
//
// Parameters:
//   - secretKey: The Clerk secret key (sk_test_... or sk_live_...)
//   - identityRepo: Repository for looking up user identities
//   - registrationService: Service for auto-registering new users
//   - logger: structured logger (nil → slog.Default())
//   - mcpAuth: MCP service-to-service auth config (nil → JWT-only)
//   - idResolver: optional cached fast path for resolving clerkUserID -> internal user UUID
//     (Issue #1531 Task 10a). nil disables the fast path — every request falls through to
//     identityRepo.GetByClerkID directly, exactly as before this parameter existed. When
//     non-nil, a successful resolution skips the (uncached) full-identity repository call
//     entirely; a resolver error OTHER than repository.ErrIdentityNotFound fails the request
//     the same way an identityRepo error always has. ErrIdentityNotFound falls through to the
//     existing identityRepo-driven auto-registration flow unchanged.
//   - profileRepo: Repository for reading the effective subscription status (nil = skip status check)
//
// The middleware stores: user_id, clerk_user_id, claims (JWT path only),
// the effective subscription_status on lookup success, or subscription_status_unresolved=true
// on lookup failure (Issue #1413) so the read-only middleware can fail closed.
func NewClerkMiddleware(
	secretKey string,
	identityRepo repository.UserIdentityRepository,
	registrationService services.RegistrationService,
	logger *slog.Logger,
	mcpAuth *MCPAuthConfig,
	idResolver services.InternalUserIDResolver,
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

	// attach resolves a Clerk user ID to the internal user, stores auth context, and
	// continues the chain. Shared by the JWT path and the MCP-origin path.
	// claims is nil for the MCP path (OAuth token, not a session JWT).
	attach := func(ctx huma.Context, next func(huma.Context), clerkUserID string, claims *clerk.SessionClaims) {
		// Issue #1531 Task 10a: try the cached scalar-UUID fast path first, when configured. A
		// hit sets userID and skips the (uncached, full-identity, decrypts email)
		// identityRepo.GetByClerkID call below entirely. A miss (ErrIdentityNotFound) falls
		// through to the unchanged identityRepo-driven lookup/registration flow; any other
		// resolver error is treated exactly like an identityRepo error always has been.
		var userID string
		if idResolver != nil {
			resolvedID, resolveErr := idResolver.ResolveInternalUserID(ctx.Context(), clerkUserID)
			switch {
			case resolveErr == nil:
				userID = resolvedID.String()
			case errors.Is(resolveErr, repository.ErrIdentityNotFound):
				// Fall through to the identityRepo-driven flow below, unchanged.
			default:
				logger.Error("identity lookup error", "clerk_user_id", clerkUserID, "error", resolveErr)
				writeClerkJSONError(ctx, http.StatusInternalServerError, "Failed to authenticate user")
				return
			}
		}

		if userID == "" {
			identity, err := identityRepo.GetByClerkID(ctx.Context(), clerkUserID)
			if err != nil {
				if err == repository.ErrIdentityNotFound {
					// Auto-register new user
					email := ""
					if claims != nil {
						email = auth.ExtractEmailFromClaims(claims)
					}
					// FALLBACK: fetch email from Clerk API when not present in claims
					// (always the case on the MCP path, which has no session claims).
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
						if errors.Is(err, services.ErrRegistrationErasureFenced) {
							logger.Warn("registration refused by durable erasure fence")
							writeClerkJSONError(ctx, http.StatusForbidden, "Account has been erased")
							return
						}
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
			userID = identity.UserID
		}

		// Store user info in context for resolvers
		newCtx := context.WithValue(ctx.Context(), inputs.UserIDKey, userID)
		newCtx = context.WithValue(newCtx, inputs.ClerkUserIDKey, clerkUserID)
		if claims != nil {
			newCtx = context.WithValue(newCtx, inputs.ClerkClaimsKey, claims)
		}
		newCtx = observability.WithUserID(newCtx, userID)

		// #930: Store the EFFECTIVE subscription status for read-only enforcement middleware.
		// Applies to BOTH paths so MCP writes cannot bypass read-only enforcement.
		//
		// #1413: On lookup failure, mark the status as unresolved (in addition to the
		// existing warning log) rather than leaving the context key simply absent. The
		// read-only middleware fails closed on SubscriptionStatusUnresolvedKey but fails
		// open when the status key is absent AND unresolved was never set — the latter is
		// the "no status reader configured" case, which must keep passing through.
		if subStatusRepo != nil {
			if subStatus, subErr := subStatusRepo.GetEffectiveSubscriptionStatusByUserID(newCtx, userID); subErr == nil {
				newCtx = context.WithValue(newCtx, inputs.EffectiveSubscriptionStatusKey, subStatus)
			} else {
				logger.Warn("failed to fetch subscription status", "user_id", userID, "error", subErr)
				newCtx = context.WithValue(newCtx, inputs.SubscriptionStatusUnresolvedKey, true)
			}
		}

		// Update last_used_at asynchronously
		go func() {
			updateCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			// Error deliberately discarded: a best-effort, fire-and-forget timestamp write on a
			// goroutine with no caller to return to. It must never affect authentication, and
			// there is nothing actionable to do with a failure. Written as an explicit `_ =` so
			// the discard reads as intentional rather than an oversight (errcheck flagged the
			// bare call). Note this column is also why UpdateLastUsed is an explicit
			// NoCacheImpact entry: no reader on the auth path consumes it, and bumping the
			// identity generation here would defeat the cache with one write per request.
			_ = identityRepo.UpdateLastUsed(updateCtx, clerkUserID)
		}()

		observability.AuthAttempts.WithLabelValues("success").Inc()
		next(huma.WithContext(ctx, newCtx))
	}

	return func(ctx huma.Context, next func(huma.Context)) {
		// Skip auth for routes without Security annotation (public routes)
		// This allows public routes like /health, /metrics, /docs to work without auth
		if len(ctx.Operation().Security) == 0 {
			next(ctx)
			return
		}

		// MCP-origin branch (#1247): trusted Worker proven by X-MCP-Worker-Secret, with
		// the user proven by Clerk-verifying the forwarded OAuth access token.
		if mcpAuth.matchesWorkerSecret(ctx.Header("X-MCP-Worker-Secret")) {
			authHeader := ctx.Header("Authorization")
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || parts[0] != "Bearer" || parts[1] == "" {
				observability.AuthAttempts.WithLabelValues("mcp_invalid").Inc()
				writeClerkJSONError(ctx, http.StatusUnauthorized, "Missing or invalid MCP bearer token")
				return
			}

			verifyCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			clerkUserID, err := mcpAuth.Verifier.VerifyAccessToken(verifyCtx, parts[1])
			if err != nil {
				logger.Warn("MCP OAuth token verify failed", "error", err)
				observability.AuthAttempts.WithLabelValues("mcp_invalid").Inc()
				writeClerkJSONError(ctx, http.StatusUnauthorized, "Invalid or expired token")
				return
			}
			if clerkUserID == "" {
				writeClerkJSONError(ctx, http.StatusUnauthorized, "Missing user ID in token")
				return
			}
			attach(ctx, next, clerkUserID, nil)
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

		attach(ctx, next, clerkUserID, sessionClaims)
	}
}
