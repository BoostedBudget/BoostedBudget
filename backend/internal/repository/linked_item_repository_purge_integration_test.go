//go:build integration

package repository

// Issue #1569, Task 6, fix round 1 — IMPORTANT 2: purging a re-linked institution used to return
// an opaque 500 forever. linked_items.replaced_by_id and accounts.replaced_by_id are both
// `REFERENCES ...(id)` with NO explicit ON DELETE action (defaults to NO ACTION, migrations
// 000032/000033) — a genuinely populated column on re-link (account_service.go's LinkAccounts
// re-link branch). Purging the NEWER item/account while an OLDER one still points at it via
// replaced_by_id violated that FK, and PurgeLinkedItem only translated
// fk_savings_goals_account_id, so any other 23503 fell through to a bare fmt.Errorf and an opaque
// 500 with no recovery path. Fixed by nulling both dangling columns before the destructive
// deletes, mirroring DeleteUserCascade's identical fix in profile_repository.go.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run TestLinkedItemRepository_PurgeLinkedItem -tags integration -v -p 1

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// disconnectItemAccountsForPurge puts an item's accounts into the state a real user disconnect
// leaves behind (accountRepository.MarkRemovedByLinkedItemID's statement, inlined so this test
// file does not need the account repository). PurgeLinkedItem refuses while any account under the
// item is still 'active' — #1569 residual E9 — so every test that purges must reach that state
// the way production does, rather than stamping plaid_removed_at and jumping the queue.
func disconnectItemAccountsForPurge(ctx context.Context, t *testing.T, pool *pgxpool.Pool, linkedItemID any) {
	t.Helper()
	_, err := pool.Exec(ctx, `
		UPDATE accounts
		SET status = 'removed', removed_at = NOW(), removed_reason = 'user_disconnect', updated_at = NOW()
		WHERE linked_item_id = $1 AND status = 'active'`, linkedItemID)
	require.NoError(t, err, "seed the post-disconnect account state PurgeLinkedItem requires")
}

// TestLinkedItemRepository_PurgeLinkedItem_Integration_RefusesUntilDisconnected proves the
// plaid_removed_at guard: a purge must never be the first thing that happens to a linked item.
func TestLinkedItemRepository_PurgeLinkedItem_Integration_RefusesUntilDisconnected(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	ctx := context.Background()
	profileRepo := NewProfileRepository(pool, enc)
	repo := NewLinkedItemRepository(pool, enc)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	userID := createMarkDisconnectedFixtureUser(t, ctx, pool, profileRepo, suffix)
	item := createMarkDisconnectedFixtureItem(t, ctx, repo, userID, suffix)

	err := repo.PurgeLinkedItem(ctx, item.ID, userID, nil)
	require.ErrorIs(t, err, ErrLinkedItemNotDisconnected)

	var count int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM linked_items WHERE id = $1`, item.ID).Scan(&count))
	assert.Equal(t, 1, count, "the refused purge must roll back — the item must still exist")
}

// TestLinkedItemRepository_PurgeLinkedItem_Integration_ClearsDanglingReplacedByReferences is the
// fix round 1 IMPORTANT 2 regression test. Seeds an OLDER item/account pair whose replaced_by_id
// points at a NEWER item/account (exactly what LinkAccounts' re-link branch produces), then
// purges the NEWER pair. Before the fix this failed with a bare foreign-key-violation error; the
// fix nulls both dangling references first.
func TestLinkedItemRepository_PurgeLinkedItem_Integration_ClearsDanglingReplacedByReferences(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	ctx := context.Background()
	profileRepo := NewProfileRepository(pool, enc)
	repo := NewLinkedItemRepository(pool, enc)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	userID := createMarkDisconnectedFixtureUser(t, ctx, pool, profileRepo, suffix)

	newItem := createMarkDisconnectedFixtureItem(t, ctx, repo, userID, suffix+"-new")
	oldItem := createMarkDisconnectedFixtureItem(t, ctx, repo, userID, suffix+"-old")

	var newAccountID string
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO accounts (linked_item_id, user_id, plaid_account_id, name, type, status)
		VALUES ($1, $2, $3, 'New Checking', 'depository', 'active')
		RETURNING id
	`, newItem.ID, userID, "plaid-acct-new-"+suffix).Scan(&newAccountID))

	var oldAccountID string
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO accounts (linked_item_id, user_id, plaid_account_id, name, type, status, replaced_at, replaced_by_id)
		VALUES ($1, $2, $3, 'Old Checking', 'depository', 'replaced', NOW(), $4)
		RETURNING id
	`, oldItem.ID, userID, "plaid-acct-old-"+suffix, newAccountID).Scan(&oldAccountID))

	// oldItem.replaced_by_id -> newItem.ID: exactly what a re-link produces at the item level.
	_, err := pool.Exec(ctx, `UPDATE linked_items SET replaced_by_id = $1, status = 'replaced', replaced_at = NOW() WHERE id = $2`, newItem.ID, oldItem.ID)
	require.NoError(t, err)

	// Disconnect newItem so neither purge precondition blocks the purge under test.
	require.NoError(t, repo.MarkPlaidRemoved(ctx, []string{newItem.ID}))
	disconnectItemAccountsForPurge(ctx, t, pool, newItem.ID)

	err = repo.PurgeLinkedItem(ctx, newItem.ID, userID, nil)
	require.NoError(t, err, "purging a re-linked-FROM item must not fail with a dangling foreign-key violation")

	var newItemCount int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM linked_items WHERE id = $1`, newItem.ID).Scan(&newItemCount))
	assert.Equal(t, 0, newItemCount, "the purged item must be gone")

	var oldItemReplacedBy *string
	require.NoError(t, pool.QueryRow(ctx, `SELECT replaced_by_id FROM linked_items WHERE id = $1`, oldItem.ID).Scan(&oldItemReplacedBy))
	assert.Nil(t, oldItemReplacedBy, "the older item's dangling replaced_by_id must be nulled, not left pointing at a deleted row")

	var oldAccountReplacedBy *string
	require.NoError(t, pool.QueryRow(ctx, `SELECT replaced_by_id FROM accounts WHERE id = $1`, oldAccountID).Scan(&oldAccountReplacedBy))
	assert.Nil(t, oldAccountReplacedBy, "the older account's dangling replaced_by_id must be nulled, not left pointing at a deleted row")

}

// TestLinkedItemRepository_PurgeLinkedItem_Integration_RemovesAllAccountsAndDerivedRows proves the
// institution-level purge destroys EVERY account under the item, not just one — fix round 2
// finding: the account-level suite proved the single-account case seven ways, but PurgeLinkedItem
// itself (which fans out over N accounts) had no test proving it doesn't stop after the first.
func TestLinkedItemRepository_PurgeLinkedItem_Integration_RemovesAllAccountsAndDerivedRows(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	repo := NewLinkedItemRepository(pool, newTestFieldEncryptor(t))

	f := seedRetainedFixture(ctx, t, pool, "purge-item-all-"+uuid.New().String())
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, f.UserID) })

	secondAccountID := uuid.New()
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, status)
		VALUES ($1, $2, $3, $4, 'Second Account', 'depository', 'active')
		RETURNING id
	`, secondAccountID, f.LinkedItemID, f.UserID, "plaid-acct-second-"+uuid.New().String()).Scan(&secondAccountID))
	second := retainedFixture{UserID: f.UserID, LinkedItemID: f.LinkedItemID, AccountID: secondAccountID, UserAccountID: f.UserAccountID}

	snap1 := seedBalanceSnapshot(ctx, t, pool, f)
	hold1 := seedHolding(ctx, t, pool, f)
	liab1 := seedCreditLiability(ctx, t, pool, f)
	inv1 := seedInvestmentTransaction(ctx, t, pool, f)

	snap2 := seedBalanceSnapshot(ctx, t, pool, second)
	hold2 := seedHolding(ctx, t, pool, second)
	liab2 := seedCreditLiability(ctx, t, pool, second)
	inv2 := seedInvestmentTransaction(ctx, t, pool, second)

	require.NoError(t, repo.MarkPlaidRemoved(ctx, []string{f.LinkedItemID.String()}))
	disconnectItemAccountsForPurge(ctx, t, pool, f.LinkedItemID)

	err := repo.PurgeLinkedItem(ctx, f.LinkedItemID.String(), f.UserID.String(), nil)
	require.NoError(t, err)

	assert.Equal(t, 0, countRows(ctx, t, pool, "linked_items", "id", f.LinkedItemID), "item must be gone")
	assert.Equal(t, 0, countRows(ctx, t, pool, "accounts", "id", f.AccountID), "first account must be gone")
	assert.Equal(t, 0, countRows(ctx, t, pool, "accounts", "id", secondAccountID), "second account must be gone")
	assert.Equal(t, 0, countRows(ctx, t, pool, "balance_snapshots", "id", snap1), "first account's balance_snapshot must be gone")
	assert.Equal(t, 0, countRows(ctx, t, pool, "balance_snapshots", "id", snap2), "second account's balance_snapshot must be gone")
	assert.Equal(t, 0, countRows(ctx, t, pool, "holdings", "id", hold1), "first account's holding must be gone")
	assert.Equal(t, 0, countRows(ctx, t, pool, "holdings", "id", hold2), "second account's holding must be gone")
	assert.Equal(t, 0, countRows(ctx, t, pool, "credit_liabilities", "id", liab1), "first account's liability must be gone")
	assert.Equal(t, 0, countRows(ctx, t, pool, "credit_liabilities", "id", liab2), "second account's liability must be gone")
	assert.Equal(t, 0, countRows(ctx, t, pool, "investment_transactions", "id", inv1), "first account's investment transaction must be gone")
	assert.Equal(t, 0, countRows(ctx, t, pool, "investment_transactions", "id", inv2), "second account's investment transaction must be gone")
}

// TestLinkedItemRepository_PurgeLinkedItem_Integration_SavingsGoalOnAnyAccountRollsBackWholeItem
// proves the transactional property that matters most for this feature: a savings goal on ANY ONE
// account under the item must refuse the WHOLE purge, and the refusal must leave the item, BOTH
// accounts, and their derived rows completely intact — a partial institution purge (some accounts
// destroyed, the goal-referenced one and the item itself surviving) would be the worst outcome
// this feature could produce. Mutation-checked (see task-6-report.md for the round-2 fix-round
// verification transcript): temporarily short-circuited the transaction to commit before the
// constraint could fire, confirmed this test goes RED, then restored.
func TestLinkedItemRepository_PurgeLinkedItem_Integration_SavingsGoalOnAnyAccountRollsBackWholeItem(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	repo := NewLinkedItemRepository(pool, newTestFieldEncryptor(t))

	f := seedRetainedFixture(ctx, t, pool, "purge-item-goal-"+uuid.New().String())
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, f.UserID) })

	secondAccountID := uuid.New()
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, status)
		VALUES ($1, $2, $3, $4, 'Second Account', 'depository', 'active')
		RETURNING id
	`, secondAccountID, f.LinkedItemID, f.UserID, "plaid-acct-second-"+uuid.New().String()).Scan(&secondAccountID))
	second := retainedFixture{UserID: f.UserID, LinkedItemID: f.LinkedItemID, AccountID: secondAccountID, UserAccountID: f.UserAccountID}

	// The goal references the SECOND account only — proving the RESTRICT on ANY account under the
	// item blocks the whole purge, not just a purge targeting that specific account.
	snap1 := seedBalanceSnapshot(ctx, t, pool, f)
	seedSavingsGoal(ctx, t, pool, second)

	require.NoError(t, repo.MarkPlaidRemoved(ctx, []string{f.LinkedItemID.String()}))
	disconnectItemAccountsForPurge(ctx, t, pool, f.LinkedItemID)

	err := repo.PurgeLinkedItem(ctx, f.LinkedItemID.String(), f.UserID.String(), nil)
	require.ErrorIs(t, err, ErrPurgeBlockedBySavingsGoal)

	assert.Equal(t, 1, countRows(ctx, t, pool, "linked_items", "id", f.LinkedItemID),
		"the refused purge must roll back — the item must still exist")
	assert.Equal(t, 1, countRows(ctx, t, pool, "accounts", "id", f.AccountID),
		"the refused purge must roll back — the first (non-goal) account must still exist")
	assert.Equal(t, 1, countRows(ctx, t, pool, "accounts", "id", secondAccountID),
		"the refused purge must roll back — the goal-referenced account must still exist")
	assert.Equal(t, 1, countRows(ctx, t, pool, "balance_snapshots", "id", snap1),
		"the refused purge must roll back — the first account's balance_snapshot must still exist")
}

// TestLinkedItemRepository_PurgeLinkedItem_Integration_BankTransactionsSurviveAcrossAllAccounts
// proves banking transactions on EVERY account under a purged item survive with account_id AND
// linked_item_id set NULL — the same guarantee TestAccountRepository_PurgeAccount_Integration_
// TransactionsSurvive proves at the single-account level, extended to the institution-level fan-out.
func TestLinkedItemRepository_PurgeLinkedItem_Integration_BankTransactionsSurviveAcrossAllAccounts(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	repo := NewLinkedItemRepository(pool, newTestFieldEncryptor(t))

	f := seedRetainedFixture(ctx, t, pool, "purge-item-txn-"+uuid.New().String())
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, f.UserID) })

	secondAccountID := uuid.New()
	secondUserAccountID := uuid.New()
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO user_accounts (id, user_id, institution_id, primary_type)
		VALUES ($1, $2, $3, 'depository') RETURNING id
	`, secondUserAccountID, f.UserID, "second-inst-"+secondUserAccountID.String()).Scan(&secondUserAccountID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, status, user_account_id)
		VALUES ($1, $2, $3, $4, 'Second Account', 'depository', 'active', $5)
		RETURNING id
	`, secondAccountID, f.LinkedItemID, f.UserID, "plaid-acct-second-"+uuid.New().String(), secondUserAccountID).Scan(&secondAccountID))
	second := retainedFixture{UserID: f.UserID, LinkedItemID: f.LinkedItemID, AccountID: secondAccountID, UserAccountID: secondUserAccountID}

	txn1 := seedBankingTxn(ctx, t, pool, f, time.Now(), "First Account Coffee")
	txn2 := seedBankingTxn(ctx, t, pool, second, time.Now(), "Second Account Coffee")

	require.NoError(t, repo.MarkPlaidRemoved(ctx, []string{f.LinkedItemID.String()}))
	disconnectItemAccountsForPurge(ctx, t, pool, f.LinkedItemID)
	require.NoError(t, repo.PurgeLinkedItem(ctx, f.LinkedItemID.String(), f.UserID.String(), nil))

	for _, txnID := range []uuid.UUID{txn1, txn2} {
		var accountIDAfter, linkedItemIDAfter *uuid.UUID
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT account_id, linked_item_id FROM transactions WHERE id = $1`, txnID,
		).Scan(&accountIDAfter, &linkedItemIDAfter))
		assert.Nil(t, accountIDAfter, "transaction %s must survive with account_id set NULL", txnID)
		assert.Nil(t, linkedItemIDAfter, "transaction %s must survive with linked_item_id set NULL", txnID)
	}
}

// TestLinkedItemRepository_PurgeLinkedItem_Integration_WritesAuditRowInSameTransaction mirrors
// TestAccountRepository_PurgeAccount_Integration_WritesAuditRowInSameTransaction at the
// institution level: the linked_item.history_purged entry must commit in the SAME transaction as
// the destruction it describes, via AuditLogRepository.LogInTx.
func TestLinkedItemRepository_PurgeLinkedItem_Integration_WritesAuditRowInSameTransaction(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	repo := NewLinkedItemRepository(pool, newTestFieldEncryptor(t))
	auditLog := NewAuditLogRepository(pool)

	f := seedRetainedFixture(ctx, t, pool, "purge-item-audit-"+uuid.New().String())
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, f.UserID) })

	require.NoError(t, repo.MarkPlaidRemoved(ctx, []string{f.LinkedItemID.String()}))
	disconnectItemAccountsForPurge(ctx, t, pool, f.LinkedItemID)
	require.NoError(t, repo.PurgeLinkedItem(ctx, f.LinkedItemID.String(), f.UserID.String(), auditLog))

	var count int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM audit_logs WHERE action = 'linked_item.history_purged' AND resource_type = 'linked_item' AND resource_id = $1`,
		f.LinkedItemID.String(),
	).Scan(&count))
	assert.Equal(t, 1, count, "a linked_item.history_purged audit entry must be written")
}

// TestLinkedItemRepository_PurgeLinkedItem_Integration_RefusesWhileAnAccountIsStillActive is
// #1569 residual E9.
//
// plaid_removed_at alone is NOT proof that the USER disconnected this institution. The billing
// cleanup sweep stamps it too: cleanupCandidateReasonPredicate selects items merely because they
// are in an error state, or stale, and PlaidItemCleanupService.RemoveItem then stamps
// plaid_removed_at and marks the item 'disconnected' — while leaving every account under it at
// status='active' (accountRepo.MarkRemovedByLinkedItemID has exactly one production caller,
// accountService.DisconnectInstitution, and the sweep is not it). So a background job could
// silently satisfy the precondition for the most destructive verb this repository exposes.
//
// The single-account path already refuses that: PurgeAccount returns ErrAccountNotDisconnected
// for a status='active' row. This is the institution-level equal, and it is what makes "disconnect
// first, then purge" a rule the schema enforces rather than a rule the UI happens to follow.
// There is no dead end: DisconnectInstitution is idempotent against an already-removed Plaid item
// (ITEM_NOT_FOUND maps to success) and marks the accounts removed on the way through.
func TestLinkedItemRepository_PurgeLinkedItem_Integration_RefusesWhileAnAccountIsStillActive(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	repo := NewLinkedItemRepository(pool, newTestFieldEncryptor(t))

	f := seedRetainedFixture(ctx, t, pool, "purge-item-still-active-"+uuid.New().String())
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, f.UserID) })

	snap := seedBalanceSnapshot(ctx, t, pool, f)

	// Exactly the state the cleanup sweep leaves behind: Plaid side recorded done, accounts
	// untouched.
	require.NoError(t, repo.MarkPlaidRemoved(ctx, []string{f.LinkedItemID.String()}))

	err := repo.PurgeLinkedItem(ctx, f.LinkedItemID.String(), f.UserID.String(), nil)
	require.ErrorIs(t, err, ErrLinkedItemNotDisconnected,
		"a purge must not be reachable while the institution still has a live account under it")

	assert.Equal(t, 1, countRows(ctx, t, pool, "linked_items", "id", f.LinkedItemID), "item must survive")
	assert.Equal(t, 1, countRows(ctx, t, pool, "accounts", "id", f.AccountID), "account must survive")
	assert.Equal(t, 1, countRows(ctx, t, pool, "balance_snapshots", "id", snap), "history must survive")

	// And once the accounts really are removed, the same call goes through — the guard is a
	// precondition, not a wall.
	_, err = pool.Exec(ctx, `
		UPDATE accounts SET status = 'removed', removed_at = NOW(), removed_reason = 'user_disconnect'
		WHERE linked_item_id = $1 AND status = 'active'`, f.LinkedItemID)
	require.NoError(t, err)
	require.NoError(t, repo.PurgeLinkedItem(ctx, f.LinkedItemID.String(), f.UserID.String(), nil))
	assert.Equal(t, 0, countRows(ctx, t, pool, "linked_items", "id", f.LinkedItemID))
}
