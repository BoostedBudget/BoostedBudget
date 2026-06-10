//go:build integration

package repository

// Integration test for the valid_boost_type CHECK constraint on the boosts table.
// Issue #972 (GAP CI): migration 000088 adds 'savings_goal' to the constraint, but this
// acceptance has only been covered by pgxmock tests — which can never validate DB CHECK
// constraints (the mock returns canned results without sending SQL to a real planner).
//
// These tests exercise a real PostgreSQL instance and prove:
//  1. Inserting a boost with boost_type='savings_goal' is accepted (000088 / AC#1).
//  2. All original boost types remain accepted (no regression from dropping+recreating the constraint).
//  3. An unknown boost_type is rejected by the constraint (regression guard).
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run TestBoostType -tags integration -v

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBoostType_SavingsGoalAccepted_Integration proves that migration 000088 allows
// inserting a boost with boost_type='savings_goal'. This is the regression guard for
// GAP CI / AC#1: without 000088 the DB CHECK rejects the insert.
func TestBoostType_SavingsGoalAccepted_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	// Seed a user (FK: boosts.user_id → users.id).
	userID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO users (id, auth0_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'us-east-1')
	`, userID, "test|"+userID.String(), "int-boost-"+userID.String()+"@example.com")
	require.NoError(t, err, "seed user")

	// Insert a boost with boost_type='savings_goal'. This would fail with a CHECK constraint
	// violation if migration 000088 had not been applied. This test is the regression guard
	// for AC#1 ("Creating a savings_goal automation boost type saves to DB without constraint error").
	_, err = tx.Exec(ctx, `
		INSERT INTO boosts
			(id, user_id, name, enabled, priority, boost_type, trigger_config, conditions, condition_logic, actions)
		VALUES
			($1, $2, $3, true, 0, 'savings_goal', '{}'::jsonb, '[]'::jsonb, 'AND', '{}'::jsonb)
	`, uuid.New(), userID, "CI Savings Goal Boost")
	assert.NoError(t, err, "inserting boost_type='savings_goal' must be accepted by the DB CHECK constraint (migration 000088)")
}

// TestBoostType_AllValidTypesAccepted_Integration proves that the constraint includes all
// four valid boost types after migration 000088 drops and recreates it.
func TestBoostType_AllValidTypesAccepted_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	// All 4 valid boost types that must be accepted — keep in sync with 000088 + domain.BoostType* consts.
	validTypes := []string{
		"transfer_suggestion",
		"budget_alert",
		"category_rollover",
		"savings_goal", // added by migration 000088 (#972)
	}

	for _, boostType := range validTypes {
		boostType := boostType
		t.Run("accepts_"+boostType, func(t *testing.T) {
			tx, err := pool.Begin(ctx)
			require.NoError(t, err)
			defer tx.Rollback(ctx) //nolint:errcheck

			userID := uuid.New()
			_, err = tx.Exec(ctx, `
				INSERT INTO users (id, auth0_user_id, email, subscription_tier, subscription_status, primary_region)
				VALUES ($1, $2, $3, 'premium', 'active', 'us-east-1')
			`, userID, "test|"+userID.String(), "int-boost-type-"+boostType+"-"+userID.String()+"@example.com")
			require.NoError(t, err, "seed user")

			_, err = tx.Exec(ctx, `
				INSERT INTO boosts
					(id, user_id, name, enabled, priority, boost_type, trigger_config, conditions, condition_logic, actions)
				VALUES
					($1, $2, $3, true, 0, $4, '{}'::jsonb, '[]'::jsonb, 'AND', '{}'::jsonb)
			`, uuid.New(), userID, "CI Boost "+boostType, boostType)
			assert.NoError(t, err, "boost_type=%q must be accepted by the DB CHECK constraint", boostType)
		})
	}
}

// TestBoostType_InvalidTypeRejected_Integration proves that an unrecognised boost_type is
// rejected by the valid_boost_type CHECK constraint. This is the inverse regression guard:
// if the constraint were dropped without being recreated, this test would fail (no error).
func TestBoostType_InvalidTypeRejected_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO users (id, auth0_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'us-east-1')
	`, userID, "test|"+userID.String(), "int-boost-bogus-"+userID.String()+"@example.com")
	require.NoError(t, err, "seed user")

	_, err = tx.Exec(ctx, `
		INSERT INTO boosts
			(id, user_id, name, enabled, priority, boost_type, trigger_config, conditions, condition_logic, actions)
		VALUES
			($1, $2, $3, true, 0, 'bogus_type', '{}'::jsonb, '[]'::jsonb, 'AND', '{}'::jsonb)
	`, uuid.New(), userID, "CI Invalid Boost")
	require.Error(t, err, "inserting boost_type='bogus_type' must be rejected by the DB CHECK constraint")
	assert.True(t, strings.Contains(err.Error(), "violates check constraint") || strings.Contains(err.Error(), "valid_boost_type"),
		"error should mention the CHECK constraint violation, got: %v", err)
}
