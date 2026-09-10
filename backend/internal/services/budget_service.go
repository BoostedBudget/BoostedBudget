package services

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/pfc"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// PlaidCategoryGetter is a minimal interface for retrieving Plaid categories.
// This allows BudgetService to depend only on what it needs from TransactionRepository.
type PlaidCategoryGetter interface {
	GetDistinctPlaidCategories(ctx context.Context, userID string) ([]string, error)
}

// ActiveGoalRepo is a minimal interface for fetching active savings goals.
// BudgetService uses this to carry-forward savings categories on budget creation (Issue #1300).
type ActiveGoalRepo interface {
	GetActiveByUserID(ctx context.Context, userID string) ([]*domain.SavingsGoal, error)
}

// GoalOwnershipChecker is a minimal interface for verifying savings goal ownership.
// BudgetService uses this to reject savings categories whose savings_goal_id doesn't
// belong to the requesting user — prevents IDOR access to another user's goals
// (Issue #1300, Codex C1).
type GoalOwnershipChecker interface {
	GetByIDForUser(ctx context.Context, id, userID string) (*domain.SavingsGoal, error)
}

// BudgetService defines the interface for budget business logic.
type BudgetService interface {
	// Budget CRUD
	CreateBudget(ctx context.Context, userID string, req *domain.CreateBudgetRequest) (*domain.Budget, error)
	GetBudget(ctx context.Context, userID, budgetID string) (*domain.Budget, error)
	GetBudgets(ctx context.Context, userID string) ([]*domain.Budget, error)
	GetActiveBudgets(ctx context.Context, userID string) ([]*domain.Budget, error)
	GetCurrentBudget(ctx context.Context, userID string) (*domain.Budget, error)
	UpdateBudget(ctx context.Context, userID, budgetID string, req *domain.UpdateBudgetRequest) (*domain.Budget, error)
	DeleteBudget(ctx context.Context, userID, budgetID string) error

	// Category CRUD
	AddCategory(ctx context.Context, userID, budgetID string, req *domain.CreateBudgetCategoryRequest) (*domain.BudgetCategory, error)
	UpdateCategory(ctx context.Context, userID, budgetID, categoryID string, req *domain.UpdateBudgetCategoryRequest) (*domain.BudgetCategory, error)
	DeleteCategory(ctx context.Context, userID, budgetID, categoryID string) error

	// Available categories (merged list of custom + Plaid)
	GetAvailableCategories(ctx context.Context, userID string) ([]string, error)

	// SetGoalRepo wires the optional active-goal repository for carry-forward and backfill.
	// When set, CreateBudget carries forward savings categories for all active goals
	// and BackfillManagedCategories becomes functional. Issue #1300.
	SetGoalRepo(r ActiveGoalRepo)

	// SetGoalOwnershipChecker wires the optional ownership checker used by AddCategory to
	// validate that a submitted savings_goal_id belongs to the authenticated user.
	// When set, AddCategory rejects savings categories pointing to another user's goal.
	// Issue #1300, Codex C1 (IDOR protection).
	SetGoalOwnershipChecker(r GoalOwnershipChecker)

	// SetAuditLogRepository wires the budget.deleted / budget_category.deleted audit entries,
	// written INSIDE the repository's own tombstone transaction via LogInTx. Nil-safe optional
	// dependency. Issue #1569, Task 8.
	SetAuditLogRepository(ar repository.AuditLogRepository)

	// BackfillManagedCategories creates missing managed savings categories for all
	// active goals across all active budgets, skipping tombstoned (budget, goal) pairs.
	// Idempotent — the unique index no-ops on duplicates. Called on sync and as an
	// admin/maintenance repair command. Issue #1300, Codex C7.
	BackfillManagedCategories(ctx context.Context, userID string) error
}

// budgetService implements BudgetService
type budgetService struct {
	budgetRepo         repository.BudgetRepository
	customCategoryRepo repository.CustomCategoryRepository
	plaidCategoryRepo  PlaidCategoryGetter
	logger             *slog.Logger
	// goalRepo is optional; when set, CreateBudget carries forward savings categories
	// for active goals and BackfillManagedCategories is functional (Issue #1300).
	goalRepo ActiveGoalRepo
	// goalOwnershipChecker is optional; when set, AddCategory verifies that the
	// savings_goal_id in a savings-kind request belongs to the authenticated user
	// before persisting (IDOR protection — Issue #1300, Codex C1).
	goalOwnershipChecker GoalOwnershipChecker
	// auditLogRepo records budget.deleted / budget_category.deleted entries inside the
	// repository's tombstone transaction (Issue #1569 Task 8). Nil-safe optional dependency.
	auditLogRepo repository.AuditLogRepository
}

// NewBudgetService creates a new BudgetService instance.
func NewBudgetService(
	budgetRepo repository.BudgetRepository,
	customCategoryRepo repository.CustomCategoryRepository,
	plaidCategoryRepo PlaidCategoryGetter,
	logger *slog.Logger,
) BudgetService {
	return &budgetService{
		budgetRepo:         budgetRepo,
		customCategoryRepo: customCategoryRepo,
		plaidCategoryRepo:  plaidCategoryRepo,
		logger:             logger,
	}
}

// CreateBudget creates a new budget for a user.
func (s *budgetService) CreateBudget(ctx context.Context, userID string, req *domain.CreateBudgetRequest) (*domain.Budget, error) {
	s.logger.Info("creating budget",
		slog.String("user_id", userID),
		slog.String("budget_name", req.Name),
		slog.String("period", string(req.Period)),
	)

	// Reconcile expected_income and amount: prefer expected_income if explicitly set (Issue #1048).
	reconcileAmount, reconcileExpectedIncome := req.Amount, req.Amount
	if req.ExpectedIncome != nil && *req.ExpectedIncome > 0 {
		reconcileAmount = *req.ExpectedIncome
		reconcileExpectedIncome = *req.ExpectedIncome
	}

	// Inherit series_id from ancestor if from_budget_id is set, else start own series (Issue #1300 M22/RG3)
	newID := uuid.New().String()
	seriesID := newID
	if req.FromBudgetID != nil {
		ancestor, err := s.budgetRepo.GetByID(ctx, *req.FromBudgetID)
		if err != nil {
			return nil, err
		}
		if ancestor.UserID != userID {
			return nil, domain.ErrBudgetNotFound
		}
		seriesID = ancestor.SeriesID
	}

	// Create budget from request. #1630 Task 9: StartDate/EndDate are domain.FlexibleDate
	// (accepts YYYY-MM-DD or RFC3339) on the request; Budget itself stays plain time.Time.
	var endDate *time.Time
	if req.EndDate != nil {
		t := req.EndDate.Time()
		endDate = &t
	}
	budget := &domain.Budget{
		ID:             newID,
		UserID:         userID,
		Name:           req.Name,
		Description:    req.Description,
		Period:         req.Period,
		StartDate:      req.StartDate.Time(),
		EndDate:        endDate,
		Amount:         reconcileAmount,
		ExpectedIncome: reconcileExpectedIncome,
		CurrencyCode:   req.CurrencyCode,
		IsActive:       true,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
		// Envelope budgeting fields (Issue #491)
		IncomeAmount:  req.IncomeAmount,
		IncomeSources: req.IncomeSources,
		// Series groups recurring budget periods; inherited from ancestor or set to own ID (Issue #1300 G5/M22).
		SeriesID: seriesID,
	}

	if budget.CurrencyCode == "" {
		budget.CurrencyCode = "USD"
	}

	if err := budget.Validate(); err != nil {
		s.logger.Warn("budget validation failed",
			slog.String("user_id", userID),
			slog.String("error", err.Error()),
		)
		return nil, err
	}

	// R4-3 (Codex C1 / IDOR): Pre-validate savings-goal ownership for ALL categories BEFORE
	// persisting the budget. The R3-4 fix placed this check inside the per-category loop after
	// budgetRepo.Create, so a failed IDOR check returned an error but left an orphaned budget
	// row already committed. Moving it here makes the operation atomic: no budget is ever
	// written if any category references another user's savings goal.
	for _, catReq := range req.Categories {
		if catReq.CategoryKind == "savings" && catReq.SavingsGoalID != nil {
			if s.goalOwnershipChecker == nil {
				// R5-1: fail closed — every path that calls CreateBudget must wire the
				// checker (HTTP path via SetGoalOwnershipChecker at startup; MCP path via
				// handler.SetGoalOwnershipChecker on the per-tx service). A nil checker
				// here indicates a mis-wired path, so we reject rather than silently allow.
				return nil, fmt.Errorf("ownership checker not configured: cannot validate savings_goal_id %s", *catReq.SavingsGoalID)
			}
			if _, err := s.goalOwnershipChecker.GetByIDForUser(ctx, *catReq.SavingsGoalID, userID); err != nil {
				return nil, err
			}
		}
	}

	// Issue #1540: reject a "custom" category referencing another user's custom_categories
	// row before persisting anything — the same IDOR protection the loop above gives
	// SavingsGoalID. GetLiveNamesByIDs is user-scoped AND tombstone-filtered
	// (custom_category_repository.go); any requested ID absent from the returned map is either
	// not owned by the caller or has been deleted. Issue #1569 Task 9 moved this off
	// GetNamesByIDs, which deliberately still resolves tombstoned names for DISPLAY — attaching
	// a new budget line to a deleted category would put it back in front of the user.
	var customCategoryIDs []string
	for _, catReq := range req.Categories {
		if catReq.CustomCategoryID != nil {
			customCategoryIDs = append(customCategoryIDs, *catReq.CustomCategoryID)
		}
	}
	if len(customCategoryIDs) > 0 {
		if s.customCategoryRepo == nil {
			// #1531: fail closed — every path that calls CreateBudget with a custom_category_id
			// must wire this repo (HTTP path at service construction; MCP path via the
			// tx-scoped repository.NewCustomCategoryRepository(tx) in budget.go). A nil repo
			// here indicates a mis-wired path; reject rather than nil-dereference (prior
			// behaviour) or silently skip the ownership check (would reopen the #1540 IDOR).
			return nil, fmt.Errorf("custom category repository not configured: cannot validate custom_category_id")
		}
		names, err := s.customCategoryRepo.GetLiveNamesByIDs(ctx, userID, customCategoryIDs)
		if err != nil {
			return nil, err
		}
		for _, catReq := range req.Categories {
			if catReq.CustomCategoryID != nil {
				if _, ok := names[*catReq.CustomCategoryID]; !ok {
					return nil, domain.ErrCategoryNotFound
				}
			}
		}
	}

	// R5-2: validate every category before persisting (matches AddCategory / UpdateCategory behaviour).
	// R5-3: build all categories in memory first so CreateWithCategories can persist them atomically.
	var categories []*domain.BudgetCategory
	var flatCategories []domain.BudgetCategory // Issue #1630 Task 4: parent-identity dedup check below
	for _, catReq := range req.Categories {
		category := &domain.BudgetCategory{
			ID:               uuid.New().String(),
			BudgetID:         budget.ID,
			CategoryKind:     catReq.CategoryKind,
			PFCPrimary:       catReq.PFCPrimary,
			PFCDetailed:      catReq.PFCDetailed,
			CustomCategoryID: catReq.CustomCategoryID,
			SavingsGoalID:    catReq.SavingsGoalID, // Issue #1300: savings bucket linkage
			AllocatedAmount:  catReq.AllocatedAmount,
			SpentAmount:      0,
			CreatedAt:        time.Now(),
			UpdatedAt:        time.Now(),
			Tags:             catReq.Tags,
			MatchLogic:       catReq.MatchLogic,
		}
		// R5-2: gate on domain invariants before any write.
		if err := category.Validate(); err != nil {
			return nil, err
		}
		// Issue #1630 Task 4: a single request can name the same parent identity twice
		// (e.g. two "FOOD_AND_DRINK" primary-only categories) — reject before persisting
		// anything, mirroring the check AddCategory runs against already-persisted rows.
		if domain.DuplicateParentIdentity(flatCategories, category) {
			return nil, domain.ErrDuplicateParentCategory
		}
		categories = append(categories, category)
		flatCategories = append(flatCategories, *category)
	}

	// R5-3: persist budget + user-provided categories atomically in a single transaction.
	// If any category INSERT fails the budget INSERT is rolled back too (no orphaned budget).
	// Best-effort carry-forward categories are outside this tx (Plan flag B — intentional).
	if err := s.budgetRepo.CreateWithCategories(ctx, budget, categories); err != nil {
		s.logger.Error("failed to create budget",
			slog.String("user_id", userID),
			slog.String("error", err.Error()),
		)
		return nil, err
	}

	// Mirror the in-memory category structs onto budget.Categories (repo may mutate them, e.g. set CreatedAt).
	for _, category := range categories {
		budget.Categories = append(budget.Categories, *category)
	}

	// Carry-forward savings categories (best-effort, unique index no-ops dupes — Issue #1300)
	if s.goalRepo != nil {
		goals, err := s.goalRepo.GetActiveByUserID(ctx, userID)
		if err != nil {
			s.logger.Warn("failed to fetch active goals for carry-forward",
				slog.String("budget_id", budget.ID),
				slog.String("error", err.Error()),
			)
		} else {
			for _, goal := range goals {
				detached, err := s.budgetRepo.IsGoalDetachedForSeries(ctx, budget.SeriesID, goal.ID)
				if err != nil {
					s.logger.Warn("carry-forward: failed to check tombstone",
						slog.String("budget_id", budget.ID),
						slog.String("series_id", budget.SeriesID),
						slog.String("goal_id", goal.ID),
						slog.String("error", err.Error()),
					)
					continue
				}
				if detached {
					continue
				}

				savingsCat := &domain.BudgetCategory{
					ID:              uuid.New().String(),
					BudgetID:        budget.ID,
					CategoryKind:    "savings",
					SavingsGoalID:   &goal.ID,
					AllocatedAmount: 0,
					AutoCreated:     true,
					ManagedByGoalID: &goal.ID,
					CreatedAt:       time.Now(),
					UpdatedAt:       time.Now(),
				}

				if err := s.budgetRepo.CreateCategory(ctx, savingsCat); err != nil {
					s.logger.Warn("carry-forward: failed to create savings category",
						slog.String("budget_id", budget.ID),
						slog.String("goal_id", goal.ID),
						slog.String("error", err.Error()),
					)
					continue
				}

				budget.Categories = append(budget.Categories, *savingsCat)
			}
		}
	}

	s.logger.Info("budget created",
		slog.String("user_id", userID),
		slog.String("budget_id", budget.ID),
	)

	enrichBudgetPFCDisplay(budget)
	return budget, nil
}

// GetBudget retrieves a budget by ID, verifying ownership.
func (s *budgetService) GetBudget(ctx context.Context, userID, budgetID string) (*domain.Budget, error) {
	budget, err := s.budgetRepo.GetByID(ctx, budgetID)
	if err != nil {
		return nil, err
	}

	// Verify ownership
	if budget.UserID != userID {
		return nil, domain.ErrBudgetNotFound
	}

	enrichBudgetPFCDisplay(budget)
	return budget, nil
}

// GetBudgets retrieves all budgets for a user.
func (s *budgetService) GetBudgets(ctx context.Context, userID string) ([]*domain.Budget, error) {
	budgets, err := s.budgetRepo.GetByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}
	for _, b := range budgets {
		cats, err := s.budgetRepo.GetCategoriesByBudgetID(ctx, b.ID)
		if err != nil {
			return nil, err
		}
		b.Categories = cats
		enrichBudgetPFCDisplay(b)
	}
	return budgets, nil
}

// GetActiveBudgets retrieves all active budgets for a user.
func (s *budgetService) GetActiveBudgets(ctx context.Context, userID string) ([]*domain.Budget, error) {
	budgets, err := s.budgetRepo.GetActiveByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}
	for _, b := range budgets {
		cats, err := s.budgetRepo.GetCategoriesByBudgetID(ctx, b.ID)
		if err != nil {
			return nil, err
		}
		b.Categories = cats
		enrichBudgetPFCDisplay(b)
	}
	return budgets, nil
}

// GetCurrentBudget retrieves the current active budget (where today is within the period).
func (s *budgetService) GetCurrentBudget(ctx context.Context, userID string) (*domain.Budget, error) {
	budgets, err := s.budgetRepo.GetActiveByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}

	now := time.Now()

	// Find budget where today is within the period
	for _, budget := range budgets {
		if !budget.IsActive {
			continue
		}

		// Check if now is after start date
		if now.Before(budget.StartDate) {
			continue
		}

		// If end date is set, check if now is before it
		if budget.EndDate != nil && now.After(*budget.EndDate) {
			continue
		}

		// For non-custom periods, calculate the end date based on period
		switch budget.Period {
		case domain.BudgetPeriodWeekly:
			endDate := budget.StartDate.AddDate(0, 0, 7)
			for now.After(endDate) {
				endDate = endDate.AddDate(0, 0, 7)
			}
			if now.After(budget.StartDate) {
				enrichBudgetPFCDisplay(budget)
				return budget, nil
			}
		case domain.BudgetPeriodMonthly:
			// For monthly, the budget is current if we're in a valid month period
			enrichBudgetPFCDisplay(budget)
			return budget, nil
		case domain.BudgetPeriodQuarterly:
			enrichBudgetPFCDisplay(budget)
			return budget, nil
		case domain.BudgetPeriodYearly:
			enrichBudgetPFCDisplay(budget)
			return budget, nil
		case domain.BudgetPeriodCustom:
			// Already checked end date above
			enrichBudgetPFCDisplay(budget)
			return budget, nil
		}
	}

	return nil, domain.ErrBudgetNotFound
}

// UpdateBudget updates an existing budget.
func (s *budgetService) UpdateBudget(ctx context.Context, userID, budgetID string, req *domain.UpdateBudgetRequest) (*domain.Budget, error) {
	s.logger.Info("updating budget",
		slog.String("user_id", userID),
		slog.String("budget_id", budgetID),
	)

	// Get existing budget
	budget, err := s.budgetRepo.GetByID(ctx, budgetID)
	if err != nil {
		return nil, err
	}

	// Verify ownership
	if budget.UserID != userID {
		return nil, domain.ErrBudgetNotFound
	}

	// Apply updates
	if req.Name != nil {
		budget.Name = *req.Name
	}
	if req.Description != nil {
		budget.Description = req.Description
	}
	if req.Period != nil {
		budget.Period = *req.Period
	}
	if req.StartDate != nil {
		budget.StartDate = *req.StartDate
	}
	if req.EndDate != nil {
		budget.EndDate = req.EndDate
	}
	// Reconcile expected_income and amount (Issue #1048): expected_income wins when set.
	if req.ExpectedIncome != nil && *req.ExpectedIncome > 0 {
		budget.ExpectedIncome = *req.ExpectedIncome
		budget.Amount = *req.ExpectedIncome // mirror for back-compat
	} else if req.Amount != nil {
		budget.Amount = *req.Amount
		budget.ExpectedIncome = *req.Amount // mirror
	}
	if req.IsActive != nil {
		budget.IsActive = *req.IsActive
	}
	// Envelope budgeting fields (Issue #491)
	if req.IncomeAmount != nil {
		budget.IncomeAmount = *req.IncomeAmount
	}
	if req.IncomeSources != nil {
		budget.IncomeSources = *req.IncomeSources
	}

	// Validate
	if err := budget.Validate(); err != nil {
		return nil, err
	}

	// Save changes
	if err := s.budgetRepo.Update(ctx, budget); err != nil {
		s.logger.Error("failed to update budget",
			slog.String("budget_id", budgetID),
			slog.String("error", err.Error()),
		)
		return nil, err
	}

	s.logger.Info("budget updated",
		slog.String("user_id", userID),
		slog.String("budget_id", budgetID),
	)

	enrichBudgetPFCDisplay(budget)
	return budget, nil
}

// DeleteBudget removes a budget.
func (s *budgetService) DeleteBudget(ctx context.Context, userID, budgetID string) error {
	s.logger.Info("deleting budget",
		slog.String("user_id", userID),
		slog.String("budget_id", budgetID),
	)

	// Get existing budget to verify ownership
	budget, err := s.budgetRepo.GetByID(ctx, budgetID)
	if err != nil {
		return err
	}

	// Verify ownership
	if budget.UserID != userID {
		return domain.ErrBudgetNotFound
	}

	// Tombstone the budget and its categories. rollover_history and budget_period_events are
	// retained; see BudgetRepository.Delete.
	if err := s.budgetRepo.Delete(ctx, budgetID, userID, s.auditLogRepo); err != nil {
		s.logger.Error("failed to delete budget",
			slog.String("budget_id", budgetID),
			slog.String("error", err.Error()),
		)
		return err
	}

	s.logger.Info("budget deleted",
		slog.String("user_id", userID),
		slog.String("budget_id", budgetID),
	)

	return nil
}

// AddCategory adds a category to a budget.
func (s *budgetService) AddCategory(ctx context.Context, userID, budgetID string, req *domain.CreateBudgetCategoryRequest) (*domain.BudgetCategory, error) {
	s.logger.Info("adding budget category",
		slog.String("user_id", userID),
		slog.String("budget_id", budgetID),
	)

	// Get budget to verify ownership
	budget, err := s.budgetRepo.GetByID(ctx, budgetID)
	if err != nil {
		return nil, err
	}

	if budget.UserID != userID {
		return nil, domain.ErrBudgetNotFound
	}

	// Note: Category validation is deferred to the domain model.
	// Categories may not exist yet (e.g., user-created) or may be added
	// dynamically, so we allow flexible category definitions.

	// IDOR check for savings: verify the goal belongs to the user (Issue #1300, Codex C1)
	if req.CategoryKind == "savings" && req.SavingsGoalID != nil {
		if s.goalOwnershipChecker == nil {
			// Fail closed: a mis-wired service must never silently allow cross-user goal links.
			return nil, fmt.Errorf("ownership checker not configured: cannot validate savings_goal_id %s", *req.SavingsGoalID)
		}
		if _, err := s.goalOwnershipChecker.GetByIDForUser(ctx, *req.SavingsGoalID, userID); err != nil {
			return nil, err
		}
	}

	// IDOR check for custom categories: verify the category belongs to the user (Issue #1540)
	// and is still live (Issue #1569 Task 9). Mirrors the savings-goal check above and
	// CreateBudget's batched version.
	if req.CustomCategoryID != nil {
		names, err := s.customCategoryRepo.GetLiveNamesByIDs(ctx, userID, []string{*req.CustomCategoryID})
		if err != nil {
			return nil, err
		}
		if _, ok := names[*req.CustomCategoryID]; !ok {
			return nil, domain.ErrCategoryNotFound
		}
	}

	// Create the category
	category := &domain.BudgetCategory{
		ID:               uuid.New().String(),
		BudgetID:         budgetID,
		CategoryKind:     req.CategoryKind,
		PFCPrimary:       req.PFCPrimary,
		PFCDetailed:      req.PFCDetailed,
		CustomCategoryID: req.CustomCategoryID,
		SavingsGoalID:    req.SavingsGoalID, // Issue #1300: savings bucket linkage (Codex C1)
		AllocatedAmount:  req.AllocatedAmount,
		SpentAmount:      0,
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
		Tags:             req.Tags,
		MatchLogic:       req.MatchLogic,
	}

	// Validate
	if err := category.Validate(); err != nil {
		return nil, err
	}

	// Issue #1630 Task 4: reject a second row sharing this budget's parent identity
	// (same pfc_primary primary-only row, or same custom_category_id) before persisting.
	// See domain.DuplicateParentIdentity for why an undetected collision is unsafe.
	if domain.DuplicateParentIdentity(budget.Categories, category) {
		return nil, domain.ErrDuplicateParentCategory
	}

	// Save to database
	if err := s.budgetRepo.CreateCategory(ctx, category); err != nil {
		s.logger.Error("failed to create budget category",
			slog.String("budget_id", budgetID),
			slog.String("error", err.Error()),
		)
		return nil, err
	}

	s.logger.Info("budget category added",
		slog.String("budget_id", budgetID),
		slog.String("category_id", category.ID),
	)

	category.PopulatePFCDisplay()
	return category, nil
}

// UpdateCategory updates a budget category.
func (s *budgetService) UpdateCategory(ctx context.Context, userID, budgetID, categoryID string, req *domain.UpdateBudgetCategoryRequest) (*domain.BudgetCategory, error) {
	s.logger.Info("updating budget category",
		slog.String("user_id", userID),
		slog.String("budget_id", budgetID),
		slog.String("category_id", categoryID),
	)

	// Get budget to verify ownership and find the category
	budget, err := s.budgetRepo.GetByID(ctx, budgetID)
	if err != nil {
		return nil, err
	}

	if budget.UserID != userID {
		return nil, domain.ErrBudgetNotFound
	}

	// Find the category
	var category *domain.BudgetCategory
	for i := range budget.Categories {
		if budget.Categories[i].ID == categoryID {
			category = &budget.Categories[i]
			break
		}
	}

	if category == nil {
		return nil, domain.ErrBudgetCategoryNotFound
	}

	// Apply updates
	if req.AllocatedAmount != nil {
		category.AllocatedAmount = *req.AllocatedAmount
	}
	// Flexible matching fields (Issue #491)
	if req.Tags != nil {
		category.Tags = *req.Tags
	}
	if req.MatchLogic != nil {
		category.MatchLogic = *req.MatchLogic
	}

	// Validate
	if err := category.Validate(); err != nil {
		return nil, err
	}

	// Save changes
	if err := s.budgetRepo.UpdateCategory(ctx, category); err != nil {
		s.logger.Error("failed to update budget category",
			slog.String("category_id", categoryID),
			slog.String("error", err.Error()),
		)
		return nil, err
	}

	s.logger.Info("budget category updated",
		slog.String("budget_id", budgetID),
		slog.String("category_id", categoryID),
	)

	category.PopulatePFCDisplay()
	return category, nil
}

// DeleteCategory removes a budget category.
func (s *budgetService) DeleteCategory(ctx context.Context, userID, budgetID, categoryID string) error {
	s.logger.Info("deleting budget category",
		slog.String("user_id", userID),
		slog.String("budget_id", budgetID),
		slog.String("category_id", categoryID),
	)

	// Get budget to verify ownership and find the category
	budget, err := s.budgetRepo.GetByID(ctx, budgetID)
	if err != nil {
		return err
	}

	if budget.UserID != userID {
		return domain.ErrBudgetNotFound
	}

	// Verify category exists in budget; capture it for tombstone logic.
	var foundCat *domain.BudgetCategory
	for i := range budget.Categories {
		if budget.Categories[i].ID == categoryID {
			foundCat = &budget.Categories[i]
			break
		}
	}

	if foundCat == nil {
		return domain.ErrBudgetCategoryNotFound
	}

	// Delete first, only tombstone after confirmed deletion to avoid orphan tombstones (Codex C6)
	if err := s.budgetRepo.DeleteCategory(ctx, categoryID, budgetID, s.auditLogRepo); err != nil {
		s.logger.Error("failed to delete budget category",
			slog.String("category_id", categoryID),
			slog.String("error", err.Error()),
		)
		return err
	}

	// Write tombstone keyed on series_id (non-fatal if fails; unique index prevents dupes)
	if foundCat.AutoCreated && foundCat.CategoryKind == "savings" && foundCat.SavingsGoalID != nil {
		if err := s.budgetRepo.InsertGoalDetach(ctx, budget.SeriesID, budgetID, *foundCat.SavingsGoalID); err != nil {
			s.logger.Warn("failed to write detach tombstone (category already deleted)",
				slog.String("budget_id", budgetID),
				slog.String("series_id", budget.SeriesID),
				slog.String("goal_id", *foundCat.SavingsGoalID),
				slog.String("error", err.Error()),
			)
		}
	}

	s.logger.Info("budget category deleted",
		slog.String("budget_id", budgetID),
		slog.String("category_id", categoryID),
	)

	return nil
}

// GetAvailableCategories returns a merged list of custom categories and curated PFCv2 primaries.
// Custom categories win on case-insensitive name collision with a curated primary.
// Result is sorted alphabetically.
func (s *budgetService) GetAvailableCategories(ctx context.Context, userID string) ([]string, error) {
	customCats, err := s.customCategoryRepo.GetByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}

	// Track which normalised display names the custom set already covers.
	customNorms := make(map[string]struct{}, len(customCats))
	var result []string

	for _, cat := range customCats {
		result = append(result, cat.Name)
		customNorms[strings.ToLower(strings.TrimSpace(cat.Name))] = struct{}{}
	}

	// Merge curated PFCv2 primaries; skip entries already covered by a custom category.
	// Return enum values (e.g. "FOOD_AND_DRINK") so BudgetCategoryForm can pass them
	// directly as pfc_primary to the API without a display→enum conversion step.
	// Check both the display form ("food and drink") and the enum form ("food_and_drink")
	// so a custom category named either way suppresses the curated duplicate.
	const uncategorizedNorm = "uncategorized"
	for _, primary := range pfc.AllPrimaries() {
		displayNorm := strings.ToLower(strings.TrimSpace(primary.Display))
		enumNorm := strings.ToLower(strings.TrimSpace(primary.Enum))
		if displayNorm == uncategorizedNorm || enumNorm == uncategorizedNorm {
			continue
		}
		_, displayExists := customNorms[displayNorm]
		_, enumExists := customNorms[enumNorm]
		if !displayExists && !enumExists {
			result = append(result, primary.Enum)
		}
	}

	sort.Strings(result)
	return result, nil
}

// SetGoalRepo wires the optional active-goal repository for carry-forward and backfill.
func (s *budgetService) SetGoalRepo(r ActiveGoalRepo) {
	s.goalRepo = r
}

// SetAuditLogRepository wires the optional audit-log repository. See the interface method.
func (s *budgetService) SetAuditLogRepository(ar repository.AuditLogRepository) {
	s.auditLogRepo = ar
}

// SetGoalOwnershipChecker wires the optional ownership checker used by AddCategory to
// validate that a submitted savings_goal_id belongs to the authenticated user.
// Issue #1300, Codex C1.
func (s *budgetService) SetGoalOwnershipChecker(r GoalOwnershipChecker) {
	s.goalOwnershipChecker = r
}

// BackfillManagedCategories creates missing managed savings categories for active goals
// across active budgets, skipping tombstoned pairs and existing categories (idempotent, Codex C7).
func (s *budgetService) BackfillManagedCategories(ctx context.Context, userID string) error {
	if s.goalRepo == nil {
		return nil
	}

	budgets, err := s.budgetRepo.GetActiveByUserID(ctx, userID)
	if err != nil {
		return err
	}

	goals, err := s.goalRepo.GetActiveByUserID(ctx, userID)
	if err != nil {
		return err
	}

	for _, budget := range budgets {
		// Build a set of goal IDs already covered by a savings category in this budget.
		coveredGoalIDs := make(map[string]struct{})
		for _, cat := range budget.Categories {
			if cat.CategoryKind == "savings" && cat.SavingsGoalID != nil {
				coveredGoalIDs[*cat.SavingsGoalID] = struct{}{}
			}
		}

		for _, goal := range goals {
			if _, covered := coveredGoalIDs[goal.ID]; covered {
				// Already exists — skip.
				continue
			}

			detached, err := s.budgetRepo.IsGoalDetachedForSeries(ctx, budget.SeriesID, goal.ID)
			if err != nil {
				s.logger.Error("backfill: failed to check tombstone",
					slog.String("budget_id", budget.ID),
					slog.String("series_id", budget.SeriesID),
					slog.String("goal_id", goal.ID),
					slog.String("error", err.Error()),
				)
				continue
			}
			if detached {
				continue
			}

			category := &domain.BudgetCategory{
				ID:              uuid.New().String(),
				BudgetID:        budget.ID,
				CategoryKind:    "savings",
				SavingsGoalID:   &goal.ID,
				AllocatedAmount: 0,
				AutoCreated:     true,
				ManagedByGoalID: &goal.ID,
				CreatedAt:       time.Now(),
				UpdatedAt:       time.Now(),
			}

			if err := s.budgetRepo.CreateCategory(ctx, category); err != nil {
				s.logger.Error("backfill: failed to create savings category",
					slog.String("budget_id", budget.ID),
					slog.String("goal_id", goal.ID),
					slog.String("error", err.Error()),
				)
			}
		}
	}

	return nil
}

// enrichBudgetPFCDisplay populates PFC display names on all BudgetCategories and
// IncomeSources within a Budget. Called after any Get/mutation that returns a Budget.
func enrichBudgetPFCDisplay(b *domain.Budget) {
	for i := range b.Categories {
		b.Categories[i].PopulatePFCDisplay()
	}
	for i := range b.IncomeSources {
		b.IncomeSources[i].PopulatePFCDisplay()
	}
}

