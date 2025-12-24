// Package services provides business logic for automation rules.
// Issue #35: Rules Engine Core - CRUD operations with tier limit enforcement.
package services

import (
	"context"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// MinExecutionIntervalForEventTriggers is the threshold that determines if a tier
// allows event-based triggers. Tiers with min_execution_interval_minutes >= this value
// are scheduled-only (free tier). Premium tiers have lower intervals.
const MinExecutionIntervalForEventTriggers = 1440 // 24 hours = free tier

// AutomationRuleService defines the interface for automation rule business logic.
type AutomationRuleService interface {
	// CreateRule creates a new automation rule after checking subscription limits.
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

	// TierAllowsEventTriggers checks if the user's tier allows event-based triggers.
	TierAllowsEventTriggers(ctx context.Context, userID string) (bool, error)
}

// automationRuleService implements AutomationRuleService.
type automationRuleService struct {
	ruleRepo            repository.AutomationRulesRepository
	subscriptionChecker SubscriptionChecker
	userGetter          UserGetter
}

// NewAutomationRuleService creates a new AutomationRuleService instance.
func NewAutomationRuleService(
	ruleRepo repository.AutomationRulesRepository,
	subscriptionChecker SubscriptionChecker,
	userGetter UserGetter,
) AutomationRuleService {
	return &automationRuleService{
		ruleRepo:            ruleRepo,
		subscriptionChecker: subscriptionChecker,
		userGetter:          userGetter,
	}
}

// CreateRule creates a new automation rule after checking subscription limits.
func (s *automationRuleService) CreateRule(ctx context.Context, userID string, req *domain.CreateAutomationRuleRequest) (*domain.AutomationRule, error) {
	// Get user to check subscription
	user, err := s.userGetter.GetUserByID(ctx, userID)
	if err != nil {
		return nil, err
	}

	// Get effective subscription limits
	effectiveSub, err := s.subscriptionChecker.GetEffectiveSubscription(ctx, user)
	if err != nil {
		return nil, err
	}

	// Check subscription limit for rules
	currentCount, err := s.ruleRepo.CountByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}

	// Check limit (-1 = unlimited)
	limit := effectiveSub.Limits.Rules
	if !domain.IsUnlimited(limit) && currentCount >= limit {
		return nil, domain.ErrAutomationRuleLimitExceeded
	}

	// Check if event triggers are allowed for this tier
	// Free tier (min_interval >= 1440) = scheduled only
	// Premium tiers (min_interval < 1440) = event-based allowed
	if req.TriggerConfig.Type == domain.TriggerTypeEvent {
		if !s.tierAllowsEventTriggersFromSub(effectiveSub) {
			return nil, domain.ErrEventTriggersNotAllowed
		}
	}

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

	// If changing to event trigger, check if tier allows it
	if req.TriggerConfig != nil && req.TriggerConfig.Type == domain.TriggerTypeEvent {
		allowed, err := s.TierAllowsEventTriggers(ctx, userID)
		if err != nil {
			return nil, err
		}
		if !allowed {
			return nil, domain.ErrEventTriggersNotAllowed
		}
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

// TierAllowsEventTriggers checks if the user's tier allows event-based triggers.
// Free tier (min_execution_interval_minutes >= 1440) = scheduled only
// Premium tiers (min_execution_interval_minutes < 1440) = event-based allowed
func (s *automationRuleService) TierAllowsEventTriggers(ctx context.Context, userID string) (bool, error) {
	user, err := s.userGetter.GetUserByID(ctx, userID)
	if err != nil {
		return false, err
	}

	effectiveSub, err := s.subscriptionChecker.GetEffectiveSubscription(ctx, user)
	if err != nil {
		return false, err
	}

	return s.tierAllowsEventTriggersFromSub(effectiveSub), nil
}

// tierAllowsEventTriggersFromSub checks if the subscription allows event-based triggers.
func (s *automationRuleService) tierAllowsEventTriggersFromSub(sub *domain.EffectiveSubscription) bool {
	// Free tier has min_execution_interval_minutes = 1440 (24 hours)
	// Premium tiers have lower values: 720 (12h), 240 (4h), 10 (10min)
	// If interval >= 1440, user is on free tier and should use scheduled triggers only
	return sub.Limits.MinExecutionIntervalMinutes < MinExecutionIntervalForEventTriggers
}
