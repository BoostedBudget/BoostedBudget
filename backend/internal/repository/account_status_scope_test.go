package repository

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The as-of predicate every read that asks "was this account live at instant T" must emit.
// Written out here rather than referenced from the production constant on purpose: this test
// is the place a silent change to that SQL has to be justified, so it compares text to text.
const wantAsOfPredicate = "EXISTS (SELECT 1 FROM account_status_history h"

// TestResolveAccountStatusScope_Precedence pins the FULL precedence table of AccountFilterOptions,
// including the AsOf axis added by #1569. The four boolean flags are independent and
// non-exclusive, so every combination resolves to exactly one predicate — or, for the two
// combinations that are self-contradictory with AsOf, to an error rather than a silent winner.
func TestResolveAccountStatusScope_Precedence(t *testing.T) {
	asOf := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name      string
		opts      AccountFilterOptions
		want      accountStatusScope
		wantError bool
	}{
		{"default is active-only", AccountFilterOptions{}, accountStatusActiveOnly, false},
		{"IncludeReplaced widens to active+replaced", AccountFilterOptions{IncludeReplaced: true}, accountStatusActiveAndReplaced, false},
		{"AllStatuses drops the predicate", AccountFilterOptions{AllStatuses: true}, accountStatusAny, false},
		{"ReplacedOnly beats AllStatuses", AccountFilterOptions{ReplacedOnly: true, AllStatuses: true}, accountStatusReplacedOnly, false},
		{"AllStatuses beats IncludeReplaced", AccountFilterOptions{AllStatuses: true, IncludeReplaced: true}, accountStatusAny, false},
		{"AsOf replaces the active-only default", AccountFilterOptions{AsOf: &asOf}, accountStatusLiveAsOf, false},
		{"AsOf beats IncludeReplaced", AccountFilterOptions{AsOf: &asOf, IncludeReplaced: true}, accountStatusLiveAsOf, false},
		{"AsOf + ReplacedOnly is rejected", AccountFilterOptions{AsOf: &asOf, ReplacedOnly: true}, 0, true},
		{"AsOf + AllStatuses is rejected", AccountFilterOptions{AsOf: &asOf, AllStatuses: true}, 0, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveAccountStatusScope(tc.opts)
			if tc.wantError {
				require.Error(t, err, "a contradictory combination must be rejected, not silently resolved")
				assert.ErrorIs(t, err, ErrAccountFilterAsOfConflict)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestAccountStatusScope_Clause pins the SQL each resolved scope emits. accountStatusLiveAsOf is
// the only one that consumes an argument placeholder; the others must ignore it entirely so a
// caller that passes 0 (no as-of arg bound) cannot produce a `$0` reference.
func TestAccountStatusScope_Clause(t *testing.T) {
	assert.Equal(t, " AND a.status = 'active'", accountStatusActiveOnly.clause(0))
	assert.Equal(t, " AND a.status IN ('active', 'replaced')", accountStatusActiveAndReplaced.clause(0))
	assert.Equal(t, " AND a.status = 'replaced'", accountStatusReplacedOnly.clause(0))
	assert.Equal(t, "", accountStatusAny.clause(0))

	asOfClause := accountStatusLiveAsOf.clause(3)
	assert.Contains(t, asOfClause, wantAsOfPredicate,
		"the as-of scope must read recorded liveness from account_status_history, not guess from accounts.status")
	assert.Contains(t, asOfClause, "h.account_id = a.id")
	assert.Contains(t, asOfClause, "h.status = 'active'")
	assert.Contains(t, asOfClause, "h.valid_from <= $3")
	assert.Contains(t, asOfClause, "h.valid_to IS NULL OR h.valid_to > $3",
		"intervals are half-open [valid_from, valid_to): live AT valid_from, NOT live at valid_to")
	assert.NotContains(t, asOfClause, "a.status",
		"the as-of scope REPLACES the current-status predicate; keeping it would re-drop the "+
			"since-disconnected accounts this option exists to resolve")
	assert.NotContains(t, asOfClause, "$0")
}

// TestAccountStatusScope_DropsReplaced pins which scopes increment the AccountsReplacedFiltered
// canary. Only the active-only default drops replaced rows; the as-of scope is a HISTORICAL
// universe and drops nothing that was live at its instant, so — like AllStatuses — it stays silent.
func TestAccountStatusScope_DropsReplaced(t *testing.T) {
	assert.True(t, accountStatusActiveOnly.dropsReplaced())
	assert.False(t, accountStatusActiveAndReplaced.dropsReplaced())
	assert.False(t, accountStatusReplacedOnly.dropsReplaced())
	assert.False(t, accountStatusAny.dropsReplaced())
	assert.False(t, accountStatusLiveAsOf.dropsReplaced(),
		"an as-of read resolves the account universe as it really was; it drops nothing to warn about")
}

// TestHouseholdScopeQuery_LiveAsOf pins that the household as-of variant emits the same recorded
// liveness predicate as the personal one, bound to $4 — the asOf argument getForHouseholdScope
// already appends after householdScopeArgs' three. Binding it anywhere else would either double-bind
// asOf or reference an argument that does not exist.
func TestHouseholdScopeQuery_LiveAsOf(t *testing.T) {
	q := householdScopeQuery(householdLiveAsOf)
	assert.Contains(t, q, wantAsOfPredicate)
	assert.Contains(t, q, "h.account_id = a.id")
	assert.Contains(t, q, "h.valid_from <= $4")
	assert.Contains(t, q, "h.valid_to IS NULL OR h.valid_to > $4")
	assert.NotContains(t, q, "a.status = 'active'",
		"the as-of variant must REPLACE the unconditional active predicate, not sit beside it")
	assert.Equal(t, 1, strings.Count(q, "account_status_history"))

	// The active-only variant is unchanged.
	assert.Contains(t, householdScopeQuery(householdActiveOnly), "a.status = 'active'")
	assert.NotContains(t, householdScopeQuery(householdActiveOnly), "account_status_history")
}
