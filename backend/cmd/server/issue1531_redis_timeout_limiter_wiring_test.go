package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/maxnussbaum/BoostedBudget/internal/config"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// slowRejectingBackend answers after a fixed delay, respecting ctx cancellation like a real
// go-redis EVAL call, and always REJECTS -- a decision the in-memory fallback (which allows an
// under-budget request) would never produce, so a 429 here can only mean this backend's own
// answer was actually waited for and honored.
type slowRejectingBackend struct{ delay time.Duration }

func (slowRejectingBackend) Name() string { return "slow-redis" }
func (b slowRejectingBackend) Allow(ctx context.Context, _ domain.RateLimitTier, _ string) (domain.RateLimitDecision, error) {
	select {
	case <-time.After(b.delay):
		return domain.RateLimitDecision{Allowed: false, RetryAfter: time.Second}, nil
	case <-ctx.Done():
		return domain.RateLimitDecision{}, ctx.Err()
	}
}

// TestWiring_RedisTimeoutMsIsActuallyAppliedToTheLimiterPrimaryTimeout is the #1531 L7 regression
// at the main.go wiring boundary (the middleware.RateLimitConfig.PrimaryTimeout field itself is
// separately unit-tested in internal/middleware/rate_limit_test.go's
// TestIPRateLimiter_PrimaryTimeoutIsConfigurable). Nothing pinned that
// registerCloudflareAndRateLimitMiddlewareWithBackend actually PASSES cfg.RedisTimeoutMs into
// PrimaryTimeout at the call site, as opposed to leaving it unset (which silently falls back to
// the hardcoded 50ms default) -- a regression there would pass every existing test the same way
// the fallback-divisor wiring gap once did (see issue1531_fallback_divisor_main_wiring_test.go).
//
// A primary that takes 80ms to answer is strictly slower than the pre-fix hardcoded 50ms budget
// and strictly faster than RATE_LIMIT config's RedisTimeoutMs=200 here. If main.go stopped
// reading cfg.RedisTimeoutMs (or read it into the wrong field), the 80ms primary would time out
// and this test would see 200 OK (memory fallback allows) instead of 429 (the primary's own,
// waited-for, rejecting decision).
func TestWiring_RedisTimeoutMsIsActuallyAppliedToTheLimiterPrimaryTimeout(t *testing.T) {
	e := echo.New()
	cfCIDRRef := newOrderingTestCFCIDRRef()
	cfg := &config.Config{
		Environment:              "development", // in CloudflareOnly's bypassEnvironments
		RateLimitIP:              100,
		RateLimitAuth:            100,
		RateLimitFallbackDivisor: 1,
		RateLimitMaxEntries:      100,
		RedisTimeoutMs:           200, // must reach PrimaryTimeout, not just the go-redis client
	}

	done := make(chan struct{})
	defer close(done)

	backend := slowRejectingBackend{delay: 80 * time.Millisecond}
	registerCloudflareAndRateLimitMiddlewareWithBackend(e, cfCIDRRef, cfg, backend, done, nil)
	e.GET("/", func(c echo.Context) error { return c.String(http.StatusOK, "ok") })

	srv := httptest.NewServer(e)
	defer srv.Close()

	resp, err := srv.Client().Get(srv.URL + "/")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusTooManyRequests, resp.StatusCode,
		"RedisTimeoutMs=200 must reach the limiter's PrimaryTimeout so an 80ms primary's own "+
			"rejecting decision is waited for and honored, rather than main.go leaving "+
			"PrimaryTimeout unset and silently falling back to the hardcoded 50ms default "+
			"(which would time out the 80ms primary and let the memory fallback's 200 OK through)")
}
