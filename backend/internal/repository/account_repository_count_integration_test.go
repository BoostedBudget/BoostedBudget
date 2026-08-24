//go:build integration

package repository

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// TestAccountRepository_CountByUserID_Integration_CountsOneRowPerRealAccount is #1569 residual
// E10, and it is a live-Postgres test on purpose: the two existing CountByUserID unit tests are
// pgxmock ones, which assert the query TEXT and then hand back a number the test itself chose, so
// they cannot see a wrong WHERE clause at all.
//
// The query excluded 'expired' (#1010) and, since task 6, 'removed' (#1569) — but still counted
// 'replaced'. A 'replaced' row is the pre-re-link copy of an account whose successor is the
// active row beside it, sharing one user_account_id: counting both reports two accounts where the
// user has one. That is wrong for the only purpose this method has ever been given (its unit test
// is named "counting accounts for subscription limit checks"), and it is inconsistent with
// CountActiveByLinkedItemID, which counts 'active' only.
//
// NOTE the method still has no production caller — see its doc comment. This is hygiene against a
// future one, and the test exists so the fix cannot silently rot back.
func TestAccountRepository_CountByUserID_Integration_CountsOneRowPerRealAccount(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	repo := NewAccountRepository(pool, newTestFieldEncryptor(t))

	f := seedRetainedFixture(ctx, t, pool, "count-by-user-"+uuid.New().String())
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, f.UserID) })

	// One re-link chain: the fixture's account is the live successor; add the superseded copy
	// that shares its user_account_id, exactly as accountService's re-link branch leaves things.
	oldID := uuid.New()
	_, err := pool.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type,
		                      status, replaced_at, replaced_by_id, user_account_id)
		VALUES ($1, $2, $3, $4, 'Old Bank Checking (pre-relink)', 'depository',
		        'replaced', NOW(), $5, $6)`,
		oldID, f.LinkedItemID, f.UserID, "count-old-"+oldID.String(), f.AccountID, f.UserAccountID)
	require.NoError(t, err)

	// And one of each status the count already agreed to ignore, so this test also holds those
	// two exclusions to account rather than trusting the pgxmock pair.
	for _, s := range []struct{ status, extra string }{
		{"expired", ", expired_at = NOW()"},
		{"removed", ", removed_at = NOW()"},
	} {
		id := uuid.New()
		_, err := pool.Exec(ctx, `
			INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, status)
			VALUES ($1, $2, $3, $4, 'Ignored', 'depository', 'active')`,
			id, f.LinkedItemID, f.UserID, "count-"+s.status+"-"+id.String())
		require.NoError(t, err)
		_, err = pool.Exec(ctx, `UPDATE accounts SET status = $2`+s.extra+` WHERE id = $1`, id, s.status)
		require.NoError(t, err)
	}

	count, err := repo.CountByUserID(ctx, f.UserID.String())
	require.NoError(t, err)
	assert.Equal(t, 1, count,
		"four rows, one real account: the 'replaced' pre-re-link copy is the same account as its "+
			"active successor and must not be counted twice")
}

// TestAccountRepository_GetByUserAccountIDAndUser_Integration_ReportsStatus is #1569 residual E2's
// one remaining reachable case.
//
// The read sorts on `CASE WHEN status = 'active' THEN 0 ELSE 1 END` — it PREFERS a live chain
// member — and then did not return status at all. So for a chain whose members are all terminal
// (a disconnected account whose identity survives, which is the state #1569 creates on purpose)
// the caller got a row back with Status = "", a value no accounts row can hold, and no way to
// tell a live account from a disconnected one. The method's own doc comment claimed it "returns
// the current live account".
func TestAccountRepository_GetByUserAccountIDAndUser_Integration_ReportsStatus(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	repo := NewAccountRepository(pool, newTestFieldEncryptor(t))

	f := seedRetainedFixture(ctx, t, pool, "by-ua-status-"+uuid.New().String())
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, f.UserID) })

	live, err := repo.GetByUserAccountIDAndUser(ctx, f.UserAccountID.String(), f.UserID.String())
	require.NoError(t, err)
	assert.Equal(t, domain.AccountStatusActive, live.Status, "the live chain member must say so")
	assert.Nil(t, live.RemovedAt)

	require.NoError(t, repo.UpdateStatus(ctx, f.AccountID.String(), f.UserID.String(),
		domain.AccountStatusRemoved, nil))

	terminal, err := repo.GetByUserAccountIDAndUser(ctx, f.UserAccountID.String(), f.UserID.String())
	require.NoError(t, err, "the identity survives the disconnect — this must still resolve")
	assert.Equal(t, domain.AccountStatusRemoved, terminal.Status,
		"a chain with no live member must report the terminal status, not an empty one")
	assert.NotNil(t, terminal.RemovedAt, "and the date the history was frozen at")
}
