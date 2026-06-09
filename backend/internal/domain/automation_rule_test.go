package domain

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// BoostType Tests
// =============================================================================

func TestRuleType_IsValid(t *testing.T) {
	tests := []struct {
		name     string
		ruleType BoostType
		want     bool
	}{
		{"transfer_suggestion is valid", BoostTypeTransferSuggestion, true},
		{"budget_alert is valid", BoostTypeBudgetAlert, true},
		{"category_rollover is valid", BoostTypeCategoryRollover, true},
		{"empty string is invalid", BoostType(""), false},
		{"unknown type is invalid", BoostType("unknown"), false},
		{"case sensitive - uppercase is invalid", BoostType("TRANSFER_SUGGESTION"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.ruleType.IsValid()
			assert.Equal(t, tt.want, got)
		})
	}
}

// =============================================================================
// TriggerType Tests
// =============================================================================

func TestTriggerType_IsValid(t *testing.T) {
	tests := []struct {
		name        string
		triggerType TriggerType
		want        bool
	}{
		{"event is valid", TriggerTypeEvent, true},
		{"schedule is valid", TriggerTypeSchedule, true},
		{"empty string is invalid", TriggerType(""), false},
		{"unknown type is invalid", TriggerType("webhook"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.triggerType.IsValid()
			assert.Equal(t, tt.want, got)
		})
	}
}

// =============================================================================
// BoostCondition Tests
// =============================================================================

func TestAutomationRuleCondition_CategoryID_JSON_RoundTrip(t *testing.T) {
	tests := []struct {
		name          string
		condition     BoostCondition
		expectedJSON  string
		expectKeyJSON bool
	}{
		{
			name: "condition with category_id",
			condition: BoostCondition{
				Field:      "budget_category.percent_used",
				Operator:   ConditionOperatorGte,
				Value:      80,
				CategoryID: "cat-123",
			},
			expectedJSON:  `"category_id":"cat-123"`,
			expectKeyJSON: true,
		},
		{
			name: "condition without category_id omits key",
			condition: BoostCondition{
				Field:    "account.balance",
				Operator: ConditionOperatorGt,
				Value:    1000,
			},
			expectedJSON:  `"category_id"`,
			expectKeyJSON: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Marshal to JSON
			jsonData, err := json.Marshal(tt.condition)
			require.NoError(t, err)

			// Check presence/absence of category_id key
			jsonStr := string(jsonData)
			if tt.expectKeyJSON {
				assert.Contains(t, jsonStr, tt.expectedJSON)
			} else {
				assert.NotContains(t, jsonStr, tt.expectedJSON)
			}

			// Unmarshal back and verify round-trip for CategoryID
			var unmarshal BoostCondition
			err = json.Unmarshal(jsonData, &unmarshal)
			require.NoError(t, err)

			assert.Equal(t, tt.condition.Field, unmarshal.Field)
			assert.Equal(t, tt.condition.Operator, unmarshal.Operator)
			// Note: Value uses interface{}, which JSON unmarshaling converts to float64 for numbers
			// So we only verify CategoryID round-trips correctly
			assert.Equal(t, tt.condition.CategoryID, unmarshal.CategoryID)
		})
	}
}

func TestAutomationRuleCondition_BudgetIDRoundTrips(t *testing.T) {
	tests := []struct {
		name          string
		condition     BoostCondition
		expectedJSON  string
		expectKeyJSON bool
	}{
		{
			name: "condition with budget_id",
			condition: BoostCondition{
				Field:    "budget.percent_used",
				Operator: ConditionOperatorGte,
				Value:    80,
				BudgetID: "budget-123",
			},
			expectedJSON:  `"budget_id":"budget-123"`,
			expectKeyJSON: true,
		},
		{
			name: "condition without budget_id omits key",
			condition: BoostCondition{
				Field:    "account.balance",
				Operator: ConditionOperatorGt,
				Value:    1000,
			},
			expectedJSON:  `"budget_id"`,
			expectKeyJSON: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Marshal to JSON
			jsonData, err := json.Marshal(tt.condition)
			require.NoError(t, err)

			// Check presence/absence of budget_id key
			jsonStr := string(jsonData)
			if tt.expectKeyJSON {
				assert.Contains(t, jsonStr, tt.expectedJSON)
			} else {
				assert.NotContains(t, jsonStr, tt.expectedJSON)
			}

			// Unmarshal back and verify round-trip for BudgetID
			var unmarshaled BoostCondition
			err = json.Unmarshal(jsonData, &unmarshaled)
			require.NoError(t, err)

			assert.Equal(t, tt.condition.Field, unmarshaled.Field)
			assert.Equal(t, tt.condition.Operator, unmarshaled.Operator)
			assert.Equal(t, tt.condition.BudgetID, unmarshaled.BudgetID)
		})
	}
}

func TestAutomationRuleConditionOperator_IsValid(t *testing.T) {
	tests := []struct {
		name     string
		operator ConditionOperator
		want     bool
	}{
		{"eq is valid", ConditionOperatorEq, true},
		{"neq is valid", ConditionOperatorNeq, true},
		{"gt is valid", ConditionOperatorGt, true},
		{"gte is valid", ConditionOperatorGte, true},
		{"lt is valid", ConditionOperatorLt, true},
		{"lte is valid", ConditionOperatorLte, true},
		{"between is valid", ConditionOperatorBetween, true},
		{"contains is valid", ConditionOperatorContains, true},
		{"empty is invalid", ConditionOperator(""), false},
		{"unknown is invalid", ConditionOperator("like"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.operator.IsValid()
			assert.Equal(t, tt.want, got)
		})
	}
}

// =============================================================================
// TriggerConfig Tests
// =============================================================================

func TestTriggerConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  TriggerConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid event trigger",
			config: TriggerConfig{
				Type:  TriggerTypeEvent,
				Event: "budget_updated",
			},
			wantErr: false,
		},
		{
			name: "valid event trigger with filters",
			config: TriggerConfig{
				Type:  TriggerTypeEvent,
				Event: "budget_period_end",
				Filters: map[string]interface{}{
					"min_amount": 100,
				},
			},
			wantErr: false,
		},
		{
			name: "valid event trigger — transaction_created",
			config: TriggerConfig{
				Type:  TriggerTypeEvent,
				Event: "transaction_created",
			},
			wantErr: false,
		},
		{
			name: "valid event trigger — balance_changed",
			config: TriggerConfig{
				Type:  TriggerTypeEvent,
				Event: "balance_changed",
			},
			wantErr: false,
		},
		{
			name: "unknown event type returns error",
			config: TriggerConfig{
				Type:  TriggerTypeEvent,
				Event: "foobar_event",
			},
			wantErr: true,
			errMsg:  "unknown event type",
		},
		{
			name: "valid schedule trigger",
			config: TriggerConfig{
				Type:     TriggerTypeSchedule,
				Cron:     "0 0 1 * *",
				Timezone: "America/New_York",
			},
			wantErr: false,
		},
		{
			name: "invalid trigger type",
			config: TriggerConfig{
				Type: TriggerType("invalid"),
			},
			wantErr: true,
			errMsg:  "invalid trigger type",
		},
		{
			name: "event trigger missing event",
			config: TriggerConfig{
				Type: TriggerTypeEvent,
			},
			wantErr: true,
			errMsg:  "event is required",
		},
		{
			name: "schedule trigger missing cron",
			config: TriggerConfig{
				Type: TriggerTypeSchedule,
			},
			wantErr: true,
			errMsg:  "cron expression is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// =============================================================================
// validateConditions Tests
// =============================================================================

func TestAutomationRule_validateConditions(t *testing.T) {
	// Helper: build a minimal valid event-triggered rule.
	validEventRule := func(event string, conditions []BoostCondition) Boost {
		return Boost{
			UserID:   "user-123",
			Name:     "Test Rule",
			BoostType: BoostTypeBudgetAlert,
			TriggerConfig: TriggerConfig{
				Type:  TriggerTypeEvent,
				Event: event,
			},
			Conditions:     conditions,
			ConditionLogic: "AND",
			Actions:        json.RawMessage(`{"notification_channels":["email"],"threshold_type":"percentage","threshold_value":80}`),
		}
	}

	tests := []struct {
		name    string
		rule    Boost
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid budget.percent_used field with eq operator",
			rule: validEventRule("budget_updated", []BoostCondition{
				{Field: "budget.percent_used", Operator: ConditionOperatorEq, Value: 80},
			}),
			wantErr: false,
		},
		{
			name: "valid budget_category.percent_used with CategoryID set",
			rule: validEventRule("budget_updated", []BoostCondition{
				{Field: "budget_category.percent_used", Operator: ConditionOperatorGte, Value: 80, CategoryID: "cat-1"},
			}),
			wantErr: false,
		},
		{
			name: "unknown field returns not valid for event error",
			rule: validEventRule("budget_updated", []BoostCondition{
				{Field: "budget.typo_field", Operator: ConditionOperatorEq, Value: 80},
			}),
			wantErr: true,
			errMsg:  "not valid for event",
		},
		{
			name: "operator between returns unsupported operator error",
			rule: validEventRule("budget_updated", []BoostCondition{
				{Field: "budget.percent_used", Operator: ConditionOperatorBetween, Value: 80},
			}),
			wantErr: true,
			errMsg:  "unsupported operator",
		},
		{
			// Issue #1271 Item 2: Validate() no longer rejects missing CategoryID.
			// The strict check moved to ValidateForPersistence() (used by create/update).
			// ExecuteBoost uses Validate() so that legacy/partial rules don't brick.
			name: "budget_category field without CategoryID is accepted by Validate()",
			rule: validEventRule("budget_updated", []BoostCondition{
				{Field: "budget_category.percent_used", Operator: ConditionOperatorGte, Value: 80},
			}),
			wantErr: false,
		},
		{
			name: "schedule-type trigger rejects unsupported operator",
			rule: Boost{
				UserID:   "user-123",
				Name:     "Scheduled Transfer",
				BoostType: BoostTypeTransferSuggestion,
				TriggerConfig: TriggerConfig{
					Type:     TriggerTypeSchedule,
					Cron:     "0 0 1 * *",
					Timezone: "America/New_York",
				},
				Conditions: []BoostCondition{
					{Field: "account.balance", Operator: ConditionOperatorBetween, Value: 1000},
				},
				ConditionLogic: "AND",
				Actions:        json.RawMessage(`{"from_account_id":"acc-1","to_account_id":"acc-2","amount":100}`),
			},
			wantErr: true,
			errMsg:  "unsupported operator",
		},
		{
			name: "schedule-type trigger accepts valid operator",
			rule: Boost{
				UserID:   "user-123",
				Name:     "Scheduled Transfer",
				BoostType: BoostTypeTransferSuggestion,
				TriggerConfig: TriggerConfig{
					Type:     TriggerTypeSchedule,
					Cron:     "0 0 1 * *",
					Timezone: "America/New_York",
				},
				Conditions: []BoostCondition{
					{Field: "account.balance", Operator: ConditionOperatorGt, Value: 1000},
				},
				ConditionLogic: "AND",
				Actions:        json.RawMessage(`{"from_account_id":"acc-1","to_account_id":"acc-2","amount":100}`),
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.rule.Validate()
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// =============================================================================
// Boost Validation Tests
// =============================================================================

func TestAutomationRule_Validate_RequiredFields(t *testing.T) {
	tests := []struct {
		name    string
		rule    Boost
		wantErr bool
		errMsg  string
	}{
		{
			name:    "missing user_id",
			rule:    Boost{Name: "Test Rule", BoostType: BoostTypeTransferSuggestion},
			wantErr: true,
			errMsg:  "user_id is required",
		},
		{
			name:    "missing name",
			rule:    Boost{UserID: "user-123", BoostType: BoostTypeTransferSuggestion},
			wantErr: true,
			errMsg:  "name is required",
		},
		{
			name:    "name too long",
			rule:    Boost{UserID: "user-123", Name: string(make([]byte, 101)), BoostType: BoostTypeTransferSuggestion},
			wantErr: true,
			errMsg:  "name must be 100 characters or less",
		},
		{
			name:    "missing boost_type",
			rule:    Boost{UserID: "user-123", Name: "Test Rule"},
			wantErr: true,
			errMsg:  "invalid boost_type",
		},
		{
			name:    "invalid boost_type",
			rule:    Boost{UserID: "user-123", Name: "Test Rule", BoostType: BoostType("invalid")},
			wantErr: true,
			errMsg:  "invalid boost_type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.rule.Validate()
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestAutomationRule_Validate_ConditionLogic(t *testing.T) {
	baseRule := func() Boost {
		return Boost{
			UserID:   "user-123",
			Name:     "Test Rule",
			BoostType: BoostTypeTransferSuggestion,
			TriggerConfig: TriggerConfig{
				Type:     TriggerTypeSchedule,
				Cron:     "0 0 1 * *",
				Timezone: "America/New_York",
			},
			Conditions: []BoostCondition{
				{Field: "account.balance", Operator: ConditionOperatorGt, Value: 1000},
			},
			Actions: json.RawMessage(`{"from_account_id":"acc-1","to_account_id":"acc-2","amount":100}`),
		}
	}

	tests := []struct {
		name           string
		conditionLogic string
		wantErr        bool
		errMsg         string
	}{
		{"AND is valid", "AND", false, ""},
		{"OR is valid", "OR", false, ""},
		{"empty defaults to AND", "", false, ""},
		{"invalid logic", "XOR", true, "condition_logic must be AND or OR"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule := baseRule()
			rule.ConditionLogic = tt.conditionLogic
			err := rule.Validate()
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestAutomationRule_Validate_TransferSuggestionRule(t *testing.T) {
	validRule := Boost{
		UserID:   "user-123",
		Name:     "Excess Savings Transfer",
		BoostType: BoostTypeTransferSuggestion,
		TriggerConfig: TriggerConfig{
			Type:     TriggerTypeSchedule,
			Cron:     "0 0 1 * *",
			Timezone: "America/New_York",
		},
		Conditions: []BoostCondition{
			{Field: "account.balance", Operator: ConditionOperatorGt, Value: 5000},
		},
		ConditionLogic: "AND",
		Actions:        json.RawMessage(`{"from_account_id":"acc-1","to_account_id":"acc-2","amount_type":"fixed","amount":100}`),
	}

	t.Run("valid transfer suggestion rule", func(t *testing.T) {
		err := validRule.Validate()
		assert.NoError(t, err)
	})

	t.Run("transfer suggestion requires from_account_id in actions", func(t *testing.T) {
		rule := validRule
		rule.Actions = json.RawMessage(`{"to_account_id":"acc-2","amount":100}`)
		err := rule.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "from_account_id")
	})

	t.Run("transfer suggestion requires to_account_id in actions", func(t *testing.T) {
		rule := validRule
		rule.Actions = json.RawMessage(`{"from_account_id":"acc-1","amount":100}`)
		err := rule.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "to_account_id")
	})

	t.Run("transfer suggestion requires positive amount for fixed type", func(t *testing.T) {
		rule := validRule
		rule.Actions = json.RawMessage(`{"from_account_id":"acc-1","to_account_id":"acc-2","amount_type":"fixed","amount":-100}`)
		err := rule.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "amount must be positive")
	})

	// Issue #972 GAP AC: excess amount_type must be accepted even when amount=0 (threshold drives amount).
	t.Run("excess amount_type with zero amount and threshold is valid", func(t *testing.T) {
		rule := validRule
		rule.Actions = json.RawMessage(`{"from_account_id":"acc-1","to_account_id":"acc-2","amount_type":"excess","amount":0,"threshold":50}`)
		err := rule.Validate()
		assert.NoError(t, err, "excess rules have amount=0; amount>0 check must be skipped for excess")
	})

	// Issue #972 GAP AC: remaining amount_type must be accepted when amount=0 (derived from triggerData).
	t.Run("remaining amount_type with zero amount is valid", func(t *testing.T) {
		rule := validRule
		rule.Actions = json.RawMessage(`{"from_account_id":"acc-1","to_account_id":"acc-2","amount_type":"remaining","amount":0}`)
		err := rule.Validate()
		assert.NoError(t, err, "remaining rules have amount=0; amount>0 check must be skipped for remaining")
	})

	// Issue #972 GAP AC: invalid amount_type must be rejected.
	t.Run("invalid amount_type is rejected", func(t *testing.T) {
		rule := validRule
		rule.Actions = json.RawMessage(`{"from_account_id":"acc-1","to_account_id":"acc-2","amount_type":"bogus","amount":100}`)
		err := rule.Validate()
		require.Error(t, err, "bogus amount_type must be rejected by validateTransferAction")
		assert.Contains(t, err.Error(), "amount_type")
	})

	// Issue #972 GAP AC (Codex Q4): legacy 'threshold' type must still be accepted so saved rules keep firing.
	t.Run("legacy threshold amount_type is valid", func(t *testing.T) {
		rule := validRule
		rule.Actions = json.RawMessage(`{"from_account_id":"acc-1","to_account_id":"acc-2","amount_type":"threshold","threshold":100}`)
		err := rule.Validate()
		assert.NoError(t, err, "threshold is a legacy amount_type and must remain valid")
	})

	// Issue #972 GAP AC (Codex Q6): negative threshold on excess rule must be rejected to prevent over-transfer.
	t.Run("negative threshold on excess is rejected", func(t *testing.T) {
		rule := validRule
		rule.Actions = json.RawMessage(`{"from_account_id":"acc-1","to_account_id":"acc-2","amount_type":"excess","amount":0,"threshold":-10}`)
		err := rule.Validate()
		require.Error(t, err, "negative threshold must be rejected")
		assert.Contains(t, err.Error(), "threshold")
	})

	// Issue #972 GAP AC (Codex Q6): negative threshold on legacy 'threshold' type is also rejected.
	t.Run("negative threshold on threshold type is rejected", func(t *testing.T) {
		rule := validRule
		rule.Actions = json.RawMessage(`{"from_account_id":"acc-1","to_account_id":"acc-2","amount_type":"threshold","threshold":-5}`)
		err := rule.Validate()
		require.Error(t, err, "negative threshold must be rejected for threshold type too")
		assert.Contains(t, err.Error(), "threshold")
	})
}

// TestTransferAmountTypeContract_C9 verifies that every amount_type offered by the
// frontend UI (TransferActionForm.tsx: fixed, percentage, excess) is accepted by
// the backend validation allow-set, and that backend-only types (remaining, threshold)
// are also valid. This is the C9 contract guard — it locks the UI⊆backend invariant
// so a UI-offered type can never be silently rejected at create/update time.
func TestTransferAmountTypeContract_C9(t *testing.T) {
	baseRule := Boost{
		UserID:   "user-c9",
		Name:     "Transfer Contract Test",
		BoostType: BoostTypeTransferSuggestion,
		TriggerConfig: TriggerConfig{
			Type:  TriggerTypeEvent,
			Event: "balance_changed",
		},
		ConditionLogic: "AND",
	}

	// UI-offered amount_types: the frontend exposes exactly these three.
	// ALL must be accepted by the backend (UI ⊆ backend allow-set).
	uiOfferedTypes := []struct {
		amountType string
		actions    string
		desc       string
	}{
		{
			amountType: "fixed",
			actions:    `{"from_account_id":"acc-1","to_account_id":"acc-2","amount_type":"fixed","amount":100}`,
			desc:       "fixed — standard dollar amount (UI option 1/3)",
		},
		{
			amountType: "percentage",
			actions:    `{"from_account_id":"acc-1","to_account_id":"acc-2","amount_type":"percentage","amount":10}`,
			desc:       "percentage — % of period income (UI option 2/3); amount stores percentage value",
		},
		{
			amountType: "excess",
			actions:    `{"from_account_id":"acc-1","to_account_id":"acc-2","amount_type":"excess","amount":0,"threshold":50}`,
			desc:       "excess — surplus above threshold (UI option 3/3); amount=0 is valid",
		},
	}

	for _, tt := range uiOfferedTypes {
		t.Run("UI type "+tt.amountType+" must be accepted: "+tt.desc, func(t *testing.T) {
			rule := baseRule
			rule.Actions = json.RawMessage(tt.actions)
			err := rule.Validate()
			assert.NoError(t, err,
				"amount_type %q is offered by the UI and must be accepted by validateTransferAction (UI ⊆ backend allow-set)",
				tt.amountType)
		})
	}

	// Backend-only amount_types (not in the UI but must remain valid for API-created rules).
	backendOnlyTypes := []struct {
		amountType string
		actions    string
	}{
		{
			amountType: "remaining",
			actions:    `{"from_account_id":"acc-1","to_account_id":"acc-2","amount_type":"remaining","amount":0}`,
		},
		{
			amountType: "threshold",
			actions:    `{"from_account_id":"acc-1","to_account_id":"acc-2","amount_type":"threshold","threshold":100}`,
		},
	}
	for _, tt := range backendOnlyTypes {
		t.Run("backend-only type "+tt.amountType+" must also be accepted", func(t *testing.T) {
			rule := baseRule
			rule.Actions = json.RawMessage(tt.actions)
			err := rule.Validate()
			assert.NoError(t, err, "backend-only amount_type %q must remain valid for API-created rules", tt.amountType)
		})
	}
}

func TestAutomationRule_Validate_BudgetAlertRule(t *testing.T) {
	validRule := Boost{
		UserID:   "user-123",
		Name:     "Budget Warning",
		BoostType: BoostTypeBudgetAlert,
		TriggerConfig: TriggerConfig{
			Type:  TriggerTypeEvent,
			Event: "budget_updated",
		},
		Conditions: []BoostCondition{
			{Field: "budget_category.percent_used", Operator: ConditionOperatorGte, Value: 80, CategoryID: "cat-1"},
		},
		ConditionLogic: "AND",
		Actions:        json.RawMessage(`{"notification_channels":["email","push"],"threshold_type":"percentage","threshold_value":80}`),
	}

	t.Run("valid budget alert rule", func(t *testing.T) {
		err := validRule.Validate()
		assert.NoError(t, err)
	})

	t.Run("budget alert requires notification_channels", func(t *testing.T) {
		rule := validRule
		rule.Actions = json.RawMessage(`{"threshold_type":"percentage","threshold_value":80}`)
		err := rule.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "notification_channels")
	})

	t.Run("budget alert requires valid threshold_type", func(t *testing.T) {
		rule := validRule
		rule.Actions = json.RawMessage(`{"notification_channels":["email"],"threshold_type":"invalid","threshold_value":80}`)
		err := rule.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "threshold_type")
	})
}

func TestAutomationRule_Validate_CategoryRolloverRule(t *testing.T) {
	validRule := Boost{
		UserID:   "user-123",
		Name:     "Savings Rollover",
		BoostType: BoostTypeCategoryRollover,
		TriggerConfig: TriggerConfig{
			Type:  TriggerTypeEvent,
			Event: "budget_period_end",
		},
		Conditions: []BoostCondition{
			{Field: "budget_category.remaining_amount", Operator: ConditionOperatorGt, Value: 0, CategoryID: "cat-1"},
		},
		ConditionLogic: "AND",
		Actions:        json.RawMessage(`{"source_budget_category_id":"cat-1","rollover_type":"full"}`),
	}

	t.Run("valid category rollover rule", func(t *testing.T) {
		err := validRule.Validate()
		assert.NoError(t, err)
	})

	t.Run("category rollover requires source_budget_category_id", func(t *testing.T) {
		rule := validRule
		rule.Actions = json.RawMessage(`{"rollover_type":"full"}`)
		err := rule.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "source_budget_category_id")
	})

	t.Run("category rollover requires valid rollover_type", func(t *testing.T) {
		rule := validRule
		rule.Actions = json.RawMessage(`{"source_budget_category_id":"cat-1","rollover_type":"invalid"}`)
		err := rule.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "rollover_type")
	})

	t.Run("category rollover capped type requires cap_amount", func(t *testing.T) {
		rule := validRule
		rule.Actions = json.RawMessage(`{"source_budget_category_id":"cat-1","rollover_type":"capped"}`)
		err := rule.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "cap_amount")
	})

	t.Run("category rollover percentage type requires percentage", func(t *testing.T) {
		rule := validRule
		rule.Actions = json.RawMessage(`{"source_budget_category_id":"cat-1","rollover_type":"percentage"}`)
		err := rule.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "percentage")
	})
}

// =============================================================================
// Action Parsing Tests
// =============================================================================

func TestAutomationRule_ParseTransferAction(t *testing.T) {
	rule := Boost{
		BoostType: BoostTypeTransferSuggestion,
		Actions:  json.RawMessage(`{"from_account_id":"acc-1","to_account_id":"acc-2","amount_type":"fixed","amount":150.50,"threshold":1000}`),
	}

	action, err := rule.ParseTransferAction()
	require.NoError(t, err)

	assert.Equal(t, "acc-1", action.FromAccountID)
	assert.Equal(t, "acc-2", action.ToAccountID)
	assert.Equal(t, "fixed", action.AmountType)
	assert.Equal(t, 150.50, action.Amount)
	assert.Equal(t, 1000.0, action.Threshold)
}

func TestAutomationRule_ParseAlertAction(t *testing.T) {
	rule := Boost{
		BoostType: BoostTypeBudgetAlert,
		Actions:  json.RawMessage(`{"notification_channels":["email","push"],"threshold_type":"percentage","threshold_value":80,"budget_id":"budget-1","category_id":"cat-1"}`),
	}

	action, err := rule.ParseAlertAction()
	require.NoError(t, err)

	assert.Equal(t, []string{"email", "push"}, action.NotificationChannels)
	assert.Equal(t, "percentage", action.ThresholdType)
	assert.Equal(t, 80.0, action.ThresholdValue)
	assert.Equal(t, "budget-1", action.BudgetID)
	assert.Equal(t, "cat-1", action.CategoryID)
}

func TestAutomationRule_ParseRolloverAction(t *testing.T) {
	rule := Boost{
		BoostType: BoostTypeCategoryRollover,
		Actions:  json.RawMessage(`{"source_budget_category_id":"cat-1","rollover_type":"capped","cap_amount":100}`),
	}

	action, err := rule.ParseRolloverAction()
	require.NoError(t, err)

	assert.Equal(t, "cat-1", action.SourceBudgetCategoryID)
	assert.Equal(t, "capped", action.RolloverType)
	assert.NotNil(t, action.CapAmount)
	assert.Equal(t, 100.0, *action.CapAmount)
}

// =============================================================================
// BoostExecution Tests
// =============================================================================

func TestRuleExecutionStatus_IsValid(t *testing.T) {
	tests := []struct {
		name   string
		status ExecutionStatus
		want   bool
	}{
		{"pending is valid", ExecutionStatusPending, true},
		{"running is valid", ExecutionStatusRunning, true},
		{"completed is valid", ExecutionStatusCompleted, true},
		{"failed is valid", ExecutionStatusFailed, true},
		{"empty is invalid", ExecutionStatus(""), false},
		{"unknown is invalid", ExecutionStatus("cancelled"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.status.IsValid()
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestRuleExecution_MarkComplete(t *testing.T) {
	execution := &BoostExecution{
		Status:    ExecutionStatusRunning,
		StartedAt: time.Now().Add(-100 * time.Millisecond),
	}

	execution.MarkComplete()

	assert.Equal(t, ExecutionStatusCompleted, execution.Status)
	assert.NotNil(t, execution.CompletedAt)
	assert.NotNil(t, execution.ExecutionDurationMs)
	assert.Greater(t, *execution.ExecutionDurationMs, 0)
}

func TestRuleExecution_MarkFailed(t *testing.T) {
	execution := &BoostExecution{
		Status:    ExecutionStatusRunning,
		StartedAt: time.Now().Add(-100 * time.Millisecond),
	}

	execution.MarkFailed("VALIDATION_ERROR", "Invalid account ID")

	assert.Equal(t, ExecutionStatusFailed, execution.Status)
	assert.NotNil(t, execution.CompletedAt)
	assert.NotNil(t, execution.ErrorCode)
	assert.Equal(t, "VALIDATION_ERROR", *execution.ErrorCode)
	assert.NotNil(t, execution.ErrorMessage)
	assert.Equal(t, "Invalid account ID", *execution.ErrorMessage)
}

// =============================================================================
// TransferSuggestion Tests
// =============================================================================

func TestTransferSuggestionStatus_IsValid(t *testing.T) {
	tests := []struct {
		name   string
		status SuggestionStatus
		want   bool
	}{
		{"pending is valid", SuggestionStatusPending, true},
		{"accepted is valid", SuggestionStatusAccepted, true},
		{"rejected is valid", SuggestionStatusRejected, true},
		{"expired is valid", SuggestionStatusExpired, true},
		{"completed is valid", SuggestionStatusCompleted, true},
		{"empty is invalid", SuggestionStatus(""), false},
		{"unknown is invalid", SuggestionStatus("cancelled"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.status.IsValid()
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestTransferSuggestionSource_IsValid(t *testing.T) {
	tests := []struct {
		name   string
		source SuggestionSource
		want   bool
	}{
		{"rule is valid", SuggestionSourceBoost, true},
		{"manual is valid", SuggestionSourceManual, true},
		{"ai is valid", SuggestionSourceAI, true},
		{"empty is invalid", SuggestionSource(""), false},
		{"unknown is invalid", SuggestionSource("system"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.source.IsValid()
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestTransferSuggestion_Validate(t *testing.T) {
	validSuggestion := TransferSuggestion{
		UserID:       "user-123",
		Amount:       100.00,
		CurrencyCode: "USD",
		Source:       SuggestionSourceBoost,
		Status:       SuggestionStatusPending,
	}

	t.Run("valid suggestion", func(t *testing.T) {
		err := validSuggestion.Validate()
		assert.NoError(t, err)
	})

	t.Run("missing user_id", func(t *testing.T) {
		s := validSuggestion
		s.UserID = ""
		err := s.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "user_id")
	})

	t.Run("negative amount", func(t *testing.T) {
		s := validSuggestion
		s.Amount = -100
		err := s.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "amount must be positive")
	})

	t.Run("zero amount", func(t *testing.T) {
		s := validSuggestion
		s.Amount = 0
		err := s.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "amount must be positive")
	})

	t.Run("invalid source", func(t *testing.T) {
		s := validSuggestion
		s.Source = SuggestionSource("invalid")
		err := s.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid source")
	})

	t.Run("invalid status", func(t *testing.T) {
		s := validSuggestion
		s.Status = SuggestionStatus("invalid")
		err := s.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid status")
	})
}

func TestTransferSuggestion_Accept(t *testing.T) {
	suggestion := &TransferSuggestion{
		Status: SuggestionStatusPending,
	}

	notes := "Approved for month end"
	suggestion.Accept(&notes)

	assert.Equal(t, SuggestionStatusAccepted, suggestion.Status)
	assert.NotNil(t, suggestion.UserActionAt)
	assert.NotNil(t, suggestion.UserNotes)
	assert.Equal(t, "Approved for month end", *suggestion.UserNotes)
}

func TestTransferSuggestion_Reject(t *testing.T) {
	suggestion := &TransferSuggestion{
		Status: SuggestionStatusPending,
	}

	notes := "Not needed this month"
	suggestion.Reject(&notes)

	assert.Equal(t, SuggestionStatusRejected, suggestion.Status)
	assert.NotNil(t, suggestion.UserActionAt)
	assert.NotNil(t, suggestion.UserNotes)
	assert.Equal(t, "Not needed this month", *suggestion.UserNotes)
}

func TestTransferSuggestion_Complete(t *testing.T) {
	t.Run("success case — pending suggestion", func(t *testing.T) {
		suggestion := &TransferSuggestion{
			Status: SuggestionStatusPending,
		}

		before := time.Now().UTC()
		err := suggestion.Complete("txn-abc", 150.0, "exact_match")
		after := time.Now().UTC()

		require.NoError(t, err)
		assert.Equal(t, SuggestionStatusCompleted, suggestion.Status)
		assert.NotNil(t, suggestion.CompletedAt)
		assert.True(t, !suggestion.CompletedAt.Before(before) && !suggestion.CompletedAt.After(after))
		require.NotNil(t, suggestion.MatchedTransactionID)
		assert.Equal(t, "txn-abc", *suggestion.MatchedTransactionID)
		require.NotNil(t, suggestion.ActualAmount)
		assert.Equal(t, 150.0, *suggestion.ActualAmount)
		require.NotNil(t, suggestion.CompletionSource)
		assert.Equal(t, "exact_match", *suggestion.CompletionSource)
	})

	t.Run("success case — accepted suggestion", func(t *testing.T) {
		suggestion := &TransferSuggestion{
			Status: SuggestionStatusAccepted,
		}

		err := suggestion.Complete("txn-def", 200.0, "user_confirmed")

		require.NoError(t, err)
		assert.Equal(t, SuggestionStatusCompleted, suggestion.Status)
		assert.NotNil(t, suggestion.CompletedAt)
	})

	t.Run("already completed returns error", func(t *testing.T) {
		suggestion := &TransferSuggestion{
			Status: SuggestionStatusCompleted,
		}

		err := suggestion.Complete("txn-xyz", 100.0, "exact_match")

		assert.ErrorIs(t, err, ErrTransferSuggestionAlreadyActioned)
	})

	t.Run("all completion fields populated", func(t *testing.T) {
		suggestion := &TransferSuggestion{
			Status: SuggestionStatusPending,
		}

		err := suggestion.Complete("txn-ghi", 75.50, "user_confirmed")
		require.NoError(t, err)

		assert.NotNil(t, suggestion.CompletedAt)
		assert.NotNil(t, suggestion.MatchedTransactionID)
		assert.NotNil(t, suggestion.ActualAmount)
		assert.NotNil(t, suggestion.CompletionSource)
	})
}

// =============================================================================
// Error Constants Tests
// =============================================================================

func TestErrorConstants(t *testing.T) {
	// Verify error constants are defined and unique
	errors := []error{
		ErrBoostNotFound,
		ErrBoostAlreadyExists,
		ErrTransferSuggestionNotFound,
	}

	// Check all are non-nil
	for _, err := range errors {
		assert.NotNil(t, err)
	}

	// Check all have unique messages
	messages := make(map[string]bool)
	for _, err := range errors {
		msg := err.Error()
		assert.False(t, messages[msg], "Duplicate error message: %s", msg)
		messages[msg] = true
	}
}

// =============================================================================
// GAP AG — numeric bound validation tests
// =============================================================================

// TestValidateTransferAction_PercentageUpperBound verifies that a transfer rule with
// amount_type:"percentage" rejects amounts > 100 (a percentage cannot exceed 100%).
// Without this bound, API-crafted rules could set percentage=500 and over-suggest 5× income.
func TestValidateTransferAction_PercentageUpperBound(t *testing.T) {
	base := Boost{
		UserID:   "user-1",
		Name:     "Transfer %",
		BoostType: BoostTypeTransferSuggestion,
		TriggerConfig: TriggerConfig{
			Type:  TriggerTypeEvent,
			Event: EventBudgetPeriodEnd,
		},
		ConditionLogic: "AND",
	}

	t.Run("percentage=200 is rejected (over 100%)", func(t *testing.T) {
		rule := base
		rule.Actions = json.RawMessage(`{"from_account_id":"a","to_account_id":"b","amount_type":"percentage","amount":200}`)
		err := rule.Validate()
		require.Error(t, err, "percentage > 100 must be rejected")
		assert.Contains(t, err.Error(), "percentage")
	})

	t.Run("percentage=100 is accepted (exactly 100%)", func(t *testing.T) {
		rule := base
		rule.Actions = json.RawMessage(`{"from_account_id":"a","to_account_id":"b","amount_type":"percentage","amount":100}`)
		err := rule.Validate()
		assert.NoError(t, err, "exactly 100% must be valid")
	})

	t.Run("percentage=50 is accepted", func(t *testing.T) {
		rule := base
		rule.Actions = json.RawMessage(`{"from_account_id":"a","to_account_id":"b","amount_type":"percentage","amount":50}`)
		err := rule.Validate()
		assert.NoError(t, err)
	})
}

// TestValidateSavingsGoalAction_RemainingThresholdBound verifies that a savings_goal rule
// with amount_type:"remaining" rejects a negative threshold. A negative threshold would
// make calculateSavingsGoalAmount compute remaining + |threshold| — an over-suggestion.
func TestValidateSavingsGoalAction_RemainingThresholdBound(t *testing.T) {
	base := Boost{
		UserID:   "user-1",
		Name:     "SG remaining",
		BoostType: BoostTypeSavingsGoal,
		TriggerConfig: TriggerConfig{
			Type:  TriggerTypeEvent,
			Event: EventBudgetPeriodEnd,
		},
		ConditionLogic: "AND",
	}

	t.Run("remaining with negative threshold is rejected", func(t *testing.T) {
		rule := base
		rule.Actions = json.RawMessage(`{"savings_goal_id":"g","from_account_id":"a","amount_type":"remaining","threshold":-100}`)
		err := rule.Validate()
		require.Error(t, err, "negative threshold for remaining amount_type must be rejected")
		assert.Contains(t, err.Error(), "threshold")
	})

	t.Run("remaining with threshold=0 is accepted (full remaining)", func(t *testing.T) {
		rule := base
		rule.Actions = json.RawMessage(`{"savings_goal_id":"g","from_account_id":"a","amount_type":"remaining","threshold":0}`)
		err := rule.Validate()
		assert.NoError(t, err, "threshold=0 means transfer full remaining amount")
	})

	t.Run("remaining with positive threshold is accepted", func(t *testing.T) {
		rule := base
		rule.Actions = json.RawMessage(`{"savings_goal_id":"g","from_account_id":"a","amount_type":"remaining","threshold":100}`)
		err := rule.Validate()
		assert.NoError(t, err, "positive threshold is valid for remaining type")
	})
}

// TestValidateRolloverAction_PercentageBound verifies that a category_rollover rule with
// rollover_type:"percentage" rejects out-of-range values (≤0 or >100).
func TestValidateRolloverAction_PercentageBound(t *testing.T) {
	base := Boost{
		UserID:   "user-1",
		Name:     "Rollover %",
		BoostType: BoostTypeCategoryRollover,
		TriggerConfig: TriggerConfig{
			Type:  TriggerTypeEvent,
			Event: EventBudgetPeriodEnd,
		},
		ConditionLogic: "AND",
	}

	t.Run("rollover percentage=150 is rejected (over 100%)", func(t *testing.T) {
		pct := float64(150)
		rule := base
		rule.Actions = json.RawMessage(`{"source_budget_category_id":"cat","rollover_type":"percentage","percentage":150}`)
		_ = pct
		err := rule.Validate()
		require.Error(t, err, "rollover percentage > 100 must be rejected")
		assert.Contains(t, err.Error(), "percentage")
	})

	t.Run("rollover percentage=0 is rejected (must be positive)", func(t *testing.T) {
		rule := base
		rule.Actions = json.RawMessage(`{"source_budget_category_id":"cat","rollover_type":"percentage","percentage":0}`)
		err := rule.Validate()
		require.Error(t, err, "rollover percentage=0 must be rejected")
		assert.Contains(t, err.Error(), "percentage")
	})

	t.Run("rollover percentage=100 is accepted", func(t *testing.T) {
		rule := base
		rule.Actions = json.RawMessage(`{"source_budget_category_id":"cat","rollover_type":"percentage","percentage":100}`)
		err := rule.Validate()
		assert.NoError(t, err, "rollover percentage=100 must be valid")
	})

	t.Run("rollover percentage=50 is accepted", func(t *testing.T) {
		rule := base
		rule.Actions = json.RawMessage(`{"source_budget_category_id":"cat","rollover_type":"percentage","percentage":50}`)
		err := rule.Validate()
		assert.NoError(t, err)
	})
}

// =============================================================================
// Issue #1271 Item 2 — strict-create/fail-safe-execute validation split (A-CR1)
// =============================================================================

// TestValidate_DoesNotRequireScopeIDs verifies that Validate() (used by ExecuteBoost) does
// NOT reject a budget_category.* condition that is missing CategoryID or BudgetID. Legacy /
// partial rules must be allowed to run without bricking.
func TestValidate_DoesNotRequireScopeIDs(t *testing.T) {
	rule := Boost{
		UserID:   "user-1",
		Name:     "partial rule",
		BoostType: BoostTypeBudgetAlert,
		TriggerConfig: TriggerConfig{
			Type:  TriggerTypeEvent,
			Event: "budget_updated",
		},
		Conditions: []BoostCondition{
			{Field: "budget_category.percent_used", Operator: ConditionOperatorGte, Value: 80},
		},
		ConditionLogic: "AND",
		Actions:        json.RawMessage(`{"notification_channels":["email"],"threshold_type":"percentage","threshold_value":80}`),
	}
	err := rule.Validate()
	assert.NoError(t, err, "Validate() must not require CategoryID — that check belongs in ValidateForPersistence()")
}

// TestValidateForPersistence_RequiresCategoryID verifies that ValidateForPersistence() rejects
// a budget_category.* condition with an empty CategoryID.
func TestValidateForPersistence_RequiresCategoryID(t *testing.T) {
	rule := Boost{
		UserID:   "user-1",
		Name:     "partial rule",
		BoostType: BoostTypeBudgetAlert,
		TriggerConfig: TriggerConfig{
			Type:  TriggerTypeEvent,
			Event: "budget_updated",
		},
		Conditions: []BoostCondition{
			{Field: "budget_category.percent_used", Operator: ConditionOperatorGte, Value: 80},
		},
		ConditionLogic: "AND",
		Actions:        json.RawMessage(`{"notification_channels":["email"],"threshold_type":"percentage","threshold_value":80}`),
	}
	err := rule.ValidateForPersistence()
	require.Error(t, err, "ValidateForPersistence() must reject a missing CategoryID")
	assert.ErrorIs(t, err, ErrInvalidBoost)
	assert.Contains(t, err.Error(), "category_id")
}

// TestValidateForPersistence_RequiresBudgetID verifies that ValidateForPersistence() rejects
// a budget_category.* condition that has CategoryID but no BudgetID.
func TestValidateForPersistence_RequiresBudgetID(t *testing.T) {
	rule := Boost{
		UserID:   "user-1",
		Name:     "partial rule",
		BoostType: BoostTypeBudgetAlert,
		TriggerConfig: TriggerConfig{
			Type:  TriggerTypeEvent,
			Event: "budget_updated",
		},
		Conditions: []BoostCondition{
			{Field: "budget_category.percent_used", Operator: ConditionOperatorGte, Value: 80, CategoryID: "cat-1"},
		},
		ConditionLogic: "AND",
		Actions:        json.RawMessage(`{"notification_channels":["email"],"threshold_type":"percentage","threshold_value":80}`),
	}
	err := rule.ValidateForPersistence()
	require.Error(t, err, "ValidateForPersistence() must reject a missing BudgetID")
	assert.ErrorIs(t, err, ErrInvalidBoost)
	assert.Contains(t, err.Error(), "budget_id")
}

// TestValidateForPersistence_PassesWhenBothPresent verifies that ValidateForPersistence()
// accepts a budget_category.* condition that has both CategoryID and BudgetID set.
func TestValidateForPersistence_PassesWhenBothPresent(t *testing.T) {
	rule := Boost{
		UserID:   "user-1",
		Name:     "fully scoped rule",
		BoostType: BoostTypeBudgetAlert,
		TriggerConfig: TriggerConfig{
			Type:  TriggerTypeEvent,
			Event: "budget_updated",
		},
		Conditions: []BoostCondition{
			{Field: "budget_category.percent_used", Operator: ConditionOperatorGte, Value: 80, CategoryID: "cat-1", BudgetID: "budget-1"},
		},
		ConditionLogic: "AND",
		Actions:        json.RawMessage(`{"notification_channels":["email"],"threshold_type":"percentage","threshold_value":80}`),
	}
	err := rule.ValidateForPersistence()
	assert.NoError(t, err, "ValidateForPersistence() must accept a condition with both CategoryID and BudgetID")
}

// =============================================================================
// BoundBudgetID Tests (A-CR2)
// =============================================================================

// TestBoundBudgetID_SingleBudget verifies that BoundBudgetID returns the budget ID
// from the first budget_category.* condition that has a BudgetID set.
func TestBoundBudgetID_SingleBudget(t *testing.T) {
	rule := Boost{
		Conditions: []BoostCondition{
			{Field: "budget_category.percent_used", Operator: ConditionOperatorGte, Value: 80, CategoryID: "cat-1", BudgetID: "budget-X"},
			{Field: "budget.percent_used", Operator: ConditionOperatorGte, Value: 50},
		},
	}
	id, bound := rule.BoundBudgetID()
	assert.True(t, bound, "BoundBudgetID should return bound=true when a budget_category.* condition has a BudgetID")
	assert.Equal(t, "budget-X", id)
}

// TestBoundBudgetID_None verifies that BoundBudgetID returns ("", false) when no
// condition has a budget_category.* field with a BudgetID.
func TestBoundBudgetID_None(t *testing.T) {
	rule := Boost{
		Conditions: []BoostCondition{
			{Field: "budget.percent_used", Operator: ConditionOperatorGte, Value: 50},
		},
	}
	id, bound := rule.BoundBudgetID()
	assert.False(t, bound, "BoundBudgetID should return bound=false when no budget_category.* condition has a BudgetID")
	assert.Equal(t, "", id)
}

// TestBoundBudgetID_IgnoresNonCategoryConditions verifies that BoundBudgetID ignores
// BudgetID set on non-budget_category.* conditions (only category-scoped fields count).
func TestBoundBudgetID_IgnoresNonCategoryConditions(t *testing.T) {
	rule := Boost{
		Conditions: []BoostCondition{
			// budget.* condition with BudgetID set — should be ignored
			{Field: "budget.percent_used", Operator: ConditionOperatorGte, Value: 50, BudgetID: "budget-X"},
			// budget_category.* condition WITHOUT BudgetID
			{Field: "budget_category.percent_used", Operator: ConditionOperatorGte, Value: 80, CategoryID: "cat-1"},
		},
	}
	id, bound := rule.BoundBudgetID()
	assert.False(t, bound, "BoundBudgetID should only count budget_category.* fields; budget.* BudgetID must be ignored")
	assert.Equal(t, "", id)
}

// =============================================================================
// ValidateForPersistence Multi-Budget Tests (A-CR2)
// =============================================================================

// TestValidateForPersistence_RejectsMultipleBudgets verifies that ValidateForPersistence()
// rejects a rule whose budget_category.* conditions bind more than one distinct budget.
func TestValidateForPersistence_RejectsMultipleBudgets(t *testing.T) {
	rule := Boost{
		UserID:   "user-1",
		Name:     "multi-budget rule",
		BoostType: BoostTypeBudgetAlert,
		TriggerConfig: TriggerConfig{
			Type:  TriggerTypeEvent,
			Event: "budget_updated",
		},
		Conditions: []BoostCondition{
			{Field: "budget_category.percent_used", Operator: ConditionOperatorGte, Value: 80, CategoryID: "cat-1", BudgetID: "budget-X"},
			{Field: "budget_category.percent_used", Operator: ConditionOperatorGte, Value: 60, CategoryID: "cat-2", BudgetID: "budget-Y"},
		},
		ConditionLogic: "AND",
		Actions:        json.RawMessage(`{"notification_channels":["email"],"threshold_type":"percentage","threshold_value":80}`),
	}
	err := rule.ValidateForPersistence()
	require.Error(t, err, "ValidateForPersistence() must reject a rule binding more than one budget")
	assert.ErrorIs(t, err, ErrInvalidBoost)
}
