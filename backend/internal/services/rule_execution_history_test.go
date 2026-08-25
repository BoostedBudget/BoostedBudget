package services

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// Issue #1569 Task 5 (#1572) — the automation audit trail gets a writer.
//
// `rule_execution_history` has had columns, constraints, a backfill and two FK migrations since
// #1569 tasks 7 and 14, and no production writer at all. Every row in it today was put there by a
// test fixture. These tests pin the writer.
//
// #1572's stated blocker — "`rules.Engine` is never constructed in production" — is FALSE. The type
// is `rules.RuleEngine`; it is constructed at rule_application_service.go:67
// (`rules.NewRuleEngine()`), reached in production from cmd/server/main.go:774 and driven on the
// live sync path from transaction_sync_service.go:1027 (ApplyRulesToBatch) and :1058
// (EmitBatchSideEffects). Verified by grep, not by reading the ticket.

// mockRuleExecutionHistoryRepository records the rows the writer would persist.
type mockRuleExecutionHistoryRepository struct {
	mock.Mock
}

func (m *mockRuleExecutionHistoryRepository) Create(ctx context.Context, execution *domain.RuleExecution) error {
	args := m.Called(ctx, execution)
	return args.Error(0)
}

func (m *mockRuleExecutionHistoryRepository) GetByUserID(ctx context.Context, userID string, limit, offset int) ([]*domain.RuleExecution, int, error) {
	args := m.Called(ctx, userID, limit, offset)
	if args.Get(0) == nil {
		return nil, args.Int(1), args.Error(2)
	}
	return args.Get(0).([]*domain.RuleExecution), args.Int(1), args.Error(2)
}

// categoryRule is the shared fixture: a PFC category rule with no tags.
func categoryRule(userID string) *domain.CategorizationRule {
	return &domain.CategorizationRule{
		ID:               "rule-cat",
		UserID:           userID,
		Name:             "Coffee is Food",
		Enabled:          true,
		Priority:         10,
		Conditions:       []domain.RuleCondition{{Type: domain.ConditionMerchantContains, Value: "coffee"}},
		ConditionLogic:   "AND",
		TargetKind:       strPtr("pfc"),
		TargetPFCPrimary: strPtr("FOOD_AND_DRINK"),
		TagBehavior:      "add",
		Source:           "manual",
	}
}

func tagRule(userID string) *domain.CategorizationRule {
	return &domain.CategorizationRule{
		ID:             "rule-tag",
		UserID:         userID,
		Name:           "Tag my coffee",
		Enabled:        true,
		Priority:       5,
		Conditions:     []domain.RuleCondition{{Type: domain.ConditionMerchantContains, Value: "coffee"}},
		ConditionLogic: "AND",
		TargetTags:     []string{"caffeine"},
		TagBehavior:    "add",
		Source:         "manual",
	}
}

// ---------------------------------------------------------------------------------------------
// ApplyRulesToBatch must carry enough to write history WITHOUT a second query.
// ---------------------------------------------------------------------------------------------

// TestApplyRulesToBatch_CarriesTheRuleNameAndDefinitionForEachChange pins trap 3 from the brief:
// BatchRuleApplication used to carry only IDs, so a writer downstream would have had to re-read
// categorization_rules — by which time a rename could already have happened. The
// *domain.CategorizationRule is in scope inside the loop, so this is struct widening.
func TestApplyRulesToBatch_CarriesTheRuleNameAndDefinitionForEachChange(t *testing.T) {
	mockRepo := new(mockRuleApplicationRepository)
	service := NewRuleApplicationService(mockRepo)
	ctx := context.Background()
	userID := "user-1572"

	mockRepo.On("GetEnabledByUserID", ctx, userID).
		Return([]*domain.CategorizationRule{categoryRule(userID)}, nil)

	merchant := "Blue Bottle Coffee"
	txns := []domain.Transaction{{ID: "tx-1", UserID: userID, MerchantName: &merchant}}

	apps, err := service.ApplyRulesToBatch(ctx, txns, userID)
	require.NoError(t, err)
	require.Len(t, apps, 1)
	require.Len(t, apps[0].Executions, 1, "one rule changed the category, so one execution record")

	rec := apps[0].Executions[0]
	assert.Equal(t, "rule-cat", rec.RuleID)
	assert.Equal(t, "Coffee is Food", rec.RuleName, "the name AS IT WAS when the rule ran")
	assert.Equal(t, domain.RuleExecutionActionCategory, rec.ActionType)

	var snap map[string]any
	require.NoError(t, json.Unmarshal(rec.DefinitionSnapshot, &snap))
	assert.Equal(t, "rule-cat", snap["rule_id"])
	assert.Equal(t, "Coffee is Food", snap["name"])
	assert.Equal(t, "pfc", snap["target_kind"])
	assert.Equal(t, "FOOD_AND_DRINK", snap["target_pfc_primary"])
	assert.Equal(t, "rule_application_service", snap["captured_by"],
		"mirrors buildBoostDefinitionSnapshot's captured_by convention")
	assert.NotEmpty(t, snap["captured_at"])
	assert.Contains(t, snap, "conditions", "why it fired must be in the snapshot")

	// Deliberately excluded, exactly as buildBoostDefinitionSnapshot excludes them.
	for _, excluded := range []string{"description", "match_count", "override_count", "last_matched_at", "created_at", "updated_at"} {
		assert.NotContains(t, snap, excluded)
	}
}

// TestApplyRulesToBatch_SnapshotSurvivesALaterRename is the invariant boost_executions states in
// its own words: "the point of a snapshot is that a later rename, edit or purge does not rewrite
// what history says happened."
func TestApplyRulesToBatch_SnapshotSurvivesALaterRename(t *testing.T) {
	mockRepo := new(mockRuleApplicationRepository)
	service := NewRuleApplicationService(mockRepo)
	ctx := context.Background()
	userID := "user-1572"

	rule := categoryRule(userID)
	mockRepo.On("GetEnabledByUserID", ctx, userID).
		Return([]*domain.CategorizationRule{rule}, nil)

	merchant := "Blue Bottle Coffee"
	txns := []domain.Transaction{{ID: "tx-1", UserID: userID, MerchantName: &merchant}}

	apps, err := service.ApplyRulesToBatch(ctx, txns, userID)
	require.NoError(t, err)
	require.Len(t, apps, 1)
	require.Len(t, apps[0].Executions, 1)

	// The user renames the rule and retargets it AFTER it ran.
	rule.Name = "Renamed after the fact"
	rule.TargetPFCPrimary = strPtr("TRANSPORTATION")

	rec := apps[0].Executions[0]
	assert.Equal(t, "Coffee is Food", rec.RuleName)

	var snap map[string]any
	require.NoError(t, json.Unmarshal(rec.DefinitionSnapshot, &snap))
	assert.Equal(t, "Coffee is Food", snap["name"])
	assert.Equal(t, "FOOD_AND_DRINK", snap["target_pfc_primary"])
}

// TestApplyRulesToBatch_RecordsNothingWhenTheRuleChangesNothing is the user's product ruling and
// trap 5 from the brief: "record only when a rule actually changes something". A rule whose target
// category is ALREADY the transaction's category matched, but did nothing — so it writes no row,
// and `result` never needs a value other than 'applied'.
func TestApplyRulesToBatch_RecordsNothingWhenTheRuleChangesNothing(t *testing.T) {
	mockRepo := new(mockRuleApplicationRepository)
	service := NewRuleApplicationService(mockRepo)
	ctx := context.Background()
	userID := "user-1572"

	mockRepo.On("GetEnabledByUserID", ctx, userID).
		Return([]*domain.CategorizationRule{categoryRule(userID)}, nil)

	merchant := "Blue Bottle Coffee"
	txns := []domain.Transaction{{
		ID:                   "tx-noop",
		UserID:               userID,
		MerchantName:         &merchant,
		CategoryOverrideKind: strPtr("pfc"),
		PFCPrimaryOverride:   strPtr("FOOD_AND_DRINK"),
	}}

	apps, err := service.ApplyRulesToBatch(ctx, txns, userID)
	require.NoError(t, err)
	require.Len(t, apps, 1, "the rule still MATCHED, so match counts and analytics are unaffected")
	assert.Equal(t, []string{"rule-cat"}, apps[0].MatchedRuleIDs)
	assert.Empty(t, apps[0].Executions,
		"the category was already FOOD_AND_DRINK — the rule changed nothing, so history records nothing")
}

// TestApplyRulesToBatch_TagOnlyAndBothActionTypes pins the action_type derivation against the
// live `valid_action_type IN ('category','tags','both')` CHECK.
func TestApplyRulesToBatch_TagOnlyAndBothActionTypes(t *testing.T) {
	t.Run("tags only", func(t *testing.T) {
		mockRepo := new(mockRuleApplicationRepository)
		service := NewRuleApplicationService(mockRepo)
		ctx := context.Background()
		userID := "user-1572"

		mockRepo.On("GetEnabledByUserID", ctx, userID).
			Return([]*domain.CategorizationRule{tagRule(userID)}, nil)

		merchant := "Blue Bottle Coffee"
		txns := []domain.Transaction{{ID: "tx-1", UserID: userID, MerchantName: &merchant}}

		apps, err := service.ApplyRulesToBatch(ctx, txns, userID)
		require.NoError(t, err)
		require.Len(t, apps, 1)
		require.Len(t, apps[0].Executions, 1)
		assert.Equal(t, "rule-tag", apps[0].Executions[0].RuleID)
		assert.Equal(t, domain.RuleExecutionActionTags, apps[0].Executions[0].ActionType)
	})

	t.Run("one rule doing both gets a single row", func(t *testing.T) {
		mockRepo := new(mockRuleApplicationRepository)
		service := NewRuleApplicationService(mockRepo)
		ctx := context.Background()
		userID := "user-1572"

		both := categoryRule(userID)
		both.TargetTags = []string{"caffeine"}
		mockRepo.On("GetEnabledByUserID", ctx, userID).
			Return([]*domain.CategorizationRule{both}, nil)

		merchant := "Blue Bottle Coffee"
		txns := []domain.Transaction{{ID: "tx-1", UserID: userID, MerchantName: &merchant}}

		apps, err := service.ApplyRulesToBatch(ctx, txns, userID)
		require.NoError(t, err)
		require.Len(t, apps, 1)
		require.Len(t, apps[0].Executions, 1,
			"unique_rule_transaction is UNIQUE (rule_id, transaction_id) — one rule may never "+
				"produce two rows for one transaction")
		assert.Equal(t, domain.RuleExecutionActionBoth, apps[0].Executions[0].ActionType)
	})
}

// TestApplyRulesToBatch_MultipleRulesEachGetTheirOwnRow answers trap 4 from the brief:
// MatchedRuleIDs can hold several rules while the result carries one CategoryRuleID. The answer is
// one row per rule that CONTRIBUTED a change, attributed to that rule — not one row for the
// category rule with the others silently dropped.
func TestApplyRulesToBatch_MultipleRulesEachGetTheirOwnRow(t *testing.T) {
	mockRepo := new(mockRuleApplicationRepository)
	service := NewRuleApplicationService(mockRepo)
	ctx := context.Background()
	userID := "user-1572"

	mockRepo.On("GetEnabledByUserID", ctx, userID).
		Return([]*domain.CategorizationRule{categoryRule(userID), tagRule(userID)}, nil)

	merchant := "Blue Bottle Coffee"
	txns := []domain.Transaction{{ID: "tx-1", UserID: userID, MerchantName: &merchant}}

	apps, err := service.ApplyRulesToBatch(ctx, txns, userID)
	require.NoError(t, err)
	require.Len(t, apps, 1)
	require.Len(t, apps[0].Executions, 2)

	byRule := map[string]string{}
	for _, rec := range apps[0].Executions {
		byRule[rec.RuleID] = rec.ActionType
	}
	assert.Equal(t, domain.RuleExecutionActionCategory, byRule["rule-cat"])
	assert.Equal(t, domain.RuleExecutionActionTags, byRule["rule-tag"])
}

// TestApplyRulesToBatch_AMatchedRuleThatContributedNothingGetsNoRow: two tag rules offering the
// same tag. The first to be evaluated adds it; the second matches but its tag is already present,
// so it changed nothing.
func TestApplyRulesToBatch_AMatchedRuleThatContributedNothingGetsNoRow(t *testing.T) {
	mockRepo := new(mockRuleApplicationRepository)
	service := NewRuleApplicationService(mockRepo)
	ctx := context.Background()
	userID := "user-1572"

	first := tagRule(userID)
	second := tagRule(userID)
	second.ID = "rule-tag-2"
	second.Name = "Duplicate tagger"
	second.Priority = 1

	mockRepo.On("GetEnabledByUserID", ctx, userID).
		Return([]*domain.CategorizationRule{first, second}, nil)

	merchant := "Blue Bottle Coffee"
	txns := []domain.Transaction{{ID: "tx-1", UserID: userID, MerchantName: &merchant}}

	apps, err := service.ApplyRulesToBatch(ctx, txns, userID)
	require.NoError(t, err)
	require.Len(t, apps, 1)
	assert.Len(t, apps[0].MatchedRuleIDs, 2, "both rules matched")
	require.Len(t, apps[0].Executions, 1, "only the rule whose tag actually landed changed anything")
	assert.Equal(t, "rule-tag", apps[0].Executions[0].RuleID)
}

// ---------------------------------------------------------------------------------------------
// EmitBatchSideEffects is the writer.
// ---------------------------------------------------------------------------------------------

// TestEmitBatchSideEffects_WritesHistoryOnlyForInsertedTransactions is trap 1 from the brief.
// rule_execution_history.transaction_id still carries an FK to transactions (ON DELETE SET NULL
// since migration 000157, but an FK all the same), so a row written for a transaction the upsert
// never inserted would violate it. EmitBatchSideEffects already filters on insertedIDs, which
// CreateBatch derives from `xmax = 0` — genuinely new rows only.
func TestEmitBatchSideEffects_WritesHistoryOnlyForInsertedTransactions(t *testing.T) {
	mockRepo := new(mockRuleApplicationRepository)
	mockHistory := new(mockRuleExecutionHistoryRepository)
	service := NewRuleApplicationService(mockRepo)
	service.SetRuleExecutionHistoryRepository(mockHistory)
	ctx := context.Background()
	userID := "user-1572"

	mockRepo.On("IncrementMatchCount", ctx, mock.Anything).Return(nil)

	snapshot := json.RawMessage(`{"name":"Coffee is Food"}`)
	apps := []BatchRuleApplication{
		{
			TransactionID:  "tx-inserted",
			MatchedRuleIDs: []string{"rule-cat"},
			Executions: []RuleExecutionRecord{{
				RuleID: "rule-cat", RuleName: "Coffee is Food",
				ActionType: domain.RuleExecutionActionCategory, DefinitionSnapshot: snapshot,
			}},
		},
		{
			TransactionID:  "tx-conflicted",
			MatchedRuleIDs: []string{"rule-cat"},
			Executions: []RuleExecutionRecord{{
				RuleID: "rule-cat", RuleName: "Coffee is Food",
				ActionType: domain.RuleExecutionActionCategory, DefinitionSnapshot: snapshot,
			}},
		},
	}

	var written []*domain.RuleExecution
	mockHistory.On("Create", ctx, mock.Anything).Run(func(args mock.Arguments) {
		written = append(written, args.Get(1).(*domain.RuleExecution))
	}).Return(nil)

	service.EmitBatchSideEffects(ctx, userID, apps, map[string]bool{"tx-inserted": true})

	require.Len(t, written, 1, "the non-inserted transaction has no row for the FK to point at")
	row := written[0]
	require.NotNil(t, row.TransactionID)
	assert.Equal(t, "tx-inserted", *row.TransactionID)
	require.NotNil(t, row.RuleID)
	assert.Equal(t, "rule-cat", *row.RuleID)
	assert.Equal(t, userID, row.UserID,
		"user_id is the row's own GDPR erasure path and the key every read uses")
	require.NotNil(t, row.RuleName)
	assert.Equal(t, "Coffee is Food", *row.RuleName)
	assert.Equal(t, domain.RuleExecutionActionCategory, row.ActionType)
	assert.Equal(t, domain.RuleExecutionResultApplied, row.Result,
		"per the user's ruling only real changes are recorded, so result is always 'applied'")
	assert.JSONEq(t, string(snapshot), string(row.DefinitionSnapshot))
}

// TestEmitBatchSideEffects_NilHistoryRepositoryIsSafe — the repository is injected by a setter, as
// SetAnalytics is, so every existing construction site keeps compiling and keeps working.
func TestEmitBatchSideEffects_NilHistoryRepositoryIsSafe(t *testing.T) {
	mockRepo := new(mockRuleApplicationRepository)
	service := NewRuleApplicationService(mockRepo)
	ctx := context.Background()

	mockRepo.On("IncrementMatchCount", ctx, "rule-cat").Return(nil)

	assert.NotPanics(t, func() {
		service.EmitBatchSideEffects(ctx, "user-1572", []BatchRuleApplication{{
			TransactionID:  "tx-1",
			MatchedRuleIDs: []string{"rule-cat"},
			Executions: []RuleExecutionRecord{{
				RuleID: "rule-cat", ActionType: domain.RuleExecutionActionCategory,
			}},
		}}, map[string]bool{"tx-1": true})
	})
	mockRepo.AssertCalled(t, "IncrementMatchCount", ctx, "rule-cat")
}

// TestEmitBatchSideEffects_HistoryWriteFailureDoesNotStopMatchCounts — losing an audit row is bad;
// losing the user's match counts because the audit row failed is worse. Same reasoning as
// buildBoostDefinitionSnapshot's "a nil snapshot is written rather than failing the execution".
func TestEmitBatchSideEffects_HistoryWriteFailureDoesNotStopMatchCounts(t *testing.T) {
	mockRepo := new(mockRuleApplicationRepository)
	mockHistory := new(mockRuleExecutionHistoryRepository)
	service := NewRuleApplicationService(mockRepo)
	service.SetRuleExecutionHistoryRepository(mockHistory)
	ctx := context.Background()

	mockHistory.On("Create", ctx, mock.Anything).Return(errors.New("boom"))
	mockRepo.On("IncrementMatchCount", ctx, "rule-cat").Return(nil)

	service.EmitBatchSideEffects(ctx, "user-1572", []BatchRuleApplication{{
		TransactionID:  "tx-1",
		MatchedRuleIDs: []string{"rule-cat"},
		Executions: []RuleExecutionRecord{{
			RuleID: "rule-cat", ActionType: domain.RuleExecutionActionCategory,
		}},
	}}, map[string]bool{"tx-1": true})

	mockRepo.AssertCalled(t, "IncrementMatchCount", ctx, "rule-cat")
}

// TestEmitBatchSideEffects_NoExecutionsMeansNoWrite closes the loop with the product ruling: a
// matched-but-changed-nothing application still increments its match count and still fires
// analytics, and still writes no history.
func TestEmitBatchSideEffects_NoExecutionsMeansNoWrite(t *testing.T) {
	mockRepo := new(mockRuleApplicationRepository)
	mockHistory := new(mockRuleExecutionHistoryRepository)
	service := NewRuleApplicationService(mockRepo)
	service.SetRuleExecutionHistoryRepository(mockHistory)
	ctx := context.Background()

	mockRepo.On("IncrementMatchCount", ctx, "rule-cat").Return(nil)

	service.EmitBatchSideEffects(ctx, "user-1572", []BatchRuleApplication{{
		TransactionID:  "tx-1",
		MatchedRuleIDs: []string{"rule-cat"},
	}}, map[string]bool{"tx-1": true})

	mockHistory.AssertNotCalled(t, "Create", mock.Anything, mock.Anything)
	mockRepo.AssertCalled(t, "IncrementMatchCount", ctx, "rule-cat")
}

// ---------------------------------------------------------------------------------------------
// The read path's service half.
// ---------------------------------------------------------------------------------------------

// TestRuleExecutionHistoryService_GetUserExecutionHistory pins the tenancy scope: the userID the
// service passes to the repository is the authenticated one and nothing else. Mirrors
// boostExecutionService.GetUserExecutionHistory, whose ownership is likewise enforced by the
// execution row's OWN denormalised user_id — a deleted rule leaves no parent to authorise against.
func TestRuleExecutionHistoryService_GetUserExecutionHistory(t *testing.T) {
	mockHistory := new(mockRuleExecutionHistoryRepository)
	service := NewRuleExecutionHistoryService(mockHistory)
	ctx := context.Background()

	rows := []*domain.RuleExecution{{ID: "exec-1", UserID: "user-1572"}}
	mockHistory.On("GetByUserID", ctx, "user-1572", 20, 0).Return(rows, 1, nil)

	got, total, err := service.GetUserExecutionHistory(ctx, "user-1572", 20, 0)
	require.NoError(t, err)
	assert.Equal(t, 1, total)
	assert.Equal(t, rows, got)
	mockHistory.AssertExpectations(t)
}
