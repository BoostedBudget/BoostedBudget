// Package cache implements the caching primitives for issue #1531 Task 9: key building,
// generation tokens, kill switches, an envelope codec, and a Cache interface with a single read
// entry point (GetOrLoad). It holds personal financial data, so every design decision here traces
// to one rule: the dangerous failure is not a cache miss, it is silently serving stale money.
//
// This package builds PRIMITIVES ONLY — no consumers, no invalidation hooks, no main.go wiring.
// Task 10 wires real callers and invalidation; Task 11 writes the parity harness that proves
// cached and uncached financial reads are identical.
package cache

import "time"

// ScopeKind identifies which kind of entity a Key (and its generation) is scoped to.
type ScopeKind string

const (
	ScopeUser      ScopeKind = "user"
	ScopeHousehold ScopeKind = "household"
	ScopeClerkID   ScopeKind = "clerk_id"
)

// Namespace is a `const`-literal cache namespace. Every namespace must be registered in
// namespaceRegistry with a non-zero TTL — see TestEveryNamespaceHasNonZeroTTL.
type Namespace string

const (
	// NamespaceAnalytics holds pre-computed spending/cash-flow analytics summaries.
	NamespaceAnalytics Namespace = "analytics"
	// NamespaceAnalyticsRows holds analytics ROW-level results that carry merchant names and
	// descriptions — GDPR requires a short cap on how long that free text stays cached.
	NamespaceAnalyticsRows Namespace = "analytics_rows"
	// NamespaceDashPrefs holds dashboard layout/preferences.
	NamespaceDashPrefs Namespace = "dashprefs"
	// NamespaceIdentity holds Clerk-id-scoped identity lookups. Task 10 enforces that this
	// namespace only ever caches a scalar UUID, never a structured payload.
	NamespaceIdentity Namespace = "identity"
	// NamespaceHHScope holds household-scope authorization-adjacent data. Disabled by default —
	// an operator must opt in explicitly before this namespace serves cached reads.
	NamespaceHHScope Namespace = "hhscope"
	// NamespaceEntitlement is reserved for subscription/entitlement checks. It is hard-disabled:
	// see namespaceConfig.HardDisabled — no env var, control key, or setter can enable it.
	NamespaceEntitlement Namespace = "entitlement"
)

// namespaceConfig is the per-namespace registry entry: TTL, schema version, scope kind, and
// enablement.
type namespaceConfig struct {
	// TTL is the data key's Redis expiration. Every registered namespace must set a non-zero TTL,
	// even NamespaceEntitlement (which can never be enabled) — see TestEveryNamespaceHasNonZeroTTL:
	// a zero TTL is always a config bug, never a deliberate "this namespace has no cap" statement.
	TTL time.Duration
	// SchemaVersion is embedded in both the Redis key (v<N>) and the envelope body (defence in
	// depth — see codec.go). Bump it when a namespace's cached payload shape changes so old
	// entries are structurally unreachable rather than misread.
	SchemaVersion int
	// ScopeKind says which kind of entity this namespace's keys are scoped to.
	ScopeKind ScopeKind
	// DefaultEnabled is the compiled-in fallback used only when neither a Redis control key nor an
	// env var override has ever been observed for this namespace.
	DefaultEnabled bool
	// HardDisabled namespaces can NEVER be enabled by any means — not an env var, not a Redis
	// control key, not a setter. Checked first, before any other resolution path.
	HardDisabled bool
}

// namespaceRegistry is the single source of truth for every namespace's TTL, schema version,
// scope kind, and default enablement.
var namespaceRegistry = map[Namespace]namespaceConfig{
	// SchemaVersion 2 (#1569, round 11): two query-semantics changes landed in this namespace's
	// underlying repositories without any generation bump (a deploy never touches the per-scope
	// generation token — it is minted once and only Bump()'d by explicit invalidation), so a
	// pre-fix Redis entry surviving a deploy would otherwise stay a valid hit under post-fix code
	// until its own TTL expired:
	//  1. balance_snapshot_repository.go's GetAggregatedHistory / GetAggregatedHistoryForHousehold
	//     moved from chain-collapsed to per-physical-account grain (fixing a balance/net-worth
	//     halving bug). GetNetWorthTrend is this namespace's only consumer of those two methods.
	//  2. analytics_repository.go's household-scoped queries (getCashFlowAggregationHousehold,
	//     getCashFlowGroupedHousehold, getSpendingByCategoryHousehold, getSpendingByMerchantHousehold,
	//     getSpendingTrendsHousehold, getAverageMonthlySpendHousehold,
	//     getSavingsRateForPeriodHousehold, getMonthOverMonthSpendingHousehold,
	//     getIncomeSourceBreakdownHousehold) moved from householdAccountsCTE to
	//     householdGrantsCTE, fixing a household spending/net-worth disagreement (#1569 T23) —
	//     all of those methods are cached in this namespace via GetCashFlowSummary/History,
	//     GetSpendingByCategory/Merchant/Trends, GetAverageMonthlySpend, GetSavingsRateForPeriod,
	//     GetMonthOverMonthSpendingChange, and GetIncomeSourceBreakdown.
	// See TestSchemaVersionBump_OrphansEntriesWrittenUnderThePriorVersion.
	// SchemaVersion 3 (#1626): the PAYLOAD of two already-cached methods changed.
	// GetSpendingByCategory and GetIncomeSourceBreakdown now resolve their display names in the
	// service — a custom category renders as its own name (never a title-cased UUID) and a PFC
	// category as its registry label rather than the raw enum. A deploy never touches the
	// per-scope generation token (it is minted once and only Bump()'d by explicit invalidation),
	// so a pre-fix Redis entry would remain a valid hit under post-fix code and keep serving raw
	// enums and UUIDs until its own TTL expired. The 120s TTL makes that window short, not absent,
	// and "short" is not a correctness argument. Same reasoning as version 2 above.
	NamespaceAnalytics: {
		TTL: 120 * time.Second, SchemaVersion: 3, ScopeKind: ScopeUser, DefaultEnabled: true,
	},
	// SchemaVersion 2 (#1569, round 11): analytics_repository.go added `t.deleted_at IS NULL` to
	// the queries backing GetTopExpense, GetLargestTransactionsForPeriod,
	// GetCategoryBreakdownForMerchant, and GetUnusualSpend — all cached in this namespace, all
	// user-scoped only (none of these four has a household variant). That changes what those
	// queries return for the same arguments (a soft-deleted transaction no longer counts), and —
	// same as NamespaceAnalytics above — this namespace's generation token is not bumped by a
	// deploy either, so the SchemaVersion bump is what actually makes the fix take effect for any
	// entry a deploy finds already sitting in Redis.
	NamespaceAnalyticsRows: {
		TTL: 300 * time.Second, SchemaVersion: 2, ScopeKind: ScopeUser, DefaultEnabled: true,
	},
	NamespaceDashPrefs: {
		TTL: 300 * time.Second, SchemaVersion: 1, ScopeKind: ScopeUser, DefaultEnabled: true,
	},
	NamespaceIdentity: {
		TTL: 600 * time.Second, SchemaVersion: 1, ScopeKind: ScopeClerkID, DefaultEnabled: true,
	},
	NamespaceHHScope: {
		TTL: 30 * time.Second, SchemaVersion: 1, ScopeKind: ScopeUser, DefaultEnabled: false,
	},
	NamespaceEntitlement: {
		// TTL is set even though this namespace can never be enabled — see the field doc comment.
		TTL: 60 * time.Second, SchemaVersion: 1, ScopeKind: ScopeUser, DefaultEnabled: false,
		HardDisabled: true,
	},
}

// namespaces returns every registered Namespace, for iteration (control.go's poller, registry
// tests).
func namespaces() []Namespace {
	out := make([]Namespace, 0, len(namespaceRegistry))
	for ns := range namespaceRegistry {
		out = append(out, ns)
	}
	return out
}

// AllNamespaces returns every registered Namespace. Task 10a's main.go wiring uses this to build
// the single Control that polls every namespace's kill-switch key, without hand-maintaining a
// second list that could silently drift from namespaceRegistry as namespaces are added.
func AllNamespaces() []Namespace {
	return namespaces()
}

// lookupNamespace returns ns's registry entry and whether it is registered at all.
func lookupNamespace(ns Namespace) (namespaceConfig, bool) {
	cfg, ok := namespaceRegistry[ns]
	return cfg, ok
}
