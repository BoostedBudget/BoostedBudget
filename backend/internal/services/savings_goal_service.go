package services

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	phclient "github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"
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
// Since migration 000098 the detach tombstone is keyed on series_id, so all methods
// that touch the tombstone use the budget's SeriesID rather than its ID.
type GoalAutoCreateBudgetRepo interface {
	GetActiveByUserID(ctx context.Context, userID string) ([]*domain.Budget, error)
	CreateCategory(ctx context.Context, category *domain.BudgetCategory) error
	IsGoalDetachedForSeries(ctx context.Context, seriesID, goalID string) (bool, error)
}

// GoalProgressRecomputer is a narrow interface for triggering goal progress recomputation.
// Used by SavingsGoalService after CreateGoal to initialize current_amount per progress_basis
// (Issue #1300 M13 / G3). Implementations must be non-blocking-safe; errors are logged only.
type GoalProgressRecomputer interface {
	RecomputeGoal(ctx context.Context, goal *domain.SavingsGoal) error
}

// GoalBasisSwitchContribRepo is a narrow interface for contribution operations needed during
// progress_basis changes (Issue #1300 M14 / Codex C10).
//   - Insert: seeds a "manual"-source contribution row equal to prior current_amount when
//     switching manual → contributions, so existing savings history is preserved.
//   - ArchiveByGoal: clears managed_by_reconciliation on all rows when switching away from
//     "contributions" so the reconciler stops managing them (they become historical records).
type GoalBasisSwitchContribRepo interface {
	Insert(ctx context.Context, userID string, contrib *domain.SavingsGoalContribution) error
	ArchiveByGoal(ctx context.Context, goalID string) error
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

	// SetProgressService wires the optional progress recomputer for initializing
	// current_amount after CreateGoal. If not set, recomputation is skipped (Issue #1300 M13).
	SetProgressService(svc GoalProgressRecomputer)

	// SetBasisSwitchContribRepo wires the optional contribution repository for basis-switch
	// semantics in UpdateGoal (seed on manual→contributions; archive on contributions→other).
	// If not set, basis-switch contribution operations are skipped (Issue #1300 M14).
	SetBasisSwitchContribRepo(repo GoalBasisSwitchContribRepo)

	// SetAnalytics wires the optional PostHog analytics client. When set, a
	// "savings_goal_progress_basis_changed" event is fired on basis change (Issue #1300 M14).
	SetAnalytics(analytics phclient.Analytics)
}

// savingsGoalService implements SavingsGoalService
type savingsGoalService struct {
	goalRepo    repository.SavingsGoalRepository
	accountRepo AccountGetter
	logger      *slog.Logger
	// budgetRepo is optional; when set, CreateGoal auto-creates savings categories
	// in all active budgets for the new goal (best-effort, Issue #1300).
	budgetRepo GoalAutoCreateBudgetRepo
	// progressService is optional; when set, CreateGoal calls RecomputeGoal to
	// initialise current_amount per progress_basis (best-effort, Issue #1300 M13).
	progressService GoalProgressRecomputer
	// basisSwitchRepo is optional; when set, UpdateGoal seeds / archives contributions
	// on progress_basis changes (best-effort, Issue #1300 M14 / Codex C10).
	basisSwitchRepo GoalBasisSwitchContribRepo
	// analytics is optional; when set, basis-change events are fired via PostHog
	// (Issue #1300 M14). Nil-safe: guard with "if s.analytics != nil".
	analytics phclient.Analytics
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
	progressBasis := req.ProgressBasis
	if progressBasis == "" {
		progressBasis = domain.ProgressBasisManual
	}
	goal := &domain.SavingsGoal{
		ID:                uuid.New().String(),
		UserID:            userID,
		Name:              req.Name,
		Description:       req.Description,
		TargetAmount:      req.TargetAmount,
		CurrentAmount:     req.CurrentAmount,
		CurrencyCode:      req.CurrencyCode,
		AccountID:         req.AccountID,
		Deadline:          req.Deadline,
		IsCompleted:       false,
		ProgressBasis:     progressBasis,
		AccountPercentage: req.AccountPercentage,
		CreatedAt:         time.Now(),
		UpdatedAt:         time.Now(),
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

	// Trigger an initial recompute so current_amount reflects the configured
	// progress_basis from creation time (Issue #1300 M13 / G3). Best-effort:
	// an error here must not fail goal creation.
	if s.progressService != nil {
		if err := s.progressService.RecomputeGoal(ctx, goal); err != nil {
			s.logger.Warn("savings goal created but RecomputeGoal failed (non-fatal)",
				slog.String("user_id", userID),
				slog.String("goal_id", goal.ID),
				slog.String("error", err.Error()),
			)
		}
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

// SetProgressService wires the optional progress recomputer (Issue #1300 M13).
func (s *savingsGoalService) SetProgressService(svc GoalProgressRecomputer) {
	s.progressService = svc
}

// SetBasisSwitchContribRepo wires the optional contribution repo for basis-switch
// seed / archive operations (Issue #1300 M14).
func (s *savingsGoalService) SetBasisSwitchContribRepo(repo GoalBasisSwitchContribRepo) {
	s.basisSwitchRepo = repo
}

// SetAnalytics wires the optional PostHog analytics client (Issue #1300 M14).
func (s *savingsGoalService) SetAnalytics(analytics phclient.Analytics) {
	s.analytics = analytics
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
		detached, err := s.budgetRepo.IsGoalDetachedForSeries(ctx, budget.SeriesID, goalID)
		if err != nil {
			s.logger.Warn("auto-create: failed to check tombstone",
				slog.String("budget_id", budget.ID),
				slog.String("series_id", budget.SeriesID),
				slog.String("goal_id", goalID),
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
			SavingsGoalID:   &goalID,
			AllocatedAmount: 0,
			AutoCreated:     true,
			ManagedByGoalID: &goalID,
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

	// Snapshot pre-update state for basis-switch semantics (Issue #1300 M14 / Codex C10).
	// These must be captured BEFORE applying any req.* field updates so that
	// handleBasisSwitch receives the truly prior values even when a caller changes
	// both progress_basis and current_amount in the same request.
	priorBasis := goal.ProgressBasis
	priorAmount := goal.CurrentAmount
	basisChanged := false

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
	// Issue #1300 gap G1: apply progress_basis / account_percentage changes.
	// If switching away from account_percentage, clear the percentage so the
	// discriminator check in goal.Validate() does not reject the new basis.
	if req.ProgressBasis != nil {
		goal.ProgressBasis = *req.ProgressBasis
		if goal.ProgressBasis != domain.ProgressBasisAccountPercentage {
			goal.AccountPercentage = nil
		}
		basisChanged = goal.ProgressBasis != priorBasis
	}
	if req.AccountPercentage != nil {
		goal.AccountPercentage = req.AccountPercentage
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

	// Issue #1300 M14: basis-switch semantics (Codex C10).
	// These are best-effort; errors are logged but do not fail the update.
	if basisChanged && s.basisSwitchRepo != nil {
		s.handleBasisSwitch(ctx, userID, goalID, priorBasis, goal.ProgressBasis, priorAmount)
	}

	// Emit analytics event for the basis change (best-effort).
	if basisChanged && s.analytics != nil {
		s.analytics.Capture(userID, phclient.EventSavingsGoalProgressBasisChanged, map[string]interface{}{
			"goal_id":    goalID,
			"prior":      priorBasis,
			"new":        goal.ProgressBasis,
		})
	}

	// Re-run recompute so current_amount reflects the new basis immediately.
	if basisChanged && s.progressService != nil {
		if err := s.progressService.RecomputeGoal(ctx, goal); err != nil {
			s.logger.Warn("basis-changed RecomputeGoal failed (non-fatal)",
				slog.String("goal_id", goalID),
				slog.String("error", err.Error()),
			)
		}
	}

	s.logger.Info("savings goal updated",
		slog.String("user_id", userID),
		slog.String("goal_id", goalID),
	)

	return goal, nil
}

// handleBasisSwitch executes the contribution repository operations required when
// progress_basis changes (Codex C10). Best-effort: errors are logged, never propagated.
//
//   - manual → contributions: seed a confirmed manual contribution equal to the prior
//     current_amount so the existing savings balance is carried over as history.
//   - contributions → anything else: archive managed rows so the reconciler stops
//     touching them (they become immutable historical records).
func (s *savingsGoalService) handleBasisSwitch(
	ctx context.Context,
	userID, goalID, priorBasis, newBasis string,
	priorAmount float64,
) {
	switch {
	case priorBasis == domain.ProgressBasisManual && newBasis == domain.ProgressBasisContributions:
		// Seed: carry the existing manual balance as a confirmed contribution.
		if priorAmount > 0 {
			seed := &domain.SavingsGoalContribution{
				SavingsGoalID:           goalID,
				Amount:                  priorAmount,
				Source:                  "manual",
				Status:                  "confirmed",
				ManagedByReconciliation: false,
				ConfirmedByUser:         true,
				OccurredOn:              time.Now().Format("2006-01-02"),
			}
			if err := s.basisSwitchRepo.Insert(ctx, userID, seed); err != nil {
				s.logger.Warn("basis-switch seed contribution failed (non-fatal)",
					slog.String("goal_id", goalID),
					slog.String("user_id", userID),
					slog.String("error", err.Error()),
				)
			}
		}

	case priorBasis == domain.ProgressBasisContributions:
		// Archive: stop the reconciler from modifying these rows.
		if err := s.basisSwitchRepo.ArchiveByGoal(ctx, goalID); err != nil {
			s.logger.Warn("basis-switch archive contributions failed (non-fatal)",
				slog.String("goal_id", goalID),
				slog.String("user_id", userID),
				slog.String("error", err.Error()),
			)
		}
	}
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
