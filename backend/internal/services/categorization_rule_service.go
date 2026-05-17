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
	ruleRepo           repository.CategorizationRuleRepository
	customCategoryRepo repository.CustomCategoryRepository
}

// NewCategorizationRuleService creates a new CategorizationRuleService instance.
func NewCategorizationRuleService(
	ruleRepo repository.CategorizationRuleRepository,
	customCategoryRepo repository.CustomCategoryRepository,
) CategorizationRuleService {
	return &categorizationRuleService{
		ruleRepo:           ruleRepo,
		customCategoryRepo: customCategoryRepo,
	}
}

// enrichCustomCategoryDisplay fetches the custom-category display name for a rule's target
// in a single batched query. No-ops when the rule has no custom-category target.
func (s *categorizationRuleService) enrichCustomCategoryDisplay(ctx context.Context, userID string, rules []*domain.CategorizationRule) error {
	ids := make([]string, 0, len(rules))
	seen := make(map[string]struct{}, len(rules))
	for _, r := range rules {
		if r.TargetCustomCategoryID != nil {
			id := *r.TargetCustomCategoryID
			if _, ok := seen[id]; !ok {
				seen[id] = struct{}{}
				ids = append(ids, id)
			}
		}
	}
	if len(ids) == 0 {
		return nil
	}
	names, err := s.customCategoryRepo.GetNamesByIDs(ctx, userID, ids)
	if err != nil {
		return err
	}
	for _, r := range rules {
		if r.TargetCustomCategoryID != nil {
			if name, ok := names[*r.TargetCustomCategoryID]; ok {
				r.TargetCustomCategoryDisplay = &name
			}
		}
	}
	return nil
}

// CreateRule creates a new categorization rule (unlimited rules for all tiers).
func (s *categorizationRuleService) CreateRule(ctx context.Context, userID string, req *domain.CreateCategorizationRuleRequest) (*domain.CategorizationRule, error) {
	// Create the rule
	rule := &domain.CategorizationRule{
		UserID:                 userID,
		Name:                   req.Name,
		Description:            req.Description,
		Enabled:                true, // Default enabled
		Priority:               req.Priority,
		Conditions:             req.Conditions,
		ConditionLogic:         req.ConditionLogic,
		TargetKind:             req.TargetKind,
		TargetPFCPrimary:       req.TargetPFCPrimary,
		TargetPFCDetailed:      req.TargetPFCDetailed,
		TargetCustomCategoryID: req.TargetCustomCategoryID,
		TargetTags:             req.TargetTags,
		TagBehavior:            req.TagBehavior,
		Source:                 "manual",
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

	rule.PopulatePFCDisplay()
	if err := s.enrichCustomCategoryDisplay(ctx, userID, []*domain.CategorizationRule{rule}); err != nil {
		return nil, err
	}
	return rule, nil
}

// GetUserRules retrieves all rules for a user, ordered by priority.
func (s *categorizationRuleService) GetUserRules(ctx context.Context, userID string) ([]*domain.CategorizationRule, error) {
	rules, err := s.ruleRepo.GetByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}
	for _, r := range rules {
		r.PopulatePFCDisplay()
	}
	if err := s.enrichCustomCategoryDisplay(ctx, userID, rules); err != nil {
		return nil, err
	}
	return rules, nil
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

	rule.PopulatePFCDisplay()
	if err := s.enrichCustomCategoryDisplay(ctx, userID, []*domain.CategorizationRule{rule}); err != nil {
		return nil, err
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
	if req.TargetKind != nil {
		rule.TargetKind = req.TargetKind
	}
	if req.TargetPFCPrimary != nil {
		rule.TargetPFCPrimary = req.TargetPFCPrimary
	}
	if req.TargetPFCDetailed != nil {
		rule.TargetPFCDetailed = req.TargetPFCDetailed
	}
	if req.TargetCustomCategoryID != nil {
		rule.TargetCustomCategoryID = req.TargetCustomCategoryID
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

	rule.PopulatePFCDisplay()
	if err := s.enrichCustomCategoryDisplay(ctx, userID, []*domain.CategorizationRule{rule}); err != nil {
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

	rule.PopulatePFCDisplay()
	if err := s.enrichCustomCategoryDisplay(ctx, userID, []*domain.CategorizationRule{rule}); err != nil {
		return nil, err
	}
	return rule, nil
}

// GetEnabledRules retrieves all enabled rules for a user.
func (s *categorizationRuleService) GetEnabledRules(ctx context.Context, userID string) ([]*domain.CategorizationRule, error) {
	rules, err := s.ruleRepo.GetEnabledByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}
	for _, r := range rules {
		r.PopulatePFCDisplay()
	}
	if err := s.enrichCustomCategoryDisplay(ctx, userID, rules); err != nil {
		return nil, err
	}
	return rules, nil
}
