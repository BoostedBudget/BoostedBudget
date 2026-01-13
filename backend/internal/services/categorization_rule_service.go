package services

import (
	"context"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// CategorizationRuleService defines the interface for categorization rule business logic.
type CategorizationRuleService interface {
	// CreateRule creates a new categorization rule (unlimited rules for all tiers).
	CreateRule(ctx context.Context, userID string, req *domain.CreateCategorizationRuleRequest) (*domain.CategorizationRule, error)

	// GetUserRules retrieves all rules for a user, ordered by priority.
	GetUserRules(ctx context.Context, userID string) ([]*domain.CategorizationRule, error)

	// GetRuleByID retrieves a specific rule by ID.
	GetRuleByID(ctx context.Context, userID, ruleID string) (*domain.CategorizationRule, error)

	// UpdateRule updates a rule's configuration.
	UpdateRule(ctx context.Context, userID, ruleID string, req *domain.UpdateCategorizationRuleRequest) (*domain.CategorizationRule, error)

	// DeleteRule removes a rule.
	DeleteRule(ctx context.Context, userID, ruleID string) error

	// ToggleRule enables or disables a rule.
	ToggleRule(ctx context.Context, userID, ruleID string, enabled bool) (*domain.CategorizationRule, error)

	// GetEnabledRules retrieves all enabled rules for a user.
	GetEnabledRules(ctx context.Context, userID string) ([]*domain.CategorizationRule, error)
}

// categorizationRuleService implements CategorizationRuleService.
type categorizationRuleService struct {
	ruleRepo repository.CategorizationRuleRepository
}

// NewCategorizationRuleService creates a new CategorizationRuleService instance.
func NewCategorizationRuleService(
	ruleRepo repository.CategorizationRuleRepository,
) CategorizationRuleService {
	return &categorizationRuleService{
		ruleRepo: ruleRepo,
	}
}

// CreateRule creates a new categorization rule (unlimited rules for all tiers).
func (s *categorizationRuleService) CreateRule(ctx context.Context, userID string, req *domain.CreateCategorizationRuleRequest) (*domain.CategorizationRule, error) {
	// Create the rule
	rule := &domain.CategorizationRule{
		UserID:         userID,
		Name:           req.Name,
		Description:    req.Description,
		Enabled:        true, // Default enabled
		Priority:       req.Priority,
		Conditions:     req.Conditions,
		ConditionLogic: req.ConditionLogic,
		TargetCategory: req.TargetCategory,
		TargetTags:     req.TargetTags,
		TagBehavior:    req.TagBehavior,
		Source:         "manual",
	}

	// Set defaults
	if rule.ConditionLogic == "" {
		rule.ConditionLogic = "AND"
	}
	if rule.TagBehavior == "" {
		rule.TagBehavior = "add"
	}

	// Validate
	if err := rule.Validate(); err != nil {
		return nil, err
	}

	// Save to database
	if err := s.ruleRepo.Create(ctx, rule); err != nil {
		return nil, err
	}

	return rule, nil
}

// GetUserRules retrieves all rules for a user, ordered by priority.
func (s *categorizationRuleService) GetUserRules(ctx context.Context, userID string) ([]*domain.CategorizationRule, error) {
	return s.ruleRepo.GetByUserID(ctx, userID)
}

// GetRuleByID retrieves a specific rule by ID.
func (s *categorizationRuleService) GetRuleByID(ctx context.Context, userID, ruleID string) (*domain.CategorizationRule, error) {
	rule, err := s.ruleRepo.GetByID(ctx, ruleID)
	if err != nil {
		return nil, err
	}

	// Verify ownership
	if rule.UserID != userID {
		return nil, domain.ErrRuleNotFound
	}

	return rule, nil
}

// UpdateRule updates a rule's configuration.
func (s *categorizationRuleService) UpdateRule(ctx context.Context, userID, ruleID string, req *domain.UpdateCategorizationRuleRequest) (*domain.CategorizationRule, error) {
	// Get existing rule
	rule, err := s.ruleRepo.GetByID(ctx, ruleID)
	if err != nil {
		return nil, err
	}

	// Verify ownership
	if rule.UserID != userID {
		return nil, domain.ErrRuleNotFound
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
	if req.Conditions != nil {
		rule.Conditions = *req.Conditions
	}
	if req.ConditionLogic != nil {
		rule.ConditionLogic = *req.ConditionLogic
	}
	if req.TargetCategory != nil {
		rule.TargetCategory = *req.TargetCategory
	}
	if req.TargetTags != nil {
		rule.TargetTags = *req.TargetTags
	}
	if req.TagBehavior != nil {
		rule.TagBehavior = *req.TagBehavior
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
func (s *categorizationRuleService) DeleteRule(ctx context.Context, userID, ruleID string) error {
	// Verify ownership first
	rule, err := s.ruleRepo.GetByID(ctx, ruleID)
	if err != nil {
		return err
	}

	if rule.UserID != userID {
		return domain.ErrRuleNotFound
	}

	return s.ruleRepo.Delete(ctx, ruleID)
}

// ToggleRule enables or disables a rule.
func (s *categorizationRuleService) ToggleRule(ctx context.Context, userID, ruleID string, enabled bool) (*domain.CategorizationRule, error) {
	// Get existing rule
	rule, err := s.ruleRepo.GetByID(ctx, ruleID)
	if err != nil {
		return nil, err
	}

	// Verify ownership
	if rule.UserID != userID {
		return nil, domain.ErrRuleNotFound
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
func (s *categorizationRuleService) GetEnabledRules(ctx context.Context, userID string) ([]*domain.CategorizationRule, error) {
	return s.ruleRepo.GetEnabledByUserID(ctx, userID)
}
