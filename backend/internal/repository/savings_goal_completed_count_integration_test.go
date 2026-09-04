//go:build integration

package repository

// Issue #1569 Task 20 (A4). SavingsProgressSummary.CompletedGoalCount was derived by counting
// IsCompleted over GetActiveByUserID's result, and that query's WHERE clause is
// `is_completed = false`. The count was therefore structurally zero for every user who has ever
// finished a goal, and the field is REQUIRED in the JSON contract — an MCP or LLM consumer asking
// "how many goals have I completed?" got a confident, well-typed, wrong 0.
//
// The service-level fix is proven in services/savings_progress_chain_test.go. This file proves the
// SQL underneath it: the three-way scoping (this user, completed only, not tombstoned) that a mock
// returning an integer cannot establish. Two of those three are hazards a plausible query gets
// wrong — a missing user_id predicate leaks another user's totals, and a missing deleted_at
// predicate makes CompletedGoalCount incomparable with the GoalCount beside it.

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSavingsGoalRepository_CountCompletedByUserID_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck // always rolled back on the shared integration DB

	userID := uuid.New().String()
	otherUserID := uuid.New().String()

	mustExec := func(what, sql string, args ...any) {
		t.Helper()
		_, execErr := tx.Exec(ctx, sql, args...)
		require.NoError(t, execErr, "seed %s", what)
	}

	accountFor := map[string]string{}
	for _, u := range []string{userID, otherUserID} {
		mustExec("user", `
			INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
			VALUES ($1, $2, $3, 'premium', 'active', 'iad')
		`, u, "test|goal-count-"+u, "goal-count-"+u+"@example.com")
		// savings_goals.account_id is NOT NULL, so each user needs a real account to hang goals off.
		itemID := uuid.New().String()
		acctID := uuid.New().String()
		mustExec("linked_item", `
			INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
			VALUES ($1, $2, $3, 'enc-test-token')
		`, itemID, u, "goal-count-item-"+itemID)
		mustExec("account", `
			INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, status)
			VALUES ($1, $2, $3, $4, 'Savings', 'depository', 'active')
		`, acctID, itemID, u, "plaid-goal-count-"+acctID)
		accountFor[u] = acctID
	}

	// Four goals for the subject: two completed and live, one completed but TOMBSTONED, one active.
	// Plus one completed goal belonging to somebody else.
	for _, g := range []struct {
		user      string
		name      string
		completed bool
		deleted   bool
	}{
		{userID, "Emergency Fund", true, false},
		{userID, "New Laptop", true, false},
		{userID, "Abandoned Trip", true, true},
		{userID, "Still Saving", false, false},
		{otherUserID, "Not Yours", true, false},
	} {
		id := uuid.New().String()
		mustExec("savings_goal", `
			INSERT INTO savings_goals (id, user_id, name, target_amount, current_amount, account_id, progress_basis, is_completed, completed_at)
			VALUES ($1, $2, $3, 5000, 5000, $4, 'manual', $5, CASE WHEN $5 THEN NOW() ELSE NULL END)
		`, id, g.user, g.name, accountFor[g.user], g.completed)
		if g.deleted {
			mustExec("tombstone", `
				UPDATE savings_goals SET deleted_at = NOW(), deleted_reason = 'user_deleted', deleted_by = 'user'
				WHERE id = $1
			`, id)
		}
	}

	repo := NewSavingsGoalRepository(tx)

	completed, err := repo.CountCompletedByUserID(ctx, userID)
	require.NoError(t, err)
	assert.Equal(t, 2, completed,
		"only the caller's own COMPLETED, non-tombstoned goals: the tombstoned one and the other "+
			"user's must both be excluded")

	// The active universe is unchanged and disjoint. Asserted here so a future "fix" that widened
	// GetActiveByUserID to make the count work fails on this line instead of silently changing what
	// TotalSaved, TotalTarget and OverallPercentComplete are sums over.
	active, err := repo.GetActiveByUserID(ctx, userID)
	require.NoError(t, err)
	require.Len(t, active, 1)
	assert.Equal(t, "Still Saving", active[0].Name)
	assert.False(t, active[0].IsCompleted,
		"the value the old code counted: within this universe it is false by construction")

	// The other user's count is theirs alone — the mirror of the scoping assertion above, so a
	// missing user_id predicate cannot pass by returning a number that happens to be right for one
	// of the two users.
	otherCompleted, err := repo.CountCompletedByUserID(ctx, otherUserID)
	require.NoError(t, err)
	assert.Equal(t, 1, otherCompleted)

	// A user with no goals at all gets 0 — and this is the ONE case where the old, broken code and
	// the new code agree, which is why it is asserted separately rather than relied on.
	empty, err := repo.CountCompletedByUserID(ctx, uuid.New().String())
	require.NoError(t, err)
	assert.Equal(t, 0, empty)
}
