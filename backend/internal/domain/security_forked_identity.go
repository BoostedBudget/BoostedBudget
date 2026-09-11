package domain

// Forked security identity detection (#1631 M8b).
//
// Plaid documents plaid_security_id (our unique dedup key, see domain.Security.PlaidSecurityID)
// as "typically" — not guaranteed — stable across a security's life, and states it can be
// reissued by a corporate action (a ticker change, a CUSIP change). When that happens, one
// real-world instrument silently forks into two `securities` rows, each with its own
// PlaidSecurityID and its own purchase/sale/dividend history hanging off
// investment_transactions.security_id — the exact shape observed in production for
// AGG/ISHARES.CORE.U.S.AGGREGATE and IEFA/IEMG/VCIT.
//
// institution_security_id (migration 000200) is "the identifier given to the security by the
// institution" — independent of Plaid's own dedup key, so it survives a plaid_security_id fork
// and gives detection something stable to join on. CUSIP/ISIN cannot do this job: Plaid nulls
// both for every customer without a separately-licensed CUSIP feed (see domain.Security's own
// comment), and every security in this system's production data has both null.
//
// ⚠ CANDIDATES, NOT ESTABLISHED FORKS (#1631 CX7). institution_security_id is documented as "the
// identifier given to the security by the institution" -- it is INSTITUTION-SCOPED, and nothing
// guarantees it is unique across institutions. The grouping behind these counts has no institution
// dimension at all: `securities` is a global table shared by every user and every provider and
// carries no institution column, so two institutions that reuse the same internal id group
// together and are, from here, indistinguishable from one instrument whose Plaid id was reissued.
//
// The dimension was considered and NOT added. It is derivable only by joining out through
// holdings/investment_transactions to accounts to linked_items.institution_id_blind_idx, which
// would silently empty the metric wherever that column is unpopulated -- a confident zero, which
// is a worse failure than a false positive on a review list. So the claim is narrowed instead:
// every name, doc comment and dashboard label says CANDIDATE, the /admin/health card renders
// neutral rather than as a warning, and an operator settles a group by looking at its members.
// Do not re-word any of this back into an assertion of a fork.
//
// This is DETECTION ONLY. Nothing here merges identities, rewrites
// investment_transactions.security_id, or touches holding_snapshots' (account_id, security_id,
// as_of_date) key. Actual merging needs its own ADR pass under the financial-history retention
// rules and does not ship here.

// ForkedSecurityIdentityMember is one `securities` row participating in a detected fork.
type ForkedSecurityIdentityMember struct {
	SecurityID      string  `json:"security_id"`
	PlaidSecurityID string  `json:"plaid_security_id"`
	Name            string  `json:"name"`
	TickerSymbol    *string `json:"ticker_symbol,omitempty"`
}

// ForkedSecurityIdentity is one detected group: two or more `securities` rows sharing a
// non-blank institution_security_id under DIFFERENT plaid_security_id values — the shape a
// corporate-action-triggered reissue of Plaid's dedup key produces, and equally the shape two
// institutions reusing one internal id produce. See the CANDIDATES note at the top of this file
// before describing a group as a confirmed fork anywhere.
type ForkedSecurityIdentity struct {
	InstitutionSecurityID string                         `json:"institution_security_id"`
	Members               []ForkedSecurityIdentityMember `json:"members"`
}

// ForkedSecurityIdentityMetrics is the admin-visible drift count on /admin/health.
//
// Available follows the same "unknown is not zero" convention as every other optional gauge on
// domain.SystemHealthResponse (PlaidOrphanedItemMetrics, DeletionResidueMetrics, ...): an unwired
// repository or a failed query reports Available=false, never a confident zero. GroupCount is
// GLOBAL, not per-user — securities is explicitly not user-scoped (see
// repository.SecurityRepository's interface comment), and a fork in one user's brokerage data is
// the same underlying Plaid identity-reissue affecting every user who holds that security.
type ForkedSecurityIdentityMetrics struct {
	// GroupCount counts CANDIDATE groups, not confirmed forks -- see the CANDIDATES note at the
	// top of this file. The JSON name is unchanged because the /admin/health card is the only
	// consumer and the narrowing is carried by the served docs and the dashboard copy rather than
	// by a rename that would break the generated client for no gain.
	GroupCount int64 `json:"group_count"`
	// Groups is the full member listing behind GroupCount (#1631 M8b I2) — without it, an operator
	// seeing a non-zero GroupCount has no way to find out WHICH securities forked. Fork groups are
	// expected to be rare (a handful at most; they only form from a corporate-action reissue of
	// plaid_security_id), so shipping the full detail alongside the count is cheap.
	Groups    []ForkedSecurityIdentity `json:"groups"`
	Available bool                     `json:"available"`
}
