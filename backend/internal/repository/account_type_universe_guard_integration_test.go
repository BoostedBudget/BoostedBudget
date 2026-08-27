//go:build integration

package repository

import (
	"context"
	"regexp"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// checkConstraintLiteral extracts the single-quoted literals from a rendered CHECK constraint.
// Everything else in the deparse output — the parenthesisation, the `::character varying` element
// casts, the `(...)::text[]` wrapper — is `ruleutils.c` formatting driven by the column's type, and
// changes with no semantic change at all (a Postgres major upgrade, or `type` becoming `text`).
// The literals are the part that says what the universe IS.
var checkConstraintLiteral = regexp.MustCompile(`'([^']*)'`)

// TestAccountTypeUniverseMatchesTheHoldingsGate is #1569 T7.8, item 3.
//
// AccountDetailPage.tsx renders the Holdings section — and therefore the ONLY route to a closed
// account's retained holdings history — when `account.type === 'investment'`. That gate was kept
// rather than widened to "gate on the retained data existing", and the reason recorded beside it
// is a claim about the SCHEMA: accounts.type can only be one of five values, and no value other
// than 'investment' can carry a holding.
//
// CLAUDE.md's own warning applies here — nothing checks that a recorded reason is still TRUE, and
// two shipped false on this branch. So the reason is pinned to the live catalog rather than to a
// migration file: a later ALTER that widens accounts.type (say, adding the legacy Plaid value
// 'brokerage', which today lives only on user_accounts.primary_type — a DIFFERENT column on a
// DIFFERENT table) fails here, and whoever writes it has to revisit the gate in the same change.
//
// Deliberately asserts the SET of literals, not a substring and not the deparse text: a check
// widened by adding a value passes any "contains 'investment'" test, while pinning
// pg_get_constraintdef's rendering verbatim (as this did until #1569 T8.7) goes red on a Postgres
// major upgrade or on `type` becoming `text` — neither of which changes the universe at all.
func TestAccountTypeUniverseMatchesTheHoldingsGate(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	var def string
	err := pool.QueryRow(ctx, `
		SELECT pg_get_constraintdef(c.oid)
		FROM pg_constraint c
		JOIN pg_class t ON t.oid = c.conrelid
		JOIN pg_namespace n ON n.oid = t.relnamespace
		WHERE t.relname = 'accounts'
		  AND n.nspname = current_schema()
		  AND c.conname = 'valid_account_type'
	`).Scan(&def)
	require.NoError(t, err,
		"accounts.valid_account_type must exist — without it accounts.type has no universe at all, and the type-based holdings gate rests on nothing")

	var got []string
	for _, m := range checkConstraintLiteral.FindAllStringSubmatch(def, -1) {
		got = append(got, m[1])
	}
	sort.Strings(got)

	// Compared as a SET, not a substring: a check widened by adding a value passes any
	// "contains 'investment'" test, and a check narrowed by removing one passes it too.
	assert.Equal(t,
		[]string{"credit", "depository", "investment", "loan", "other"},
		got,
		"accounts.type's universe changed (constraint now reads: %s).\n"+
			"Only 'investment' can carry a holding today, which is why AccountDetailPage.tsx gates the "+
			"Holdings section (and a closed account's ONLY path to its retained holdings history) on "+
			"that one value. If a new type can hold positions, widen the gate in the same change — "+
			"otherwise a closed account of that type keeps history it cannot reach.", def)
}
