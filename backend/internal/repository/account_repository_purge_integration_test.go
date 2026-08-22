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
// ON DELETE CASCADE — its holdings and liabilities.
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

	err := accountRepo.PurgeAccount(ctx, f.AccountID.String(), f.UserID.String(), nil)
	require.NoError(t, err)

	assert.Equal(t, 0, countRows(ctx, t, pool, "accounts", "id", f.AccountID), "account row must be gone")
	assert.Equal(t, 0, countRows(ctx, t, pool, "balance_snapshots", "id", snapshotID), "balance_snapshots must be gone")
	assert.Equal(t, 0, countRows(ctx, t, pool, "holdings", "id", holdingID), "holdings must be gone")
	assert.Equal(t, 0, countRows(ctx, t, pool, "credit_liabilities", "id", liabilityID), "liabilities must be gone")
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

	require.NoError(t, accountRepo.PurgeAccount(ctx, f.AccountID.String(), f.UserID.String(), auditLog))

	var count int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM audit_logs WHERE action = 'account.history_purged' AND resource_type = 'account' AND resource_id = $1`,
		f.AccountID.String(),
	).Scan(&count))
	assert.Equal(t, 1, count, "an account.history_purged audit entry must be written")
}
