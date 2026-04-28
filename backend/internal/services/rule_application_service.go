package services

import (
	"context"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	phclient "github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"
	"github.com/maxnussbaum/BoostedBudget/internal/rules"
)

// RuleApplicationRepository defines the repository methods needed for rule application.
// This is a subset of the full CategorizationRuleRepository.
type RuleApplicationRepository interface {
	GetEnabledByUserID(ctx context.Context, userID string) ([]*domain.CategorizationRule, error)
	IncrementMatchCount(ctx context.Context, ruleID string) error
}

// RuleApplicationService applies categorization rules to transactions.
type RuleApplicationService interface {
	// ApplyRulesToTransaction applies all enabled rules to a single transaction.
	// Respects manual category overrides (doesn't change if CategorySource is "manual").
	// Returns nil if no rules matched or if transaction already has manual override.
	ApplyRulesToTransaction(ctx context.Context, tx *domain.Transaction) error

	// ApplyRulesToBatch applies rules to multiple transactions.
	// Fetches rules once and applies to all transactions.
	ApplyRulesToBatch(ctx context.Context, txns []domain.Transaction, userID string) error
}

// ruleApplicationService implements RuleApplicationService.
type ruleApplicationService struct {
	ruleRepo   RuleApplicationRepository
	ruleEngine *rules.RuleEngine
	analytics  phclient.Analytics
}

// NewRuleApplicationService creates a new RuleApplicationService instance.
func NewRuleApplicationService(ruleRepo RuleApplicationRepository) *ruleApplicationService {
	return &ruleApplicationService{
		ruleRepo:   ruleRepo,
		ruleEngine: rules.NewRuleEngine(),
	}
}

// SetAnalytics sets the analytics client for event capture.
func (s *ruleApplicationService) SetAnalytics(analytics phclient.Analytics) {
	s.analytics = analytics
}

// ApplyRulesToTransaction applies all enabled rules to a single transaction.
func (s *ruleApplicationService) ApplyRulesToTransaction(ctx context.Context, tx *domain.Transaction) error {
	// Skip if transaction already has a manual category override
	if tx.CategorySourceField == domain.CategorySourceManual {
		return nil
	}

	// Get enabled rules for the user
	userRules, err := s.ruleRepo.GetEnabledByUserID(ctx, tx.UserID)
	if err != nil {
		return err
	}

	// No rules - nothing to apply
	if len(userRules) == 0 {
		return nil
	}

	// Evaluate rules against transaction
	result, err := s.ruleEngine.EvaluateRules(tx, userRules)
	if err != nil {
		return err
	}

	// No rules matched
	if result == nil || (result.TargetKind == nil && len(result.Tags) == 0) {
		return nil
	}

	// Apply category override if matched (first-match-wins)
	if result.TargetKind != nil {
		switch *result.TargetKind {
		case "pfc":
			tx.CategoryOverrideKind = result.TargetKind
			tx.PFCPrimaryOverride = result.TargetPFCPrimary
			tx.PFCDetailedOverride = result.TargetPFCDetailed
			tx.CustomCategoryOverrideID = nil
		case "custom":
			tx.CategoryOverrideKind = result.TargetKind
			tx.CustomCategoryOverrideID = result.TargetCustomCategoryID
			tx.PFCPrimaryOverride = nil
			tx.PFCDetailedOverride = nil
		case "clear":
			tx.CategoryOverrideKind = nil
			tx.PFCPrimaryOverride = nil
			tx.PFCDetailedOverride = nil
			tx.CustomCategoryOverrideID = nil
		}
		tx.CategorySourceField = domain.CategorySourceRule

		if s.analytics != nil && len(result.MatchedRuleIDs) > 0 {
			s.analytics.Capture(tx.UserID, phclient.EventAutoCategorizationExecuted, map[string]interface{}{
				"transaction_id": tx.ID,
				"rule_id":        result.MatchedRuleIDs[0],
			})
		}
	}

	// Apply tags (all-match)
	if len(result.Tags) > 0 {
		// Collect existing manual tags to preserve
		existingManualTags := make(map[string]domain.TagSource)
		for _, ts := range tx.TagsSource {
			if ts.Source == "manual" {
				existingManualTags[ts.Tag] = ts
			}
		}

		// Check if we need to replace rule tags
		replaceRuleTags := false
		for _, ruleID := range result.TagRuleIDs {
			for _, rule := range userRules {
				if rule.ID == ruleID && rule.TagBehavior == "replace" {
					replaceRuleTags = true
					break
				}
			}
			if replaceRuleTags {
				break
			}
		}

		// Build new tag list
		newTags := make([]string, 0)
		newTagSources := make([]domain.TagSource, 0)

		// Add manual tags first (always preserved)
		for tag, source := range existingManualTags {
			newTags = append(newTags, tag)
			newTagSources = append(newTagSources, source)
		}

		// Add rule tags (unless we're replacing and they already exist)
		tagSet := make(map[string]bool)
		for _, tag := range newTags {
			tagSet[tag] = true
		}

		for i, tag := range result.Tags {
			if !tagSet[tag] {
				newTags = append(newTags, tag)
				newTagSources = append(newTagSources, result.TagSources[i])
				tagSet[tag] = true
			}
		}

		tx.Tags = newTags
		tx.TagsSource = newTagSources
	}

	// Increment match counts for matched rules
	for _, ruleID := range result.MatchedRuleIDs {
		if err := s.ruleRepo.IncrementMatchCount(ctx, ruleID); err != nil {
			// Log but don't fail - incrementing counts is non-critical
			continue
		}
	}

	return nil
}

// ApplyRulesToBatch applies rules to multiple transactions.
func (s *ruleApplicationService) ApplyRulesToBatch(ctx context.Context, txns []domain.Transaction, userID string) error {
	if len(txns) == 0 {
		return nil
	}

	// Get enabled rules once for all transactions
	userRules, err := s.ruleRepo.GetEnabledByUserID(ctx, userID)
	if err != nil {
		return err
	}

	// No rules - nothing to apply
	if len(userRules) == 0 {
		return nil
	}

	// Apply rules to each transaction
	for i := range txns {
		tx := &txns[i]

		// Skip if transaction already has a manual category override
		if tx.CategorySourceField == domain.CategorySourceManual {
			continue
		}

		// Evaluate rules against transaction
		result, err := s.ruleEngine.EvaluateRules(tx, userRules)
		if err != nil {
			// Log but continue with other transactions
			continue
		}

		// No rules matched
		if result == nil || (result.TargetKind == nil && len(result.Tags) == 0) {
			continue
		}

		// Apply category override if matched
		if result.TargetKind != nil {
			switch *result.TargetKind {
			case "pfc":
				tx.CategoryOverrideKind = result.TargetKind
				tx.PFCPrimaryOverride = result.TargetPFCPrimary
				tx.PFCDetailedOverride = result.TargetPFCDetailed
				tx.CustomCategoryOverrideID = nil
			case "custom":
				tx.CategoryOverrideKind = result.TargetKind
				tx.CustomCategoryOverrideID = result.TargetCustomCategoryID
				tx.PFCPrimaryOverride = nil
				tx.PFCDetailedOverride = nil
			case "clear":
				tx.CategoryOverrideKind = nil
				tx.PFCPrimaryOverride = nil
				tx.PFCDetailedOverride = nil
				tx.CustomCategoryOverrideID = nil
			}
			tx.CategorySourceField = domain.CategorySourceRule

			if s.analytics != nil && len(result.MatchedRuleIDs) > 0 {
				s.analytics.Capture(userID, phclient.EventAutoCategorizationExecuted, map[string]interface{}{
					"transaction_id": tx.ID,
					"rule_id":        result.MatchedRuleIDs[0],
				})
			}
		}

		// Apply tags
		if len(result.Tags) > 0 {
			// Preserve manual tags
			existingManualTags := make(map[string]domain.TagSource)
			for _, ts := range tx.TagsSource {
				if ts.Source == "manual" {
					existingManualTags[ts.Tag] = ts
				}
			}

			newTags := make([]string, 0)
			newTagSources := make([]domain.TagSource, 0)

			for tag, source := range existingManualTags {
				newTags = append(newTags, tag)
				newTagSources = append(newTagSources, source)
			}

			tagSet := make(map[string]bool)
			for _, tag := range newTags {
				tagSet[tag] = true
			}

			for i, tag := range result.Tags {
				if !tagSet[tag] {
					newTags = append(newTags, tag)
					newTagSources = append(newTagSources, result.TagSources[i])
					tagSet[tag] = true
				}
			}

			tx.Tags = newTags
			tx.TagsSource = newTagSources
		}

		// Increment match counts
		for _, ruleID := range result.MatchedRuleIDs {
			_ = s.ruleRepo.IncrementMatchCount(ctx, ruleID)
		}
	}

	return nil
}
