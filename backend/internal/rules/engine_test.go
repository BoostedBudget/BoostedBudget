package rules

import (
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// ============================================================================
// Rule Engine Tests
// ============================================================================

func TestRuleEngine_EvaluateRules_FirstMatchWins_Category(t *testing.T) {
	engine := NewRuleEngine()

	// Create rules with different priorities
	rules := []*domain.CategorizationRule{
		{
			ID:       "rule-1",
			Name:     "Low Priority Coffee",
			Enabled:  true,
			Priority: 1,
			Conditions: []domain.RuleCondition{
				{Type: domain.ConditionMerchantContains, Value: "starbucks"},
			},
			ConditionLogic:  "AND",
			TargetKind:      strPtr("pfc"),
			TargetPFCPrimary: strPtr("FOOD_AND_DRINK"),
			TagBehavior:     "add",
			Source:          "manual",
		},
		{
			ID:       "rule-2",
			Name:     "High Priority Coffee",
			Enabled:  true,
			Priority: 10, // Higher priority - should win
			Conditions: []domain.RuleCondition{
				{Type: domain.ConditionMerchantContains, Value: "starbucks"},
			},
			ConditionLogic:  "AND",
			TargetKind:      strPtr("pfc"),
			TargetPFCPrimary: strPtr("GENERAL_MERCHANDISE"),
			TagBehavior:     "add",
			Source:          "manual",
		},
	}

	tx := newTestTransaction("STARBUCKS COFFEE #123", -5.50, "in store", nil)

	result, err := engine.EvaluateRules(tx, rules)
	if err != nil {
		t.Fatalf("EvaluateRules() error = %v", err)
	}

	// Higher priority rule should win for category
	if result.TargetKind == nil || *result.TargetKind != "pfc" {
		t.Errorf("Expected TargetKind = pfc, got %v", result.TargetKind)
	}
	if result.TargetPFCPrimary == nil || *result.TargetPFCPrimary != "GENERAL_MERCHANDISE" {
		t.Errorf("Expected TargetPFCPrimary = GENERAL_MERCHANDISE, got %v", result.TargetPFCPrimary)
	}

	if result.CategoryRuleID != "rule-2" {
		t.Errorf("Expected CategoryRuleID = rule-2, got %s", result.CategoryRuleID)
	}
}

func TestRuleEngine_EvaluateRules_AllMatch_Tags(t *testing.T) {
	engine := NewRuleEngine()

	// Create rules - both should match and contribute tags
	rules := []*domain.CategorizationRule{
		{
			ID:       "rule-1",
			Name:     "Tag Coffee",
			Enabled:  true,
			Priority: 10,
			Conditions: []domain.RuleCondition{
				{Type: domain.ConditionMerchantContains, Value: "starbucks"},
			},
			ConditionLogic: "AND",
			TargetTags:     []string{"coffee", "beverage"},
			TagBehavior:    "add",
			Source:         "manual",
		},
		{
			ID:       "rule-2",
			Name:     "Tag Small Purchase",
			Enabled:  true,
			Priority: 5,
			Conditions: []domain.RuleCondition{
				{Type: domain.ConditionAmountLessThan, Value: float64(10)},
			},
			ConditionLogic: "AND",
			TargetTags:     []string{"small-purchase"},
			TagBehavior:    "add",
			Source:         "manual",
		},
	}

	tx := newTestTransaction("STARBUCKS COFFEE #123", -5.50, "in store", nil)

	result, err := engine.EvaluateRules(tx, rules)
	if err != nil {
		t.Fatalf("EvaluateRules() error = %v", err)
	}

	// Both rules should contribute tags (all-match)
	expectedTags := map[string]bool{"coffee": true, "beverage": true, "small-purchase": true}
	if len(result.Tags) != 3 {
		t.Errorf("Expected 3 tags, got %d: %v", len(result.Tags), result.Tags)
	}
	for _, tag := range result.Tags {
		if !expectedTags[tag] {
			t.Errorf("Unexpected tag: %s", tag)
		}
	}

	// Should have two matched tag rules
	if len(result.TagRuleIDs) != 2 {
		t.Errorf("Expected 2 tag rule IDs, got %d", len(result.TagRuleIDs))
	}
}

func TestRuleEngine_EvaluateRules_ConditionLogic_AND(t *testing.T) {
	engine := NewRuleEngine()

	// Rule requires both conditions to match
	rules := []*domain.CategorizationRule{
		{
			ID:       "rule-1",
			Name:     "Expensive Coffee",
			Enabled:  true,
			Priority: 10,
			Conditions: []domain.RuleCondition{
				{Type: domain.ConditionMerchantContains, Value: "starbucks"},
				{Type: domain.ConditionAmountGreaterThan, Value: float64(20)},
			},
			ConditionLogic:  "AND",
			TargetKind:      strPtr("pfc"),
			TargetPFCPrimary: strPtr("FOOD_AND_DRINK"),
			TagBehavior:     "add",
			Source:          "manual",
		},
	}

	// Small purchase - only first condition matches
	tx := newTestTransaction("STARBUCKS COFFEE #123", -5.50, "in store", nil)

	result, err := engine.EvaluateRules(tx, rules)
	if err != nil {
		t.Fatalf("EvaluateRules() error = %v", err)
	}

	// Rule should NOT match (AND requires all conditions)
	if result.TargetKind != nil {
		t.Errorf("Expected no category match, got TargetKind=%v", result.TargetKind)
	}
}

func TestRuleEngine_EvaluateRules_ConditionLogic_OR(t *testing.T) {
	engine := NewRuleEngine()

	// Rule requires any condition to match
	rules := []*domain.CategorizationRule{
		{
			ID:       "rule-1",
			Name:     "Coffee or Tea",
			Enabled:  true,
			Priority: 10,
			Conditions: []domain.RuleCondition{
				{Type: domain.ConditionMerchantContains, Value: "starbucks"},
				{Type: domain.ConditionMerchantContains, Value: "tea"},
			},
			ConditionLogic:  "OR",
			TargetKind:      strPtr("pfc"),
			TargetPFCPrimary: strPtr("FOOD_AND_DRINK"),
			TagBehavior:     "add",
			Source:          "manual",
		},
	}

	tx := newTestTransaction("STARBUCKS COFFEE #123", -5.50, "in store", nil)

	result, err := engine.EvaluateRules(tx, rules)
	if err != nil {
		t.Fatalf("EvaluateRules() error = %v", err)
	}

	// Rule should match (OR only requires one condition)
	if result.TargetKind == nil || *result.TargetKind != "pfc" {
		t.Errorf("Expected TargetKind = pfc, got %v", result.TargetKind)
	}
	if result.TargetPFCPrimary == nil || *result.TargetPFCPrimary != "FOOD_AND_DRINK" {
		t.Errorf("Expected TargetPFCPrimary = FOOD_AND_DRINK, got %v", result.TargetPFCPrimary)
	}
}

func TestRuleEngine_EvaluateRules_DisabledRulesSkipped(t *testing.T) {
	engine := NewRuleEngine()

	rules := []*domain.CategorizationRule{
		{
			ID:       "rule-1",
			Name:     "Disabled Rule",
			Enabled:  false, // Disabled
			Priority: 100,
			Conditions: []domain.RuleCondition{
				{Type: domain.ConditionMerchantContains, Value: "starbucks"},
			},
			ConditionLogic:  "AND",
			TargetKind:      strPtr("pfc"),
			TargetPFCPrimary: strPtr("FOOD_AND_DRINK"),
			TagBehavior:     "add",
			Source:          "manual",
		},
	}

	tx := newTestTransaction("STARBUCKS COFFEE #123", -5.50, "in store", nil)

	result, err := engine.EvaluateRules(tx, rules)
	if err != nil {
		t.Fatalf("EvaluateRules() error = %v", err)
	}

	// Disabled rule should not match
	if result.TargetKind != nil {
		t.Errorf("Expected no category match for disabled rule, got TargetKind=%v", result.TargetKind)
	}
}

func TestRuleEngine_EvaluateRules_NoMatchingRules(t *testing.T) {
	engine := NewRuleEngine()

	rules := []*domain.CategorizationRule{
		{
			ID:       "rule-1",
			Name:     "Pizza Rule",
			Enabled:  true,
			Priority: 10,
			Conditions: []domain.RuleCondition{
				{Type: domain.ConditionMerchantContains, Value: "pizza"},
			},
			ConditionLogic:  "AND",
			TargetKind:      strPtr("pfc"),
			TargetPFCPrimary: strPtr("FOOD_AND_DRINK"),
			TagBehavior:     "add",
			Source:          "manual",
		},
	}

	tx := newTestTransaction("STARBUCKS COFFEE #123", -5.50, "in store", nil)

	result, err := engine.EvaluateRules(tx, rules)
	if err != nil {
		t.Fatalf("EvaluateRules() error = %v", err)
	}

	// No rules should match
	if result.TargetKind != nil {
		t.Errorf("Expected no category match, got TargetKind=%v", result.TargetKind)
	}
	if len(result.Tags) != 0 {
		t.Errorf("Expected no tags, got %v", result.Tags)
	}
	if len(result.MatchedRuleIDs) != 0 {
		t.Errorf("Expected no matched rules, got %v", result.MatchedRuleIDs)
	}
}

func TestRuleEngine_EvaluateRules_CategoryAndTags(t *testing.T) {
	engine := NewRuleEngine()

	// Single rule that sets both category and tags
	rules := []*domain.CategorizationRule{
		{
			ID:       "rule-1",
			Name:     "Complete Coffee Rule",
			Enabled:  true,
			Priority: 10,
			Conditions: []domain.RuleCondition{
				{Type: domain.ConditionMerchantContains, Value: "starbucks"},
			},
			ConditionLogic:  "AND",
			TargetKind:      strPtr("pfc"),
			TargetPFCPrimary: strPtr("FOOD_AND_DRINK"),
			TargetTags:      []string{"coffee", "recurring"},
			TagBehavior:     "add",
			Source:          "manual",
		},
	}

	tx := newTestTransaction("STARBUCKS COFFEE #123", -5.50, "in store", nil)

	result, err := engine.EvaluateRules(tx, rules)
	if err != nil {
		t.Fatalf("EvaluateRules() error = %v", err)
	}

	// Should have both category and tags
	if result.TargetKind == nil || *result.TargetKind != "pfc" {
		t.Errorf("Expected TargetKind = pfc, got %v", result.TargetKind)
	}
	if len(result.Tags) != 2 {
		t.Errorf("Expected 2 tags, got %v", result.Tags)
	}
}

func TestRuleEngine_EvaluateRules_TagBehavior_Replace(t *testing.T) {
	engine := NewRuleEngine()

	// First rule adds tags, second rule replaces
	rules := []*domain.CategorizationRule{
		{
			ID:       "rule-1",
			Name:     "Add Tags",
			Enabled:  true,
			Priority: 10,
			Conditions: []domain.RuleCondition{
				{Type: domain.ConditionMerchantContains, Value: "starbucks"},
			},
			ConditionLogic: "AND",
			TargetTags:     []string{"coffee", "beverage"},
			TagBehavior:    "add",
			Source:         "manual",
		},
		{
			ID:       "rule-2",
			Name:     "Replace Tags",
			Enabled:  true,
			Priority: 5, // Lower priority, evaluated later
			Conditions: []domain.RuleCondition{
				{Type: domain.ConditionAmountLessThan, Value: float64(10)},
			},
			ConditionLogic: "AND",
			TargetTags:     []string{"cheap"},
			TagBehavior:    "replace",
			Source:         "manual",
		},
	}

	tx := newTestTransaction("STARBUCKS COFFEE #123", -5.50, "in store", nil)

	result, err := engine.EvaluateRules(tx, rules)
	if err != nil {
		t.Fatalf("EvaluateRules() error = %v", err)
	}

	// Replace behavior should clear previous tags
	if len(result.Tags) != 1 || result.Tags[0] != "cheap" {
		t.Errorf("Expected tags [cheap] after replace, got %v", result.Tags)
	}
}

func TestRuleEngine_EvaluateRules_EmptyRules(t *testing.T) {
	engine := NewRuleEngine()

	tx := newTestTransaction("STARBUCKS COFFEE #123", -5.50, "in store", nil)

	result, err := engine.EvaluateRules(tx, nil)
	if err != nil {
		t.Fatalf("EvaluateRules() error = %v", err)
	}

	if result.TargetKind != nil || len(result.Tags) != 0 {
		t.Errorf("Expected empty result for empty rules")
	}
}

// ============================================================================
// RuleMatch Tests
// ============================================================================

func TestRuleEngine_RuleMatches(t *testing.T) {
	engine := NewRuleEngine()

	tests := []struct {
		name string
		rule *domain.CategorizationRule
		tx   *domain.Transaction
		want bool
	}{
		{
			name: "single condition match",
			rule: &domain.CategorizationRule{
				Enabled: true,
				Conditions: []domain.RuleCondition{
					{Type: domain.ConditionMerchantContains, Value: "starbucks"},
				},
				ConditionLogic: "AND",
			},
			tx:   newTestTransaction("STARBUCKS COFFEE", -5.50, "in store", nil),
			want: true,
		},
		{
			name: "single condition no match",
			rule: &domain.CategorizationRule{
				Enabled: true,
				Conditions: []domain.RuleCondition{
					{Type: domain.ConditionMerchantContains, Value: "dunkin"},
				},
				ConditionLogic: "AND",
			},
			tx:   newTestTransaction("STARBUCKS COFFEE", -5.50, "in store", nil),
			want: false,
		},
		{
			name: "AND logic - all match",
			rule: &domain.CategorizationRule{
				Enabled: true,
				Conditions: []domain.RuleCondition{
					{Type: domain.ConditionMerchantContains, Value: "starbucks"},
					{Type: domain.ConditionPaymentChannel, Value: "in store"},
				},
				ConditionLogic: "AND",
			},
			tx:   newTestTransaction("STARBUCKS COFFEE", -5.50, "in store", nil),
			want: true,
		},
		{
			name: "AND logic - partial match",
			rule: &domain.CategorizationRule{
				Enabled: true,
				Conditions: []domain.RuleCondition{
					{Type: domain.ConditionMerchantContains, Value: "starbucks"},
					{Type: domain.ConditionPaymentChannel, Value: "online"},
				},
				ConditionLogic: "AND",
			},
			tx:   newTestTransaction("STARBUCKS COFFEE", -5.50, "in store", nil),
			want: false,
		},
		{
			name: "OR logic - one match",
			rule: &domain.CategorizationRule{
				Enabled: true,
				Conditions: []domain.RuleCondition{
					{Type: domain.ConditionMerchantContains, Value: "starbucks"},
					{Type: domain.ConditionMerchantContains, Value: "dunkin"},
				},
				ConditionLogic: "OR",
			},
			tx:   newTestTransaction("STARBUCKS COFFEE", -5.50, "in store", nil),
			want: true,
		},
		{
			name: "OR logic - no match",
			rule: &domain.CategorizationRule{
				Enabled: true,
				Conditions: []domain.RuleCondition{
					{Type: domain.ConditionMerchantContains, Value: "pizza"},
					{Type: domain.ConditionMerchantContains, Value: "burger"},
				},
				ConditionLogic: "OR",
			},
			tx:   newTestTransaction("STARBUCKS COFFEE", -5.50, "in store", nil),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := engine.RuleMatches(tt.tx, tt.rule)
			if err != nil {
				t.Errorf("RuleMatches() error = %v", err)
				return
			}
			if got != tt.want {
				t.Errorf("RuleMatches() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestRuleEngine_EvaluateRules_ClearCategory_Uncategorized tests that rules with
// TargetKind="clear" correctly signal a clear-category action.
// Issue #404: TargetKind="clear" represents "clear category" / "uncategorized".
func TestRuleEngine_EvaluateRules_ClearCategory_Uncategorized(t *testing.T) {
	engine := NewRuleEngine()

	// Rule with TargetKind="clear" = "clear to uncategorized"
	rules := []*domain.CategorizationRule{
		{
			ID:       "rule-clear",
			Name:     "Clear Refund Categories",
			Enabled:  true,
			Priority: 10,
			Conditions: []domain.RuleCondition{
				{Type: domain.ConditionMerchantContains, Value: "refund"},
			},
			ConditionLogic: "AND",
			TargetKind:     strPtr("clear"), // clear to uncategorized
			TagBehavior:    "add",
			Source:         "manual",
		},
	}

	tx := newTestTransaction("AMAZON REFUND #123", 50.00, "online", nil)

	result, err := engine.EvaluateRules(tx, rules)
	if err != nil {
		t.Fatalf("EvaluateRules() error = %v", err)
	}

	// Rule should match
	if len(result.MatchedRuleIDs) != 1 {
		t.Errorf("Expected 1 matched rule, got %d", len(result.MatchedRuleIDs))
	}

	// TargetKind should be set to "clear"
	if result.TargetKind == nil || *result.TargetKind != "clear" {
		t.Errorf("Expected TargetKind = clear, got %v", result.TargetKind)
	}

	// CategoryRuleID should be set
	if result.CategoryRuleID != "rule-clear" {
		t.Errorf("Expected CategoryRuleID = rule-clear, got %s", result.CategoryRuleID)
	}
}

// TestRuleEngine_EvaluateRules_ClearCategory_FirstMatchWins tests that the first
// matching "clear category" rule wins over later assign category rules.
func TestRuleEngine_EvaluateRules_ClearCategory_FirstMatchWins(t *testing.T) {
	engine := NewRuleEngine()

	rules := []*domain.CategorizationRule{
		{
			ID:       "rule-clear",
			Name:     "Clear Category (high priority)",
			Enabled:  true,
			Priority: 20, // Higher priority - should win
			Conditions: []domain.RuleCondition{
				{Type: domain.ConditionMerchantContains, Value: "amazon"},
			},
			ConditionLogic: "AND",
			TargetKind:     strPtr("clear"), // Clear to uncategorized
			TagBehavior:    "add",
			Source:         "manual",
		},
		{
			ID:       "rule-assign",
			Name:     "Assign Shopping Category (low priority)",
			Enabled:  true,
			Priority: 10, // Lower priority
			Conditions: []domain.RuleCondition{
				{Type: domain.ConditionMerchantContains, Value: "amazon"},
			},
			ConditionLogic:  "AND",
			TargetKind:      strPtr("pfc"),
			TargetPFCPrimary: strPtr("GENERAL_MERCHANDISE"),
			TagBehavior:     "add",
			Source:          "manual",
		},
	}

	tx := newTestTransaction("AMAZON.COM", -25.00, "online", nil)

	result, err := engine.EvaluateRules(tx, rules)
	if err != nil {
		t.Fatalf("EvaluateRules() error = %v", err)
	}

	// Higher priority "clear" rule should win
	if result.TargetKind == nil || *result.TargetKind != "clear" {
		t.Errorf("Expected TargetKind = clear from high-priority rule, got %v", result.TargetKind)
	}

	if result.CategoryRuleID != "rule-clear" {
		t.Errorf("Expected CategoryRuleID = rule-clear, got %s", result.CategoryRuleID)
	}
}

// TestRuleEngine_EvaluateRules_EqualPriority_DeterministicTieBreaker verifies that
// when two rules share the same priority the engine always picks the same winner,
// regardless of which order the caller passes the slice. Without a tie-breaker,
// sort.Slice is unstable and the winner is nondeterministic across Go versions /
// slice orderings — a P0 bug (Codex review #957).
//
// Tie-breaking semantics: among equal-priority rules the one with the later
// CreatedAt wins (more-recently created rule is treated as more specific). The ID
// string is a final tiebreaker for exact-timestamp ties.
func TestRuleEngine_EvaluateRules_EqualPriority_DeterministicTieBreaker(t *testing.T) {
	engine := NewRuleEngine()

	older := &domain.CategorizationRule{
		ID:       "rule-older",
		Name:     "Older Rule",
		Enabled:  true,
		Priority: 5,
		Conditions: []domain.RuleCondition{
			{Type: domain.ConditionMerchantContains, Value: "starbucks"},
		},
		ConditionLogic:  "AND",
		TargetKind:      strPtr("pfc"),
		TargetPFCPrimary: strPtr("FOOD_AND_DRINK"),
		TagBehavior:     "add",
		Source:          "manual",
		CreatedAt:       time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	newer := &domain.CategorizationRule{
		ID:       "rule-newer",
		Name:     "Newer Rule (same priority)",
		Enabled:  true,
		Priority: 5, // same priority
		Conditions: []domain.RuleCondition{
			{Type: domain.ConditionMerchantContains, Value: "starbucks"},
		},
		ConditionLogic:  "AND",
		TargetKind:      strPtr("pfc"),
		TargetPFCPrimary: strPtr("GENERAL_MERCHANDISE"),
		TagBehavior:     "add",
		Source:          "manual",
		CreatedAt:       time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC), // later
	}

	tx := newTestTransaction("STARBUCKS COFFEE", -5.50, "in store", nil)

	// Pass in [older, newer] order
	result1, err := engine.EvaluateRules(tx, []*domain.CategorizationRule{older, newer})
	if err != nil {
		t.Fatalf("EvaluateRules() order1 error = %v", err)
	}

	// Pass in [newer, older] order — must produce the same winner
	result2, err := engine.EvaluateRules(tx, []*domain.CategorizationRule{newer, older})
	if err != nil {
		t.Fatalf("EvaluateRules() order2 error = %v", err)
	}

	// The newer rule must always win (later CreatedAt = tiebreaker)
	if result1.CategoryRuleID != "rule-newer" {
		t.Errorf("order1: expected rule-newer to win, got %q", result1.CategoryRuleID)
	}
	if result2.CategoryRuleID != "rule-newer" {
		t.Errorf("order2: expected rule-newer to win, got %q", result2.CategoryRuleID)
	}
	// Both orderings must agree on the winner
	if result1.CategoryRuleID != result2.CategoryRuleID {
		t.Errorf("nondeterministic: order1 winner=%q, order2 winner=%q", result1.CategoryRuleID, result2.CategoryRuleID)
	}
}

// A8 (#1625): one broken rule must not stop every other rule for that transaction.
//
// EvaluateRules returned on the FIRST rule's error, so a single un-evaluatable condition — the
// kind a pre-#1625 row can legitimately hold — silently suppressed every other rule the user had.
// The rule that failed is the only one that should be affected.
//
// The broken rule is deliberately sorted FIRST (higher priority) so the abort would swallow the
// rule after it; with the rules the other way round the old code passes and the test proves
// nothing.
func TestRuleEngine_EvaluateRules_OneBrokenRuleDoesNotSuppressTheRest(t *testing.T) {
	engine := NewRuleEngine()

	rules := []*domain.CategorizationRule{
		{
			ID:       "rule-broken",
			Name:     "Un-evaluatable",
			Enabled:  true,
			Priority: 100,
			Conditions: []domain.RuleCondition{
				{Type: domain.ConditionAmountGreaterThan, Value: "not-a-number"},
			},
			ConditionLogic:   "AND",
			TargetKind:       strPtr("pfc"),
			TargetPFCPrimary: strPtr("RENT_AND_UTILITIES"),
			TagBehavior:      "add",
			Source:           "manual",
		},
		{
			ID:       "rule-good",
			Name:     "Coffee",
			Enabled:  true,
			Priority: 1,
			Conditions: []domain.RuleCondition{
				{Type: domain.ConditionMerchantContains, Value: "starbucks"},
			},
			ConditionLogic:   "AND",
			TargetKind:       strPtr("pfc"),
			TargetPFCPrimary: strPtr("FOOD_AND_DRINK"),
			TagBehavior:      "add",
			Source:           "manual",
		},
	}

	tx := newTestTransaction("STARBUCKS COFFEE #123", -5.50, "in store", nil)

	result, err := engine.EvaluateRules(tx, rules)
	if err != nil {
		t.Fatalf("a single broken rule aborted the whole evaluation: %v", err)
	}
	if result.CategoryRuleID != "rule-good" {
		t.Errorf("the rule AFTER the broken one never ran: CategoryRuleID = %q, want \"rule-good\"",
			result.CategoryRuleID)
	}

	// ⚠ #1625 Task 2 moved WHERE this rule is reported. "not-a-number" for an amount is an
	// uncoercible value, so RuleMatches now refuses the rule before any evaluator sees it: it
	// returns (false, nil) rather than an error, and the reporting channel is quarantine — the
	// metric, the log line, and `quarantined` on the read model, all of which name the rule.
	// FailedRuleIDs is therefore empty here, and that is the enforcement working.
	if len(result.FailedRuleIDs) != 0 {
		t.Errorf("an ENFORCED quarantine reason must stop the rule quietly and be reported through "+
			"quarantine, not as an evaluation failure: %v", result.FailedRuleIDs)
	}

	// The original property — an un-evaluatable rule must not suppress the rules after it, and
	// must be REPORTED rather than swallowed — still holds on the path that can still produce an
	// evaluator error: enforcement disabled.
	original := enforceQuarantine
	enforceQuarantine = false
	t.Cleanup(func() { enforceQuarantine = original })

	result, err = engine.EvaluateRules(tx, rules)
	if err != nil {
		t.Fatalf("a single broken rule aborted the whole evaluation: %v", err)
	}
	if result.CategoryRuleID != "rule-good" {
		t.Errorf("with enforcement off, the rule AFTER the broken one never ran: CategoryRuleID = %q",
			result.CategoryRuleID)
	}
	if len(result.FailedRuleIDs) != 1 || result.FailedRuleIDs[0] != "rule-broken" {
		t.Errorf("the broken rule was not reported as failed: %v", result.FailedRuleIDs)
	}
}
