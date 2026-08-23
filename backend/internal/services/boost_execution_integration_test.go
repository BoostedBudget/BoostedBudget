//go:build integration

// Package services provides real-Postgres integration tests for category-scoped boost execution.
// These tests exercise the full boost evaluation stack (BoostService.CreateBoost →
// BoostExecutionService.ProcessEventTrigger → transfer_suggestions insert) against a live
// database — a level of coverage that pgxmock cannot provide because it never sends SQL to a
// real planner and thus cannot verify constraint enforcement or multi-table behavior.
//
// Issue #1271 A6′, A-CR10: category-scoped condition fires/skips, ownership rejections.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/services/... -tags integration -v -run TestBoostExecution_CategoryScoped_Integration
package services

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/maxnussbaum/BoostedBudget/internal/database"
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
	// Issue #1461: route through database.NewPoolFromURL (not a bare pgxpool.New) so this pool
	// pins session TimeZone=UTC exactly like the production pool does.
	pool, err := database.NewPoolFromURL(context.Background(), dsn)
	require.NoError(t, err, "connect to test database")
	t.Cleanup(pool.Close)
	return pool
}

// categoryFixture holds the seeded IDs for a single sub-test fixture.
type categoryFixture struct {
	userID        string
	budgetXID     string
	budgetYID     string
	catCID        string // category in Budget X
	catDID        string // category in Budget Y
	fromAccountID string // source account for transfer suggestions
	toAccountID   string // destination account for transfer suggestions
}

// seedFixtureInTx seeds the standard fixture inside a pgx transaction and returns the IDs.
// db must satisfy repository.DB (satisfied by both *pgxpool.Pool and pgx.Tx).
func seedFixtureInTx(ctx context.Context, t *testing.T, db repository.DB) categoryFixture {
	t.Helper()

	userID := uuid.New().String()
	_, err := db.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "int-cat|"+userID, "int-cat-"+userID+"@example.com")
	require.NoError(t, err, "seed user")

	// Seed linked_item + two accounts required by transfer_suggestions FK.
	linkedItemID := uuid.New().String()
	_, err = db.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-boost-test-token')
	`, linkedItemID, userID, "boost-item-"+linkedItemID)
	require.NoError(t, err, "seed linked_item")

	fromAccountID := uuid.New().String()
	_, err = db.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type)
		VALUES ($1, $2, $3, $4, 'Checking From', 'depository')
	`, fromAccountID, linkedItemID, userID, "plaid-from-"+fromAccountID)
	require.NoError(t, err, "seed from account")

	toAccountID := uuid.New().String()
	_, err = db.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type)
		VALUES ($1, $2, $3, $4, 'Savings To', 'depository')
	`, toAccountID, linkedItemID, userID, "plaid-to-"+toAccountID)
	require.NoError(t, err, "seed to account")

	budgetXID := uuid.New().String()
	_, err = db.Exec(ctx, `
		INSERT INTO budgets (id, user_id, name, period, start_date, amount, series_id)
		VALUES ($1, $2, 'Budget X', 'monthly', '2024-01-01', 1000.00, $1)
	`, budgetXID, userID)
	require.NoError(t, err, "seed Budget X")

	budgetYID := uuid.New().String()
	_, err = db.Exec(ctx, `
		INSERT INTO budgets (id, user_id, name, period, start_date, amount, series_id)
		VALUES ($1, $2, 'Budget Y', 'monthly', '2024-01-01', 800.00, $1)
	`, budgetYID, userID)
	require.NoError(t, err, "seed Budget Y")

	// Category C: in Budget X, PercentUsed = 200/500 * 100 = 40 %
	catCID := uuid.New().String()
	_, err = db.Exec(ctx, `
		INSERT INTO budget_categories (id, budget_id, category_kind, pfc_primary, allocated_amount, spent_amount)
		VALUES ($1, $2, 'pfc', 'FOOD_AND_DRINK', 500.00, 200.00)
	`, catCID, budgetXID)
	require.NoError(t, err, "seed Category C")

	// Category D: in Budget Y, PercentUsed = 150/300 * 100 = 50 %
	catDID := uuid.New().String()
	_, err = db.Exec(ctx, `
		INSERT INTO budget_categories (id, budget_id, category_kind, pfc_primary, allocated_amount, spent_amount)
		VALUES ($1, $2, 'pfc', 'TRANSPORTATION', 300.00, 150.00)
	`, catDID, budgetYID)
	require.NoError(t, err, "seed Category D")

	return categoryFixture{
		userID:        userID,
		budgetXID:     budgetXID,
		budgetYID:     budgetYID,
		catCID:        catCID,
		catDID:        catDID,
		fromAccountID: fromAccountID,
		toAccountID:   toAccountID,
	}
}

// buildTransferActionsJSON marshals a minimal TransferAction to JSON.
// fromAccountID and toAccountID must be valid rows in the accounts table;
// transfer_suggestions.from_account_id/to_account_id carry FK constraints.
func buildTransferActionsJSON(t *testing.T, fromAccountID, toAccountID string) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(domain.TransferAction{
		FromAccountID: fromAccountID,
		ToAccountID:   toAccountID,
		AmountType:    "fixed",
		Amount:        100.00,
	})
	require.NoError(t, err)
	return b
}

// buildCategoryScopedBoostReq returns a CreateBoostRequest for a budget_category.percent_used > threshold
// boost scoped to the given budgetID + categoryID with a transfer_suggestion action.
func buildCategoryScopedBoostReq(t *testing.T, budgetID, categoryID, fromAccountID, toAccountID string, threshold float64) *domain.CreateBoostRequest {
	t.Helper()
	return &domain.CreateBoostRequest{
		Name:           "test-cat-rule-" + uuid.New().String()[:8],
		BoostType:      domain.BoostTypeTransferSuggestion,
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
		Actions: buildTransferActionsJSON(t, fromAccountID, toAccountID),
	}
}

// TestBoostExecution_CategoryScoped_Integration exercises the full category-scoped rule stack
// against a real Postgres database. It is gated on DATABASE_URL and skips automatically in
// environments without a live database.
func TestBoostExecution_CategoryScoped_Integration(t *testing.T) {
	pool := connectIntegrationDB(t)
	ctx := context.Background()

	// Shared service constructor helper — wires real repos onto the transaction-scoped repos.
	buildServices := func(txDB repository.DB) (*boostService, *boostExecutionService) {
		budgetRepo := repository.NewBudgetRepository(txDB)
		boostsRepo := repository.NewBoostsRepository(txDB)
		execRepo := repository.NewBoostExecutionRepository(txDB)
		suggRepo := repository.NewTransferSuggestionRepository(txDB)

		boostSvc := NewBoostService(boostsRepo)
		boostSvc.SetBudgetRepo(budgetRepo)

		execSvc := NewBoostExecutionService(execRepo, suggRepo, boostsRepo, nil /* notificationService */)
		return boostSvc, execSvc
	}

	// -------------------------------------------------------------------------
	// Sub-test 1: rule fires when event budget_id matches the rule's bound budget.
	// -------------------------------------------------------------------------
	t.Run("fires_on_matching_budget_close", func(t *testing.T) {
		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		defer tx.Rollback(ctx) //nolint:errcheck

		fix := seedFixtureInTx(ctx, t, tx)
		boostSvc, execSvc := buildServices(tx)

		// Create a rule scoped to Budget X / Category C with threshold 30 % → fires at 40 %.
		req := buildCategoryScopedBoostReq(t, fix.budgetXID, fix.catCID, fix.fromAccountID, fix.toAccountID, 30.0)
		_, err = boostSvc.CreateBoost(ctx, fix.userID, req)
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
		boostSvc, execSvc := buildServices(tx)

		// Rule scoped to Budget X / Category C.
		req := buildCategoryScopedBoostReq(t, fix.budgetXID, fix.catCID, fix.fromAccountID, fix.toAccountID, 30.0)
		_, err = boostSvc.CreateBoost(ctx, fix.userID, req)
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
	// Sub-test 3: CreateBoost rejects a category owned by a different user (403).
	// -------------------------------------------------------------------------
	t.Run("ownership_foreign_category_403", func(t *testing.T) {
		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		defer tx.Rollback(ctx) //nolint:errcheck

		fix := seedFixtureInTx(ctx, t, tx)

		// Seed a second user and a category that belongs to them.
		user2ID := uuid.New().String()
		_, err = tx.Exec(ctx, `
			INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
			VALUES ($1, $2, $3, 'premium', 'active', 'iad')
		`, user2ID, "int-u2|"+user2ID, "int-u2-"+user2ID+"@example.com")
		require.NoError(t, err, "seed user2")

		budget2ID := uuid.New().String()
		_, err = tx.Exec(ctx, `
			INSERT INTO budgets (id, user_id, name, period, start_date, amount, series_id)
			VALUES ($1, $2, 'User2 Budget', 'monthly', '2024-01-01', 500.00, $1)
		`, budget2ID, user2ID)
		require.NoError(t, err, "seed user2 budget")

		cat2ID := uuid.New().String()
		_, err = tx.Exec(ctx, `
			INSERT INTO budget_categories (id, budget_id, category_kind, pfc_primary, allocated_amount, spent_amount)
			VALUES ($1, $2, 'pfc', 'FOOD_AND_DRINK', 200.00, 50.00)
		`, cat2ID, budget2ID)
		require.NoError(t, err, "seed user2 category")

		boostSvc, _ := buildServices(tx)

		// user1 tries to create a rule scoped to user2's category → must be rejected.
		// We use budget2ID as BudgetID so the budget+category binding is consistent for
		// ValidateForPersistence; the ownership check fires in validateBoostReferenceOwnership.
		req := buildCategoryScopedBoostReq(t, budget2ID, cat2ID, fix.fromAccountID, fix.toAccountID, 30.0)
		_, err = boostSvc.CreateBoost(ctx, fix.userID, req)
		require.Error(t, err, "CreateBoost with another user's category must fail")
		assert.True(t, errors.Is(err, domain.ErrBoostConditionForbidden),
			"expected ErrBoostConditionForbidden, got: %v", err)
	})

	// -------------------------------------------------------------------------
	// Sub-test 4: CreateBoost rejects a category not in the stated budget (400).
	// -------------------------------------------------------------------------
	t.Run("ownership_category_not_in_budget_400", func(t *testing.T) {
		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		defer tx.Rollback(ctx) //nolint:errcheck

		fix := seedFixtureInTx(ctx, t, tx)
		boostSvc, _ := buildServices(tx)

		// Category C is in Budget X but we claim it belongs to Budget Y → must fail.
		req := buildCategoryScopedBoostReq(t, fix.budgetYID, fix.catCID, fix.fromAccountID, fix.toAccountID, 30.0)
		_, err = boostSvc.CreateBoost(ctx, fix.userID, req)
		require.Error(t, err, "category C is not in Budget Y — must be rejected")
		assert.True(t, errors.Is(err, domain.ErrInvalidBoost),
			"expected ErrInvalidBoost (category not in budget), got: %v", err)
	})

	// -------------------------------------------------------------------------
	// Sub-test 5: CreateBoost rejects a boost whose conditions span two budgets (400).
	// -------------------------------------------------------------------------
	t.Run("multi_budget_rejection_400", func(t *testing.T) {
		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		defer tx.Rollback(ctx) //nolint:errcheck

		fix := seedFixtureInTx(ctx, t, tx)
		boostSvc, _ := buildServices(tx)

		// Two conditions: one scoped to Budget X / Cat C, one to Budget Y / Cat D.
		req := &domain.CreateBoostRequest{
			Name:           "multi-budget-rule-" + uuid.New().String()[:8],
			BoostType:      domain.BoostTypeTransferSuggestion,
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
			Actions: buildTransferActionsJSON(t, fix.fromAccountID, fix.toAccountID),
		}

		_, err = boostSvc.CreateBoost(ctx, fix.userID, req)
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
		boostSvc, execSvc := buildServices(tx)

		req := buildCategoryScopedBoostReq(t, fix.budgetXID, fix.catCID, fix.fromAccountID, fix.toAccountID, 30.0)
		_, err = boostSvc.CreateBoost(ctx, fix.userID, req)
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
		boostSvc, execSvc := buildServices(tx)

		req := buildCategoryScopedBoostReq(t, fix.budgetXID, fix.catCID, fix.fromAccountID, fix.toAccountID, 30.0)
		_, err = boostSvc.CreateBoost(ctx, fix.userID, req)
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
			INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
			VALUES ($1, $2, $3, 'premium', 'active', 'iad')
		`, userID, "int-zero|"+userID, "int-zero-"+userID+"@example.com")
		require.NoError(t, err)

		budgetID := uuid.New().String()
		_, err = tx.Exec(ctx, `
			INSERT INTO budgets (id, user_id, name, period, start_date, amount, series_id)
			VALUES ($1, $2, 'Zero Alloc Budget', 'monthly', '2024-01-01', 500.00, $1)
		`, budgetID, userID)
		require.NoError(t, err)

		// AllocatedAmount=0, SpentAmount=50 → PercentUsed() = 100 (over budget with no allocation).
		catID := uuid.New().String()
		_, err = tx.Exec(ctx, `
			INSERT INTO budget_categories (id, budget_id, category_kind, pfc_primary, allocated_amount, spent_amount)
			VALUES ($1, $2, 'pfc', 'FOOD_AND_DRINK', 0.00, 50.00)
		`, catID, budgetID)
		require.NoError(t, err)

		// Seed linked_item + two accounts required by transfer_suggestions FK.
		zeroLinkedItemID := uuid.New().String()
		_, err = tx.Exec(ctx, `
			INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
			VALUES ($1, $2, $3, 'enc-zero-test-token')
		`, zeroLinkedItemID, userID, "zero-item-"+zeroLinkedItemID)
		require.NoError(t, err, "seed zero-test linked_item")

		zeroFromAccID := uuid.New().String()
		_, err = tx.Exec(ctx, `
			INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type)
			VALUES ($1, $2, $3, $4, 'Zero From', 'depository')
		`, zeroFromAccID, zeroLinkedItemID, userID, "plaid-zero-from-"+zeroFromAccID)
		require.NoError(t, err, "seed zero-test from account")

		zeroToAccID := uuid.New().String()
		_, err = tx.Exec(ctx, `
			INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type)
			VALUES ($1, $2, $3, $4, 'Zero To', 'depository')
		`, zeroToAccID, zeroLinkedItemID, userID, "plaid-zero-to-"+zeroToAccID)
		require.NoError(t, err, "seed zero-test to account")

		boostSvc, execSvc := buildServices(tx)

		// Rule: percent_used > 50 — should fire because PercentUsed() = 100.
		req := buildCategoryScopedBoostReq(t, budgetID, catID, zeroFromAccID, zeroToAccID, 50.0)
		_, err = boostSvc.CreateBoost(ctx, userID, req)
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
