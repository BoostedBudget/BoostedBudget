//go:build integration

package repository

// Issue #1569, Task 6 — the explicit deletion escape hatch.
//
// PurgeAccount is the ONE place a hard delete is correct: it must actually destroy the account row
// and everything derived from it (balance_snapshots, holdings, investment_transactions,
// liabilities), while leaving the user's transactions and any savings-goal-referenced account
// alone. pgxmock cannot prove any of that — it cannot enforce migration 000150's RESTRICT
// constraint, and it cannot prove a CASCADE actually fired against a real schema — so this must run
// against real Postgres.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run TestAccountRepository_PurgeAccount -tags integration -v -p 1

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// seedBalanceSnapshot inserts one balance_snapshot for the fixture account.
func seedBalanceSnapshot(ctx context.Context, t *testing.T, db DB, f retainedFixture) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	require.NoError(t, db.QueryRow(ctx, `
		INSERT INTO balance_snapshots (account_id, user_id, current_balance, currency_code, snapshot_at, source)
		VALUES ($1, $2, 500.00, 'USD', NOW(), 'manual')
		RETURNING id
	`, f.AccountID, f.UserID).Scan(&id), "seed balance_snapshot")
	return id
}

// seedHolding inserts one security + holding for the fixture account.
func seedHolding(ctx context.Context, t *testing.T, db DB, f retainedFixture) uuid.UUID {
	t.Helper()
	secSuffix := uuid.New().String()
	var securityID uuid.UUID
	require.NoError(t, db.QueryRow(ctx, `
		INSERT INTO securities (plaid_security_id, name) VALUES ($1, 'Test Security') RETURNING id
	`, "plaid-sec-"+secSuffix).Scan(&securityID), "seed security")

	var holdingID uuid.UUID
	require.NoError(t, db.QueryRow(ctx, `
		INSERT INTO holdings (user_id, account_id, security_id, quantity)
		VALUES ($1, $2, $3, 10) RETURNING id
	`, f.UserID, f.AccountID, securityID).Scan(&holdingID), "seed holding")
	return holdingID
}

// seedCreditLiability inserts one credit_liabilities row for the fixture account.
// seedInvestmentTransaction inserts one investment_transactions row for the fixture account.
// investment_transactions.account_id is ON DELETE CASCADE (migration 000051) — unlike a banking
// transaction, an investment transaction (buy/sell/dividend) has no independent existence apart
// from the account and IS destroyed by a purge. Fix round 1 finding: the purge dialog and API
// descriptions previously said "transactions are not destroyed" without this carve-out, which was
// false for every investment account.
func seedInvestmentTransaction(ctx context.Context, t *testing.T, db DB, f retainedFixture) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	suffix := uuid.New().String()
	require.NoError(t, db.QueryRow(ctx, `
		INSERT INTO investment_transactions (user_id, account_id, plaid_investment_transaction_id, type, name, amount, date)
		VALUES ($1, $2, $3, 'buy', 'Test Buy', 100.00, CURRENT_DATE)
		RETURNING id
	`, f.UserID, f.AccountID, "plaid-inv-txn-"+suffix).Scan(&id), "seed investment_transaction")
	return id
}

func seedCreditLiability(ctx context.Context, t *testing.T, db DB, f retainedFixture) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	require.NoError(t, db.QueryRow(ctx, `
		INSERT INTO credit_liabilities (user_id, account_id) VALUES ($1, $2) RETURNING id
	`, f.UserID, f.AccountID).Scan(&id), "seed credit_liability")
	return id
}

// seedSavingsGoal inserts one savings_goals row pointing at the fixture account.
func seedSavingsGoal(ctx context.Context, t *testing.T, db DB, f retainedFixture) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	require.NoError(t, db.QueryRow(ctx, `
		INSERT INTO savings_goals (user_id, name, target_amount, account_id, progress_basis)
		VALUES ($1, 'Emergency Fund', 1000.00, $2, 'manual') RETURNING id
	`, f.UserID, f.AccountID).Scan(&id), "seed savings_goal")
	return id
}

func countRows(ctx context.Context, t *testing.T, db DB, table, whereCol string, id uuid.UUID) int {
	t.Helper()
	var count int
	require.NoError(t, db.QueryRow(ctx,
		"SELECT COUNT(*) FROM "+table+" WHERE "+whereCol+" = $1", id, //nolint:gosec // table/col are fixed test-internal literals, never user input
	).Scan(&count))
	return count
}

// TestAccountRepository_PurgeAccount_Integration_RemovesEverythingDerived proves PurgeAccount
// transactionally destroys the account row, its balance_snapshots (explicit — SET NULL since
// migration 000150, so a plain account delete would only orphan them), and — via unchanged
// ON DELETE CASCADE — its holdings, liabilities, AND investment_transactions.
//
// Fix round 1, CRITICAL finding: investment_transactions.account_id is ON DELETE CASCADE
// (migration 000051) — a buy/sell/dividend has no independent existence apart from the account,
// unlike a banking transaction. The purge dialog and API descriptions previously said
// "transactions are not destroyed" without this carve-out, which was false for any investment
// account. Asserted here BEFORE and AFTER the purge (not just after) so this assertion cannot
// pass vacuously if the seed helper silently failed to insert the row.
func TestAccountRepository_PurgeAccount_Integration_RemovesEverythingDerived(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	accountRepo := NewAccountRepository(pool, newTestFieldEncryptor(t))

	f := seedRetainedFixture(ctx, t, pool, "purge-all-"+uuid.New().String())
	// The fixture's linked_item has no cleanup_scheduled_at; once PurgeAccount removes its
	// only account, it becomes an unscheduled zero-account cleanup CANDIDATE that
	// GetItemsForCleanupPage's sweep (a different suite, same real table) would otherwise pick
	// up on a later run in this package. Delete the fixture user — cascades everything.
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, f.UserID) })
	snapshotID := seedBalanceSnapshot(ctx, t, pool, f)
	holdingID := seedHolding(ctx, t, pool, f)
	liabilityID := seedCreditLiability(ctx, t, pool, f)
	invTxnID := seedInvestmentTransaction(ctx, t, pool, f)
	require.Equal(t, 1, countRows(ctx, t, pool, "investment_transactions", "id", invTxnID),
		"sanity: the investment transaction must exist before the purge, or the assertion below is vacuous")

	// PurgeAccount refuses a live account (fix round 1, IMPORTANT 4) — disconnect first.
	disconnect(ctx, t, pool, f)

	err := accountRepo.PurgeAccount(ctx, f.AccountID.String(), f.UserID.String(), nil)
	require.NoError(t, err)

	assert.Equal(t, 0, countRows(ctx, t, pool, "accounts", "id", f.AccountID), "account row must be gone")
	assert.Equal(t, 0, countRows(ctx, t, pool, "balance_snapshots", "id", snapshotID), "balance_snapshots must be gone")
	assert.Equal(t, 0, countRows(ctx, t, pool, "holdings", "id", holdingID), "holdings must be gone")
	assert.Equal(t, 0, countRows(ctx, t, pool, "credit_liabilities", "id", liabilityID), "liabilities must be gone")
	assert.Equal(t, 0, countRows(ctx, t, pool, "investment_transactions", "id", invTxnID),
		"investment transactions must be destroyed — they have no independent existence apart from the account")
}

// TestAccountRepository_PurgeAccount_Integration_RefusesLiveAccount proves the fix round 1,
// IMPORTANT 4 guard: PurgeAccount refuses a still-'active' account rather than destroying its
// history out from under an actively-syncing Plaid item (whose next sync would recreate the row
// via MatchOrCreate, silently undoing the "permanent" deletion). Mirrors
// ErrLinkedItemNotDisconnected's institution-level guard.
func TestAccountRepository_PurgeAccount_Integration_RefusesLiveAccount(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	accountRepo := NewAccountRepository(pool, newTestFieldEncryptor(t))

	f := seedRetainedFixture(ctx, t, pool, "purge-live-"+uuid.New().String())
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, f.UserID) })

	err := accountRepo.PurgeAccount(ctx, f.AccountID.String(), f.UserID.String(), nil)
	require.ErrorIs(t, err, ErrAccountNotDisconnected)
	assert.Equal(t, 1, countRows(ctx, t, pool, "accounts", "id", f.AccountID),
		"the refused purge must roll back — the account must still exist")
}

// TestAccountRepository_PurgeAccount_Integration_TransactionsSurvive proves purging an account does
// NOT delete the transactions the user categorized and budgeted against — migration 000035's
// ON DELETE SET NULL orphans them instead, and the unified feed's NULL-account_id leg keeps them
// visible (transaction_repository_test.go / transaction_retained_visibility_integration_test.go).
func TestAccountRepository_PurgeAccount_Integration_TransactionsSurvive(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	accountRepo := NewAccountRepository(pool, newTestFieldEncryptor(t))

	f := seedRetainedFixture(ctx, t, pool, "purge-txn-"+uuid.New().String())
	// The fixture's linked_item has no cleanup_scheduled_at; once PurgeAccount removes its
	// only account, it becomes an unscheduled zero-account cleanup CANDIDATE that
	// GetItemsForCleanupPage's sweep (a different suite, same real table) would otherwise pick
	// up on a later run in this package. Delete the fixture user — cascades everything.
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, f.UserID) })
	txnID := seedBankingTxn(ctx, t, pool, f, time.Now(), "Coffee Shop")

	disconnect(ctx, t, pool, f)
	require.NoError(t, accountRepo.PurgeAccount(ctx, f.AccountID.String(), f.UserID.String(), nil))

	var accountIDAfter *uuid.UUID
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT account_id FROM transactions WHERE id = $1`, txnID,
	).Scan(&accountIDAfter))
	assert.Nil(t, accountIDAfter, "transaction must survive with account_id set NULL, not deleted")
}

// TestAccountRepository_PurgeAccount_Integration_RefusesWhileSavingsGoalReferencesAccount proves
// migration 000150's RESTRICT constraint on fk_savings_goals_account_id blocks the purge, that the
// whole transaction rolls back (the account and its snapshot both survive untouched), and that the
// raw pgconn.PgError is translated into ErrPurgeBlockedBySavingsGoal rather than leaking out.
func TestAccountRepository_PurgeAccount_Integration_RefusesWhileSavingsGoalReferencesAccount(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	accountRepo := NewAccountRepository(pool, newTestFieldEncryptor(t))

	f := seedRetainedFixture(ctx, t, pool, "purge-goal-"+uuid.New().String())
	// The fixture's linked_item has no cleanup_scheduled_at; once PurgeAccount removes its
	// only account, it becomes an unscheduled zero-account cleanup CANDIDATE that
	// GetItemsForCleanupPage's sweep (a different suite, same real table) would otherwise pick
	// up on a later run in this package. Delete the fixture user — cascades everything.
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, f.UserID) })
	snapshotID := seedBalanceSnapshot(ctx, t, pool, f)
	seedSavingsGoal(ctx, t, pool, f)
	disconnect(ctx, t, pool, f)

	err := accountRepo.PurgeAccount(ctx, f.AccountID.String(), f.UserID.String(), nil)
	require.ErrorIs(t, err, ErrPurgeBlockedBySavingsGoal)

	assert.Equal(t, 1, countRows(ctx, t, pool, "accounts", "id", f.AccountID),
		"the refused purge must roll back — the account must still exist")
	assert.Equal(t, 1, countRows(ctx, t, pool, "balance_snapshots", "id", snapshotID),
		"the refused purge must roll back — the snapshot must still exist")
}

// TestAccountRepository_PurgeAccount_Integration_WritesAuditRowInSameTransaction proves the
// account.history_purged entry commits in the SAME transaction as the destruction it describes,
// via AuditLogRepository.LogInTx.
func TestAccountRepository_PurgeAccount_Integration_WritesAuditRowInSameTransaction(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	accountRepo := NewAccountRepository(pool, newTestFieldEncryptor(t))
	auditLog := NewAuditLogRepository(pool)

	f := seedRetainedFixture(ctx, t, pool, "purge-audit-"+uuid.New().String())
	// The fixture's linked_item has no cleanup_scheduled_at; once PurgeAccount removes its
	// only account, it becomes an unscheduled zero-account cleanup CANDIDATE that
	// GetItemsForCleanupPage's sweep (a different suite, same real table) would otherwise pick
	// up on a later run in this package. Delete the fixture user — cascades everything.
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, f.UserID) })
	disconnect(ctx, t, pool, f)

	require.NoError(t, accountRepo.PurgeAccount(ctx, f.AccountID.String(), f.UserID.String(), auditLog))

	var count int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM audit_logs WHERE action = 'account.history_purged' AND resource_type = 'account' AND resource_id = $1`,
		f.AccountID.String(),
	).Scan(&count))
	assert.Equal(t, 1, count, "an account.history_purged audit entry must be written")
}

// TestAccountRepository_PurgeAccount_Integration_ClearsDanglingReplacedByReference is the fix
// round 1 IMPORTANT 2 regression test at the account level (mirrors the linked-item-level test in
// linked_item_repository_purge_integration_test.go). accounts.replaced_by_id has NO explicit
// ON DELETE action (defaults to NO ACTION, migration 000033) — if an OLDER account was replaced
// by the one being purged, the purge must not fail with a dangling foreign-key violation.
func TestAccountRepository_PurgeAccount_Integration_ClearsDanglingReplacedByReference(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	accountRepo := NewAccountRepository(pool, newTestFieldEncryptor(t))

	f := seedRetainedFixture(ctx, t, pool, "purge-replby-"+uuid.New().String())
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, f.UserID) })

	var oldAccountID uuid.UUID
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO accounts (linked_item_id, user_id, plaid_account_id, name, type, status, replaced_at, replaced_by_id)
		VALUES ($1, $2, $3, 'Old Checking', 'depository', 'replaced', NOW(), $4)
		RETURNING id
	`, f.LinkedItemID, f.UserID, "plaid-acct-old-"+uuid.New().String(), f.AccountID).Scan(&oldAccountID))

	disconnect(ctx, t, pool, f)

	err := accountRepo.PurgeAccount(ctx, f.AccountID.String(), f.UserID.String(), nil)
	require.NoError(t, err, "purging an account another account's replaced_by_id points at must not fail with a dangling foreign-key violation")

	var replacedBy *string
	require.NoError(t, pool.QueryRow(ctx, `SELECT replaced_by_id FROM accounts WHERE id = $1`, oldAccountID).Scan(&replacedBy))
	assert.Nil(t, replacedBy, "the older account's dangling replaced_by_id must be nulled, not left pointing at a deleted row")
}

// TestAccountRepository_PurgeAccount_Integration_MultiMemberChain_TransactionsStayVisibleUnderSurvivor
// proves the Task 6 "multi-member chain" decision (documented in the task report): a purge is
// ALLOWED on an account that has a surviving chain member, rather than refused, because
// MatchOrCreate matches on institution+mask+name+type against user_accounts independently of any
// single account row — a surviving chain member keeps the SAME user_account_id, and the unified
// feed's chain-expansion leg (transaction_repository.go's GetByUserIDKeysetUnified) admits any
// transaction whose user_account_id has a live 'active' chain member, with no dependency on the
// purged account's row still existing. Uses a rollback tx (not the shared pool) so this test needs
// no manual fixture cleanup.
func TestAccountRepository_PurgeAccount_Integration_MultiMemberChain_TransactionsStayVisibleUnderSurvivor(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	f := seedRetainedFixture(ctx, t, tx, "purge-chain-"+uuid.New().String())

	// The surviving chain member: same user_account_id, same institution, a different account row
	// — exactly what a re-link produces. It stays 'active' after f.AccountID is purged.
	survivorID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, status, user_account_id)
		VALUES ($1, $2, $3, $4, 'Old Bank Checking (relinked)', 'depository', 'active', $5)
	`, survivorID, f.LinkedItemID, f.UserID, "survivor-"+survivorID.String(), f.UserAccountID)
	require.NoError(t, err, "seed surviving chain member")

	seedBankingTxn(ctx, t, tx, f, time.Now().UTC().AddDate(0, 0, -3), "Coffee Before Purge")
	disconnect(ctx, t, tx, f)

	accountRepo := NewAccountRepository(tx, newTestFieldEncryptor(t))
	require.NoError(t, accountRepo.PurgeAccount(ctx, f.AccountID.String(), f.UserID.String(), nil),
		"purge must succeed on a multi-member chain, not refuse")

	txnRepo := NewTransactionRepository(tx, nil)
	items, _, _, err := txnRepo.GetByUserIDKeysetUnified(ctx, f.UserID.String(), domain.TransactionFilters{}, nil, 50)
	require.NoError(t, err)
	assert.Contains(t, feedNames(items), "Coffee Before Purge",
		"purging one chain member must not hide transactions the surviving active member's chain key still covers")
}

// TestAccountRepository_PurgeAccount_Integration_TombstonedSavingsGoalDoesNotBlock is the COMPOSED
// case — the one twenty adversarial reviews did not construct, because each seeded a fixture built
// for the mechanism under test and this defect lives BETWEEN two mechanisms.
//
// THE DEAD VERB. Migration 000150 made savings_goals.account_id RESTRICT specifically so PurgeAccount
// would refuse while a goal still pointed at the account, and the 409 tells the user to "delete the
// goal first, then try again". Task 8 then made goal deletion a TOMBSTONE, which leaves account_id
// set. So the user does exactly what the error asks, the FK still fires, and the same 409 comes
// back — while the goal is invisible to every read and deleting it again returns
// ErrSavingsGoalNotFound. Explicit destruction, the ADR's one sanctioned way to destroy data, was
// permanently unreachable for any account that had ever had a savings goal.
//
// THE RULING TAKEN HERE. A tombstoned goal does NOT block the purge. The user has by then performed
// three separate explicit acts — disconnect, delete the goal, confirm the purge — and the second of
// those is exactly PurgeGoal's own precondition (ErrSavingsGoalNotDeleted). A LIVE goal still
// blocks, which is what makes the error text true rather than a dead end:
// TestAccountRepository_PurgeAccount_Integration_RefusesWhileSavingsGoalReferencesAccount is
// unchanged and still passes.
//
// WHAT IT COSTS, asserted rather than left to be discovered: the tombstoned goal's hand-logged
// contributions go with it. That is the same destruction POST /savings-goals/{id}/purge would have
// performed on the same already-tombstoned goal, so no data becomes reachable-for-destruction that
// was not already.
func TestAccountRepository_PurgeAccount_Integration_TombstonedSavingsGoalDoesNotBlock(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	accountRepo := NewAccountRepository(pool, newTestFieldEncryptor(t))
	goalRepo := NewSavingsGoalRepository(pool)

	f := seedRetainedFixture(ctx, t, pool, "purge-tombstoned-goal-"+uuid.New().String())
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, f.UserID) })

	goalID := seedSavingsGoal(ctx, t, pool, f)
	contributionID := uuid.New()
	_, err := pool.Exec(ctx, `
		INSERT INTO savings_goal_contributions
			(id, savings_goal_id, amount, source, status, managed_by_reconciliation, confirmed_by_user, occurred_on)
		VALUES ($1, $2, 250.00, 'manual', 'confirmed', false, true, CURRENT_DATE)
	`, contributionID, goalID)
	require.NoError(t, err, "seed savings_goal_contribution")

	disconnect(ctx, t, pool, f)

	// Step 1: the user does exactly what the 409 told them to do.
	require.NoError(t, goalRepo.Delete(ctx, goalID.String(), f.UserID.String(), nil),
		"deleting the goal must succeed — this is the step the error text asks for")

	// The tombstone is what makes this the composed case: the row is still there, still pointing
	// at the account, and invisible to the user.
	var deletedAt *time.Time
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT deleted_at FROM savings_goals WHERE id = $1`, goalID).Scan(&deletedAt))
	require.NotNil(t, deletedAt, "the goal must be TOMBSTONED, not deleted — otherwise this test "+
		"is not exercising the defect at all")

	// Step 2: "then try again". Before this fix, this line returned ErrPurgeBlockedBySavingsGoal
	// forever and there was no third step the user could take.
	require.NoError(t, accountRepo.PurgeAccount(ctx, f.AccountID.String(), f.UserID.String(), nil),
		"a goal the user has already deleted must not permanently block explicit destruction")

	assert.Equal(t, 0, countRows(ctx, t, pool, "accounts", "id", f.AccountID),
		"the account must actually be gone")
	assert.Equal(t, 0, countRows(ctx, t, pool, "savings_goals", "id", goalID),
		"the tombstoned goal goes with the account it pointed at")
	assert.Equal(t, 0, countRows(ctx, t, pool, "savings_goal_contributions", "id", contributionID),
		"and so do its contributions — the cost of the ruling, stated rather than discovered")
}
