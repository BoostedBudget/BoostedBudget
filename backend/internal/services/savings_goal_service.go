package services

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// AccountGetter is a minimal interface for retrieving user accounts.
// This allows SavingsGoalService to validate account ownership without
// depending on the full AccountRepository.
type AccountGetter interface {
	GetByUserID(ctx context.Context, userID string) ([]domain.Account, error)
}

// GoalAutoCreateBudgetRepo is a minimal interface for auto-creating savings categories
// in active budgets when a new savings goal is created (Issue #1300).
type GoalAutoCreateBudgetRepo interface {
	GetActiveByUserID(ctx context.Context, userID string) ([]*domain.Budget, error)
	CreateCategory(ctx context.Context, category *domain.BudgetCategory) error
	IsGoalDetachedForBudget(ctx context.Context, budgetID, goalID string) (bool, error)
}

// SavingsGoalService defines the interface for savings goal business logic.
type SavingsGoalService interface {
	// Goal CRUD
	CreateGoal(ctx context.Context, userID string, req *domain.CreateSavingsGoalRequest) (*domain.SavingsGoal, error)
	GetGoal(ctx context.Context, userID, goalID string) (*domain.SavingsGoal, error)
	GetGoals(ctx context.Context, userID string) ([]*domain.SavingsGoal, error)
	GetActiveGoals(ctx context.Context, userID string) ([]*domain.SavingsGoal, error)
	UpdateGoal(ctx context.Context, userID, goalID string, req *domain.UpdateSavingsGoalRequest) (*domain.SavingsGoal, error)
	DeleteGoal(ctx context.Context, userID, goalID string) error

	// SetBudgetRepo wires the optional budget repository for auto-creating savings categories
	// in active budgets when a goal is created. If not set, auto-create is skipped. Issue #1300.
	SetBudgetRepo(repo GoalAutoCreateBudgetRepo)
}

// savingsGoalService implements SavingsGoalService
type savingsGoalService struct {
	goalRepo    repository.SavingsGoalRepository
	accountRepo AccountGetter
	logger      *slog.Logger
	// budgetRepo is optional; when set, CreateGoal auto-creates savings categories
	// in all active budgets for the new goal (best-effort, Issue #1300).
	budgetRepo GoalAutoCreateBudgetRepo
}

// NewSavingsGoalService creates a new SavingsGoalService instance.
func NewSavingsGoalService(
	goalRepo repository.SavingsGoalRepository,
	accountRepo AccountGetter,
	logger *slog.Logger,
) SavingsGoalService {
	return &savingsGoalService{
		goalRepo:    goalRepo,
		accountRepo: accountRepo,
		logger:      logger,
	}
}

// CreateGoal creates a new savings goal for a user.
func (s *savingsGoalService) CreateGoal(ctx context.Context, userID string, req *domain.CreateSavingsGoalRequest) (*domain.SavingsGoal, error) {
	s.logger.Info("creating savings goal",
		slog.String("user_id", userID),
		slog.String("goal_name", req.Name),
		slog.String("account_id", req.AccountID),
	)

	// Create goal from request
	goal := &domain.SavingsGoal{
		ID:            uuid.New().String(),
		UserID:        userID,
		Name:          req.Name,
		Description:   req.Description,
		TargetAmount:  req.TargetAmount,
		CurrentAmount: req.CurrentAmount,
		CurrencyCode:  req.CurrencyCode,
		AccountID:     req.AccountID,
		Deadline:      req.Deadline,
		IsCompleted:   false,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	// Default currency if not provided
	if goal.CurrencyCode == "" {
		goal.CurrencyCode = "USD"
	}

	// Validate goal fields first
	if err := goal.Validate(); err != nil {
		s.logger.Warn("savings goal validation failed",
			slog.String("user_id", userID),
			slog.String("error", err.Error()),
		)
		return nil, err
	}

	// Issue #1109: Validate account ownership and capture stable UserAccountID for re-link stability.
	account, err := s.validateAccountOwnershipWithAccount(ctx, userID, req.AccountID)
	if err != nil {
		return nil, err
	}
	if account != nil && account.UserAccountID != nil {
		goal.UserAccountID = account.UserAccountID
	}

	// Save to database
	if err := s.goalRepo.Create(ctx, goal); err != nil {
		s.logger.Error("failed to create savings goal",
			slog.String("user_id", userID),
			slog.String("error", err.Error()),
		)
		return nil, err
	}

	// Auto-create savings categories in all active budgets (Issue #1300).
	// Best-effort: NOT in the goal-insert transaction; unique index no-ops dupes.
	if s.budgetRepo != nil {
		s.autoCreateSavingsCategories(ctx, userID, goal.ID)
	}

	s.logger.Info("savings goal created",
		slog.String("user_id", userID),
		slog.String("goal_id", goal.ID),
	)

	return goal, nil
}

// SetBudgetRepo wires the optional budget repository for auto-creating savings categories.
func (s *savingsGoalService) SetBudgetRepo(repo GoalAutoCreateBudgetRepo) {
	s.budgetRepo = repo
}

// autoCreateSavingsCategories fans out a savings category insert to each active budget
// for which the (budget, goal) pair is not tombstoned. Errors are logged and ignored
// (best-effort per plan — goal creation already succeeded at this point).
func (s *savingsGoalService) autoCreateSavingsCategories(ctx context.Context, userID, goalID string) {
	budgets, err := s.budgetRepo.GetActiveByUserID(ctx, userID)
	if err != nil {
		s.logger.Warn("auto-create: failed to fetch active budgets",
			slog.String("goal_id", goalID),
			slog.String("error", err.Error()),
		)
		return
	}

	for _, budget := range budgets {
		detached, err := s.budgetRepo.IsGoalDetachedForBudget(ctx, budget.ID, goalID)
		if err != nil {
			s.logger.Warn("auto-create: failed to check tombstone",
				slog.String("budget_id", budget.ID),
				slog.String("goal_id", goalID),
				slog.String("error", err.Error()),
			)
			continue
		}
		if detached {
			continue
		}

		goalIDCopy := goalID
		category := &domain.BudgetCategory{
			ID:              uuid.New().String(),
			BudgetID:        budget.ID,
			CategoryKind:    "savings",
			SavingsGoalID:   &goalIDCopy,
			AllocatedAmount: 0,
			AutoCreated:     true,
			ManagedByGoalID: &goalIDCopy,
			CreatedAt:       time.Now(),
			UpdatedAt:       time.Now(),
		}

		if err := s.budgetRepo.CreateCategory(ctx, category); err != nil {
			s.logger.Warn("auto-create: failed to create savings category",
				slog.String("budget_id", budget.ID),
				slog.String("goal_id", goalID),
				slog.String("error", err.Error()),
			)
		}
	}
}

// GetGoal retrieves a savings goal by ID, verifying ownership.
func (s *savingsGoalService) GetGoal(ctx context.Context, userID, goalID string) (*domain.SavingsGoal, error) {
	goal, err := s.goalRepo.GetByID(ctx, goalID)
	if err != nil {
		return nil, err
	}

	// Verify ownership
	if goal.UserID != userID {
		return nil, domain.ErrSavingsGoalNotFound
	}

	return goal, nil
}

// GetGoals retrieves all savings goals for a user.
func (s *savingsGoalService) GetGoals(ctx context.Context, userID string) ([]*domain.SavingsGoal, error) {
	return s.goalRepo.GetByUserID(ctx, userID)
}

// GetActiveGoals retrieves all active (not completed) savings goals for a user.
func (s *savingsGoalService) GetActiveGoals(ctx context.Context, userID string) ([]*domain.SavingsGoal, error) {
	return s.goalRepo.GetActiveByUserID(ctx, userID)
}

// UpdateGoal updates an existing savings goal.
func (s *savingsGoalService) UpdateGoal(ctx context.Context, userID, goalID string, req *domain.UpdateSavingsGoalRequest) (*domain.SavingsGoal, error) {
	s.logger.Info("updating savings goal",
		slog.String("user_id", userID),
		slog.String("goal_id", goalID),
	)

	// Get existing goal
	goal, err := s.goalRepo.GetByID(ctx, goalID)
	if err != nil {
		return nil, err
	}

	// Verify ownership
	if goal.UserID != userID {
		return nil, domain.ErrSavingsGoalNotFound
	}

	// If changing account, validate new account ownership and refresh UserAccountID
	// Issue #1109: B.5 — refresh to stable identity rather than potentially replaced account_id
	if req.AccountID != nil && *req.AccountID != goal.AccountID {
		newAccount, err := s.validateAccountOwnershipWithAccount(ctx, userID, *req.AccountID)
		if err != nil {
			return nil, err
		}
		if newAccount != nil {
			goal.UserAccountID = newAccount.UserAccountID
		}
	}

	// Apply updates
	if req.Name != nil {
		goal.Name = *req.Name
	}
	if req.Description != nil {
		goal.Description = req.Description
	}
	if req.TargetAmount != nil {
		goal.TargetAmount = *req.TargetAmount
	}
	if req.CurrentAmount != nil {
		goal.CurrentAmount = *req.CurrentAmount
	}
	if req.AccountID != nil {
		goal.AccountID = *req.AccountID
	}
	if req.Deadline != nil {
		goal.Deadline = req.Deadline
	}

	// Validate
	if err := goal.Validate(); err != nil {
		return nil, err
	}

	// Save changes
	if err := s.goalRepo.Update(ctx, goal); err != nil {
		s.logger.Error("failed to update savings goal",
			slog.String("goal_id", goalID),
			slog.String("error", err.Error()),
		)
		return nil, err
	}

	s.logger.Info("savings goal updated",
		slog.String("user_id", userID),
		slog.String("goal_id", goalID),
	)

	return goal, nil
}

// DeleteGoal removes a savings goal.
func (s *savingsGoalService) DeleteGoal(ctx context.Context, userID, goalID string) error {
	s.logger.Info("deleting savings goal",
		slog.String("user_id", userID),
		slog.String("goal_id", goalID),
	)

	// Get existing goal to verify ownership
	goal, err := s.goalRepo.GetByID(ctx, goalID)
	if err != nil {
		return err
	}

	// Verify ownership
	if goal.UserID != userID {
		return domain.ErrSavingsGoalNotFound
	}

	// Delete the goal
	if err := s.goalRepo.Delete(ctx, goalID, userID); err != nil {
		s.logger.Error("failed to delete savings goal",
			slog.String("goal_id", goalID),
			slog.String("error", err.Error()),
		)
		return err
	}

	s.logger.Info("savings goal deleted",
		slog.String("user_id", userID),
		slog.String("goal_id", goalID),
	)

	return nil
}

// validateAccountOwnershipWithAccount checks account ownership and returns the matched account.
// Issue #1109: Returns the account so the caller can capture its UserAccountID.
func (s *savingsGoalService) validateAccountOwnershipWithAccount(ctx context.Context, userID, accountID string) (*domain.Account, error) {
	accounts, err := s.accountRepo.GetByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}

	for i := range accounts {
		if accounts[i].ID == accountID {
			return &accounts[i], nil
		}
	}

	return nil, errors.New("account not found or does not belong to user")
}

func (s *savingsGoalService) validateAccountOwnership(ctx context.Context, userID, accountID string) error {
	_, err := s.validateAccountOwnershipWithAccount(ctx, userID, accountID)
	return err
}
