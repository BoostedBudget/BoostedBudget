package rules

import (
	"sort"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// RuleResult contains the outcome of evaluating rules against a transaction.
type RuleResult struct {
	// Category is set by the first matching rule with TargetCategory (first-match-wins).
	Category []string
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
// - First matching rule with TargetCategory sets the category
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
	}

	if len(rules) == 0 {
		return result, nil
	}

	// Sort rules by priority (highest first)
	sortedRules := make([]*domain.CategorizationRule, len(rules))
	copy(sortedRules, rules)
	sort.Slice(sortedRules, func(i, j int) bool {
		return sortedRules[i].Priority > sortedRules[j].Priority
	})

	// Track tags to avoid duplicates
	tagSet := make(map[string]bool)

	for _, rule := range sortedRules {
		// Skip disabled rules
		if !rule.Enabled {
			continue
		}

		// Check if rule matches
		matches, err := e.RuleMatches(tx, rule)
		if err != nil {
			return nil, err
		}

		if !matches {
			continue
		}

		// Record matched rule
		result.MatchedRuleIDs = append(result.MatchedRuleIDs, rule.ID)

		// Handle category (first-match-wins)
		if result.Category == nil && rule.HasCategoryAction() {
			result.Category = rule.TargetCategory
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

// RuleMatches checks if a single rule matches a transaction.
func (e *RuleEngine) RuleMatches(tx *domain.Transaction, rule *domain.CategorizationRule) (bool, error) {
	if !rule.Enabled {
		return false, nil
	}

	if len(rule.Conditions) == 0 {
		// No conditions means always match (edge case)
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
