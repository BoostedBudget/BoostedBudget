package middleware_test

import (
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
		IPLimit:           100,  // Regular limit
		AuthEndpointLimit: 2,    // Strict limit for auth/webhook paths
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

func TestIPRateLimiter_XForwardedForUsedAsClientIP(t *testing.T) {
	cfg := middleware.RateLimitConfig{
		IPLimit:        1,
		WindowDuration: time.Minute,
	}
	e := newEchoWithIPLimiter(cfg)

	// Both requests come from same X-Forwarded-For IP (simulating Fly.io proxy)
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = "10.0.0.1:12345"     // Proxy IP
		req.Header.Set("X-Forwarded-For", "203.0.113.1") // Real client IP
		rr := httptest.NewRecorder()
		e.ServeHTTP(rr, req)
		if i == 0 {
			assert.Equal(t, http.StatusOK, rr.Code)
		} else {
			assert.Equal(t, http.StatusTooManyRequests, rr.Code,
				"second request from same X-Forwarded-For IP should be rate limited")
		}
	}
}
