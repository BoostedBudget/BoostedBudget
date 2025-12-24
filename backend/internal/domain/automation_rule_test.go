package domain

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// RuleType Tests
// =============================================================================

func TestRuleType_IsValid(t *testing.T) {
	tests := []struct {
		name     string
		ruleType RuleType
		want     bool
	}{
		{"transfer_suggestion is valid", RuleTypeTransferSuggestion, true},
		{"budget_alert is valid", RuleTypeBudgetAlert, true},
		{"category_rollover is valid", RuleTypeCategoryRollover, true},
		{"empty string is invalid", RuleType(""), false},
		{"unknown type is invalid", RuleType("unknown"), false},
		{"case sensitive - uppercase is invalid", RuleType("TRANSFER_SUGGESTION"), false},
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
// AutomationRuleCondition Tests
// =============================================================================

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
				Event: "transaction_created",
				Filters: map[string]interface{}{
					"min_amount": 100,
				},
			},
			wantErr: false,
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
// AutomationRule Validation Tests
// =============================================================================

func TestAutomationRule_Validate_RequiredFields(t *testing.T) {
	tests := []struct {
		name    string
		rule    AutomationRule
		wantErr bool
		errMsg  string
	}{
		{
			name:    "missing user_id",
			rule:    AutomationRule{Name: "Test Rule", RuleType: RuleTypeTransferSuggestion},
			wantErr: true,
			errMsg:  "user_id is required",
		},
		{
			name:    "missing name",
			rule:    AutomationRule{UserID: "user-123", RuleType: RuleTypeTransferSuggestion},
			wantErr: true,
			errMsg:  "name is required",
		},
		{
			name:    "name too long",
			rule:    AutomationRule{UserID: "user-123", Name: string(make([]byte, 101)), RuleType: RuleTypeTransferSuggestion},
			wantErr: true,
			errMsg:  "name must be 100 characters or less",
		},
		{
			name:    "missing rule_type",
			rule:    AutomationRule{UserID: "user-123", Name: "Test Rule"},
			wantErr: true,
			errMsg:  "invalid rule_type",
		},
		{
			name:    "invalid rule_type",
			rule:    AutomationRule{UserID: "user-123", Name: "Test Rule", RuleType: RuleType("invalid")},
			wantErr: true,
			errMsg:  "invalid rule_type",
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
	baseRule := func() AutomationRule {
		return AutomationRule{
			UserID:   "user-123",
			Name:     "Test Rule",
			RuleType: RuleTypeTransferSuggestion,
			TriggerConfig: TriggerConfig{
				Type:  TriggerTypeEvent,
				Event: "budget_updated",
			},
			Conditions: []AutomationRuleCondition{
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
	validRule := AutomationRule{
		UserID:   "user-123",
		Name:     "Excess Savings Transfer",
		RuleType: RuleTypeTransferSuggestion,
		TriggerConfig: TriggerConfig{
			Type:  TriggerTypeEvent,
			Event: "balance_changed",
		},
		Conditions: []AutomationRuleCondition{
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

	t.Run("transfer suggestion requires positive amount", func(t *testing.T) {
		rule := validRule
		rule.Actions = json.RawMessage(`{"from_account_id":"acc-1","to_account_id":"acc-2","amount":-100}`)
		err := rule.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "amount must be positive")
	})
}

func TestAutomationRule_Validate_BudgetAlertRule(t *testing.T) {
	validRule := AutomationRule{
		UserID:   "user-123",
		Name:     "Budget Warning",
		RuleType: RuleTypeBudgetAlert,
		TriggerConfig: TriggerConfig{
			Type:  TriggerTypeEvent,
			Event: "budget_updated",
		},
		Conditions: []AutomationRuleCondition{
			{Field: "budget_category.percent_used", Operator: ConditionOperatorGte, Value: 80},
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
	validRule := AutomationRule{
		UserID:   "user-123",
		Name:     "Savings Rollover",
		RuleType: RuleTypeCategoryRollover,
		TriggerConfig: TriggerConfig{
			Type:  TriggerTypeEvent,
			Event: "budget_period_end",
		},
		Conditions: []AutomationRuleCondition{
			{Field: "budget_category.remaining", Operator: ConditionOperatorGt, Value: 0},
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
	rule := AutomationRule{
		RuleType: RuleTypeTransferSuggestion,
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
	rule := AutomationRule{
		RuleType: RuleTypeBudgetAlert,
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
	rule := AutomationRule{
		RuleType: RuleTypeCategoryRollover,
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
// RuleExecution Tests
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
	execution := &RuleExecution{
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
	execution := &RuleExecution{
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
		{"rule is valid", SuggestionSourceRule, true},
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
		Source:       SuggestionSourceRule,
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

// =============================================================================
// Error Constants Tests
// =============================================================================

func TestErrorConstants(t *testing.T) {
	// Verify error constants are defined and unique
	errors := []error{
		ErrAutomationRuleNotFound,
		ErrAutomationRuleAlreadyExists,
		ErrAutomationRuleLimitExceeded,
		ErrExecutionLimitExceeded,
		ErrExecutionIntervalNotMet,
		ErrEventTriggersNotAllowed,
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
