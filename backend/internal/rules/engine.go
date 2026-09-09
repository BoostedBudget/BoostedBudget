package rules

import (
	"os"
	"sort"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// RuleResult contains the outcome of evaluating rules against a transaction.
type RuleResult struct {
	// Category action is set by the first matching rule with target category fields (first-match-wins).
	TargetKind             *string
	TargetPFCPrimary       *string
	TargetPFCDetailed      *string
	TargetCustomCategoryID *string
	// CategoryRuleID is the ID of the rule that set the category.
	CategoryRuleID string

	// Tags are accumulated from all matching rules (all-match).
	Tags []string
	// TagRuleIDs are the IDs of rules that contributed tags.
	TagRuleIDs []string

	// TagSources tracks which rule contributed each tag.
	TagSources []domain.TagSource

	// MatchedRuleIDs are all rules that matched the transaction.
	MatchedRuleIDs []string

	// FailedRuleIDs are rules that could not be evaluated at all. #1625 A8: an evaluator error is
	// LOCAL to its rule — it skips that rule and no other. Before this, EvaluateRules returned on
	// the first error, so one un-evaluatable condition silently suppressed every other rule the
	// user had for that transaction.
	//
	// Reported rather than swallowed: quarantine classification and the operator both need to know
	// which rule failed, and "it just stopped matching" is the failure mode this ticket exists to
	// eliminate.
	FailedRuleIDs []string
}

// RuleEngine evaluates categorization rules against transactions.
type RuleEngine struct {
	registry *ConditionRegistry
}

// NewRuleEngine creates a new rule engine with the default condition registry.
func NewRuleEngine() *RuleEngine {
	return &RuleEngine{
		registry: NewConditionRegistry(),
	}
}

// NewRuleEngineWithRegistry creates a rule engine with a custom registry.
func NewRuleEngineWithRegistry(registry *ConditionRegistry) *RuleEngine {
	return &RuleEngine{
		registry: registry,
	}
}

// EvaluateRules evaluates all rules against a transaction and returns the result.
//
// Category assignment follows "first-match-wins" semantics:
// - Rules are sorted by priority (highest first)
// - First matching rule with a category action sets the category outcome
//
// Tag assignment follows "all-match" semantics:
// - All matching rules contribute their tags
// - Tag behavior (add/replace) is respected
func (e *RuleEngine) EvaluateRules(tx *domain.Transaction, rules []*domain.CategorizationRule) (*RuleResult, error) {
	result := &RuleResult{
		Tags:           make([]string, 0),
		TagRuleIDs:     make([]string, 0),
		TagSources:     make([]domain.TagSource, 0),
		MatchedRuleIDs: make([]string, 0),
		FailedRuleIDs:  make([]string, 0),
	}

	if len(rules) == 0 {
		return result, nil
	}

	// Sort rules by priority (highest first).
	// Tie-breaking (Codex review #957): equal-priority rules are ordered by
	// CreatedAt descending (newer rule wins) then by ID for exact-timestamp ties,
	// making the result deterministic regardless of the caller's slice order.
	sortedRules := make([]*domain.CategorizationRule, len(rules))
	copy(sortedRules, rules)
	sort.Slice(sortedRules, func(i, j int) bool {
		a, b := sortedRules[i], sortedRules[j]
		if a.Priority != b.Priority {
			return a.Priority > b.Priority
		}
		if !a.CreatedAt.Equal(b.CreatedAt) {
			return a.CreatedAt.After(b.CreatedAt)
		}
		return a.ID > b.ID
	})

	// Track tags to avoid duplicates
	tagSet := make(map[string]bool)

	for _, rule := range sortedRules {
		// Skip disabled rules
		if !rule.Enabled {
			continue
		}

		// Check if rule matches. An error is local to THIS rule (#1625 A8): skip it, record it,
		// and carry on with the rest. Returning here made one broken condition disable every other
		// rule for the transaction, with nothing anywhere saying so.
		matches, err := e.RuleMatches(tx, rule)
		if err != nil {
			result.FailedRuleIDs = append(result.FailedRuleIDs, rule.ID)
			continue
		}

		if !matches {
			continue
		}

		// Record matched rule
		result.MatchedRuleIDs = append(result.MatchedRuleIDs, rule.ID)

		// Handle category action (first-match-wins)
		if result.TargetKind == nil && rule.HasCategoryAction() {
			result.TargetKind = rule.TargetKind
			result.TargetPFCPrimary = rule.TargetPFCPrimary
			result.TargetPFCDetailed = rule.TargetPFCDetailed
			result.TargetCustomCategoryID = rule.TargetCustomCategoryID
			result.CategoryRuleID = rule.ID
		}

		// Handle tags (all-match)
		if rule.HasTagAction() {
			// Handle tag behavior
			if rule.TagBehavior == "replace" {
				// Clear existing tags
				result.Tags = make([]string, 0)
				result.TagRuleIDs = make([]string, 0)
				result.TagSources = make([]domain.TagSource, 0)
				tagSet = make(map[string]bool)
			}

			// Add new tags (avoid duplicates)
			for _, tag := range rule.TargetTags {
				if !tagSet[tag] {
					tagSet[tag] = true
					result.Tags = append(result.Tags, tag)
					ruleID := rule.ID
					result.TagSources = append(result.TagSources, domain.TagSource{
						Tag:    tag,
						Source: "rule",
						RuleID: &ruleID,
					})
				}
			}

			// Record which rule contributed tags
			if len(rule.TargetTags) > 0 {
				result.TagRuleIDs = append(result.TagRuleIDs, rule.ID)
			}
		}
	}

	return result, nil
}

// enforceZeroConditionQuarantine gates Task 8's only enforced quarantine reason.
//
// Default ON. Set RULE_QUARANTINE_ENFORCE_ZERO_CONDITIONS=false to restore the pre-#1625
// match-everything behaviour without a deploy of new code — the same kill-switch convention as
// CACHE_ENABLED. Package-level rather than threaded through config because the engine is
// constructed in several places and a flag nobody can find is not a usable switch.
var enforceZeroConditionQuarantine = os.Getenv("RULE_QUARANTINE_ENFORCE_ZERO_CONDITIONS") != "false"

// RuleMatches checks if a single rule matches a transaction.
func (e *RuleEngine) RuleMatches(tx *domain.Transaction, rule *domain.CategorizationRule) (bool, error) {
	if !rule.Enabled {
		return false, nil
	}

	if len(rule.Conditions) == 0 {
		// #1625 Task 8: a rule with no conditions matches NOTHING.
		//
		// This returned true, commented "No conditions means always match (edge case)". It is not
		// an edge case: such a rule applies its target to every transaction the user has, which is
		// a ledger-wide recategorisation nobody asked for. It is the one quarantine reason where
		// leaving the rule alone is the RISKY choice, so it is the one that is enforced.
		//
		// Safe without a production inventory because the row is unreachable from the product:
		// CategorizationRule.Validate has rejected empty conditions since dedac0aa (#346), the
		// same commit that created the file, and no migration inserts rules. Reaching this branch
		// requires a direct database edit — or the table's own `conditions` DEFAULT '[]' via a
		// non-service insert, which is precisely what this guards.
		//
		// The rule is NOT hidden from reads: it still lists, and the read model marks it
		// quarantined with a reason (Task 7). Filtering it out of GetEnabledByUserID instead would
		// make it vanish from the UI, which is the silent failure this ticket exists to remove.
		//
		// Enforcement is reason-scoped: the other three quarantine reasons are surfaced only.
		if enforceZeroConditionQuarantine {
			return false, nil
		}
		return true, nil
	}

	switch rule.ConditionLogic {
	case "AND":
		return e.evaluateAND(tx, rule.Conditions)
	case "OR":
		return e.evaluateOR(tx, rule.Conditions)
	default:
		// Default to AND logic
		return e.evaluateAND(tx, rule.Conditions)
	}
}

// evaluateAND returns true if ALL conditions match.
func (e *RuleEngine) evaluateAND(tx *domain.Transaction, conditions []domain.RuleCondition) (bool, error) {
	for _, condition := range conditions {
		matches, err := e.registry.Evaluate(tx, condition)
		if err != nil {
			return false, err
		}
		if !matches {
			return false, nil
		}
	}
	return true, nil
}

// evaluateOR returns true if ANY condition matches.
func (e *RuleEngine) evaluateOR(tx *domain.Transaction, conditions []domain.RuleCondition) (bool, error) {
	for _, condition := range conditions {
		matches, err := e.registry.Evaluate(tx, condition)
		if err != nil {
			return false, err
		}
		if matches {
			return true, nil
		}
	}
	return false, nil
}
