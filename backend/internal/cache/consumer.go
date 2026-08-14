package cache

import (
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// Scope is the (ScopeKind, ScopeID) pair a Key is rendered against. It exists as its own type so
// ScopeFor can return a single value from a single call-site expression — see the package doc on
// NewKey.
type Scope struct {
	Kind ScopeKind
	ID   string
}

// ScopeFor resolves the scope a cached read should be keyed under: household scope when hh is
// non-nil (so a household member's write can invalidate every other member's cached household
// aggregate in Task 10b), personal (user) scope otherwise. A nil *HouseholdScope means "personal" —
// today's single-user behavior — everywhere else in this codebase, and the cache key must agree.
func ScopeFor(userID string, hh *domain.HouseholdScope) Scope {
	if hh != nil {
		return Scope{Kind: ScopeHousehold, ID: hh.HouseholdID}
	}
	return Scope{Kind: ScopeUser, ID: userID}
}

// NewKey builds a Key for the given namespace, scope, and method. method MUST be a `const` string
// literal at the call site (see Key's doc comment) — never derived via reflection or
// interpolation. Call .Enc(...) on the result to fill in ArgHash before use.
func NewKey(ns Namespace, scope Scope, method string) Key {
	return Key{Namespace: ns, ScopeKind: scope.Kind, ScopeID: scope.ID, Method: method}
}

// Enc renders k's ArgHash by running enc against a fresh ArgEncoder, returning the updated Key.
// Consumers should write every encoder call inline in the closure, in the same order every time —
// see EncodeSpendingFilters/EncodeCashFlowFilters/EncodeHouseholdScope for the pattern this
// package holds itself to.
func (k Key) Enc(enc func(e *ArgEncoder)) Key {
	e := NewArgEncoder()
	enc(e)
	k.ArgHash = e.Hash()
	return k
}

// EncodeHouseholdScope writes hh's fields into e: a presence bool first (nil means personal scope
// — a completely different query from a populated household scope, so this must never collide
// with the non-nil encoding), then HouseholdID and MemberUserIDs when non-nil.
//
// MemberUserIDs is deliberately part of the hash even though HouseholdScope's own doc comment
// says it is pre-validated/resolved server-side: encoding it here means a household membership
// change changes the hash of every household-scoped cache key immediately, even before Task 10b's
// generation-bump invalidation lands — a free second safety net against a stale membership list
// being served to a household that just changed.
func EncodeHouseholdScope(e *ArgEncoder, hh *domain.HouseholdScope) {
	e.Bool(hh != nil)
	if hh == nil {
		return
	}
	e.String(hh.HouseholdID)
	e.StringSetSorted(hh.MemberUserIDs)
}

// EncodeSpendingFilters writes every field of f into e, in struct-declaration order.
//
// AccountTypes / CustomCategoryIDs use StringSetSorted (order/duplicate-insensitive — see the
// `= ANY($n)` SQL in internal/repository/analytics_repository.go, e.g. buildSpendingFilter around
// lines 123-131, 615-620, and 630-635). A nil slice and an empty (non-nil) slice hash differently
// under StringSetSorted ('N' vs 'L'+0), but the repository branches on `len(x) > 0` for both
// fields — nil and empty take the identical SQL branch. So nil vs. [] can produce two distinct
// cache entries for what is semantically the same query: never an incorrectness (each entry is
// independently correct), just a missed cache-reuse opportunity. Not worth special-casing here:
// ArgEncoder's nil/empty distinction is shared machinery used by other fields where the
// distinction DOES matter (see HouseholdScope above), and narrowing it here would be a one-off
// exception future maintainers would have to relearn.
//
// This function is what TestSpendingFiltersEncoder_CoversEveryStructField and
// TestEveryEncodedField_MutationChangesHash hold to account — see cache_consumer_test.go.
func EncodeSpendingFilters(e *ArgEncoder, f domain.SpendingFilters) {
	e.Time(f.StartDate)
	e.Time(f.EndDate)
	e.StringSetSorted(f.AccountTypes)
	e.Bool(f.IncludeTransfers)
	e.String(f.Category)
	e.StringSetSorted(f.CustomCategoryIDs)
	e.Bool(f.RollUpChildren)
	EncodeHouseholdScope(e, f.HouseholdScope)
}

// EncodeCashFlowFilters writes every field of f into e, in struct-declaration order. Same
// AccountTypes nil-vs-empty note as EncodeSpendingFilters applies here (see
// analytics_repository.go lines 175-180, 240-245, 326-331 — all `len(filters.AccountTypes) > 0`).
func EncodeCashFlowFilters(e *ArgEncoder, f domain.CashFlowFilters) {
	e.Time(f.StartDate)
	e.Time(f.EndDate)
	e.StringSetSorted(f.AccountTypes)
	e.Bool(f.IncludeTransfers)
	e.String(f.Grouping)
	EncodeHouseholdScope(e, f.HouseholdScope)
}
