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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

)

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

	// Disconnect newItem so the plaid_removed_at guard doesn't block the purge under test.
	require.NoError(t, repo.MarkPlaidRemoved(ctx, []string{newItem.ID}))

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
