package services

// Issue #1109: Chain-transparency tests for boost execution service.
// Verifies that transfer suggestions created by boosts carry FromUserAccountID
// and ToUserAccountID when the boost action has those stable IDs.

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBoostExecution_TransferAction_WritesUserAccountIDs verifies that when a
// transfer_suggestion boost has FromUserAccountID/ToUserAccountID in its actions
// JSONB (backfilled by migration 000070), the created TransferSuggestion carries
// those stable IDs in the From/ToUserAccountID fields.
func TestBoostExecution_TransferAction_WritesUserAccountIDs(t *testing.T) {
	execRepo := &mockBoostExecutionRepository{}
	suggRepo := &mockTransferSuggestionRepository{}
	boostRepo := &mockBoostsRepository{}

	svc := NewBoostExecutionService(execRepo, suggRepo, boostRepo, nil)

	ctx := context.Background()
	now := time.Now()

	actionsJSON, _ := json.Marshal(domain.TransferAction{
		FromAccountID:     "old-from-account",
		ToAccountID:       "old-to-account",
		FromUserAccountID: "stable-from-user-acct",
		ToUserAccountID:   "stable-to-user-acct",
		AmountType:        "fixed",
		Amount:            100,
	})

	boost := &domain.Boost{
		ID:             "rule-chain-001",
		UserID:         "user-123",
		Name:           "Transfer to Savings",
		Enabled:        true,
		BoostType:      domain.BoostTypeTransferSuggestion,
		ConditionLogic: "AND",
		Conditions:     []domain.BoostCondition{},
		Actions:        actionsJSON,
		TriggerConfig: domain.TriggerConfig{
			Type:  domain.TriggerTypeEvent,
			Event: "transaction_created",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}

	_, err := svc.ExecuteBoost(ctx, boost, map[string]interface{}{})
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

// TestBoostExecution_TransferAction_LegacyActionNoUserAccountIDs verifies backward
// compat: a boost action without user_account_id keys creates a suggestion with
// nil From/ToUserAccountID (pre-migration data).
func TestBoostExecution_TransferAction_LegacyActionNoUserAccountIDs(t *testing.T) {
	execRepo := &mockBoostExecutionRepository{}
	suggRepo := &mockTransferSuggestionRepository{}
	boostRepo := &mockBoostsRepository{}

	svc := NewBoostExecutionService(execRepo, suggRepo, boostRepo, nil)

	ctx := context.Background()
	now := time.Now()

	actionsJSON, _ := json.Marshal(domain.TransferAction{
		FromAccountID: "from-account-legacy",
		ToAccountID:   "to-account-legacy",
		AmountType:    "fixed",
		Amount:        75,
	})

	boost := &domain.Boost{
		ID:             "rule-legacy-001",
		UserID:         "user-123",
		Name:           "Legacy Transfer Rule",
		Enabled:        true,
		BoostType:      domain.BoostTypeTransferSuggestion,
		ConditionLogic: "AND",
		Conditions:     []domain.BoostCondition{},
		Actions:        actionsJSON,
		TriggerConfig: domain.TriggerConfig{
			Type:  domain.TriggerTypeEvent,
			Event: "transaction_created",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}

	_, err := svc.ExecuteBoost(ctx, boost, map[string]interface{}{})
	require.NoError(t, err)

	require.Len(t, suggRepo.suggestions, 1)
	s := suggRepo.suggestions[0]

	assert.Equal(t, "from-account-legacy", *s.FromAccountID)
	assert.Equal(t, "to-account-legacy", *s.ToAccountID)
	assert.Nil(t, s.FromUserAccountID, "Legacy boosts should have nil FromUserAccountID")
	assert.Nil(t, s.ToUserAccountID, "Legacy boosts should have nil ToUserAccountID")
}

// TestBoostExecution_TransferAction_ChainResolver_ResolvesActiveID verifies that when
// a chainResolver is set and FromUserAccountID is non-empty, the suggestion's
// FromAccountID is updated to the active account resolved by the chain resolver.
// Issue #1109: B.2 fix.
func TestBoostExecution_TransferAction_ChainResolver_ResolvesActiveID(t *testing.T) {
	execRepo := &mockBoostExecutionRepository{}
	suggRepo := &mockTransferSuggestionRepository{}
	boostRepo := &mockBoostsRepository{}

	mockResolver := &mockAccountChainResolver{
		resolveActiveIDFunc: func(ctx context.Context, userAccountID, userID string) (string, error) {
			switch userAccountID {
			case "stable-from-uaid":
				return "active-from-account", nil
			case "stable-to-uaid":
				return "active-to-account", nil
			}
			return "", errors.New("unknown user_account_id")
		},
	}

	svc := NewBoostExecutionService(execRepo, suggRepo, boostRepo, nil)
	svc.SetChainResolver(mockResolver)

	ctx := context.Background()
	now := time.Now()

	actionsJSON, _ := json.Marshal(domain.TransferAction{
		FromAccountID:     "replaced-from-account",
		ToAccountID:       "replaced-to-account",
		FromUserAccountID: "stable-from-uaid",
		ToUserAccountID:   "stable-to-uaid",
		AmountType:        "fixed",
		Amount:            500,
	})

	boost := &domain.Boost{
		ID:             "rule-chain-resolver-001",
		UserID:         "user-123",
		Name:           "Chain Resolver Transfer",
		Enabled:        true,
		BoostType:      domain.BoostTypeTransferSuggestion,
		ConditionLogic: "AND",
		Conditions:     []domain.BoostCondition{},
		Actions:        actionsJSON,
		TriggerConfig: domain.TriggerConfig{
			Type:  domain.TriggerTypeEvent,
			Event: "transaction_created",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}

	_, err := svc.ExecuteBoost(ctx, boost, map[string]interface{}{})
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

// TestBoostExecution_TransferAction_ChainResolver_AmbiguousChain_UsesPinnedAccount pins #1569
// R11 T2's site-2 decision (fix round 1: an earlier revision skipped the suggestion here, which
// was a monotonicity bug — see resolveAndVerifyUserAccount's doc comment). When the chain has
// more than one live account (ErrAmbiguousActiveAccount), the suggestion still gets created,
// using the rule's pinned FromAccountID/ToAccountID — the SAME fallback behaviour as
// ErrNoActiveAccount, and the same kind of fallback the savings-goal destination site (T2 site 3)
// uses. The resolver still refuses to guess WHICH live sibling to use; it does not refuse to act.
func TestBoostExecution_TransferAction_ChainResolver_AmbiguousChain_UsesPinnedAccount(t *testing.T) {
	execRepo := &mockBoostExecutionRepository{}
	suggRepo := &mockTransferSuggestionRepository{}
	boostRepo := &mockBoostsRepository{}

	mockResolver := &mockAccountChainResolver{
		resolveActiveIDFunc: func(ctx context.Context, userAccountID, userID string) (string, error) {
			return "", ErrAmbiguousActiveAccount
		},
	}

	svc := NewBoostExecutionService(execRepo, suggRepo, boostRepo, nil)
	svc.SetChainResolver(mockResolver)

	ctx := context.Background()
	now := time.Now()

	actionsJSON, _ := json.Marshal(domain.TransferAction{
		FromAccountID:     "replaced-from-account",
		ToAccountID:       "replaced-to-account",
		FromUserAccountID: "stable-from-uaid-ambiguous",
		ToUserAccountID:   "stable-to-uaid-ambiguous",
		AmountType:        "fixed",
		Amount:            500,
	})

	boost := &domain.Boost{
		ID:             "rule-chain-resolver-ambiguous",
		UserID:         "user-123",
		Name:           "Ambiguous Chain Transfer",
		Enabled:        true,
		BoostType:      domain.BoostTypeTransferSuggestion,
		ConditionLogic: "AND",
		Conditions:     []domain.BoostCondition{},
		Actions:        actionsJSON,
		TriggerConfig: domain.TriggerConfig{
			Type:  domain.TriggerTypeEvent,
			Event: "transaction_created",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}

	_, err := svc.ExecuteBoost(ctx, boost, map[string]interface{}{})
	require.NoError(t, err)

	require.Len(t, suggRepo.suggestions, 1,
		"an ambiguous chain must not block the suggestion — it is not a guess, it is the rule's already-pinned account")
	s := suggRepo.suggestions[0]
	require.NotNil(t, s.FromAccountID)
	assert.Equal(t, "replaced-from-account", *s.FromAccountID,
		"an ambiguous chain must fall back to the pinned FromAccountID, the same outcome as ErrNoActiveAccount")
	require.NotNil(t, s.ToAccountID)
	assert.Equal(t, "replaced-to-account", *s.ToAccountID,
		"an ambiguous chain must fall back to the pinned ToAccountID, not guess between the two live siblings")
}

// TestBoostExecution_TransferAction_ChainResolver_NilFallback verifies that when
// chainResolver is nil, FromAccountID falls back to the raw value in the action.
func TestBoostExecution_TransferAction_ChainResolver_NilFallback(t *testing.T) {
	execRepo := &mockBoostExecutionRepository{}
	suggRepo := &mockTransferSuggestionRepository{}
	boostRepo := &mockBoostsRepository{}

	svc := NewBoostExecutionService(execRepo, suggRepo, boostRepo, nil)
	// No SetChainResolver call — chainResolver stays nil.

	ctx := context.Background()
	now := time.Now()

	actionsJSON, _ := json.Marshal(domain.TransferAction{
		FromAccountID:     "raw-from-account",
		ToAccountID:       "raw-to-account",
		FromUserAccountID: "stable-from-uaid",
		ToUserAccountID:   "stable-to-uaid",
		AmountType:        "fixed",
		Amount:            100,
	})

	boost := &domain.Boost{
		ID:             "rule-chain-nil-001",
		UserID:         "user-123",
		Name:           "Nil Resolver Fallback",
		Enabled:        true,
		BoostType:      domain.BoostTypeTransferSuggestion,
		ConditionLogic: "AND",
		Conditions:     []domain.BoostCondition{},
		Actions:        actionsJSON,
		TriggerConfig: domain.TriggerConfig{
			Type:  domain.TriggerTypeEvent,
			Event: "transaction_created",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}

	_, err := svc.ExecuteBoost(ctx, boost, map[string]interface{}{})
	require.NoError(t, err)

	require.Len(t, suggRepo.suggestions, 1)
	s := suggRepo.suggestions[0]

	// Without resolver, raw account IDs are used.
	require.NotNil(t, s.FromAccountID)
	assert.Equal(t, "raw-from-account", *s.FromAccountID, "fallback: raw FromAccountID")
	require.NotNil(t, s.ToAccountID)
	assert.Equal(t, "raw-to-account", *s.ToAccountID, "fallback: raw ToAccountID")
}

// TestBoostExecution_TransferAction_ChainResolver_EmptyUserAccountID verifies that when
// FromUserAccountID is empty, the raw account ID is used regardless of resolver.
func TestBoostExecution_TransferAction_ChainResolver_EmptyUserAccountID(t *testing.T) {
	execRepo := &mockBoostExecutionRepository{}
	suggRepo := &mockTransferSuggestionRepository{}
	boostRepo := &mockBoostsRepository{}

	mockResolver := &mockAccountChainResolver{
		resolveActiveIDFunc: func(_ context.Context, _, _ string) (string, error) {
			return "should-not-be-called", nil
		},
	}

	svc := NewBoostExecutionService(execRepo, suggRepo, boostRepo, nil)
	svc.SetChainResolver(mockResolver)

	ctx := context.Background()
	now := time.Now()

	// No from_user_account_id / to_user_account_id in action
	actionsJSON, _ := json.Marshal(domain.TransferAction{
		FromAccountID: "raw-from-account-legacy",
		ToAccountID:   "raw-to-account-legacy",
		AmountType:    "fixed",
		Amount:        75,
	})

	boost := &domain.Boost{
		ID:             "rule-legacy-empty-uaid",
		UserID:         "user-123",
		Name:           "Legacy No UAID",
		Enabled:        true,
		BoostType:      domain.BoostTypeTransferSuggestion,
		ConditionLogic: "AND",
		Conditions:     []domain.BoostCondition{},
		Actions:        actionsJSON,
		TriggerConfig: domain.TriggerConfig{
			Type:  domain.TriggerTypeEvent,
			Event: "transaction_created",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}

	_, err := svc.ExecuteBoost(ctx, boost, map[string]interface{}{})
	require.NoError(t, err)

	require.Len(t, suggRepo.suggestions, 1)
	s := suggRepo.suggestions[0]
	require.NotNil(t, s.FromAccountID)
	assert.Equal(t, "raw-from-account-legacy", *s.FromAccountID)
	require.NotNil(t, s.ToAccountID)
	assert.Equal(t, "raw-to-account-legacy", *s.ToAccountID)
}

// TestBoostExecution_SimulateAction_IncludesFromUserAccountID verifies that simulateAction
// adds from_user_account_id to the result when it is set. Issue #1109: B.2 fix.
func TestBoostExecution_SimulateAction_IncludesFromUserAccountID(t *testing.T) {
	execRepo := &mockBoostExecutionRepository{}
	suggRepo := &mockTransferSuggestionRepository{}
	boostRepo := &mockBoostsRepository{}

	svc := NewBoostExecutionService(execRepo, suggRepo, boostRepo, nil)

	ctx := context.Background()
	now := time.Now()

	actionsJSON, _ := json.Marshal(domain.TransferAction{
		FromAccountID:     "raw-from",
		ToAccountID:       "raw-to",
		FromUserAccountID: "stable-from-uaid",
		AmountType:        "fixed",
		Amount:            150,
	})

	boost := &domain.Boost{
		ID:             "rule-simulate-uaid",
		UserID:         "user-sim",
		Name:           "Simulate With UAID",
		Enabled:        true,
		BoostType:      domain.BoostTypeTransferSuggestion,
		ConditionLogic: "AND",
		Conditions:     []domain.BoostCondition{},
		Actions:        actionsJSON,
		TriggerConfig: domain.TriggerConfig{
			Type:  domain.TriggerTypeEvent,
			Event: "transaction_created",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}

	result, err := svc.TestBoost(ctx, boost, map[string]interface{}{})
	require.NoError(t, err)
	require.NotNil(t, result)

	fromUAID, ok := result.SimulatedAction["from_user_account_id"]
	assert.True(t, ok, "from_user_account_id should be present in simulated action")
	assert.Equal(t, "stable-from-uaid", fromUAID)
}

// mockAccountChainResolver is a lightweight mock for AccountChainResolver used in
// boost_execution_chain_test.go.
type mockAccountChainResolver struct {
	resolveActiveIDFunc func(ctx context.Context, userAccountID, userID string) (string, error)
}

func (m *mockAccountChainResolver) ListActiveByUser(ctx context.Context, userID string) ([]domain.Account, error) {
	return nil, nil
}

func (m *mockAccountChainResolver) ResolveActiveID(ctx context.Context, userAccountID, userID string) (string, error) {
	if m.resolveActiveIDFunc != nil {
		return m.resolveActiveIDFunc(ctx, userAccountID, userID)
	}
	return "", errors.New("ResolveActiveID not configured")
}

func (m *mockAccountChainResolver) ChainAccountIDs(ctx context.Context, userAccountID string) ([]string, error) {
	return nil, nil
}

func TestBoostExecution_TransferAction_ChainResolver_ResolvesActiveID_Depth2(t *testing.T) {
	// Depth-2 chain: acct_old -> acct_mid -> acct_current
	// Rule actions carry the stable user_account_id. The chain resolver must return
	// the current-generation account ID even after two re-links.
	execRepo := &mockBoostExecutionRepository{}
	suggRepo := &mockTransferSuggestionRepository{}
	boostRepo := &mockBoostsRepository{}

	acctFromCurrent := "active-from-account-gen3"
	acctToCurrent := "active-to-account-gen3"

	mockResolver := &mockAccountChainResolver{
		resolveActiveIDFunc: func(ctx context.Context, userAccountID, userID string) (string, error) {
			switch userAccountID {
			case "stable-from-uaid-depth2":
				return acctFromCurrent, nil
			case "stable-to-uaid-depth2":
				return acctToCurrent, nil
			}
			return "", errors.New("unknown user_account_id")
		},
	}

	svc := NewBoostExecutionService(execRepo, suggRepo, boostRepo, nil)
	svc.SetChainResolver(mockResolver)

	ctx := context.Background()
	now := time.Now()

	actionsJSON, _ := json.Marshal(domain.TransferAction{
		FromAccountID:     "replaced-from-gen1",
		ToAccountID:       "replaced-to-gen1",
		FromUserAccountID: "stable-from-uaid-depth2",
		ToUserAccountID:   "stable-to-uaid-depth2",
		AmountType:        "fixed",
		Amount:            1000,
	})

	boost := &domain.Boost{
		ID:             "rule-depth2-chain-001",
		UserID:         "user-depth2",
		Name:           "Depth-2 Chain Transfer",
		Enabled:        true,
		BoostType:      domain.BoostTypeTransferSuggestion,
		ConditionLogic: "AND",
		Conditions:     []domain.BoostCondition{},
		Actions:        actionsJSON,
		TriggerConfig: domain.TriggerConfig{
			Type:  domain.TriggerTypeEvent,
			Event: "transaction_created",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}

	_, err := svc.ExecuteBoost(ctx, boost, map[string]interface{}{})
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
