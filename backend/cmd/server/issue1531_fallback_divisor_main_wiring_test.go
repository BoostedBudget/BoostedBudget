package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/maxnussbaum/BoostedBudget/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Issue #1531 Slice B / L10: rateLimitFallbackLimit itself is unit-tested (issue1531_wiring_test.go's
// TestRateLimitFallbackLimit / TestRateLimitFallbackDivisorMatchesIntendedMachineCount), and it was
// already found inert ONCE (fly.toml never actually SET RATE_LIMIT_FALLBACK_DIVISOR — the fix for
// that added the fly-config guard above). But nothing pins that
// registerCloudflareAndRateLimitMiddlewareWithBackend actually PASSES cfg.RateLimitFallbackDivisor
// into rateLimitFallbackLimit at the call site, as opposed to a hardcoded 1 — a regression there
// would pass every existing test (rateLimitFallbackLimit's own unit tests call it directly with
// whatever divisor they choose; the fly.toml guard only checks the config FILE, not that main.go
// reads the field it names). This is a behavioural, end-to-end test of the actual middleware chain:
// it proves the in-memory fallback limiter enforces limit/divisor requests per window, not the raw
// limit, when cfg.RateLimitFallbackDivisor is set.
func TestWiring_RateLimitFallbackDivisorIsActuallyAppliedAtCallSite(t *testing.T) {
	e := echo.New()
	cfCIDRRef := newOrderingTestCFCIDRRef()
	cfg := &config.Config{
		Environment:              "development", // in CloudflareOnly's bypassEnvironments
		RateLimitIP:              100,
		RateLimitAuth:            100,
		RateLimitFallbackDivisor: 25, // 100 / 25 == 4 — a hardcoded-1 regression would allow 100
		RateLimitMaxEntries:      100,
	}

	done := make(chan struct{})
	defer close(done)

	// backend=nil: the in-memory fallback (memoryBackend) is used directly, with no primary to
	// fail over from first — see newFallbackLimiter's doc comment ("if primary is nil, the
	// limiter runs on memory-only mode" — deterministic, no simulated-outage indirection needed).
	registerCloudflareAndRateLimitMiddlewareWithBackend(e, cfCIDRRef, cfg, nil, done, nil)
	e.GET("/", func(c echo.Context) error { return c.String(http.StatusOK, "ok") })

	srv := httptest.NewServer(e)
	defer srv.Close()

	var statuses []int
	for i := 0; i < 5; i++ {
		resp, err := srv.Client().Get(srv.URL + "/")
		require.NoError(t, err)
		statuses = append(statuses, resp.StatusCode)
		_ = resp.Body.Close()
	}

	// RateLimitIP=100 divided by RateLimitFallbackDivisor=25 == 4: the first 4 requests must
	// succeed and the 5th must be rejected. If main.go regressed to passing a hardcoded 1 (or
	// otherwise stopped reading cfg.RateLimitFallbackDivisor), all 5 requests would succeed here
	// (100 far exceeds 5), and this assertion would fail.
	assert.Equal(t, []int{
		http.StatusOK, http.StatusOK, http.StatusOK, http.StatusOK, http.StatusTooManyRequests,
	}, statuses, "the in-memory fallback limiter must enforce RateLimitIP/RateLimitFallbackDivisor "+
		"(100/25 == 4) requests per window, proving main.go actually passes "+
		"cfg.RateLimitFallbackDivisor into rateLimitFallbackLimit at the call site rather than a "+
		"hardcoded 1")
}
