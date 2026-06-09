//go:build integration

// Package services provides real-Postgres integration tests for category-scoped rule execution.
// These tests exercise the full rule evaluation stack (BoostService.CreateRule →
// BoostExecutionService.ProcessEventTrigger → transfer_suggestions insert) against a live
// database — a level of coverage that pgxmock cannot provide because it never sends SQL to a
// real planner and thus cannot verify constraint enforcement or multi-table behavior.
//
// Issue #1271 A6′, A-CR10: category-scoped condition fires/skips, ownership rejections.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/services/... -tags integration -v -run TestRuleExecution_CategoryScoped_Integration
package services

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// connectIntegrationDB returns a pool connected to DATABASE_URL.
// If the env var is unset the test is skipped (CI without a live DB, local dev).
func connectIntegrationDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set — skipping integration test")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	require.NoError(t, err, "connect to test database")
	t.Cleanup(pool.Close)
	return pool
}

// categoryFixture holds the seeded IDs for a single sub-test fixture.
type categoryFixture struct {
	userID    string
	budgetXID string
	budgetYID string
	catCID    string // category in Budget X
	catDID    string // category in Budget Y
}

// seedFixtureInTx seeds the standard fixture inside a pgx transaction and returns the IDs.
// db must satisfy repository.DB (satisfied by both *pgxpool.Pool and pgx.Tx).
func seedFixtureInTx(ctx context.Context, t *testing.T, db repository.DB) categoryFixture {
	t.Helper()

	userID := uuid.New().String()
	_, err := db.Exec(ctx, `
		INSERT INTO users (id, auth0_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'us-east-1')
	`, userID, "int-cat|"+userID, "int-cat-"+userID+"@example.com")
	require.NoError(t, err, "seed user")

	budgetXID := uuid.New().String()
	_, err = db.Exec(ctx, `
		INSERT INTO budgets (id, user_id, name, period, start_date, amount)
		VALUES ($1, $2, 'Budget X', 'monthly', '2024-01-01', 1000.00)
	`, budgetXID, userID)
	require.NoError(t, err, "seed Budget X")

	budgetYID := uuid.New().String()
	_, err = db.Exec(ctx, `
		INSERT INTO budgets (id, user_id, name, period, start_date, amount)
		VALUES ($1, $2, 'Budget Y', 'monthly', '2024-01-01', 800.00)
	`, budgetYID, userID)
	require.NoError(t, err, "seed Budget Y")

	// Category C: in Budget X, PercentUsed = 200/500 * 100 = 40 %
	catCID := uuid.New().String()
	_, err = db.Exec(ctx, `
		INSERT INTO budget_categories (id, budget_id, user_id, name, allocated_amount, spent_amount)
		VALUES ($1, $2, $3, 'Category C', 500.00, 200.00)
	`, catCID, budgetXID, userID)
	require.NoError(t, err, "seed Category C")

	// Category D: in Budget Y, PercentUsed = 150/300 * 100 = 50 %
	catDID := uuid.New().String()
	_, err = db.Exec(ctx, `
		INSERT INTO budget_categories (id, budget_id, user_id, name, allocated_amount, spent_amount)
		VALUES ($1, $2, $3, 'Category D', 300.00, 150.00)
	`, catDID, budgetYID, userID)
	require.NoError(t, err, "seed Category D")

	return categoryFixture{
		userID:    userID,
		budgetXID: budgetXID,
		budgetYID: budgetYID,
		catCID:    catCID,
		catDID:    catDID,
	}
}

// buildTransferActionsJSON marshals a minimal TransferAction to JSON.
// FromAccountID/ToAccountID are fake UUIDs because the suggestion repo does NOT
// enforce FKs on those columns for rule-created suggestions.
func buildTransferActionsJSON(t *testing.T) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(domain.TransferAction{
		FromAccountID: uuid.New().String(),
		ToAccountID:   uuid.New().String(),
		AmountType:    "fixed",
		Amount:        100.00,
	})
	require.NoError(t, err)
	return b
}

// buildRule returns a CreateBoostRequest for a budget_category.percent_used > threshold
// rule scoped to the given budgetID + categoryID with a transfer_suggestion action.
func buildCategoryScopedRuleReq(t *testing.T, budgetID, categoryID string, threshold float64) *domain.CreateBoostRequest {
	t.Helper()
	return &domain.CreateBoostRequest{
		Name:           "test-cat-rule-" + uuid.New().String()[:8],
		BoostType:       domain.BoostTypeTransferSuggestion,
		Priority:       1,
		ConditionLogic: "AND",
		TriggerConfig: domain.TriggerConfig{
			Type:  domain.TriggerTypeEvent,
			Event: domain.EventBudgetPeriodEnd,
		},
		Conditions: []domain.BoostCondition{
			{
				Field:      "budget_category.percent_used",
				Operator:   domain.ConditionOperatorGt,
				Value:      threshold,
				CategoryID: categoryID,
				BudgetID:   budgetID,
			},
		},
		Actions: buildTransferActionsJSON(t),
	}
}

// TestRuleExecution_CategoryScoped_Integration exercises the full category-scoped rule stack
// against a real Postgres database. It is gated on DATABASE_URL and skips automatically in
// environments without a live database.
func TestRuleExecution_CategoryScoped_Integration(t *testing.T) {
	pool := connectIntegrationDB(t)
	ctx := context.Background()

	// Shared service constructor helper — wires real repos onto the transaction-scoped repos.
	buildServices := func(txDB repository.DB) (*automationRuleService, *ruleExecutionService) {
		budgetRepo := repository.NewBudgetRepository(txDB)
		rulesRepo := repository.NewBoostsRepository(txDB)
		execRepo := repository.NewBoostExecutionRepository(txDB)
		suggRepo := repository.NewTransferSuggestionRepository(txDB)

		ruleSvc := NewBoostService(rulesRepo)
		ruleSvc.SetBudgetRepo(budgetRepo)

		execSvc := NewBoostExecutionService(execRepo, suggRepo, rulesRepo, nil /* notificationService */)
		return ruleSvc, execSvc
	}

	// -------------------------------------------------------------------------
	// Sub-test 1: rule fires when event budget_id matches the rule's bound budget.
	// -------------------------------------------------------------------------
	t.Run("fires_on_matching_budget_close", func(t *testing.T) {
		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		defer tx.Rollback(ctx) //nolint:errcheck

		fix := seedFixtureInTx(ctx, t, tx)
		ruleSvc, execSvc := buildServices(tx)

		// Create a rule scoped to Budget X / Category C with threshold 30 % → fires at 40 %.
		req := buildCategoryScopedRuleReq(t, fix.budgetXID, fix.catCID, 30.0)
		_, err = ruleSvc.CreateRule(ctx, fix.userID, req)
		require.NoError(t, err)

		// EventData: budget_id = X, categories has C at 40 % used.
		catC := domain.BudgetCategory{
			ID:              fix.catCID,
			BudgetID:        fix.budgetXID,
			AllocatedAmount: 500,
			SpentAmount:     200,
		}
		eventData := map[string]interface{}{
			domain.EventDataKeyBudgetID:   fix.budgetXID,
			domain.EventDataKeyCategories: domain.BuildCategoryEventData([]domain.BudgetCategory{catC}),
		}

		err = execSvc.ProcessEventTrigger(ctx, fix.userID, domain.EventBudgetPeriodEnd, eventData)
		require.NoError(t, err)

		// Verify at least one transfer suggestion was created.
		var count int
		err = tx.QueryRow(ctx, `SELECT COUNT(*) FROM transfer_suggestions WHERE user_id = $1`, fix.userID).Scan(&count)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, count, 1, "rule should have fired and created a transfer suggestion")
	})

	// -------------------------------------------------------------------------
	// Sub-test 2: rule does NOT fire when event budget_id differs from rule's budget.
	// -------------------------------------------------------------------------
	t.Run("does_not_fire_on_other_budget_close", func(t *testing.T) {
		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		defer tx.Rollback(ctx) //nolint:errcheck

		fix := seedFixtureInTx(ctx, t, tx)
		ruleSvc, execSvc := buildServices(tx)

		// Rule scoped to Budget X / Category C.
		req := buildCategoryScopedRuleReq(t, fix.budgetXID, fix.catCID, 30.0)
		_, err = ruleSvc.CreateRule(ctx, fix.userID, req)
		require.NoError(t, err)

		// EventData: budget_id = Y (wrong budget) — rule must be skipped entirely.
		catD := domain.BudgetCategory{
			ID:              fix.catDID,
			BudgetID:        fix.budgetYID,
			AllocatedAmount: 300,
			SpentAmount:     150,
		}
		eventData := map[string]interface{}{
			domain.EventDataKeyBudgetID:   fix.budgetYID,
			domain.EventDataKeyCategories: domain.BuildCategoryEventData([]domain.BudgetCategory{catD}),
		}

		err = execSvc.ProcessEventTrigger(ctx, fix.userID, domain.EventBudgetPeriodEnd, eventData)
		require.NoError(t, err)

		var count int
		err = tx.QueryRow(ctx, `SELECT COUNT(*) FROM transfer_suggestions WHERE user_id = $1`, fix.userID).Scan(&count)
		require.NoError(t, err)
		assert.Equal(t, 0, count, "rule scoped to Budget X must not fire on a Budget Y close event")
	})

	// -------------------------------------------------------------------------
	// Sub-test 3: CreateRule rejects a category owned by a different user (403).
	// -------------------------------------------------------------------------
	t.Run("ownership_foreign_category_403", func(t *testing.T) {
		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		defer tx.Rollback(ctx) //nolint:errcheck

		fix := seedFixtureInTx(ctx, t, tx)

		// Seed a second user and a category that belongs to them.
		user2ID := uuid.New().String()
		_, err = tx.Exec(ctx, `
			INSERT INTO users (id, auth0_user_id, email, subscription_tier, subscription_status, primary_region)
			VALUES ($1, $2, $3, 'premium', 'active', 'us-east-1')
		`, user2ID, "int-u2|"+user2ID, "int-u2-"+user2ID+"@example.com")
		require.NoError(t, err, "seed user2")

		budget2ID := uuid.New().String()
		_, err = tx.Exec(ctx, `
			INSERT INTO budgets (id, user_id, name, period, start_date, amount)
			VALUES ($1, $2, 'User2 Budget', 'monthly', '2024-01-01', 500.00)
		`, budget2ID, user2ID)
		require.NoError(t, err, "seed user2 budget")

		cat2ID := uuid.New().String()
		_, err = tx.Exec(ctx, `
			INSERT INTO budget_categories (id, budget_id, user_id, name, allocated_amount, spent_amount)
			VALUES ($1, $2, $3, 'User2 Cat', 200.00, 50.00)
		`, cat2ID, budget2ID, user2ID)
		require.NoError(t, err, "seed user2 category")

		ruleSvc, _ := buildServices(tx)

		// user1 tries to create a rule scoped to user2's category → must be rejected.
		// We use budget2ID as BudgetID so the budget+category binding is consistent for
		// ValidateForPersistence; the ownership check fires in validateRuleReferenceOwnership.
		req := buildCategoryScopedRuleReq(t, budget2ID, cat2ID, 30.0)
		_, err = ruleSvc.CreateRule(ctx, fix.userID, req)
		require.Error(t, err, "CreateRule with another user's category must fail")
		assert.True(t, errors.Is(err, domain.ErrBoostConditionForbidden),
			"expected ErrBoostConditionForbidden, got: %v", err)
	})

	// -------------------------------------------------------------------------
	// Sub-test 4: CreateRule rejects a category not in the stated budget (400).
	// -------------------------------------------------------------------------
	t.Run("ownership_category_not_in_budget_400", func(t *testing.T) {
		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		defer tx.Rollback(ctx) //nolint:errcheck

		fix := seedFixtureInTx(ctx, t, tx)
		ruleSvc, _ := buildServices(tx)

		// Category C is in Budget X but we claim it belongs to Budget Y → must fail.
		req := buildCategoryScopedRuleReq(t, fix.budgetYID, fix.catCID, 30.0)
		_, err = ruleSvc.CreateRule(ctx, fix.userID, req)
		require.Error(t, err, "category C is not in Budget Y — must be rejected")
		assert.True(t, errors.Is(err, domain.ErrInvalidBoost),
			"expected ErrInvalidBoost (category not in budget), got: %v", err)
	})

	// -------------------------------------------------------------------------
	// Sub-test 5: CreateRule rejects a rule whose conditions span two budgets (400).
	// -------------------------------------------------------------------------
	t.Run("multi_budget_rejection_400", func(t *testing.T) {
		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		defer tx.Rollback(ctx) //nolint:errcheck

		fix := seedFixtureInTx(ctx, t, tx)
		ruleSvc, _ := buildServices(tx)

		// Two conditions: one scoped to Budget X / Cat C, one to Budget Y / Cat D.
		req := &domain.CreateBoostRequest{
			Name:           "multi-budget-rule-" + uuid.New().String()[:8],
			BoostType:       domain.BoostTypeTransferSuggestion,
			Priority:       1,
			ConditionLogic: "AND",
			TriggerConfig: domain.TriggerConfig{
				Type:  domain.TriggerTypeEvent,
				Event: domain.EventBudgetPeriodEnd,
			},
			Conditions: []domain.BoostCondition{
				{
					Field:      "budget_category.percent_used",
					Operator:   domain.ConditionOperatorGt,
					Value:      30.0,
					CategoryID: fix.catCID,
					BudgetID:   fix.budgetXID,
				},
				{
					Field:      "budget_category.percent_used",
					Operator:   domain.ConditionOperatorGt,
					Value:      30.0,
					CategoryID: fix.catDID,
					BudgetID:   fix.budgetYID,
				},
			},
			Actions: buildTransferActionsJSON(t),
		}

		_, err = ruleSvc.CreateRule(ctx, fix.userID, req)
		require.Error(t, err, "rule spanning two budgets must be rejected")
		assert.True(t, errors.Is(err, domain.ErrInvalidBoost),
			"expected ErrInvalidBoost (multi-budget), got: %v", err)
	})

	// -------------------------------------------------------------------------
	// Sub-test 6: JSON round-trip shape fires correctly (dual-shape reader).
	// -------------------------------------------------------------------------
	t.Run("json_round_trip_fires", func(t *testing.T) {
		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		defer tx.Rollback(ctx) //nolint:errcheck

		fix := seedFixtureInTx(ctx, t, tx)
		ruleSvc, execSvc := buildServices(tx)

		req := buildCategoryScopedRuleReq(t, fix.budgetXID, fix.catCID, 30.0)
		_, err = ruleSvc.CreateRule(ctx, fix.userID, req)
		require.NoError(t, err)

		// Build event data using the typed map shape, then JSON round-trip it to produce
		// the map[string]interface{} shape that arrives after a River worker deserialises it.
		catC := domain.BudgetCategory{
			ID:              fix.catCID,
			BudgetID:        fix.budgetXID,
			AllocatedAmount: 500,
			SpentAmount:     200,
		}
		typedEventData := map[string]interface{}{
			domain.EventDataKeyBudgetID:   fix.budgetXID,
			domain.EventDataKeyCategories: domain.BuildCategoryEventData([]domain.BudgetCategory{catC}),
		}
		// Marshal → unmarshal to simulate River JSON round-trip.
		raw, err := json.Marshal(typedEventData)
		require.NoError(t, err)
		var roundTripped map[string]interface{}
		require.NoError(t, json.Unmarshal(raw, &roundTripped))

		err = execSvc.ProcessEventTrigger(ctx, fix.userID, domain.EventBudgetPeriodEnd, roundTripped)
		require.NoError(t, err)

		var count int
		err = tx.QueryRow(ctx, `SELECT COUNT(*) FROM transfer_suggestions WHERE user_id = $1`, fix.userID).Scan(&count)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, count, 1,
			"dual-shape reader must handle JSON round-tripped categories map")
	})

	// -------------------------------------------------------------------------
	// Sub-test 7: missing category in event data → condition evaluates to false.
	// -------------------------------------------------------------------------
	t.Run("missing_or_deleted_category_false", func(t *testing.T) {
		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		defer tx.Rollback(ctx) //nolint:errcheck

		fix := seedFixtureInTx(ctx, t, tx)
		ruleSvc, execSvc := buildServices(tx)

		req := buildCategoryScopedRuleReq(t, fix.budgetXID, fix.catCID, 30.0)
		_, err = ruleSvc.CreateRule(ctx, fix.userID, req)
		require.NoError(t, err)

		// EventData contains Budget X's ID but an empty categories map — Category C is absent.
		eventData := map[string]interface{}{
			domain.EventDataKeyBudgetID:   fix.budgetXID,
			domain.EventDataKeyCategories: map[string]map[string]float64{},
		}

		err = execSvc.ProcessEventTrigger(ctx, fix.userID, domain.EventBudgetPeriodEnd, eventData)
		require.NoError(t, err)

		var count int
		err = tx.QueryRow(ctx, `SELECT COUNT(*) FROM transfer_suggestions WHERE user_id = $1`, fix.userID).Scan(&count)
		require.NoError(t, err)
		assert.Equal(t, 0, count,
			"missing category in event data must evaluate to false (not an error), no suggestion created")
	})

	// -------------------------------------------------------------------------
	// Sub-test 8: zero-allocated category → PercentUsed returns 100 when spent > 0.
	// -------------------------------------------------------------------------
	t.Run("zero_allocated_percent_used_fires", func(t *testing.T) {
		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		defer tx.Rollback(ctx) //nolint:errcheck

		// Seed a user, budget, and a category with AllocatedAmount=0, SpentAmount=50.
		userID := uuid.New().String()
		_, err = tx.Exec(ctx, `
			INSERT INTO users (id, auth0_user_id, email, subscription_tier, subscription_status, primary_region)
			VALUES ($1, $2, $3, 'premium', 'active', 'us-east-1')
		`, userID, "int-zero|"+userID, "int-zero-"+userID+"@example.com")
		require.NoError(t, err)

		budgetID := uuid.New().String()
		_, err = tx.Exec(ctx, `
			INSERT INTO budgets (id, user_id, name, period, start_date, amount)
			VALUES ($1, $2, 'Zero Alloc Budget', 'monthly', '2024-01-01', 500.00)
		`, budgetID, userID)
		require.NoError(t, err)

		// AllocatedAmount=0, SpentAmount=50 → PercentUsed() = 100 (over budget with no allocation).
		catID := uuid.New().String()
		_, err = tx.Exec(ctx, `
			INSERT INTO budget_categories (id, budget_id, user_id, name, allocated_amount, spent_amount)
			VALUES ($1, $2, $3, 'Zero Cat', 0.00, 50.00)
		`, catID, budgetID, userID)
		require.NoError(t, err)

		ruleSvc, execSvc := buildServices(tx)

		// Rule: percent_used > 50 — should fire because PercentUsed() = 100.
		req := buildCategoryScopedRuleReq(t, budgetID, catID, 50.0)
		_, err = ruleSvc.CreateRule(ctx, userID, req)
		require.NoError(t, err)

		zeroCat := domain.BudgetCategory{
			ID:              catID,
			BudgetID:        budgetID,
			AllocatedAmount: 0,
			SpentAmount:     50,
		}
		eventData := map[string]interface{}{
			domain.EventDataKeyBudgetID:   budgetID,
			domain.EventDataKeyCategories: domain.BuildCategoryEventData([]domain.BudgetCategory{zeroCat}),
		}

		err = execSvc.ProcessEventTrigger(ctx, userID, domain.EventBudgetPeriodEnd, eventData)
		require.NoError(t, err)

		var count int
		err = tx.QueryRow(ctx, `SELECT COUNT(*) FROM transfer_suggestions WHERE user_id = $1`, userID).Scan(&count)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, count, 1,
			"zero-allocation category with spent>0 returns PercentUsed=100, rule should fire")
	})
}
