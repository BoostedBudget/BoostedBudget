package rules

import (
	"testing"

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
			ConditionLogic: "AND",
			TargetCategory: []string{"Food & Drink", "Coffee"},
			TagBehavior:    "add",
			Source:         "manual",
		},
		{
			ID:       "rule-2",
			Name:     "High Priority Coffee",
			Enabled:  true,
			Priority: 10, // Higher priority - should win
			Conditions: []domain.RuleCondition{
				{Type: domain.ConditionMerchantContains, Value: "starbucks"},
			},
			ConditionLogic: "AND",
			TargetCategory: []string{"Expenses", "Premium Coffee"},
			TagBehavior:    "add",
			Source:         "manual",
		},
	}

	tx := newTestTransaction("STARBUCKS COFFEE #123", -5.50, "in store", nil)

	result, err := engine.EvaluateRules(tx, rules)
	if err != nil {
		t.Fatalf("EvaluateRules() error = %v", err)
	}

	// Higher priority rule should win for category
	if len(result.Category) != 2 || result.Category[0] != "Expenses" {
		t.Errorf("Expected category [Expenses, Premium Coffee], got %v", result.Category)
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
			ConditionLogic: "AND",
			TargetCategory: []string{"Premium Coffee"},
			TagBehavior:    "add",
			Source:         "manual",
		},
	}

	// Small purchase - only first condition matches
	tx := newTestTransaction("STARBUCKS COFFEE #123", -5.50, "in store", nil)

	result, err := engine.EvaluateRules(tx, rules)
	if err != nil {
		t.Fatalf("EvaluateRules() error = %v", err)
	}

	// Rule should NOT match (AND requires all conditions)
	if result.Category != nil {
		t.Errorf("Expected no category match, got %v", result.Category)
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
			ConditionLogic: "OR",
			TargetCategory: []string{"Beverages"},
			TagBehavior:    "add",
			Source:         "manual",
		},
	}

	tx := newTestTransaction("STARBUCKS COFFEE #123", -5.50, "in store", nil)

	result, err := engine.EvaluateRules(tx, rules)
	if err != nil {
		t.Fatalf("EvaluateRules() error = %v", err)
	}

	// Rule should match (OR only requires one condition)
	if len(result.Category) != 1 || result.Category[0] != "Beverages" {
		t.Errorf("Expected category [Beverages], got %v", result.Category)
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
			ConditionLogic: "AND",
			TargetCategory: []string{"Should Not Match"},
			TagBehavior:    "add",
			Source:         "manual",
		},
	}

	tx := newTestTransaction("STARBUCKS COFFEE #123", -5.50, "in store", nil)

	result, err := engine.EvaluateRules(tx, rules)
	if err != nil {
		t.Fatalf("EvaluateRules() error = %v", err)
	}

	// Disabled rule should not match
	if result.Category != nil {
		t.Errorf("Expected no category match for disabled rule, got %v", result.Category)
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
			ConditionLogic: "AND",
			TargetCategory: []string{"Food"},
			TagBehavior:    "add",
			Source:         "manual",
		},
	}

	tx := newTestTransaction("STARBUCKS COFFEE #123", -5.50, "in store", nil)

	result, err := engine.EvaluateRules(tx, rules)
	if err != nil {
		t.Fatalf("EvaluateRules() error = %v", err)
	}

	// No rules should match
	if result.Category != nil {
		t.Errorf("Expected no category match, got %v", result.Category)
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
			ConditionLogic: "AND",
			TargetCategory: []string{"Food & Drink", "Coffee"},
			TargetTags:     []string{"coffee", "recurring"},
			TagBehavior:    "add",
			Source:         "manual",
		},
	}

	tx := newTestTransaction("STARBUCKS COFFEE #123", -5.50, "in store", nil)

	result, err := engine.EvaluateRules(tx, rules)
	if err != nil {
		t.Fatalf("EvaluateRules() error = %v", err)
	}

	// Should have both category and tags
	if len(result.Category) != 2 {
		t.Errorf("Expected 2-level category, got %v", result.Category)
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

	if result.Category != nil || len(result.Tags) != 0 {
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
