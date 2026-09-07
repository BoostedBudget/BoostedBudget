package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	echomiddleware "github.com/labstack/echo/v4/middleware"
	"github.com/maxnussbaum/BoostedBudget/internal/middleware"
	"github.com/stretchr/testify/assert"
)

// corsErrorResponseAllowedOrigin is the single allowed origin used across these tests.
const corsErrorResponseAllowedOrigin = "https://boostedbudget.com"

// corsErrorResponseDisallowedOrigin is an origin never present in the allowlist.
const corsErrorResponseDisallowedOrigin = "https://evil.example.com"

// buildCORSErrorResponseChain wires a chain mirroring production ordering:
// CORSErrorHeaders -> CloudflareOnly -> IPRateLimiter -> handler.
//
// ipLimit controls the rate limiter's per-IP budget so tests can drive it past its
// budget deterministically. cfSecret/cfEnv/cfCIDRs configure CloudflareOnly so tests
// can also drive a 403 deterministically (using an env NOT in bypassEnvironments and
// an IP not in cfCIDRs, or a missing/wrong secret).
func buildCORSErrorResponseChain(t *testing.T, ipLimit int) *echo.Echo {
	t.Helper()
	e := echo.New()

	cfCIDRRef := newCIDRRef(testCIDRs)

	e.Use(middleware.CORSErrorHeaders(corsErrorResponseAllowedOrigin))
	e.Use(middleware.CloudflareOnly(cfCIDRRef, testSecret, "production"))
	e.Use(middleware.IPRateLimiter(middleware.RateLimitConfig{
		IPLimit:        ipLimit,
		WindowDuration: time.Minute,
	}))
	e.GET("/test", func(c echo.Context) error {
		return c.String(http.StatusOK, "ok")
	})
	return e
}

// corsErrorResponseRequest describes one request against the chain.
type corsErrorResponseRequest struct {
	origin       string
	flyClientIP  string
	originSecret string
	method       string
	extraHeaders map[string]string
}

func doCORSErrorResponseRequest(e *echo.Echo, r corsErrorResponseRequest) *httptest.ResponseRecorder {
	method := r.method
	if method == "" {
		method = http.MethodGet
	}
	req := httptest.NewRequest(method, "/test", nil)
	req.RemoteAddr = "203.0.113.5:12345" // in cfCIDRs range, used for the passing-gate cases
	if r.origin != "" {
		req.Header.Set("Origin", r.origin)
	}
	if r.flyClientIP != "" {
		req.Header.Set("Fly-Client-Ip", r.flyClientIP)
	}
	if r.originSecret != "" {
		req.Header.Set("X-Origin-Secret", r.originSecret)
	}
	for k, v := range r.extraHeaders {
		req.Header.Set(k, v)
	}
	rr := httptest.NewRecorder()
	e.ServeHTTP(rr, req)
	return rr
}

// TestCORSErrorHeaders_429CarriesACAO drives the rate limiter past its budget and
// asserts the resulting 429 still carries Access-Control-Allow-Origin.
func TestCORSErrorHeaders_429CarriesACAO(t *testing.T) {
	e := buildCORSErrorResponseChain(t, 1)

	// First request consumes the single allowed slot.
	rr := doCORSErrorResponseRequest(e, corsErrorResponseRequest{
		origin:       corsErrorResponseAllowedOrigin,
		flyClientIP:  validCloudflareIPv4,
		originSecret: testSecret,
	})
	assert.Equal(t, http.StatusOK, rr.Code)

	// Second request from the same IP is rate limited.
	rr = doCORSErrorResponseRequest(e, corsErrorResponseRequest{
		origin:       corsErrorResponseAllowedOrigin,
		flyClientIP:  validCloudflareIPv4,
		originSecret: testSecret,
	})
	assert.Equal(t, http.StatusTooManyRequests, rr.Code)
	assert.Equal(t, corsErrorResponseAllowedOrigin, rr.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "true", rr.Header().Get("Access-Control-Allow-Credentials"))
}

// TestCORSErrorHeaders_403CarriesACAO makes CloudflareOnly reject (missing Fly-Client-Ip)
// and asserts the 403 still carries Access-Control-Allow-Origin.
func TestCORSErrorHeaders_403CarriesACAO(t *testing.T) {
	e := buildCORSErrorResponseChain(t, 100)

	rr := doCORSErrorResponseRequest(e, corsErrorResponseRequest{
		origin: corsErrorResponseAllowedOrigin,
		// no Fly-Client-Ip, no secret -> CloudflareOnly blocks with 403
	})
	assert.Equal(t, http.StatusForbidden, rr.Code)
	assert.Equal(t, corsErrorResponseAllowedOrigin, rr.Header().Get("Access-Control-Allow-Origin"))
}

// TestCORSErrorHeaders_200StillCarriesACAO is the regression guard for the working case.
func TestCORSErrorHeaders_200StillCarriesACAO(t *testing.T) {
	e := buildCORSErrorResponseChain(t, 100)

	rr := doCORSErrorResponseRequest(e, corsErrorResponseRequest{
		origin:       corsErrorResponseAllowedOrigin,
		flyClientIP:  validCloudflareIPv4,
		originSecret: testSecret,
	})
	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, corsErrorResponseAllowedOrigin, rr.Header().Get("Access-Control-Allow-Origin"))
}

// TestCORSErrorHeaders_RetryAfterIsExposed asserts Access-Control-Expose-Headers contains
// Retry-After on a rate-limited response, so a browser client can read it cross-origin.
func TestCORSErrorHeaders_RetryAfterIsExposed(t *testing.T) {
	e := echo.New()
	cfCIDRRef := newCIDRRef(testCIDRs)

	corsConfig := middleware.GetCORSConfig(corsErrorResponseAllowedOrigin)

	e.Use(middleware.CORSErrorHeaders(corsErrorResponseAllowedOrigin))
	e.Use(middleware.CloudflareOnly(cfCIDRRef, testSecret, "production"))
	e.Use(middleware.IPRateLimiter(middleware.RateLimitConfig{
		IPLimit:        1,
		WindowDuration: time.Minute,
	}))
	// Real CORS middleware last, mirroring its production position (registered last,
	// after the gate — see main.go).
	e.Use(echomiddleware.CORSWithConfig(corsConfig))
	e.GET("/test", func(c echo.Context) error {
		return c.String(http.StatusOK, "ok")
	})

	rr := doCORSErrorResponseRequest(e, corsErrorResponseRequest{
		origin:       corsErrorResponseAllowedOrigin,
		flyClientIP:  validCloudflareIPv4,
		originSecret: testSecret,
	})
	assert.Equal(t, http.StatusOK, rr.Code)

	rr = doCORSErrorResponseRequest(e, corsErrorResponseRequest{
		origin:       corsErrorResponseAllowedOrigin,
		flyClientIP:  validCloudflareIPv4,
		originSecret: testSecret,
	})
	assert.Equal(t, http.StatusTooManyRequests, rr.Code)
	assert.Contains(t, rr.Header().Get("Access-Control-Expose-Headers"), "Retry-After")
}

// TestCORSErrorHeaders_DisallowedOriginGetsNoACAO asserts a request from an origin not in
// the allowlist never receives Access-Control-Allow-Origin.
func TestCORSErrorHeaders_DisallowedOriginGetsNoACAO(t *testing.T) {
	e := buildCORSErrorResponseChain(t, 100)

	rr := doCORSErrorResponseRequest(e, corsErrorResponseRequest{
		origin:       corsErrorResponseDisallowedOrigin,
		flyClientIP:  validCloudflareIPv4,
		originSecret: testSecret,
	})
	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Empty(t, rr.Header().Get("Access-Control-Allow-Origin"))
}

// ---------------------------------------------------------------------------
// Security regression: OPTIONS must still hit the gate.
// ---------------------------------------------------------------------------

// TestCORSErrorHeaders_OPTIONSNoOriginStillHitsGate asserts an OPTIONS request with no
// Origin header at all is still rejected by CloudflareOnly (403), never short-circuited
// to 204 by the new middleware.
func TestCORSErrorHeaders_OPTIONSNoOriginStillHitsGate(t *testing.T) {
	e := buildCORSErrorResponseChain(t, 100)

	rr := doCORSErrorResponseRequest(e, corsErrorResponseRequest{
		method: http.MethodOptions,
		// no Origin, no Fly-Client-Ip, no secret
	})
	assert.Equal(t, http.StatusForbidden, rr.Code, "OPTIONS with no Origin must still be gated, never short-circuited to 204")
}

// TestCORSErrorHeaders_OPTIONSDisallowedOriginStillHitsGate asserts an OPTIONS request
// from a disallowed origin is still rejected by CloudflareOnly (403).
func TestCORSErrorHeaders_OPTIONSDisallowedOriginStillHitsGate(t *testing.T) {
	e := buildCORSErrorResponseChain(t, 100)

	rr := doCORSErrorResponseRequest(e, corsErrorResponseRequest{
		method: http.MethodOptions,
		origin: corsErrorResponseDisallowedOrigin,
		// no Fly-Client-Ip, no secret
	})
	assert.Equal(t, http.StatusForbidden, rr.Code, "OPTIONS from a disallowed origin must still be gated, never short-circuited to 204")
}

// TestCORSErrorHeaders_OPTIONSAllowedPreflightStillHitsGate asserts a genuine-looking
// preflight (allowed Origin + Access-Control-Request-Method) is still rejected by
// CloudflareOnly (403) when the gate headers are absent — it must never reach a 204
// before the gate runs.
func TestCORSErrorHeaders_OPTIONSAllowedPreflightStillHitsGate(t *testing.T) {
	e := buildCORSErrorResponseChain(t, 100)

	rr := doCORSErrorResponseRequest(e, corsErrorResponseRequest{
		method: http.MethodOptions,
		origin: corsErrorResponseAllowedOrigin,
		extraHeaders: map[string]string{
			"Access-Control-Request-Method": "GET",
		},
		// no Fly-Client-Ip, no secret
	})
	assert.Equal(t, http.StatusForbidden, rr.Code, "an allowed-origin preflight must still be gated, never short-circuited to 204")
}
