package middleware_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/maxnussbaum/BoostedBudget/internal/middleware"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

// TestIPRateLimiter_MalformedCFConnectingIPFallsBackNotUsedAsKey proves that a
// malformed (or duplicated) Cf-Connecting-Ip on an otherwise-gated request is
// never used verbatim as a bucket key: two different garbage values from the
// same already-verified Fly-Client-Ip peer must still share one bucket.
func TestIPRateLimiter_MalformedCFConnectingIPFallsBackNotUsedAsKey(t *testing.T) {
	cfg := middleware.RateLimitConfig{IPLimit: 2, WindowDuration: time.Minute}
	e := newEchoWithGateStatus(cfg, middleware.GateStatusPassed)

	for i := 0; i < 2; i++ {
		code := doRateLimitedRequest(e, "/test", fmt.Sprintf("192.0.2.1:%d", 1000+i), map[string][]string{
			"Cf-Connecting-Ip": {"not-an-ip"},
			"Fly-Client-Ip":    {"192.0.2.1"},
		})
		assert.Equal(t, http.StatusOK, code, "request %d should pass", i+1)
	}

	code := doRateLimitedRequest(e, "/test", "192.0.2.1:1099", map[string][]string{
		"Cf-Connecting-Ip": {"also-garbage"},
		"Fly-Client-Ip":    {"192.0.2.1"},
	})
	assert.Equal(t, http.StatusTooManyRequests, code,
		"malformed Cf-Connecting-Ip must fall back to the verified Fly-Client-Ip, not be used verbatim")
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
