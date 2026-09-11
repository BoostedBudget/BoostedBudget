//go:build integration

package repository

// Integration test for GetBudgetVsActual.
// Tests the budgets + budget_categories JOIN that pgxmock cannot validate.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run TestGetBudgetVsActual_Integration -tags integration -v

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetBudgetVsActual_Integration(t *testing.T) {
	pool := connectTestDB(t)
	repo := NewAnalyticsRepository(pool)
	ctx := context.Background()

	// Wrap all seed data in a transaction so it is automatically rolled back.
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID := uuid.New()

	_, err = tx.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "test|"+userID.String(), "int-bva-"+userID.String()+"@example.com")
	require.NoError(t, err, "seed user")

	now := time.Now().UTC()

	// Seed 1 active monthly budget with 2 categories: one over budget, one under.
	monthlyBudgetID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO budgets (id, user_id, name, period, start_date, amount, is_active, series_id)
		VALUES ($1, $2, 'Monthly Budget', 'monthly', $3, 1000.00, true, $1)
	`, monthlyBudgetID, userID, now.Format("2006-01-02"))
	require.NoError(t, err, "seed monthly budget")

	// Category 1: Groceries — over budget (spent 300 > allocated 200)
	cat1ID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO budget_categories (id, budget_id, category_kind, pfc_primary, allocated_amount, spent_amount)
		VALUES ($1, $2, 'pfc', 'FOOD_AND_DRINK', 200.00, 300.00)
	`, cat1ID, monthlyBudgetID)
	require.NoError(t, err, "seed budget category 1")

	// Category 2: Transport — under budget (spent 50 < allocated 150)
	cat2ID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO budget_categories (id, budget_id, category_kind, pfc_primary, allocated_amount, spent_amount)
		VALUES ($1, $2, 'pfc', 'TRANSPORTATION', 150.00, 50.00)
	`, cat2ID, monthlyBudgetID)
	require.NoError(t, err, "seed budget category 2")

	// Seed 1 active yearly budget — should NOT be returned when querying monthly.
	yearlyBudgetID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO budgets (id, user_id, name, period, start_date, amount, is_active, series_id)
		VALUES ($1, $2, 'Yearly Budget', 'yearly', $3, 5000.00, true, $1)
	`, yearlyBudgetID, userID, now.Format("2006-01-02"))
	require.NoError(t, err, "seed yearly budget")

	cat3ID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO budget_categories (id, budget_id, category_kind, pfc_primary, allocated_amount, spent_amount)
		VALUES ($1, $2, 'pfc', 'GENERAL_MERCHANDISE', 500.00, 250.00)
	`, cat3ID, yearlyBudgetID)
	require.NoError(t, err, "seed yearly budget category")

	// Use transaction-wrapped repo.
	txRepo := NewAnalyticsRepository(tx)

	t.Run("monthly period returns monthly budget with correct totals", func(t *testing.T) {
		_ = repo // silence unused warning — txRepo is used below
		results, err := txRepo.GetBudgetVsActual(ctx, userID.String(), "monthly")
		require.NoError(t, err)
		require.Len(t, results, 1, "only the monthly budget should be returned")

		r := results[0]
		assert.Equal(t, monthlyBudgetID.String(), r.BudgetID)
		assert.Equal(t, "Monthly Budget", r.BudgetName)
		assert.Equal(t, "monthly", r.Period)
		// SUM of allocated: 200+150=350
		assert.InDelta(t, 350.0, r.AllocatedAmount, 0.01)
		// SUM of spent: 300+50=350
		assert.InDelta(t, 350.0, r.SpentAmount, 0.01)
	})

	t.Run("yearly period returns yearly budget not monthly budget", func(t *testing.T) {
		results, err := txRepo.GetBudgetVsActual(ctx, userID.String(), "yearly")
		require.NoError(t, err)
		require.Len(t, results, 1, "only the yearly budget should be returned")
		assert.Equal(t, yearlyBudgetID.String(), results[0].BudgetID)
	})

	t.Run("quarterly period returns empty slice when none exist", func(t *testing.T) {
		results, err := txRepo.GetBudgetVsActual(ctx, userID.String(), "quarterly")
		require.NoError(t, err)
		assert.Empty(t, results, "no quarterly budgets seeded")
	})
}

// insertMatchLogicBudgetCategory inserts a budget_categories row with an explicit match_logic,
// tags, and optional pfc_detailed/custom_category_id, returning its id. Helper for the Issue
// #1630 dedup-rule subtests below, which need control over match_logic that the plain seed
// helpers above don't exercise.
func insertMatchLogicBudgetCategory(
	t *testing.T, tx pgx.Tx, budgetID string,
	categoryKind string, pfcPrimary, pfcDetailed, customCategoryID *string,
	allocated, spent float64, matchLogic string, tags []string,
) string {
	t.Helper()
	ctx := context.Background()
	id := uuid.New()
	if tags == nil {
		tags = []string{} // tags column is NOT NULL
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO budget_categories
			(id, budget_id, category_kind, pfc_primary, pfc_detailed, custom_category_id,
			 allocated_amount, spent_amount, match_logic, tags)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`, id, budgetID, categoryKind, pfcPrimary, pfcDetailed, customCategoryID, allocated, spent, matchLogic, tags)
	require.NoError(t, err, "seed budget category with match_logic")
	return id.String()
}

// TestGetBudgetVsActual_Integration_DedupSubAllocations covers Issue #1630: a budget holding a
// parent line AND a child line nested under it must not double-count the child's spend/
// allocation. The parent's spent_amount already rolls the child's matched spend up (Issue
// #1203); GetBudgetVsActual previously summed EVERY row, so the child's amount landed in the
// total twice. Each subtest is a fresh budget so the aggregate is unambiguous about which row(s)
// contributed it.
func TestGetBudgetVsActual_Integration_DedupSubAllocations(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "test|"+userID.String(), "int-bva-dedup-"+userID.String()+"@example.com")
	require.NoError(t, err, "seed user")

	repo := NewAnalyticsRepository(tx)
	now := time.Now().UTC().Format("2006-01-02")

	newBudget := func(t *testing.T, name string) string {
		t.Helper()
		id := uuid.New()
		_, err := tx.Exec(ctx, `
			INSERT INTO budgets (id, user_id, name, period, start_date, amount, is_active, series_id)
			VALUES ($1, $2, $3, 'monthly', $4, 1000.00, true, $1)
		`, id, userID, name, now)
		require.NoError(t, err, "seed budget %q", name)
		return id.String()
	}

	food := "FOOD_AND_DRINK"
	fastFood := "FOOD_AND_DRINK_FAST_FOOD"

	t.Run("HOA case: custom child nested under a budgeted PFC primary counts once", func(t *testing.T) {
		budgetID := newBudget(t, "HOA under RENT_AND_UTILITIES")
		rent := "RENT_AND_UTILITIES"
		insertMatchLogicBudgetCategory(t, tx, budgetID, "pfc", &rent, nil, nil, 300, 150, "category_only", nil)

		// The actual reported shape: a custom category ("HOA") whose parent_pfc_primary is the
		// budgeted PFC primary above, budgeted as its own line in the same budget.
		hoaID := uuid.New()
		_, err := tx.Exec(ctx, `
			INSERT INTO custom_categories (id, user_id, name, parent_pfc_primary)
			VALUES ($1, $2, 'HOA', $3)
		`, hoaID, userID, rent)
		require.NoError(t, err, "seed HOA custom category")
		insertMatchLogicBudgetCategory(t, tx, budgetID, "custom", nil, nil, ptrUUID(hoaID), 50, 20, "category_only", nil)

		results, err := repo.GetBudgetVsActual(ctx, userID.String(), "monthly")
		require.NoError(t, err)
		var row *domain.BudgetVsActualRow
		for i := range results {
			if results[i].BudgetID == budgetID {
				row = &results[i]
			}
		}
		require.NotNil(t, row, "budget must be present in results")
		// The parent's own spent_amount (150) already rolls up the child's matched spend
		// (Issue #1203). Only the parent's row should count: 300 allocated / 150 spent — NOT
		// 350/170, which is what summing every row (the pre-#1630 bug) produces.
		assert.InDelta(t, 300.0, row.AllocatedAmount, 0.01, "HOA child's allocation must not be added on top of the parent's")
		assert.InDelta(t, 150.0, row.SpentAmount, 0.01, "HOA child's spend must not be added on top of the parent's roll-up")
	})

	t.Run("pfc child: detailed nested under budgeted primary counts once", func(t *testing.T) {
		budgetID := newBudget(t, "PFC detailed under primary")
		insertMatchLogicBudgetCategory(t, tx, budgetID, "pfc", &food, nil, nil, 300, 150, "category_only", nil)
		insertMatchLogicBudgetCategory(t, tx, budgetID, "pfc", &food, &fastFood, nil, 50, 20, "category_only", nil)

		results, err := repo.GetBudgetVsActual(ctx, userID.String(), "monthly")
		require.NoError(t, err)
		row := findBudgetVsActualRow(t, results, budgetID)
		assert.InDelta(t, 300.0, row.AllocatedAmount, 0.01)
		assert.InDelta(t, 150.0, row.SpentAmount, 0.01)
	})

	t.Run("custom child by custom_category_id parent counts once", func(t *testing.T) {
		budgetID := newBudget(t, "custom child of custom parent")
		parentCCID := uuid.New()
		_, err := tx.Exec(ctx, `INSERT INTO custom_categories (id, user_id, name) VALUES ($1, $2, 'Parent Custom')`, parentCCID, userID)
		require.NoError(t, err, "seed parent custom category")
		childCCID := uuid.New()
		_, err = tx.Exec(ctx, `INSERT INTO custom_categories (id, user_id, name, parent_id) VALUES ($1, $2, 'Child Custom', $3)`, childCCID, userID, parentCCID)
		require.NoError(t, err, "seed child custom category")

		insertMatchLogicBudgetCategory(t, tx, budgetID, "custom", nil, nil, ptrUUID(parentCCID), 300, 150, "category_only", nil)
		insertMatchLogicBudgetCategory(t, tx, budgetID, "custom", nil, nil, ptrUUID(childCCID), 50, 20, "category_only", nil)

		results, err := repo.GetBudgetVsActual(ctx, userID.String(), "monthly")
		require.NoError(t, err)
		row := findBudgetVsActualRow(t, results, budgetID)
		assert.InDelta(t, 300.0, row.AllocatedAmount, 0.01)
		assert.InDelta(t, 150.0, row.SpentAmount, 0.01)
	})

	t.Run("savings category is never deduped, even alongside a budgeted parent", func(t *testing.T) {
		budgetID := newBudget(t, "savings alongside parent")
		insertMatchLogicBudgetCategory(t, tx, budgetID, "pfc", &food, nil, nil, 300, 150, "category_only", nil)

		// FK chain: linked_item -> account -> savings_goal (mirrors budget_series_migration_integration_test.go).
		linkedItemID := uuid.New()
		_, err := tx.Exec(ctx, `
			INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
			VALUES ($1, $2, $3, 'encrypted_token_test')
		`, linkedItemID, userID, "item_"+linkedItemID.String())
		require.NoError(t, err, "seed linked_item")
		accountID := uuid.New()
		_, err = tx.Exec(ctx, `
			INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, subtype, current_balance)
			VALUES ($1, $2, $3, $4, 'Test Savings Account', 'depository', 'savings', 1000.00)
		`, accountID, linkedItemID, userID, "plaid_acct_"+accountID.String())
		require.NoError(t, err, "seed account")
		goalID := uuid.New()
		_, err = tx.Exec(ctx, `
			INSERT INTO savings_goals (id, user_id, account_id, name, target_amount, current_amount, progress_basis)
			VALUES ($1, $2, $3, 'Vacation', 1000, 0, 'manual')
		`, goalID, userID, accountID)
		require.NoError(t, err, "seed savings goal")
		_, err = tx.Exec(ctx, `
			INSERT INTO budget_categories (id, budget_id, category_kind, savings_goal_id, allocated_amount, spent_amount, match_logic)
			VALUES ($1, $2, 'savings', $3, 50, 20, 'category_only')
		`, uuid.New(), budgetID, goalID)
		require.NoError(t, err, "seed savings budget category")

		results, err := repo.GetBudgetVsActual(ctx, userID.String(), "monthly")
		require.NoError(t, err)
		row := findBudgetVsActualRow(t, results, budgetID)
		// Savings must always count in full — it is never a sub-allocation of anything.
		assert.InDelta(t, 350.0, row.AllocatedAmount, 0.01, "savings must count in full")
		assert.InDelta(t, 170.0, row.SpentAmount, 0.01, "savings must count in full")
	})

	t.Run("tag_only child escapes the parent's roll-up and counts standalone", func(t *testing.T) {
		budgetID := newBudget(t, "tag_only child escapes")
		insertMatchLogicBudgetCategory(t, tx, budgetID, "pfc", &food, nil, nil, 300, 150, "category_only", nil)
		insertMatchLogicBudgetCategory(t, tx, budgetID, "pfc", &food, &fastFood, nil, 50, 20, "tag_only", []string{"work"})

		results, err := repo.GetBudgetVsActual(ctx, userID.String(), "monthly")
		require.NoError(t, err)
		row := findBudgetVsActualRow(t, results, budgetID)
		// A tag_only child can match transactions the parent's category-based roll-up never
		// counted at all, so excluding it would UNDERcount. It must add on top of the parent.
		assert.InDelta(t, 350.0, row.AllocatedAmount, 0.01, "tag_only child must count standalone")
		assert.InDelta(t, 170.0, row.SpentAmount, 0.01, "tag_only child must count standalone")
	})

	t.Run("and-parent has no envelope, so its nested child still counts standalone", func(t *testing.T) {
		budgetID := newBudget(t, "and parent no envelope")
		insertMatchLogicBudgetCategory(t, tx, budgetID, "pfc", &food, nil, nil, 300, 150, "and", []string{"work"})
		insertMatchLogicBudgetCategory(t, tx, budgetID, "pfc", &food, &fastFood, nil, 50, 20, "category_only", nil)

		results, err := repo.GetBudgetVsActual(ctx, userID.String(), "monthly")
		require.NoError(t, err)
		row := findBudgetVsActualRow(t, results, budgetID)
		assert.InDelta(t, 350.0, row.AllocatedAmount, 0.01, "and-parent has no envelope for its child")
		assert.InDelta(t, 170.0, row.SpentAmount, 0.01, "and-parent has no envelope for its child")
	})
}

// findBudgetVsActualRow locates the row for budgetID or fails the test.
func findBudgetVsActualRow(t *testing.T, results []domain.BudgetVsActualRow, budgetID string) domain.BudgetVsActualRow {
	t.Helper()
	for _, r := range results {
		if r.BudgetID == budgetID {
			return r
		}
	}
	t.Fatalf("budget %s not present in GetBudgetVsActual results", budgetID)
	return domain.BudgetVsActualRow{}
}

// ptrUUID returns a pointer to the string form of a uuid.UUID, for passing as a nullable
// custom_category_id/parent_id argument.
func ptrUUID(id uuid.UUID) *string {
	s := id.String()
	return &s
}
