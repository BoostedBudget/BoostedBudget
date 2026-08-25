//go:build integration

package repository

// #1569 whole-branch review C1 — a Plaid sync must not shrink a DELETED goal's retained ledger.
//
// DeleteManagedForTransaction is called from transactionSyncService.processRemovedTransactions for
// every `removed` entry Plaid delivers. Its predicate was `transaction_id = $1 AND
// managed_by_reconciliation = true` with NO goal-liveness clause, so a bank re-stating a
// transaction weeks after the user deleted a goal silently destroyed rows out of the ledger that
// #1569 T2/T7 built specifically to prove retention: the Deleted-goals row still read
// "$300.00 of $1,000.00 saved" (savings_goals.current_amount, untouched) while the expanded ledger
// underneath it read "Confirmed total $200.00". Two numbers for the same money, no notice, no audit
// row. That is side-effect destruction — the ADR's forbidden class.
//
// ⚠ THE OBVIOUS REFUTATION IS WRONG, and it is the reason this hole survived review once.
// "ConfirmContribution (savings_contribution_repository.go:~729) sets
// managed_by_reconciliation = false, so a managed row can never be confirmed, so only
// regenerable proposals are ever deleted." Enumerate ALL FOUR writers of status='confirmed'
// instead of that one:
//
//	1. CreateContribution (:~203)          — caller-supplied flags; managed is whatever is passed.
//	2. UpsertFromTransaction (:~313)       — INSERTS status='confirmed', managed=true. Reached from
//	                                         transferCorrelationService.createBoostMatchContribution
//	                                         with source='boost_match', and its ON CONFLICT DO UPDATE
//	                                         (:~316) promotes a proposed row to confirmed WITHOUT
//	                                         clearing the flag.
//	3. InsertManualFromTransaction (:~407) — clears managed (=false). Harmless.
//	4. ConfirmContribution (:~729)         — clears managed (=false). Harmless.
//
// Writer 2 is the counterexample: a CONFIRMED, reconciler-flagged record of the user's own money
// moving into a goal. It is not a proposal and it is not regenerable — the boost that matched it
// may be long gone.
//
// The fixture below is built through the production write path (UpsertFromTransaction) rather than
// a hand-rolled INSERT, on purpose: the schema rejected four unfaithful shapes during review
// (valid_contribution_source, savings_goals_tombstone_consistency, and two NOT NULLs), and a
// fixture the application could never produce proves nothing even when it inserts.
//
// Both directions are asserted: the tombstoned goal's confirmed row SURVIVES the sync cleanup, and
// a LIVE goal's managed row is still removed (the fix must not turn the cleaner into a no-op —
// phantom progress on a live goal, Codex C4, is what it exists to prevent).

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeleteManagedForTransaction_DeletedGoalLedgerSurvives_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID := uuid.New().String()
	linkedItemID := uuid.New().String()
	accountID := uuid.New().String()
	deletedGoalID := uuid.New().String()
	liveGoalID := uuid.New().String()
	deletedTxnID := uuid.New().String()
	liveTxnID := uuid.New().String()

	mustExec := func(what, sql string, args ...any) {
		t.Helper()
		_, execErr := tx.Exec(ctx, sql, args...)
		require.NoError(t, execErr, "seed %s", what)
	}

	mustExec("user", `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "test|managed-del-"+userID, "managed-del-"+userID+"@example.com")
	mustExec("linked_item", `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-test-token')
	`, linkedItemID, userID, "managed-del-item-"+linkedItemID)
	mustExec("account", `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, status)
		VALUES ($1, $2, $3, $4, 'Savings', 'depository', 'active')
	`, accountID, linkedItemID, userID, "plaid-managed-del-"+accountID)

	for _, g := range []struct{ id, name string }{
		{deletedGoalID, "Abandoned Trip"},
		{liveGoalID, "Still Saving"},
	} {
		mustExec("savings_goal", `
			INSERT INTO savings_goals (id, user_id, name, target_amount, current_amount, account_id, progress_basis)
			VALUES ($1, $2, $3, 1000, 300, $4, 'contributions')
		`, g.id, userID, g.name, accountID)
	}
	for _, txn := range []string{deletedTxnID, liveTxnID} {
		mustExec("transaction", `
			INSERT INTO transactions (id, user_id, account_id, linked_item_id, source, amount, name, date, created_at, updated_at)
			VALUES ($1, $2, $3, $4, 'plaid', -100, 'Transfer to savings', now()::date, now(), now())
		`, txn, userID, accountID, linkedItemID)
	}

	repo := NewSavingsContributionRepository(tx)
	goalRepo := NewSavingsGoalRepository(tx)

	// The production boost-match path: a CONFIRMED, managed_by_reconciliation=true row.
	require.NoError(t, repo.UpsertFromTransaction(ctx, userID, deletedGoalID, deletedTxnID, 100, "boost_match"))
	require.NoError(t, repo.UpsertFromTransaction(ctx, userID, liveGoalID, liveTxnID, 100, "boost_match"))

	// Prove the fixture is the shape the hole needs, BEFORE exercising the hole. If either flag is
	// wrong the test below would pass for the wrong reason.
	var status string
	var managed bool
	require.NoError(t, tx.QueryRow(ctx, `
		SELECT status, managed_by_reconciliation FROM savings_goal_contributions
		WHERE transaction_id = $1 AND savings_goal_id = $2
	`, deletedTxnID, deletedGoalID).Scan(&status, &managed))
	require.Equal(t, "confirmed", status, "UpsertFromTransaction writes status='confirmed'")
	require.True(t, managed, "…and leaves managed_by_reconciliation=true — this is the counterexample")

	// The user deletes the goal. T8 tombstones; nothing is destroyed.
	require.NoError(t, goalRepo.Delete(ctx, deletedGoalID, userID, nil))

	before, err := repo.SumConfirmedByDeletedGoal(ctx, userID, deletedGoalID)
	require.NoError(t, err)
	require.InDelta(t, 100.0, before, 0.001, "the deleted goal's retained ledger before the sync")

	// Weeks later, the bank re-states the transaction and Plaid delivers it as `removed`.
	require.NoError(t, repo.DeleteManagedForTransaction(ctx, deletedTxnID))

	after, err := repo.SumConfirmedByDeletedGoal(ctx, userID, deletedGoalID)
	require.NoError(t, err)
	assert.InDelta(t, 100.0, after, 0.001,
		"a Plaid `removed` entry must NOT shrink a deleted goal's retained ledger (#1569 C1): "+
			"the goal is a tombstone, the row is a confirmed record of the user's money, and the "+
			"panel header (savings_goals.current_amount) is not updated to match — the two numbers "+
			"would disagree in one visual block")

	entries, err := repo.ListByDeletedGoal(ctx, userID, deletedGoalID)
	require.NoError(t, err)
	assert.Len(t, entries, 1, "the itemised entry is still readable, not just the total")

	// The other direction: a LIVE goal's managed row must still be cleaned up, or the fix trades
	// C1 for the phantom-progress bug (Codex C4) the cleaner was written to prevent.
	require.NoError(t, repo.DeleteManagedForTransaction(ctx, liveTxnID))
	var liveRemaining int
	require.NoError(t, tx.QueryRow(ctx, `
		SELECT count(*) FROM savings_goal_contributions WHERE savings_goal_id = $1
	`, liveGoalID).Scan(&liveRemaining))
	assert.Equal(t, 0, liveRemaining,
		"a live goal's reconciler-owned row is still removed when its transaction is removed")
}
