//go:build integration

package redisclient

import (
	"context"
	"net"
	"os"
	"testing"
	"time"

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
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			// Accept and hold the connection open, but never write a reply: a genuine stall,
			// unlike a closed port (which fails at dial) or an instantly-erroring mock.
			t.Cleanup(func() { _ = conn.Close() })
		}
	}()

	c := newTestClient(t, Options{URL: "redis://" + ln.Addr().String() + "/0", Name: t.Name()})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	err = c.Ping(ctx)
	elapsed := time.Since(start)

	require.Error(t, err)
	assert.Less(t, elapsed, 150*time.Millisecond,
		"context deadline (50ms) should bound the call, not ReadTimeout (200ms default) — "+
			"got %s, which is only possible if ContextTimeoutEnabled is honoured", elapsed)
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
