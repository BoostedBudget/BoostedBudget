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

	// SetBudgetRepo sets the budget repository for category ownership validation.
	// Issue #1270: Task 3 — category ownership validation for conditions.
	SetBudgetRepo(repo repository.BudgetRepository)
}

// automationRuleService implements AutomationRuleService.
type automationRuleService struct {
	ruleRepo   repository.AutomationRulesRepository
	budgetRepo repository.BudgetRepository
}

// NewAutomationRuleService creates a new AutomationRuleService instance.
func NewAutomationRuleService(
	ruleRepo repository.AutomationRulesRepository,
) AutomationRuleService {
	return &automationRuleService{
		ruleRepo: ruleRepo,
	}
}

// SetBudgetRepo sets the budget repository for category ownership validation.
// Issue #1270: Task 3 — category ownership validation for conditions.
func (s *automationRuleService) SetBudgetRepo(repo repository.BudgetRepository) {
	s.budgetRepo = repo
}

// validateRuleReferenceOwnership checks that any category_id values referenced in
// rule conditions are owned by the rule's user. Skips validation when budgetRepo
// is not set (backward-compatible with existing callers that don't inject it).
func (s *automationRuleService) validateRuleReferenceOwnership(ctx context.Context, rule *domain.AutomationRule) error {
	if s.budgetRepo == nil {
		return nil
	}
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

	// Validate that any category_id values in conditions are owned by this user.
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

	// Validate that any category_id values in conditions are owned by this user.
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
