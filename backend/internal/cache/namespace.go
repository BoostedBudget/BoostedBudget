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
	// SchemaVersion 4 (#1626 Phase 3): GetAggregatedHistory — this namespace's only balance read,
	// reached through GetNetWorthTrend — changed what it RETURNS for the same arguments, in three
	// ways that all move money. (1) Its range bounds moved from instant to calendar-date
	// comparison, so a snapshot written on the end date at any hour is now inside the range instead
	// of outside it. (2) Each account's last known balance is now carried forward across days it
	// has no snapshot for, so a day on which only one Plaid item synced no longer collapses toward
	// zero. (3) An orphaned snapshot now recovers its account type from its chain row and, failing
	// that, is reported as an explicit untyped balance rather than vanishing from the type
	// breakdown. A deploy never touches the per-scope generation token, so without this bump a
	// pre-fix Redis entry would remain a valid hit under post-fix code and keep serving the
	// collapsed day the fix exists to repair. Same reasoning as versions 2 and 3 above.
	// SchemaVersion 5 (#1626 Phase 3, Defect 1): GetCashFlowHistory — cached in this namespace —
	// changed what it RETURNS for the same arguments whenever the requested end date reaches into
	// the future. It no longer fabricates a zero-valued bucket for a period that has not happened
	// (the user-visible symptom was "Week of Sep 13 / Sep 20 / Sep 27" rendered on Sep 9, flat at
	// exactly 0), and its period_end and end_date now stop at today rather than at the requested
	// end. An entry written before the fix decodes cleanly into the unchanged response struct —
	// which is exactly what makes the bump necessary rather than optional — and a deploy never
	// touches the per-scope generation token (it is minted once and only Bump()'d by explicit
	// invalidation), so without this bump a pre-fix entry would stay a valid hit and keep serving
	// the future buckets the fix exists to remove. The 120s TTL makes that window short, not
	// absent, and "short" is not a correctness argument. Same reasoning as versions 2, 3 and 4.
	// SchemaVersion 6 (#1626 Phase 2, review finding X1): GetAggregatedHistory — this namespace's
	// only balance read, reached through GetNetWorthTrend — again changed what it RETURNS for
	// identical arguments, and this time a WRONG NUMBER becomes a right one. Version 4's
	// carry-forward permitted the carry whenever an account had no recorded 'active' interval, and
	// two real shapes satisfy that condition forever: migration 000152 deliberately writes a
	// terminal-only history when removed_at is NULL or <= created_at, and an orphaned snapshot
	// (account_id IS NULL) has no history to find at all. So a closed account's last retained
	// balance was carried onto every later observed day — permanently overstating balance and net
	// worth. The carry is now scoped to an account that still exists and is active. A pre-fix entry
	// decodes cleanly into the unchanged response struct, which is exactly what makes the bump
	// necessary rather than optional, and a deploy never touches the per-scope generation token —
	// so without it a pre-fix entry would stay a valid hit and keep serving the overstated total.
	// The 120s TTL makes that window short, not absent. Same reasoning as versions 2 through 5.
	//
	// Version 6 also covers GetCashFlowSummary (review finding X7), likewise cached here: its
	// echoed end_date is now clamped to today for a future-ranged request, so that response too
	// differs for identical arguments.
	// SchemaVersion 7 (#1626 E2 Part 1): GetAggregatedHistoryForHousehold — the HOUSEHOLD balance
	// read, reached through GetNetWorthTrend with a scope, and this namespace's second balance read
	// — gained the per-account carry-forward its personal twin got at version 4. It therefore
	// returns different money for identical arguments: a day on which only one member's Plaid item
	// synced no longer reports only that member's accounts, so the household total no longer
	// collapses toward zero on it. A pre-fix entry decodes cleanly into the unchanged response
	// struct — which is exactly what makes the bump necessary rather than optional — and a deploy
	// never touches the per-scope generation token (it is minted once and only Bump()'d by explicit
	// invalidation), so without this bump a pre-fix entry would stay a valid hit under post-fix code
	// and keep serving the collapsed day the fix exists to repair. Same reasoning as versions 2
	// through 6.
	//
	// ⚠ #1626 E2 Part 2 is NOT what moved it, and the distinction is worth keeping: that change made
	// GetSpendingByCategory / GetSpendingTrends and their household twins tolerate a NULL
	// effective_category_name instead of failing the scan. A request that hit the NULL previously
	// ERRORED, so it wrote no cache entry at all — there is no pre-fix envelope for it to invalidate
	// — and a request that did not hit one returns byte-identical results. A bump for it would have
	// been cargo-culted, not reasoned.
	// SchemaVersion 8 (#1626 final whole-branch review, finding F2): GetCashFlowSummary and
	// GetCashFlowHistory — both cached in this namespace — again return different bytes for
	// identical arguments. GetCashFlowSummary echoes the requested end date again (version 6
	// clamped it, which made the endpoint label a window its own aggregate did not measure), and
	// GetCashFlowHistory's response gained requested_end_date. A pre-fix entry decodes cleanly into
	// the unchanged response struct, which is exactly what makes the bump necessary rather than
	// optional, and a deploy never touches the per-scope generation token — so without it a
	// pre-fix entry would stay a valid hit and keep serving the stale echoed date. The 120s TTL
	// makes that window short, not absent. Same reasoning as versions 2 through 7.
	//
	// ⚠ The same review's findings F1 and F5 do NOT justify this bump, despite an earlier version
	// of this comment claiming they did — the eleventh false comment retracted on this branch. F1
	// fixes DashboardService.GetNetWorthHistory (the household-orphan UntypedBalance sign bug), and
	// F5 changes GetAggregatedHistory / GetAggregatedHistoryForHousehold's ByType/UntypedBalance
	// output and account-type filtering. But the only method from that call chain actually cached
	// in this namespace is GetNetWorthTrend (cachedAnalyticsService.GetNetWorthTrend), which passes
	// a nil account-type filter and builds its cached response from each BalanceHistoryPoint's
	// TotalBalance alone — never ByType or UntypedBalance. So neither F1 nor F5 changes the bytes
	// GetNetWorthTrend returns for identical arguments, and the bump does not need them: F2 alone
	// is sufficient and is the only reason recorded here.
	//
	// ⚠ NOT BUMPED to 9 for the final gate's finding F6, and that is a decision rather than an
	// oversight. F6 made GetNetWorthTrend's bounds `time.Now().UTC()` instead of `time.Now()`
	// (analytics_service.go), which is the SAME INSTANT with a different Location — so the response
	// bytes change only where that Location is read, i.e. only on a host whose local calendar day
	// differs from UTC's. Every deployed environment runs UTC, where the pre-fix and post-fix bytes
	// are identical for identical arguments, and a bump's cost (a cold cache for every user on
	// every method in this namespace) buys nothing. On a non-UTC DEVELOPER machine a pre-fix entry
	// could serve the old local-day bound for up to the 120s TTL; that is not an environment this
	// cache's correctness is claimed for.
	NamespaceAnalytics: {
		TTL: 120 * time.Second, SchemaVersion: 8, ScopeKind: ScopeUser, DefaultEnabled: true,
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
