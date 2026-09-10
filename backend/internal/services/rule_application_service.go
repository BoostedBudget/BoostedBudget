package services

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	phclient "github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"
	"github.com/maxnussbaum/BoostedBudget/internal/observability"
	"github.com/maxnussbaum/BoostedBudget/internal/rules"
)

// RuleApplicationRepository defines the repository methods needed for rule application.
// This is a subset of the full CategorizationRuleRepository.
type RuleApplicationRepository interface {
	GetEnabledByUserID(ctx context.Context, userID string) ([]*domain.CategorizationRule, error)
	IncrementMatchCount(ctx context.Context, ruleID string) error
}

// RuleExecutionHistoryRepository is the automation audit trail's data access, declared here (as
// RuleApplicationRepository is) so the service depends on the narrow shape it needs rather than on
// the repository package. Implemented by repository.ruleExecutionHistoryRepository.
type RuleExecutionHistoryRepository interface {
	Create(ctx context.Context, execution *domain.RuleExecution) error
	GetByUserID(ctx context.Context, userID string, limit, offset int) ([]*domain.RuleExecution, int, error)
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

	// Executions is the automation audit trail for this transaction: one entry per rule that
	// ACTUALLY CHANGED something, carrying the rule's name and definition as they were at that
	// moment. Issue #1569 Task 5 (#1572).
	//
	// ⚠ Executions is NOT MatchedRuleIDs. MatchedRuleIDs is every rule whose conditions matched,
	// and it drives match counts, which are a "how often is this rule relevant" statistic. A rule
	// can match and change nothing — its target category is already the transaction's category, or
	// the tag it offers is already present. The user's ruling for #1572 is that history records
	// what a rule DID, so those produce no entry here while still counting as a match.
	//
	// ⚠ It is also not MatchedRuleIDs minus the no-ops: the rule name and definition must be
	// captured HERE, inside ApplyRulesToBatch, where the *domain.CategorizationRule is in scope.
	// Re-reading categorization_rules from the writer would capture the rule as it is at WRITE
	// time, which is precisely the drift rule_name and definition_snapshot exist to prevent.
	Executions []RuleExecutionRecord
}

// RuleExecutionRecord is one rule's contribution to one transaction, captured at evaluation time
// and written to rule_execution_history by EmitBatchSideEffects.
//
// ONE RECORD PER RULE, NEVER PER ACTION. `unique_rule_transaction UNIQUE (rule_id, transaction_id)`
// makes a second row for the same pair unrepresentable, so a rule that set the category AND added a
// tag produces a single record with ActionType "both". This is why ActionType is derived by merging
// contributions rather than emitted per action.
type RuleExecutionRecord struct {
	// RuleID is the rule that changed something. Never empty.
	RuleID string
	// RuleName is the rule's name AS IT WAS at evaluation time.
	RuleName string
	// ActionType is one of domain.RuleExecutionAction{Category,Tags,Both}, derived from what this
	// rule actually changed — not from what it was configured to attempt.
	ActionType string
	// DefinitionSnapshot is the marshalled rule definition as it was at evaluation time.
	// nil when marshalling failed, which writes SQL NULL rather than failing the sync.
	DefinitionSnapshot json.RawMessage
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
	ruleRepo    RuleApplicationRepository
	ruleEngine  *rules.RuleEngine
	analytics   phclient.Analytics
	historyRepo RuleExecutionHistoryRepository
	// logger defaults to slog.Default() so every existing NewRuleApplicationService call site
	// keeps compiling and shadow-mode quarantine still reports even where none is injected.
	logger *slog.Logger
	// merchantResolver enriches EffectiveMerchantName so a rule can match the name the USER sees
	// (#1625 Task 11). Optional and FAIL-OPEN: nil, or an error, leaves EffectiveMerchantName
	// unset and the evaluator falls back to the raw provider name. A merchant lookup must never
	// stop a sync from categorising.
	merchantResolver EffectiveMerchantResolver
}

// EffectiveMerchantResolver resolves merchant IDs to the per-user display name, in one query.
// Satisfied by repository.TransactionRepository.
type EffectiveMerchantResolver interface {
	ResolveEffectiveMerchantNames(ctx context.Context, userID string, merchantIDs []string) (map[string]string, error)
}

// NewRuleApplicationService creates a new RuleApplicationService instance.
func NewRuleApplicationService(ruleRepo RuleApplicationRepository) *ruleApplicationService {
	return &ruleApplicationService{
		ruleRepo:   ruleRepo,
		ruleEngine: rules.NewRuleEngine(),
		logger:     slog.Default(),
	}
}

// SetEffectiveMerchantResolver wires per-user merchant name resolution (#1625 Task 11).
//
// Setter injection, matching SetAnalytics/SetLogger, so every existing
// NewRuleApplicationService call site keeps compiling. Unwired, effective_merchant conditions
// simply fall back to the raw provider name rather than failing.
func (s *ruleApplicationService) SetEffectiveMerchantResolver(r EffectiveMerchantResolver) {
	s.merchantResolver = r
}

// enrichEffectiveMerchantNames fills EffectiveMerchantName on a batch in ONE query, so a rule
// written against the name the user sees matches during sync exactly as it does in preview.
//
// Best-effort throughout: no resolver, no merchant ids, or a failed query all leave the field
// unset, and the evaluator falls back to the raw name. Never returns an error — see the field
// comment for why a merchant lookup must not be able to fail a sync.
func (s *ruleApplicationService) enrichEffectiveMerchantNames(ctx context.Context, txns []domain.Transaction, userID string) {
	if s.merchantResolver == nil {
		return
	}
	seen := make(map[string]struct{}, len(txns))
	ids := make([]string, 0, len(txns))
	for i := range txns {
		if txns[i].MerchantID == nil || *txns[i].MerchantID == "" {
			continue
		}
		if _, dup := seen[*txns[i].MerchantID]; dup {
			continue
		}
		seen[*txns[i].MerchantID] = struct{}{}
		ids = append(ids, *txns[i].MerchantID)
	}
	if len(ids) == 0 {
		return
	}
	nameByID, err := s.merchantResolver.ResolveEffectiveMerchantNames(ctx, userID, ids)
	if err != nil {
		s.logger.WarnContext(ctx, "could not resolve effective merchant names; effective_merchant "+
			"conditions will fall back to the raw provider name",
			slog.String("user_id", userID),
			slog.String("error", err.Error()),
		)
		return
	}
	for i := range txns {
		if txns[i].MerchantID == nil {
			continue
		}
		if name, ok := nameByID[*txns[i].MerchantID]; ok && name != "" {
			n := name
			txns[i].EffectiveMerchantName = &n
		}
	}
}

// SetLogger overrides the default logger.
//
// ⚠ TEST SEAM, not a production wiring gap. #1625 Task 9 checked: nothing in cmd/server calls
// this, and nothing should — main.go:91 installs appLogger as slog.Default(), which the
// constructor above already picks up, so the "default" is not a degraded fallback. Its only
// callers capture log output to assert on it. Do not "fix" the missing main.go call.
func (s *ruleApplicationService) SetLogger(logger *slog.Logger) {
	if logger != nil {
		s.logger = logger
	}
}

// SetAnalytics sets the analytics client for event capture.
func (s *ruleApplicationService) SetAnalytics(analytics phclient.Analytics) {
	s.analytics = analytics
}

// SetRuleExecutionHistoryRepository wires the automation audit trail's writer.
// Issue #1569 Task 5 (#1572). Setter injection, exactly as SetAnalytics is, so the ~15 existing
// NewRuleApplicationService call sites keep compiling and a nil repository is a silent no-op rather
// than a panic.
func (s *ruleApplicationService) SetRuleExecutionHistoryRepository(repo RuleExecutionHistoryRepository) {
	s.historyRepo = repo
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

	s.reportQuarantineCandidates(ctx, tx.UserID, userRules)

	// Evaluate rules against transaction.
	//
	// ⚠ EvaluateRules cannot currently return a non-nil error at all — #1625 A8 made every
	// evaluator failure rule-local, and the failures now arrive in result.FailedRuleIDs below.
	// The check is retained as a guard on the SIGNATURE, not as a live path: it is what catches a
	// future change that reintroduces a fatal error, rather than letting one be dropped silently.
	result, err := s.ruleEngine.EvaluateRules(tx, userRules)
	if err != nil {
		return err
	}
	if result != nil {
		s.logFailedRules(ctx, tx.UserID, tx.ID, result.FailedRuleIDs)
	}

	// No rules matched
	if result == nil || (result.TargetKind == nil && len(result.Tags) == 0) {
		return nil
	}

	// Apply category override if matched (first-match-wins)
	if result.TargetKind != nil {
		applyCategoryOverride(tx, result)

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

	// #1625 Task 11: resolve the merchant names the USER sees, once for the whole batch, before
	// any rule runs. The sync path stamps MerchantID immediately before calling this.
	s.enrichEffectiveMerchantNames(ctx, txns, userID)

	// Indexed once for the audit trail: a record must carry the rule's name and definition AS THEY
	// WERE at evaluation time, and the *domain.CategorizationRule holding them is only in scope
	// here. Issue #1569 Task 5.
	rulesByID := make(map[string]*domain.CategorizationRule, len(userRules))
	for _, r := range userRules {
		rulesByID[r.ID] = r
	}

	s.reportQuarantineCandidates(ctx, userID, userRules)

	var applications []BatchRuleApplication

	for i := range txns {
		tx := &txns[i]

		// Skip if the transaction has a manual override or has already been reviewed
		// (reviewed rows are protected from auto-recategorization, Issue #1203).
		if tx.CategorySourceField == domain.CategorySourceManual || tx.Reviewed {
			continue
		}

		// Captured BEFORE any mutation. The audit trail records what a rule CHANGED, and the only
		// way to know that is to compare before with after. Issue #1569 Task 5.
		categoryBefore := categoryAssignment(tx)
		reviewedBefore := tx.Reviewed
		tagsBefore := make(map[string]bool, len(tx.Tags))
		for _, tag := range tx.Tags {
			tagsBefore[tag] = true
		}

		result, err := s.ruleEngine.EvaluateRules(tx, userRules)
		if err != nil {
			// Non-fatal: log and continue with remaining transactions.
			//
			// #1625: this comment said "log and continue" while the code only continued — the
			// error was discarded with no record anywhere. One bad condition silently stopped
			// EVERY rule from applying to this transaction (EvaluateRules returns on the first
			// error), and nothing said so.
			s.logger.WarnContext(ctx, "rule evaluation failed for transaction — no rules applied to it",
				slog.String("user_id", userID),
				slog.String("transaction_id", tx.ID),
				slog.String("error", err.Error()),
			)
			continue
		}

		if result == nil {
			continue
		}
		s.logFailedRules(ctx, userID, tx.ID, result.FailedRuleIDs)

		if result.TargetKind == nil && len(result.Tags) == 0 {
			continue
		}

		// Apply category override if matched.
		if result.TargetKind != nil {
			applyCategoryOverride(tx, result)

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
				Executions: buildRuleExecutionRecords(
					tx, result, rulesByID, categoryBefore, reviewedBefore, tagsBefore),
			})
		}
	}

	return applications, nil
}

// categoryAssignment renders a transaction's category assignment as one comparable value.
//
// THE FOUR FIELDS MOVE AS A UNIT and the database enforces it: the
// `valid_category_override_discriminator` CHECK rejects a row whose category_override_kind is
// 'custom' without a custom_category_override_id. Comparing them one at a time would let a
// half-change read as no change.
//
// DELIBERATELY EXCLUDES category_source, which is bookkeeping about the fact that a rule ran rather
// than a record of what the rule DID: it is set unconditionally whenever result.TargetKind != nil,
// so folding it in would make every category match look like a category change.
//
// ⚠ `reviewed` USED TO BE EXCLUDED HERE TOO, and that was a defect — R73, review round 1 D2. It is
// now handled OUTSIDE this function, in buildRuleExecutionRecords, as its own contribution test.
// The reason it does not belong in this fingerprint is that it is not part of the category
// assignment; the reason it must be tested somewhere is that flipping it is permanent and
// user-visible. Read buildRuleExecutionRecords' `reviewedChanged` for the argument.
//
// Uses a rune Postgres forbids in an identifier as the separator so no combination of real values
// can collide across field boundaries.
func categoryAssignment(tx *domain.Transaction) string {
	parts := []*string{
		tx.CategoryOverrideKind,
		tx.PFCPrimaryOverride,
		tx.PFCDetailedOverride,
		tx.CustomCategoryOverrideID,
	}
	out := ""
	for _, p := range parts {
		if p == nil {
			out += "\x00<nil>"
			continue
		}
		out += "\x00" + *p
	}
	return out
}

// buildRuleExecutionRecords attributes each ACTUAL change to the rule that made it.
//
// Issue #1569 Task 5 (#1572), and this function is where three of that task's four hard questions
// are answered:
//
//   - MATCHED vs CHANGED. A rule can match and do nothing: every tag it offers is already present.
//     Those get no record, per the user's ruling, which is also why `result` is always 'applied'
//     and never 'skipped_manual'.
//     ⚠ R73 NARROWED THIS, and the narrowing is worth stating rather than discovering. A category
//     rule ALSO flips `reviewed` false→true, and that flip is permanent: the next batch hits the
//     `tx.Reviewed → continue` guard and skips the row forever. So for category rules "matched" and
//     "changed" now coincide on every transaction the loop does not skip, and the
//     matched-but-changed-nothing case survives only for tag-only rules. That is deliberate — the
//     alternative silently loses a durable, user-visible state change (a `target_kind='clear'` rule
//     on a fresh Plaid transaction changes NOTHING ELSE) to keep a distinction tidy.
//   - MULTI-RULE. result.MatchedRuleIDs can hold several rules while result.CategoryRuleID names
//     one. The answer is one record per CONTRIBUTING rule, attributed to that rule — the category
//     goes to result.CategoryRuleID, and each landed tag goes to the rule that supplied it (which
//     domain.TagSource.RuleID records). Collapsing several rules onto CategoryRuleID would
//     misattribute another rule's tag, and dropping them would lose it entirely.
//   - ONE ROW PER RULE. unique_rule_transaction UNIQUE (rule_id, transaction_id) makes a second row
//     for the same pair unrepresentable, so a rule that did both is merged into ActionType "both".
//
// Emission order follows result.MatchedRuleIDs, which the engine produces in a deterministic
// priority order, so the records are stable run to run.
//
// ⚠ A tag REMOVED by a replace-behaviour rule is a change nothing here attributes: applyTagsToTransaction
// drops rule-sourced tags that are not re-offered, and the removal belongs to no single rule. Such
// a transaction produces no tag record. Recording removals needs an action_type the live
// `valid_action_type` CHECK does not have.
//
// ⚠ THE `tagsBefore` CLAUSE IS DEFENSIVE-ONLY ON TODAY'S ONLY CALLER — do not read it as exercised.
// The sole production path is transaction_sync_service.processAddedTransactions, and nothing on it
// populates Tags, TagsSource, CategoryOverrideKind, CategorySourceField or Reviewed before
// ApplyRulesToBatch runs: the rows come straight from the Plaid mapper (services/plaid_mapper.go),
// which sets none of them. So in production `tagsBefore` is always EMPTY and `categoryBefore` is
// always the all-nil tuple. Deleting `|| tagsBefore[ts.Tag]` used to leave the entire suite green
// (review round 1, D1); it is now pinned by
// TestApplyRulesToBatch_ARuleWhoseOwnTagIsAlreadyPresentContributesNothing, which constructs the
// unreachable shape on purpose and says so. Any unit fixture that hand-builds a transaction with
// pre-existing annotations is describing a state this caller cannot produce.
func buildRuleExecutionRecords(
	tx *domain.Transaction,
	result *rules.RuleResult,
	rulesByID map[string]*domain.CategorizationRule,
	categoryBefore string,
	reviewedBefore bool,
	tagsBefore map[string]bool,
) []RuleExecutionRecord {
	type contribution struct {
		category bool
		tags     bool
	}
	changed := make(map[string]*contribution)

	categoryChanged := categoryAssignment(tx) != categoryBefore
	// R73 (review round 1, D2): a reviewed false→true transition IS a change, and on the only
	// production caller it is the ONLY change a `target_kind='clear'` rule makes.
	reviewedChanged := !reviewedBefore && tx.Reviewed
	if result.CategoryRuleID != "" && (categoryChanged || reviewedChanged) {
		changed[result.CategoryRuleID] = &contribution{category: true}
	}

	for _, ts := range tx.TagsSource {
		if ts.Source != "rule" || ts.RuleID == nil || tagsBefore[ts.Tag] {
			continue
		}
		c, ok := changed[*ts.RuleID]
		if !ok {
			c = &contribution{}
			changed[*ts.RuleID] = c
		}
		c.tags = true
	}

	if len(changed) == 0 {
		return nil
	}

	records := make([]RuleExecutionRecord, 0, len(changed))
	for _, ruleID := range result.MatchedRuleIDs {
		c, ok := changed[ruleID]
		if !ok {
			continue
		}
		delete(changed, ruleID) // a rule id appearing twice must not yield two rows

		rule := rulesByID[ruleID]
		if rule == nil {
			// Unreachable in practice: every id in MatchedRuleIDs came from userRules. Guarded so
			// a future engine change degrades to a nameless record rather than a nil dereference.
			records = append(records, RuleExecutionRecord{RuleID: ruleID, ActionType: actionTypeFor(c.category, c.tags)})
			continue
		}

		records = append(records, RuleExecutionRecord{
			RuleID:             ruleID,
			RuleName:           rule.Name,
			ActionType:         actionTypeFor(c.category, c.tags),
			DefinitionSnapshot: buildRuleDefinitionSnapshot(rule),
		})
	}

	return records
}

// actionTypeFor maps what a rule changed onto the live `valid_action_type` CHECK's three values.
// Never called with both false: buildRuleExecutionRecords only creates a contribution when
// something changed.
func actionTypeFor(category, tags bool) string {
	switch {
	case category && tags:
		return domain.RuleExecutionActionBoth
	case category:
		return domain.RuleExecutionActionCategory
	default:
		return domain.RuleExecutionActionTags
	}
}

// buildRuleDefinitionSnapshot freezes the rule definition as it stands at execution time.
//
// Mirrors buildBoostDefinitionSnapshot (boost_execution_service.go) field for field in convention,
// including captured_at / captured_by, and matches the field set migration 000164's backfill wrote
// so a backfilled row and a live-written row are the same shape.
//
// DELIBERATELY EXCLUDED, for the same reasons the boost snapshot excludes its equivalents:
//
//	description                                free text, no bearing on behaviour, and a PII
//	                                           surface with no reader
//	match_count, override_count, last_matched_at
//	                                           counters describing the rule's whole lifetime rather
//	                                           than this execution — and this very row is one of the
//	                                           things they count
//	created_at, updated_at                     properties of the definition, not of what it did
//	user_id                                    already a first-class column on the row
//
// ⚠ `conditions` IS DELIBERATELY INCLUDED, THOUGH IT CARRIES USER-TYPED TEXT (merchant substrings,
// amounts). Review round 1 asked whether it belongs with `description`, which is excluded above
// partly as a PII surface. The decision is to keep it, on three grounds, recorded so it is a
// decision rather than an oversight:
//
//  1. The exclusion of `description` rests FIRST on "no bearing on behaviour". `conditions` is the
//     entire answer to "why did this fire" — the one question an orphaned row exists to answer.
//     Dropping it leaves a snapshot that says what happened and cannot say why.
//  2. It is NOT a new exposure. GET /api/v1/auto-categorization already returns every enabled rule
//     to its owner with `conditions` populated (domain.CategorizationRule.Conditions is
//     `json:"conditions"`), as does GET /api/v1/auto-categorization/{id}. The same user sees the
//     same bytes today. GET /api/v1/rule-executions is tenancy-scoped to the row's own user_id, so
//     no second party gains anything.
//  3. What it DOES add is a copy that outlives the rule, which is the point — and which means a
//     GDPR erasure must reach it. It does: rule_execution_history.user_id -> users(id) ON DELETE
//     CASCADE (migration 000164), the row's own path, independent of the rule.
//
// ⚠ The obligation this creates is on any future UI: a rendered condition value is user-typed
// financial text and needs `data-ph-mask` for session replay, exactly as a merchant name does.
// There is no UI today; this is the note for whoever builds one.
//
// Marshalling cannot fail for these types in practice; if it somehow did, a nil snapshot is written
// (SQL NULL) rather than failing the sync. Losing the explanation is bad; refusing to categorize
// the user's transactions because the explanation would not serialise is worse.
func buildRuleDefinitionSnapshot(rule *domain.CategorizationRule) json.RawMessage {
	snapshot := map[string]interface{}{
		"rule_id":         rule.ID,
		"name":            rule.Name,
		"enabled":         rule.Enabled,
		"priority":        rule.Priority,
		"condition_logic": rule.ConditionLogic,
		"conditions":      rule.Conditions,
		"tag_behavior":    rule.TagBehavior,
		"source":          rule.Source,
		"captured_at":     time.Now().UTC().Format(time.RFC3339),
		"captured_by":     "rule_application_service",
	}
	if rule.TargetKind != nil {
		snapshot["target_kind"] = *rule.TargetKind
	}
	if rule.TargetPFCPrimary != nil {
		snapshot["target_pfc_primary"] = *rule.TargetPFCPrimary
	}
	if rule.TargetPFCDetailed != nil {
		snapshot["target_pfc_detailed"] = *rule.TargetPFCDetailed
	}
	if rule.TargetCustomCategoryID != nil {
		snapshot["target_custom_category_id"] = *rule.TargetCustomCategoryID
	}
	if len(rule.TargetTags) > 0 {
		snapshot["target_tags"] = rule.TargetTags
	}

	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return nil
	}
	return encoded
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

// applyCategoryOverride applies a category result to a transaction based on the target kind.
// Extracted to eliminate duplication between ApplyRulesToTransaction and ApplyRulesToBatch.
func applyCategoryOverride(tx *domain.Transaction, result *rules.RuleResult) {
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

		s.writeExecutionHistory(ctx, userID, app)
	}
}

// writeExecutionHistory persists the automation audit trail for one transaction.
// Issue #1569 Task 5 (#1572).
//
// WHY IT LIVES HERE AND NOT IN ApplyRulesToBatch. rule_execution_history.transaction_id carries a
// foreign key to transactions — ON DELETE SET NULL since migration 000157, but an FK all the same —
// so a row written before the transaction exists, or for one the upsert never inserted, violates it
// (SQLSTATE 23503). EmitBatchSideEffects runs AFTER CreateBatch and is already gated on insertedIDs,
// which CreateBatch derives from `xmax::text::bigint = 0`: genuinely new rows only, never
// conflict-preserved ones.
//
// BEST-EFFORT, DELIBERATELY. This runs after the transactions are already committed and the caller
// has no error channel. A failed audit write is logged and the sync continues; the alternative is
// telling the user their transactions did not sync because the explanation of the sync did not save.
func (s *ruleApplicationService) writeExecutionHistory(ctx context.Context, userID string, app BatchRuleApplication) {
	if s.historyRepo == nil || len(app.Executions) == 0 {
		return
	}

	transactionID := app.TransactionID
	for _, rec := range app.Executions {
		ruleID := rec.RuleID
		var ruleName *string
		if rec.RuleName != "" {
			ruleName = &rec.RuleName
		}

		err := s.historyRepo.Create(ctx, &domain.RuleExecution{
			RuleID:        &ruleID,
			TransactionID: &transactionID,
			UserID:        userID,
			RuleName:      ruleName,
			// Only actual changes reach this point, so the result is always 'applied'. See
			// domain.RuleExecutionResultApplied for why skipped_manual is unrecordable here.
			ActionType:         rec.ActionType,
			Result:             domain.RuleExecutionResultApplied,
			DefinitionSnapshot: rec.DefinitionSnapshot,
		})
		if err != nil {
			slog.WarnContext(ctx, "failed to record rule execution history",
				"error", err,
				"rule_id", rec.RuleID,
				"transaction_id", app.TransactionID,
				"action_type", rec.ActionType,
			)
		}
	}
}

// ─── Shadow-mode quarantine (#1625) ─────────────────────────────────────────────────────────
//
// A rule whose conditions do not validate has always executed anyway: unknown condition types
// evaluate to no-match per condition, which under OR logic lets a VALID sibling still fire the
// rule. So a rule the user cannot see is broken quietly matches on a condition set that is not
// what it says. Three more traps ride along: a rule with ZERO conditions matches EVERY
// transaction (engine.go), an invalid condition_logic silently degrades to AND, and one
// evaluator error aborts every other rule for that transaction.
//
// ⚠ This is no longer shadow mode. #1625 Task 2 enforces the three reasons that CANNOT be
// produced through the API (no_conditions, invalid_condition_logic, unnormalizable_condition)
// plus uncoercible values: those rules match nothing, and the read model tells the user so. The
// fourth, invalid_condition_value_shape, IS reachable through the ordinary API — #346 never
// validated value shapes — so it is REPAIRED by read-path coercion and keeps running. This
// reporter still counts and logs both kinds; the log line says which.
// logFailedRules records rules that could not be EVALUATED, one line each.
//
// #1625 A8: an evaluator error used to abort EvaluateRules, so one broken rule silently
// suppressed every other rule for that transaction and the batch handler logged a single
// transaction-level line. Errors are rule-local now, so the honest record is per-rule — and it
// names which rule, which the old line could not.
func (s *ruleApplicationService) logFailedRules(ctx context.Context, userID, txID string, failed []string) {
	for _, ruleID := range failed {
		s.logger.WarnContext(ctx, "rule evaluation failed — this rule was skipped, others still applied",
			slog.String("user_id", userID),
			slog.String("transaction_id", txID),
			slog.String("rule_id", ruleID),
		)
	}
}

func (s *ruleApplicationService) reportQuarantineCandidates(ctx context.Context, userID string, rules []*domain.CategorizationRule) {
	for _, rule := range rules {
		reason := quarantineReason(rule)
		if reason == "" {
			continue
		}
		// The counter is the INVENTORY that a future enforcing pass depends on — the log line
		// alone is not aggregatable, and "counts and logs" was only half true without it.
		// Only the fixed reason vocabulary is labelled; ids and user content stay in the log.
		observability.RecordRuleQuarantineCandidate(reason)
		s.logger.WarnContext(ctx, "categorization rule is quarantined",
			slog.String("user_id", userID),
			slog.String("rule_id", rule.ID),
			slog.String("reason", string(reason)),
			// The operator cannot infer this from the reason without knowing the enforcement
			// set, and the whole failure class here is a rule being described as one thing
			// while it does another.
			slog.Bool("stopped_matching", quarantineStopsMatching(reason)),
		)
	}
}

// quarantineReason is a thin alias for the ONE classifier, rules.QuarantineReasonFor.
//
// #1625 Task 2 moved the implementation into package rules because the engine has to enforce the
// same classification it reports, and the engine cannot import services. Keeping the local name
// keeps every service call site unchanged; what it must never become again is a SECOND
// implementation, because a rule the user is told is quarantined and a rule the engine refuses to
// run would then be two different sets.
func quarantineReason(rule *domain.CategorizationRule) observability.QuarantineReason {
	return rules.QuarantineReasonFor(rule)
}

// quarantineStopsMatching is the ONE answer to "is this rule actually prevented from matching".
//
// #1625 final review I3. There were THREE sources of truth: RuleMatches gated on the kill switch,
// while annotateQuarantineStatus and reportQuarantineCandidates consulted
// observability.QuarantineReasonStopsMatching alone. With RULE_QUARANTINE_ENFORCE off, the card
// therefore said "Not running" while the engine went on recategorising the user's ledger — the
// divergence RuleMatches' own doc comment calls impossible, and the precise failure this ticket
// exists to remove.
//
// The reason predicate answers "does this REASON stop a rule". That is only half the question
// while a kill switch exists; the other half is whether enforcement is on at all.
func quarantineStopsMatching(reason observability.QuarantineReason) bool {
	return rules.QuarantineEnforced() && observability.QuarantineReasonStopsMatching(reason)
}
