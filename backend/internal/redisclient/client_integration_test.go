//go:build integration

package redisclient

import (
	"context"
	"net"
	"os"
	"testing"
	"time"

	redis "github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// connectTestRedisURL mirrors connectTestDB at
// backend/internal/repository/transaction_repository_explain_test.go:42-56: it reads
// REDIS_TEST_URL (never REDIS_URL — the app's own config variable must never be set in the CI job
// environment, or it leaks into every config.Load() unit test on the same runner) and skips the
// test when unset, rather than failing a developer's unrelated local run.
func connectTestRedisURL(t *testing.T) string {
	t.Helper()
	url := os.Getenv("REDIS_TEST_URL")
	if url == "" {
		t.Skip("REDIS_TEST_URL not set — skipping redisclient integration test")
	}
	return url
}

func newTestClient(t *testing.T, opts Options) Client {
	t.Helper()
	c, err := New(opts)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// newStalledListener returns a raw net.Listener that accepts TCP connections and then never
// writes a reply — a genuine stall, unlike a closed port (which fails at dial) or a mock that
// returns an error instantly. Used to prove ReadTimeout/ContextTimeoutEnabled actually bound a
// blocked read, which nothing that returns immediately could ever demonstrate.
func newStalledListener(t *testing.T) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			// Accept and hold the connection open, but never write a reply.
			t.Cleanup(func() { _ = conn.Close() })
		}
	}()

	return ln
}

func TestClientIntegration_PingSucceedsAgainstRealRedis(t *testing.T) {
	url := connectTestRedisURL(t)
	c := newTestClient(t, Options{URL: url, Name: t.Name()})

	err := c.Ping(context.Background())

	require.NoError(t, err)
}

// TestClientIntegration_PingFailsFastAgainstAnUnreachableServer uses a "blackhole" address (the
// TEST-NET-1 documentation range, RFC 5737) instead of a closed port. A closed port fails
// instantly with "connection refused" and would not exercise DialTimeout at all; a blackhole
// address never responds to the SYN, so only an enforced DialTimeout (default 1s) keeps this test
// fast instead of hanging on the OS's own multi-second (or longer) TCP connect timeout.
func TestClientIntegration_PingFailsFastAgainstAnUnreachableServer(t *testing.T) {
	c := newTestClient(t, Options{URL: "redis://192.0.2.1:6379/0", Name: t.Name()})

	start := time.Now()
	err := c.Ping(context.Background())
	elapsed := time.Since(start)

	require.Error(t, err)
	assert.Less(t, elapsed, 2*time.Second, "DialTimeout should bound the dial well under 2s")
}

// TestClientIntegration_ContextDeadlineIsHonouredNotJustTheReadTimeout is the test that actually
// proves ContextTimeoutEnabled works. A mock that returns an error instantly cannot prove this: it
// never blocks, so it can't distinguish "returned because the context expired" from "returned
// because it always returns immediately." This test uses a genuine stalled server — a raw
// net.Listener that accepts the TCP connection and then never writes a reply — so the read really
// would block for the full ReadTimeout (200ms default) if ContextTimeoutEnabled did not make
// go-redis respect the shorter 50ms context deadline instead.
func TestClientIntegration_ContextDeadlineIsHonouredNotJustTheReadTimeout(t *testing.T) {
	ln := newStalledListener(t)
	c := newTestClient(t, Options{URL: "redis://" + ln.Addr().String() + "/0", Name: t.Name()})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := c.Ping(ctx)
	elapsed := time.Since(start)

	require.Error(t, err)
	assert.Less(t, elapsed, 150*time.Millisecond,
		"context deadline (50ms) should bound the call, not ReadTimeout (200ms default) — "+
			"got %s, which is only possible if ContextTimeoutEnabled is honoured", elapsed)
}

// TestClientIntegration_ReadTimeoutBoundsACommandWithNoContextDeadline is the counterpart to the
// ContextDeadline test above: it proves ReadTimeout itself (not just the context-deadline
// shortcut) actually reaches go-redis and bounds a blocked read. context.Background() carries no
// deadline of its own, so if the `parsed.ReadTimeout = resolved.ReadTimeout` wiring in New were
// ever deleted, go-redis would fall back to ITS OWN default ReadTimeout (5s), and this call would
// take seconds instead of ~200ms.
func TestClientIntegration_ReadTimeoutBoundsACommandWithNoContextDeadline(t *testing.T) {
	ln := newStalledListener(t)
	c := newTestClient(t, Options{URL: "redis://" + ln.Addr().String() + "/0", Name: t.Name()})

	start := time.Now()
	err := c.Ping(context.Background())
	elapsed := time.Since(start)

	require.Error(t, err)
	assert.Less(t, elapsed, time.Second,
		"our 200ms default ReadTimeout should bound the call even with NO context deadline — "+
			"got %s, which would only happen if go-redis fell back to its own multi-second default", elapsed)
}

// TestClientIntegration_RunScriptFailsFastAgainstBlackholedServer proves DialerRetries=1 also
// bounds the dial for RunScript (EVALSHA/EVAL), not just Ping — RunScript is the path Task 4's
// GCRA rate limiter will actually exercise in production.
func TestClientIntegration_RunScriptFailsFastAgainstBlackholedServer(t *testing.T) {
	c := newTestClient(t, Options{URL: "redis://192.0.2.1:6379/0", Name: t.Name()})
	script := redis.NewScript("return 1")

	start := time.Now()
	_, err := c.RunScript(context.Background(), script, nil)
	elapsed := time.Since(start)

	require.Error(t, err)
	assert.Less(t, elapsed, 2*time.Second, "DialerRetries=1 should bound the dial for RunScript too, well under 2s")
}

func TestClientIntegration_PoolStatsReflectRealUsage(t *testing.T) {
	url := connectTestRedisURL(t)
	c := newTestClient(t, Options{URL: url, Name: t.Name()})

	before := c.PoolStats()

	for i := 0; i < 3; i++ {
		require.NoError(t, c.Ping(context.Background()))
	}

	after := c.PoolStats()

	assert.GreaterOrEqual(t, after.TotalConns, uint32(1))
	assert.GreaterOrEqual(t, after.Hits+after.Misses, before.Hits+before.Misses+1,
		"issuing commands should move the cumulative hits/misses counters")
}

func TestClientIntegration_CloseIsIdempotent(t *testing.T) {
	url := connectTestRedisURL(t)
	c, err := New(Options{URL: url, Name: t.Name()})
	require.NoError(t, err)

	require.NoError(t, c.Close())
	require.NoError(t, c.Close(), "a second Close must not error or panic")
}

// TestClientIntegration_GetSetSetNXMGetDel exercises the Issue #1531 Task 9 data-command
// additions against a real Redis server: a miss reports ErrNotFound (not a zero-value string with
// a nil error), SetNX only wins once per key, and MGet reports per-key presence rather than
// folding a missing key into an empty string.
func TestClientIntegration_GetSetSetNXMGetDel(t *testing.T) {
	url := connectTestRedisURL(t)
	c := newTestClient(t, Options{URL: url, Name: t.Name()})
	ctx := context.Background()

	key := t.Name() + ":k1"
	t.Cleanup(func() { _ = c.Del(context.Background(), key) })

	_, err := c.Get(ctx, key)
	assert.ErrorIs(t, err, ErrNotFound)

	require.NoError(t, c.Set(ctx, key, "v1", time.Minute))
	val, err := c.Get(ctx, key)
	require.NoError(t, err)
	assert.Equal(t, "v1", val)

	genKey := t.Name() + ":gen"
	t.Cleanup(func() { _ = c.Del(context.Background(), genKey) })
	ok, err := c.SetNX(ctx, genKey, "token-a", 0)
	require.NoError(t, err)
	assert.True(t, ok)
	ok, err = c.SetNX(ctx, genKey, "token-b", 0)
	require.NoError(t, err)
	assert.False(t, ok)

	vals, found, err := c.MGet(ctx, key, genKey, key+":missing")
	require.NoError(t, err)
	require.Len(t, vals, 3)
	require.Len(t, found, 3)
	assert.True(t, found[0])
	assert.Equal(t, "v1", vals[0])
	assert.True(t, found[1])
	assert.Equal(t, "token-a", vals[1])
	assert.False(t, found[2])

	require.NoError(t, c.Del(ctx, key))
	_, err = c.Get(ctx, key)
	assert.ErrorIs(t, err, ErrNotFound)
}

// TestClientIntegration_ConsecutiveMissesDoNotTripTheBreaker guards the interaction between Get's
// redis.Nil special-case and the circuit breaker: a cache miss must NEVER count as a breaker
// failure. Get is intentionally special-cased at client.go's "A miss is not a breaker failure" —
// see that comment — but nothing previously exercised the two together: breaker_test.go only
// drives the breaker in isolation with injected errors, and the ErrNotFound-returning Get tests
// never asserted on breaker state. If someone deleted that special-case, every cache miss would
// become a breaker failure, the breaker would trip under entirely normal operation (a cold cache
// is ALL misses), and the cache would silently stop working — nothing else in the suite would
// catch it.
//
// This issues more consecutive Get misses than FailureThreshold against a REAL Redis server (the
// interaction under test lives inside the concrete client's real go-redis call, not something a
// Fake can stand in for), then confirms the breaker is still Closed by making one more real call
// (Ping) succeed immediately rather than short-circuiting with ErrBreakerOpen.
func TestClientIntegration_ConsecutiveMissesDoNotTripTheBreaker(t *testing.T) {
	url := connectTestRedisURL(t)
	c := newTestClient(t, Options{
		URL:  url,
		Name: t.Name(),
		Breaker: BreakerOptions{
			FailureThreshold: 3, // low on purpose: fewer misses needed to prove the point
		},
	})
	ctx := context.Background()

	missingKey := t.Name() + ":definitely-absent"

	// Issue more consecutive misses than FailureThreshold. A broken implementation (no redis.Nil
	// special-case) would trip the breaker to Open partway through this loop.
	for i := 0; i < 10; i++ {
		_, err := c.Get(ctx, missingKey)
		require.ErrorIs(t, err, ErrNotFound, "iteration %d: a miss must report ErrNotFound, not a breaker error", i)
	}

	// The breaker must still be Closed: a real Ping must succeed, not fail fast with
	// ErrBreakerOpen.
	err := c.Ping(ctx)
	require.NoError(t, err, "10 consecutive cache misses must NOT have tripped the circuit breaker")
	assert.NotErrorIs(t, err, ErrBreakerOpen)
}
