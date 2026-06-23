//go:build integration

package repository

// Integration test for GetByUserIDFiltered — boost_type filter on the boosts list.
// Issue #1267 (R8-1b): validates that the ANY($2::text[]) SQL type binding works
// against a real Postgres planner, which pgxmock cannot verify.
//
// Tests:
//  1. Filtered query returns only matching boost types (transfer_suggestion, savings_goal).
//  2. Other types (budget_alert, category_rollover) are excluded when not in the filter.
//  3. Empty filter slice returns all boosts — same as GetByUserID (back-compat).
//  4. Unknown boost_type in the filter returns an empty result, not an error.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run TestGetByUserIDFiltered_Integration -tags integration -v

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetByUserIDFiltered_Integration(t *testing.T) {
	pool := connectTestDB(t)
	repo := NewBoostsRepository(pool)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	// Seed a user.
	userID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "test|"+userID.String(), "int-filter-rule-"+userID.String()+"@example.com")
	require.NoError(t, err, "seed user")

	// Seed one boost of each valid type.
	seedBoost := func(boostType string) string {
		id := uuid.New()
		_, err := tx.Exec(ctx, `
			INSERT INTO boosts
				(id, user_id, name, enabled, priority, boost_type, trigger_config, conditions, condition_logic, actions)
			VALUES
				($1, $2, $3, true, 0, $4, '{}'::jsonb, '[]'::jsonb, 'AND', '{}'::jsonb)
		`, id, userID, "Boost "+boostType+"-"+id.String(), boostType)
		require.NoError(t, err, "seed boost type=%s", boostType)
		return id.String()
	}

	tsID := seedBoost("transfer_suggestion")
	sgID := seedBoost("savings_goal")
	baID := seedBoost("budget_alert")
	crID := seedBoost("category_rollover")

	// The repo wraps a DB pool, not a transaction, so we need to commit the seed data
	// and then clean up with a deferred delete — same pattern as savings_goal_ownership tests.
	require.NoError(t, tx.Commit(ctx))

	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM boosts WHERE user_id = $1`, userID)
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID)
	})

	t.Run("filters to transfer_suggestion and savings_goal only", func(t *testing.T) {
		filter := []domain.BoostType{domain.BoostTypeTransferSuggestion, domain.BoostTypeSavingsGoal}
		boosts, err := repo.GetByUserIDFiltered(ctx, userID.String(), filter)
		require.NoError(t, err)

		ids := make([]string, len(boosts))
		for i, b := range boosts {
			ids[i] = b.ID
		}

		assert.Contains(t, ids, tsID, "transfer_suggestion boost should be included")
		assert.Contains(t, ids, sgID, "savings_goal boost should be included")
		assert.NotContains(t, ids, baID, "budget_alert boost should be excluded")
		assert.NotContains(t, ids, crID, "category_rollover boost should be excluded")
		assert.Len(t, boosts, 2)
	})

	t.Run("empty filter returns all boosts (back-compat with GetByUserID)", func(t *testing.T) {
		boosts, err := repo.GetByUserIDFiltered(ctx, userID.String(), nil)
		require.NoError(t, err)
		assert.Len(t, boosts, 4, "no filter should return all 4 boosts")
	})

	t.Run("unknown boost_type in filter returns empty, not an error", func(t *testing.T) {
		filter := []domain.BoostType{"nonexistent_type"}
		boosts, err := repo.GetByUserIDFiltered(ctx, userID.String(), filter)
		require.NoError(t, err)
		assert.Empty(t, boosts, "unknown boost type should yield zero results")
	})
}
