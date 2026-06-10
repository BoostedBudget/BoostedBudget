//go:build integration

package repository

// Integration test for GetByUserIDFiltered — rule_type filter on the rules list.
// Issue #1267 (R8-1b): validates that the ANY($2::text[]) SQL type binding works
// against a real Postgres planner, which pgxmock cannot verify.
//
// Tests:
//  1. Filtered query returns only matching rule types (transfer_suggestion, savings_goal).
//  2. Other types (budget_alert, category_rollover) are excluded when not in the filter.
//  3. Empty filter slice returns all rules — same as GetByUserID (back-compat).
//  4. Unknown rule_type in the filter returns an empty result, not an error.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run TestGetByUserIDFiltered -tags integration -v

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
		INSERT INTO users (id, auth0_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'us-east-1')
	`, userID, "test|"+userID.String(), "int-filter-rule-"+userID.String()+"@example.com")
	require.NoError(t, err, "seed user")

	// Seed one rule of each valid type.
	seedRule := func(ruleType string) string {
		id := uuid.New()
		_, err := tx.Exec(ctx, `
			INSERT INTO rules
				(id, user_id, name, enabled, priority, rule_type, trigger_config, conditions, condition_logic, actions)
			VALUES
				($1, $2, $3, true, 0, $4, '{}'::jsonb, '[]'::jsonb, 'AND', '{}'::jsonb)
		`, id, userID, "Rule "+ruleType+"-"+id.String(), ruleType)
		require.NoError(t, err, "seed rule type=%s", ruleType)
		return id.String()
	}

	tsID := seedRule("transfer_suggestion")
	sgID := seedRule("savings_goal")
	baID := seedRule("budget_alert")
	crID := seedRule("category_rollover")

	// The repo wraps a DB pool, not a transaction, so we need to commit the seed data
	// and then clean up with a deferred delete — same pattern as savings_goal_ownership tests.
	require.NoError(t, tx.Commit(ctx))

	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM rules WHERE user_id = $1`, userID)
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID)
	})

	t.Run("filters to transfer_suggestion and savings_goal only", func(t *testing.T) {
		filter := []domain.BoostType{domain.BoostTypeTransferSuggestion, domain.BoostTypeSavingsGoal}
		rules, err := repo.GetByUserIDFiltered(ctx, userID.String(), filter)
		require.NoError(t, err)

		ids := make([]string, len(rules))
		for i, r := range rules {
			ids[i] = r.ID
		}

		assert.Contains(t, ids, tsID, "transfer_suggestion rule should be included")
		assert.Contains(t, ids, sgID, "savings_goal rule should be included")
		assert.NotContains(t, ids, baID, "budget_alert rule should be excluded")
		assert.NotContains(t, ids, crID, "category_rollover rule should be excluded")
		assert.Len(t, rules, 2)
	})

	t.Run("empty filter returns all rules (back-compat with GetByUserID)", func(t *testing.T) {
		rules, err := repo.GetByUserIDFiltered(ctx, userID.String(), nil)
		require.NoError(t, err)
		assert.Len(t, rules, 4, "no filter should return all 4 rules")
	})

	t.Run("unknown rule_type in filter returns empty, not an error", func(t *testing.T) {
		filter := []domain.BoostType{"nonexistent_type"}
		rules, err := repo.GetByUserIDFiltered(ctx, userID.String(), filter)
		require.NoError(t, err)
		assert.Empty(t, rules, "unknown rule type should yield zero results")
	})
}
