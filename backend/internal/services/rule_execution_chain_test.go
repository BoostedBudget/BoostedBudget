package services

// Issue #1109: Chain-transparency tests for rule execution service.
// Verifies that transfer suggestions created by rules carry FromUserAccountID
// and ToUserAccountID when the rule action has those stable IDs.

import (
	"errors"
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

// TestRuleExecution_TransferAction_ChainResolver_ResolvesActiveID verifies that when
// a chainResolver is set and FromUserAccountID is non-empty, the suggestion's
// FromAccountID is updated to the active account resolved by the chain resolver.
// Issue #1109: B.2 fix.
func TestRuleExecution_TransferAction_ChainResolver_ResolvesActiveID(t *testing.T) {
	execRepo := &mockRuleExecutionRepository{}
	suggRepo := &mockTransferSuggestionRepository{}
	ruleRepo := &mockAutomationRulesRepository{}

	mockResolver := &mockAccountChainResolver{
		resolveActiveIDFunc: func(ctx context.Context, userAccountID string) (string, error) {
			switch userAccountID {
			case "stable-from-uaid":
				return "active-from-account", nil
			case "stable-to-uaid":
				return "active-to-account", nil
			}
			return "", errors.New("unknown user_account_id")
		},
	}

	svc := NewRuleExecutionService(execRepo, suggRepo, ruleRepo, nil)
	svc.SetChainResolver(mockResolver)

	ctx := context.Background()
	now := time.Now()

	actions := map[string]interface{}{
		"transfer": map[string]interface{}{
			"from_account_id":      "replaced-from-account",
			"to_account_id":        "replaced-to-account",
			"from_user_account_id": "stable-from-uaid",
			"to_user_account_id":   "stable-to-uaid",
			"amount_type":          "fixed",
			"amount":               float64(500),
		},
	}
	actionsJSON, _ := json.Marshal(actions)

	rule := &domain.AutomationRule{
		ID:             "rule-chain-resolver-001",
		UserID:         "user-123",
		Name:           "Chain Resolver Transfer",
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

	// Chain resolver should have replaced the raw account IDs with active ones.
	require.NotNil(t, s.FromAccountID)
	assert.Equal(t, "active-from-account", *s.FromAccountID, "FromAccountID should be resolved to active")
	require.NotNil(t, s.ToAccountID)
	assert.Equal(t, "active-to-account", *s.ToAccountID, "ToAccountID should be resolved to active")
	// Stable IDs should still be propagated.
	require.NotNil(t, s.FromUserAccountID)
	assert.Equal(t, "stable-from-uaid", *s.FromUserAccountID)
	require.NotNil(t, s.ToUserAccountID)
	assert.Equal(t, "stable-to-uaid", *s.ToUserAccountID)
}

// TestRuleExecution_TransferAction_ChainResolver_NilFallback verifies that when
// chainResolver is nil, FromAccountID falls back to the raw value in the action.
func TestRuleExecution_TransferAction_ChainResolver_NilFallback(t *testing.T) {
	execRepo := &mockRuleExecutionRepository{}
	suggRepo := &mockTransferSuggestionRepository{}
	ruleRepo := &mockAutomationRulesRepository{}

	svc := NewRuleExecutionService(execRepo, suggRepo, ruleRepo, nil)
	// No SetChainResolver call — chainResolver stays nil.

	ctx := context.Background()
	now := time.Now()

	actions := map[string]interface{}{
		"transfer": map[string]interface{}{
			"from_account_id":      "raw-from-account",
			"to_account_id":        "raw-to-account",
			"from_user_account_id": "stable-from-uaid",
			"to_user_account_id":   "stable-to-uaid",
			"amount_type":          "fixed",
			"amount":               float64(100),
		},
	}
	actionsJSON, _ := json.Marshal(actions)

	rule := &domain.AutomationRule{
		ID:             "rule-chain-nil-001",
		UserID:         "user-123",
		Name:           "Nil Resolver Fallback",
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

	// Without resolver, raw account IDs are used.
	require.NotNil(t, s.FromAccountID)
	assert.Equal(t, "raw-from-account", *s.FromAccountID, "fallback: raw FromAccountID")
	require.NotNil(t, s.ToAccountID)
	assert.Equal(t, "raw-to-account", *s.ToAccountID, "fallback: raw ToAccountID")
}

// TestRuleExecution_TransferAction_ChainResolver_EmptyUserAccountID verifies that when
// FromUserAccountID is empty, the raw account ID is used regardless of resolver.
func TestRuleExecution_TransferAction_ChainResolver_EmptyUserAccountID(t *testing.T) {
	execRepo := &mockRuleExecutionRepository{}
	suggRepo := &mockTransferSuggestionRepository{}
	ruleRepo := &mockAutomationRulesRepository{}

	mockResolver := &mockAccountChainResolver{
		resolveActiveIDFunc: func(_ context.Context, _ string) (string, error) {
			return "should-not-be-called", nil
		},
	}

	svc := NewRuleExecutionService(execRepo, suggRepo, ruleRepo, nil)
	svc.SetChainResolver(mockResolver)

	ctx := context.Background()
	now := time.Now()

	// No from_user_account_id / to_user_account_id in action
	actions := map[string]interface{}{
		"transfer": map[string]interface{}{
			"from_account_id": "raw-from-account-legacy",
			"to_account_id":   "raw-to-account-legacy",
			"amount_type":     "fixed",
			"amount":          float64(75),
		},
	}
	actionsJSON, _ := json.Marshal(actions)

	rule := &domain.AutomationRule{
		ID:             "rule-legacy-empty-uaid",
		UserID:         "user-123",
		Name:           "Legacy No UAID",
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
	require.NotNil(t, s.FromAccountID)
	assert.Equal(t, "raw-from-account-legacy", *s.FromAccountID)
	require.NotNil(t, s.ToAccountID)
	assert.Equal(t, "raw-to-account-legacy", *s.ToAccountID)
}

// TestRuleExecution_SimulateAction_IncludesFromUserAccountID verifies that simulateAction
// adds from_user_account_id to the result when it is set. Issue #1109: B.2 fix.
func TestRuleExecution_SimulateAction_IncludesFromUserAccountID(t *testing.T) {
	execRepo := &mockRuleExecutionRepository{}
	suggRepo := &mockTransferSuggestionRepository{}
	ruleRepo := &mockAutomationRulesRepository{}

	svc := NewRuleExecutionService(execRepo, suggRepo, ruleRepo, nil)

	ctx := context.Background()
	now := time.Now()

	actions := map[string]interface{}{
		"transfer": map[string]interface{}{
			"from_account_id":      "raw-from",
			"to_account_id":        "raw-to",
			"from_user_account_id": "stable-from-uaid",
			"amount_type":          "fixed",
			"amount":               float64(150),
		},
	}
	actionsJSON, _ := json.Marshal(actions)

	rule := &domain.AutomationRule{
		ID:             "rule-simulate-uaid",
		UserID:         "user-sim",
		Name:           "Simulate With UAID",
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

	result, err := svc.TestRule(ctx, rule, map[string]interface{}{})
	require.NoError(t, err)
	require.NotNil(t, result)

	fromUAID, ok := result.SimulatedAction["from_user_account_id"]
	assert.True(t, ok, "from_user_account_id should be present in simulated action")
	assert.Equal(t, "stable-from-uaid", fromUAID)
}

// mockAccountChainResolver is a lightweight mock for AccountChainResolver used in
// rule_execution_chain_test.go.
type mockAccountChainResolver struct {
	resolveActiveIDFunc func(ctx context.Context, userAccountID string) (string, error)
}

func (m *mockAccountChainResolver) ListActiveByUser(ctx context.Context, userID string) ([]domain.Account, error) {
	return nil, nil
}

func (m *mockAccountChainResolver) ResolveActiveID(ctx context.Context, userAccountID string) (string, error) {
	if m.resolveActiveIDFunc != nil {
		return m.resolveActiveIDFunc(ctx, userAccountID)
	}
	return "", errors.New("ResolveActiveID not configured")
}

func (m *mockAccountChainResolver) ChainAccountIDs(ctx context.Context, userAccountID string) ([]string, error) {
	return nil, nil
}

func TestRuleExecution_TransferAction_ChainResolver_ResolvesActiveID_Depth2(t *testing.T) {
	// Depth-2 chain: acct_old -> acct_mid -> acct_current
	// Rule actions carry the stable user_account_id. The chain resolver must return
	// the current-generation account ID even after two re-links.
	execRepo := &mockRuleExecutionRepository{}
	suggRepo := &mockTransferSuggestionRepository{}
	ruleRepo := &mockAutomationRulesRepository{}

	acctFromCurrent := "active-from-account-gen3"
	acctToCurrent := "active-to-account-gen3"

	mockResolver := &mockAccountChainResolver{
		resolveActiveIDFunc: func(ctx context.Context, userAccountID string) (string, error) {
			switch userAccountID {
			case "stable-from-uaid-depth2":
				return acctFromCurrent, nil
			case "stable-to-uaid-depth2":
				return acctToCurrent, nil
			}
			return "", errors.New("unknown user_account_id")
		},
	}

	svc := NewRuleExecutionService(execRepo, suggRepo, ruleRepo, nil)
	svc.SetChainResolver(mockResolver)

	ctx := context.Background()
	now := time.Now()

	actions := map[string]interface{}{
		"transfer": map[string]interface{}{
			"from_account_id":      "replaced-from-gen1",
			"to_account_id":        "replaced-to-gen1",
			"from_user_account_id": "stable-from-uaid-depth2",
			"to_user_account_id":   "stable-to-uaid-depth2",
			"amount_type":          "fixed",
			"amount":               float64(1000),
		},
	}
	actionsJSON, _ := json.Marshal(actions)

	rule := &domain.AutomationRule{
		ID:             "rule-depth2-chain-001",
		UserID:         "user-depth2",
		Name:           "Depth-2 Chain Transfer",
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

	require.NotNil(t, s.FromAccountID)
	assert.Equal(t, acctFromCurrent, *s.FromAccountID,
		"depth-2 chain: FromAccountID should resolve to current active account")
	require.NotNil(t, s.ToAccountID)
	assert.Equal(t, acctToCurrent, *s.ToAccountID,
		"depth-2 chain: ToAccountID should resolve to current active account")
	require.NotNil(t, s.FromUserAccountID)
	assert.Equal(t, "stable-from-uaid-depth2", *s.FromUserAccountID)
	require.NotNil(t, s.ToUserAccountID)
	assert.Equal(t, "stable-to-uaid-depth2", *s.ToUserAccountID)
}
