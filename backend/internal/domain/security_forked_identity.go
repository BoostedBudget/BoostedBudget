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
// institution_security_id (migration 000195) is "the identifier given to the security by the
// institution" — independent of Plaid's own dedup key, so it survives a plaid_security_id fork
// and gives detection something stable to join on. CUSIP/ISIN cannot do this job: Plaid nulls
// both for every customer without a separately-licensed CUSIP feed (see domain.Security's own
// comment), and every security in this system's production data has both null.
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
// corporate-action-triggered reissue of Plaid's dedup key produces.
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
	GroupCount int64 `json:"group_count"`
	Available  bool  `json:"available"`
}
