//go:build integration

package repository

// Issue #1537 round 9, finding F13: `UpsertBatch`'s conflict target is user-unscoped.
//
// `unique_plaid_investment_transaction_id` is UNIQUE on the plaid id ALONE
// (migrations/000051_create_investment_tables.up.sql:161), deliberately, for idempotent upserts. The
// DO UPDATE therefore matches on a key that says nothing about ownership, and its SET touches
// `user_account_id` — so a plaid-id collision across two users lets one user's sync write into the
// other's row.
//
// ⚠ THE SEVERITY HERE IS NARROW, and stating it honestly matters more than making it sound bad. An
// adversarial review reported this as "user A's sync rewrites a row owned by user B, with zero
// tenancy check". That overstates it twice: the DO UPDATE already carries
// `WHERE investment_transactions.user_account_id IS NULL AND EXCLUDED.user_account_id IS NOT NULL`,
// so it can only touch an UNATTRIBUTED row and can only fill in one column; and reaching it at all
// needs Plaid to reuse a transaction id across two users, which its API contract does not do.
//
// So this is not a live leak. It is a missing tenancy predicate on a write that has a cross-row
// mechanism, in a package whose own stated principle is "Ownership is enforced HERE ... not delegated
// to caller discipline" (holding_repository.go:53). The fix is one predicate and needs no migration,
// no interface change and no new sentinel — which is the argument for just doing it.

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// TestUpsertBatch_ForeignUsersRow_IsNotBackfilled pins that the DO UPDATE cannot reach across users.
//
// Shape: user B owns a row with a NULL user_account_id. User A syncs a transaction carrying the SAME
// plaid id and a non-NULL user_account_id. Pre-fix the ON CONFLICT matched B's row and both WHERE
// conditions held, so A's sync stamped A's user_account_id onto B's transaction.
func TestUpsertBatch_ForeignUsersRow_IsNotBackfilled(t *testing.T) {
	ctx := context.Background()
	pool := connectTestDB(t)

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	sharedPlaidID := "plaid-inv-txn-shared-" + suffix

	victimUser, _, victimAccount, _ := seedInvestmentFixture(ctx, t, tx, "victim-"+suffix)
	actorUser, _, actorAccount, _ := seedInvestmentFixture(ctx, t, tx, "actor-"+suffix)

	// The actor needs a user_accounts row, since that is the column the DO UPDATE writes.
	actorUserAccount := uuid.New()
	actorUserAccountStr := actorUserAccount.String()
	_, err = tx.Exec(ctx, `
		INSERT INTO user_accounts (id, user_id, institution_id) VALUES ($1, $2, $3)
	`, actorUserAccount, actorUser, "ins-"+suffix)
	require.NoError(t, err)

	// Victim's row: the shared plaid id, NULL user_account_id — the state the DO UPDATE may fill.
	_, err = tx.Exec(ctx, `
		INSERT INTO investment_transactions (
			id, user_id, account_id, user_account_id,
			plaid_investment_transaction_id, type, subtype, name,
			quantity, amount, price, fees, date, created_at
		) VALUES (gen_random_uuid(), $1, $2, NULL,
			$3, 'buy', 'buy', 'Victim holding', 1, 100, 100, 0, now(), now())
	`, victimUser, victimAccount, sharedPlaidID)
	require.NoError(t, err, "seeding the victim row")

	var before *uuid.UUID
	require.NoError(t, tx.QueryRow(ctx,
		`SELECT user_account_id FROM investment_transactions WHERE plaid_investment_transaction_id = $1`,
		sharedPlaidID).Scan(&before))
	require.Nil(t, before, "the victim row must start unattributed for this test to mean anything")

	// The actor's upsert, THROUGH THE REPOSITORY.
	//
	// ⚠ This test previously issued the upsert as inline SQL that contained the predicate itself, so it
	// verified PostgreSQL's ON CONFLICT semantics for a statement the test wrote — and could NOT fail if
	// the repository's own query lost the predicate. Both reviewers caught it independently (#1537
	// round 10). Driving UpsertBatch is the whole point: the assertion below now fails when
	// investment_transaction_repository.go changes, which is the only thing worth pinning.
	buySubtype, oneQty := "buy", 1.0
	repo := NewInvestmentTransactionRepository(tx)
	_, err = repo.UpsertBatch(ctx, []domain.InvestmentTransaction{{
		ID:                           uuid.NewString(),
		UserID:                       actorUser.String(),
		AccountID:                    actorAccount.String(),
		UserAccountID:                &actorUserAccountStr,
		PlaidInvestmentTransactionID: sharedPlaidID,
		Type:                         "buy",
		Subtype:                      &buySubtype,
		Name:                         "Actor holding",
		Quantity:                     &oneQty,
		Amount:                       100,
		Date:                         time.Now().UTC(),
	}})
	require.NoError(t, err, "the upsert itself must succeed; the tenancy predicate makes it a no-op, not an error")

	var after *uuid.UUID
	var owner uuid.UUID
	require.NoError(t, tx.QueryRow(ctx, `
		SELECT user_id, user_account_id FROM investment_transactions
		WHERE plaid_investment_transaction_id = $1
	`, sharedPlaidID).Scan(&owner, &after))

	require.Equal(t, victimUser, owner, "the row must still belong to the victim")
	require.Nil(t, after,
		"the actor's sync stamped its own user_account_id onto ANOTHER user's transaction. The "+
			"ON CONFLICT target is the plaid id alone, which carries no ownership, so the DO UPDATE "+
			"needs its own user_id predicate.")
}
