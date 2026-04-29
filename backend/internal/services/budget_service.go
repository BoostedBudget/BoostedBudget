package services

import (
	"context"
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
}

// budgetService implements BudgetService
type budgetService struct {
	budgetRepo         repository.BudgetRepository
	customCategoryRepo repository.CustomCategoryRepository
	plaidCategoryRepo  PlaidCategoryGetter
	logger             *slog.Logger
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

	// Create budget from request
	budget := &domain.Budget{
		ID:             uuid.New().String(),
		UserID:         userID,
		Name:           req.Name,
		Description:    req.Description,
		Period:         req.Period,
		StartDate:      req.StartDate,
		EndDate:        req.EndDate,
		Amount:         reconcileAmount,
		ExpectedIncome: reconcileExpectedIncome,
		CurrencyCode:   req.CurrencyCode,
		IsActive:       true,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
		// Envelope budgeting fields (Issue #491)
		IncomeAmount:  req.IncomeAmount,
		IncomeSources: req.IncomeSources,
	}

	// Default currency if not provided
	if budget.CurrencyCode == "" {
		budget.CurrencyCode = "USD"
	}

	// Validate
	if err := budget.Validate(); err != nil {
		s.logger.Warn("budget validation failed",
			slog.String("user_id", userID),
			slog.String("error", err.Error()),
		)
		return nil, err
	}

	// Save to database
	if err := s.budgetRepo.Create(ctx, budget); err != nil {
		s.logger.Error("failed to create budget",
			slog.String("user_id", userID),
			slog.String("error", err.Error()),
		)
		return nil, err
	}

	// Create categories if provided
	for _, catReq := range req.Categories {
		category := &domain.BudgetCategory{
			ID:               uuid.New().String(),
			BudgetID:         budget.ID,
			CategoryKind:     catReq.CategoryKind,
			PFCPrimary:       catReq.PFCPrimary,
			PFCDetailed:      catReq.PFCDetailed,
			CustomCategoryID: catReq.CustomCategoryID,
			AllocatedAmount:  catReq.AllocatedAmount,
			SpentAmount:      0,
			CreatedAt:        time.Now(),
			UpdatedAt:        time.Now(),
			Tags:             catReq.Tags,
			MatchLogic:       catReq.MatchLogic,
		}

		if err := s.budgetRepo.CreateCategory(ctx, category); err != nil {
			s.logger.Error("failed to create budget category",
				slog.String("budget_id", budget.ID),
				slog.String("error", err.Error()),
			)
			return nil, err
		}

		budget.Categories = append(budget.Categories, *category)
	}

	s.logger.Info("budget created",
		slog.String("user_id", userID),
		slog.String("budget_id", budget.ID),
	)

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

	return budget, nil
}

// GetBudgets retrieves all budgets for a user.
func (s *budgetService) GetBudgets(ctx context.Context, userID string) ([]*domain.Budget, error) {
	return s.budgetRepo.GetByUserID(ctx, userID)
}

// GetActiveBudgets retrieves all active budgets for a user.
func (s *budgetService) GetActiveBudgets(ctx context.Context, userID string) ([]*domain.Budget, error) {
	return s.budgetRepo.GetActiveByUserID(ctx, userID)
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
				return budget, nil
			}
		case domain.BudgetPeriodMonthly:
			// For monthly, the budget is current if we're in a valid month period
			return budget, nil
		case domain.BudgetPeriodQuarterly:
			return budget, nil
		case domain.BudgetPeriodYearly:
			return budget, nil
		case domain.BudgetPeriodCustom:
			// Already checked end date above
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

	// Delete the budget (cascade deletes categories)
	if err := s.budgetRepo.Delete(ctx, budgetID, userID); err != nil {
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

	// Create the category
	category := &domain.BudgetCategory{
		ID:               uuid.New().String(),
		BudgetID:         budgetID,
		CategoryKind:     req.CategoryKind,
		PFCPrimary:       req.PFCPrimary,
		PFCDetailed:      req.PFCDetailed,
		CustomCategoryID: req.CustomCategoryID,
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

	// Verify category exists in budget
	found := false
	for _, cat := range budget.Categories {
		if cat.ID == categoryID {
			found = true
			break
		}
	}

	if !found {
		return domain.ErrBudgetCategoryNotFound
	}

	// Delete the category
	if err := s.budgetRepo.DeleteCategory(ctx, categoryID, budgetID); err != nil {
		s.logger.Error("failed to delete budget category",
			slog.String("category_id", categoryID),
			slog.String("error", err.Error()),
		)
		return err
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

