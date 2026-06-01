package services

import (
	"context"
	"time"

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

// BatchRuleApplication records the outcome of applying rules to one transaction
// during the batch (sync) path.  Side-effects (analytics + match-count) are
// deferred to EmitBatchSideEffects so they can be gated on persistence outcome.
type BatchRuleApplication struct {
	// TransactionID is the ID of the transaction (must be pre-assigned before
	// ApplyRulesToBatch runs so it can be joined to the insertedIDs from CreateBatch).
	TransactionID string
	// MatchedRuleIDs contains all rule IDs that matched (category + tag rules).
	MatchedRuleIDs []string
	// CategoryApplied is true when a rule set a category action on the transaction.
	CategoryApplied bool
	// CategoryRuleID is the ID of the specific rule that set the category.
	// Empty when CategoryApplied is false.
	CategoryRuleID string
}

// RuleApplicationService applies categorization rules to transactions.
type RuleApplicationService interface {
	// ApplyRulesToTransaction applies all enabled rules to a single transaction.
	// Respects manual category overrides (doesn't change if CategorySource is "manual").
	// Returns nil if no rules matched or if transaction already has manual override.
	ApplyRulesToTransaction(ctx context.Context, tx *domain.Transaction) error

	// ApplyRulesToBatch applies rules to multiple transactions.
	// Fetches rules once, mutates each transaction in place, and returns one
	// BatchRuleApplication per matched transaction.  It does NOT emit analytics
	// or increment match counts — call EmitBatchSideEffects after CreateBatch
	// to gate those side-effects on genuine inserts only (Issue #1213).
	ApplyRulesToBatch(ctx context.Context, txns []domain.Transaction, userID string) ([]BatchRuleApplication, error)

	// EmitBatchSideEffects fires analytics and increments per-rule match counts,
	// but ONLY for applications whose TransactionID is present in insertedIDs.
	// This ensures side-effects reflect only transactions that were actually
	// (re)categorized by the upsert (i.e. fresh INSERTs), not conflict-preserved rows.
	EmitBatchSideEffects(ctx context.Context, userID string, applications []BatchRuleApplication, insertedIDs map[string]bool)
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
	// Skip if the transaction has a manual category override or has already been
	// reviewed — reviewed transactions are protected from auto-recategorization
	// (Issue #1203).
	if tx.CategorySourceField == domain.CategorySourceManual || tx.Reviewed {
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

		// A rule applying a category action counts as an automated review
		// (Issue #1203). Tag-only matches do NOT mark the transaction reviewed.
		if !tx.Reviewed {
			tx.Reviewed = true
			now := time.Now()
			tx.ReviewedAt = &now
		}

		// Issue #1213: use CategoryRuleID (the rule that actually set the category),
		// not MatchedRuleIDs[0] which may be a higher-priority tag-only rule.
		if s.analytics != nil && result.CategoryRuleID != "" {
			s.analytics.Capture(tx.UserID, phclient.EventAutoCategorizationExecuted, map[string]interface{}{
				"transaction_id": tx.ID,
				"rule_id":        result.CategoryRuleID,
			})
		}
	}

	// Apply tags (all-match). The rule engine already handles TagBehavior="replace"
	// by clearing accumulated tags before adding the replacing rule's tags, so we
	// just need to merge result.Tags with the existing manual tags here.
	if len(result.Tags) > 0 {
		s.applyTagsToTransaction(tx, result)
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
// It mutates each transaction in place and returns one BatchRuleApplication per
// matched transaction.  Side-effects (analytics, match-count) are NOT emitted
// here — call EmitBatchSideEffects after CreateBatch to gate on insert outcome.
func (s *ruleApplicationService) ApplyRulesToBatch(ctx context.Context, txns []domain.Transaction, userID string) ([]BatchRuleApplication, error) {
	if len(txns) == 0 {
		return nil, nil
	}

	// Get enabled rules once for all transactions.
	userRules, err := s.ruleRepo.GetEnabledByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}

	if len(userRules) == 0 {
		return nil, nil
	}

	var applications []BatchRuleApplication

	for i := range txns {
		tx := &txns[i]

		// Skip if the transaction has a manual override or has already been reviewed
		// (reviewed rows are protected from auto-recategorization, Issue #1203).
		if tx.CategorySourceField == domain.CategorySourceManual || tx.Reviewed {
			continue
		}

		result, err := s.ruleEngine.EvaluateRules(tx, userRules)
		if err != nil {
			// Non-fatal: log and continue with remaining transactions.
			continue
		}

		if result == nil || (result.TargetKind == nil && len(result.Tags) == 0) {
			continue
		}

		// Apply category override if matched.
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

			// A rule applying a category counts as an automated review (Issue #1203).
			// Tag-only matches do not mark reviewed.
			if !tx.Reviewed {
				tx.Reviewed = true
				now := time.Now()
				tx.ReviewedAt = &now
			}
		}

		// Apply tags (all-match).
		if len(result.Tags) > 0 {
			s.applyTagsToTransaction(tx, result)
		}

		if len(result.MatchedRuleIDs) > 0 {
			applications = append(applications, BatchRuleApplication{
				TransactionID:   tx.ID,
				MatchedRuleIDs:  result.MatchedRuleIDs,
				CategoryApplied: result.TargetKind != nil,
				CategoryRuleID:  result.CategoryRuleID,
			})
		}
	}

	return applications, nil
}

// applyTagsToTransaction merges rule tags with existing manual tags, deduplicating as needed.
// Manual tags are always preserved; rule tags are added if not already present.
func (s *ruleApplicationService) applyTagsToTransaction(tx *domain.Transaction, result *rules.RuleResult) {
	existingManualTags := make(map[string]domain.TagSource)
	for _, ts := range tx.TagsSource {
		if ts.Source == "manual" {
			existingManualTags[ts.Tag] = ts
		}
	}

	newTags := make([]string, 0)
	newTagSources := make([]domain.TagSource, 0)

	// Add manual tags first (always preserved)
	for tag, source := range existingManualTags {
		newTags = append(newTags, tag)
		newTagSources = append(newTagSources, source)
	}

	// Add rule tags (deduplicate with existing)
	tagSet := make(map[string]bool)
	for _, tag := range newTags {
		tagSet[tag] = true
	}

	for j, tag := range result.Tags {
		if !tagSet[tag] {
			newTags = append(newTags, tag)
			newTagSources = append(newTagSources, result.TagSources[j])
			tagSet[tag] = true
		}
	}

	tx.Tags = newTags
	tx.TagsSource = newTagSources
}

// EmitBatchSideEffects fires analytics and per-rule match-count increments for
// batch rule applications, gated on the insertedIDs returned by CreateBatch.
// Only transactions that were genuinely inserted (not conflict-preserved) receive
// side-effects, preventing auto_categorization_executed over-counting (Issue #1213).
func (s *ruleApplicationService) EmitBatchSideEffects(ctx context.Context, userID string, applications []BatchRuleApplication, insertedIDs map[string]bool) {
	for _, app := range applications {
		if !insertedIDs[app.TransactionID] {
			continue
		}

		// Emit analytics only when a category action was applied.
		if s.analytics != nil && app.CategoryApplied && app.CategoryRuleID != "" {
			s.analytics.Capture(userID, phclient.EventAutoCategorizationExecuted, map[string]interface{}{
				"transaction_id": app.TransactionID,
				"rule_id":        app.CategoryRuleID, // Issue #1213: use CategoryRuleID, not MatchedRuleIDs[0]
			})
		}

		// Increment match counts for all matched rules (category + tag-only).
		for _, ruleID := range app.MatchedRuleIDs {
			_ = s.ruleRepo.IncrementMatchCount(ctx, ruleID)
		}
	}
}
