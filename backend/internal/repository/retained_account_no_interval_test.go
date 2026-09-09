package repository

// Issue #1626 Phase 2 — an account with NO recorded 'active' interval must not hide its
// transactions.
//
// WHY THIS EXISTS. The unified feed shows a transaction when its chain has an active sibling
// (activeChainSiblingLeg) or when the account itself has an 'active' history row whose interval
// covers the date (retainedAccountNoLiveSuccessorLeg). An account with NO 'active' row at all
// satisfies NEITHER — migration 000152's zero-length-interval guard skips the 'active' prefix when
// replaced_at/removed_at/updated_at is NULL or <= created_at, so such an account has only a
// terminal row. transaction_repository.go's own comment records the consequence: "its transactions
// stay hidden ... That is a backfill-data property, not a decision taken here."
//
// It became a decision the moment the Sankey started claiming a clicked node's amount equals the
// navigated list's total: the analytics reads apply no visibility predicate, so a posted
// uncategorised expense on such an account counts toward the node and cannot appear in the list.
// Hiding a user's own retained history because of missing interval data is the #1569 bug in
// miniature, so the leg below admits it.
//
// ⚠ Deliberately narrow: it fires ONLY when there is no 'active' row whatsoever. An account the
// user genuinely disconnected HAS an 'active' row followed by a terminal one, so the historical
// leg keeps governing it with its existing date bounds. This does not resurrect deliberately
// removed accounts.

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// noActiveHistoryRowRe matches a NOT EXISTS whose target is account_status_history filtered to
// 'active' — i.e. the assertion "this account has no active interval recorded at all".
//
// The two pre-existing legs cannot satisfy this: their only NOT EXISTS targets `accounts` (asking
// "is there a live successor?"), and their EXISTS over account_status_history is POSITIVE. An
// earlier version of this test asserted merely that the fragment contained "NOT EXISTS" and three
// "OR "s — both of which the old legs ALREADY satisfied, so it passed before the leg existed. That
// is the failure mode this file is named after; the regex below is the fix.
var noActiveHistoryRowRe = regexp.MustCompile(
	`(?s)NOT\s+EXISTS\s*\(\s*SELECT\s+1\s+FROM\s+account_status_history[^)]*status\s*=\s*'active'`)

func TestRetainedAccountVisibilityLegs_AdmitAnAccountWithNoActiveInterval(t *testing.T) {
	legs := retainedAccountVisibilityLegs("t", "t.date")

	require.Regexp(t, noActiveHistoryRowRe, squashWS(legs),
		"a third leg must admit an account with NO 'active' interval recorded, by asserting the "+
			"ABSENCE of such a row in account_status_history. Without it the feed hides the user's "+
			"own retained history on a backfill artifact, and a Sankey node's amount cannot equal "+
			"the navigated list's total (#1626). Got:\n%s", squashWS(legs))

	assert.Contains(t, legs, "h.valid_to",
		"the historical leg's date bound must survive — the new leg is additive, not a replacement")
}

// TestRetainedAccountVisibilityLegs_StillRequireSomethingOfEveryRow guards against the lazy fix:
// a tautology would make every parity assertion pass while silently resurrecting accounts the user
// deliberately disconnected.
func TestRetainedAccountVisibilityLegs_StillRequireSomethingOfEveryRow(t *testing.T) {
	legs := squashWS(retainedAccountVisibilityLegs("t", "t.date"))

	for _, tautology := range []string{"OR TRUE", "OR 1=1", "OR (1 = 1)"} {
		assert.NotContains(t, legs, tautology,
			"visibility must stay predicated on real account state")
	}
	assert.Contains(t, legs, "status = 'active'")
}
