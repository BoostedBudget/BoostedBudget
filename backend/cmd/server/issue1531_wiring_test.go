package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2/adapters/humaecho"
	"github.com/labstack/echo/v4"
	"github.com/maxnussbaum/BoostedBudget/internal/config"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/health"
	humaconfig "github.com/maxnussbaum/BoostedBudget/internal/huma"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/operations"
	"github.com/maxnussbaum/BoostedBudget/internal/ratelimitstore"
	"github.com/maxnussbaum/BoostedBudget/internal/redisclient"
	redis "github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// slowFailingRedisClient is a redisclient.Client that IGNORES its context and always fails after
// a fixed real delay. Deliberately ignoring ctx: a health.Options.Timeout wrapper only bounds the
// context handed to the probe function — a probe that ignores context cancellation (as a raw
// blocking call sometimes does) can still run for its own full duration. Wiring a Redis check
// backed by this client is how TestWiring_HealthProbeSucceedsAndIsFastWithRedisCompletelyDown
// proves GetHealth structurally never invokes it, rather than merely invoking-but-bounding it.
type slowFailingRedisClient struct {
	name  string
	delay time.Duration
}

func (s slowFailingRedisClient) Name() string         { return s.name }
func (s slowFailingRedisClient) RedactedAddr() string { return "slow:0" }

func (s slowFailingRedisClient) Ping(_ context.Context) error {
	time.Sleep(s.delay)
	return errors.New("simulated redis completely down")
}

func (s slowFailingRedisClient) RunScript(_ context.Context, _ *redis.Script, _ []string, _ ...any) (any, error) {
	time.Sleep(s.delay)
	return nil, errors.New("simulated redis completely down")
}

func (s slowFailingRedisClient) PoolStats() redisclient.PoolStats { return redisclient.PoolStats{} }
func (s slowFailingRedisClient) Close() error                     { return nil }

// Get, Set, SetNX, MGet, and Del (added to redisclient.Client for Issue #1531 Task 9) are never
// exercised by this test — it only ever wires slowFailingRedisClient in as a Ping/RunScript-based
// health check — but the interface requires an implementation of every method.
func (s slowFailingRedisClient) Get(_ context.Context, _ string) (string, error) {
	time.Sleep(s.delay)
	return "", errors.New("simulated redis completely down")
}

func (s slowFailingRedisClient) Set(_ context.Context, _, _ string, _ time.Duration) error {
	time.Sleep(s.delay)
	return errors.New("simulated redis completely down")
}

func (s slowFailingRedisClient) SetNX(_ context.Context, _, _ string, _ time.Duration) (bool, error) {
	time.Sleep(s.delay)
	return false, errors.New("simulated redis completely down")
}

func (s slowFailingRedisClient) MGet(_ context.Context, _ ...string) ([]string, []bool, error) {
	time.Sleep(s.delay)
	return nil, nil, errors.New("simulated redis completely down")
}

func (s slowFailingRedisClient) Del(_ context.Context, _ ...string) error {
	time.Sleep(s.delay)
	return errors.New("simulated redis completely down")
}

// Scan (added to redisclient.Client for Issue #1531 Task 10b's GDPR purge) is never exercised by
// this test either, for the same reason as Get/Set/SetNX/MGet/Del above.
func (s slowFailingRedisClient) Scan(_ context.Context, _ string) ([]string, error) {
	time.Sleep(s.delay)
	return nil, errors.New("simulated redis completely down")
}

var _ redisclient.Client = slowFailingRedisClient{}

// TestWiring_HealthProbeSucceedsAndIsFastWithRedisCompletelyDown is the self-inflicted-outage
// guard for Issue #1531 Task 7c: /health is Fly's liveness probe, and this proves that wiring a
// REAL (if completely down) Redis client into health.Registry.Redis cannot slow down or fail
// /health, because operations.HealthHandler.GetHealth structurally never reads registry.Redis.
//
// The Redis client used here ignores ctx entirely and sleeps 5 real seconds before failing — far
// longer than createRedisCheck's own 500ms Timeout — specifically so that if GetHealth were ever
// changed to also consult registry.Redis, this test would time out and fail loudly instead of
// merely running slow within some tolerance.
func TestWiring_HealthProbeSucceedsAndIsFastWithRedisCompletelyDown(t *testing.T) {
	registry := health.Registry{
		DB:    createDBCheck(alwaysHealthyPinger{}),
		Redis: createRedisCheck(slowFailingRedisClient{name: "limiter", delay: 5 * time.Second}),
	}

	e := echo.New()
	humaAPI := humaecho.NewV4(e, humaconfig.NewAPIConfig("test", "test"))
	handler := operations.NewHealthHandler(slog.Default(), registry)
	operations.RegisterHealthRoutes(humaAPI, handler)

	srv := httptest.NewServer(e)
	defer srv.Close()

	start := time.Now()
	resp, err := http.Get(srv.URL + "/health")
	elapsed := time.Since(start)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Less(t, elapsed, 100*time.Millisecond,
		"GET /health must stay fast even with a completely-down (5s-delayed-failure) Redis client wired into the registry")
}

// TestWiring_ApiRequestsAreStillRateLimitedWithRedisDown proves that when the Redis-backed
// primary rate-limit backend is down, requests are still rate limited — via the in-memory
// fallback (internal/middleware's fallbackLimiter, Task 5) — rather than the outage silently
// disabling rate limiting altogether.
func TestWiring_ApiRequestsAreStillRateLimitedWithRedisDown(t *testing.T) {
	e := echo.New()
	cfCIDRRef := newOrderingTestCFCIDRRef()
	cfg := &config.Config{
		Environment:         "development", // in CloudflareOnly's bypassEnvironments
		RateLimitIP:         2,
		RateLimitAuth:       2,
		RateLimitMaxEntries: 100,
	}

	fake := redisclient.NewFake("limiter")
	fake.RunScriptErr = errors.New("simulated redis down")
	backend := ratelimitstore.NewRedisBackend(fake, ratelimitstore.Options{
		RegularLimit: cfg.RateLimitIP,
		AuthLimit:    cfg.RateLimitAuth,
		Window:       time.Minute,
	})
	done := make(chan struct{})
	defer close(done)

	registerCloudflareAndRateLimitMiddlewareWithBackend(e, cfCIDRRef, cfg, backend, done, nil)
	e.GET("/", func(c echo.Context) error { return c.String(http.StatusOK, "ok") })

	srv := httptest.NewServer(e)
	defer srv.Close()

	var lastStatus int
	for i := 0; i < 5; i++ {
		resp, err := srv.Client().Get(srv.URL + "/")
		require.NoError(t, err)
		lastStatus = resp.StatusCode
		resp.Body.Close()
	}

	assert.Equal(t, http.StatusTooManyRequests, lastStatus,
		"requests must still be rate limited (via the in-memory fallback) even though the Redis-backed primary always errors")
}

// TestBuildRateLimitBackend_SentinelLimitEqualsRegularLimitNotAuthLimit is the fix-round-1
// regression guard for Issue #1531 Task 7: the sentinel bucket ("rl:v1:sentinel") is used
// whenever a Cloudflare-gated request's Cf-Connecting-Ip is missing, duplicated, or malformed —
// and under Redis it is ONE fleet-wide key, not per-machine. Wiring it to cfg.RateLimitAuth (the
// stricter, per-identity auth-endpoint limit) would mean a Cloudflare header anomaly collapses
// ALL affected fleet-wide traffic into a bucket capped at the auth limit — 10x tighter than
// intended, in precisely the incident where the API most needs to keep serving. This asserts the
// wired sentinel limit matches RegularLimit (cfg.RateLimitIP), not AuthLimit.
//
// Uses distinct RateLimitIP/RateLimitAuth values specifically so a backend that wired
// AuthLimit into the sentinel tier would produce a DIFFERENT (and wrong) emission interval here —
// see the assertion's failure message for what a reintroduced regression would report.
func TestBuildRateLimitBackend_SentinelLimitEqualsRegularLimitNotAuthLimit(t *testing.T) {
	fake := redisclient.NewFake("limiter")
	fake.RunScriptResult = []any{int64(1), int64(0), int64(9)}

	cfg := &config.Config{
		RateLimitIP:   100, // RegularLimit — what SentinelLimit must equal
		RateLimitAuth: 10,  // AuthLimit — what SentinelLimit must NOT equal
	}

	backend, err := buildRateLimitBackend(fake, cfg)
	require.NoError(t, err)
	require.NotNil(t, backend)

	redisBackend, ok := backend.(*ratelimitstore.RedisBackend)
	require.True(t, ok, "buildRateLimitBackend must return a *ratelimitstore.RedisBackend when given a non-nil client")

	_, err = redisBackend.Allow(context.Background(), domain.RateLimitTierSentinel, "")
	require.NoError(t, err)

	calls := fake.ScriptCalls()
	require.Len(t, calls, 1)

	// emission_interval_us = window_us / limit (see ratelimitstore's gcraScript doc comment).
	// window defaults to 1 minute (buildRateLimitBackend sets Window: time.Minute explicitly).
	wantEmissionRegular := time.Minute.Microseconds() / int64(cfg.RateLimitIP)
	wantEmissionAuth := time.Minute.Microseconds() / int64(cfg.RateLimitAuth)

	gotEmission, ok := calls[0].Args[0].(int64)
	require.True(t, ok, "script call ARGV[1] (emission_interval_us) must be int64, got %T", calls[0].Args[0])

	assert.Equal(t, wantEmissionRegular, gotEmission,
		"sentinel bucket's emission interval must be derived from RegularLimit (cfg.RateLimitIP), not AuthLimit — "+
			"the sentinel bucket is a single FLEET-WIDE key, and capping it at the auth limit turns a Cloudflare "+
			"header anomaly into a fleet-wide outage")
	assert.NotEqual(t, wantEmissionAuth, gotEmission,
		"sentinel bucket's emission interval must NOT match what AuthLimit alone would produce")
}

// TestRateLimitFallbackLimit is the fix-round-1 regression test for Issue #1531 Task 8's
// RATE_LIMIT_FALLBACK_DIVISOR wiring: rateLimitFallbackLimit divides the in-memory fallback
// limiter's per-machine limit so N machines independently falling back to memory-only limiting
// don't multiply the intended fleet-wide budget by N.
//
// The limit=0 case is the discrimination proof for the fix itself: the FIRST version of this
// function floored limit<=0 to 1, which silently turned a self-correcting misconfiguration
// (RATE_LIMIT_IP=0, which middleware.RateLimitConfig.withDefaults() maps to its built-in default
// of 100) into a near-total lockout (1 req/min) — a real behaviour regression this function
// introduced, not a pre-existing one. limit<=0 must pass through UNCHANGED so
// withDefaults()'s "<=0 means use the default" contract still fires downstream, exactly as it did
// before RATE_LIMIT_FALLBACK_DIVISOR existed.
func TestRateLimitFallbackLimit(t *testing.T) {
	testCases := []struct {
		name    string
		limit   int
		divisor int
		want    int
	}{
		{"divisor 0 treated as 1 (no division)", 100, 0, 100},
		{"divisor negative treated as 1 (no division)", 100, -3, 100},
		{"divisor 1 is a no-op", 100, 1, 100},
		{"normal divisor divides evenly", 100, 2, 50},
		{"divisor larger than limit floors at 1, never 0", 100, 1000, 1},
		{
			"limit 0 passes through UNCHANGED, not floored to 1 — must keep " +
				"middleware.RateLimitConfig.withDefaults()'s <=0-means-default contract alive",
			0, 1, 0,
		},
		{"limit negative also passes through unchanged", -5, 2, -5},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := rateLimitFallbackLimit(tc.limit, tc.divisor)
			assert.Equal(t, tc.want, got,
				"rateLimitFallbackLimit(%d, %d) must equal %d", tc.limit, tc.divisor, tc.want)
		})
	}
}
