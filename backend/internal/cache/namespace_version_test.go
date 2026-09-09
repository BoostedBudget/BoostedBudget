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
// It is parameterized over every namespace whose query semantics a shipped change has altered, and
// each carries its OWN required floor.
//
// ⚠ This used to assert `SchemaVersion > 1` for both, which could not fail in the way it claimed
// to (#1626 Phase 2). NamespaceAnalytics was at 3 by then, so reverting it to 2 — undoing the bump
// that #1626's grouping-key and hierarchy changes require to take effect through Redis — left this
// test green, and the doc comment above it still said "at least 2". A test that passes while the
// thing it names is broken is worse than no test: it is a green light over a hole.
//
// The floors are a TABLE, so each namespace's requirement is stated once and independently:
//
//   - NamespaceAnalytics 8 — #1531/#1569 took it to 2 (GetNetWorthTrend's grain fix); #1626 Phase 2
//     took it to 3, because the analytics grouping keys and display resolution changed shape and a
//     pre-#1626 envelope decodes cleanly into the new struct while carrying UUIDs where names
//     belong. Decoding cleanly is exactly what makes the bump necessary rather than optional.
//     #1626 Phase 3 took it to 4: GetAggregatedHistory, which GetNetWorthTrend is cached over,
//     changed its range bounds from instant to calendar-date comparison, gained a per-account
//     carry-forward, and stopped discarding an orphaned snapshot's balance from the type
//     breakdown — so the same arguments return different money, and a surviving pre-fix entry
//     would keep serving the collapsed day the fix exists to repair. #1626 Phase 3 Defect 1 took
//     it to 5: GetCashFlowHistory, also cached here, stopped fabricating zero-valued buckets for
//     periods that have not happened yet and now clamps its period_end and end_date to today, so
//     a pre-fix entry — which decodes cleanly, the response struct being unchanged — would keep
//     serving chart points dated in the future. #1626 Phase 2's review finding X1 took it to 6:
//     GetAggregatedHistory's carry-forward was permitted by "this account has no recorded 'active'
//     interval", which is TRUE FOREVER for migration 000152's deliberate terminal-only backfill
//     shape and vacuously true for an orphaned snapshot — so a closed account's retained balance
//     was carried onto every later observed day, and a pre-fix entry (which again decodes cleanly)
//     would keep serving that overstated total. The same bump covers GetCashFlowSummary's echoed
//     end_date, now clamped to today for a future-ranged request. #1626 E2 Part 1 took it to 7:
//     GetAggregatedHistoryForHousehold, the OTHER balance read cached here (GetNetWorthTrend with a
//     household scope), gained the same per-account carry-forward its personal twin got at version
//     4, so a household day on which only one member's item synced stops collapsing toward zero —
//     and a pre-fix entry, decoding cleanly as ever, would keep serving that collapsed day. E2's
//     Part 2 (four analytics reads now tolerating a NULL effective_category_name) deliberately did
//     NOT contribute: the requests it fixes used to ERROR, so they cached nothing, and the requests
//     that succeeded return byte-identical results. The final whole-branch review took it to 8:
//     finding F1 is another WRONG NUMBER in GetAggregatedHistoryForHousehold — its `observed`
//     projection read `a.type` alone where the personal statement recovers an orphan's type from
//     its chain row, so an orphaned CREDIT CARD reached UntypedBalance, which GetNetWorthHistory
//     adds to totalAssets by its positive sign, overstating household net worth by twice the debt.
//     A pre-fix entry decodes cleanly, as ever, and would keep serving that overstated figure. The
//     same bump covers finding F5 — the personal read's accountTypes filter now tests the recovered
//     type, so a filtered request returns an orphan it used to omit — and finding F2, which puts
//     GetCashFlowSummary's echoed end_date back on the requested range (version 6's clamp made the
//     endpoint label a window its own aggregate did not measure) and adds requested_end_date to
//     GetCashFlowHistory's response. Three changes in one undeployed branch, one bump, exactly as
//     version 6 covered two.
//   - NamespaceAnalyticsRows 2 — the deleted_at tombstone filters and household-grants rewrite in
//     analytics_repository.go, whose GetTopExpense / GetLargestTransactionsForPeriod /
//     GetCategoryBreakdownForMerchant / GetUnusualSpend results are cached here.
//
// Raising a floor here is the deliberate act; lowering one should require deleting a row and
// saying why in the same commit.
func TestSchemaVersionBump_OrphansEntriesWrittenUnderThePriorVersion(t *testing.T) {
	requiredFloor := map[Namespace]int{
		NamespaceAnalytics:     8,
		NamespaceAnalyticsRows: 2,
	}

	for _, ns := range []Namespace{NamespaceAnalytics, NamespaceAnalyticsRows} {
		t.Run(string(ns), func(t *testing.T) {
			cfg, ok := lookupNamespace(ns)
			require.True(t, ok)
			floor, hasFloor := requiredFloor[ns]
			require.True(t, hasFloor, "namespace %s has no recorded SchemaVersion floor", ns)
			require.GreaterOrEqual(t, cfg.SchemaVersion, floor,
				"namespace %s must be at SchemaVersion >= %d for the shipped query-semantics "+
					"changes to take effect through Redis; see the floor table above for which "+
					"change put it there", ns, floor)

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
