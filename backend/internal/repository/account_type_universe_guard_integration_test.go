//go:build integration

package repository

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
// Deliberately asserts the SET, not a substring: a check widened by adding a value passes any
// "contains 'investment'" test.
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

	assert.Equal(t,
		"CHECK (((type)::text = ANY ((ARRAY['depository'::character varying, 'credit'::character varying, 'loan'::character varying, 'investment'::character varying, 'other'::character varying])::text[])))",
		def,
		"accounts.type's universe changed. Only 'investment' can carry a holding today, which is why "+
			"AccountDetailPage.tsx gates the Holdings section (and a closed account's ONLY path to its "+
			"retained holdings history) on that one value. If a new type can hold positions, widen the "+
			"gate in the same change — otherwise a closed account of that type keeps history it cannot reach.")
}
