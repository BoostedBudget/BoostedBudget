package repository

import (
	"context"

	"regexp"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupBudgetMockDB creates a mock database for budget repository testing
func setupBudgetMockDB(t *testing.T) (pgxmock.PgxPoolIface, BudgetRepository) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)

	repo := NewBudgetRepository(mock)
	return mock, repo
}

// createTestBudget creates a test budget
func createTestBudget(userID string) *domain.Budget {
	desc := "Monthly grocery budget"
	now := time.Now()
	return &domain.Budget{
		ID:           uuid.New().String(),
		UserID:       userID,
		Name:         "Groceries",
		Description:  &desc,
		Period:       domain.BudgetPeriodMonthly,
		StartDate:    now,
		Amount:       500.00,
		CurrencyCode: "USD",
		IsActive:     true,
	}
}

// createTestBudgetCategory creates a test budget category
func createTestBudgetCategory(budgetID string) *domain.BudgetCategory {
	pfcPrimary := "FOOD_AND_DRINK"
	return &domain.BudgetCategory{
		ID:              uuid.New().String(),
		BudgetID:        budgetID,
		CategoryKind:    "pfc",
		PFCPrimary:      &pfcPrimary,
		AllocatedAmount: 300.00,
		SpentAmount:     150.00,
	}
}

// TestBudgetRepository_Create tests successful budget creation
func TestBudgetRepository_Create(t *testing.T) {
	mock, repo := setupBudgetMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	budget := createTestBudget(uuid.New().String())

	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO budgets`)).WithArgs(
		pgxmock.AnyArg(), // id
		budget.UserID,
		budget.Name,
		budget.Description,
		budget.Period,
		pgxmock.AnyArg(), // start_date
		budget.EndDate,
		budget.Amount,
		pgxmock.AnyArg(), // currency_code
		budget.IsActive,
		pgxmock.AnyArg(),    // created_at
		pgxmock.AnyArg(),    // updated_at
		budget.IncomeAmount, // income_amount (Issue #491)
		pgxmock.AnyArg(),    // income_sources JSON (Issue #491)
		pgxmock.AnyArg(),    // expected_income (Issue #1048)
		pgxmock.AnyArg(),    // series_id (#1300 G5)
	).WillReturnResult(pgxmock.NewResult("INSERT", 1))

	err := repo.Create(ctx, budget)

	assert.NoError(t, err)
	assert.NotEmpty(t, budget.ID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestBudgetRepository_Create_DuplicateName tests duplicate name error
func TestBudgetRepository_Create_DuplicateName(t *testing.T) {
	mock, repo := setupBudgetMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	budget := createTestBudget(uuid.New().String())

	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO budgets`)).WithArgs(
		pgxmock.AnyArg(),
		budget.UserID,
		budget.Name,
		pgxmock.AnyArg(),
		pgxmock.AnyArg(),
		pgxmock.AnyArg(),
		pgxmock.AnyArg(),
		pgxmock.AnyArg(),
		pgxmock.AnyArg(),
		pgxmock.AnyArg(),
		pgxmock.AnyArg(),
		pgxmock.AnyArg(),
		pgxmock.AnyArg(), // income_amount (Issue #491)
		pgxmock.AnyArg(), // income_sources JSON (Issue #491)
		pgxmock.AnyArg(), // expected_income (Issue #1048)
		pgxmock.AnyArg(), // series_id (#1300 G5)
	).WillReturnError(pgx.ErrNoRows) // Simulating constraint violation

	err := repo.Create(ctx, budget)

	assert.Error(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestBudgetRepository_GetByID tests retrieval by ID
func TestBudgetRepository_GetByID(t *testing.T) {
	mock, repo := setupBudgetMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	budgetID := uuid.New().String()
	userID := uuid.New().String()
	now := time.Now()
	desc := "Test budget"

	// Mock the main budget query (Issue #491: added income_amount, income_sources; #1048: expected_income; #1300 G5: series_id)
	budgetColumns := []string{
		"id", "user_id", "name", "description", "period", "start_date",
		"end_date", "amount", "currency_code", "is_active", "created_at", "updated_at",
		"income_amount", "income_sources", "expected_income", "series_id",
	}

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT`)).
		WithArgs(budgetID).
		WillReturnRows(pgxmock.NewRows(budgetColumns).AddRow(
			budgetID,
			userID,
			"Groceries",
			&desc, // Use pointer for nullable description
			domain.BudgetPeriodMonthly,
			now,
			nil,
			500.00,
			"USD",
			true,
			now,
			now,
			0.00,     // income_amount (Issue #491)
			`[]`,     // income_sources JSON (Issue #491)
			0.00,     // expected_income (Issue #1048)
			budgetID, // series_id (#1300 G5: standalone budgets have series_id = id)
		))

	// Mock the categories query (PFC v2: category_kind + discriminated union fields + savings #1300)
	categoryColumns := []string{
		"id", "budget_id", "category_kind", "pfc_primary", "pfc_detailed", "custom_category_id",
		"allocated_amount", "spent_amount", "created_at", "updated_at", "tags", "match_logic",
		"parent_id", "parent_pfc_primary",
		"savings_goal_id", "auto_created", "managed_by_goal_id",
		"savings_goal_name", // JOIN savings_goals — Issue #1300
	}

	pfcPrimary := "FOOD_AND_DRINK"
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT`)).
		WithArgs(budgetID).
		WillReturnRows(pgxmock.NewRows(categoryColumns).AddRow(
			uuid.New().String(),
			budgetID,
			"pfc",
			&pfcPrimary,
			nil, // pfc_detailed
			nil, // custom_category_id
			300.00,
			150.00,
			now,
			now,
			`[]`,            // tags
			"category_only", // match_logic
			nil,             // parent_id (custom join)
			nil,             // parent_pfc_primary (custom join)
			nil,             // savings_goal_id (#1300)
			false,           // auto_created
			nil,             // managed_by_goal_id
			nil,             // savings_goal_name (JOIN — nil for pfc category)
		))

	budget, err := repo.GetByID(ctx, budgetID)

	assert.NoError(t, err)
	assert.NotNil(t, budget)
	assert.Equal(t, budgetID, budget.ID)
	assert.Equal(t, userID, budget.UserID)
	assert.Equal(t, "Groceries", budget.Name)
	assert.Len(t, budget.Categories, 1)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestBudgetRepository_GetByID_NotFound tests retrieval of non-existent budget
func TestBudgetRepository_GetByID_NotFound(t *testing.T) {
	mock, repo := setupBudgetMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	budgetID := uuid.New().String()

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT`)).
		WithArgs(budgetID).
		WillReturnError(pgx.ErrNoRows)

	budget, err := repo.GetByID(ctx, budgetID)

	assert.Error(t, err)
	assert.Nil(t, budget)
	assert.Equal(t, domain.ErrBudgetNotFound, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestBudgetRepository_GetByUserID tests retrieval by user ID
func TestBudgetRepository_GetByUserID(t *testing.T) {
	mock, repo := setupBudgetMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	now := time.Now()

	// Helpers for nullable strings
	desc1, desc2 := "Desc 1", "Desc 2"
	budget1ID, budget2ID := uuid.New().String(), uuid.New().String()

	// Issue #491: added income_amount, income_sources; #1048: added expected_income; #1300 G5: series_id
	columns := []string{
		"id", "user_id", "name", "description", "period", "start_date",
		"end_date", "amount", "currency_code", "is_active", "created_at", "updated_at",
		"income_amount", "income_sources", "expected_income", "series_id",
	}

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT`)).
		WithArgs(userID).
		WillReturnRows(pgxmock.NewRows(columns).
			AddRow(
				budget1ID,
				userID,
				"Budget 1",
				&desc1, // Use pointer for nullable description
				domain.BudgetPeriodMonthly,
				now,
				nil,
				500.00,
				"USD",
				true,
				now,
				now,
				0.00,      // income_amount
				`[]`,      // income_sources
				0.00,      // expected_income (#1048)
				budget1ID, // series_id (#1300 G5)
			).
			AddRow(
				budget2ID,
				userID,
				"Budget 2",
				&desc2, // Use pointer for nullable description
				domain.BudgetPeriodWeekly,
				now,
				nil,
				100.00,
				"USD",
				true,
				now,
				now,
				0.00,      // income_amount
				`[]`,      // income_sources
				0.00,      // expected_income (#1048)
				budget2ID, // series_id (#1300 G5)
			))

	budgets, err := repo.GetByUserID(ctx, userID)

	assert.NoError(t, err)
	assert.Len(t, budgets, 2)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestBudgetRepository_GetActiveByUserID tests retrieval of active budgets
func TestBudgetRepository_GetActiveByUserID(t *testing.T) {
	mock, repo := setupBudgetMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	now := time.Now()

	// Helper for nullable string
	desc := "Desc"
	activeBudgetID := uuid.New().String()

	// Issue #491: added income_amount, income_sources; #1048: added expected_income; #1300 G5: series_id
	columns := []string{
		"id", "user_id", "name", "description", "period", "start_date",
		"end_date", "amount", "currency_code", "is_active", "created_at", "updated_at",
		"income_amount", "income_sources", "expected_income", "series_id",
	}

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT`)).
		WithArgs(userID).
		WillReturnRows(pgxmock.NewRows(columns).AddRow(
			activeBudgetID,
			userID,
			"Active Budget",
			&desc, // Use pointer for nullable description
			domain.BudgetPeriodMonthly,
			now,
			nil,
			500.00,
			"USD",
			true,
			now,
			now,
			0.00,           // income_amount
			`[]`,           // income_sources
			0.00,           // expected_income (#1048)
			activeBudgetID, // series_id (#1300 G5)
		))

	budgets, err := repo.GetActiveByUserID(ctx, userID)

	assert.NoError(t, err)
	assert.Len(t, budgets, 1)
	assert.True(t, budgets[0].IsActive)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestBudgetRepository_Update tests budget update
func TestBudgetRepository_Update(t *testing.T) {
	mock, repo := setupBudgetMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	budget := createTestBudget(uuid.New().String())
	budget.ID = uuid.New().String()
	budget.Name = "Updated Name"

	// Issue #491: added income_amount, income_sources; #1048: added expected_income
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE budgets`)).
		WithArgs(
			budget.Name,
			pgxmock.AnyArg(),
			pgxmock.AnyArg(),
			pgxmock.AnyArg(),
			pgxmock.AnyArg(),
			pgxmock.AnyArg(),
			pgxmock.AnyArg(),
			pgxmock.AnyArg(),
			pgxmock.AnyArg(),
			pgxmock.AnyArg(), // income_amount
			pgxmock.AnyArg(), // income_sources JSON
			pgxmock.AnyArg(), // expected_income (#1048)
			budget.ID,
			budget.UserID,
		).WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	err := repo.Update(ctx, budget)

	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestBudgetRepository_Update_NotFound tests update of non-existent budget
func TestBudgetRepository_Update_NotFound(t *testing.T) {
	mock, repo := setupBudgetMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	budget := createTestBudget(uuid.New().String())
	budget.ID = uuid.New().String()

	// Issue #491: added income_amount, income_sources; #1048: added expected_income
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE budgets`)).
		WithArgs(
			pgxmock.AnyArg(),
			pgxmock.AnyArg(),
			pgxmock.AnyArg(),
			pgxmock.AnyArg(),
			pgxmock.AnyArg(),
			pgxmock.AnyArg(),
			pgxmock.AnyArg(),
			pgxmock.AnyArg(),
			pgxmock.AnyArg(),
			pgxmock.AnyArg(), // income_amount
			pgxmock.AnyArg(), // income_sources JSON
			pgxmock.AnyArg(), // expected_income (#1048)
			budget.ID,
			budget.UserID,
		).WillReturnResult(pgxmock.NewResult("UPDATE", 0))

	err := repo.Update(ctx, budget)

	assert.Error(t, err)
	assert.Equal(t, domain.ErrBudgetNotFound, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestBudgetRepository_Delete tests budget deletion
func TestBudgetRepository_Delete(t *testing.T) {
	mock, repo := setupBudgetMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	budgetID := uuid.New().String()
	userID := uuid.New().String()

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE budgets`)).
		WithArgs(budgetID, userID, domain.SoftDeleteReasonUser, domain.SoftDeletedByUser).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE budget_categories`)).
		WithArgs(budgetID, domain.SoftDeleteReasonParent, domain.SoftDeletedByUser).
		WillReturnResult(pgxmock.NewResult("UPDATE", 2))
	mock.ExpectCommit()

	err := repo.Delete(ctx, budgetID, userID, nil)

	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestBudgetRepository_Delete_NotFound tests deletion of non-existent budget
func TestBudgetRepository_Delete_NotFound(t *testing.T) {
	mock, repo := setupBudgetMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	budgetID := uuid.New().String()
	userID := uuid.New().String()

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE budgets`)).
		WithArgs(budgetID, userID, domain.SoftDeleteReasonUser, domain.SoftDeletedByUser).
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))
	mock.ExpectRollback()

	err := repo.Delete(ctx, budgetID, userID, nil)

	assert.Error(t, err)
	assert.Equal(t, domain.ErrBudgetNotFound, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestBudgetRepository_CountByUserID tests counting budgets by user
func TestBudgetRepository_CountByUserID(t *testing.T) {
	mock, repo := setupBudgetMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT COUNT`)).
		WithArgs(userID).
		WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(3))

	count, err := repo.CountByUserID(ctx, userID)

	assert.NoError(t, err)
	assert.Equal(t, 3, count)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestBudgetRepository_CreateCategory tests successful category creation
func TestBudgetRepository_CreateCategory(t *testing.T) {
	mock, repo := setupBudgetMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	budgetID := uuid.New().String()
	category := createTestBudgetCategory(budgetID)

	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO budget_categories`)).WithArgs(
		pgxmock.AnyArg(),          // id
		category.BudgetID,         // budget_id
		category.CategoryKind,     // category_kind
		category.PFCPrimary,       // pfc_primary
		category.PFCDetailed,      // pfc_detailed
		category.CustomCategoryID, // custom_category_id
		category.AllocatedAmount,  // allocated_amount
		category.SpentAmount,      // spent_amount
		pgxmock.AnyArg(),          // created_at
		pgxmock.AnyArg(),          // updated_at
		pgxmock.AnyArg(),          // tags JSON
		pgxmock.AnyArg(),          // match_logic
		category.SavingsGoalID,    // savings_goal_id (#1300)
		category.AutoCreated,      // auto_created
		category.ManagedByGoalID,  // managed_by_goal_id
	).WillReturnResult(pgxmock.NewResult("INSERT", 1))

	err := repo.CreateCategory(ctx, category)

	assert.NoError(t, err)
	assert.NotEmpty(t, category.ID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestBudgetRepository_GetCategoriesByBudgetID tests retrieval of categories
func TestBudgetRepository_GetCategoriesByBudgetID(t *testing.T) {
	mock, repo := setupBudgetMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	budgetID := uuid.New().String()
	now := time.Now()

	columns := []string{
		"id", "budget_id", "category_kind", "pfc_primary", "pfc_detailed",
		"custom_category_id", "allocated_amount", "spent_amount",
		"created_at", "updated_at", "tags", "match_logic",
		"parent_id", "parent_pfc_primary",
		"savings_goal_id", "auto_created", "managed_by_goal_id",
		"savings_goal_name", // JOIN savings_goals — Issue #1300
	}

	pfcPrimary := "FOOD_AND_DRINK"
	pfcDetailed := "FOOD_AND_DRINK_COFFEE"
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT`)).
		WithArgs(budgetID).
		WillReturnRows(pgxmock.NewRows(columns).
			AddRow(
				uuid.New().String(),
				budgetID,
				"pfc",
				&pfcPrimary,
				nil,
				nil,
				300.00,
				150.00,
				now,
				now,
				`[]`,
				"category_only",
				nil,   // parent_id
				nil,   // parent_pfc_primary
				nil,   // savings_goal_id
				false, // auto_created
				nil,   // managed_by_goal_id
				nil,   // savings_goal_name
			).
			AddRow(
				uuid.New().String(),
				budgetID,
				"pfc",
				&pfcPrimary,
				&pfcDetailed,
				nil,
				50.00,
				25.00,
				now,
				now,
				`[]`,
				"category_only",
				nil,   // parent_id
				nil,   // parent_pfc_primary
				nil,   // savings_goal_id
				false, // auto_created
				nil,   // managed_by_goal_id
				nil,   // savings_goal_name
			))

	categories, err := repo.GetCategoriesByBudgetID(ctx, budgetID)

	assert.NoError(t, err)
	assert.Len(t, categories, 2)
	assert.Equal(t, "pfc", categories[0].CategoryKind)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestBudgetRepository_UpdateCategory tests category update
func TestBudgetRepository_UpdateCategory(t *testing.T) {
	mock, repo := setupBudgetMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	category := createTestBudgetCategory(uuid.New().String())
	category.ID = uuid.New().String()
	category.AllocatedAmount = 400.00

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE budget_categories`)).
		WithArgs(
			category.CategoryKind,     // category_kind
			category.PFCPrimary,       // pfc_primary
			category.PFCDetailed,      // pfc_detailed
			category.CustomCategoryID, // custom_category_id
			category.AllocatedAmount,  // allocated_amount
			category.SpentAmount,      // spent_amount
			pgxmock.AnyArg(),          // updated_at
			pgxmock.AnyArg(),          // tags JSON
			pgxmock.AnyArg(),          // match_logic
			category.ID,
			category.BudgetID,
		).WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	err := repo.UpdateCategory(ctx, category)

	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestBudgetRepository_UpdateCategory_NotFound tests update of non-existent category
func TestBudgetRepository_UpdateCategory_NotFound(t *testing.T) {
	mock, repo := setupBudgetMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	category := createTestBudgetCategory(uuid.New().String())
	category.ID = uuid.New().String()

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE budget_categories`)).
		WithArgs(
			pgxmock.AnyArg(),
			pgxmock.AnyArg(),
			pgxmock.AnyArg(),
			pgxmock.AnyArg(),
			pgxmock.AnyArg(),
			pgxmock.AnyArg(),
			pgxmock.AnyArg(),
			pgxmock.AnyArg(),
			pgxmock.AnyArg(),
			category.ID,
			category.BudgetID,
		).WillReturnResult(pgxmock.NewResult("UPDATE", 0))

	err := repo.UpdateCategory(ctx, category)

	assert.Error(t, err)
	assert.Equal(t, domain.ErrBudgetCategoryNotFound, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestBudgetRepository_UpdateCategorySpentAmount tests spent amount update
func TestBudgetRepository_UpdateCategorySpentAmount(t *testing.T) {
	mock, repo := setupBudgetMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	categoryID := uuid.New().String()
	newSpentAmount := 200.00

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE budget_categories`)).
		WithArgs(newSpentAmount, categoryID).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	err := repo.UpdateCategorySpentAmount(ctx, categoryID, newSpentAmount)

	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestBudgetRepository_UpdateCategorySpentAmount_NotFound tests spent amount update on non-existent category
func TestBudgetRepository_UpdateCategorySpentAmount_NotFound(t *testing.T) {
	mock, repo := setupBudgetMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	categoryID := uuid.New().String()

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE budget_categories`)).
		WithArgs(200.00, categoryID).
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))

	err := repo.UpdateCategorySpentAmount(ctx, categoryID, 200.00)

	assert.Error(t, err)
	assert.Equal(t, domain.ErrBudgetCategoryNotFound, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestBudgetRepository_DeleteCategory tests category deletion
func TestBudgetRepository_DeleteCategory(t *testing.T) {
	mock, repo := setupBudgetMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	categoryID := uuid.New().String()
	budgetID := uuid.New().String()

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE budget_categories`)).
		WithArgs(categoryID, budgetID, domain.SoftDeleteReasonUser, domain.SoftDeletedByUser).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	mock.ExpectCommit()

	err := repo.DeleteCategory(ctx, categoryID, budgetID, nil)

	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestBudgetRepository_DeleteCategory_NotFound tests deletion of non-existent category
func TestBudgetRepository_DeleteCategory_NotFound(t *testing.T) {
	mock, repo := setupBudgetMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	categoryID := uuid.New().String()
	budgetID := uuid.New().String()

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE budget_categories`)).
		WithArgs(categoryID, budgetID, domain.SoftDeleteReasonUser, domain.SoftDeletedByUser).
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))
	mock.ExpectRollback()

	err := repo.DeleteCategory(ctx, categoryID, budgetID, nil)

	assert.Error(t, err)
	assert.Equal(t, domain.ErrBudgetCategoryNotFound, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// =============================================================================
// ExpectedIncome Tests (Issue #1048: Budgets/Income Unification)
// =============================================================================

// TestBudgetRepository_Create_IncludesExpectedIncome verifies that Create sends expected_income
// to the database, mirroring the budget amount as the initial value.
func TestBudgetRepository_Create_IncludesExpectedIncome(t *testing.T) {
	mock, repo := setupBudgetMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	budget := createTestBudget(uuid.New().String())
	budget.ExpectedIncome = 500.00 // mirrors Amount

	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO budgets`)).WithArgs(
		pgxmock.AnyArg(), // id
		budget.UserID,
		budget.Name,
		budget.Description,
		budget.Period,
		pgxmock.AnyArg(), // start_date
		budget.EndDate,
		budget.Amount,
		pgxmock.AnyArg(), // currency_code
		budget.IsActive,
		pgxmock.AnyArg(),      // created_at
		pgxmock.AnyArg(),      // updated_at
		budget.IncomeAmount,   // income_amount
		pgxmock.AnyArg(),      // income_sources JSON
		budget.ExpectedIncome, // expected_income (#1048)
		pgxmock.AnyArg(),      // series_id (#1300 G5)
	).WillReturnResult(pgxmock.NewResult("INSERT", 1))

	err := repo.Create(ctx, budget)

	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestBudgetRepository_Create_DefaultsExpectedIncomeToAmount verifies that when ExpectedIncome
// is 0, Create defaults it to Amount (mirror write).
func TestBudgetRepository_Create_DefaultsExpectedIncomeToAmount(t *testing.T) {
	mock, repo := setupBudgetMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	budget := createTestBudget(uuid.New().String())

	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO budgets`)).WithArgs(
		pgxmock.AnyArg(), // id
		budget.UserID,
		budget.Name,
		budget.Description,
		budget.Period,
		pgxmock.AnyArg(), // start_date
		budget.EndDate,
		budget.Amount,
		pgxmock.AnyArg(), // currency_code
		budget.IsActive,
		pgxmock.AnyArg(),    // created_at
		pgxmock.AnyArg(),    // updated_at
		budget.IncomeAmount, // income_amount
		pgxmock.AnyArg(),    // income_sources JSON
		budget.Amount,       // expected_income defaults to Amount when 0 (#1048)
		pgxmock.AnyArg(),    // series_id (#1300 G5)
	).WillReturnResult(pgxmock.NewResult("INSERT", 1))

	err := repo.Create(ctx, budget)

	assert.NoError(t, err)
	assert.Equal(t, budget.Amount, budget.ExpectedIncome) // struct updated too
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestBudgetRepository_GetByID_ReturnsExpectedIncome verifies that GetByID scans expected_income
// from the SELECT result into Budget.ExpectedIncome.
func TestBudgetRepository_GetByID_ReturnsExpectedIncome(t *testing.T) {
	mock, repo := setupBudgetMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	budgetID := uuid.New().String()
	userID := uuid.New().String()
	now := time.Now()
	desc := "Test budget"

	budgetColumns := []string{
		"id", "user_id", "name", "description", "period", "start_date",
		"end_date", "amount", "currency_code", "is_active", "created_at", "updated_at",
		"income_amount", "income_sources", "expected_income", "series_id",
	}

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT`)).
		WithArgs(budgetID).
		WillReturnRows(pgxmock.NewRows(budgetColumns).AddRow(
			budgetID,
			userID,
			"Groceries",
			&desc,
			domain.BudgetPeriodMonthly,
			now,
			nil,
			500.00,
			"USD",
			true,
			now,
			now,
			0.00,     // income_amount
			`[]`,     // income_sources JSON
			500.00,   // expected_income (#1048)
			budgetID, // series_id (#1300 G5)
		))

	// Mock categories query (PFC v2 columns + savings #1300)
	categoryColumns := []string{
		"id", "budget_id", "category_kind", "pfc_primary", "pfc_detailed", "custom_category_id",
		"allocated_amount", "spent_amount", "created_at", "updated_at", "tags", "match_logic",
		"parent_id", "parent_pfc_primary",
		"savings_goal_id", "auto_created", "managed_by_goal_id",
		"savings_goal_name",
	}
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT`)).
		WithArgs(budgetID).
		WillReturnRows(pgxmock.NewRows(categoryColumns))

	budget, err := repo.GetByID(ctx, budgetID)

	assert.NoError(t, err)
	assert.NotNil(t, budget)
	assert.Equal(t, 500.00, budget.ExpectedIncome)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestBudgetRepository_GetByUserID_ReturnsExpectedIncome verifies that GetByUserID scans
// expected_income for each returned budget.
func TestBudgetRepository_GetByUserID_ReturnsExpectedIncome(t *testing.T) {
	mock, repo := setupBudgetMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	now := time.Now()
	desc := "Desc"
	byUserBudgetID := uuid.New().String()

	columns := []string{
		"id", "user_id", "name", "description", "period", "start_date",
		"end_date", "amount", "currency_code", "is_active", "created_at", "updated_at",
		"income_amount", "income_sources", "expected_income", "series_id",
	}

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT`)).
		WithArgs(userID).
		WillReturnRows(pgxmock.NewRows(columns).AddRow(
			byUserBudgetID,
			userID,
			"Budget 1",
			&desc,
			domain.BudgetPeriodMonthly,
			now,
			nil,
			500.00,
			"USD",
			true,
			now,
			now,
			0.00,           // income_amount
			`[]`,           // income_sources
			450.00,         // expected_income (#1048)
			byUserBudgetID, // series_id (#1300 G5)
		))

	budgets, err := repo.GetByUserID(ctx, userID)

	assert.NoError(t, err)
	assert.Len(t, budgets, 1)
	assert.Equal(t, 450.00, budgets[0].ExpectedIncome)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestBudgetRepository_GetActiveByUserID_ReturnsExpectedIncome verifies GetActiveByUserID scans
// expected_income.
func TestBudgetRepository_GetActiveByUserID_ReturnsExpectedIncome(t *testing.T) {
	mock, repo := setupBudgetMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	now := time.Now()
	desc := "Desc"
	activeBudget2ID := uuid.New().String()

	columns := []string{
		"id", "user_id", "name", "description", "period", "start_date",
		"end_date", "amount", "currency_code", "is_active", "created_at", "updated_at",
		"income_amount", "income_sources", "expected_income", "series_id",
	}

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT`)).
		WithArgs(userID).
		WillReturnRows(pgxmock.NewRows(columns).AddRow(
			activeBudget2ID,
			userID,
			"Active Budget",
			&desc,
			domain.BudgetPeriodMonthly,
			now,
			nil,
			500.00,
			"USD",
			true,
			now,
			now,
			0.00,            // income_amount
			`[]`,            // income_sources
			500.00,          // expected_income (#1048)
			activeBudget2ID, // series_id (#1300 G5)
		))

	budgets, err := repo.GetActiveByUserID(ctx, userID)

	assert.NoError(t, err)
	assert.Len(t, budgets, 1)
	assert.Equal(t, 500.00, budgets[0].ExpectedIncome)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestBudgetRepository_Update_IncludesExpectedIncome verifies that Update sends expected_income
// in the SET clause.
func TestBudgetRepository_Update_IncludesExpectedIncome(t *testing.T) {
	mock, repo := setupBudgetMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	budget := createTestBudget(uuid.New().String())
	budget.ID = uuid.New().String()
	budget.ExpectedIncome = 500.00

	// The UPDATE must include expected_income as an additional arg (#1048).
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE budgets`)).
		WithArgs(
			budget.Name,
			pgxmock.AnyArg(),      // description
			pgxmock.AnyArg(),      // period
			pgxmock.AnyArg(),      // start_date
			pgxmock.AnyArg(),      // end_date
			pgxmock.AnyArg(),      // amount
			pgxmock.AnyArg(),      // currency_code
			pgxmock.AnyArg(),      // is_active
			pgxmock.AnyArg(),      // updated_at
			pgxmock.AnyArg(),      // income_amount
			pgxmock.AnyArg(),      // income_sources JSON
			budget.ExpectedIncome, // expected_income (#1048)
			budget.ID,
			budget.UserID,
		).WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	err := repo.Update(ctx, budget)

	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestBudgetRepository_Update_DefaultsExpectedIncomeToAmount verifies that when ExpectedIncome
// is 0 on update, it defaults to Amount.
func TestBudgetRepository_Update_DefaultsExpectedIncomeToAmount(t *testing.T) {
	mock, repo := setupBudgetMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	budget := createTestBudget(uuid.New().String())
	budget.ID = uuid.New().String()
	// ExpectedIncome is 0; Update should fall back to Amount

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE budgets`)).
		WithArgs(
			budget.Name,
			pgxmock.AnyArg(), // description
			pgxmock.AnyArg(), // period
			pgxmock.AnyArg(), // start_date
			pgxmock.AnyArg(), // end_date
			pgxmock.AnyArg(), // amount
			pgxmock.AnyArg(), // currency_code
			pgxmock.AnyArg(), // is_active
			pgxmock.AnyArg(), // updated_at
			pgxmock.AnyArg(), // income_amount
			pgxmock.AnyArg(), // income_sources JSON
			budget.Amount,    // expected_income defaults to Amount when 0 (#1048)
			budget.ID,
			budget.UserID,
		).WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	err := repo.Update(ctx, budget)

	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestBudgetRepository_UpdateBudgetIncome_OnlyUpdatesIncomeAmount verifies that UpdateBudgetIncome
// only touches income_amount (the actual received sum), never expected_income (the user's plan).
func TestBudgetRepository_UpdateBudgetIncome_OnlyUpdatesIncomeAmount(t *testing.T) {
	mock, repo := setupBudgetMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	budgetID := uuid.New().String()
	incomeAmount := 3500.00

	// Only income_amount and budgetID — expected_income must NOT be touched.
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE budgets`)).
		WithArgs(incomeAmount, budgetID).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	err := repo.UpdateBudgetIncome(ctx, budgetID, incomeAmount)

	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestBudgetRepository_GetByName_ReturnsExpectedIncome verifies that GetByName scans
// expected_income from the result.
func TestBudgetRepository_GetByName_ReturnsExpectedIncome(t *testing.T) {
	mock, repo := setupBudgetMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	budgetName := "Groceries"
	now := time.Now()
	desc := "Monthly grocery budget"
	byNameBudgetID := uuid.New().String()

	budgetColumns := []string{
		"id", "user_id", "name", "description", "period", "start_date",
		"end_date", "amount", "currency_code", "is_active", "created_at", "updated_at",
		"income_amount", "income_sources", "expected_income", "series_id",
	}

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT`)).
		WithArgs(userID, budgetName).
		WillReturnRows(pgxmock.NewRows(budgetColumns).AddRow(
			byNameBudgetID,
			userID,
			budgetName,
			&desc,
			domain.BudgetPeriodMonthly,
			now,
			nil,
			500.00,
			"USD",
			true,
			now,
			now,
			0.00,           // income_amount
			`[]`,           // income_sources JSON
			500.00,         // expected_income (#1048)
			byNameBudgetID, // series_id (#1300 G5)
		))

	budget, err := repo.GetByName(ctx, userID, budgetName)

	assert.NoError(t, err)
	assert.NotNil(t, budget)
	assert.Equal(t, 500.00, budget.ExpectedIncome)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestBudgetRepository_GetTimezonesByUserIDs tests the batch timezone lookup (Issue #972).
// Verifies: correct map returned for known IDs; missing IDs produce no map entry.
func TestBudgetRepository_GetTimezonesByUserIDs(t *testing.T) {
	t.Run("returns correct map for known user IDs", func(t *testing.T) {
		mock, repo := setupBudgetMockDB(t)
		defer mock.Close()

		ctx := context.Background()
		userID1 := uuid.New().String()
		userID2 := uuid.New().String()

		rows := pgxmock.NewRows([]string{"id", "coalesce"}).
			AddRow(userID1, "America/New_York").
			AddRow(userID2, "Europe/London")

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, COALESCE(timezone, 'UTC') FROM users WHERE id = ANY($1)`)).
			WithArgs(pgxmock.AnyArg()).
			WillReturnRows(rows)

		result, err := repo.GetTimezonesByUserIDs(ctx, []string{userID1, userID2})

		require.NoError(t, err)
		assert.Equal(t, "America/New_York", result[userID1])
		assert.Equal(t, "Europe/London", result[userID2])
		assert.Len(t, result, 2)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("missing user IDs produce no map entry", func(t *testing.T) {
		mock, repo := setupBudgetMockDB(t)
		defer mock.Close()

		ctx := context.Background()
		knownID := uuid.New().String()
		missingID := uuid.New().String()

		// Only the known user is returned by the DB (missing user has no row).
		rows := pgxmock.NewRows([]string{"id", "coalesce"}).
			AddRow(knownID, "UTC")

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, COALESCE(timezone, 'UTC') FROM users WHERE id = ANY($1)`)).
			WithArgs(pgxmock.AnyArg()).
			WillReturnRows(rows)

		result, err := repo.GetTimezonesByUserIDs(ctx, []string{knownID, missingID})

		require.NoError(t, err)
		assert.Equal(t, "UTC", result[knownID])
		_, present := result[missingID]
		assert.False(t, present, "missing user ID should not appear in map; caller defaults to UTC")
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("empty input returns empty map without querying DB", func(t *testing.T) {
		mock, repo := setupBudgetMockDB(t)
		defer mock.Close()

		ctx := context.Background()

		result, err := repo.GetTimezonesByUserIDs(ctx, []string{})

		require.NoError(t, err)
		assert.Empty(t, result)
		assert.NoError(t, mock.ExpectationsWereMet()) // no DB calls expected
	})
}

// =============================================================================
// Savings goal category detach tombstone tests (Issue #1300, Codex C6 / G5)
// Since migration 000098 tombstones are keyed by (series_id, savings_goal_id).
// =============================================================================

func TestBudgetRepository_IsGoalDetachedForSeries(t *testing.T) {
	t.Run("returns true when tombstone exists", func(t *testing.T) {
		mock, repo := setupBudgetMockDB(t)
		defer mock.Close()
		ctx := context.Background()

		mock.ExpectQuery(`SELECT EXISTS`).
			WithArgs("series-1", "goal-1").
			WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(true))

		found, err := repo.IsGoalDetachedForSeries(ctx, "series-1", "goal-1")
		require.NoError(t, err)
		assert.True(t, found)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("returns false when no tombstone exists", func(t *testing.T) {
		mock, repo := setupBudgetMockDB(t)
		defer mock.Close()
		ctx := context.Background()

		mock.ExpectQuery(`SELECT EXISTS`).
			WithArgs("series-1", "goal-2").
			WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(false))

		found, err := repo.IsGoalDetachedForSeries(ctx, "series-1", "goal-2")
		require.NoError(t, err)
		assert.False(t, found)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestBudgetRepository_InsertGoalDetach(t *testing.T) {
	t.Run("inserts tombstone successfully (series_id, budget_id, goal_id)", func(t *testing.T) {
		mock, repo := setupBudgetMockDB(t)
		defer mock.Close()
		ctx := context.Background()

		// InsertGoalDetach(ctx, seriesID, budgetID, goalID) — 3 string args.
		// For a standalone budget seriesID == budgetID.
		mock.ExpectExec(`INSERT INTO savings_goal_category_detach`).
			WithArgs("series-1", "budget-1", "goal-1").
			WillReturnResult(pgxmock.NewResult("INSERT", 1))

		err := repo.InsertGoalDetach(ctx, "series-1", "budget-1", "goal-1")
		require.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("idempotent on conflict", func(t *testing.T) {
		mock, repo := setupBudgetMockDB(t)
		defer mock.Close()
		ctx := context.Background()

		// ON CONFLICT (series_id, savings_goal_id) DO NOTHING → 0 rows affected; no error.
		mock.ExpectExec(`INSERT INTO savings_goal_category_detach`).
			WithArgs("series-1", "budget-1", "goal-1").
			WillReturnResult(pgxmock.NewResult("INSERT", 0))

		err := repo.InsertGoalDetach(ctx, "series-1", "budget-1", "goal-1")
		require.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestBudgetRepository_DeleteGoalDetach(t *testing.T) {
	t.Run("deletes tombstone successfully", func(t *testing.T) {
		mock, repo := setupBudgetMockDB(t)
		defer mock.Close()
		ctx := context.Background()

		mock.ExpectExec(`DELETE FROM savings_goal_category_detach`).
			WithArgs("series-1", "goal-1").
			WillReturnResult(pgxmock.NewResult("DELETE", 1))

		err := repo.DeleteGoalDetach(ctx, "series-1", "goal-1")
		require.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("no error when tombstone does not exist", func(t *testing.T) {
		mock, repo := setupBudgetMockDB(t)
		defer mock.Close()
		ctx := context.Background()

		mock.ExpectExec(`DELETE FROM savings_goal_category_detach`).
			WithArgs("series-1", "goal-99").
			WillReturnResult(pgxmock.NewResult("DELETE", 0))

		err := repo.DeleteGoalDetach(ctx, "series-1", "goal-99")
		require.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestBudgetRepository_GetDetachedGoalIDsForSeries(t *testing.T) {
	t.Run("returns all detached goal IDs for a series", func(t *testing.T) {
		mock, repo := setupBudgetMockDB(t)
		defer mock.Close()
		ctx := context.Background()

		rows := pgxmock.NewRows([]string{"savings_goal_id"}).
			AddRow("goal-1").
			AddRow("goal-2")

		mock.ExpectQuery(`SELECT savings_goal_id`).
			WithArgs("series-1").
			WillReturnRows(rows)

		ids, err := repo.GetDetachedGoalIDsForSeries(ctx, "series-1")
		require.NoError(t, err)
		assert.Equal(t, []string{"goal-1", "goal-2"}, ids)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("returns empty slice when no detachments exist", func(t *testing.T) {
		mock, repo := setupBudgetMockDB(t)
		defer mock.Close()
		ctx := context.Background()

		mock.ExpectQuery(`SELECT savings_goal_id`).
			WithArgs("series-no-detach").
			WillReturnRows(pgxmock.NewRows([]string{"savings_goal_id"}))

		ids, err := repo.GetDetachedGoalIDsForSeries(ctx, "series-no-detach")
		require.NoError(t, err)
		assert.Empty(t, ids)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}
