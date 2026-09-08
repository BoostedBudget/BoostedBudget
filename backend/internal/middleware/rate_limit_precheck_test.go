package middleware_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/middleware"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Cost circuit breaker for the rate limiter (2026-09-08 Cloudflare/cost review).
//
// IPRateLimiter sits immediately after CloudflareOnly in main.go's chain, and it consults the
// Redis-backed Backend on EVERY request — including the ones it goes on to reject. Redis is billed
// per command, so rate limiting was a per-request toll on hostile traffic rather than protection
// from it: a flood at an endpoint with no Cloudflare rate-limit rule (notably /webhooks/, which is
// deliberately un-limited at the edge so real provider deliveries are never dropped) cost one
// billable command per request, unbounded.
//
// PrecheckLimit adds an in-process token bucket consulted BEFORE Backend. Once a single client
// identity exhausts it, further requests are rejected without touching Backend at all, so the
// billable cost per identity per window is bounded instead of unbounded.
//
// THE CORRECTNESS INVARIANT, and the reason this is not simply "reuse the existing memory
// backend": the pre-check must NEVER deny a request the authoritative Backend would have allowed.
// The existing per-tier memory stores are built from cfg.IPLimit, which main.go already passes
// FLEET-DIVIDED (RateLimitConfig.IPLimit's doc comment) so that a Redis outage degrades to each
// machine enforcing its share. Reusing those stores as a pre-check would reject traffic well
// under the authoritative fleet-wide limit. PrecheckLimit is therefore a separate, UNDIVIDED
// budget, and TestIPRateLimiter_PrecheckNeverDeniesWithinTheAuthoritativeBudget pins that.

func newEchoWithPrecheck(cfg middleware.RateLimitConfig) *echo.Echo {
	e := echo.New()
	e.Use(middleware.IPRateLimiter(cfg))
	handler := func(c echo.Context) error { return c.String(http.StatusOK, "ok") }
	e.GET("/test", handler)
	e.POST("/webhooks/plaid", handler)
	e.GET("/health", handler)
	return e
}

func requestTo(e *echo.Echo, method, path, ip string) int {
	req := httptest.NewRequest(method, path, nil)
	req.RemoteAddr = ip + ":12345"
	rr := httptest.NewRecorder()
	e.ServeHTTP(rr, req)
	return rr.Code
}

func allowAllBackend() *fakeBackend {
	return &fakeBackend{name: "redis", allowFunc: func(context.Context, domain.RateLimitTier, string) (domain.RateLimitDecision, error) {
		return domain.RateLimitDecision{Allowed: true}, nil
	}}
}

// The motivating defect: a flood costs one Backend call per request, forever.
func TestIPRateLimiter_PrecheckBoundsBackendCallsUnderAFlood(t *testing.T) {
	backend := allowAllBackend()
	e := newEchoWithPrecheck(middleware.RateLimitConfig{
		IPLimit:        1000, // high, so the memory tier never denies first
		WindowDuration: time.Minute,
		PrecheckLimit:  5,
		Backend:        backend,
	})

	for i := 0; i < 200; i++ {
		requestTo(e, http.MethodPost, "/webhooks/plaid", "203.0.113.9")
	}

	assert.LessOrEqual(t, backend.callCount(), 5,
		"200 requests from one identity must cost at most PrecheckLimit backend calls, not 200")
	assert.Positive(t, backend.callCount(), "the pre-check must not block the backend entirely")
}

// The invariant that makes the optimisation safe. PrecheckLimit is the UNDIVIDED authoritative
// budget, so every request inside it must still reach Backend and be allowed.
func TestIPRateLimiter_PrecheckNeverDeniesWithinTheAuthoritativeBudget(t *testing.T) {
	backend := allowAllBackend()
	e := newEchoWithPrecheck(middleware.RateLimitConfig{
		IPLimit:        1000,
		WindowDuration: time.Minute,
		PrecheckLimit:  50,
		Backend:        backend,
	})

	for i := 0; i < 50; i++ {
		require.Equal(t, http.StatusOK, requestTo(e, http.MethodGet, "/test", "203.0.113.10"),
			"request %d is within PrecheckLimit and must not be denied by the pre-check", i+1)
	}
	assert.Equal(t, 50, backend.callCount(), "every in-budget request must still reach the backend")
}

// Backward compatibility: every existing call site and test constructs RateLimitConfig without
// this field. Unset must mean "no pre-check", i.e. exactly today's behaviour.
func TestIPRateLimiter_PrecheckDisabledWhenUnset(t *testing.T) {
	backend := allowAllBackend()
	e := newEchoWithPrecheck(middleware.RateLimitConfig{
		IPLimit:        1000,
		WindowDuration: time.Minute,
		Backend:        backend,
	})

	for i := 0; i < 100; i++ {
		requestTo(e, http.MethodGet, "/test", "203.0.113.11")
	}
	assert.Equal(t, 100, backend.callCount(),
		"with PrecheckLimit unset the backend must be consulted on every request, as before")
}

// The pre-check is per identity, not global: one abusive client must not deny service to others.
func TestIPRateLimiter_PrecheckIsPerIdentity(t *testing.T) {
	backend := allowAllBackend()
	e := newEchoWithPrecheck(middleware.RateLimitConfig{
		IPLimit:        1000,
		WindowDuration: time.Minute,
		PrecheckLimit:  3,
		Backend:        backend,
	})

	for i := 0; i < 50; i++ {
		requestTo(e, http.MethodPost, "/webhooks/plaid", "203.0.113.12") // exhausts its own budget
	}
	assert.Equal(t, http.StatusOK, requestTo(e, http.MethodGet, "/test", "198.51.100.7"),
		"a different identity must be unaffected by another's exhausted pre-check budget")
}

// A pre-check rejection is a real rate-limit rejection: it must carry Retry-After, like any other.
func TestIPRateLimiter_PrecheckRejectionCarriesRetryAfter(t *testing.T) {
	backend := allowAllBackend()
	e := newEchoWithPrecheck(middleware.RateLimitConfig{
		IPLimit:        1000,
		WindowDuration: time.Minute,
		PrecheckLimit:  2,
		Backend:        backend,
	})

	for i := 0; i < 2; i++ {
		requestTo(e, http.MethodGet, "/test", "203.0.113.13")
	}

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.RemoteAddr = "203.0.113.13:12345"
	rr := httptest.NewRecorder()
	e.ServeHTTP(rr, req)

	require.Equal(t, http.StatusTooManyRequests, rr.Code)
	assert.NotEmpty(t, rr.Header().Get("Retry-After"),
		"a pre-check rejection must still tell the client when to retry")
}

// Exempt paths (/health) already bypass Backend entirely via AllowExempt. The pre-check must not
// change that, and must not start rejecting Fly's liveness probes.
func TestIPRateLimiter_PrecheckLeavesExemptPathsAlone(t *testing.T) {
	e := newEchoWithPrecheck(middleware.RateLimitConfig{
		IPLimit:         1000,
		ExemptPathLimit: 1000,
		WindowDuration:  time.Minute,
		PrecheckLimit:   2,
		Backend:         &panickingBackend{}, // proves the backend is never touched at all
	})

	for i := 0; i < 20; i++ {
		assert.Equal(t, http.StatusOK, requestTo(e, http.MethodGet, "/health", "203.0.113.14"),
			"health probe %d must not be rate limited by the pre-check", i+1)
	}
}
