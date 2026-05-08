package services

// Issue #1109: Chain-transparency tests for rule execution service.
// Verifies that transfer suggestions created by rules carry FromUserAccountID
// and ToUserAccountID when the rule action has those stable IDs.

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRuleExecution_TransferAction_WritesUserAccountIDs verifies that when a
// transfer_suggestion rule has FromUserAccountID/ToUserAccountID in its actions
// JSONB (backfilled by migration 000070), the created TransferSuggestion carries
// those stable IDs in the From/ToUserAccountID fields.
func TestRuleExecution_TransferAction_WritesUserAccountIDs(t *testing.T) {
	execRepo := &mockRuleExecutionRepository{}
	suggRepo := &mockTransferSuggestionRepository{}
	ruleRepo := &mockAutomationRulesRepository{}

	svc := NewRuleExecutionService(execRepo, suggRepo, ruleRepo, nil)

	ctx := context.Background()
	now := time.Now()

	actions := map[string]interface{}{
		"transfer": map[string]interface{}{
			"from_account_id":      "old-from-account",
			"to_account_id":        "old-to-account",
			"from_user_account_id": "stable-from-user-acct",
			"to_user_account_id":   "stable-to-user-acct",
			"amount_type":          "fixed",
			"amount":               float64(100),
		},
	}
	actionsJSON, _ := json.Marshal(actions)

	rule := &domain.AutomationRule{
		ID:             "rule-chain-001",
		UserID:         "user-123",
		Name:           "Transfer to Savings",
		Enabled:        true,
		RuleType:       domain.RuleTypeTransferSuggestion,
		ConditionLogic: "AND",
		Conditions:     []domain.AutomationRuleCondition{},
		Actions:        actionsJSON,
		TriggerConfig: domain.TriggerConfig{
			Type:  domain.TriggerTypeEvent,
			Event: "transaction_created",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}


	_, err := svc.ExecuteRule(ctx, rule, map[string]interface{}{})
	require.NoError(t, err)

	require.Len(t, suggRepo.suggestions, 1)
	s := suggRepo.suggestions[0]

	assert.Equal(t, "old-from-account", *s.FromAccountID, "FromAccountID should be preserved")
	assert.Equal(t, "old-to-account", *s.ToAccountID, "ToAccountID should be preserved")
	require.NotNil(t, s.FromUserAccountID, "FromUserAccountID should be set")
	assert.Equal(t, "stable-from-user-acct", *s.FromUserAccountID)
	require.NotNil(t, s.ToUserAccountID, "ToUserAccountID should be set")
	assert.Equal(t, "stable-to-user-acct", *s.ToUserAccountID)
}

// TestRuleExecution_TransferAction_LegacyActionNoUserAccountIDs verifies backward
// compat: a rule action without user_account_id keys creates a suggestion with
// nil From/ToUserAccountID (pre-migration data).
func TestRuleExecution_TransferAction_LegacyActionNoUserAccountIDs(t *testing.T) {
	execRepo := &mockRuleExecutionRepository{}
	suggRepo := &mockTransferSuggestionRepository{}
	ruleRepo := &mockAutomationRulesRepository{}

	svc := NewRuleExecutionService(execRepo, suggRepo, ruleRepo, nil)

	ctx := context.Background()
	now := time.Now()

	actions := map[string]interface{}{
		"transfer": map[string]interface{}{
			"from_account_id": "from-account-legacy",
			"to_account_id":   "to-account-legacy",
			"amount_type":     "fixed",
			"amount":          float64(75),
		},
	}
	actionsJSON, _ := json.Marshal(actions)

	rule := &domain.AutomationRule{
		ID:             "rule-legacy-001",
		UserID:         "user-123",
		Name:           "Legacy Transfer Rule",
		Enabled:        true,
		RuleType:       domain.RuleTypeTransferSuggestion,
		ConditionLogic: "AND",
		Conditions:     []domain.AutomationRuleCondition{},
		Actions:        actionsJSON,
		TriggerConfig: domain.TriggerConfig{
			Type:  domain.TriggerTypeEvent,
			Event: "transaction_created",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}


	_, err := svc.ExecuteRule(ctx, rule, map[string]interface{}{})
	require.NoError(t, err)

	require.Len(t, suggRepo.suggestions, 1)
	s := suggRepo.suggestions[0]

	assert.Equal(t, "from-account-legacy", *s.FromAccountID)
	assert.Equal(t, "to-account-legacy", *s.ToAccountID)
	assert.Nil(t, s.FromUserAccountID, "Legacy rules should have nil FromUserAccountID")
	assert.Nil(t, s.ToUserAccountID, "Legacy rules should have nil ToUserAccountID")
}
