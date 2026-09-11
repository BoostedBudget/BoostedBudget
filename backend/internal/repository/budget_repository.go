package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	// Delete TOMBSTONES a budget and, with it, its categories. It sets
	// deleted_at/deleted_reason/deleted_by instead of issuing `DELETE FROM budgets`, so that
	// rollover_history and budget_period_events -- both ON DELETE CASCADE off budgets, and
	// rollover_history additionally CASCADE off budget_categories -- survive instead of being
	// destroyed alongside it. Issue #1569, Task 8.
	//
	// The categories are tombstoned in the SAME transaction with reason 'parent_deleted', because
	// fk_budget_categories_budget_id used to remove them outright.
	//
	// Writes a "budget.deleted" audit entry via auditLog.LogInTx inside that transaction; auditLog
	// may be nil. Returns ErrBudgetNotFound if the budget does not exist, belongs to another user,
	// or is already tombstoned.
	Delete(ctx context.Context, id, userID string, auditLog AuditLogRepository) error
	CountByUserID(ctx context.Context, userID string) (int, error)

	// BudgetCategory operations
	CreateCategory(ctx context.Context, category *domain.BudgetCategory) error
	GetCategoriesByBudgetID(ctx context.Context, budgetID string) ([]domain.BudgetCategory, error)
	// GetCategoriesByBudgetIDLocked locks budgetID's row (SELECT ... FOR UPDATE) inside a
	// transaction it opens and commits internally, re-reads that budget's live categories while
	// the lock is held, then releases it and returns the snapshot. Gives a caller composing a
	// cross-budget hierarchy check (e.g. custom_category_service's reparent-containment guard) the
	// same re-read-under-lock protocol AddCategoryChecked / UpdateCategoryChecked use for a single
	// add/edit, so it serializes against a concurrent AddCategory/UpdateCategory on THIS budget for
	// as long as this call takes (Issue #1630 B3). Returns domain.ErrBudgetNotFound if budgetID
	// does not exist or is tombstoned.
	//
	// ⚠ The lock is released when this call returns, not held across a subsequent unrelated write
	// (e.g. the custom_categories parent-linkage write a reparent persists afterward) — narrower
	// than AddCategoryChecked/UpdateCategoryChecked's single-transaction guarantee, but it closes
	// the same window for every budget individually while the multi-budget check runs, which is
	// what that check needs.
	GetCategoriesByBudgetIDLocked(ctx context.Context, budgetID string) ([]domain.BudgetCategory, error)
	// AddCategoryChecked atomically locks budgetID's row (SELECT ... FOR UPDATE), re-reads its
	// LIVE categories inside that lock, evaluates domain.DuplicateParentIdentity and
	// domain.EscapedNestedChild against that fresh snapshot, and only then inserts candidate — all
	// in one transaction. Closes the TOCTOU window between two concurrent adds that AddCategory's
	// old read(outside tx)-check(in memory)-write left open (Issue #1630 B3): two goroutines
	// racing to add categories that are individually fine but jointly violate one of the two
	// guards can no longer both succeed. Returns domain.ErrBudgetNotFound if budgetID does not
	// exist or is tombstoned, domain.ErrDuplicateParentCategory / domain.ErrEscapedNestedChild if
	// the locked snapshot trips a guard — both already map to a 4xx via
	// huma/errors.LoggingMapError, so the losing goroutine of a genuine race sees the same refusal
	// it would have seen sequentially, never a raw serialization failure surfacing as a 500.
	AddCategoryChecked(ctx context.Context, budgetID string, candidate *domain.BudgetCategory) (*domain.BudgetCategory, error)
	// UpdateCategoryChecked is AddCategoryChecked's counterpart for edits: it locks budgetID's row,
	// re-reads its live categories, locates categoryID among them, hands mutate a pointer to apply
	// request-specific field mutations (mutate should call category.Validate() itself if it wants
	// field-level validation), re-runs domain.EscapedNestedChild against the mutated row and its
	// FRESH siblings, and persists — all inside the same lock. mutate receives only the domain
	// object, never the transaction, so business logic stays in the service layer while
	// lock/lifecycle management stays in the repository (Issue #1630 B3 — deliberately narrower
	// than exposing a generic transaction callback). Returns domain.ErrBudgetNotFound /
	// domain.ErrBudgetCategoryNotFound when the budget or category is missing, whatever mutate
	// itself returns, or domain.ErrEscapedNestedChild if the mutated row escapes containment
	// against the locked snapshot.
	UpdateCategoryChecked(ctx context.Context, budgetID, categoryID string, mutate func(category *domain.BudgetCategory) error) (*domain.BudgetCategory, error)
	// GetCategoriesByBudgetIDs batch-loads categories for multiple budgets in a single query.
	// Returns a map from budget ID to its categories. Returns an empty map (not nil) when
	// budgetIDs is empty. Used by the period-close scanner for batched page loading (Issue #1271).
	GetCategoriesByBudgetIDs(ctx context.Context, budgetIDs []string) (map[string][]domain.BudgetCategory, error)
	// GetCategoryByIDAndUser returns a budget category only if it belongs to the given user (via the parent budget).
	// Returns an error if not found or not owned. Issue #1270: category ownership validation.
	GetCategoryByIDAndUser(ctx context.Context, categoryID, userID string) (*domain.BudgetCategory, error)
	UpdateCategory(ctx context.Context, category *domain.BudgetCategory) error
	UpdateCategorySpentAmount(ctx context.Context, categoryID string, spentAmount float64) error
	// DeleteCategory TOMBSTONES a budget category, so that rollover_history rows naming it as
	// their source_budget_category_id survive rather than being CASCADEd away by
	// fk_rollover_history_source_budget_category_id. Issue #1569, Task 8.
	//
	// Writes a "budget_category.deleted" audit entry via auditLog.LogInTx inside the same
	// transaction; auditLog may be nil. Returns ErrBudgetCategoryNotFound if the category does not
	// exist, is not in budgetID, or is already tombstoned.
	DeleteCategory(ctx context.Context, id, budgetID string, auditLog AuditLogRepository) error

	// Envelope budgeting income management (Issue #491)
	UpdateBudgetIncome(ctx context.Context, budgetID string, incomeAmount float64) error

	// GetAllActivePaginated returns all active budgets across all users, ordered by id ASC.
	// "Active" means is_active = true. Used by the period-close scanner (Issue #972).
	GetAllActivePaginated(ctx context.Context, limit int, offset int) ([]*domain.Budget, error)

	// GetTimezonesByUserIDs returns a map of userID → timezone string for the given user IDs.
	// Missing users are not included in the map; callers should default to "UTC" for absent keys.
	// Used by the period-close scanner (Issue #972) to avoid N+1 timezone lookups.
	GetTimezonesByUserIDs(ctx context.Context, userIDs []string) (map[string]string, error)

	// Savings goal category detach tombstone operations (Issue #1300, G5 — migration 000098).
	// Since migration 000098 the tombstone is keyed by (series_id, savings_goal_id) so a
	// user's detach intent persists across all budget periods that share the same series.

	// IsGoalDetachedForSeries returns true when a tombstone exists for (seriesID, goalID).
	IsGoalDetachedForSeries(ctx context.Context, seriesID, goalID string) (bool, error)
	// InsertGoalDetach writes a tombstone keyed on (seriesID, goalID). budgetID is stored
	// as a nullable audit-trail field indicating which period triggered the detach.
	InsertGoalDetach(ctx context.Context, seriesID, budgetID, goalID string) error
	// DeleteGoalDetach clears the tombstone for (seriesID, goalID) (user re-linked the goal).
	DeleteGoalDetach(ctx context.Context, seriesID, goalID string) error
	// GetDetachedGoalIDsForSeries returns all savings_goal_ids tombstoned for a series.
	GetDetachedGoalIDsForSeries(ctx context.Context, seriesID string) ([]string, error)

	// CreateWithCategories atomically creates a budget and its user-provided categories in a
	// single transaction. If any category INSERT fails the budget INSERT is rolled back too —
	// no orphaned budget rows are left behind (R5-3). Best-effort carry-forward categories
	// are intentionally outside this call (they use CreateCategory individually).
	//
	// When r.db is already a pgx.Tx (e.g. the MCP transactional path), db.Begin() creates a
	// savepoint so the call composes safely with an outer transaction.
	CreateWithCategories(ctx context.Context, budget *domain.Budget, categories []*domain.BudgetCategory) error
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

	// series_id defaults to own id for standalone budgets (Issue #1300 G5).
	if budget.SeriesID == "" {
		budget.SeriesID = budget.ID
	}

	query := `
		INSERT INTO budgets (
			id, user_id, name, description, period, start_date, end_date,
			amount, currency_code, is_active, created_at, updated_at,
			income_amount, income_sources, expected_income, series_id
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
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
		budget.SeriesID,
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
			income_amount, income_sources, expected_income, series_id
		FROM budgets
		WHERE id = $1 AND deleted_at IS NULL
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
		&budget.SeriesID,
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
			income_amount, income_sources, expected_income, series_id
		FROM budgets
		WHERE user_id = $1 AND deleted_at IS NULL
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
			&budget.SeriesID,
		)
		if err != nil {
			return nil, err
		}

		// Unmarshal income_sources from JSON (Issue #491)
		sources, err := unmarshalIncomeSources(incomeSourcesJSON)
		if err != nil {
			return nil, err
		}
		budget.IncomeSources = sources

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
			income_amount, income_sources, expected_income, series_id
		FROM budgets
		WHERE user_id = $1 AND is_active = true AND deleted_at IS NULL
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
			&budget.SeriesID,
		)
		if err != nil {
			return nil, err
		}

		// Unmarshal income_sources from JSON (Issue #491)
		sources, err := unmarshalIncomeSources(incomeSourcesJSON)
		if err != nil {
			return nil, err
		}
		budget.IncomeSources = sources

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
			income_amount, income_sources, expected_income, series_id
		FROM budgets
		WHERE user_id = $1 AND name = $2 AND deleted_at IS NULL
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
		&budget.SeriesID,
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
		WHERE id = $13 AND user_id = $14 AND deleted_at IS NULL
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

// Delete implements BudgetRepository.Delete — see that method's doc comment for why this is a
// tombstone rather than a DELETE.
func (r *budgetRepository) Delete(ctx context.Context, id, userID string, auditLog AuditLogRepository) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after a successful commit is a no-op; its error is not actionable

	ct, err := tx.Exec(ctx, `
		UPDATE budgets
		SET deleted_at = NOW(), deleted_reason = $3, deleted_by = $4, updated_at = NOW()
		WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL
	`, id, userID, domain.SoftDeleteReasonUser, domain.SoftDeletedByUser)
	if err != nil {
		return fmt.Errorf("delete budget %s: %w", id, err)
	}
	if ct.RowsAffected() == 0 {
		return domain.ErrBudgetNotFound
	}

	if _, err := tx.Exec(ctx, `
		UPDATE budget_categories
		SET deleted_at = NOW(), deleted_reason = $2, deleted_by = $3, updated_at = NOW()
		WHERE budget_id = $1 AND deleted_at IS NULL
	`, id, domain.SoftDeleteReasonParent, domain.SoftDeletedByUser); err != nil {
		return fmt.Errorf("delete budget %s: tombstoning its categories: %w", id, err)
	}

	if auditLog != nil {
		if err := auditLog.LogInTx(ctx, tx, "budget.deleted", "budget", id); err != nil {
			return fmt.Errorf("delete budget %s: audit log: %w", id, err)
		}
	}

	return tx.Commit(ctx)
}

// CountByUserID counts the total number of budgets for a user.
func (r *budgetRepository) CountByUserID(ctx context.Context, userID string) (int, error) {
	query := `SELECT COUNT(*) FROM budgets WHERE user_id = $1 AND deleted_at IS NULL`

	var count int
	err := r.db.QueryRow(ctx, query, userID).Scan(&count)
	if err != nil {
		return 0, err
	}

	return count, nil
}

// CreateCategory creates a new budget category.
func (r *budgetRepository) CreateCategory(ctx context.Context, category *domain.BudgetCategory) error {
	return r.createCategoryQuerier(ctx, r.db, category)
}

// createCategoryQuerier is CreateCategory's body, parameterized over Querier so AddCategoryChecked
// (Issue #1630 B3) can run the identical insert inside its own locked transaction.
func (r *budgetRepository) createCategoryQuerier(ctx context.Context, q Querier, category *domain.BudgetCategory) error {
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
			tags, match_logic,
			savings_goal_id, auto_created, managed_by_goal_id
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
	`

	_, err = q.Exec(ctx, query,
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
		category.SavingsGoalID,   // $13: nullable FK to savings_goals (Issue #1300)
		category.AutoCreated,     // $14: bool — auto-created by savings goal linkage
		category.ManagedByGoalID, // $15: nullable FK — which goal manages this category
	)

	return err
}

// lockBudgetRowInTx locks budgetID's row (SELECT ... FOR UPDATE) inside tx, blocking until any
// other transaction holding the same lock (AddCategoryChecked, UpdateCategoryChecked, or another
// call composing several of these) commits or rolls back. Returns domain.ErrBudgetNotFound if
// budgetID does not exist or is already tombstoned. Issue #1630 B3.
func (r *budgetRepository) lockBudgetRowInTx(ctx context.Context, tx pgx.Tx, budgetID string) error {
	var locked string
	err := tx.QueryRow(ctx, `SELECT id FROM budgets WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`, budgetID).Scan(&locked)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrBudgetNotFound
		}
		return err
	}
	return nil
}

// GetCategoriesByBudgetIDLocked implements BudgetRepository.GetCategoriesByBudgetIDLocked — see
// that method's doc comment.
func (r *budgetRepository) GetCategoriesByBudgetIDLocked(ctx context.Context, budgetID string) ([]domain.BudgetCategory, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after a successful commit is a no-op

	if err := r.lockBudgetRowInTx(ctx, tx, budgetID); err != nil {
		return nil, err
	}

	categories, err := r.getCategoriesByBudgetIDQuerier(ctx, tx, budgetID)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return categories, nil
}

// AddCategoryChecked implements BudgetRepository.AddCategoryChecked — see that method's doc
// comment.
func (r *budgetRepository) AddCategoryChecked(ctx context.Context, budgetID string, candidate *domain.BudgetCategory) (*domain.BudgetCategory, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after a successful commit is a no-op

	if err := r.lockBudgetRowInTx(ctx, tx, budgetID); err != nil {
		return nil, err
	}

	categories, err := r.getCategoriesByBudgetIDQuerier(ctx, tx, budgetID)
	if err != nil {
		return nil, err
	}

	if domain.DuplicateParentIdentity(categories, candidate) {
		return nil, domain.ErrDuplicateParentCategory
	}
	if domain.EscapedNestedChild(categories, candidate) {
		return nil, fmt.Errorf("%w: match_logic %q on this category escapes its hierarchical parent's containment envelope — a parent whose own match_logic is category_only/or only fully rolls up children whose match_logic is category_only/and; use a top-level (non-nested) category instead if this one must match tag_only or or", domain.ErrEscapedNestedChild, candidate.MatchLogic)
	}

	if err := r.createCategoryQuerier(ctx, tx, candidate); err != nil {
		// Defense-in-depth: unique_budget_category_discriminated (migration 000083) backstops the
		// identity half of this guard at the DB level too. The FOR UPDATE lock above should make
		// this branch unreachable in practice, but a losing writer must still surface a 4xx, not a
		// raw constraint-violation 500, if it is ever reached.
		if strings.Contains(err.Error(), "unique_budget_category_discriminated") || strings.Contains(err.Error(), "duplicate key") {
			return nil, domain.ErrDuplicateParentCategory
		}
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return candidate, nil
}

// UpdateCategoryChecked implements BudgetRepository.UpdateCategoryChecked — see that method's doc
// comment.
func (r *budgetRepository) UpdateCategoryChecked(ctx context.Context, budgetID, categoryID string, mutate func(category *domain.BudgetCategory) error) (*domain.BudgetCategory, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after a successful commit is a no-op

	if err := r.lockBudgetRowInTx(ctx, tx, budgetID); err != nil {
		return nil, err
	}

	categories, err := r.getCategoriesByBudgetIDQuerier(ctx, tx, budgetID)
	if err != nil {
		return nil, err
	}

	var category *domain.BudgetCategory
	siblings := make([]domain.BudgetCategory, 0, len(categories))
	for i := range categories {
		if categories[i].ID == categoryID {
			c := categories[i]
			category = &c
		} else {
			siblings = append(siblings, categories[i])
		}
	}
	if category == nil {
		return nil, domain.ErrBudgetCategoryNotFound
	}

	if err := mutate(category); err != nil {
		return nil, err
	}

	if domain.EscapedNestedChild(siblings, category) {
		return nil, fmt.Errorf("%w: match_logic %q on this category escapes its hierarchical parent's containment envelope — a parent whose own match_logic is category_only/or only fully rolls up children whose match_logic is category_only/and; use a top-level (non-nested) category instead if this one must match tag_only or or", domain.ErrEscapedNestedChild, category.MatchLogic)
	}

	if err := r.updateCategoryQuerier(ctx, tx, category); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return category, nil
}

// GetCategoriesByBudgetID retrieves all categories for a budget.
func (r *budgetRepository) GetCategoriesByBudgetID(ctx context.Context, budgetID string) ([]domain.BudgetCategory, error) {
	return r.getCategoriesByBudgetIDQuerier(ctx, r.db, budgetID)
}

// getCategoriesByBudgetIDQuerier is GetCategoriesByBudgetID's body, parameterized over Querier so
// AddCategoryChecked / UpdateCategoryChecked / GetCategoriesByBudgetIDLocked (Issue #1630 B3) can
// run the identical read INSIDE an open transaction, against a budget row they hold locked, rather
// than duplicating this query.
func (r *budgetRepository) getCategoriesByBudgetIDQuerier(ctx context.Context, q Querier, budgetID string) ([]domain.BudgetCategory, error) {
	// Issue #1203: join custom_categories for parent linkage (sub-allocation detection).
	// Issue #1300: join savings_goals for display name so the API returns savings_goal_name
	// and the frontend never shows a raw UUID as the category label.
	// Issue #1630 Task B2: a SECOND join hop (gp) resolves cc's own parent's parent_pfc_primary
	// — i.e. this row's GRANDPARENT PFC root, when cc itself is nested under a PFC primary via
	// parent_id rather than being a direct child of it. Mirrors customCategoryPathJoins()
	// (analytics_category_path.go) walked one hop further; needed so
	// BudgetCategory.CustomGrandparentPFCPrimary can recognize a transitive sub-allocation
	// pair the same way buildCategoryClause's widened roll-up now sums it.
	query := `
		SELECT bc.id, bc.budget_id, bc.category_kind, bc.pfc_primary, bc.pfc_detailed, bc.custom_category_id,
			bc.allocated_amount, bc.spent_amount, bc.created_at, bc.updated_at,
			bc.tags, bc.match_logic,
			cc.parent_id, cc.parent_pfc_primary, gp.parent_pfc_primary AS grandparent_pfc_primary,
			bc.savings_goal_id, bc.auto_created, bc.managed_by_goal_id,
			sg.name AS savings_goal_name
		-- tombstone-exempt(custom_categories cc, custom_categories gp): the cc and gp joins resolve the PARENT
		-- and GRANDPARENT LINKAGE of a budget line that is itself already tombstone-filtered
		-- (bc.deleted_at IS NULL). DeleteWithReassignment tombstones a category's budget lines in
		-- the same transaction as the category, so a live bc never names a tombstoned cc, and cc's
		-- own parent (gp) is subject to the identical invariant one hop up; adding
		-- AND cc.deleted_at IS NULL / AND gp.deleted_at IS NULL would buy nothing and would
		-- silently blank sub-allocation detection if that invariant ever slipped. #1569 T9.
		FROM budget_categories bc
		LEFT JOIN custom_categories cc ON cc.id = bc.custom_category_id
		LEFT JOIN custom_categories gp ON gp.id = cc.parent_id
		LEFT JOIN savings_goals sg ON sg.id = bc.savings_goal_id AND sg.deleted_at IS NULL
		WHERE bc.budget_id = $1 AND bc.deleted_at IS NULL
		ORDER BY bc.created_at ASC
	`

	rows, err := q.Query(ctx, query, budgetID)
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
			&bc.CustomParentID,
			&bc.CustomParentPFCPrimary,
			&bc.CustomGrandparentPFCPrimary,
			&bc.SavingsGoalID,
			&bc.AutoCreated,
			&bc.ManagedByGoalID,
			&bc.SavingsGoalName, // populated from LEFT JOIN savings_goals (Issue #1300)
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

// GetCategoriesByBudgetIDs batch-loads categories for multiple budgets in a single query.
// Uses the same SELECT as GetCategoriesByBudgetID with WHERE bc.budget_id = ANY($1).
// Returns an empty map (not nil) when budgetIDs is empty. Issue #1271 Item 6.
func (r *budgetRepository) GetCategoriesByBudgetIDs(ctx context.Context, budgetIDs []string) (map[string][]domain.BudgetCategory, error) {
	if len(budgetIDs) == 0 {
		return map[string][]domain.BudgetCategory{}, nil
	}

	// Issue #1300: join savings_goals for display name (same as GetCategoriesByBudgetID).
	// Issue #1630 Task B2: second join hop (gp) for CustomGrandparentPFCPrimary — see
	// GetCategoriesByBudgetID's comment for the full reasoning; identical shape here.
	query := `
		SELECT bc.id, bc.budget_id, bc.category_kind, bc.pfc_primary, bc.pfc_detailed, bc.custom_category_id,
			bc.allocated_amount, bc.spent_amount, bc.created_at, bc.updated_at,
			bc.tags, bc.match_logic,
			cc.parent_id, cc.parent_pfc_primary, gp.parent_pfc_primary AS grandparent_pfc_primary,
			bc.savings_goal_id, bc.auto_created, bc.managed_by_goal_id,
			sg.name AS savings_goal_name
		-- tombstone-exempt(custom_categories cc, custom_categories gp): identical to GetCategoriesByBudgetID above —
		-- the cc and gp joins are parent/grandparent linkage for an already tombstone-filtered bc row. #1569 T9.
		FROM budget_categories bc
		LEFT JOIN custom_categories cc ON cc.id = bc.custom_category_id
		LEFT JOIN custom_categories gp ON gp.id = cc.parent_id
		LEFT JOIN savings_goals sg ON sg.id = bc.savings_goal_id AND sg.deleted_at IS NULL
		WHERE bc.budget_id = ANY($1) AND bc.deleted_at IS NULL
		ORDER BY bc.budget_id ASC, bc.created_at ASC
	`

	rows, err := r.db.Query(ctx, query, budgetIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string][]domain.BudgetCategory)
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
			&bc.CustomParentID,
			&bc.CustomParentPFCPrimary,
			&bc.CustomGrandparentPFCPrimary,
			&bc.SavingsGoalID,
			&bc.AutoCreated,
			&bc.ManagedByGoalID,
			&bc.SavingsGoalName, // populated from LEFT JOIN savings_goals (Issue #1300)
		)
		if err != nil {
			return nil, err
		}

		if err := json.Unmarshal(tagsJSON, &bc.Tags); err != nil {
			return nil, err
		}

		result[bc.BudgetID] = append(result[bc.BudgetID], bc)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return result, nil
}

// UpdateCategory updates an existing budget category.
func (r *budgetRepository) UpdateCategory(ctx context.Context, category *domain.BudgetCategory) error {
	return r.updateCategoryQuerier(ctx, r.db, category)
}

// updateCategoryQuerier is UpdateCategory's body, parameterized over Querier so
// UpdateCategoryChecked (Issue #1630 B3) can run the identical update inside its own locked
// transaction.
func (r *budgetRepository) updateCategoryQuerier(ctx context.Context, q Querier, category *domain.BudgetCategory) error {
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
		WHERE id = $10 AND budget_id = $11 AND deleted_at IS NULL
	`

	ct, err := q.Exec(ctx, query,
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
		WHERE id = $2 AND deleted_at IS NULL
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

// DeleteCategory implements BudgetRepository.DeleteCategory — see that method's doc comment for
// why this is a tombstone rather than a DELETE.
func (r *budgetRepository) DeleteCategory(ctx context.Context, id, budgetID string, auditLog AuditLogRepository) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after a successful commit is a no-op; its error is not actionable

	ct, err := tx.Exec(ctx, `
		UPDATE budget_categories
		SET deleted_at = NOW(), deleted_reason = $3, deleted_by = $4, updated_at = NOW()
		WHERE id = $1 AND budget_id = $2 AND deleted_at IS NULL
	`, id, budgetID, domain.SoftDeleteReasonUser, domain.SoftDeletedByUser)
	if err != nil {
		return fmt.Errorf("delete budget category %s: %w", id, err)
	}
	if ct.RowsAffected() == 0 {
		return domain.ErrBudgetCategoryNotFound
	}

	if auditLog != nil {
		if err := auditLog.LogInTx(ctx, tx, "budget_category.deleted", "budget_category", id); err != nil {
			return fmt.Errorf("delete budget category %s: audit log: %w", id, err)
		}
	}

	return tx.Commit(ctx)
}

// UpdateBudgetIncome updates only income_amount for a budget (the actual received sum).
// expected_income is the user's planned target and must NOT be overwritten on every sync.
func (r *budgetRepository) UpdateBudgetIncome(ctx context.Context, budgetID string, incomeAmount float64) error {
	query := `
		UPDATE budgets
		SET income_amount = $1, updated_at = NOW()
		WHERE id = $2 AND deleted_at IS NULL
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

// GetAllActivePaginated returns all active budgets across all users ordered by id ASC.
// Used by the budget period-close scanner (Issue #972) to fan out across all budgets.
func (r *budgetRepository) GetAllActivePaginated(ctx context.Context, limit int, offset int) ([]*domain.Budget, error) {
	query := `
		SELECT id, user_id, name, description, period, start_date, end_date,
			amount, currency_code, is_active, created_at, updated_at,
			income_amount, income_sources, expected_income, series_id
		FROM budgets
		WHERE is_active = true AND deleted_at IS NULL
		ORDER BY id ASC
		LIMIT $1 OFFSET $2
	`

	rows, err := r.db.Query(ctx, query, limit, offset)
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
			&budget.SeriesID,
		)
		if err != nil {
			return nil, err
		}

		sources, err := unmarshalIncomeSources(incomeSourcesJSON)
		if err != nil {
			return nil, err
		}
		budget.IncomeSources = sources

		budgets = append(budgets, budget)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	if budgets == nil {
		budgets = []*domain.Budget{}
	}

	return budgets, nil
}

// GetTimezonesByUserIDs returns a map of userID → timezone for the given set of user IDs.
// Uses COALESCE so users with a NULL timezone column fall back to "UTC".
// Missing user IDs produce no map entry; callers should default to "UTC" for absent keys.
func (r *budgetRepository) GetTimezonesByUserIDs(ctx context.Context, userIDs []string) (map[string]string, error) {
	if len(userIDs) == 0 {
		return map[string]string{}, nil
	}

	query := `SELECT id, COALESCE(timezone, 'UTC') FROM users WHERE id = ANY($1)`

	rows, err := r.db.Query(ctx, query, userIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]string, len(userIDs))
	for rows.Next() {
		var userID, tz string
		if err := rows.Scan(&userID, &tz); err != nil {
			return nil, err
		}
		result[userID] = tz
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return result, nil
}

// GetCategoryByIDAndUser returns a budget category only if it is owned by the given user.
// Ownership is established through the parent budget (budget_categories has no user_id column).
// Returns an error if the category does not exist or belongs to a different user.
// Issue #1270: category ownership validation for automation rule conditions.
func (r *budgetRepository) GetCategoryByIDAndUser(ctx context.Context, categoryID, userID string) (*domain.BudgetCategory, error) {
	query := `
		SELECT bc.id, bc.budget_id, bc.category_kind, bc.pfc_primary, bc.pfc_detailed, bc.custom_category_id,
			bc.allocated_amount, bc.spent_amount, bc.created_at, bc.updated_at,
			bc.tags, bc.match_logic
		FROM budget_categories bc
		JOIN budgets b ON b.id = bc.budget_id AND b.user_id = $2 AND b.deleted_at IS NULL
		WHERE bc.id = $1 AND bc.deleted_at IS NULL
	`
	var bc domain.BudgetCategory
	var tagsJSON []byte

	err := r.db.QueryRow(ctx, query, categoryID, userID).Scan(
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
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrBudgetCategoryNotFound
		}
		return nil, err
	}
	return &bc, nil
}

// =============================================================================
// Savings goal category detach tombstone operations (Issue #1300, G5 — migration 000098)
// All methods now key on series_id so detach intent persists across budget periods.
// =============================================================================

// IsGoalDetachedForSeries returns true when a (series_id, savings_goal_id) tombstone exists.
func (r *budgetRepository) IsGoalDetachedForSeries(ctx context.Context, seriesID, goalID string) (bool, error) {
	query := `
		SELECT EXISTS(
			SELECT 1 FROM savings_goal_category_detach
			WHERE series_id = $1 AND savings_goal_id = $2
		)
	`
	var exists bool
	if err := r.db.QueryRow(ctx, query, seriesID, goalID).Scan(&exists); err != nil {
		return false, err
	}
	return exists, nil
}

// InsertGoalDetach writes a tombstone keyed on (series_id, savings_goal_id).
// budgetID is stored as a nullable audit-trail column.
// Idempotent: ON CONFLICT (series_id, savings_goal_id) DO NOTHING.
func (r *budgetRepository) InsertGoalDetach(ctx context.Context, seriesID, budgetID, goalID string) error {
	query := `
		INSERT INTO savings_goal_category_detach (series_id, budget_id, savings_goal_id)
		VALUES ($1, $2, $3)
		ON CONFLICT (series_id, savings_goal_id) DO NOTHING
	`
	_, err := r.db.Exec(ctx, query, seriesID, budgetID, goalID)
	return err
}

// DeleteGoalDetach clears the tombstone for (series_id, savings_goal_id).
func (r *budgetRepository) DeleteGoalDetach(ctx context.Context, seriesID, goalID string) error {
	query := `
		DELETE FROM savings_goal_category_detach
		WHERE series_id = $1 AND savings_goal_id = $2
	`
	_, err := r.db.Exec(ctx, query, seriesID, goalID)
	return err
}

// GetDetachedGoalIDsForSeries returns all savings_goal_ids tombstoned for a given series.
func (r *budgetRepository) GetDetachedGoalIDsForSeries(ctx context.Context, seriesID string) ([]string, error) {
	query := `
		SELECT savings_goal_id::text
		FROM savings_goal_category_detach
		WHERE series_id = $1
	`
	rows, err := r.db.Query(ctx, query, seriesID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var goalIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		goalIDs = append(goalIDs, id)
	}
	return goalIDs, rows.Err()
}

// CreateWithCategories atomically inserts a budget and all its user-provided categories.
// If any category INSERT fails the budget INSERT is rolled back; no orphaned budget rows
// are left behind (R5-3 / Codex C1 / Issue #1300).
//
// When r.db is already a pgx.Tx (e.g. the MCP transactional path in budget.go), db.Begin()
// opens a savepoint so this call composes safely with an outer transaction.
func (r *budgetRepository) CreateWithCategories(ctx context.Context, budget *domain.Budget, categories []*domain.BudgetCategory) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	txRepo := &budgetRepository{db: tx}

	if err := txRepo.Create(ctx, budget); err != nil {
		return err
	}

	for _, cat := range categories {
		if err := txRepo.CreateCategory(ctx, cat); err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}
