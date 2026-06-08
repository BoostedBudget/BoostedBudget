//go:build integration

package repository

// Integration tests for savings goal ownership scoping (GAP AE).
// These tests prove GetByIDForUser enforces user_id ownership against a real Postgres
// planner — pgxmock never validates WHERE predicates, so these guards cannot be covered
// by the unit tests alone. See [[feedback_pgxmock_integration_coverage]].
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -tags integration -run TestGetByIDForUser -v

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGetByIDForUser_OwnerCanFetchOwnGoal verifies that the goal owner can retrieve their
// own goal via GetByIDForUser (the user-scoped lookup used by GAP AE).
func TestGetByIDForUser_OwnerCanFetchOwnGoal(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	// Seed a user and the required account (savings_goals.account_id NOT NULL).
	ownerID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO users (id, auth0_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'us-east-1')
	`, ownerID, "test|"+ownerID.String(), "int-sgowner-"+ownerID.String()+"@example.com")
	require.NoError(t, err, "seed owner user")

	linkedItemID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-test-token')
	`, linkedItemID, ownerID, "item-"+linkedItemID.String())
	require.NoError(t, err, "seed linked_item")

	accountID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type)
		VALUES ($1, $2, $3, $4, 'Test Savings', 'depository')
	`, accountID, linkedItemID, ownerID, "plaid-sg-"+accountID.String())
	require.NoError(t, err, "seed account")

	goalID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO savings_goals (id, user_id, name, target_amount, account_id)
		VALUES ($1, $2, 'Emergency Fund', 5000.00, $3)
	`, goalID, ownerID, accountID)
	require.NoError(t, err, "seed savings_goal")

	repo := NewSavingsGoalRepository(tx)

	goal, err := repo.GetByIDForUser(ctx, goalID.String(), ownerID.String())
	require.NoError(t, err, "owner must be able to fetch their own goal")
	assert.Equal(t, goalID.String(), goal.ID, "returned goal ID must match")
	assert.Equal(t, ownerID.String(), goal.UserID, "returned goal must belong to the owner")
	assert.Equal(t, "Emergency Fund", goal.Name)
}

// TestGetByIDForUser_NonOwnerCannotFetchGoal verifies that a different user cannot fetch
// a goal they do not own — the WHERE id=$1 AND user_id=$2 predicate must scope correctly.
// This is the critical IDOR guard: without the user_id filter, GetByID returns any goal
// regardless of ownership, leaking the goal name and account into the suggestion.
func TestGetByIDForUser_NonOwnerCannotFetchGoal(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	// Seed the real owner and an attacker (different user).
	ownerID := uuid.New()
	attackerID := uuid.New()
	for _, u := range []struct {
		id    uuid.UUID
		label string
	}{
		{ownerID, "owner"},
		{attackerID, "attacker"},
	} {
		_, err = tx.Exec(ctx, `
			INSERT INTO users (id, auth0_user_id, email, subscription_tier, subscription_status, primary_region)
			VALUES ($1, $2, $3, 'premium', 'active', 'us-east-1')
		`, u.id, "test|"+u.id.String(), "int-sg"+u.label+"-"+u.id.String()+"@example.com")
		require.NoError(t, err, "seed "+u.label+" user")
	}

	linkedItemID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-test-token')
	`, linkedItemID, ownerID, "item-"+linkedItemID.String())
	require.NoError(t, err, "seed linked_item")

	accountID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type)
		VALUES ($1, $2, $3, $4, 'Owner Savings', 'depository')
	`, accountID, linkedItemID, ownerID, "plaid-own-"+accountID.String())
	require.NoError(t, err, "seed account")

	goalID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO savings_goals (id, user_id, name, target_amount, account_id)
		VALUES ($1, $2, 'Owner Secret Goal', 10000.00, $3)
	`, goalID, ownerID, accountID)
	require.NoError(t, err, "seed savings_goal owned by owner")

	repo := NewSavingsGoalRepository(tx)

	// Attacker tries to fetch the owner's goal — must get ErrSavingsGoalNotFound.
	goal, err := repo.GetByIDForUser(ctx, goalID.String(), attackerID.String())
	require.Error(t, err, "non-owner must NOT be able to fetch another user's goal")
	assert.Nil(t, goal, "returned goal must be nil for non-owner")
	assert.ErrorIs(t, err, domain.ErrSavingsGoalNotFound, "error must be ErrSavingsGoalNotFound")
}

// TestGetByIDForUser_NonexistentGoalReturnsNotFound verifies that querying a goal ID that
// doesn't exist at all returns ErrSavingsGoalNotFound (not a generic DB error).
func TestGetByIDForUser_NonexistentGoalReturnsNotFound(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO users (id, auth0_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'us-east-1')
	`, userID, "test|"+userID.String(), "int-sgnoexist-"+userID.String()+"@example.com")
	require.NoError(t, err, "seed user")

	repo := NewSavingsGoalRepository(tx)

	goal, err := repo.GetByIDForUser(ctx, uuid.New().String(), userID.String())
	require.Error(t, err, "nonexistent goal must return error")
	assert.Nil(t, goal)
	assert.ErrorIs(t, err, ErrSavingsGoalNotFound)
}
