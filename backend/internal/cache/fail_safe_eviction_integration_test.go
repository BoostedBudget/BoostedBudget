//go:build integration

// Package cache — Issue #1531 Task 11d, property 3: eviction under REAL memory pressure.
//
// Upstash's actual, non-configurable eviction policy is `optimistic-volatile`: it evicts
// volatile (TTL-bearing) keys first, but WILL evict non-TTL keys — including this package's
// generation keys — once volatile keys run short. OSS Redis (what redis:7-alpine ships) has no
// policy of that exact name. `volatile-lru` would be the wrong stand-in: it refuses to evict a
// non-TTL key at all, which would prove the opposite of what Upstash actually does and give false
// confidence that generation keys are safe from eviction. This file uses `allkeys-lru` instead,
// which evicts ANY key regardless of TTL under memory pressure. That is a strict superset of what
// optimistic-volatile permits (which only evicts non-TTL keys once volatile ones run short) — so
// if this test's property (a lost generation key is benign, never resurrecting stale data) holds
// under allkeys-lru's broader eviction eligibility, it necessarily holds under Upstash's narrower
// one too. Verified by hand against a throwaway container before writing this test: 200x50KB
// filler writes against a 5mb maxmemory/allkeys-lru instance reliably evicts a same-vintage
// non-TTL key alongside TTL-bearing ones (see task report for the exact transcript).
package cache_test

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/cache"
	"github.com/maxnussbaum/BoostedBudget/internal/cache/cachetest"
	"github.com/maxnussbaum/BoostedBudget/internal/redisclient"
)

// TestEvictionUnderMemoryPressure_GenerationKeyLoss_NeverResurrectsStale forces REAL Redis
// eviction (a small maxmemory, allkeys-lru, and enough filler writes to exceed it — confirmed via
// INFO stats' evicted_keys counter, not assumed), then proves that whatever got evicted — the
// generation key, the data key, or both — the next read reflects CURRENT ground truth, never the
// value warmed before the eviction.
func TestEvictionUnderMemoryPressure_GenerationKeyLoss_NeverResurrectsStale(t *testing.T) {
	container := cachetest.StartRedisContainer(t, "--maxmemory", "5mb", "--maxmemory-policy", "allkeys-lru")
	ctx := context.Background()

	client, err := redisclient.New(redisclient.Options{URL: container.URL, Name: "cache-test-evict"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })
	c := cache.NewCache(cache.Options{Client: client})

	k := cache.Key{
		Namespace: cache.NamespaceAnalytics,
		ScopeKind: cache.ScopeUser,
		ScopeID:   "eviction-test-user",
		Method:    "TestMethod",
		ArgHash:   "argsEvict",
	}

	// Warm the cache with value A: mints a generation key (no TTL) and a data key (TTL).
	gotA, hit, err := c.GetOrLoad(ctx, k, func(ctx context.Context) ([]byte, error) {
		return []byte(`{"value":"A"}`), nil
	})
	require.NoError(t, err)
	require.False(t, hit)
	require.JSONEq(t, `{"value":"A"}`, string(gotA))

	gotAWarm, hit, err := c.GetOrLoad(ctx, k, func(ctx context.Context) ([]byte, error) {
		t.Fatal("loader must not be called on a warm hit")
		return nil, nil
	})
	require.NoError(t, err)
	require.True(t, hit)
	require.JSONEq(t, `{"value":"A"}`, string(gotAWarm))

	before, err := container.Client.Info(ctx, "stats").Result()
	require.NoError(t, err)
	evictedBefore := parseEvictedKeys(t, before)

	// Force real memory pressure: write filler far past the 5mb ceiling. 250 x 50KB = ~12.5mb,
	// comfortably over maxmemory even accounting for Redis's own ~1mb base footprint (measured by
	// hand against a throwaway container before writing this test).
	filler := strings.Repeat("x", 50_000)
	for i := 0; i < 250; i++ {
		// Best-effort: once eviction starts, some of THESE writes may themselves get evicted
		// immediately, which is fine — they exist only to consume memory, not to be read back.
		_ = container.Client.Set(ctx, fmt.Sprintf("filler:%d", i), filler, 0).Err()
	}

	after, err := container.Client.Info(ctx, "stats").Result()
	require.NoError(t, err)
	evictedAfter := parseEvictedKeys(t, after)
	require.Greater(t, evictedAfter, evictedBefore,
		"sanity: this test proves nothing unless Redis genuinely evicted keys under the memory pressure it created")

	// Whatever was evicted (generation key, data key, both, or neither — allkeys-lru's exact
	// victim selection is Redis's own business, not this test's), the cache must never resurrect
	// the pre-eviction value A once ground truth has moved to B.
	gotB, _, err := c.GetOrLoad(ctx, k, func(ctx context.Context) ([]byte, error) {
		return []byte(`{"value":"B"}`), nil
	})
	require.NoError(t, err)
	assert.JSONEq(t, `{"value":"B"}`, string(gotB), "eviction must never resurrect the stale pre-eviction value A")
	assert.NotEqual(t, `{"value":"A"}`, string(gotB))
}

func parseEvictedKeys(t *testing.T, info string) int64 {
	t.Helper()
	for _, line := range strings.Split(info, "\r\n") {
		if strings.HasPrefix(line, "evicted_keys:") {
			n, err := strconv.ParseInt(strings.TrimPrefix(line, "evicted_keys:"), 10, 64)
			require.NoError(t, err)
			return n
		}
	}
	t.Fatal("evicted_keys not found in INFO stats output")
	return 0
}
