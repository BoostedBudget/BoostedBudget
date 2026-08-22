package main

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/health"
	"github.com/maxnussbaum/BoostedBudget/internal/redisclient"
)

// errDBNotConfigured, errRedisNotConfigured, and errClerkNotConfigured are the sentinel probe
// errors returned when a dependency isn't wired for this deployment. health.Memoize wraps these
// in fmt.Errorf("health check %q: %w", ...), so errors.Is still finds them through the wrapping.
var (
	// errDBNotConfigured deliberately makes a nil pool an ERROR, not a healthy no-op (Issue #1531
	// Task 7c). validate() already forbids DATABASE_URL from being empty in production/staging,
	// so reaching this in those environments means a real outage. The one behaviour change this
	// introduces: a bare local `go run` with no DATABASE_URL set now gets a 503 from /health
	// instead of an unconditional "ok" — an API with no database is not healthy.
	errDBNotConfigured = fmt.Errorf("database: not configured: %w", health.ErrNotConfigured)
	// errRedisNotConfigured means no Redis client is wired for this deployment. Redis stays
	// NON-CRITICAL regardless (see createRedisCheck's doc comment) — this error only ever
	// surfaces on the admin dashboard, never on the public /health endpoint (Task 6 structurally
	// prevents GetHealth from ever reading registry.Redis at all).
	//
	// It wraps health.ErrNotConfigured (via %w) so system_health_service.go's checkRedis can tell
	// "not wired" apart from "wired but down" via errors.Is — the Check this error travels inside
	// is never nil in production (see createRedisCheck below), so that distinction can only be
	// made from the error's identity, not the Check's. See Issue #1531 L2.
	errRedisNotConfigured = fmt.Errorf("redis: not configured: %w", health.ErrNotConfigured)
	// errClerkNotConfigured means CLERK_SECRET_KEY is unset. Same non-critical treatment as Redis,
	// and wraps health.ErrNotConfigured for the same reason (see errRedisNotConfigured above).
	errClerkNotConfigured = fmt.Errorf("clerk: not configured (CLERK_SECRET_KEY unset): %w", health.ErrNotConfigured)
)

// dbPinger narrows *pgxpool.Pool to the one method createDBCheck needs, so unit tests can inject
// a fake without a real database connection. *pgxpool.Pool satisfies this trivially.
//
// ⚠ Callers MUST pass a true nil interface (not a typed-nil *pgxpool.Pool) when there is no pool —
// see main()'s call site, which builds this explicitly via a `var dbPinger` guarded by `db != nil`
// rather than converting a possibly-nil *pgxpool.Pool directly. A typed-nil *pgxpool.Pool boxed
// into this interface would compare != nil here and then panic inside pool.Ping.
type dbPinger interface {
	Ping(ctx context.Context) error
}

const (
	// dbCheckTTL/dbCheckTimeout: DB is CRITICAL — only this check can 503 the public /health.
	dbCheckTTL     = 5 * time.Second
	dbCheckTimeout = time.Second

	// redisCheckTTL/redisCheckTimeout: Redis is NON-CRITICAL. Never promote this — see
	// createRedisCheck's doc comment for why a Redis blip must never cycle the whole Fly app.
	redisCheckTTL     = 5 * time.Second
	redisCheckTimeout = 500 * time.Millisecond

	// clerkCheckTTL is deliberately longer than DB/Redis: Clerk is a rate-limited third-party
	// call, so probing it every 5s would burn budget for no operational benefit.
	clerkCheckTTL     = 30 * time.Second
	clerkCheckTimeout = 2 * time.Second

	// clerkJWKSURL is the production endpoint createClerkCheck probes. Tests use
	// createClerkCheckWithURL to point at an httptest server instead.
	clerkJWKSURL = "https://api.clerk.com/v1/jwks"
)

// createDBCheck builds the memoized, CRITICAL database health check (Issue #1531 Task 7c). This
// is the only check operations.HealthHandler.GetHealth ever reads — see that handler's doc
// comment for why Redis/Clerk are structurally excluded from the public /health path.
func createDBCheck(pool dbPinger) health.Check {
	return health.Memoize(health.Options{
		Name:    "db",
		TTL:     dbCheckTTL,
		Timeout: dbCheckTimeout,
	}, func(ctx context.Context) error {
		if pool == nil {
			return errDBNotConfigured
		}
		return pool.Ping(ctx)
	})
}

// createRedisCheck builds the memoized, NON-CRITICAL Redis health check.
//
// ⚠ REDIS MUST STAY NON-CRITICAL. /health is Fly's liveness probe: if Redis health ever flowed
// into the public endpoint's decision, a Redis blip would fail the liveness check on every
// machine simultaneously and Fly would cycle the entire app — a self-inflicted outage caused by a
// dependency this app is explicitly designed to degrade gracefully without (see
// internal/middleware's fallback limiter). This is an explicit non-goal of issue #1531; do not
// wire this Check into operations.HealthHandler's registry in any way that changes that.
func createRedisCheck(client redisclient.Client) health.Check {
	return health.Memoize(health.Options{
		Name:    "redis",
		TTL:     redisCheckTTL,
		Timeout: redisCheckTimeout,
	}, func(ctx context.Context) error {
		if client == nil {
			return errRedisNotConfigured
		}
		return client.Ping(ctx)
	})
}

// createClerkCheck builds the memoized, non-critical Clerk health check against the real
// production JWKS endpoint. A nil httpClient falls back to http.DefaultClient.
func createClerkCheck(secretKey string, httpClient *http.Client) health.Check {
	return createClerkCheckWithURL(secretKey, httpClient, clerkJWKSURL)
}

// createClerkCheckWithURL is createClerkCheck with the probed URL as a parameter — the test seam
// TestCreateClerkCheck_DoesNotCallClerkMoreThanOncePerTTL uses to point at an httptest server
// instead of the real api.clerk.com.
func createClerkCheckWithURL(secretKey string, httpClient *http.Client, url string) health.Check {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return health.Memoize(health.Options{
		Name:    "clerk",
		TTL:     clerkCheckTTL,
		Timeout: clerkCheckTimeout,
	}, func(ctx context.Context) error {
		if secretKey == "" {
			return errClerkNotConfigured
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return fmt.Errorf("build clerk jwks request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+secretKey)

		resp, err := httpClient.Do(req)
		if err != nil {
			return fmt.Errorf("clerk jwks request: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode >= 400 {
			return fmt.Errorf("clerk jwks request: unexpected status %d", resp.StatusCode)
		}
		return nil
	})
}
