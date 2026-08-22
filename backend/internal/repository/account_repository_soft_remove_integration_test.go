//go:build integration

package repository

// Integration tests proving Issue #1569's core property: removing an account (or disconnecting its
// institution) no longer destroys its financial history.
//
// WHY THESE MUST RUN AGAINST REAL POSTGRES AND NOT pgxmock. pgxmock cannot prove a row SURVIVES —
// it only replays a canned RowsAffected for whatever query text it matched. Before this issue,
// AccountRepository.Delete was a real `DELETE FROM accounts`, which — prior to migration 000150 —
// CASCADE-deleted balance_snapshots; the only way to prove the NEW code path (UpdateStatus /
// MarkRemovedByLinkedItemID) actually leaves the account row and its balance_snapshots in place is
// to seed both against a real schema and query them back afterward.
//
// Task 2 brief, required assertion 5: "DeleteAccount leaves the account row present with
// status='removed' and removed_at set, and leaves its balance_snapshots intact. Assert the
// snapshots survive — that is the whole point."
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run TestAccountRepository_.*Removed -tags integration -v -p 1

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// TestAccountRepository_UpdateStatus_Integration_RemovedPreservesBalanceSnapshots proves
// AccountRepository.UpdateStatus(..., AccountStatusRemoved, nil) — what accountService.DeleteAccount
// now calls instead of Delete — leaves the account row present (status='removed', removed_at set)
// and does not touch its balance_snapshots.
func TestAccountRepository_UpdateStatus_Integration_RemovedPreservesBalanceSnapshots(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	ctx := context.Background()
	profileRepo := NewProfileRepository(pool, enc)
	linkedItemRepo := NewLinkedItemRepository(pool, enc)
	accountRepo := NewAccountRepository(pool, enc)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	userID := createMarkDisconnectedFixtureUser(t, ctx, pool, profileRepo, suffix)
	item := createMarkDisconnectedFixtureItem(t, ctx, linkedItemRepo, userID, suffix)

	var accountID string
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO accounts (linked_item_id, user_id, plaid_account_id, name, type)
		VALUES ($1, $2, $3, 'Test Checking', 'depository')
		RETURNING id`,
		item.ID, userID, "plaid-acct-"+suffix).Scan(&accountID))

	var snapshotID string
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO balance_snapshots (account_id, user_id, current_balance, currency_code, snapshot_at, source)
		VALUES ($1, $2, 1234.56, 'USD', NOW(), 'manual')
		RETURNING id`,
		accountID, userID).Scan(&snapshotID))

	require.NoError(t, accountRepo.UpdateStatus(ctx, accountID, userID, domain.AccountStatusRemoved, nil))

	var status string
	var removedAt *time.Time
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT status, removed_at FROM accounts WHERE id = $1`, accountID,
	).Scan(&status, &removedAt))
	assert.Equal(t, "removed", status, "the account row must survive, transitioned to 'removed'")
	require.NotNil(t, removedAt, "removed_at must be stamped — required by accounts_removed_at_consistency")

	var snapshotStillThere int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM balance_snapshots WHERE id = $1 AND account_id = $2`, snapshotID, accountID,
	).Scan(&snapshotStillThere))
	assert.Equal(t, 1, snapshotStillThere,
		"the balance_snapshot must survive untouched — this is the whole point of the #1569 soft-remove")
}

// TestAccountRepository_MarkRemovedByLinkedItemID_Integration_PreservesBalanceSnapshots proves the
// institution-level disconnect path (accountService.DisconnectInstitution's step 4c): every ACTIVE
// account under a linked item transitions to 'removed' with removed_at set, and its
// balance_snapshots survive.
func TestAccountRepository_MarkRemovedByLinkedItemID_Integration_PreservesBalanceSnapshots(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	ctx := context.Background()
	profileRepo := NewProfileRepository(pool, enc)
	linkedItemRepo := NewLinkedItemRepository(pool, enc)
	accountRepo := NewAccountRepository(pool, enc)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	userID := createMarkDisconnectedFixtureUser(t, ctx, pool, profileRepo, suffix)
	item := createMarkDisconnectedFixtureItem(t, ctx, linkedItemRepo, userID, suffix)

	var checkingID, savingsID string
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO accounts (linked_item_id, user_id, plaid_account_id, name, type)
		VALUES ($1, $2, $3, 'Test Checking', 'depository') RETURNING id`,
		item.ID, userID, "plaid-checking-"+suffix).Scan(&checkingID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO accounts (linked_item_id, user_id, plaid_account_id, name, type)
		VALUES ($1, $2, $3, 'Test Savings', 'depository') RETURNING id`,
		item.ID, userID, "plaid-savings-"+suffix).Scan(&savingsID))

	var snapshotID string
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO balance_snapshots (account_id, user_id, current_balance, currency_code, snapshot_at, source)
		VALUES ($1, $2, 500.00, 'USD', NOW(), 'manual') RETURNING id`,
		checkingID, userID).Scan(&snapshotID))

	require.NoError(t, accountRepo.MarkRemovedByLinkedItemID(ctx, item.ID))

	rows, err := pool.Query(ctx, `SELECT id, status, removed_at FROM accounts WHERE linked_item_id = $1 ORDER BY id`, item.ID)
	require.NoError(t, err)
	defer rows.Close()

	count := 0
	for rows.Next() {
		var id, status string
		var removedAt *time.Time
		require.NoError(t, rows.Scan(&id, &status, &removedAt))
		assert.Equal(t, "removed", status, "every active account under the item must transition to 'removed'")
		assert.NotNil(t, removedAt, "removed_at must be stamped on every transitioned account")
		count++
	}
	require.NoError(t, rows.Err())
	assert.Equal(t, 2, count, "both accounts under the item must have been transitioned")

	var snapshotStillThere int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM balance_snapshots WHERE id = $1 AND account_id = $2`, snapshotID, checkingID,
	).Scan(&snapshotStillThere))
	assert.Equal(t, 1, snapshotStillThere, "the balance_snapshot must survive the institution-level disconnect")
}
