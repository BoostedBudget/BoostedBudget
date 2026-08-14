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
	NamespaceAnalytics: {
		TTL: 120 * time.Second, SchemaVersion: 1, ScopeKind: ScopeUser, DefaultEnabled: true,
	},
	NamespaceAnalyticsRows: {
		TTL: 300 * time.Second, SchemaVersion: 1, ScopeKind: ScopeUser, DefaultEnabled: true,
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

// lookupNamespace returns ns's registry entry and whether it is registered at all.
func lookupNamespace(ns Namespace) (namespaceConfig, bool) {
	cfg, ok := namespaceRegistry[ns]
	return cfg, ok
}
