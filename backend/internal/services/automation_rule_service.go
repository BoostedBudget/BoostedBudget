// Package services provides business logic for automation rules.
// Issue #35: Rules Engine Core - CRUD operations.
package services

import (
	"context"
	"fmt"
	"strings"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// AutomationRuleService defines the interface for automation rule business logic.
type AutomationRuleService interface {
	// CreateRule creates a new automation rule (unlimited rules for all tiers).
	CreateRule(ctx context.Context, userID string, req *domain.CreateAutomationRuleRequest) (*domain.AutomationRule, error)

	// GetUserRules retrieves all automation rules for a user, ordered by priority.
	GetUserRules(ctx context.Context, userID string) ([]*domain.AutomationRule, error)

	// GetRuleByID retrieves a specific rule by ID.
	GetRuleByID(ctx context.Context, userID, ruleID string) (*domain.AutomationRule, error)

	// UpdateRule updates a rule's configuration.
	UpdateRule(ctx context.Context, userID, ruleID string, req *domain.UpdateAutomationRuleRequest) (*domain.AutomationRule, error)

	// DeleteRule removes a rule.
	DeleteRule(ctx context.Context, userID, ruleID string) error

	// ToggleRule enables or disables a rule.
	ToggleRule(ctx context.Context, userID, ruleID string, enabled bool) (*domain.AutomationRule, error)

	// GetEnabledRules retrieves all enabled rules for a user.
	GetEnabledRules(ctx context.Context, userID string) ([]*domain.AutomationRule, error)

	// GetRulesByType retrieves enabled rules of a specific type.
	GetRulesByType(ctx context.Context, userID string, ruleType domain.RuleType) ([]*domain.AutomationRule, error)
}

// automationRuleService implements AutomationRuleService.
type automationRuleService struct {
	ruleRepo        repository.AutomationRulesRepository
	savingsGoalRepo repository.SavingsGoalRepository // optional; used for ownership validation (GAP AE)
	accountRepo     repository.AccountRepository     // optional; used for ownership validation (GAP AE)
	budgetRepo      repository.BudgetRepository      // optional; used for category ownership validation (#1270)
}

// NewAutomationRuleService creates a new AutomationRuleService instance.
// Returns the concrete type so callers in main.go can call optional setter methods
// (SetSavingsGoalRepo, SetAccountRepo) without adding them to the interface — consistent
// with NewRuleExecutionService's pattern.
func NewAutomationRuleService(
	ruleRepo repository.AutomationRulesRepository,
) *automationRuleService {
	return &automationRuleService{
		ruleRepo: ruleRepo,
	}
}

// SetSavingsGoalRepo injects the savings goal repository for ownership validation.
// Must be called in production (cmd/server/main.go) to enable savings_goal_id IDOR protection.
// Issue #972: GAP AE.
func (s *automationRuleService) SetSavingsGoalRepo(repo repository.SavingsGoalRepository) {
	s.savingsGoalRepo = repo
}

// SetAccountRepo injects the account repository for ownership validation.
// Must be called in production (cmd/server/main.go) to enable from/to_account_id IDOR protection.
// Issue #972: GAP AE.
func (s *automationRuleService) SetAccountRepo(repo repository.AccountRepository) {
	s.accountRepo = repo
}

// SetBudgetRepo injects the budget repository for category ownership validation.
// Must be called in production to enable budget_category.* condition CategoryID IDOR protection.
// Issue #1270.
func (s *automationRuleService) SetBudgetRepo(repo repository.BudgetRepository) {
	s.budgetRepo = repo
}

// validateRuleReferenceOwnership checks that IDs referenced in the rule's actions (savings_goal_id,
// from_account_id, to_account_id, from_user_account_id, to_user_account_id) belong to rule.UserID.
// Returns an error if any referenced ID belongs to a different user. Silently skips if the required
// repo is not injected.
// Issue #972: GAP AE — prevents IDOR via crafted automation rules, including the chain-resolver
// attack vector where an attacker supplies their own from_account_id (passes the ID check) plus a
// victim's from_user_account_id (used by the chain resolver to overwrite the destination).
func (s *automationRuleService) validateRuleReferenceOwnership(ctx context.Context, rule *domain.AutomationRule) error {
	switch rule.RuleType {
	case domain.RuleTypeSavingsGoal:
		action, err := rule.ParseSavingsGoalAction()
		if err != nil {
			return err
		}
		if s.savingsGoalRepo != nil && action.SavingsGoalID != "" {
			if _, err := s.savingsGoalRepo.GetByIDForUser(ctx, action.SavingsGoalID, rule.UserID); err != nil {
				return fmt.Errorf("savings_goal_id does not belong to this user")
			}
		}
		if s.accountRepo != nil && action.FromAccountID != "" {
			if _, err := s.accountRepo.GetByIDAndUser(ctx, action.FromAccountID, rule.UserID); err != nil {
				return fmt.Errorf("from_account_id does not belong to this user")
			}
		}

	case domain.RuleTypeTransferSuggestion:
		action, err := rule.ParseTransferAction()
		if err != nil {
			return err
		}
		if s.accountRepo == nil {
			break
		}
		// Verify direct account IDs belong to the user
		if action.FromAccountID != "" {
			if _, err := s.accountRepo.GetByIDAndUser(ctx, action.FromAccountID, rule.UserID); err != nil {
				return fmt.Errorf("from_account_id does not belong to this user")
			}
		}
		if action.ToAccountID != "" {
			if _, err := s.accountRepo.GetByIDAndUser(ctx, action.ToAccountID, rule.UserID); err != nil {
				return fmt.Errorf("to_account_id does not belong to this user")
			}
		}
		// Chain-resolver IDOR guard: from_user_account_id and to_user_account_id are stable
		// identifiers that survive Plaid re-links. An attacker could supply their own from_account_id
		// (passes the check above) plus a victim's from_user_account_id, and the unscoped
		// ResolveActiveID would overwrite the suggestion's destination at execution time.
		if action.FromUserAccountID != "" {
			if _, err := s.accountRepo.GetByUserAccountIDAndUser(ctx, action.FromUserAccountID, rule.UserID); err != nil {
				return fmt.Errorf("from_user_account_id does not belong to this user")
			}
		}
		if action.ToUserAccountID != "" {
			if _, err := s.accountRepo.GetByUserAccountIDAndUser(ctx, action.ToUserAccountID, rule.UserID); err != nil {
				return fmt.Errorf("to_user_account_id does not belong to this user")
			}
		}
	}

	// Issue #1270: verify any condition CategoryID belongs to the rule's user.
	if s.budgetRepo != nil {
		for _, condition := range rule.Conditions {
			if condition.CategoryID == "" {
				continue
			}
			// Only budget_category.* fields carry a CategoryID; guard with prefix check for safety.
			if !strings.HasPrefix(condition.Field, "budget_category.") {
				continue
			}
			if _, err := s.budgetRepo.GetCategoryByIDAndUser(ctx, condition.CategoryID, rule.UserID); err != nil {
				return fmt.Errorf("condition category_id does not belong to this user")
			}
		}
	}
	return nil
}

// CreateRule creates a new automation rule (unlimited rules for all tiers).
func (s *automationRuleService) CreateRule(ctx context.Context, userID string, req *domain.CreateAutomationRuleRequest) (*domain.AutomationRule, error) {
	// Create the rule
	rule := &domain.AutomationRule{
		UserID:         userID,
		Name:           req.Name,
		Description:    req.Description,
		Enabled:        true, // Default enabled
		Priority:       req.Priority,
		RuleType:       req.RuleType,
		TriggerConfig:  req.TriggerConfig,
		Conditions:     req.Conditions,
		ConditionLogic: req.ConditionLogic,
		Actions:        req.Actions,
	}

	// Set defaults
	if rule.ConditionLogic == "" {
		rule.ConditionLogic = "AND"
	}

	// Validate the rule
	if err := rule.Validate(); err != nil {
		return nil, err
	}

	// Verify referenced IDs (savings_goal_id, account IDs) belong to the rule owner (GAP AE).
	if err := s.validateRuleReferenceOwnership(ctx, rule); err != nil {
		return nil, err
	}

	// Save to database
	if err := s.ruleRepo.Create(ctx, rule); err != nil {
		return nil, err
	}

	return rule, nil
}

// GetUserRules retrieves all automation rules for a user, ordered by priority.
func (s *automationRuleService) GetUserRules(ctx context.Context, userID string) ([]*domain.AutomationRule, error) {
	return s.ruleRepo.GetByUserID(ctx, userID)
}

// GetRuleByID retrieves a specific rule by ID.
func (s *automationRuleService) GetRuleByID(ctx context.Context, userID, ruleID string) (*domain.AutomationRule, error) {
	rule, err := s.ruleRepo.GetByID(ctx, ruleID)
	if err != nil {
		return nil, err
	}

	// Verify ownership
	if rule.UserID != userID {
		return nil, domain.ErrAutomationRuleNotFound
	}

	return rule, nil
}

// UpdateRule updates a rule's configuration.
func (s *automationRuleService) UpdateRule(ctx context.Context, userID, ruleID string, req *domain.UpdateAutomationRuleRequest) (*domain.AutomationRule, error) {
	// Get existing rule
	rule, err := s.ruleRepo.GetByID(ctx, ruleID)
	if err != nil {
		return nil, err
	}

	// Verify ownership
	if rule.UserID != userID {
		return nil, domain.ErrAutomationRuleNotFound
	}

	// Apply updates
	if req.Name != nil {
		rule.Name = *req.Name
	}
	if req.Description != nil {
		rule.Description = req.Description
	}
	if req.Enabled != nil {
		rule.Enabled = *req.Enabled
	}
	if req.Priority != nil {
		rule.Priority = *req.Priority
	}
	if req.TriggerConfig != nil {
		rule.TriggerConfig = *req.TriggerConfig
	}
	if req.Conditions != nil {
		rule.Conditions = *req.Conditions
	}
	if req.ConditionLogic != nil {
		rule.ConditionLogic = *req.ConditionLogic
	}
	if req.Actions != nil {
		rule.Actions = *req.Actions
	}

	// Validate
	if err := rule.Validate(); err != nil {
		return nil, err
	}

	// Verify referenced IDs (savings_goal_id, account IDs) belong to the rule owner (GAP AE).
	if err := s.validateRuleReferenceOwnership(ctx, rule); err != nil {
		return nil, err
	}

	// Save updates
	if err := s.ruleRepo.Update(ctx, rule); err != nil {
		return nil, err
	}

	return rule, nil
}

// DeleteRule removes a rule.
func (s *automationRuleService) DeleteRule(ctx context.Context, userID, ruleID string) error {
	// Verify ownership first
	rule, err := s.ruleRepo.GetByID(ctx, ruleID)
	if err != nil {
		return err
	}

	if rule.UserID != userID {
		return domain.ErrAutomationRuleNotFound
	}

	return s.ruleRepo.Delete(ctx, ruleID)
}

// ToggleRule enables or disables a rule.
func (s *automationRuleService) ToggleRule(ctx context.Context, userID, ruleID string, enabled bool) (*domain.AutomationRule, error) {
	// Get existing rule
	rule, err := s.ruleRepo.GetByID(ctx, ruleID)
	if err != nil {
		return nil, err
	}

	// Verify ownership
	if rule.UserID != userID {
		return nil, domain.ErrAutomationRuleNotFound
	}

	// Update enabled status
	rule.Enabled = enabled

	// Save update
	if err := s.ruleRepo.Update(ctx, rule); err != nil {
		return nil, err
	}

	return rule, nil
}

// GetEnabledRules retrieves all enabled rules for a user.
func (s *automationRuleService) GetEnabledRules(ctx context.Context, userID string) ([]*domain.AutomationRule, error) {
	return s.ruleRepo.GetEnabledByUserID(ctx, userID)
}

// GetRulesByType retrieves enabled rules of a specific type.
func (s *automationRuleService) GetRulesByType(ctx context.Context, userID string, ruleType domain.RuleType) ([]*domain.AutomationRule, error) {
	return s.ruleRepo.GetRulesByType(ctx, userID, ruleType)
}
