//go:build integration

package database

// Issue #1569 residual E7: account_status_history.user_account_id was write-only AND unrepairable.
//
// The column is populated by 000152's backfill and by the record_account_status_change() trigger,
// which copies NEW.user_account_id at the instant of the status change. Nothing reads it today —
// the as-of liveness predicates all key on account_id — but that is not what makes it a defect.
// The defect is that backfillDownstreamUserAccountIDs, the startup repair that cascades a
// late-minted accounts.user_account_id to every table that derives one (transactions,
// investment_transactions, holdings, balance_snapshots, savings_goals, both transfer_suggestions
// columns), did not include this table. And a late mint is the NORMAL case for exactly the rows
// this issue cares about: #1569 added minting for accounts that arrive without a stable identity,
// so the account row exists — and its first 'active' history interval is already written, with
// user_account_id NULL — before the identity is assigned.
//
// So the first reader of this column would have found NULLs that no repair pass could ever fill,
// on precisely the pre-mint accounts whose history #1569 exists to keep. Cheaper to close now,
// while the answer is one more entry in a list, than to discover it from a chain query that
// quietly returns nothing.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/database/... \
//	  -run TestBackfillDownstream_AccountStatusHistory -tags integration -v

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBackfillDownstream_AccountStatusHistory_Integration(t *testing.T) {
	pool := connectBackfillTestDB(t)
	ctx := context.Background()

	suffix := uuid.New().String()[:8]
	userID := uuid.New().String()
	_, err := pool.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "test|ash-"+userID, "ash-"+userID+"@example.com")
	require.NoError(t, err)
	t.Cleanup(func() { pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", userID) }) //nolint:errcheck

	itemID := uuid.New().String()
	_, err = pool.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-test-token')
	`, itemID, userID, "ash-item-"+suffix)
	require.NoError(t, err)

	// The account is created WITHOUT a stable identity — the state #1569's minting path exists to
	// resolve — so the trigger writes its first history interval with user_account_id NULL.
	accountID := uuid.New().String()
	_, err = pool.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, status)
		VALUES ($1, $2, $3, $4, 'Pre-mint Checking', 'depository', 'active')
	`, accountID, itemID, userID, "ash-acct-"+suffix)
	require.NoError(t, err)

	var historyRows, nullBefore int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*), COUNT(*) FILTER (WHERE user_account_id IS NULL)
		 FROM account_status_history WHERE account_id = $1`, accountID).Scan(&historyRows, &nullBefore))
	require.Equal(t, 1, historyRows, "the insert trigger must have written the opening interval")
	require.Equal(t, 1, nullBefore, "and it must have carried the account's NULL identity")

	// The identity is minted afterwards, as it is in production. Updating user_account_id does not
	// fire the history trigger (AFTER UPDATE OF status), so nothing rewrites the interval.
	userAccountID := uuid.New().String()
	_, err = pool.Exec(ctx, `
		INSERT INTO user_accounts (id, user_id, institution_id, primary_type)
		VALUES ($1, $2, $3, 'depository')
	`, userAccountID, userID, "ash-inst-"+suffix)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE accounts SET user_account_id = $1 WHERE id = $2`, userAccountID, accountID)
	require.NoError(t, err)

	require.NoError(t, backfillDownstreamUserAccountIDs(ctx, pool))

	var repaired *string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT user_account_id::text FROM account_status_history WHERE account_id = $1`, accountID).Scan(&repaired))
	require.NotNil(t, repaired,
		"account_status_history is missing from the downstream cascade, so a late-minted identity "+
			"never reaches the history rows written before it existed")
	assert.Equal(t, userAccountID, *repaired)
}
