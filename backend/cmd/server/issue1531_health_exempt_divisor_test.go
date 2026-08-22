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

// TestWiring_HealthExemptPathIsNotDividedByFallbackDivisor is the #1531 L3 regression.
//
// registerCloudflareAndRateLimitMiddlewareWithBackend divides cfg.RateLimitIP by
// cfg.RateLimitFallbackDivisor to build IPLimit — the budget backing the memory fallback's
// REGULAR-tier store (see rateLimitFallbackLimit's doc comment: N machines each independently
// falling back to memory must not multiply a FLEET-WIDE budget by N). But /health is the only
// isExemptPath path, it ALWAYS uses the memory backend directly (never the Redis-backed primary,
// by design — a Redis blip must never threaten Fly's liveness probe), and its budget was never
// fleet-wide to begin with: it's Fly's own single-machine liveness check. Applying the
// fleet-machine-count divisor to it — the divisor's entire purpose is a DIFFERENT problem — simply
// halves (at the default divisor of 2) /health's real budget in completely normal operation, no
// outage required.
//
// This test sets RateLimitIP=4 and RateLimitFallbackDivisor=2 and proves /health still gets 4
// requests through per window (its own, undivided budget), not 2.
func TestWiring_HealthExemptPathIsNotDividedByFallbackDivisor(t *testing.T) {
	e := echo.New()
	cfCIDRRef := newOrderingTestCFCIDRRef()
	cfg := &config.Config{
		Environment:              "development", // in CloudflareOnly's bypassEnvironments
		RateLimitIP:              4,
		RateLimitAuth:            100,
		RateLimitFallbackDivisor: 2, // if the bug were still present, /health's budget would be 4/2 == 2
		RateLimitMaxEntries:      100,
	}

	done := make(chan struct{})
	defer close(done)

	registerCloudflareAndRateLimitMiddlewareWithBackend(e, cfCIDRRef, cfg, nil, done, nil)
	e.GET("/health", func(c echo.Context) error { return c.String(http.StatusOK, "ok") })

	srv := httptest.NewServer(e)
	defer srv.Close()

	var statuses []int
	for i := 0; i < 5; i++ {
		resp, err := srv.Client().Get(srv.URL + "/health")
		require.NoError(t, err)
		statuses = append(statuses, resp.StatusCode)
		_ = resp.Body.Close()
	}

	// RateLimitIP=4, undivided: the first 4 requests must succeed and the 5th must be rejected.
	// Under the pre-fix behaviour (divided by RateLimitFallbackDivisor=2), only the first 2 would
	// succeed and this assertion would fail.
	assert.Equal(t, []int{
		http.StatusOK, http.StatusOK, http.StatusOK, http.StatusOK, http.StatusTooManyRequests,
	}, statuses, "/health's own budget (RateLimitIP=4) must not be divided by "+
		"RateLimitFallbackDivisor -- that divisor exists to stop N machines from multiplying a "+
		"FLEET-WIDE budget, and /health's budget was never fleet-wide")
}
