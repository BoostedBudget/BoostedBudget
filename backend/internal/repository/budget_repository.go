package repository

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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
	GetCategoriesMatchingTransaction(ctx context.Context, userID string, effective *domain.EffectiveCategory) ([]domain.BudgetCategory, error)

	// Envelope budgeting income management (Issue #491)
	UpdateBudgetIncome(ctx context.Context, budgetID string, incomeAmount float64) error
	AddIncomeSource(ctx context.Context, budgetID string, source domain.IncomeSource) error
	RemoveIncomeSource(ctx context.Context, budgetID string, sourceIndex int) error

	// Envelope budgeting rollover operations (Issue #36: Rule Executors)
	// AdjustCategoryAllocatedAmount adjusts the allocated amount by delta (can be positive or negative)
	AdjustCategoryAllocatedAmount(ctx context.Context, categoryID string, delta float64) error
}

// budgetRepository implements BudgetRepository using PostgreSQL.
type budgetRepository struct {
	db DB
}

// NewBudgetRepository creates a new BudgetRepository instance.
func NewBudgetRepository(db DB) BudgetRepository {
	return &budgetRepository{db: db}
}

// unmarshalIncomeSources deserializes income_sources JSON. Returns empty slice on nil.
func unmarshalIncomeSources(data []byte) ([]domain.IncomeSource, error) {
	if data == nil {
		return []domain.IncomeSource{}, nil
	}
	var sources []domain.IncomeSource
	if err := json.Unmarshal(data, &sources); err != nil {
		return nil, err
	}
	return sources, nil
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

	// Marshal income_sources to JSON (Issue #491)
	incomeSources := budget.IncomeSources
	if incomeSources == nil {
		incomeSources = []domain.IncomeSource{}
	}
	incomeSourcesJSON, err := json.Marshal(incomeSources)
	if err != nil {
		return err
	}

	// Mirror expected_income to Amount when not explicitly set (Issue #1048).
	if budget.ExpectedIncome == 0 {
		budget.ExpectedIncome = budget.Amount
	}

	query := `
		INSERT INTO budgets (
			id, user_id, name, description, period, start_date, end_date,
			amount, currency_code, is_active, created_at, updated_at,
			income_amount, income_sources, expected_income
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
	`

	_, err = r.db.Exec(ctx, query,
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
		budget.IncomeAmount,
		incomeSourcesJSON,
		budget.ExpectedIncome,
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
			amount, currency_code, is_active, created_at, updated_at,
			income_amount, income_sources, expected_income
		FROM budgets
		WHERE id = $1
	`

	budget := &domain.Budget{}
	var incomeSourcesJSON []byte
	err := r.db.QueryRow(ctx, query, id).Scan(
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
		&budget.IncomeAmount,
		&incomeSourcesJSON,
		&budget.ExpectedIncome,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrBudgetNotFound
	}
	if err != nil {
		return nil, err
	}

	// Unmarshal income_sources from JSON (Issue #491)
	sources, err := unmarshalIncomeSources(incomeSourcesJSON)
	if err != nil {
		return nil, err
	}
	budget.IncomeSources = sources

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
			amount, currency_code, is_active, created_at, updated_at,
			income_amount, income_sources, expected_income
		FROM budgets
		WHERE user_id = $1
		ORDER BY created_at DESC
	`

	rows, err := r.db.Query(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var budgets []*domain.Budget
	for rows.Next() {
		budget := &domain.Budget{}
		var incomeSourcesJSON []byte
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
			&budget.IncomeAmount,
			&incomeSourcesJSON,
			&budget.ExpectedIncome,
		)
		if err != nil {
			return nil, err
		}

		// Unmarshal income_sources from JSON (Issue #491)
		if err := json.Unmarshal(incomeSourcesJSON, &budget.IncomeSources); err != nil {
			return nil, err
		}

		budgets = append(budgets, budget)
	}

	if budgets == nil {
		budgets = make([]*domain.Budget, 0)
	}

	return budgets, nil
}

// GetActiveByUserID retrieves all active budgets for a user.
func (r *budgetRepository) GetActiveByUserID(ctx context.Context, userID string) ([]*domain.Budget, error) {
	query := `
		SELECT id, user_id, name, description, period, start_date, end_date,
			amount, currency_code, is_active, created_at, updated_at,
			income_amount, income_sources, expected_income
		FROM budgets
		WHERE user_id = $1 AND is_active = true
		ORDER BY created_at DESC
	`

	rows, err := r.db.Query(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var budgets []*domain.Budget
	for rows.Next() {
		budget := &domain.Budget{}
		var incomeSourcesJSON []byte
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
			&budget.IncomeAmount,
			&incomeSourcesJSON,
			&budget.ExpectedIncome,
		)
		if err != nil {
			return nil, err
		}

		// Unmarshal income_sources from JSON (Issue #491)
		if err := json.Unmarshal(incomeSourcesJSON, &budget.IncomeSources); err != nil {
			return nil, err
		}

		budgets = append(budgets, budget)
	}

	if budgets == nil {
		budgets = make([]*domain.Budget, 0)
	}

	return budgets, nil
}

// GetByName retrieves a budget by user ID and name.
func (r *budgetRepository) GetByName(ctx context.Context, userID, name string) (*domain.Budget, error) {
	query := `
		SELECT id, user_id, name, description, period, start_date, end_date,
			amount, currency_code, is_active, created_at, updated_at,
			income_amount, income_sources, expected_income
		FROM budgets
		WHERE user_id = $1 AND name = $2
	`

	budget := &domain.Budget{}
	var incomeSourcesJSON []byte
	err := r.db.QueryRow(ctx, query, userID, name).Scan(
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
		&budget.IncomeAmount,
		&incomeSourcesJSON,
		&budget.ExpectedIncome,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrBudgetNotFound
	}
	if err != nil {
		return nil, err
	}

	// Unmarshal income_sources from JSON (Issue #491)
	sources, err := unmarshalIncomeSources(incomeSourcesJSON)
	if err != nil {
		return nil, err
	}
	budget.IncomeSources = sources

	return budget, nil
}

// Update updates an existing budget.
func (r *budgetRepository) Update(ctx context.Context, budget *domain.Budget) error {
	budget.UpdatedAt = time.Now()

	// Marshal income_sources to JSON (Issue #491)
	incomeSources := budget.IncomeSources
	if incomeSources == nil {
		incomeSources = []domain.IncomeSource{}
	}
	incomeSourcesJSON, err := json.Marshal(incomeSources)
	if err != nil {
		return err
	}

	// Mirror expected_income to Amount when not explicitly set (Issue #1048).
	if budget.ExpectedIncome == 0 {
		budget.ExpectedIncome = budget.Amount
	}

	query := `
		UPDATE budgets
		SET name = $1, description = $2, period = $3, start_date = $4,
			end_date = $5, amount = $6, currency_code = $7, is_active = $8,
			updated_at = $9, income_amount = $10, income_sources = $11,
			expected_income = $12
		WHERE id = $13 AND user_id = $14
	`

	ct, err := r.db.Exec(ctx, query,
		budget.Name,
		budget.Description,
		budget.Period,
		budget.StartDate,
		budget.EndDate,
		budget.Amount,
		budget.CurrencyCode,
		budget.IsActive,
		budget.UpdatedAt,
		budget.IncomeAmount,
		incomeSourcesJSON,
		budget.ExpectedIncome,
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

	if ct.RowsAffected() == 0 {
		return domain.ErrBudgetNotFound
	}

	return nil
}

// Delete removes a budget and its categories from the database.
func (r *budgetRepository) Delete(ctx context.Context, id, userID string) error {
	query := `DELETE FROM budgets WHERE id = $1 AND user_id = $2`

	ct, err := r.db.Exec(ctx, query, id, userID)
	if err != nil {
		return err
	}

	if ct.RowsAffected() == 0 {
		return domain.ErrBudgetNotFound
	}

	return nil
}

// CountByUserID counts the total number of budgets for a user.
func (r *budgetRepository) CountByUserID(ctx context.Context, userID string) (int, error) {
	query := `SELECT COUNT(*) FROM budgets WHERE user_id = $1`

	var count int
	err := r.db.QueryRow(ctx, query, userID).Scan(&count)
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

	// Marshal tags to JSON (Issue #491)
	tags := category.Tags
	if tags == nil {
		tags = []string{}
	}
	tagsJSON, err := json.Marshal(tags)
	if err != nil {
		return err
	}

	// Default match_logic to category_only if empty (Issue #491)
	matchLogic := category.MatchLogic
	if matchLogic == "" {
		matchLogic = domain.MatchLogicCategoryOnly
	}

	query := `
		INSERT INTO budget_categories (
			id, budget_id, category_kind, pfc_primary, pfc_detailed, custom_category_id,
			allocated_amount, spent_amount, created_at, updated_at,
			tags, match_logic
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`

	_, err = r.db.Exec(ctx, query,
		category.ID,
		category.BudgetID,
		category.CategoryKind,
		category.PFCPrimary,
		category.PFCDetailed,
		category.CustomCategoryID,
		category.AllocatedAmount,
		category.SpentAmount,
		category.CreatedAt,
		category.UpdatedAt,
		tagsJSON,
		matchLogic,
	)

	return err
}

// GetCategoriesByBudgetID retrieves all categories for a budget.
func (r *budgetRepository) GetCategoriesByBudgetID(ctx context.Context, budgetID string) ([]domain.BudgetCategory, error) {
	query := `
		SELECT id, budget_id, category_kind, pfc_primary, pfc_detailed, custom_category_id,
			allocated_amount, spent_amount, created_at, updated_at,
			tags, match_logic
		FROM budget_categories
		WHERE budget_id = $1
		ORDER BY created_at ASC
	`

	rows, err := r.db.Query(ctx, query, budgetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var categories []domain.BudgetCategory
	for rows.Next() {
		var bc domain.BudgetCategory
		var tagsJSON []byte

		err := rows.Scan(
			&bc.ID,
			&bc.BudgetID,
			&bc.CategoryKind,
			&bc.PFCPrimary,
			&bc.PFCDetailed,
			&bc.CustomCategoryID,
			&bc.AllocatedAmount,
			&bc.SpentAmount,
			&bc.CreatedAt,
			&bc.UpdatedAt,
			&tagsJSON,
			&bc.MatchLogic,
		)
		if err != nil {
			return nil, err
		}

		// Unmarshal tags from JSON (Issue #491)
		if err := json.Unmarshal(tagsJSON, &bc.Tags); err != nil {
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

	// Marshal tags to JSON (Issue #491)
	tags := category.Tags
	if tags == nil {
		tags = []string{}
	}
	tagsJSON, err := json.Marshal(tags)
	if err != nil {
		return err
	}

	// Default match_logic to category_only if empty (Issue #491)
	matchLogic := category.MatchLogic
	if matchLogic == "" {
		matchLogic = domain.MatchLogicCategoryOnly
	}

	query := `
		UPDATE budget_categories
		SET category_kind = $1, pfc_primary = $2, pfc_detailed = $3, custom_category_id = $4,
			allocated_amount = $5, spent_amount = $6, updated_at = $7,
			tags = $8, match_logic = $9
		WHERE id = $10 AND budget_id = $11
	`

	ct, err := r.db.Exec(ctx, query,
		category.CategoryKind,
		category.PFCPrimary,
		category.PFCDetailed,
		category.CustomCategoryID,
		category.AllocatedAmount,
		category.SpentAmount,
		category.UpdatedAt,
		tagsJSON,
		matchLogic,
		category.ID,
		category.BudgetID,
	)

	if err != nil {
		return err
	}

	if ct.RowsAffected() == 0 {
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

	ct, err := r.db.Exec(ctx, query, spentAmount, categoryID)
	if err != nil {
		return err
	}

	if ct.RowsAffected() == 0 {
		return domain.ErrBudgetCategoryNotFound
	}

	return nil
}

// DeleteCategory removes a budget category.
func (r *budgetRepository) DeleteCategory(ctx context.Context, id, budgetID string) error {
	query := `DELETE FROM budget_categories WHERE id = $1 AND budget_id = $2`

	ct, err := r.db.Exec(ctx, query, id, budgetID)
	if err != nil {
		return err
	}

	if ct.RowsAffected() == 0 {
		return domain.ErrBudgetCategoryNotFound
	}

	return nil
}

// GetCategoriesMatchingTransaction finds budget categories that match a transaction's category.
// This is used for recalculating spent amounts when transactions change (Issue #451).
// Note: This method currently only matches by category. Future enhancement could use match_logic
// and tags for more flexible matching (Issue #491).
func (r *budgetRepository) GetCategoriesMatchingTransaction(ctx context.Context, userID string, effective *domain.EffectiveCategory) ([]domain.BudgetCategory, error) {
	if effective == nil {
		return []domain.BudgetCategory{}, nil
	}

	var query string
	var args []interface{}

	switch effective.Kind {
	case domain.EffectiveCategoryKindPFC:
		query = `
			SELECT bc.id, bc.budget_id, bc.category_kind, bc.pfc_primary, bc.pfc_detailed,
				bc.custom_category_id, bc.allocated_amount, bc.spent_amount, bc.created_at, bc.updated_at,
				bc.tags, bc.match_logic
			FROM budget_categories bc
			JOIN budgets b ON bc.budget_id = b.id
			WHERE b.user_id = $1 AND b.is_active = true
			  AND bc.category_kind = 'pfc' AND bc.pfc_primary = $2
		`
		args = []interface{}{userID, effective.PFCPrimary}
	case domain.EffectiveCategoryKindCustom:
		query = `
			SELECT bc.id, bc.budget_id, bc.category_kind, bc.pfc_primary, bc.pfc_detailed,
				bc.custom_category_id, bc.allocated_amount, bc.spent_amount, bc.created_at, bc.updated_at,
				bc.tags, bc.match_logic
			FROM budget_categories bc
			JOIN budgets b ON bc.budget_id = b.id
			WHERE b.user_id = $1 AND b.is_active = true
			  AND bc.category_kind = 'custom' AND bc.custom_category_id::text = $2
		`
		args = []interface{}{userID, effective.CustomCategoryID}
	default:
		return []domain.BudgetCategory{}, nil
	}

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var categories []domain.BudgetCategory
	for rows.Next() {
		var bc domain.BudgetCategory
		var tagsJSON []byte

		err := rows.Scan(
			&bc.ID,
			&bc.BudgetID,
			&bc.CategoryKind,
			&bc.PFCPrimary,
			&bc.PFCDetailed,
			&bc.CustomCategoryID,
			&bc.AllocatedAmount,
			&bc.SpentAmount,
			&bc.CreatedAt,
			&bc.UpdatedAt,
			&tagsJSON,
			&bc.MatchLogic,
		)
		if err != nil {
			return nil, err
		}

		// Unmarshal tags from JSON (Issue #491)
		if err := json.Unmarshal(tagsJSON, &bc.Tags); err != nil {
			return nil, err
		}

		categories = append(categories, bc)
	}

	if categories == nil {
		categories = []domain.BudgetCategory{}
	}

	return categories, nil
}

// UpdateBudgetIncome updates only income_amount for a budget (the actual received sum).
// expected_income is the user's planned target and must NOT be overwritten on every sync.
func (r *budgetRepository) UpdateBudgetIncome(ctx context.Context, budgetID string, incomeAmount float64) error {
	query := `
		UPDATE budgets
		SET income_amount = $1, updated_at = NOW()
		WHERE id = $2
	`

	ct, err := r.db.Exec(ctx, query, incomeAmount, budgetID)
	if err != nil {
		return err
	}

	if ct.RowsAffected() == 0 {
		return domain.ErrBudgetNotFound
	}

	return nil
}

// AddIncomeSource appends a new income source to a budget's income_sources array.
// This is used for envelope budgeting income tracking (Issue #491).
func (r *budgetRepository) AddIncomeSource(ctx context.Context, budgetID string, source domain.IncomeSource) error {
	sourceJSON, err := json.Marshal(source)
	if err != nil {
		return err
	}

	// Use JSONB concatenation to append the new source
	query := `
		UPDATE budgets
		SET income_sources = income_sources || $1::jsonb, updated_at = NOW()
		WHERE id = $2
	`

	ct, err := r.db.Exec(ctx, query, sourceJSON, budgetID)
	if err != nil {
		return err
	}

	if ct.RowsAffected() == 0 {
		return domain.ErrBudgetNotFound
	}

	return nil
}

// RemoveIncomeSource removes an income source at the specified index from a budget's income_sources array.
// This is used for envelope budgeting income tracking (Issue #491).
func (r *budgetRepository) RemoveIncomeSource(ctx context.Context, budgetID string, sourceIndex int) error {
	// Use JSONB array removal by index
	// jsonb_set with the element at index removed
	query := `
		UPDATE budgets
		SET income_sources = (
			SELECT COALESCE(jsonb_agg(elem), '[]'::jsonb)
			FROM jsonb_array_elements(income_sources) WITH ORDINALITY arr(elem, idx)
			WHERE idx != $1 + 1
		), updated_at = NOW()
		WHERE id = $2
	`

	ct, err := r.db.Exec(ctx, query, sourceIndex, budgetID)
	if err != nil {
		return err
	}

	if ct.RowsAffected() == 0 {
		return domain.ErrBudgetNotFound
	}

	return nil
}

// AdjustCategoryAllocatedAmount adjusts the allocated_amount by a delta value.
// This is used for envelope budgeting rollovers (Issue #36: Rule Executors).
// Delta can be positive (increase allocation) or negative (decrease allocation).
func (r *budgetRepository) AdjustCategoryAllocatedAmount(ctx context.Context, categoryID string, delta float64) error {
	query := `
		UPDATE budget_categories
		SET allocated_amount = allocated_amount + $1, updated_at = $2
		WHERE id = $3
	`

	ct, err := r.db.Exec(ctx, query, delta, time.Now(), categoryID)
	if err != nil {
		return err
	}

	if ct.RowsAffected() == 0 {
		return domain.ErrBudgetCategoryNotFound
	}

	return nil
}
