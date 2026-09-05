//go:build integration

package repository

// #1569 T12 gap 2 — the deleted-goals listing.
//
// Task 8 made deleting a savings goal a TOMBSTONE and added POST /savings-goals/{id}/purge,
// gated on the goal already being soft-deleted. Both shipped. Neither is reachable: every read
// in this repository carries `deleted_at IS NULL`, so once a goal is deleted there is no query
// that can find it again, and therefore no way for a user to satisfy the purge's precondition or
// to reclaim the row. Retention that nobody can see or act on is the same dead end as deletion,
// arriving politely.
//
// GetDeletedByUserID is the one read allowed to look the other way, and this file is the
// behavioural statement of what it may and may not return.

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSavingsGoals_DeletedListing_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID := uuid.New().String()
	otherUserID := uuid.New().String()
	linkedItemID := uuid.New().String()
	accountID := uuid.New().String()
	otherAccountID := uuid.New().String()
	liveGoalID := uuid.New().String()
	deletedGoalID := uuid.New().String()
	otherGoalID := uuid.New().String()

	mustExec := func(what, sql string, args ...any) {
		t.Helper()
		_, execErr := tx.Exec(ctx, sql, args...)
		require.NoError(t, execErr, "seed %s", what)
	}

	for _, u := range []string{userID, otherUserID} {
		mustExec("user", `
			INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
			VALUES ($1, $2, $3, 'premium', 'active', 'iad')
		`, u, "test|goal-del-"+u, "goal-del-"+u+"@example.com")
	}
	mustExec("linked_item", `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-test-token')
	`, linkedItemID, userID, "goal-del-item-"+linkedItemID)
	mustExec("account", `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, status)
		VALUES ($1, $2, $3, $4, 'Savings', 'depository', 'active')
	`, accountID, linkedItemID, userID, "plaid-goal-del-"+accountID)

	otherItemID := uuid.New().String()
	mustExec("other linked_item", `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-test-token')
	`, otherItemID, otherUserID, "goal-del-item-"+otherItemID)
	mustExec("other account", `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, status)
		VALUES ($1, $2, $3, $4, 'Savings', 'depository', 'active')
	`, otherAccountID, otherItemID, otherUserID, "plaid-goal-del-"+otherAccountID)

	for _, g := range []struct{ id, user, account, name string }{
		{liveGoalID, userID, accountID, "Still Saving"},
		{deletedGoalID, userID, accountID, "Abandoned Trip"},
		{otherGoalID, otherUserID, otherAccountID, "Someone Else's"},
	} {
		mustExec("savings_goal", `
			INSERT INTO savings_goals (id, user_id, name, target_amount, current_amount, account_id, progress_basis)
			VALUES ($1, $2, $3, 5000, 100, $4, 'manual')
		`, g.id, g.user, g.name, g.account)
	}

	repo := NewSavingsGoalRepository(tx)

	// The other user deletes theirs too — a listing that keys on deleted_at alone would return it.
	require.NoError(t, repo.Delete(ctx, otherGoalID, otherUserID, nil))
	require.NoError(t, repo.Delete(ctx, deletedGoalID, userID, nil))

	deleted, err := repo.GetDeletedByUserID(ctx, userID)
	require.NoError(t, err)

	require.Len(t, deleted, 1, "exactly the caller's own tombstoned goals")
	assert.Equal(t, deletedGoalID, deleted[0].ID)
	assert.Equal(t, "Abandoned Trip", deleted[0].Name)
	require.NotNil(t, deleted[0].DeletedAt,
		"the listing must carry WHEN it was deleted — the UI has nothing else to show")

	// The live goal is not in the deleted listing, and the deleted goal is not in the live one.
	live, err := repo.GetByUserID(ctx, userID)
	require.NoError(t, err)
	require.Len(t, live, 1)
	assert.Equal(t, liveGoalID, live[0].ID)
	assert.Nil(t, live[0].DeletedAt, "a live goal carries no deletion timestamp")
}
