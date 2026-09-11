// Package services provides business logic for boosts.
// Issue #35: Boosts Engine Core - CRUD operations.
package services

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// BoostService defines the interface for boost business logic.
type BoostService interface {
	// CreateBoost creates a new automation boost (unlimited boosts for all tiers).
	CreateBoost(ctx context.Context, userID string, req *domain.CreateBoostRequest) (*domain.Boost, error)

	// GetUserBoosts retrieves all boosts for a user, ordered by priority.
	GetUserBoosts(ctx context.Context, userID string) ([]*domain.Boost, error)

	// GetUserBoostsFiltered retrieves boosts optionally filtered by boost types.
	// When boostTypes is empty, all boosts are returned (same as GetUserBoosts).
	GetUserBoostsFiltered(ctx context.Context, userID string, boostTypes []domain.BoostType) ([]*domain.Boost, error)

	// GetBoostByID retrieves a specific boost by ID.
	GetBoostByID(ctx context.Context, userID, boostID string) (*domain.Boost, error)

	// UpdateBoost updates a boost's configuration.
	UpdateBoost(ctx context.Context, userID, boostID string, req *domain.UpdateBoostRequest) (*domain.Boost, error)

	// DeleteBoost removes a boost.
	DeleteBoost(ctx context.Context, userID, boostID string) error

	// ToggleBoost enables or disables a boost.
	ToggleBoost(ctx context.Context, userID, boostID string, enabled bool) (*domain.Boost, error)

	// GetEnabledBoosts retrieves all enabled boosts for a user.
	GetEnabledBoosts(ctx context.Context, userID string) ([]*domain.Boost, error)

	// GetBoostsByType retrieves enabled boosts of a specific type.
	GetBoostsByType(ctx context.Context, userID string, boostType domain.BoostType) ([]*domain.Boost, error)
}

// boostService implements BoostService.
type boostService struct {
	boostRepo        repository.BoostsRepository
	savingsGoalRepo repository.SavingsGoalRepository // optional; used for ownership validation (GAP AE)
	accountRepo     repository.AccountRepository     // optional; used for ownership validation (GAP AE)
	budgetRepo      repository.BudgetRepository      // optional; used for category ownership validation (#1270)
	// auditLogRepo records the boost.deleted entry INSIDE BoostsRepository.Delete's own
	// transaction (Issue #1569 Task 8), via LogInTx. Nil-safe optional dependency wired by
	// SetAuditLogRepository; when unset the delete still works and writes no entry.
	auditLogRepo repository.AuditLogRepository
}

// NewBoostService creates a new BoostService instance.
// Returns the concrete type so callers in main.go can call optional setter methods
// (SetSavingsGoalRepo, SetAccountRepo) without adding them to the interface — consistent
// with NewBoostExecutionService's pattern.
func NewBoostService(
	boostRepo repository.BoostsRepository,
) *boostService {
	return &boostService{
		boostRepo: boostRepo,
	}
}

// SetSavingsGoalRepo injects the savings goal repository for ownership validation.
// Must be called in production (cmd/server/main.go) to enable savings_goal_id IDOR protection.
// Issue #972: GAP AE.
func (s *boostService) SetSavingsGoalRepo(repo repository.SavingsGoalRepository) {
	s.savingsGoalRepo = repo
}

// SetAuditLogRepository injects the audit-log repository so DeleteBoost records a boost.deleted
// entry inside the same transaction as the tombstone. Issue #1569, Task 8.
func (s *boostService) SetAuditLogRepository(ar repository.AuditLogRepository) {
	s.auditLogRepo = ar
}

// SetAccountRepo injects the account repository for ownership validation.
// Must be called in production (cmd/server/main.go) to enable from/to_account_id IDOR protection.
// Issue #972: GAP AE.
func (s *boostService) SetAccountRepo(repo repository.AccountRepository) {
	s.accountRepo = repo
}

// SetBudgetRepo injects the budget repository for category ownership validation.
// Must be called in production to enable budget_category.* condition CategoryID IDOR protection.
// Issue #1270.
func (s *boostService) SetBudgetRepo(repo repository.BudgetRepository) {
	s.budgetRepo = repo
}

// validateBoostReferenceOwnership checks that IDs referenced in the boost's actions (savings_goal_id,
// from_account_id, to_account_id, from_user_account_id, to_user_account_id) belong to boost.UserID.
// Returns an error if any referenced ID belongs to a different user. Silently skips if the required
// repo is not injected.
// Issue #972: GAP AE — prevents IDOR via crafted automation boosts, including the chain-resolver
// attack vector where an attacker supplies their own from_account_id (passes the ID check) plus a
// victim's from_user_account_id (used by the chain resolver to overwrite the destination).
func (s *boostService) validateBoostReferenceOwnership(ctx context.Context, boost *domain.Boost) error {
	switch boost.BoostType {
	case domain.BoostTypeSavingsGoal:
		action, err := boost.ParseSavingsGoalAction()
		if err != nil {
			return err
		}
		if s.savingsGoalRepo != nil && action.SavingsGoalID != "" {
			if _, err := s.savingsGoalRepo.GetByIDForUser(ctx, action.SavingsGoalID, boost.UserID); err != nil {
				return fmt.Errorf("savings_goal_id does not belong to this user")
			}
		}
		if s.accountRepo != nil && action.FromAccountID != "" {
			if _, err := s.accountRepo.GetByIDAndUser(ctx, action.FromAccountID, boost.UserID); err != nil {
				return fmt.Errorf("from_account_id does not belong to this user")
			}
		}

	case domain.BoostTypeCategoryRollover:
		action, err := boost.ParseRolloverAction()
		if err != nil {
			return err
		}
		if s.budgetRepo != nil && action.SourceBudgetCategoryID != "" {
			cat, err := s.budgetRepo.GetCategoryByIDAndUser(ctx, action.SourceBudgetCategoryID, boost.UserID)
			if err != nil {
				if errors.Is(err, domain.ErrBudgetCategoryNotFound) {
					return fmt.Errorf("%w: source_budget_category_id does not belong to this user", domain.ErrBoostConditionForbidden)
				}
				return err
			}
			if err := s.refuseSubAllocationRolloverSource(ctx, cat); err != nil {
				return err
			}
		}

	case domain.BoostTypeTransferSuggestion:
		action, err := boost.ParseTransferAction()
		if err != nil {
			return err
		}
		if s.accountRepo == nil {
			break
		}
		// Verify direct account IDs belong to the user
		if action.FromAccountID != "" {
			if _, err := s.accountRepo.GetByIDAndUser(ctx, action.FromAccountID, boost.UserID); err != nil {
				return fmt.Errorf("from_account_id does not belong to this user")
			}
		}
		if action.ToAccountID != "" {
			if _, err := s.accountRepo.GetByIDAndUser(ctx, action.ToAccountID, boost.UserID); err != nil {
				return fmt.Errorf("to_account_id does not belong to this user")
			}
		}
		// Chain-resolver IDOR guard: from_user_account_id and to_user_account_id are stable
		// identifiers that survive Plaid re-links. An attacker could supply their own from_account_id
		// (passes the check above) plus a victim's from_user_account_id, and the unscoped
		// ResolveActiveID would overwrite the suggestion's destination at execution time.
		if action.FromUserAccountID != "" {
			if _, err := s.accountRepo.GetByUserAccountIDAndUser(ctx, action.FromUserAccountID, boost.UserID); err != nil {
				return fmt.Errorf("from_user_account_id does not belong to this user")
			}
		}
		if action.ToUserAccountID != "" {
			if _, err := s.accountRepo.GetByUserAccountIDAndUser(ctx, action.ToUserAccountID, boost.UserID); err != nil {
				return fmt.Errorf("to_user_account_id does not belong to this user")
			}
		}
	}

	// Issue #1270: verify any condition CategoryID belongs to the boost's user.
	if s.budgetRepo != nil {
		for _, condition := range boost.Conditions {
			if condition.CategoryID == "" {
				continue
			}
			// Only budget_category.* fields carry a CategoryID; guard with prefix check for safety.
			if !strings.HasPrefix(condition.Field, "budget_category.") {
				continue
			}
			cat, err := s.budgetRepo.GetCategoryByIDAndUser(ctx, condition.CategoryID, boost.UserID)
			if err != nil {
				if errors.Is(err, domain.ErrBudgetCategoryNotFound) {
					return fmt.Errorf("%w: condition category_id does not belong to this user", domain.ErrBoostConditionForbidden)
				}
				return err
			}
			// Issue #1271 (A-CR4): verify the category belongs to the specified budget.
			// cat.BudgetID is trusted (resolved via GetCategoryByIDAndUser JOIN on user_id).
			// A mismatch means the caller named a real category but paired it with a different
			// budget → invalid request (400), not a 403.
			if condition.BudgetID != "" && cat.BudgetID != condition.BudgetID {
				return fmt.Errorf("%w: condition category_id is not in the selected budget_id", domain.ErrInvalidBoost)
			}
		}
	}
	return nil
}

// refuseSubAllocationRolloverSource refuses a category_rollover boost whose source category is a
// sub-allocation of another budgeted category in the SAME budget (Issue #1630 B6). A
// sub-allocation's dollars are already reflected in its budgeted PARENT's own remaining/spent
// (domain.BudgetCategory.IsSubAllocationWithin) — rolling it independently would roll the same
// dollars twice: once via the parent's own rollover (if any), and again via the child.
//
// This is the CREATION-time half of the fix. executeCategoryRollover
// (boost_execution_service.go) already refuses this shape too, silently, at EXECUTION time,
// against the live hierarchy as it stands each period — this teaches the user the SAME refusal
// at creation time, when the shape can be caught up front, instead of letting the user save a
// boost that will report success while doing nothing every period it runs. The two checks are
// deliberately not merged: creation time cannot see execution-time triggerData, and a category
// that was a valid top-level source when the boost was created can become a sub-allocation later
// if the hierarchy changes — validateRolloverAction's cousin at execution time (B6's second half)
// is what catches THAT case, at run time, when this creation-time check no longer applies.
//
// cat must be a category already confirmed to belong to boost.UserID (see the caller).
func (s *boostService) refuseSubAllocationRolloverSource(ctx context.Context, cat *domain.BudgetCategory) error {
	categories, err := s.budgetRepo.GetCategoriesByBudgetID(ctx, cat.BudgetID)
	if err != nil {
		return err
	}
	metrics := domain.BuildCategoryEventData(categories)
	if m, ok := metrics[cat.ID]; ok && m[domain.EventDataCategoryKeyIsSubAllocation] != 0 {
		return fmt.Errorf("%w: source_budget_category_id %q is a sub-allocation of another budgeted category in this budget -- rolling it independently would double-count dollars already reflected in its parent's own rollover; configure the rollover on the parent category instead", domain.ErrInvalidBoost, cat.ID)
	}
	return nil
}

// CreateBoost creates a new automation boost (unlimited boosts for all tiers).
func (s *boostService) CreateBoost(ctx context.Context, userID string, req *domain.CreateBoostRequest) (*domain.Boost, error) {
	// Create the boost
	boost := &domain.Boost{
		UserID:         userID,
		Name:           req.Name,
		Description:    req.Description,
		Enabled:        true, // Default enabled
		Priority:       req.Priority,
		BoostType:      req.BoostType,
		TriggerConfig:  req.TriggerConfig,
		Conditions:     req.Conditions,
		ConditionLogic: req.ConditionLogic,
		Actions:        req.Actions,
	}

	// Set defaults
	if boost.ConditionLogic == "" {
		boost.ConditionLogic = "AND"
	}

	// ValidateForPersistence checks static rules (via Validate) AND requires scope IDs for
	// category-scoped conditions. It already wraps errors with ErrInvalidBoost so the HTTP
	// layer returns 400 (not 500).
	if err := boost.ValidateForPersistence(); err != nil {
		return nil, err
	}

	// Verify referenced IDs (savings_goal_id, account IDs) belong to the boost owner (GAP AE).
	if err := s.validateBoostReferenceOwnership(ctx, boost); err != nil {
		return nil, err
	}

	// Save to database
	if err := s.boostRepo.Create(ctx, boost); err != nil {
		return nil, err
	}

	return boost, nil
}

// GetUserBoosts retrieves all automation boosts for a user, ordered by priority.
func (s *boostService) GetUserBoosts(ctx context.Context, userID string) ([]*domain.Boost, error) {
	return s.boostRepo.GetByUserID(ctx, userID)
}

// GetUserBoostsFiltered retrieves boosts optionally filtered by boost types.
// When boostTypes is empty, all boosts are returned (delegates to GetUserBoosts).
func (s *boostService) GetUserBoostsFiltered(ctx context.Context, userID string, boostTypes []domain.BoostType) ([]*domain.Boost, error) {
	return s.boostRepo.GetByUserIDFiltered(ctx, userID, boostTypes)
}

// GetBoostByID retrieves a specific boost by ID.
func (s *boostService) GetBoostByID(ctx context.Context, userID, boostID string) (*domain.Boost, error) {
	boost, err := s.boostRepo.GetByID(ctx, boostID)
	if err != nil {
		return nil, err
	}

	// Verify ownership
	if boost.UserID != userID {
		return nil, domain.ErrBoostNotFound
	}

	return boost, nil
}

// UpdateBoost updates a boost's configuration.
func (s *boostService) UpdateBoost(ctx context.Context, userID, boostID string, req *domain.UpdateBoostRequest) (*domain.Boost, error) {
	// Get existing boost
	boost, err := s.boostRepo.GetByID(ctx, boostID)
	if err != nil {
		return nil, err
	}

	// Verify ownership
	if boost.UserID != userID {
		return nil, domain.ErrBoostNotFound
	}

	// Apply updates
	if req.Name != nil {
		boost.Name = *req.Name
	}
	if req.Description != nil {
		boost.Description = req.Description
	}
	if req.Enabled != nil {
		boost.Enabled = *req.Enabled
	}
	if req.Priority != nil {
		boost.Priority = *req.Priority
	}
	if req.TriggerConfig != nil {
		boost.TriggerConfig = *req.TriggerConfig
	}
	if req.Conditions != nil {
		boost.Conditions = *req.Conditions
	}
	if req.ConditionLogic != nil {
		boost.ConditionLogic = *req.ConditionLogic
	}
	if req.Actions != nil {
		boost.Actions = *req.Actions
	}

	// ValidateForPersistence checks static rules (via Validate) AND requires scope IDs for
	// category-scoped conditions. It already wraps errors with ErrInvalidBoost so the HTTP
	// layer returns 400 (not 500).
	if err := boost.ValidateForPersistence(); err != nil {
		return nil, err
	}

	// Verify referenced IDs (savings_goal_id, account IDs) belong to the boost owner (GAP AE).
	if err := s.validateBoostReferenceOwnership(ctx, boost); err != nil {
		return nil, err
	}

	// Save updates
	if err := s.boostRepo.Update(ctx, boost); err != nil {
		return nil, err
	}

	return boost, nil
}

// DeleteBoost removes a boost.
func (s *boostService) DeleteBoost(ctx context.Context, userID, boostID string) error {
	// Verify ownership first
	boost, err := s.boostRepo.GetByID(ctx, boostID)
	if err != nil {
		return err
	}

	if boost.UserID != userID {
		return domain.ErrBoostNotFound
	}

	// The repository scopes the tombstone by user_id as well (Issue #1569 Task 8); the
	// GetByID-then-compare above is kept so a cross-user id still reports ErrBoostNotFound from
	// the same place it always did rather than depending on the repository's row count.
	return s.boostRepo.Delete(ctx, boostID, userID, s.auditLogRepo)
}

// ToggleBoost enables or disables a boost.
func (s *boostService) ToggleBoost(ctx context.Context, userID, boostID string, enabled bool) (*domain.Boost, error) {
	// Get existing boost
	boost, err := s.boostRepo.GetByID(ctx, boostID)
	if err != nil {
		return nil, err
	}

	// Verify ownership
	if boost.UserID != userID {
		return nil, domain.ErrBoostNotFound
	}

	// Update enabled status
	boost.Enabled = enabled

	// Save update
	if err := s.boostRepo.Update(ctx, boost); err != nil {
		return nil, err
	}

	return boost, nil
}

// GetEnabledBoosts retrieves all enabled boosts for a user.
func (s *boostService) GetEnabledBoosts(ctx context.Context, userID string) ([]*domain.Boost, error) {
	return s.boostRepo.GetEnabledByUserID(ctx, userID)
}

// GetBoostsByType retrieves enabled boosts of a specific type.
func (s *boostService) GetBoostsByType(ctx context.Context, userID string, boostType domain.BoostType) ([]*domain.Boost, error) {
	return s.boostRepo.GetBoostsByType(ctx, userID, boostType)
}
