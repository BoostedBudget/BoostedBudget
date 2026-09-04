//go:build integration

package repository

// Issue #1569 round 17 task 11 — the mutant-killing half of ADR control 2 for the three purges.
//
// TestAccountRepository_PurgeAccount_Integration_WritesAuditRowInSameTransaction and its
// linked-item and savings-goal equivalents count an audit_logs row AFTER the call returns. That is
// satisfied just as well by a second, independent write on another connection, so swapping
// LogInTx for Log leaves all of them green. The failing-sink tests do not close the gap either: a
// sink that errors aborts the method whichever method was called.
//
// This file refuses the swap directly, the way
// TestDeleteContribution_AuditRowSharesTheDeletesTransaction_Integration does for contributions:
// the audit MUST go through the destroying transaction's own handle, and from inside that handle
// the destroyed row must already be invisible — something no other connection could yet observe.

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// purgeAuditSpy records WHICH audit method the purge used and, from inside LogInTx, whether the
// caller's own transaction can already see the destroyed row as gone.
//
// countSQL must be a single-column `SELECT count(*) ... WHERE <pk> = $1` against the table the
// purge destroys; rowID is its parameter.
type purgeAuditSpy struct {
	countSQL string
	rowID    string

	calledLog     bool
	calledLogInTx bool
	rowGoneInTx   bool
	scanErr       error
}

func (s *purgeAuditSpy) Log(context.Context, string, string, string) error {
	s.calledLog = true
	return nil
}

func (s *purgeAuditSpy) LogInTx(ctx context.Context, tx pgx.Tx, _, _, _ string) error {
	s.calledLogInTx = true
	var n int
	if err := tx.QueryRow(ctx, s.countSQL, s.rowID).Scan(&n); err != nil {
		s.scanErr = err
		return nil
	}
	s.rowGoneInTx = n == 0
	return nil
}

// assertSharedTheDestroyingTransaction is the three assertions every caller here makes.
func (s *purgeAuditSpy) assertSharedTheDestroyingTransaction(t *testing.T) {
	t.Helper()
	require.NoError(t, s.scanErr)
	assert.True(t, s.calledLogInTx, "the audit entry must be written with LogInTx")
	assert.False(t, s.calledLog, "Log() writes on a SEPARATE connection — that is not control 2")
	assert.True(t, s.rowGoneInTx,
		"the transaction handed to LogInTx must be the one that performed the destruction: from "+
			"inside it the row is already gone, which no other connection could yet observe")
}

// TestAccountRepository_PurgeAccount_Integration_AuditRowSharesThePurgesTransaction is the
// account-level mutant killer described in this file's header.
func TestAccountRepository_PurgeAccount_Integration_AuditRowSharesThePurgesTransaction(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	accountRepo := NewAccountRepository(pool, newTestFieldEncryptor(t))

	f := seedRetainedFixture(ctx, t, pool, "purge-audit-sametx-"+uuid.New().String())
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, f.UserID) })
	disconnect(ctx, t, pool, f)

	spy := &purgeAuditSpy{
		countSQL: `SELECT count(*) FROM accounts WHERE id = $1`,
		rowID:    f.AccountID.String(),
	}
	require.NoError(t, accountRepo.PurgeAccount(ctx, f.AccountID.String(), f.UserID.String(), spy))

	spy.assertSharedTheDestroyingTransaction(t)
}

// TestLinkedItemRepository_PurgeLinkedItem_Integration_AuditRowSharesThePurgesTransaction is the
// institution-level equal.
func TestLinkedItemRepository_PurgeLinkedItem_Integration_AuditRowSharesThePurgesTransaction(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	repo := NewLinkedItemRepository(pool, newTestFieldEncryptor(t))

	f := seedRetainedFixture(ctx, t, pool, "purge-item-audit-sametx-"+uuid.New().String())
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, f.UserID) })

	require.NoError(t, repo.MarkPlaidRemoved(ctx, []string{f.LinkedItemID.String()}))
	disconnectItemAccountsForPurge(ctx, t, pool, f.LinkedItemID)

	spy := &purgeAuditSpy{
		countSQL: `SELECT count(*) FROM linked_items WHERE id = $1`,
		rowID:    f.LinkedItemID.String(),
	}
	require.NoError(t, repo.PurgeLinkedItem(ctx, f.LinkedItemID.String(), f.UserID.String(), spy))

	spy.assertSharedTheDestroyingTransaction(t)
}

// TestSavingsGoalRepository_PurgeGoal_Integration_AuditRowSharesThePurgesTransaction is the
// savings-goal equal.
func TestSavingsGoalRepository_PurgeGoal_Integration_AuditRowSharesThePurgesTransaction(t *testing.T) {
	f := seedConfigTombstoneFixture(t)
	ctx := context.Background()

	require.NoError(t, f.goalRepo.Delete(ctx, f.goalID, f.userID, f.auditRepo))

	spy := &purgeAuditSpy{
		countSQL: `SELECT count(*) FROM savings_goals WHERE id = $1`,
		rowID:    f.goalID,
	}
	require.NoError(t, f.goalRepo.PurgeGoal(ctx, f.goalID, f.userID, spy))

	spy.assertSharedTheDestroyingTransaction(t)
}
