package repository

import (
	"context"
	"database/sql"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupBudgetMockDB creates a mock database for budget repository testing
func setupBudgetMockDB(t *testing.T) (*sql.DB, sqlmock.Sqlmock, BudgetRepository) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)

	repo := NewBudgetRepository(db)
	return db, mock, repo
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
	return &domain.BudgetCategory{
		ID:              uuid.New().String(),
		BudgetID:        budgetID,
		Category:        []string{"Food & Drink", "Groceries"},
		AllocatedAmount: 300.00,
		SpentAmount:     150.00,
	}
}

// TestBudgetRepository_Create tests successful budget creation
func TestBudgetRepository_Create(t *testing.T) {
	db, mock, repo := setupBudgetMockDB(t)
	defer db.Close()

	ctx := context.Background()
	budget := createTestBudget(uuid.New().String())

	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO budgets`)).WithArgs(
		sqlmock.AnyArg(), // id
		budget.UserID,
		budget.Name,
		budget.Description,
		budget.Period,
		sqlmock.AnyArg(), // start_date
		budget.EndDate,
		budget.Amount,
		sqlmock.AnyArg(), // currency_code
		budget.IsActive,
		sqlmock.AnyArg(), // created_at
		sqlmock.AnyArg(), // updated_at
	).WillReturnResult(sqlmock.NewResult(1, 1))

	err := repo.Create(ctx, budget)

	assert.NoError(t, err)
	assert.NotEmpty(t, budget.ID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestBudgetRepository_Create_DuplicateName tests duplicate name error
func TestBudgetRepository_Create_DuplicateName(t *testing.T) {
	db, mock, repo := setupBudgetMockDB(t)
	defer db.Close()

	ctx := context.Background()
	budget := createTestBudget(uuid.New().String())

	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO budgets`)).WithArgs(
		sqlmock.AnyArg(),
		budget.UserID,
		budget.Name,
		sqlmock.AnyArg(),
		sqlmock.AnyArg(),
		sqlmock.AnyArg(),
		sqlmock.AnyArg(),
		sqlmock.AnyArg(),
		sqlmock.AnyArg(),
		sqlmock.AnyArg(),
		sqlmock.AnyArg(),
		sqlmock.AnyArg(),
	).WillReturnError(sql.ErrNoRows) // Simulating constraint violation

	err := repo.Create(ctx, budget)

	assert.Error(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestBudgetRepository_GetByID tests retrieval by ID
func TestBudgetRepository_GetByID(t *testing.T) {
	db, mock, repo := setupBudgetMockDB(t)
	defer db.Close()

	ctx := context.Background()
	budgetID := uuid.New().String()
	userID := uuid.New().String()
	now := time.Now()
	desc := "Test budget"

	// Mock the main budget query
	budgetColumns := []string{
		"id", "user_id", "name", "description", "period", "start_date",
		"end_date", "amount", "currency_code", "is_active", "created_at", "updated_at",
	}

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT`)).
		WithArgs(budgetID).
		WillReturnRows(sqlmock.NewRows(budgetColumns).AddRow(
			budgetID,
			userID,
			"Groceries",
			desc,
			domain.BudgetPeriodMonthly,
			now,
			nil,
			500.00,
			"USD",
			true,
			now,
			now,
		))

	// Mock the categories query
	categoryColumns := []string{
		"id", "budget_id", "category", "allocated_amount", "spent_amount",
		"created_at", "updated_at",
	}

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT`)).
		WithArgs(budgetID).
		WillReturnRows(sqlmock.NewRows(categoryColumns).AddRow(
			uuid.New().String(),
			budgetID,
			`["Food & Drink", "Groceries"]`,
			300.00,
			150.00,
			now,
			now,
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
	db, mock, repo := setupBudgetMockDB(t)
	defer db.Close()

	ctx := context.Background()
	budgetID := uuid.New().String()

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT`)).
		WithArgs(budgetID).
		WillReturnError(sql.ErrNoRows)

	budget, err := repo.GetByID(ctx, budgetID)

	assert.Error(t, err)
	assert.Nil(t, budget)
	assert.Equal(t, domain.ErrBudgetNotFound, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestBudgetRepository_GetByUserID tests retrieval by user ID
func TestBudgetRepository_GetByUserID(t *testing.T) {
	db, mock, repo := setupBudgetMockDB(t)
	defer db.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	now := time.Now()

	columns := []string{
		"id", "user_id", "name", "description", "period", "start_date",
		"end_date", "amount", "currency_code", "is_active", "created_at", "updated_at",
	}

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT`)).
		WithArgs(userID).
		WillReturnRows(sqlmock.NewRows(columns).
			AddRow(
				uuid.New().String(),
				userID,
				"Budget 1",
				"Desc 1",
				domain.BudgetPeriodMonthly,
				now,
				nil,
				500.00,
				"USD",
				true,
				now,
				now,
			).
			AddRow(
				uuid.New().String(),
				userID,
				"Budget 2",
				"Desc 2",
				domain.BudgetPeriodWeekly,
				now,
				nil,
				100.00,
				"USD",
				true,
				now,
				now,
			))

	budgets, err := repo.GetByUserID(ctx, userID)

	assert.NoError(t, err)
	assert.Len(t, budgets, 2)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestBudgetRepository_GetActiveByUserID tests retrieval of active budgets
func TestBudgetRepository_GetActiveByUserID(t *testing.T) {
	db, mock, repo := setupBudgetMockDB(t)
	defer db.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	now := time.Now()

	columns := []string{
		"id", "user_id", "name", "description", "period", "start_date",
		"end_date", "amount", "currency_code", "is_active", "created_at", "updated_at",
	}

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT`)).
		WithArgs(userID).
		WillReturnRows(sqlmock.NewRows(columns).AddRow(
			uuid.New().String(),
			userID,
			"Active Budget",
			"Desc",
			domain.BudgetPeriodMonthly,
			now,
			nil,
			500.00,
			"USD",
			true,
			now,
			now,
		))

	budgets, err := repo.GetActiveByUserID(ctx, userID)

	assert.NoError(t, err)
	assert.Len(t, budgets, 1)
	assert.True(t, budgets[0].IsActive)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestBudgetRepository_Update tests budget update
func TestBudgetRepository_Update(t *testing.T) {
	db, mock, repo := setupBudgetMockDB(t)
	defer db.Close()

	ctx := context.Background()
	budget := createTestBudget(uuid.New().String())
	budget.ID = uuid.New().String()
	budget.Name = "Updated Name"

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE budgets`)).
		WithArgs(
			budget.Name,
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
			budget.ID,
			budget.UserID,
		).WillReturnResult(sqlmock.NewResult(0, 1))

	err := repo.Update(ctx, budget)

	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestBudgetRepository_Update_NotFound tests update of non-existent budget
func TestBudgetRepository_Update_NotFound(t *testing.T) {
	db, mock, repo := setupBudgetMockDB(t)
	defer db.Close()

	ctx := context.Background()
	budget := createTestBudget(uuid.New().String())
	budget.ID = uuid.New().String()

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE budgets`)).
		WithArgs(
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
			budget.ID,
			budget.UserID,
		).WillReturnResult(sqlmock.NewResult(0, 0))

	err := repo.Update(ctx, budget)

	assert.Error(t, err)
	assert.Equal(t, domain.ErrBudgetNotFound, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestBudgetRepository_Delete tests budget deletion
func TestBudgetRepository_Delete(t *testing.T) {
	db, mock, repo := setupBudgetMockDB(t)
	defer db.Close()

	ctx := context.Background()
	budgetID := uuid.New().String()
	userID := uuid.New().String()

	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM budgets`)).
		WithArgs(budgetID, userID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := repo.Delete(ctx, budgetID, userID)

	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestBudgetRepository_Delete_NotFound tests deletion of non-existent budget
func TestBudgetRepository_Delete_NotFound(t *testing.T) {
	db, mock, repo := setupBudgetMockDB(t)
	defer db.Close()

	ctx := context.Background()
	budgetID := uuid.New().String()
	userID := uuid.New().String()

	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM budgets`)).
		WithArgs(budgetID, userID).
		WillReturnResult(sqlmock.NewResult(0, 0))

	err := repo.Delete(ctx, budgetID, userID)

	assert.Error(t, err)
	assert.Equal(t, domain.ErrBudgetNotFound, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestBudgetRepository_CountByUserID tests counting budgets by user
func TestBudgetRepository_CountByUserID(t *testing.T) {
	db, mock, repo := setupBudgetMockDB(t)
	defer db.Close()

	ctx := context.Background()
	userID := uuid.New().String()

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT COUNT`)).
		WithArgs(userID).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(3))

	count, err := repo.CountByUserID(ctx, userID)

	assert.NoError(t, err)
	assert.Equal(t, 3, count)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestBudgetRepository_CreateCategory tests successful category creation
func TestBudgetRepository_CreateCategory(t *testing.T) {
	db, mock, repo := setupBudgetMockDB(t)
	defer db.Close()

	ctx := context.Background()
	budgetID := uuid.New().String()
	category := createTestBudgetCategory(budgetID)

	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO budget_categories`)).WithArgs(
		sqlmock.AnyArg(), // id
		category.BudgetID,
		sqlmock.AnyArg(), // category JSON
		category.AllocatedAmount,
		category.SpentAmount,
		sqlmock.AnyArg(), // created_at
		sqlmock.AnyArg(), // updated_at
	).WillReturnResult(sqlmock.NewResult(1, 1))

	err := repo.CreateCategory(ctx, category)

	assert.NoError(t, err)
	assert.NotEmpty(t, category.ID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestBudgetRepository_GetCategoriesByBudgetID tests retrieval of categories
func TestBudgetRepository_GetCategoriesByBudgetID(t *testing.T) {
	db, mock, repo := setupBudgetMockDB(t)
	defer db.Close()

	ctx := context.Background()
	budgetID := uuid.New().String()
	now := time.Now()

	columns := []string{
		"id", "budget_id", "category", "allocated_amount", "spent_amount",
		"created_at", "updated_at",
	}

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT`)).
		WithArgs(budgetID).
		WillReturnRows(sqlmock.NewRows(columns).
			AddRow(
				uuid.New().String(),
				budgetID,
				`["Food & Drink", "Groceries"]`,
				300.00,
				150.00,
				now,
				now,
			).
			AddRow(
				uuid.New().String(),
				budgetID,
				`["Food & Drink", "Coffee"]`,
				50.00,
				25.00,
				now,
				now,
			))

	categories, err := repo.GetCategoriesByBudgetID(ctx, budgetID)

	assert.NoError(t, err)
	assert.Len(t, categories, 2)
	assert.Equal(t, []string{"Food & Drink", "Groceries"}, categories[0].Category)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestBudgetRepository_UpdateCategory tests category update
func TestBudgetRepository_UpdateCategory(t *testing.T) {
	db, mock, repo := setupBudgetMockDB(t)
	defer db.Close()

	ctx := context.Background()
	category := createTestBudgetCategory(uuid.New().String())
	category.ID = uuid.New().String()
	category.AllocatedAmount = 400.00

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE budget_categories`)).
		WithArgs(
			sqlmock.AnyArg(), // category JSON
			category.AllocatedAmount,
			category.SpentAmount,
			sqlmock.AnyArg(), // updated_at
			category.ID,
			category.BudgetID,
		).WillReturnResult(sqlmock.NewResult(0, 1))

	err := repo.UpdateCategory(ctx, category)

	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestBudgetRepository_UpdateCategory_NotFound tests update of non-existent category
func TestBudgetRepository_UpdateCategory_NotFound(t *testing.T) {
	db, mock, repo := setupBudgetMockDB(t)
	defer db.Close()

	ctx := context.Background()
	category := createTestBudgetCategory(uuid.New().String())
	category.ID = uuid.New().String()

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE budget_categories`)).
		WithArgs(
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
			category.ID,
			category.BudgetID,
		).WillReturnResult(sqlmock.NewResult(0, 0))

	err := repo.UpdateCategory(ctx, category)

	assert.Error(t, err)
	assert.Equal(t, domain.ErrBudgetCategoryNotFound, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestBudgetRepository_UpdateCategorySpentAmount tests spent amount update
func TestBudgetRepository_UpdateCategorySpentAmount(t *testing.T) {
	db, mock, repo := setupBudgetMockDB(t)
	defer db.Close()

	ctx := context.Background()
	categoryID := uuid.New().String()
	newSpentAmount := 200.00

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE budget_categories`)).
		WithArgs(newSpentAmount, categoryID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := repo.UpdateCategorySpentAmount(ctx, categoryID, newSpentAmount)

	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestBudgetRepository_UpdateCategorySpentAmount_NotFound tests spent amount update on non-existent category
func TestBudgetRepository_UpdateCategorySpentAmount_NotFound(t *testing.T) {
	db, mock, repo := setupBudgetMockDB(t)
	defer db.Close()

	ctx := context.Background()
	categoryID := uuid.New().String()

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE budget_categories`)).
		WithArgs(200.00, categoryID).
		WillReturnResult(sqlmock.NewResult(0, 0))

	err := repo.UpdateCategorySpentAmount(ctx, categoryID, 200.00)

	assert.Error(t, err)
	assert.Equal(t, domain.ErrBudgetCategoryNotFound, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestBudgetRepository_DeleteCategory tests category deletion
func TestBudgetRepository_DeleteCategory(t *testing.T) {
	db, mock, repo := setupBudgetMockDB(t)
	defer db.Close()

	ctx := context.Background()
	categoryID := uuid.New().String()
	budgetID := uuid.New().String()

	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM budget_categories`)).
		WithArgs(categoryID, budgetID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := repo.DeleteCategory(ctx, categoryID, budgetID)

	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestBudgetRepository_DeleteCategory_NotFound tests deletion of non-existent category
func TestBudgetRepository_DeleteCategory_NotFound(t *testing.T) {
	db, mock, repo := setupBudgetMockDB(t)
	defer db.Close()

	ctx := context.Background()
	categoryID := uuid.New().String()
	budgetID := uuid.New().String()

	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM budget_categories`)).
		WithArgs(categoryID, budgetID).
		WillReturnResult(sqlmock.NewResult(0, 0))

	err := repo.DeleteCategory(ctx, categoryID, budgetID)

	assert.Error(t, err)
	assert.Equal(t, domain.ErrBudgetCategoryNotFound, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestBudgetRepository_GetCategoriesMatchingTransaction tests finding matching categories
func TestBudgetRepository_GetCategoriesMatchingTransaction(t *testing.T) {
	db, mock, repo := setupBudgetMockDB(t)
	defer db.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	transactionCategory := []string{"Food & Drink", "Coffee"}
	now := time.Now()

	columns := []string{
		"id", "budget_id", "category", "allocated_amount", "spent_amount",
		"created_at", "updated_at",
	}

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT bc.id`)).
		WithArgs(userID, sqlmock.AnyArg()). // userID and category JSON
		WillReturnRows(sqlmock.NewRows(columns).AddRow(
			uuid.New().String(),
			uuid.New().String(),
			`["Food & Drink", "Coffee"]`,
			100.00,
			50.00,
			now,
			now,
		))

	categories, err := repo.GetCategoriesMatchingTransaction(ctx, userID, transactionCategory)

	assert.NoError(t, err)
	assert.Len(t, categories, 1)
	assert.Equal(t, []string{"Food & Drink", "Coffee"}, categories[0].Category)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestBudgetRepository_GetCategoriesMatchingTransaction_NoMatches tests when no categories match
func TestBudgetRepository_GetCategoriesMatchingTransaction_NoMatches(t *testing.T) {
	db, mock, repo := setupBudgetMockDB(t)
	defer db.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	transactionCategory := []string{"Entertainment"}

	columns := []string{
		"id", "budget_id", "category", "allocated_amount", "spent_amount",
		"created_at", "updated_at",
	}

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT bc.id`)).
		WithArgs(userID, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows(columns))

	categories, err := repo.GetCategoriesMatchingTransaction(ctx, userID, transactionCategory)

	assert.NoError(t, err)
	assert.Empty(t, categories)
	assert.NoError(t, mock.ExpectationsWereMet())
}
