package middleware_test

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/middleware"
	"github.com/maxnussbaum/BoostedBudget/internal/observability"
	dto "github.com/prometheus/client_model/go"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// prometheusGatherRateLimitRejections gathers the current sample set for the
// ratelimit_rejections_total metric family from the default (process-wide) Prometheus registry,
// returning each individual metric so callers can inspect its labels.
func prometheusGatherRateLimitRejections(t *testing.T) ([]*dto.Metric, error) {
	t.Helper()
	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		return nil, err
	}
	for _, f := range families {
		if f.GetName() == "ratelimit_rejections_total" {
			return f.GetMetric(), nil
		}
	}
	return nil, nil
}

func newEchoWithIPLimiter(cfg middleware.RateLimitConfig) *echo.Echo {
	e := echo.New()
	e.Use(middleware.IPRateLimiter(cfg))
	e.GET("/test", func(c echo.Context) error {
		return c.String(http.StatusOK, "ok")
	})
	e.GET("/webhooks/plaid", func(c echo.Context) error {
		return c.String(http.StatusOK, "ok")
	})
	return e
}

func makeRequest(e *echo.Echo, ip string) int {
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.RemoteAddr = ip + ":12345"
	rr := httptest.NewRecorder()
	e.ServeHTTP(rr, req)
	return rr.Code
}

func TestIPRateLimiter_UnderLimitPasses(t *testing.T) {
	cfg := middleware.RateLimitConfig{
		IPLimit:        10,
		WindowDuration: time.Minute,
	}
	e := newEchoWithIPLimiter(cfg)

	for i := 0; i < 5; i++ {
		code := makeRequest(e, "192.168.1.1")
		assert.Equal(t, http.StatusOK, code, "request %d should pass", i+1)
	}
}

func TestIPRateLimiter_OverLimitReturns429(t *testing.T) {
	cfg := middleware.RateLimitConfig{
		IPLimit:        3,
		WindowDuration: time.Minute,
	}
	e := newEchoWithIPLimiter(cfg)

	// First 3 requests should pass
	for i := 0; i < 3; i++ {
		code := makeRequest(e, "10.0.0.1")
		assert.Equal(t, http.StatusOK, code, "request %d should pass", i+1)
	}

	// 4th request should be rate limited
	code := makeRequest(e, "10.0.0.1")
	assert.Equal(t, http.StatusTooManyRequests, code)
}

func TestIPRateLimiter_429HasRetryAfterHeader(t *testing.T) {
	cfg := middleware.RateLimitConfig{
		IPLimit:        1,
		WindowDuration: time.Minute,
	}
	e := newEchoWithIPLimiter(cfg)
	ip := "172.16.0.1"

	// Exhaust the limit
	makeRequest(e, ip)

	// Second request should be rate-limited with Retry-After header
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.RemoteAddr = ip + ":9999"
	rr := httptest.NewRecorder()
	e.ServeHTTP(rr, req)

	require.Equal(t, http.StatusTooManyRequests, rr.Code)
	assert.NotEmpty(t, rr.Header().Get("Retry-After"), "Retry-After header must be set on 429")
}

func TestIPRateLimiter_DifferentIPsAreIndependent(t *testing.T) {
	cfg := middleware.RateLimitConfig{
		IPLimit:        1,
		WindowDuration: time.Minute,
	}
	e := newEchoWithIPLimiter(cfg)

	// IP A exhausts its limit
	makeRequest(e, "1.1.1.1")
	codeA := makeRequest(e, "1.1.1.1")
	assert.Equal(t, http.StatusTooManyRequests, codeA, "IP A should be rate limited")

	// IP B still gets through
	codeB := makeRequest(e, "2.2.2.2")
	assert.Equal(t, http.StatusOK, codeB, "IP B should not be affected by IP A's limit")
}

func TestIPRateLimiter_StrictLimitForAuthEndpoints(t *testing.T) {
	cfg := middleware.RateLimitConfig{
		IPLimit:           100, // Regular limit
		AuthEndpointLimit: 2,   // Strict limit for auth/webhook paths
		WindowDuration:    time.Minute,
	}
	e := echo.New()
	e.Use(middleware.IPRateLimiter(cfg))
	e.POST("/webhooks/plaid", func(c echo.Context) error {
		return c.String(http.StatusOK, "ok")
	})
	e.GET("/api/v1/accounts", func(c echo.Context) error {
		return c.String(http.StatusOK, "ok")
	})

	ip := "5.5.5.5"

	// Auth endpoint: 2 requests pass, 3rd fails
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/webhooks/plaid", nil)
		req.RemoteAddr = ip + ":12345"
		rr := httptest.NewRecorder()
		e.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusOK, rr.Code)
	}
	req := httptest.NewRequest(http.MethodPost, "/webhooks/plaid", nil)
	req.RemoteAddr = ip + ":12345"
	rr := httptest.NewRecorder()
	e.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusTooManyRequests, rr.Code)

	// Regular endpoint uses different limiter bucket
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/accounts", nil)
	req2.RemoteAddr = ip + ":12345"
	rr2 := httptest.NewRecorder()
	e.ServeHTTP(rr2, req2)
	assert.Equal(t, http.StatusOK, rr2.Code)
}

func TestIPRateLimiter_SameRemoteAddrSharesBucketRegardlessOfXFF(t *testing.T) {
	cfg := middleware.RateLimitConfig{
		IPLimit:        1,
		WindowDuration: time.Minute,
	}
	e := newEchoWithIPLimiter(cfg)

	// Both requests come from the same real TCP peer. The identical
	// X-Forwarded-For value is incidental here -- see the exploit test below for
	// proof that XFF itself is never consulted for identity.
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = "10.0.0.1:12345"                // Real peer
		req.Header.Set("X-Forwarded-For", "203.0.113.1") // Client-supplied, must be ignored
		rr := httptest.NewRecorder()
		e.ServeHTTP(rr, req)
		if i == 0 {
			assert.Equal(t, http.StatusOK, rr.Code)
		} else {
			assert.Equal(t, http.StatusTooManyRequests, rr.Code,
				"second request from same real peer should be rate limited")
		}
	}
}

// ---------------------------------------------------------------------------
// Source-aware identity — exploit and hardening tests (issue #1514, F1)
// ---------------------------------------------------------------------------

// newEchoWithGateStatus wires IPRateLimiter behind a fake CloudflareOnly stand-in
// that stamps the given GateStatus onto every request, so these tests can drive
// each of clientIdentity's policy branches without standing up the full
// CloudflareOnly middleware (CIDR ranges, origin secret, etc).
func newEchoWithGateStatus(cfg middleware.RateLimitConfig, status middleware.GateStatus) *echo.Echo {
	e := echo.New()
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			middleware.SetGateStatus(c, status)
			return next(c)
		}
	})
	e.Use(middleware.IPRateLimiter(cfg))
	handler := func(c echo.Context) error {
		return c.String(http.StatusOK, "ok")
	}
	e.GET("/test", handler)
	e.GET("/health", handler)
	return e
}

// doRateLimitedRequest fires one request with the given RemoteAddr and headers
// and returns the response status code.
func doRateLimitedRequest(e *echo.Echo, path, remoteAddr string, headers map[string][]string) int {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.RemoteAddr = remoteAddr
	for k, vs := range headers {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	rr := httptest.NewRecorder()
	e.ServeHTTP(rr, req)
	return rr.Code
}

// TestIPRateLimiter_DifferentXFFFromSamePeerShareBucket is the exploit test.
// Before the fix, clientIP() read X-Forwarded-For -- entirely client-controlled
// end to end, since Cloudflare appends to rather than replaces an inbound XFF --
// so a different value per request bought a fresh token bucket every time and
// the 4th request below would incorrectly return 200. Run against the
// pre-fix code, this test fails with:
//
//	Error: Not equal: expected: 429, actual: 200
//	Messages: different X-Forwarded-For per request from the same real peer must share one bucket
//
// because clientIP() keyed each request on its unique spoofed XFF value.
func TestIPRateLimiter_DifferentXFFFromSamePeerShareBucket(t *testing.T) {
	cfg := middleware.RateLimitConfig{IPLimit: 3, WindowDuration: time.Minute}
	e := newEchoWithGateStatus(cfg, middleware.GateStatusUnknown)

	for i := 0; i < 3; i++ {
		code := doRateLimitedRequest(e, "/test", fmt.Sprintf("10.0.0.1:%d", 1000+i), map[string][]string{
			"X-Forwarded-For": {fmt.Sprintf("203.0.113.%d", i+1)},
		})
		assert.Equal(t, http.StatusOK, code, "request %d should pass", i+1)
	}

	code := doRateLimitedRequest(e, "/test", "10.0.0.1:9999", map[string][]string{
		"X-Forwarded-For": {"203.0.113.99"},
	})
	assert.Equal(t, http.StatusTooManyRequests, code,
		"different X-Forwarded-For per request from the same real peer must share one bucket")
}

// TestIPRateLimiter_SpoofedCFConnectingIPIgnoredWithoutGate proves a request that
// did NOT pass CloudflareOnly cannot use a spoofed Cf-Connecting-Ip to fabricate
// distinct identities. All three requests share one real peer and the same
// exempt-path GateStatus, but each carries a different Cf-Connecting-Ip value; if
// that header were trusted here, the 3rd request would wrongly get its own fresh
// bucket and return 200.
func TestIPRateLimiter_SpoofedCFConnectingIPIgnoredWithoutGate(t *testing.T) {
	cfg := middleware.RateLimitConfig{IPLimit: 2, WindowDuration: time.Minute}
	e := newEchoWithGateStatus(cfg, middleware.GateStatusExemptPath)

	for i := 0; i < 2; i++ {
		code := doRateLimitedRequest(e, "/health", fmt.Sprintf("10.0.0.9:%d", 1000+i), map[string][]string{
			"Cf-Connecting-Ip": {fmt.Sprintf("198.51.100.%d", i+1)},
		})
		assert.Equal(t, http.StatusOK, code, "request %d should pass", i+1)
	}

	code := doRateLimitedRequest(e, "/health", "10.0.0.9:2000", map[string][]string{
		"Cf-Connecting-Ip": {"198.51.100.250"},
	})
	assert.Equal(t, http.StatusTooManyRequests, code,
		"a route that did not pass CloudflareOnly must not trust a spoofed Cf-Connecting-Ip as identity")
}

// TestIPRateLimiter_ExemptPathMissingHeaderNotRejected covers the first trap
// called out in the issue: Fly's own health/metrics probes hit exempt paths
// without any Fly-Client-Ip header (no Cloudflare hop in front of them locally),
// and must not be rejected for that.
func TestIPRateLimiter_ExemptPathMissingHeaderNotRejected(t *testing.T) {
	cfg := middleware.RateLimitConfig{IPLimit: 5, WindowDuration: time.Minute}
	e := newEchoWithGateStatus(cfg, middleware.GateStatusExemptPath)

	code := doRateLimitedRequest(e, "/health", "10.1.1.1:5000", nil)
	assert.Equal(t, http.StatusOK, code, "exempt path with no Fly-Client-Ip header must not be rejected")
}

// TestIPRateLimiter_ExemptPathProbeNotStarvedByPublicTraffic covers the second
// trap: a constant shared key for every exempt-path probe would let unrelated
// public traffic to the same path exhaust the probe's own budget. Keying on
// Fly-Client-Ip (set by Fly's edge from the real TCP connection, not
// client-suppliable) keeps the probe's bucket independent of public traffic.
func TestIPRateLimiter_ExemptPathProbeNotStarvedByPublicTraffic(t *testing.T) {
	cfg := middleware.RateLimitConfig{IPLimit: 2, WindowDuration: time.Minute}
	e := newEchoWithGateStatus(cfg, middleware.GateStatusExemptPath)

	for i := 0; i < 2; i++ {
		code := doRateLimitedRequest(e, "/health", fmt.Sprintf("127.0.0.1:%d", 1+i), map[string][]string{
			"Fly-Client-Ip": {"127.0.0.1"},
		})
		assert.Equal(t, http.StatusOK, code, "probe request %d should pass", i+1)
	}
	code := doRateLimitedRequest(e, "/health", "127.0.0.1:9", map[string][]string{
		"Fly-Client-Ip": {"127.0.0.1"},
	})
	assert.Equal(t, http.StatusTooManyRequests, code, "probe's budget should now be exhausted")

	publicCode := doRateLimitedRequest(e, "/health", "203.0.113.5:4000", map[string][]string{
		"Fly-Client-Ip": {"203.0.113.5"},
	})
	assert.Equal(t, http.StatusOK, publicCode,
		"unrelated public traffic to the same exempt path must not share the probe's bucket")
}

// TestIPRateLimiter_MalformedCFConnectingIPFallsBackToSentinelNotUsedAsKey
// proves that a malformed Cf-Connecting-Ip on an otherwise Cloudflare-gated
// request is never used verbatim as a bucket key. Per the #1514 fix-round-1
// ruling, it does NOT fall back to Fly-Client-Ip or RemoteAddr (both are the
// Fly proxy's address in production, not the real client). It falls back to
// the single fixed sentinel bucket instead — proven here by two garbage-header
// requests from two DIFFERENT real peers still sharing one bucket, which
// per-peer fallback (the old behavior) would not do. The duplicated-header
// case (two Cf-Connecting-Ip values on one request) is a separate code path
// in singleValuedHeaderAddr (len(values) != 1) and is covered by
// TestIPRateLimiter_DuplicatedCFConnectingIPFallsBackToSentinelNotUsedAsKey
// below, not by this test.
func TestIPRateLimiter_MalformedCFConnectingIPFallsBackToSentinelNotUsedAsKey(t *testing.T) {
	cfg := middleware.RateLimitConfig{IPLimit: 2, WindowDuration: time.Minute}
	e := newEchoWithGateStatus(cfg, middleware.GateStatusPassed)

	code1 := doRateLimitedRequest(e, "/test", "192.0.2.1:1000", map[string][]string{
		"Cf-Connecting-Ip": {"not-an-ip"},
		"Fly-Client-Ip":    {"192.0.2.1"},
	})
	assert.Equal(t, http.StatusOK, code1, "first anomalous request should pass")

	// A second anomalous request from a DIFFERENT real peer, with a DIFFERENT
	// garbage Cf-Connecting-Ip and a DIFFERENT Fly-Client-Ip. If either header
	// were used as (or to derive) the key, this would land in its own fresh
	// bucket and return 200. It must instead share the sentinel bucket with the
	// first request and be throttled.
	code2 := doRateLimitedRequest(e, "/test", "198.51.100.9:2000", map[string][]string{
		"Cf-Connecting-Ip": {"also-garbage"},
		"Fly-Client-Ip":    {"198.51.100.9"},
	})
	assert.Equal(t, http.StatusOK, code2, "second anomalous request (still under the 2-req cap) should pass")

	code3 := doRateLimitedRequest(e, "/test", "203.0.113.77:3000", map[string][]string{
		"Cf-Connecting-Ip": {"still-garbage"},
		"Fly-Client-Ip":    {"203.0.113.77"},
	})
	assert.Equal(t, http.StatusTooManyRequests, code3,
		"a third anomalous request from yet another peer must share the global sentinel bucket and be throttled")
}

// TestIPRateLimiter_DuplicatedCFConnectingIPFallsBackToSentinelNotUsedAsKey
// exercises the multi-value branch of singleValuedHeaderAddr (len(values) != 1)
// that the malformed-value test above never reaches: each request there sends
// exactly one Cf-Connecting-Ip value (just an unparseable one). Here every
// request genuinely carries TWO Cf-Connecting-Ip header occurrences — each
// individually a validly-formed, DIFFERENT IP — proving duplication alone
// (independent of whether either value parses) is rejected as untrustworthy
// and falls back to the sentinel bucket, using the same
// different-peers-share-one-bucket proof as the malformed-value test.
func TestIPRateLimiter_DuplicatedCFConnectingIPFallsBackToSentinelNotUsedAsKey(t *testing.T) {
	cfg := middleware.RateLimitConfig{IPLimit: 2, WindowDuration: time.Minute}
	e := newEchoWithGateStatus(cfg, middleware.GateStatusPassed)

	code1 := doRateLimitedRequest(e, "/test", "192.0.2.2:1000", map[string][]string{
		"Cf-Connecting-Ip": {"192.0.2.100", "192.0.2.101"},
		"Fly-Client-Ip":    {"192.0.2.2"},
	})
	assert.Equal(t, http.StatusOK, code1, "first duplicated-header request should pass")

	// A second request from a DIFFERENT real peer, with a DIFFERENT pair of
	// duplicated Cf-Connecting-Ip values and a DIFFERENT Fly-Client-Ip. If the
	// first (or any) of the duplicated values were used as (or to derive) the
	// key, this would land in its own fresh bucket and return 200. It must
	// instead share the sentinel bucket with the first request and be
	// throttled once the cap is reached.
	code2 := doRateLimitedRequest(e, "/test", "198.51.100.10:2000", map[string][]string{
		"Cf-Connecting-Ip": {"198.51.100.200", "198.51.100.201"},
		"Fly-Client-Ip":    {"198.51.100.10"},
	})
	assert.Equal(t, http.StatusOK, code2, "second duplicated-header request (still under the 2-req cap) should pass")

	code3 := doRateLimitedRequest(e, "/test", "203.0.113.88:3000", map[string][]string{
		"Cf-Connecting-Ip": {"203.0.113.200", "203.0.113.201"},
		"Fly-Client-Ip":    {"203.0.113.88"},
	})
	assert.Equal(t, http.StatusTooManyRequests, code3,
		"a third duplicated-header request from yet another peer must share the global sentinel bucket and be throttled")
}

// TestIPRateLimiter_SentinelFallbackIsObservable proves the sentinel branch
// increments observability.RateLimitIdentitySentinelFallbackTotal every time
// it fires, so this near-should-never-happen path is monitorable rather than
// silent.
func TestIPRateLimiter_SentinelFallbackIsObservable(t *testing.T) {
	cfg := middleware.RateLimitConfig{IPLimit: 100, WindowDuration: time.Minute}
	e := newEchoWithGateStatus(cfg, middleware.GateStatusPassed)

	before := testutil.ToFloat64(observability.RateLimitIdentitySentinelFallbackTotal)

	doRateLimitedRequest(e, "/test", "192.0.2.50:1000", nil) // no Cf-Connecting-Ip at all

	after := testutil.ToFloat64(observability.RateLimitIdentitySentinelFallbackTotal)
	assert.Equal(t, before+1, after,
		"a Cloudflare-gated request with no usable Cf-Connecting-Ip must increment the sentinel-fallback counter exactly once")
}

// TestIPRateLimiter_IPv6FormsCanonicalizeToSameBucket proves that IPv6 addresses
// written in different textual forms (compressed vs. fully expanded) canonicalize
// to the same identity.
func TestIPRateLimiter_IPv6FormsCanonicalizeToSameBucket(t *testing.T) {
	cfg := middleware.RateLimitConfig{IPLimit: 1, WindowDuration: time.Minute}
	e := newEchoWithGateStatus(cfg, middleware.GateStatusUnknown)

	code1 := doRateLimitedRequest(e, "/test", "[2001:db8::1]:1000", nil)
	assert.Equal(t, http.StatusOK, code1)

	code2 := doRateLimitedRequest(e, "/test", "[2001:0db8:0000:0000:0000:0000:0000:0001]:2000", nil)
	assert.Equal(t, http.StatusTooManyRequests, code2,
		"different textual forms of the same IPv6 address must canonicalize to one bucket")
}

// ---------------------------------------------------------------------------
// Backend seam — Task 5 (Issue #1531)
// ---------------------------------------------------------------------------

// fakeBackend is a minimal test double implementing middleware.RateLimitBackend from outside the
// package (as a real caller — e.g. main.go wiring the Redis backend — would).
type fakeBackend struct {
	name      string
	allowFunc func(ctx context.Context, tier domain.RateLimitTier, key string) (domain.RateLimitDecision, error)

	mu    sync.Mutex
	calls int
}

func (f *fakeBackend) Name() string { return f.name }

func (f *fakeBackend) Allow(ctx context.Context, tier domain.RateLimitTier, key string) (domain.RateLimitDecision, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	return f.allowFunc(ctx, tier, key)
}

func (f *fakeBackend) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// panickingBackend panics the instant Allow is called — proves a code path never touches it at
// all, rather than merely "wasn't observed to run".
type panickingBackend struct{}

func (panickingBackend) Name() string { return "panic" }
func (panickingBackend) Allow(context.Context, domain.RateLimitTier, string) (domain.RateLimitDecision, error) {
	panic("primary backend must never be called on this path")
}

// TestIPRateLimiter_DefaultsToTheInMemoryBackendWhenNoneIsInjected pins backward compatibility:
// a RateLimitConfig with no Backend set (the zero value, which is what every pre-#1531 call site
// and test already constructs) must behave exactly as it did before this task.
func TestIPRateLimiter_DefaultsToTheInMemoryBackendWhenNoneIsInjected(t *testing.T) {
	cfg := middleware.RateLimitConfig{IPLimit: 2, WindowDuration: time.Minute}
	require.Nil(t, cfg.Backend, "zero-value RateLimitConfig.Backend must be nil")
	e := newEchoWithIPLimiter(cfg)

	assert.Equal(t, http.StatusOK, makeRequest(e, "192.0.2.10"))
	assert.Equal(t, http.StatusOK, makeRequest(e, "192.0.2.10"))
	assert.Equal(t, http.StatusTooManyRequests, makeRequest(e, "192.0.2.10"))
}

// TestIPRateLimiter_UsesTheInjectedBackend proves a configured Backend is actually consulted (not
// just accepted and ignored): an injected backend that always denies must produce 429s even on
// the very first request, which the in-memory backend alone would never do at this limit.
func TestIPRateLimiter_UsesTheInjectedBackend(t *testing.T) {
	backend := &fakeBackend{name: "redis", allowFunc: func(context.Context, domain.RateLimitTier, string) (domain.RateLimitDecision, error) {
		return domain.RateLimitDecision{Allowed: false, RetryAfter: 42 * time.Second}, nil
	}}
	cfg := middleware.RateLimitConfig{IPLimit: 100, WindowDuration: time.Minute, Backend: backend}
	e := newEchoWithIPLimiter(cfg)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.RemoteAddr = "192.0.2.11:1000"
	rr := httptest.NewRecorder()
	e.ServeHTTP(rr, req)

	require.Equal(t, http.StatusTooManyRequests, rr.Code)
	assert.Equal(t, "42", rr.Header().Get("Retry-After"))
	assert.GreaterOrEqual(t, backend.callCount(), 1)
}

// TestIPRateLimiter_RejectionIncrementsRejectionCounterWithTierLabel drives a rejection on each of
// the three tiers and asserts observability.RateLimitRejectionsTotal{tier=...} increments by
// exactly one each time.
func TestIPRateLimiter_RejectionIncrementsRejectionCounterWithTierLabel(t *testing.T) {
	t.Run("regular", func(t *testing.T) {
		cfg := middleware.RateLimitConfig{IPLimit: 1, WindowDuration: time.Minute}
		e := newEchoWithGateStatus(cfg, middleware.GateStatusUnknown)
		before := testutil.ToFloat64(observability.RateLimitRejectionsTotal.WithLabelValues("regular"))

		doRateLimitedRequest(e, "/test", "198.51.100.20:1", nil)
		code := doRateLimitedRequest(e, "/test", "198.51.100.20:2", nil)
		require.Equal(t, http.StatusTooManyRequests, code)

		after := testutil.ToFloat64(observability.RateLimitRejectionsTotal.WithLabelValues("regular"))
		assert.Equal(t, before+1, after)
	})

	t.Run("auth", func(t *testing.T) {
		cfg := middleware.RateLimitConfig{IPLimit: 100, AuthEndpointLimit: 1, WindowDuration: time.Minute}
		e := newEchoWithGateStatus(cfg, middleware.GateStatusUnknown)
		before := testutil.ToFloat64(observability.RateLimitRejectionsTotal.WithLabelValues("auth"))

		doRateLimitedRequest(e, "/webhooks/plaid", "198.51.100.21:1", nil)
		code := doRateLimitedRequest(e, "/webhooks/plaid", "198.51.100.21:2", nil)
		require.Equal(t, http.StatusTooManyRequests, code)

		after := testutil.ToFloat64(observability.RateLimitRejectionsTotal.WithLabelValues("auth"))
		assert.Equal(t, before+1, after)
	})

	t.Run("sentinel", func(t *testing.T) {
		cfg := middleware.RateLimitConfig{IPLimit: 1, WindowDuration: time.Minute}
		e := newEchoWithGateStatus(cfg, middleware.GateStatusPassed)
		before := testutil.ToFloat64(observability.RateLimitRejectionsTotal.WithLabelValues("sentinel"))

		// No Cf-Connecting-Ip header at all -> falls back to the sentinel bucket (see clientIdentity).
		doRateLimitedRequest(e, "/test", "198.51.100.22:1", nil)
		code := doRateLimitedRequest(e, "/test", "198.51.100.23:2", nil)
		require.Equal(t, http.StatusTooManyRequests, code)

		after := testutil.ToFloat64(observability.RateLimitRejectionsTotal.WithLabelValues("sentinel"))
		assert.Equal(t, before+1, after)
	})
}

// panicCapturingHandler is a minimal slog.Handler that records whether any record was ever
// handled, used below to detect a panic that IPRateLimiter's own defer recover() caught and
// logged -- which would otherwise make a purely panic-based "the primary must never be called"
// proof pass by accident (the handler-level recover legitimately swallows and logs the panic
// regardless of WHY the primary was invoked, so response code alone cannot distinguish "never
// called" from "called, panicked, and recovered").
type panicCapturingHandler struct {
	mu      sync.Mutex
	handled bool
}

func (h *panicCapturingHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *panicCapturingHandler) Handle(context.Context, slog.Record) error {
	h.mu.Lock()
	h.handled = true
	h.mu.Unlock()
	return nil
}
func (h *panicCapturingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *panicCapturingHandler) WithGroup(string) slog.Handler      { return h }
func (h *panicCapturingHandler) sawAnyRecord() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.handled
}

// TestIPRateLimiter_ExemptPathsNeverTouchThePrimaryBackend is the required discrimination test:
// the injected primary PANICS the instant Allow is called. Response code 200 alone is NOT proof
// here, because IPRateLimiter's own defer recover() legitimately catches and logs any panic
// (including one from the primary) and falls through to next(ec) regardless -- so a 200 would be
// returned whether or not the exempt-path bypass actually ran. This test additionally asserts NO
// log record was ever emitted through the injected logger, which the recover() path always
// produces when it fires. Only both together prove the primary was never called at all.
func TestIPRateLimiter_ExemptPathsNeverTouchThePrimaryBackend(t *testing.T) {
	capture := &panicCapturingHandler{}
	cfg := middleware.RateLimitConfig{
		IPLimit:        100,
		WindowDuration: time.Minute,
		Backend:        panickingBackend{},
		Logger:         slog.New(capture),
	}
	e := echo.New()
	e.Use(middleware.IPRateLimiter(cfg))
	e.GET("/health", func(c echo.Context) error { return c.String(http.StatusOK, "ok") })

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.RemoteAddr = "198.51.100.30:1"
	rr := httptest.NewRecorder()
	e.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code, "GET /health must succeed")
	assert.False(t, capture.sawAnyRecord(),
		"no log record (including a recovered-panic record) may have been emitted — any record proves the panicking primary was actually invoked")
}

// TestIPRateLimiter_CleanupGoroutineStopsWhenDoneIsClosed proves the cleanup goroutine leak fix:
// closing cfg.Done must cause the background goroutine to exit.
func TestIPRateLimiter_CleanupGoroutineStopsWhenDoneIsClosed(t *testing.T) {
	runtime.GC()
	before := runtime.NumGoroutine()

	done := make(chan struct{})
	close(done) // already closed before IPRateLimiter's goroutine even starts

	cfg := middleware.RateLimitConfig{IPLimit: 5, WindowDuration: time.Minute, CleanupInterval: time.Hour, Done: done}
	_ = middleware.IPRateLimiter(cfg)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		runtime.GC()
		if runtime.NumGoroutine() <= before {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("cleanup goroutine did not stop after Done was closed: goroutines before=%d, now=%d", before, runtime.NumGoroutine())
}

// TestIPRateLimiter_BackendPanicDoesNotPanicTheRequest proves the handler-level defer recover():
// a panicking primary backend must not crash the connection or the test process — it is caught,
// logged, and the request falls through to next(ec).
func TestIPRateLimiter_BackendPanicDoesNotPanicTheRequest(t *testing.T) {
	cfg := middleware.RateLimitConfig{IPLimit: 100, WindowDuration: time.Minute, Backend: panickingBackend{}}
	e := newEchoWithIPLimiter(cfg)

	require.NotPanics(t, func() {
		code := makeRequest(e, "198.51.100.40")
		assert.Equal(t, http.StatusOK, code, "a panicking backend must fail open (request falls through to next) rather than crash the connection")
	})
}

// TestIPRateLimiter_RetryAfterIsNeverZeroOrNegative proves the middleware itself guards against a
// misbehaving backend that returns a zero (or negative) RetryAfter on denial — which would invite
// an immediate retry storm.
func TestIPRateLimiter_RetryAfterIsNeverZeroOrNegative(t *testing.T) {
	backend := &fakeBackend{name: "buggy", allowFunc: func(context.Context, domain.RateLimitTier, string) (domain.RateLimitDecision, error) {
		return domain.RateLimitDecision{Allowed: false, RetryAfter: 0}, nil
	}}
	cfg := middleware.RateLimitConfig{IPLimit: 100, WindowDuration: time.Minute, Backend: backend}
	e := newEchoWithIPLimiter(cfg)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.RemoteAddr = "198.51.100.50:1"
	rr := httptest.NewRecorder()
	e.ServeHTTP(rr, req)

	require.Equal(t, http.StatusTooManyRequests, rr.Code)
	retryAfter := rr.Header().Get("Retry-After")
	require.NotEmpty(t, retryAfter)
	assert.NotEqual(t, "0", retryAfter)
	assert.NotContains(t, retryAfter, "-")
}

// TestRateLimitRejections_OnlyEmitsBoundedTierLabelsFromProductionCode is the REAL enforcement
// test called for by Task 2's review carry-forward: it drives real rejections across all three
// tiers through the actual HTTP middleware (not by writing to the counter directly, which would be
// self-fulfilling) and then inspects every label value the metric family has ever published,
// asserting each one is in the small fixed set {regular, auth, sentinel}.
func TestRateLimitRejections_OnlyEmitsBoundedTierLabelsFromProductionCode(t *testing.T) {
	// Regular tier.
	regCfg := middleware.RateLimitConfig{IPLimit: 1, WindowDuration: time.Minute}
	regE := newEchoWithGateStatus(regCfg, middleware.GateStatusUnknown)
	doRateLimitedRequest(regE, "/test", "203.0.113.10:1", nil)
	doRateLimitedRequest(regE, "/test", "203.0.113.10:2", nil)

	// Auth tier.
	authCfg := middleware.RateLimitConfig{IPLimit: 100, AuthEndpointLimit: 1, WindowDuration: time.Minute}
	authE := newEchoWithGateStatus(authCfg, middleware.GateStatusUnknown)
	doRateLimitedRequest(authE, "/webhooks/plaid", "203.0.113.11:1", nil)
	doRateLimitedRequest(authE, "/webhooks/plaid", "203.0.113.11:2", nil)

	// Sentinel tier.
	sentinelCfg := middleware.RateLimitConfig{IPLimit: 1, WindowDuration: time.Minute}
	sentinelE := newEchoWithGateStatus(sentinelCfg, middleware.GateStatusPassed)
	doRateLimitedRequest(sentinelE, "/test", "203.0.113.12:1", nil)
	doRateLimitedRequest(sentinelE, "/test", "203.0.113.13:2", nil)

	metrics, err := prometheusGatherRateLimitRejections(t)
	require.NoError(t, err)

	allowed := map[string]bool{"regular": true, "auth": true, "sentinel": true}
	seen := 0
	for _, m := range metrics {
		for _, label := range m.GetLabel() {
			if label.GetName() != "tier" {
				continue
			}
			seen++
			assert.True(t, allowed[label.GetValue()], "production code emitted an unbounded/unexpected tier label value: %q", label.GetValue())
		}
	}
	assert.Greater(t, seen, 0, "test must have actually observed at least one tier label, or this assertion is vacuous")
}
