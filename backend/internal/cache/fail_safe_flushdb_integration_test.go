//go:build integration

// Package cache — Issue #1531 Task 11d, property 2: FLUSHDB mid-request must not serve stale.
//
// FLUSHDB is BANNED against the shared cache/limiter Redis instances (cachetest.ConnectTestRedis's
// doc comment) — it is destructive to every other test sharing that instance, and a production
// footgun on Upstash's single db 0. This file exercises it anyway, against a disposable
// StartRedisContainer this process fully owns, so a FLUSHDB is exactly as safe here as it would be
// destructive there. This is what makes FLUSHDB a safe runbook step (e.g. "the cache looks corrupt,
// just flush it") rather than a gamble that could resurrect stale money.
package cache_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/cache"
	"github.com/maxnussbaum/BoostedBudget/internal/cache/cachetest"
	"github.com/maxnussbaum/BoostedBudget/internal/redisclient"
)

// TestFlushDB_MidRequest_DoesNotServeStale warms a cached value A, wipes the ENTIRE database with
// FLUSHDB (which — unlike DEL on a single generation key — destroys the generation key, the data
// key, and every control/tombstone key this cache instance has ever written, all at once), and
// then confirms a read for the same key returns the CURRENT ground truth B, not the pre-flush
// value A, and surfaces no error to the caller.
func TestFlushDB_MidRequest_DoesNotServeStale(t *testing.T) {
	container := cachetest.StartRedisContainer(t)
	ctx := context.Background()

	client, err := redisclient.New(redisclient.Options{URL: container.URL, Name: "cache-test-flushdb"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })
	c := cache.NewCache(cache.Options{Client: client})

	k := cache.Key{
		Namespace: cache.NamespaceAnalytics,
		ScopeKind: cache.ScopeUser,
		ScopeID:   "flushdb-test-user",
		Method:    "TestMethod",
		ArgHash:   "argsFlush",
	}

	// Warm the cache with value A.
	gotA, hit, err := c.GetOrLoad(ctx, k, func(ctx context.Context) ([]byte, error) {
		return []byte(`{"value":"A"}`), nil
	})
	require.NoError(t, err)
	require.False(t, hit)
	require.JSONEq(t, `{"value":"A"}`, string(gotA))

	// Confirm it's genuinely warm.
	gotAWarm, hit, err := c.GetOrLoad(ctx, k, func(ctx context.Context) ([]byte, error) {
		t.Fatal("loader must not be called on a warm hit")
		return nil, nil
	})
	require.NoError(t, err)
	require.True(t, hit)
	require.JSONEq(t, `{"value":"A"}`, string(gotAWarm))

	// The banned command, issued against a container this test fully owns.
	require.NoError(t, container.Client.FlushDB(ctx).Err(), "FLUSHDB against the disposable container")

	// The underlying source has since changed to B. A read immediately after FLUSHDB — modeling a
	// request that lands "mid" the operational window between the flush and any downstream
	// rewarming — must see B, with no error surfacing from the wiped generation/data keys.
	gotB, hit, err := c.GetOrLoad(ctx, k, func(ctx context.Context) ([]byte, error) {
		return []byte(`{"value":"B"}`), nil
	})
	require.NoError(t, err)
	assert.False(t, hit, "FLUSHDB must force a fresh loader call, not serve the wiped entry")
	assert.JSONEq(t, `{"value":"B"}`, string(gotB), "MUST return the fresh value B, never the flushed stale value A")

	// Normal operation resumes: a second read is a genuine hit against the newly-written entry.
	gotBWarm, hit, err := c.GetOrLoad(ctx, k, func(ctx context.Context) ([]byte, error) {
		t.Fatal("loader must not be called on a warm hit after FLUSHDB has been recovered from")
		return nil, nil
	})
	require.NoError(t, err)
	assert.True(t, hit)
	assert.JSONEq(t, `{"value":"B"}`, string(gotBWarm))
}
