//go:build integration

package repository

// Integration test for LinkedItemRepository.DeleteByUser's accounts guard (Issue #1569).
//
// WHY THIS MUST RUN AGAINST REAL POSTGRES AND NOT pgxmock. The whole point of the guard is a SQL
// predicate — `AND NOT EXISTS (SELECT 1 FROM accounts WHERE linked_item_id = $1)` — and pgxmock
// matches query TEXT, not semantics: it would happily "prove" the guard blocks a delete by
// returning whatever RowsAffected the test tells it to, regardless of whether the predicate is
// actually present or correct in the real WHERE clause. Proving the guard requires seeding a real
// account row and observing that the real database refuses the delete.
//
// Task 2 brief, required assertion 4: "DeleteByUser is a no-op while accounts still reference the
// item (integration, live DB)."
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run TestLinkedItemRepository_DeleteByUser_Integration -tags integration -v -p 1

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLinkedItemRepository_DeleteByUser_Integration_BlockedWhileAccountsExist proves the guard
// added alongside DisconnectInstitution's #1569 rewrite: DeleteByUser must not delete a linked_items
// row while any account still references it via linked_item_id, because accounts.linked_item_id is
// ON DELETE CASCADE — an unguarded delete would cascade away the accounts and, through them, their
// balance_snapshots/holdings/investment transactions/liability rows.
//
// Falsifiability: dropping the `AND NOT EXISTS (...)` clause from DeleteByUser's SQL (reverting to
// the pre-#1569 query) makes this test fail — the row DOES get deleted while the account (and its
// balance_snapshot) is CASCADE-removed with it. Verified by temporarily reverting the query.
func TestLinkedItemRepository_DeleteByUser_Integration_BlockedWhileAccountsExist(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	ctx := context.Background()
	profileRepo := NewProfileRepository(pool, enc)
	repo := NewLinkedItemRepository(pool, enc)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	userID := createMarkDisconnectedFixtureUser(t, ctx, pool, profileRepo, suffix)
	item := createMarkDisconnectedFixtureItem(t, ctx, repo, userID, suffix)

	var accountID string
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO accounts (linked_item_id, user_id, plaid_account_id, name, type)
		VALUES ($1, $2, $3, 'Test Checking', 'depository')
		RETURNING id`,
		item.ID, userID, "plaid-acct-"+suffix).Scan(&accountID))

	var snapshotID string
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO balance_snapshots (account_id, user_id, current_balance, currency_code, snapshot_at, source)
		VALUES ($1, $2, 1000.00, 'USD', NOW(), 'manual')
		RETURNING id`,
		accountID, userID).Scan(&snapshotID))

	// The guard must refuse: the account still references the item.
	err := repo.DeleteByUser(ctx, item.ID, userID)
	require.ErrorIs(t, err, ErrLinkedItemNotFound,
		"DeleteByUser must be a no-op (reporting the same sentinel as not-found/not-owned) while an account still references the item")

	// The load-bearing proof: the row, the account, AND its balance_snapshot all survived.
	var stillThere int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM linked_items WHERE id = $1`, item.ID).Scan(&stillThere))
	assert.Equal(t, 1, stillThere, "the linked_items row must survive the blocked delete")

	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM accounts WHERE id = $1`, accountID).Scan(&stillThere))
	assert.Equal(t, 1, stillThere, "the account must survive — this is what the guard exists to protect")

	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM balance_snapshots WHERE id = $1`, snapshotID).Scan(&stillThere))
	assert.Equal(t, 1, stillThere, "the balance_snapshot must survive — a CASCADE through accounts would have destroyed it")
}

// TestLinkedItemRepository_DeleteByUser_Integration_SucceedsOnceAccountsAreGone proves the guard is
// not a permanent lock: once no account references the item, DeleteByUser proceeds exactly as
// before.
func TestLinkedItemRepository_DeleteByUser_Integration_SucceedsOnceAccountsAreGone(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	ctx := context.Background()
	profileRepo := NewProfileRepository(pool, enc)
	repo := NewLinkedItemRepository(pool, enc)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	userID := createMarkDisconnectedFixtureUser(t, ctx, pool, profileRepo, suffix)
	item := createMarkDisconnectedFixtureItem(t, ctx, repo, userID, suffix)

	// No accounts reference this item at all.
	require.NoError(t, repo.DeleteByUser(ctx, item.ID, userID))

	var stillThere int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM linked_items WHERE id = $1`, item.ID).Scan(&stillThere))
	assert.Equal(t, 0, stillThere, "with no accounts referencing it, DeleteByUser must actually delete the row")
}
