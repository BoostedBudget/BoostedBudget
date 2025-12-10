package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// BudgetRepository defines the interface for budget data access.
type BudgetRepository interface {
	// Budget CRUD operations
	Create(ctx context.Context, budget *domain.Budget) error
	GetByID(ctx context.Context, id string) (*domain.Budget, error)
	GetByUserID(ctx context.Context, userID string) ([]*domain.Budget, error)
	GetActiveByUserID(ctx context.Context, userID string) ([]*domain.Budget, error)
	GetByName(ctx context.Context, userID, name string) (*domain.Budget, error)
	Update(ctx context.Context, budget *domain.Budget) error
	Delete(ctx context.Context, id, userID string) error
	CountByUserID(ctx context.Context, userID string) (int, error)

	// BudgetCategory operations
	CreateCategory(ctx context.Context, category *domain.BudgetCategory) error
	GetCategoriesByBudgetID(ctx context.Context, budgetID string) ([]domain.BudgetCategory, error)
	UpdateCategory(ctx context.Context, category *domain.BudgetCategory) error
	UpdateCategorySpentAmount(ctx context.Context, categoryID string, spentAmount float64) error
	DeleteCategory(ctx context.Context, id, budgetID string) error

	// Query for spent amount sync (Issue #451)
	GetCategoriesMatchingTransaction(ctx context.Context, userID string, transactionCategory []string) ([]domain.BudgetCategory, error)
}

// budgetRepository implements BudgetRepository using PostgreSQL.
type budgetRepository struct {
	db *sql.DB
}

// NewBudgetRepository creates a new BudgetRepository instance.
func NewBudgetRepository(db *sql.DB) BudgetRepository {
	return &budgetRepository{db: db}
}

// Create creates a new budget in the database.
func (r *budgetRepository) Create(ctx context.Context, budget *domain.Budget) error {
	if budget.ID == "" {
		budget.ID = uuid.New().String()
	}

	now := time.Now()
	budget.CreatedAt = now
	budget.UpdatedAt = now

	if budget.CurrencyCode == "" {
		budget.CurrencyCode = "USD"
	}

	query := `
		INSERT INTO budgets (
			id, user_id, name, description, period, start_date, end_date,
			amount, currency_code, is_active, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`

	_, err := r.db.ExecContext(ctx, query,
		budget.ID,
		budget.UserID,
		budget.Name,
		budget.Description,
		budget.Period,
		budget.StartDate,
		budget.EndDate,
		budget.Amount,
		budget.CurrencyCode,
		budget.IsActive,
		budget.CreatedAt,
		budget.UpdatedAt,
	)

	if err != nil {
		if strings.Contains(err.Error(), "unique_budget_name_per_user") ||
			strings.Contains(err.Error(), "duplicate key") {
			return domain.ErrBudgetAlreadyExists
		}
		return err
	}

	return nil
}

// GetByID retrieves a budget by its ID with categories.
func (r *budgetRepository) GetByID(ctx context.Context, id string) (*domain.Budget, error) {
	query := `
		SELECT id, user_id, name, description, period, start_date, end_date,
			amount, currency_code, is_active, created_at, updated_at
		FROM budgets
		WHERE id = $1
	`

	budget := &domain.Budget{}
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&budget.ID,
		&budget.UserID,
		&budget.Name,
		&budget.Description,
		&budget.Period,
		&budget.StartDate,
		&budget.EndDate,
		&budget.Amount,
		&budget.CurrencyCode,
		&budget.IsActive,
		&budget.CreatedAt,
		&budget.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, domain.ErrBudgetNotFound
	}
	if err != nil {
		return nil, err
	}

	// Load categories
	categories, err := r.GetCategoriesByBudgetID(ctx, budget.ID)
	if err != nil {
		return nil, err
	}
	budget.Categories = categories

	return budget, nil
}

// GetByUserID retrieves all budgets for a specific user.
func (r *budgetRepository) GetByUserID(ctx context.Context, userID string) ([]*domain.Budget, error) {
	query := `
		SELECT id, user_id, name, description, period, start_date, end_date,
			amount, currency_code, is_active, created_at, updated_at
		FROM budgets
		WHERE user_id = $1
		ORDER BY created_at DESC
	`

	rows, err := r.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var budgets []*domain.Budget
	for rows.Next() {
		budget := &domain.Budget{}
		err := rows.Scan(
			&budget.ID,
			&budget.UserID,
			&budget.Name,
			&budget.Description,
			&budget.Period,
			&budget.StartDate,
			&budget.EndDate,
			&budget.Amount,
			&budget.CurrencyCode,
			&budget.IsActive,
			&budget.CreatedAt,
			&budget.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		budgets = append(budgets, budget)
	}

	if budgets == nil {
		budgets = []*domain.Budget{}
	}

	return budgets, nil
}

// GetActiveByUserID retrieves all active budgets for a user.
func (r *budgetRepository) GetActiveByUserID(ctx context.Context, userID string) ([]*domain.Budget, error) {
	query := `
		SELECT id, user_id, name, description, period, start_date, end_date,
			amount, currency_code, is_active, created_at, updated_at
		FROM budgets
		WHERE user_id = $1 AND is_active = true
		ORDER BY created_at DESC
	`

	rows, err := r.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var budgets []*domain.Budget
	for rows.Next() {
		budget := &domain.Budget{}
		err := rows.Scan(
			&budget.ID,
			&budget.UserID,
			&budget.Name,
			&budget.Description,
			&budget.Period,
			&budget.StartDate,
			&budget.EndDate,
			&budget.Amount,
			&budget.CurrencyCode,
			&budget.IsActive,
			&budget.CreatedAt,
			&budget.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		budgets = append(budgets, budget)
	}

	if budgets == nil {
		budgets = []*domain.Budget{}
	}

	return budgets, nil
}

// GetByName retrieves a budget by user ID and name.
func (r *budgetRepository) GetByName(ctx context.Context, userID, name string) (*domain.Budget, error) {
	query := `
		SELECT id, user_id, name, description, period, start_date, end_date,
			amount, currency_code, is_active, created_at, updated_at
		FROM budgets
		WHERE user_id = $1 AND name = $2
	`

	budget := &domain.Budget{}
	err := r.db.QueryRowContext(ctx, query, userID, name).Scan(
		&budget.ID,
		&budget.UserID,
		&budget.Name,
		&budget.Description,
		&budget.Period,
		&budget.StartDate,
		&budget.EndDate,
		&budget.Amount,
		&budget.CurrencyCode,
		&budget.IsActive,
		&budget.CreatedAt,
		&budget.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, domain.ErrBudgetNotFound
	}
	if err != nil {
		return nil, err
	}

	return budget, nil
}

// Update updates an existing budget.
func (r *budgetRepository) Update(ctx context.Context, budget *domain.Budget) error {
	budget.UpdatedAt = time.Now()

	query := `
		UPDATE budgets
		SET name = $1, description = $2, period = $3, start_date = $4,
			end_date = $5, amount = $6, currency_code = $7, is_active = $8,
			updated_at = $9
		WHERE id = $10 AND user_id = $11
	`

	result, err := r.db.ExecContext(ctx, query,
		budget.Name,
		budget.Description,
		budget.Period,
		budget.StartDate,
		budget.EndDate,
		budget.Amount,
		budget.CurrencyCode,
		budget.IsActive,
		budget.UpdatedAt,
		budget.ID,
		budget.UserID,
	)

	if err != nil {
		if strings.Contains(err.Error(), "unique_budget_name_per_user") ||
			strings.Contains(err.Error(), "duplicate key") {
			return domain.ErrBudgetAlreadyExists
		}
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return domain.ErrBudgetNotFound
	}

	return nil
}

// Delete removes a budget and its categories from the database.
func (r *budgetRepository) Delete(ctx context.Context, id, userID string) error {
	query := `DELETE FROM budgets WHERE id = $1 AND user_id = $2`

	result, err := r.db.ExecContext(ctx, query, id, userID)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return domain.ErrBudgetNotFound
	}

	return nil
}

// CountByUserID counts the total number of budgets for a user.
func (r *budgetRepository) CountByUserID(ctx context.Context, userID string) (int, error) {
	query := `SELECT COUNT(*) FROM budgets WHERE user_id = $1`

	var count int
	err := r.db.QueryRowContext(ctx, query, userID).Scan(&count)
	if err != nil {
		return 0, err
	}

	return count, nil
}

// CreateCategory creates a new budget category.
func (r *budgetRepository) CreateCategory(ctx context.Context, category *domain.BudgetCategory) error {
	if category.ID == "" {
		category.ID = uuid.New().String()
	}

	now := time.Now()
	category.CreatedAt = now
	category.UpdatedAt = now

	categoryJSON, err := json.Marshal(category.Category)
	if err != nil {
		return err
	}

	query := `
		INSERT INTO budget_categories (
			id, budget_id, category, allocated_amount, spent_amount, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
	`

	_, err = r.db.ExecContext(ctx, query,
		category.ID,
		category.BudgetID,
		categoryJSON,
		category.AllocatedAmount,
		category.SpentAmount,
		category.CreatedAt,
		category.UpdatedAt,
	)

	return err
}

// GetCategoriesByBudgetID retrieves all categories for a budget.
func (r *budgetRepository) GetCategoriesByBudgetID(ctx context.Context, budgetID string) ([]domain.BudgetCategory, error) {
	query := `
		SELECT id, budget_id, category, allocated_amount, spent_amount, created_at, updated_at
		FROM budget_categories
		WHERE budget_id = $1
		ORDER BY created_at ASC
	`

	rows, err := r.db.QueryContext(ctx, query, budgetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var categories []domain.BudgetCategory
	for rows.Next() {
		var bc domain.BudgetCategory
		var categoryJSON []byte

		err := rows.Scan(
			&bc.ID,
			&bc.BudgetID,
			&categoryJSON,
			&bc.AllocatedAmount,
			&bc.SpentAmount,
			&bc.CreatedAt,
			&bc.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}

		if err := json.Unmarshal(categoryJSON, &bc.Category); err != nil {
			return nil, err
		}

		categories = append(categories, bc)
	}

	if categories == nil {
		categories = []domain.BudgetCategory{}
	}

	return categories, nil
}

// UpdateCategory updates an existing budget category.
func (r *budgetRepository) UpdateCategory(ctx context.Context, category *domain.BudgetCategory) error {
	category.UpdatedAt = time.Now()

	categoryJSON, err := json.Marshal(category.Category)
	if err != nil {
		return err
	}

	query := `
		UPDATE budget_categories
		SET category = $1, allocated_amount = $2, spent_amount = $3, updated_at = $4
		WHERE id = $5 AND budget_id = $6
	`

	result, err := r.db.ExecContext(ctx, query,
		categoryJSON,
		category.AllocatedAmount,
		category.SpentAmount,
		category.UpdatedAt,
		category.ID,
		category.BudgetID,
	)

	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return domain.ErrBudgetCategoryNotFound
	}

	return nil
}

// UpdateCategorySpentAmount updates the spent_amount for a category.
// This is used by the budget sync service (Issue #451).
func (r *budgetRepository) UpdateCategorySpentAmount(ctx context.Context, categoryID string, spentAmount float64) error {
	query := `
		UPDATE budget_categories
		SET spent_amount = $1, updated_at = NOW()
		WHERE id = $2
	`

	result, err := r.db.ExecContext(ctx, query, spentAmount, categoryID)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return domain.ErrBudgetCategoryNotFound
	}

	return nil
}

// DeleteCategory removes a budget category.
func (r *budgetRepository) DeleteCategory(ctx context.Context, id, budgetID string) error {
	query := `DELETE FROM budget_categories WHERE id = $1 AND budget_id = $2`

	result, err := r.db.ExecContext(ctx, query, id, budgetID)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return domain.ErrBudgetCategoryNotFound
	}

	return nil
}

// GetCategoriesMatchingTransaction finds budget categories that match a transaction's category.
// This is used for recalculating spent amounts when transactions change (Issue #451).
func (r *budgetRepository) GetCategoriesMatchingTransaction(ctx context.Context, userID string, transactionCategory []string) ([]domain.BudgetCategory, error) {
	categoryJSON, err := json.Marshal(transactionCategory)
	if err != nil {
		return nil, err
	}

	// Find budget categories where the stored category matches the transaction category
	// Uses JSONB containment operator to check if categories match
	query := `
		SELECT bc.id, bc.budget_id, bc.category, bc.allocated_amount, bc.spent_amount, bc.created_at, bc.updated_at
		FROM budget_categories bc
		JOIN budgets b ON bc.budget_id = b.id
		WHERE b.user_id = $1 AND b.is_active = true AND bc.category @> $2
	`

	rows, err := r.db.QueryContext(ctx, query, userID, categoryJSON)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var categories []domain.BudgetCategory
	for rows.Next() {
		var bc domain.BudgetCategory
		var categoryJSONResult []byte

		err := rows.Scan(
			&bc.ID,
			&bc.BudgetID,
			&categoryJSONResult,
			&bc.AllocatedAmount,
			&bc.SpentAmount,
			&bc.CreatedAt,
			&bc.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}

		if err := json.Unmarshal(categoryJSONResult, &bc.Category); err != nil {
			return nil, err
		}

		categories = append(categories, bc)
	}

	if categories == nil {
		categories = []domain.BudgetCategory{}
	}

	return categories, nil
}
