//go:build integration

// Issue #1531 Slice B / M6 — the GDPR purge SCAN path had zero real-Redis coverage. Every existing
// PurgeUser test (purge_test.go) runs against redisclient.Fake, whose Scan (fake.go) implements the
// glob via path.Match with a comment ASSERTING equivalence to Redis SCAN's own glob semantics —
// never proven against a real server. And no test forces the real cursor-pagination loop
// (redisclient/client.go's Scan: a for { SCAN cursor ... } loop) to actually iterate more than once
// — with a small keyspace, a single SCAN call with a generous COUNT hint can return everything in
// one pass, silently exercising only the cursor==0-on-first-call branch. This file seeds enough
// keys under one purge scope to force multiple real cursor iterations against a disposable
// redis:7-alpine container this test fully owns (cachetest.StartRedisContainer), and asserts every
// targeted key is gone while a control scope's keys survive untouched.
package cache_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/cache"
	"github.com/maxnussbaum/BoostedBudget/internal/cache/cachetest"
	"github.com/maxnussbaum/BoostedBudget/internal/redisclient"
)

// purgeScanKeyCount is chosen well above redisclient's scanBatchSize (500, the COUNT hint passed
// to each SCAN call) so a single SCAN round-trip cannot plausibly return every key: forcing the
// real client.Scan loop to actually advance its cursor more than once, not merely take the
// cursor==0-on-first-call shortcut a smaller keyspace would allow.
const purgeScanKeyCount = 2000

// TestPurgeUser_RealRedis_ScanCursorPaginationDeletesEveryTargetedKey seeds purgeScanKeyCount data
// keys under the target user's scope (forcing the production SCAN cursor loop in
// internal/redisclient's client.Scan to iterate multiple times against a real server, not the
// glob-via-path.Match approximation redisclient.Fake uses) plus a handful of control keys under an
// unrelated scope, runs the real PurgeUser, and asserts:
//   - every targeted key is gone except the tombstone (the one deliberate survivor)
//   - every control (non-targeted) key survives untouched
func TestPurgeUser_RealRedis_ScanCursorPaginationDeletesEveryTargetedKey(t *testing.T) {
	container := cachetest.StartRedisContainer(t)
	ctx := context.Background()

	client, err := redisclient.New(redisclient.Options{URL: container.URL, Name: "cache-test-purge-scan"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })

	c := cache.NewCache(cache.Options{Client: client})
	purger, ok := c.(cache.Purger)
	require.True(t, ok, "a real redis-backed Cache must implement Purger")

	const userID = "purge-scan-real-redis-user"
	const clerkUserID = "" // exercise the single-scope path; the multi-scope loop is already
	// covered against the fake in purge_test.go — this file's whole point is the SCAN
	// cursor-pagination behaviour, not scope-set logic.
	userTag := cache.ScopeTag(cache.ScopeUser, userID)

	// Seed purgeScanKeyCount data keys under the target scope, via the real client directly
	// (bypassing GetOrLoad — this test only cares about SCAN+DEL over raw keys matching the
	// purge glob, not the cache-fill path).
	for i := 0; i < purgeScanKeyCount; i++ {
		key := fmt.Sprintf("%s:{%s}:target:%d", "bb:c", userTag, i)
		require.NoError(t, client.Set(ctx, key, "v", 0))
	}

	// Control keys under an UNRELATED scope: these must survive PurgeUser untouched. If the real
	// SCAN cursor loop ever mis-paginated (e.g. an off-by-one on the cursor, or stopping early)
	// in a way that scanned too broadly, this is what would catch it.
	const controlKeyCount = 20
	controlTag := cache.ScopeTag(cache.ScopeUser, "purge-scan-real-redis-CONTROL-user")
	var controlKeys []string
	for i := 0; i < controlKeyCount; i++ {
		key := fmt.Sprintf("%s:{%s}:target:%d", "bb:c", controlTag, i)
		controlKeys = append(controlKeys, key)
		require.NoError(t, client.Set(ctx, key, "v", 0))
	}

	require.NoError(t, purger.PurgeUser(ctx, userID, clerkUserID))

	// Every key under the purged scope must be gone EXCEPT the tombstone — proving the real
	// cursor-pagination SCAN found (and Del subsequently removed) every one of purgeScanKeyCount
	// keys, not just whatever a single COUNT-500 SCAN round-trip happened to return.
	survivingUserKeys, err := client.Scan(ctx, "bb:c:{"+userTag+"}:*")
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{cache.TombstoneKey(userTag)}, survivingUserKeys,
		"only the tombstone may survive under the purged scope — the real SCAN cursor loop must "+
			"have found every one of the %d seeded target keys", purgeScanKeyCount)

	// Control scope must be untouched.
	survivingControlKeys, err := client.Scan(ctx, "bb:c:{"+controlTag+"}:*")
	require.NoError(t, err)
	assert.ElementsMatch(t, controlKeys, survivingControlKeys,
		"an unrelated scope's keys must survive PurgeUser untouched")
}

// TestScan_RealRedis_CursorPaginationActuallyIteratesMultiplePages is Issue #1531 GA-7's
// regression test. The test above assumes purgeScanKeyCount (2000) forces more than one SCAN
// cursor page, but COUNT is only a hint — nothing in it actually asserted a page count or a
// nonzero intermediate cursor, so a real server that happened to satisfy the whole request in one
// round trip would still pass it. This test drives the SAME cursor-pagination loop
// redisclient.Client.Scan implements (SCAN cursor MATCH pattern COUNT scanBatchSize, looping until
// cursor==0) directly against the container's go-redis client, over the identical
// purgeScanKeyCount-keys-under-one-scope dataset and COUNT hint, and asserts it took more than one
// page — i.e., the cursor was nonzero at least once before finishing. This proves the
// cursor-pagination claim itself, independent of the deletion assertions above.
func TestScan_RealRedis_CursorPaginationActuallyIteratesMultiplePages(t *testing.T) {
	container := cachetest.StartRedisContainer(t)
	ctx := context.Background()

	const scanBatchSize = 500 // mirrors internal/redisclient/client.go's scanBatchSize COUNT hint
	const userID = "purge-scan-cursor-pages-user"
	userTag := cache.ScopeTag(cache.ScopeUser, userID)
	pattern := "bb:c:{" + userTag + "}:*"

	for i := 0; i < purgeScanKeyCount; i++ {
		key := fmt.Sprintf("%s:{%s}:target:%d", "bb:c", userTag, i)
		require.NoError(t, container.Client.Set(ctx, key, "v", 0).Err())
	}

	var (
		cursor     uint64
		pages      int
		sawNonzero bool
		collected  []string
	)
	for {
		batch, next, err := container.Client.Scan(ctx, cursor, pattern, scanBatchSize).Result()
		require.NoError(t, err)
		collected = append(collected, batch...)
		pages++
		cursor = next
		if cursor != 0 {
			sawNonzero = true
		}
		if cursor == 0 {
			break
		}
	}

	assert.Greater(t, pages, 1,
		"SCAN over %d keys with COUNT=%d must take more than one round trip — otherwise the "+
			"cursor-pagination loop in redisclient.Client.Scan is exercised only on its "+
			"cursor==0-on-first-call shortcut, never its actual multi-page path", purgeScanKeyCount, scanBatchSize)
	assert.True(t, sawNonzero, "the cursor must be observed nonzero at least once before the scan completes")
	assert.Len(t, collected, purgeScanKeyCount, "the multi-page scan must still collect every seeded key")
}
