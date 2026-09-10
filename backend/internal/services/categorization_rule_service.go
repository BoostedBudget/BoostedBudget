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

	// PreviewRule reports which recent transactions a candidate rule would match, without
	// persisting anything (#1625).
	PreviewRule(ctx context.Context, userID string, rule *domain.CategorizationRule, limit int) (*domain.RulePreviewResult, error)
}

// categorizationRuleService implements CategorizationRuleService.
type categorizationRuleService struct {
	ruleRepo           repository.CategorizationRuleRepository
	customCategoryRepo repository.CustomCategoryRepository
	// transactionReader backs PreviewRule (#1625). Optional: injected via SetTransactionReader
	// so every existing constructor call site keeps compiling.
	transactionReader PreviewTransactionReader
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

// assertTargetCategoryOwnedAndLive refuses a custom-category rule target that is not the caller's
// (Issue #1540's IDOR guard) or that has been tombstoned (Issue #1569 Task 9). A nil target is a
// no-op. GetLiveNamesByIDs answers both questions in one query: it is user-scoped AND filters
// `deleted_at IS NULL`, so an ID absent from the returned map is either not owned or deleted.
//
// The tombstone half is not tidiness. DeleteWithReassignment DISABLES the rules that pointed at a
// deleted category rather than deleting or blanking them, on the promise that a rule whose target
// the user can no longer see cannot keep acting. That promise is worth nothing if the rule can be
// ARMED AGAIN afterwards, which is why this is called from the two re-enable paths (UpdateRule's
// `enabled` field and ToggleRule) and not only from the two places that supply a target.
func (s *categorizationRuleService) assertTargetCategoryOwnedAndLive(ctx context.Context, userID string, targetID *string) error {
	if targetID == nil {
		return nil
	}
	names, err := s.customCategoryRepo.GetLiveNamesByIDs(ctx, userID, []string{*targetID})
	if err != nil {
		return err
	}
	if _, ok := names[*targetID]; !ok {
		return domain.ErrCategoryNotFound
	}
	return nil
}

// CreateRule creates a new categorization rule (unlimited rules for all tiers).
func (s *categorizationRuleService) CreateRule(ctx context.Context, userID string, req *domain.CreateCategorizationRuleRequest) (*domain.CategorizationRule, error) {
	// IDOR check: reject a target_custom_category_id belonging to another user before
	// persisting (Issue #1540), or one that has been tombstoned (Issue #1569 Task 9).
	if err := s.assertTargetCategoryOwnedAndLive(ctx, userID, req.TargetCustomCategoryID); err != nil {
		return nil, err
	}

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
		annotateQuarantineStatus(r)
	}
	if err := s.enrichCustomCategoryDisplay(ctx, userID, rules); err != nil {
		return nil, err
	}
	return rules, nil
}

// annotateQuarantineStatus fills the response-only quarantine fields.
//
// #1625 Task 7. It classifies with quarantineReason -- the SAME function that labels the
// shadow-mode metric -- rather than a second implementation, so the status a user is shown and
// the inventory an operator reads can never describe the same rule differently.
func annotateQuarantineStatus(r *domain.CategorizationRule) {
	if r == nil {
		return
	}
	reason := quarantineReason(r)
	if reason == "" {
		return
	}
	r.QuarantineReason = string(reason)
	// Quarantined means "the server is stopping this rule RIGHT NOW", not "the rule has a
	// problem". The reason field carries the problem; this field carries the consequence.
	//
	// ⚠ #1625 second fix wave H2/M3 settled this once, and every surface derives from it: the
	// value is SWITCH-DEPENDENT. quarantineStopsMatching is
	// rules.QuarantineEnforced() && observability.QuarantineReasonStopsMatching(reason), so with
	// RULE_QUARANTINE_ENFORCE off an enforcement-class rule reports false — because the engine
	// really is running it. The corollary that cost three surfaces a correction: false does NOT
	// mean "repaired". See internal/services/quarantine_published_contract_test.go.
	//
	// ⚠ #1625 final review I1: this comment used to explain why invalid_condition_logic and
	// unnormalizable_condition are NOT reported as stopped — sitting on the line that reports them
	// as stopped. Both are `true` in QuarantineReasonStopsMatching and have been since Task 2
	// moved enforcement into RuleMatches; the comment described the behaviour of an earlier round
	// and was falsified by a later commit on the same branch. The surviving distinction is
	// invalid_condition_value_shape, which is REPAIRED at read time (domain.CoercedValue) and DOES
	// still match in BOTH switch positions.
	//
	// ⚠ #1625 final review I3: this goes through quarantineStopsMatching, NOT
	// QuarantineReasonStopsMatching directly, so the badge and the engine cannot disagree while the
	// RULE_QUARANTINE_ENFORCE kill switch is off.
	r.Quarantined = quarantineStopsMatching(reason)
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
	annotateQuarantineStatus(rule)
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
		// IDOR check: reject a target belonging to another user (#1540) or a tombstoned one (#1569).
		if err := s.assertTargetCategoryOwnedAndLive(ctx, userID, req.TargetCustomCategoryID); err != nil {
			return nil, err
		}
		rule.TargetCustomCategoryID = req.TargetCustomCategoryID
	}

	// Issue #1569 Task 9: ARMING the rule re-checks the target it ALREADY has.
	//
	// The check above only fires when the request supplies a target. `PATCH {"enabled": true}`
	// supplies none, so without this second gate a rule that DeleteWithReassignment disabled could
	// be switched straight back on with `target_custom_category_id` still naming a tombstoned
	// category — and ruleApplicationService would then write that id onto transactions, which is
	// exactly the "keeps firing at something the user cannot see" failure the disable exists to
	// prevent. Skipped when the request also supplies a target, because the block above has
	// already validated the value being written.
	if req.Enabled != nil && *req.Enabled && req.TargetCustomCategoryID == nil {
		if err := s.assertTargetCategoryOwnedAndLive(ctx, userID, rule.TargetCustomCategoryID); err != nil {
			return nil, err
		}
	}
	if req.TargetTags != nil {
		rule.TargetTags = *req.TargetTags
	}
	if req.TagBehavior != nil {
		rule.TagBehavior = *req.TagBehavior
	}

	// Validate.
	//
	// #1625 final review B1: the condition SHAPE check applies only when this request supplied
	// conditions. `req.Conditions != nil` is the same expression that gated the merge above, so the
	// rule is exactly "judge what this request writes, do not re-judge what it left alone" — see
	// CategorizationRule.ValidateForUpdate for why re-judging made every legacy rule un-renamable.
	if err := rule.ValidateForUpdate(req.Conditions != nil); err != nil {
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

	// Issue #1569 Task 9: the OTHER re-enable path. UpdateRule's `enabled` field is not the only
	// way back on; this one takes the flag directly. Same gate, same reason — see
	// assertTargetCategoryOwnedAndLive.
	if enabled {
		if err := s.assertTargetCategoryOwnedAndLive(ctx, userID, rule.TargetCustomCategoryID); err != nil {
			return nil, err
		}
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
