//go:build integration

// Package services — Issue #1531 Task 11d, property 1 (service level).
//
// internal/cache's own TestGenerationKeyLost_OrphansOldEntries_DoesNotServeStale (Task 9) proves
// the primitive: losing a generation key orphans the old data key rather than resurrecting it.
// This file extends that proof one layer up, through the real production path: a
// CachedAnalyticsService backed by real Postgres and real Redis, exactly as main.go wires it. The
// primitive-level test uses a synthetic loader returning "A" then "B"; this test uses an actual
// AnalyticsService method reading actual Postgres rows, so it also exercises the key-building and
// generation-lookup code AnalyticsService's cached wrapper actually calls.
package services

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/cache"
	"github.com/maxnussbaum/BoostedBudget/internal/cache/cachetest"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// TestGenerationKeyLost_ServiceLevel_DoesNotServeStale warms a real cached analytics read (
// GetCashFlowSummary, reading real Postgres transactions through the real cache-key/generation
// machinery), DELs the generation key directly (simulating the same optimistic-volatile eviction
// modeled at the primitive level), mutates Postgres, and re-reads. The re-read MUST reflect the
// mutation — never the value warmed before the generation key was lost.
func TestGenerationKeyLost_ServiceLevel_DoesNotServeStale(t *testing.T) {
	pool := connectIntegrationDB(t)
	redisClient := cachetest.ConnectTestRedis(t)
	c := cache.NewCache(cache.Options{Client: redisClient})
	ctx := context.Background()

	f := newParityFixture(t, ctx, pool)
	analyticsRepo := repository.NewAnalyticsRepository(pool)
	balanceRepo := repository.NewBalanceSnapshotRepository(pool)
	inner := NewAnalyticsService(analyticsRepo)
	inner.SetBalanceSnapshotRepo(balanceRepo)
	cachedSvc := NewCachedAnalyticsService(inner, c)

	now := time.Now().UTC()
	periodStart := now.AddDate(0, 0, -14)
	periodEnd := now.Add(time.Hour)
	filters := domain.CashFlowFilters{StartDate: periodStart, EndDate: periodEnd}

	f.seedTransaction(t, ctx, 20.00, now.AddDate(0, 0, -3), "Groceries", "Local Grocer", "FOOD_AND_DRINK", "FOOD_AND_DRINK_GROCERIES")

	// Warm the cache.
	wantBefore, err := cachedSvc.GetCashFlowSummary(ctx, f.userID, filters)
	require.NoError(t, err)

	// Confirm it is genuinely warm: a second call must be a cache HIT (loader not re-run), proven
	// via the same cache.Snapshot/Delta mechanism the parity harness uses.
	before := cache.Snapshot()
	warmAgain, err := cachedSvc.GetCashFlowSummary(ctx, f.userID, filters)
	require.NoError(t, err)
	after := cache.Snapshot()
	delta := after.Delta(before)[cache.NamespaceAnalytics]
	require.Equal(t, float64(1), delta.Hits, "sanity: second read must be a cache hit before we lose the generation key")
	assert.JSONEq(t, mustJSON(t, wantBefore), mustJSON(t, warmAgain))

	// Simulate the generation key being evicted (Upstash's optimistic-volatile policy can evict a
	// non-TTL key at any time) by deleting it directly — the same key this package's own key.go
	// would render, not a test-only shortcut.
	genKey := cache.GenerationKey(cache.ScopeTag(cache.ScopeUser, f.userID))
	require.NoError(t, redisClient.Del(ctx, genKey))

	// Mutate the underlying Postgres data the cached read depends on.
	f.seedTransaction(t, ctx, 55.00, now.AddDate(0, 0, -2), "Electronics", "Gadget Store", "GENERAL_MERCHANDISE", "GENERAL_MERCHANDISE_OTHER")

	wantAfter, err := inner.GetCashFlowSummary(ctx, f.userID, filters)
	require.NoError(t, err)
	require.NotEqual(t, mustJSON(t, wantBefore), mustJSON(t, wantAfter),
		"sanity: the mutation must actually move the ground-truth result, or this test proves nothing")

	beforeRead := cache.Snapshot()
	gotAfter, err := cachedSvc.GetCashFlowSummary(ctx, f.userID, filters)
	require.NoError(t, err)
	afterRead := cache.Snapshot()
	readDelta := afterRead.Delta(beforeRead)[cache.NamespaceAnalytics]

	assert.JSONEq(t, mustJSON(t, wantAfter), mustJSON(t, gotAfter),
		"losing the generation key must orphan the pre-loss entry, never resurrect it as stale data")
	assert.Equal(t, float64(0), readDelta.Hits, "the post-loss read must be a fresh miss, not a hit against the orphaned entry")
	assert.Equal(t, float64(1), readDelta.Misses)
}
