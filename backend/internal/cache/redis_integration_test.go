//go:build integration

package cache_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/cache"
	"github.com/maxnussbaum/BoostedBudget/internal/cache/cachetest"
)

// TestGenerationKeyLost_OrphansOldEntries_DoesNotServeStale is THE proof of this package's whole
// design. Warm the cache with value A, DEL the generation key directly (simulating an
// optimistic-volatile eviction of a non-TTL key — verified against Upstash's documented eviction
// behaviour), mutate the underlying source to B, then read again. It MUST return B.
//
// A counter-based generation implementation fails this test: losing the counter resets it to 1,
// which is exactly the generation the OLD entry (value A) was written under, so a counter-based
// cache would serve A — stale financial data — forever (or until A's own TTL expires). This test
// is the reason generations are minted random tokens, never counted.
func TestGenerationKeyLost_OrphansOldEntries_DoesNotServeStale(t *testing.T) {
	c, scopeKind, scopeID := cachetest.NewIsolatedCache(t)
	ctx := context.Background()

	k := cache.Key{
		Namespace: cache.NamespaceAnalytics,
		ScopeKind: scopeKind,
		ScopeID:   scopeID,
		Method:    "TestMethod",
		ArgHash:   "argsA",
	}

	// Warm the cache with value A.
	gotA, hit, err := c.GetOrLoad(ctx, k, func(ctx context.Context) ([]byte, error) {
		return []byte(`{"value":"A"}`), nil
	})
	require.NoError(t, err)
	require.False(t, hit)
	require.JSONEq(t, `{"value":"A"}`, string(gotA))

	// Confirm it's now warm (loader NOT called).
	gotAWarm, hit, err := c.GetOrLoad(ctx, k, func(ctx context.Context) ([]byte, error) {
		t.Fatal("loader must not be called on a warm hit")
		return nil, nil
	})
	require.NoError(t, err)
	require.True(t, hit)
	require.JSONEq(t, `{"value":"A"}`, string(gotAWarm))

	// Simulate the generation key being evicted by Redis (Upstash's optimistic-volatile policy
	// CAN evict a non-TTL key — this is a direct DEL against the SAME generation key this
	// package's own key.go renders, not a test-only shortcut).
	genKey := cache.GenerationKey(cache.ScopeTag(scopeKind, scopeID))
	client := cachetest.ConnectTestRedis(t)
	require.NoError(t, client.Del(ctx, genKey))

	// The underlying source has since changed to B.
	gotB, hit, err := c.GetOrLoad(ctx, k, func(ctx context.Context) ([]byte, error) {
		return []byte(`{"value":"B"}`), nil
	})
	require.NoError(t, err)
	assert.False(t, hit, "losing the generation key must orphan the old entry, forcing a fresh loader call")
	assert.JSONEq(t, `{"value":"B"}`, string(gotB), "MUST return the fresh value B, never the orphaned stale value A")
}

// TestGenerationKeys_HaveNoTTL_DataKeysDo is a hygiene assertion only — NOT a claim of eviction
// immunity (that property does not exist on Upstash's optimistic-volatile policy). It confirms
// this package writes generation keys without an expiry (PTTL == -1) while data keys DO carry the
// namespace's TTL.
func TestGenerationKeys_HaveNoTTL_DataKeysDo(t *testing.T) {
	c, scopeKind, scopeID := cachetest.NewIsolatedCache(t)
	client := cachetest.ConnectTestRedis(t)
	ctx := context.Background()

	k := cache.Key{
		Namespace: cache.NamespaceAnalytics,
		ScopeKind: scopeKind,
		ScopeID:   scopeID,
		Method:    "TestMethod",
		ArgHash:   "argsHygiene",
	}
	_, _, err := c.GetOrLoad(ctx, k, func(ctx context.Context) ([]byte, error) {
		return []byte(`{"value":"A"}`), nil
	})
	require.NoError(t, err)

	genKey := cache.GenerationKey(cache.ScopeTag(scopeKind, scopeID))
	genPTTL, err := pttl(ctx, t, genKey)
	require.NoError(t, err)
	assert.Equal(t, time.Duration(-1), genPTTL, "generation key must have NO TTL (this is a hygiene check, not an eviction-immunity claim)")

	gen, err := cache.NewGenerations(client).Get(ctx, scopeKind, scopeID)
	require.NoError(t, err)
	// Discover the data key GetOrLoad actually wrote instead of reconstructing one with a
	// hand-picked schema version (k.DataKey(1, gen) went stale the moment round 12 bumped
	// NamespaceAnalytics to SchemaVersion 2 — namespace.go has no exported accessor for a
	// namespace's version from this external test package, and hardcoding "2" here would only
	// buy one more bump before going stale again). The pattern wildcards the "vN" segment and
	// otherwise pins every other segment of key.go's DataKey format (scope tag, namespace,
	// generation, method, arg hash) via genKey's own rendered prefix, so it can only match the
	// one data key this test itself wrote — it cannot match genKey (no ":gen" suffix, no
	// namespace segment) and cannot match another test's key (scopeTag is unique per test, see
	// cachetest.NewIsolatedCache).
	prefix := genKey[:len(genKey)-len(":gen")]
	pattern := fmt.Sprintf("%s:%s:v*:g%s:%s:%s", prefix, k.Namespace, gen, k.Method, k.ArgHash)
	dataKey := findExactlyOneKey(ctx, t, pattern)
	dataPTTL, err := pttl(ctx, t, dataKey)
	require.NoError(t, err)
	assert.Greater(t, dataPTTL, time.Duration(0), "data key must carry a real TTL")
}
