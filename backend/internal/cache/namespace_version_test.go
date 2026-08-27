package cache

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/redisclient"
)

// TestSchemaVersionBump_OrphansEntriesWrittenUnderThePriorVersion pins the property #1531/#1569's
// cache design relies on for every semantic query change: an entry written under a namespace's OLD
// SchemaVersion must never be served as a hit once that namespace's SchemaVersion has moved on. If
// it were served, a query whose meaning changed (e.g. round 10's balance/net-worth grain fix, or
// the household-grants/tombstone-filter rewrite in analytics_repository.go) would keep answering
// with pre-fix data for as long as Redis keeps the old entry alive — which, for a namespace whose
// generation token was never bumped by the fix itself, is until the entry's own TTL expires.
//
// This tests the KEY-CONSTRUCTION property directly, not Redis I/O behind GetOrLoad: an entry
// keyed at schemaVersion N-1 renders a different Redis key than the one GetOrLoad computes once
// the registry's SchemaVersion is N, so the old entry is not merely decoded-and-rejected — it is
// never looked up at all, and GetOrLoad must fall through to the loader.
//
// It is parameterized over every namespace this branch's history/analytics changes actually
// reached: NamespaceAnalytics (GetNetWorthTrend's grain fix) and NamespaceAnalyticsRows (the
// deleted_at tombstone filters + household-grants rewrite added to analytics_repository.go, whose
// GetTopExpense/GetLargestTransactionsForPeriod/GetCategoryBreakdownForMerchant/GetUnusualSpend
// results are cached under this namespace). Both namespaces must carry a SchemaVersion of at least
// 2 as of this branch — see the doc comments at their namespaceRegistry entries for why.
func TestSchemaVersionBump_OrphansEntriesWrittenUnderThePriorVersion(t *testing.T) {
	for _, ns := range []Namespace{NamespaceAnalytics, NamespaceAnalyticsRows} {
		t.Run(string(ns), func(t *testing.T) {
			cfg, ok := lookupNamespace(ns)
			require.True(t, ok)
			require.Greater(t, cfg.SchemaVersion, 1,
				"namespace %s must have bumped SchemaVersion past 1 for this branch's query "+
					"semantics changes to actually take effect through Redis", ns)

			fake := redisclient.NewFake("cache")
			c := NewCache(Options{Client: fake})
			ctx := context.Background()

			k := Key{Namespace: ns, ScopeKind: ScopeUser, ScopeID: "u1", Method: "M", ArgHash: "h1"}
			gen, err := c.Generations().Get(ctx, k.ScopeKind, k.ScopeID)
			require.NoError(t, err)

			// Simulate a pre-bump entry: an envelope encoded and keyed under the PRIOR schema
			// version, still sitting in Redis (its own TTL hasn't expired) when the bump deploys.
			priorVersion := cfg.SchemaVersion - 1
			staleEnvelope, err := encodeEnvelope(priorVersion, []byte(`{"stale":true}`), time.Now())
			require.NoError(t, err)
			priorKey := k.DataKey(priorVersion, gen)
			require.NoError(t, fake.Set(ctx, priorKey, string(staleEnvelope), cfg.TTL))

			// GetOrLoad, running under the CURRENT (bumped) registry, must not find that entry: it
			// renders a different data key, so this must be a miss that calls the loader — not a
			// hit that decodes and rejects the stale envelope on schema mismatch.
			var loaderCalls int
			got, hit, err := c.GetOrLoad(ctx, k, func(context.Context) ([]byte, error) {
				loaderCalls++
				return []byte(`{"stale":false}`), nil
			})
			require.NoError(t, err)
			assert.False(t, hit, "an entry written under the prior schema version must not be served as a hit")
			assert.Equal(t, 1, loaderCalls)
			assert.JSONEq(t, `{"stale":false}`, string(got))

			// The stale entry must still be sitting untouched at its own (now-unreachable) key —
			// this is an orphan, not a delete-and-replace.
			raw, err := fake.Get(ctx, priorKey)
			require.NoError(t, err)
			assert.Contains(t, raw, `"stale":true`)
		})
	}
}
