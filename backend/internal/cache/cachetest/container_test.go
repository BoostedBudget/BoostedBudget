//go:build integration

package cachetest_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"testing"

	redis "github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/cache/cachetest"
)

// randomSuffix returns 8 random hex digits, for building a key name unique to this one test
// invocation — so concurrent `go test` runs (or a leftover key from a previously killed run)
// sharing the same real CACHE_REDIS_TEST_URL instance can never collide on it.
func randomSuffix(t *testing.T) string {
	t.Helper()
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatalf("randomSuffix: crypto/rand unavailable: %v", err)
	}
	return hex.EncodeToString(b[:])
}

// TestStartRedisContainer_FlushDBIsGenuinelyIsolatedFromTheSharedInstance is the required proof
// that the disposable container is what it claims to be. FLUSHDB is banned on the shared
// cache-test instance (CACHE_REDIS_TEST_URL, redis://localhost:6397/0 locally) because it is
// destructive to every other test that shares it — see ConnectTestRedis's doc comment. This test:
//
//  1. starts its own throwaway container via StartRedisContainer,
//  2. writes a marker key (unique to this run — see randomSuffix) on the SHARED instance first
//     (skipped cleanly if unconfigured),
//  3. writes a same-named key into the disposable container and FLUSHDBs the container,
//  4. confirms the container's key is gone, and
//  5. confirms the shared instance's marker key is COMPLETELY untouched by that FLUSHDB —
//     the actual proof of isolation, not just "FLUSHDB worked on some client".
func TestStartRedisContainer_FlushDBIsGenuinelyIsolatedFromTheSharedInstance(t *testing.T) {
	ctx := context.Background()
	key := "cachetest-flushdb-proof-" + randomSuffix(t)

	shared := cachetest.ConnectTestRedis(t)
	require.NoError(t, shared.Set(ctx, key, "shared-value", 0))

	container := cachetest.StartRedisContainer(t)
	require.NotEmpty(t, container.URL)

	require.NoError(t, container.Client.Set(ctx, key, "container-value", 0).Err())
	got, err := container.Client.Get(ctx, key).Result()
	require.NoError(t, err)
	assert.Equal(t, "container-value", got)

	require.NoError(t, container.Client.FlushDB(ctx).Err())

	_, err = container.Client.Get(ctx, key).Result()
	assert.ErrorIs(t, err, redis.Nil, "key must be gone from the disposable container after FLUSHDB")

	sharedVal, err := shared.Get(ctx, key)
	require.NoError(t, err, "the shared instance's key must survive the disposable container's FLUSHDB untouched")
	assert.Equal(t, "shared-value", sharedVal)

	// Cleanup: this test is the one exception that gets to touch the shared instance's key it
	// itself just wrote — every other test in this package still must never FLUSHDB it.
	t.Cleanup(func() { _ = shared.Del(ctx, key) })
}

// TestStartRedisContainer_AcceptsServerArgsForEvictionTests confirms serverArgs reach the
// container's redis-server command line, using a real CONFIG GET round-trip — the fail-safe
// suite's eviction test depends on --maxmemory/--maxmemory-policy actually taking effect.
func TestStartRedisContainer_AcceptsServerArgsForEvictionTests(t *testing.T) {
	ctx := context.Background()

	container := cachetest.StartRedisContainer(t, "--maxmemory", "10mb", "--maxmemory-policy", "allkeys-lru")

	vals, err := container.Client.ConfigGet(ctx, "maxmemory-policy").Result()
	require.NoError(t, err)
	assert.Equal(t, "allkeys-lru", vals["maxmemory-policy"])
}
