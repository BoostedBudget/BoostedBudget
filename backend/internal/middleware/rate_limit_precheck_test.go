package middleware_test

import (
	"context"
	"fmt"
	"log/slog"
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

// newEchoGatedAsCloudflarePassed is newEchoWithPrecheck with every request stamped as having
// cleared the Cloudflare gate. Combined with an absent Cf-Connecting-Ip that is exactly the
// condition clientIdentity answers with identitySentinelFallback, i.e. the sentinel tier.
func newEchoGatedAsCloudflarePassed(cfg middleware.RateLimitConfig) *echo.Echo {
	e := echo.New()
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			middleware.SetGateStatus(c, middleware.GateStatusPassed)
			return next(c)
		}
	})
	e.Use(middleware.IPRateLimiter(cfg))
	handler := func(c echo.Context) error { return c.String(http.StatusOK, "ok") }
	e.GET("/test", handler)
	e.POST("/webhooks/plaid", handler)
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
		// ⚠ This test drives POST /webhooks/plaid, which isAuthEndpoint routes to the AUTH tier.
		// The pre-check budgets are PER TIER and a tier left unset is DISABLED — deliberately not
		// defaulted from another tier — so the auth budget is what bounds this flood. Setting
		// only PrecheckLimit here would leave the backend consulted on all 200 requests, which is
		// exactly what this assertion caught when the per-tier split landed.
		PrecheckAuthLimit: 5,
		Backend:           backend,
	})

	for i := 0; i < 200; i++ {
		requestTo(e, http.MethodPost, "/webhooks/plaid", "203.0.113.9")
	}

	// precheckHeadroomFactor deliberately doubles every pre-check budget above its authoritative
	// counterpart, so the bound is factor x limit. Still bounded — unbounded was the defect.
	assert.LessOrEqual(t, backend.callCount(), 5*2,
		"200 requests from one identity must cost at most precheckHeadroomFactor x the tier's "+
			"pre-check budget in backend calls, not 200")
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

	// Drain the regular tier's pre-check budget. It is precheckHeadroomFactor x PrecheckLimit —
	// the headroom is deliberate (see that constant), so the drain loop must follow it rather
	// than assume parity with the authoritative limit.
	for i := 0; i < 2*2; i++ {
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
	// ⚠ CORRECTED (adversarial review). This previously read
	//     Backend: &panickingBackend{}, // proves the backend is never touched at all
	// and proved no such thing: IPRateLimiter's own defer recover() swallows a panicking backend
	// and fallbackLimiter degrades to memory, which allows — so the 20 x 200 OK loop below would
	// pass identically whether or not /health reached the backend. A status-only assertion
	// structurally cannot observe backend contact here.
	//
	// The log capture is what makes it a real proof, and the pattern is not invented here:
	// TestIPRateLimiter_ExemptPathsNeverTouchThePrimaryBackend (rate_limit_test.go) already
	// discriminates exactly this way, for exactly this reason. Any record at all — including the
	// recovered-panic record — proves the panicking backend was invoked.
	capture := &panicCapturingHandler{}
	e := newEchoWithPrecheck(middleware.RateLimitConfig{
		IPLimit:         1000,
		ExemptPathLimit: 1000,
		WindowDuration:  time.Minute,
		PrecheckLimit:   2,
		Backend:         &panickingBackend{},
		Logger:          slog.New(capture),
	})

	for i := 0; i < 20; i++ {
		assert.Equal(t, http.StatusOK, requestTo(e, http.MethodGet, "/health", "203.0.113.14"),
			"health probe %d must not be rate limited by the pre-check", i+1)
	}

	assert.False(t, capture.sawAnyRecord(),
		"no log record may have been emitted — any record proves the panicking backend was "+
			"actually invoked, i.e. the exempt-path bypass did not run")
}

// ── The pre-check's invariant, falsified three ways (2026-09-08 adversarial review) ───────────
//
// Two independent adversarial reviewers (Codex, and a fresh-context Claude reviewer) each
// EXECUTED reproductions showing the first draft of the pre-check violated its own stated
// invariant — "never deny a request the authoritative Backend would have allowed" — in three
// distinct ways. All three came from the same root cause: ONE bucket, keyed on the RAW identity,
// standing in for THREE independently-budgeted authoritative buckets keyed on a CANONICAL
// identity. The tests below are those reproductions.

// TestIPRateLimiter_PrecheckDoesNotChargeAuthTrafficToTheRegularBudget is the cross-tier
// violation. The Redis backend keeps SEPARATE budgets per tier (redis_backend.go bucketKey
// namespaces by tier), so one identity legitimately has regular+auth allowance. A single shared
// pre-check bucket charges both to one budget and denies inside the authoritative limit.
func TestIPRateLimiter_PrecheckDoesNotChargeAuthTrafficToTheRegularBudget(t *testing.T) {
	backend := allowAllBackend()
	const regularLimit = 2
	e := newEchoWithPrecheck(middleware.RateLimitConfig{
		IPLimit:               1000, // high, so the memory tier never denies first
		WindowDuration:        time.Minute,
		PrecheckLimit:         regularLimit, // regular tier's own budget
		PrecheckAuthLimit:     5,            // auth tier's own, independent budget
		PrecheckSentinelLimit: 10,
		Backend:               backend, // allow-all: the authority permits every request here
	})

	// ⚠ Drain the regular tier's pre-check bucket COMPLETELY — precheckHeadroomFactor tokens per
	// configured unit, not one. An earlier draft of this test sent only `regularLimit` requests
	// and PASSED under the shared-bucket mutant, because the headroom still had tokens left: the
	// test was measuring nothing. A RED is not a RED until the mutant has been observed to fail
	// it.
	for i := 0; i < regularLimit*2; i++ {
		require.Equal(t, http.StatusOK, requestTo(e, http.MethodGet, "/test", "203.0.113.9"),
			"regular request %d must be allowed — the authoritative backend allows everything here", i)
	}

	// An auth-tier request now. Redis would allow it — its auth bucket is untouched.
	assert.Equal(t, http.StatusOK, requestTo(e, http.MethodPost, "/webhooks/plaid", "203.0.113.9"),
		"a 429 here is the cross-tier violation: regular traffic drained a shared pre-check "+
			"bucket and denied a webhook the authoritative auth bucket would have allowed. "+
			"/webhooks/ is exactly the traffic that must never be dropped.")
}

// TestIPRateLimiter_PrecheckBucketsIPv6ByTheSameIdentityRedisDoes is the cost-bound violation.
// It does not test the invariant (per-/128 is strictly MORE permissive, so it cannot deny
// wrongly) — it tests that the fix actually fixes what it claims. The authoritative backend
// aggregates IPv6 to /64; if the pre-check keys on the raw /128, an attacker rotating addresses
// inside one routed /64 gets a fresh full bucket per address and the Redis cost stays unbounded —
// defeating the change for exactly the attacker aggregateIPv6Identity was written about.
func TestIPRateLimiter_PrecheckBucketsIPv6ByTheSameIdentityRedisDoes(t *testing.T) {
	var backendCalls int
	backend := &fakeBackend{name: "redis", allowFunc: func(context.Context, domain.RateLimitTier, string) (domain.RateLimitDecision, error) {
		backendCalls++
		return domain.RateLimitDecision{Allowed: false, RetryAfter: time.Minute}, nil
	}}

	e := newEchoWithPrecheck(middleware.RateLimitConfig{
		IPLimit:               1000,
		WindowDuration:        time.Minute,
		PrecheckLimit:         5,
		PrecheckAuthLimit:     5,
		PrecheckSentinelLimit: 5,
		Backend:               backend,
	})

	// 200 distinct /128 addresses, all inside 2001:db8::/64 — one routed subscriber allocation.
	// ⚠ Bracketed: net.SplitHostPort needs "[v6]:port", and an unbracketed IPv6 RemoteAddr falls
	// through remoteAddrIdentity's unparsed-string branch, making every request its own identity
	// for reasons that have nothing to do with what this test is about.
	for i := 0; i < 200; i++ {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = fmt.Sprintf("[2001:db8::%x]:12345", i+1)
		rr := httptest.NewRecorder()
		e.ServeHTTP(rr, req)
	}

	assert.LessOrEqual(t, backendCalls, 20,
		"the pre-check must bucket by the SAME canonical identity the backend enforces (/64). "+
			"A count near 200 means every rotated /128 minted its own full bucket and the Redis "+
			"cost this change exists to bound is still unbounded — %d calls", backendCalls)
}

// TestIPRateLimiter_PrecheckHonoursASentinelBudgetRaisedAboveTheRegularOne is the sentinel
// violation, and it is the worst of the three because of WHEN it fires.
// RATE_LIMIT_SENTINEL_LIMIT is a documented escape hatch for raising the sentinel bucket during a
// live Cloudflare header anomaly — the incident where ALL traffic collapses onto one shared
// identity. A pre-check ceiling derived only from the regular limit silently neuters that knob at
// maximum blast radius.
func TestIPRateLimiter_PrecheckHonoursASentinelBudgetRaisedAboveTheRegularOne(t *testing.T) {
	backend := allowAllBackend()
	e := newEchoGatedAsCloudflarePassed(middleware.RateLimitConfig{
		IPLimit:               1000,
		WindowDuration:        time.Minute,
		PrecheckLimit:         10,  // regular
		PrecheckAuthLimit:     10,  // auth
		PrecheckSentinelLimit: 100, // the operator raised this during an incident
		Backend:               backend,
	})

	// ⚠ The sentinel tier is reached ONLY on a request that PASSED the Cloudflare gate and then
	// had no usable Cf-Connecting-Ip (clientIdentity's GateStatusPassed arm). A plain RemoteAddr
	// resolves fine and lands in the REGULAR tier, so the gate status must be stamped by a
	// middleware ahead of the limiter — which is what SetGateStatus is exported for.
	denied := 0
	for i := 0; i < 40; i++ {
		req := httptest.NewRequest(http.MethodGet, "/test", nil) // no Cf-Connecting-Ip
		req.RemoteAddr = "203.0.113.77:12345"
		rr := httptest.NewRecorder()
		e.ServeHTTP(rr, req)
		if rr.Code == http.StatusTooManyRequests {
			denied++
		}
	}

	assert.Zero(t, denied,
		"the sentinel pre-check budget must follow the AUTHORITATIVE sentinel limit (100), not "+
			"the regular one. Denials here mean an operator's incident escape hatch does nothing "+
			"— during the exact incident it exists for, affecting every customer at once.")
}

// TestIPRateLimiter_PrecheckIsNeverStricterThanGCRAAtARefillBoundary covers the divergence Codex
// found and the other reviewer refuted — the other reviewer was wrong, and the arithmetic is why.
// The backend derives GCRA's emission interval with INTEGER microsecond division
// (window_us / limit, ratelimitstore.emissionAndDVT), while x/time/rate uses a float rate. For a
// limit that does not divide the window evenly — 34/min gives 1_764_705us vs 1_764_705.88us —
// truncation makes GCRA fractionally MORE permissive, so a request arriving exactly at the Redis
// interval can be allowed there while the token bucket is still a sliver short.
//
// The fix is not to match the arithmetic exactly, which would be fragile against clock skew and
// against the pre-check being per-machine while the backend is fleet-wide. It is to give the
// pre-check deliberate HEADROOM (precheckHeadroomFactor): a cost ceiling does not need to be
// tight, and slack makes "never stricter" robust rather than arithmetically lucky.
func TestIPRateLimiter_PrecheckIsNeverStricterThanGCRAAtARefillBoundary(t *testing.T) {
	backend := allowAllBackend()
	const limit = 34 // deliberately does not divide 60_000_000us evenly
	e := newEchoWithPrecheck(middleware.RateLimitConfig{
		IPLimit:               1000,
		WindowDuration:        time.Minute,
		PrecheckLimit:         limit,
		PrecheckAuthLimit:     limit,
		PrecheckSentinelLimit: limit,
		Backend:               backend,
	})

	denied := 0
	for i := 0; i < limit; i++ {
		if requestTo(e, http.MethodGet, "/test", "203.0.113.34") == http.StatusTooManyRequests {
			denied++
		}
	}
	assert.Zero(t, denied,
		"the authoritative budget is %d per window; the pre-check denied %d of them", limit, denied)

	// One more, immediately: the authoritative GCRA bucket is exactly empty here, so this may be
	// denied — but it must be denied by the BACKEND's answer, not by a pre-check that ran out
	// first. With headroom the pre-check still has tokens, so the backend is consulted.
	backend.calls = 0
	requestTo(e, http.MethodGet, "/test", "203.0.113.34")
	assert.Positive(t, backend.calls,
		"the pre-check must still be consulting the backend at the authoritative limit; zero "+
			"backend calls means it ran out first and is acting as a second enforcement layer")
}
