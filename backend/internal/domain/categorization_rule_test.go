package domain

import (
	"encoding/json"
	"testing"
	"time"
)

// TestCategorizationRule_Validate tests that CategorizationRule validates required fields
func TestCategorizationRule_Validate(t *testing.T) {
	tests := []struct {
		name    string
		rule    CategorizationRule
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid rule with category action",
			rule: CategorizationRule{
				UserID:         "user-123",
				Name:           "Coffee Shops",
				Conditions:     []RuleCondition{{Type: ConditionMerchantContains, Value: "starbucks"}},
				ConditionLogic: "AND",
				TargetCategory: []string{"Food & Drink", "Coffee Shops"},
				TagBehavior:    "add",
				Source:         "manual",
			},
			wantErr: false,
		},
		{
			name: "valid rule with tag action only",
			rule: CategorizationRule{
				UserID:         "user-123",
				Name:           "Large Purchases",
				Conditions:     []RuleCondition{{Type: ConditionAmountGreaterThan, Value: float64(500)}},
				ConditionLogic: "AND",
				TargetTags:     []string{"major-expense", "needs-review"},
				TagBehavior:    "add",
				Source:         "manual",
			},
			wantErr: false,
		},
		{
			name: "valid rule with both category and tags",
			rule: CategorizationRule{
				UserID:         "user-123",
				Name:           "Rideshare",
				Conditions:     []RuleCondition{{Type: ConditionMerchantContains, Value: "uber"}},
				ConditionLogic: "AND",
				TargetCategory: []string{"Transportation", "Rideshare"},
				TargetTags:     []string{"transportation"},
				TagBehavior:    "add",
				Source:         "manual",
			},
			wantErr: false,
		},
		{
			name: "missing user_id",
			rule: CategorizationRule{
				Name:           "Test Rule",
				Conditions:     []RuleCondition{{Type: ConditionMerchantContains, Value: "test"}},
				ConditionLogic: "AND",
				TargetCategory: []string{"Test"},
				TagBehavior:    "add",
				Source:         "manual",
			},
			wantErr: true,
			errMsg:  "user_id is required",
		},
		{
			name: "missing name",
			rule: CategorizationRule{
				UserID:         "user-123",
				Conditions:     []RuleCondition{{Type: ConditionMerchantContains, Value: "test"}},
				ConditionLogic: "AND",
				TargetCategory: []string{"Test"},
				TagBehavior:    "add",
				Source:         "manual",
			},
			wantErr: true,
			errMsg:  "name is required",
		},
		{
			name: "name too long",
			rule: CategorizationRule{
				UserID:         "user-123",
				Name:           string(make([]byte, 101)), // 101 characters
				Conditions:     []RuleCondition{{Type: ConditionMerchantContains, Value: "test"}},
				ConditionLogic: "AND",
				TargetCategory: []string{"Test"},
				TagBehavior:    "add",
				Source:         "manual",
			},
			wantErr: true,
			errMsg:  "name must be 100 characters or less",
		},
		{
			name: "no action (no category or tags)",
			rule: CategorizationRule{
				UserID:         "user-123",
				Name:           "No Action Rule",
				Conditions:     []RuleCondition{{Type: ConditionMerchantContains, Value: "test"}},
				ConditionLogic: "AND",
				TagBehavior:    "add",
				Source:         "manual",
			},
			wantErr: true,
			errMsg:  "rule must have at least one action",
		},
		{
			name: "empty conditions",
			rule: CategorizationRule{
				UserID:         "user-123",
				Name:           "Empty Conditions Rule",
				Conditions:     []RuleCondition{},
				ConditionLogic: "AND",
				TargetCategory: []string{"Test"},
				TagBehavior:    "add",
				Source:         "manual",
			},
			wantErr: true,
			errMsg:  "at least one condition is required",
		},
		{
			name: "invalid condition logic",
			rule: CategorizationRule{
				UserID:         "user-123",
				Name:           "Invalid Logic Rule",
				Conditions:     []RuleCondition{{Type: ConditionMerchantContains, Value: "test"}},
				ConditionLogic: "XOR",
				TargetCategory: []string{"Test"},
				TagBehavior:    "add",
				Source:         "manual",
			},
			wantErr: true,
			errMsg:  "condition_logic must be AND or OR",
		},
		{
			name: "invalid tag behavior",
			rule: CategorizationRule{
				UserID:         "user-123",
				Name:           "Invalid Tag Behavior Rule",
				Conditions:     []RuleCondition{{Type: ConditionMerchantContains, Value: "test"}},
				ConditionLogic: "AND",
				TargetTags:     []string{"test"},
				TagBehavior:    "invalid",
				Source:         "manual",
			},
			wantErr: true,
			errMsg:  "tag_behavior must be add or replace",
		},
		{
			name: "invalid source",
			rule: CategorizationRule{
				UserID:         "user-123",
				Name:           "Invalid Source Rule",
				Conditions:     []RuleCondition{{Type: ConditionMerchantContains, Value: "test"}},
				ConditionLogic: "AND",
				TargetCategory: []string{"Test"},
				TagBehavior:    "add",
				Source:         "unknown",
			},
			wantErr: true,
			errMsg:  "source must be manual, ai, or template",
		},
		{
			name: "invalid condition type",
			rule: CategorizationRule{
				UserID:         "user-123",
				Name:           "Invalid Condition Type Rule",
				Conditions:     []RuleCondition{{Type: ConditionType("invalid_type"), Value: "test"}},
				ConditionLogic: "AND",
				TargetCategory: []string{"Test"},
				TagBehavior:    "add",
				Source:         "manual",
			},
			wantErr: true,
			errMsg:  "invalid condition type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.rule.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && tt.errMsg != "" {
				if err == nil || !contains(err.Error(), tt.errMsg) {
					t.Errorf("Error should contain '%s', got: %v", tt.errMsg, err)
				}
			}
		})
	}
}

// TestCategorizationRule_HasCategoryAction tests the HasCategoryAction helper
func TestCategorizationRule_HasCategoryAction(t *testing.T) {
	tests := []struct {
		name string
		rule CategorizationRule
		want bool
	}{
		{
			name: "has category action",
			rule: CategorizationRule{
				TargetCategory: []string{"Food & Drink"},
			},
			want: true,
		},
		{
			name: "no category action (nil)",
			rule: CategorizationRule{
				TargetCategory: nil,
			},
			want: false,
		},
		{
			name: "no category action (empty slice)",
			rule: CategorizationRule{
				TargetCategory: []string{},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.rule.HasCategoryAction(); got != tt.want {
				t.Errorf("HasCategoryAction() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestCategorizationRule_HasTagAction tests the HasTagAction helper
func TestCategorizationRule_HasTagAction(t *testing.T) {
	tests := []struct {
		name string
		rule CategorizationRule
		want bool
	}{
		{
			name: "has tag action",
			rule: CategorizationRule{
				TargetTags: []string{"recurring", "subscription"},
			},
			want: true,
		},
		{
			name: "no tag action (nil)",
			rule: CategorizationRule{
				TargetTags: nil,
			},
			want: false,
		},
		{
			name: "no tag action (empty slice)",
			rule: CategorizationRule{
				TargetTags: []string{},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.rule.HasTagAction(); got != tt.want {
				t.Errorf("HasTagAction() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestCategorizationRule_JSONMarshaling tests JSON serialization
func TestCategorizationRule_JSONMarshaling(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)

	original := &CategorizationRule{
		ID:             "rule-550e8400-e29b-41d4-a716-446655440000",
		UserID:         "user-650e8400-e29b-41d4-a716-446655440001",
		Name:           "Coffee Shops",
		Description:    strPtr("Categorize coffee shop purchases"),
		Enabled:        true,
		Priority:       10,
		Conditions:     []RuleCondition{{Type: ConditionMerchantContains, Value: "starbucks"}},
		ConditionLogic: "AND",
		TargetCategory: []string{"Food & Drink", "Coffee Shops"},
		TargetTags:     []string{"recurring"},
		TagBehavior:    "add",
		Source:         "manual",
		MatchCount:     42,
		OverrideCount:  3,
		LastMatchedAt:  &now,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	// Marshal to JSON
	jsonData, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Failed to marshal CategorizationRule: %v", err)
	}

	// Unmarshal back
	var unmarshaled CategorizationRule
	err = json.Unmarshal(jsonData, &unmarshaled)
	if err != nil {
		t.Fatalf("Failed to unmarshal CategorizationRule: %v", err)
	}

	// Verify critical fields match
	if unmarshaled.ID != original.ID {
		t.Errorf("ID mismatch: got %v, want %v", unmarshaled.ID, original.ID)
	}
	if unmarshaled.UserID != original.UserID {
		t.Errorf("UserID mismatch: got %v, want %v", unmarshaled.UserID, original.UserID)
	}
	if unmarshaled.Name != original.Name {
		t.Errorf("Name mismatch: got %v, want %v", unmarshaled.Name, original.Name)
	}
	if unmarshaled.Priority != original.Priority {
		t.Errorf("Priority mismatch: got %v, want %v", unmarshaled.Priority, original.Priority)
	}
	if unmarshaled.ConditionLogic != original.ConditionLogic {
		t.Errorf("ConditionLogic mismatch: got %v, want %v", unmarshaled.ConditionLogic, original.ConditionLogic)
	}
	if len(unmarshaled.Conditions) != len(original.Conditions) {
		t.Errorf("Conditions length mismatch: got %v, want %v", len(unmarshaled.Conditions), len(original.Conditions))
	}
	if unmarshaled.TagBehavior != original.TagBehavior {
		t.Errorf("TagBehavior mismatch: got %v, want %v", unmarshaled.TagBehavior, original.TagBehavior)
	}
	if unmarshaled.Source != original.Source {
		t.Errorf("Source mismatch: got %v, want %v", unmarshaled.Source, original.Source)
	}
}

// TestRuleCondition_JSONMarshaling tests RuleCondition JSON serialization
func TestRuleCondition_JSONMarshaling(t *testing.T) {
	tests := []struct {
		name      string
		condition RuleCondition
	}{
		{
			name: "merchant contains condition",
			condition: RuleCondition{
				Type:  ConditionMerchantContains,
				Value: "starbucks",
			},
		},
		{
			name: "amount greater than condition",
			condition: RuleCondition{
				Type:  ConditionAmountGreaterThan,
				Value: float64(500),
			},
		},
		{
			name: "amount range condition",
			condition: RuleCondition{
				Type: ConditionAmountRange,
				Value: map[string]interface{}{
					"min": float64(10),
					"max": float64(100),
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Marshal to JSON
			jsonData, err := json.Marshal(tt.condition)
			if err != nil {
				t.Fatalf("Failed to marshal RuleCondition: %v", err)
			}

			// Unmarshal back
			var unmarshaled RuleCondition
			err = json.Unmarshal(jsonData, &unmarshaled)
			if err != nil {
				t.Fatalf("Failed to unmarshal RuleCondition: %v", err)
			}

			if unmarshaled.Type != tt.condition.Type {
				t.Errorf("Type mismatch: got %v, want %v", unmarshaled.Type, tt.condition.Type)
			}
		})
	}
}

// TestConditionType_IsValid tests the IsValid helper for condition types
func TestConditionType_IsValid(t *testing.T) {
	validTypes := []ConditionType{
		ConditionMerchantContains,
		ConditionMerchantEquals,
		ConditionMerchantStartsWith,
		ConditionMerchantEndsWith,
		ConditionAmountGreaterThan,
		ConditionAmountLessThan,
		ConditionAmountEquals,
		ConditionAmountRange,
		ConditionAccountID,
		ConditionPaymentChannel,
		ConditionExistingCategory,
		ConditionDateRange,
	}

	for _, ct := range validTypes {
		t.Run(string(ct), func(t *testing.T) {
			if !ct.IsValid() {
				t.Errorf("IsValid() = false, want true for %s", ct)
			}
		})
	}

	// Test invalid type
	invalidType := ConditionType("invalid")
	if invalidType.IsValid() {
		t.Errorf("IsValid() = true, want false for invalid type")
	}
}
