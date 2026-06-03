package services

import (
	"context"
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	phclient "github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// mockRuleApplicationRepository mocks rule repository for testing
type mockRuleApplicationRepository struct {
	mock.Mock
}

func (m *mockRuleApplicationRepository) GetEnabledByUserID(ctx context.Context, userID string) ([]*domain.CategorizationRule, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.CategorizationRule), args.Error(1)
}

func (m *mockRuleApplicationRepository) IncrementMatchCount(ctx context.Context, ruleID string) error {
	args := m.Called(ctx, ruleID)
	return args.Error(0)
}

// TestRuleApplicationService_ApplyRulesToTransaction tests applying rules to a single transaction
func TestRuleApplicationService_ApplyRulesToTransaction(t *testing.T) {
	mockRepo := new(mockRuleApplicationRepository)
	service := NewRuleApplicationService(mockRepo)
	ctx := context.Background()
	userID := "user-123"

	// Setup rules
	rules := []*domain.CategorizationRule{
		{
			ID:               "rule-1",
			UserID:           userID,
			Name:             "Coffee Rule",
			Enabled:          true,
			Priority:         10,
			Conditions:       []domain.RuleCondition{{Type: domain.ConditionMerchantContains, Value: "starbucks"}},
			ConditionLogic:   "AND",
			TargetKind:       strPtr("pfc"),
			TargetPFCPrimary: strPtr("FOOD_AND_DRINK"),
			TagBehavior:      "add",
			Source:           "manual",
		},
	}

	mockRepo.On("GetEnabledByUserID", ctx, userID).Return(rules, nil)
	mockRepo.On("IncrementMatchCount", ctx, "rule-1").Return(nil)

	// Create transaction
	merchantName := "Starbucks Coffee"
	tx := &domain.Transaction{
		ID:           "tx-123",
		UserID:       userID,
		MerchantName: &merchantName,
	}

	err := service.ApplyRulesToTransaction(ctx, tx)

	require.NoError(t, err)
	assert.Equal(t, domain.CategorySourceRule, tx.CategorySourceField)
	// Category override kind should be set to "pfc"
	require.NotNil(t, tx.CategoryOverrideKind)
	assert.Equal(t, "pfc", *tx.CategoryOverrideKind)
	require.NotNil(t, tx.PFCPrimaryOverride)
	assert.Equal(t, "FOOD_AND_DRINK", *tx.PFCPrimaryOverride)
	mockRepo.AssertExpectations(t)
}

// TestRuleApplicationService_SkipManualOverride tests that manual overrides are respected
func TestRuleApplicationService_SkipManualOverride(t *testing.T) {
	mockRepo := new(mockRuleApplicationRepository)
	service := NewRuleApplicationService(mockRepo)
	ctx := context.Background()
	userID := "user-123"

	rules := []*domain.CategorizationRule{
		{
			ID:               "rule-1",
			UserID:           userID,
			Name:             "Coffee Rule",
			Enabled:          true,
			Priority:         10,
			Conditions:       []domain.RuleCondition{{Type: domain.ConditionMerchantContains, Value: "starbucks"}},
			ConditionLogic:   "AND",
			TargetKind:       strPtr("pfc"),
			TargetPFCPrimary: strPtr("FOOD_AND_DRINK"),
			TagBehavior:      "add",
			Source:           "manual",
		},
	}

	mockRepo.On("GetEnabledByUserID", ctx, userID).Return(rules, nil)

	// Transaction with manual override - should NOT be changed
	merchantName := "Starbucks Coffee"
	existingKind := "pfc"
	existingPrimary := "GENERAL_MERCHANDISE"
	tx := &domain.Transaction{
		ID:                   "tx-123",
		UserID:               userID,
		MerchantName:         &merchantName,
		CategorySourceField:  domain.CategorySourceManual,
		CategoryOverrideKind: &existingKind,
		PFCPrimaryOverride:   &existingPrimary,
	}

	err := service.ApplyRulesToTransaction(ctx, tx)

	require.NoError(t, err)
	// Category should NOT be changed since it was manually set
	assert.Equal(t, domain.CategorySourceManual, tx.CategorySourceField)
	assert.Equal(t, "GENERAL_MERCHANDISE", *tx.PFCPrimaryOverride)
	// IncrementMatchCount should NOT be called since we skipped the rule
	mockRepo.AssertNotCalled(t, "IncrementMatchCount", mock.Anything, mock.Anything)
}

// TestRuleApplicationService_CategoryMatchMarksReviewed verifies a rule applying a
// category action marks the transaction reviewed (Issue #1203).
func TestRuleApplicationService_CategoryMatchMarksReviewed(t *testing.T) {
	mockRepo := new(mockRuleApplicationRepository)
	service := NewRuleApplicationService(mockRepo)
	ctx := context.Background()
	userID := "user-123"

	rules := []*domain.CategorizationRule{{
		ID: "rule-1", UserID: userID, Name: "Coffee", Enabled: true, Priority: 10,
		Conditions:     []domain.RuleCondition{{Type: domain.ConditionMerchantContains, Value: "coffee"}},
		ConditionLogic: "AND", TargetKind: strPtr("pfc"), TargetPFCPrimary: strPtr("FOOD_AND_DRINK"),
		TagBehavior: "add", Source: "manual",
	}}
	mockRepo.On("GetEnabledByUserID", ctx, userID).Return(rules, nil)
	mockRepo.On("IncrementMatchCount", ctx, "rule-1").Return(nil)

	merchant := "Blue Bottle Coffee"
	tx := &domain.Transaction{ID: "tx-1", UserID: userID, MerchantName: &merchant}

	require.NoError(t, service.ApplyRulesToTransaction(ctx, tx))
	assert.True(t, tx.Reviewed, "category-applying rule should mark reviewed")
	require.NotNil(t, tx.ReviewedAt)
}

// TestRuleApplicationService_TagOnlyDoesNotMarkReviewed verifies a tag-only match does
// NOT mark the transaction reviewed (Issue #1203).
func TestRuleApplicationService_TagOnlyDoesNotMarkReviewed(t *testing.T) {
	mockRepo := new(mockRuleApplicationRepository)
	service := NewRuleApplicationService(mockRepo)
	ctx := context.Background()
	userID := "user-123"

	rules := []*domain.CategorizationRule{{
		ID: "rule-1", UserID: userID, Name: "Tag only", Enabled: true, Priority: 10,
		Conditions:     []domain.RuleCondition{{Type: domain.ConditionMerchantContains, Value: "coffee"}},
		ConditionLogic: "AND", TargetTags: []string{"morning"}, TagBehavior: "add", Source: "manual",
	}}
	mockRepo.On("GetEnabledByUserID", ctx, userID).Return(rules, nil)
	mockRepo.On("IncrementMatchCount", ctx, "rule-1").Return(nil)

	merchant := "Blue Bottle Coffee"
	tx := &domain.Transaction{ID: "tx-1", UserID: userID, MerchantName: &merchant}

	require.NoError(t, service.ApplyRulesToTransaction(ctx, tx))
	assert.False(t, tx.Reviewed, "tag-only rule must not mark reviewed")
	assert.Nil(t, tx.ReviewedAt)
}

// TestRuleApplicationService_SkipsReviewed verifies reviewed transactions are protected
// from auto-recategorization (Issue #1203).
func TestRuleApplicationService_SkipsReviewed(t *testing.T) {
	mockRepo := new(mockRuleApplicationRepository)
	service := NewRuleApplicationService(mockRepo)
	ctx := context.Background()
	userID := "user-123"

	merchant := "Blue Bottle Coffee"
	tx := &domain.Transaction{ID: "tx-1", UserID: userID, MerchantName: &merchant, Reviewed: true}

	require.NoError(t, service.ApplyRulesToTransaction(ctx, tx))
	assert.Nil(t, tx.CategoryOverrideKind, "reviewed transaction must not be recategorized")
	mockRepo.AssertNotCalled(t, "GetEnabledByUserID", mock.Anything, mock.Anything)
}

// TestRuleApplicationService_ApplyTags tests that tags are applied from all matching rules
func TestRuleApplicationService_ApplyTags(t *testing.T) {
	mockRepo := new(mockRuleApplicationRepository)
	service := NewRuleApplicationService(mockRepo)
	ctx := context.Background()
	userID := "user-123"

	rules := []*domain.CategorizationRule{
		{
			ID:               "rule-1",
			UserID:           userID,
			Name:             "Coffee Rule",
			Enabled:          true,
			Priority:         10,
			Conditions:       []domain.RuleCondition{{Type: domain.ConditionMerchantContains, Value: "coffee"}},
			ConditionLogic:   "AND",
			TargetKind:       strPtr("pfc"),
			TargetPFCPrimary: strPtr("FOOD_AND_DRINK"),
			TargetTags:       []string{"coffee", "morning"},
			TagBehavior:      "add",
			Source:           "manual",
		},
		{
			ID:             "rule-2",
			UserID:         userID,
			Name:           "Subscription Rule",
			Enabled:        true,
			Priority:       5,
			Conditions:     []domain.RuleCondition{{Type: domain.ConditionMerchantContains, Value: "starbucks"}},
			ConditionLogic: "AND",
			TargetTags:     []string{"subscription"},
			TagBehavior:    "add",
			Source:         "manual",
		},
	}

	mockRepo.On("GetEnabledByUserID", ctx, userID).Return(rules, nil)
	mockRepo.On("IncrementMatchCount", ctx, "rule-1").Return(nil)
	mockRepo.On("IncrementMatchCount", ctx, "rule-2").Return(nil)

	merchantName := "Starbucks Coffee Shop"
	tx := &domain.Transaction{
		ID:           "tx-123",
		UserID:       userID,
		MerchantName: &merchantName,
	}

	err := service.ApplyRulesToTransaction(ctx, tx)

	require.NoError(t, err)
	// Tags from both rules should be applied
	assert.Len(t, tx.Tags, 3)
	assert.Contains(t, tx.Tags, "coffee")
	assert.Contains(t, tx.Tags, "morning")
	assert.Contains(t, tx.Tags, "subscription")
	// Tag sources should track origins
	assert.Len(t, tx.TagsSource, 3)
}

// TestRuleApplicationService_ApplyRulesToBatch tests applying rules to multiple transactions
func TestRuleApplicationService_ApplyRulesToBatch(t *testing.T) {
	mockRepo := new(mockRuleApplicationRepository)
	service := NewRuleApplicationService(mockRepo)
	ctx := context.Background()
	userID := "user-123"

	rules := []*domain.CategorizationRule{
		{
			ID:               "rule-1",
			UserID:           userID,
			Name:             "Coffee Rule",
			Enabled:          true,
			Priority:         10,
			Conditions:       []domain.RuleCondition{{Type: domain.ConditionMerchantContains, Value: "coffee"}},
			ConditionLogic:   "AND",
			TargetKind:       strPtr("pfc"),
			TargetPFCPrimary: strPtr("FOOD_AND_DRINK"),
			TagBehavior:      "add",
			Source:           "manual",
		},
	}

	mockRepo.On("GetEnabledByUserID", ctx, userID).Return(rules, nil)
	// IncrementMatchCount is NOT called by ApplyRulesToBatch (deferred to EmitBatchSideEffects).

	merchant1 := "Starbucks Coffee"
	merchant2 := "Walmart"
	txns := []domain.Transaction{
		{ID: "tx-1", UserID: userID, MerchantName: &merchant1},
		{ID: "tx-2", UserID: userID, MerchantName: &merchant2},
	}

	_, err := service.ApplyRulesToBatch(ctx, txns, userID)

	require.NoError(t, err)
	// First transaction should match
	assert.Equal(t, domain.CategorySourceRule, txns[0].CategorySourceField)
	// Second transaction should NOT match (no coffee in name)
	assert.Equal(t, domain.CategorySource(""), txns[1].CategorySourceField)
	mockRepo.AssertNotCalled(t, "IncrementMatchCount", mock.Anything, mock.Anything)
}

// TestRuleApplicationService_ApplyRulesToBatch_MarksReviewed verifies that a batch
// (sync-path) category match marks the transaction reviewed (Issue #1203).
func TestRuleApplicationService_ApplyRulesToBatch_MarksReviewed(t *testing.T) {
	mockRepo := new(mockRuleApplicationRepository)
	service := NewRuleApplicationService(mockRepo)
	ctx := context.Background()
	userID := "user-123"

	rules := []*domain.CategorizationRule{{
		ID: "rule-1", UserID: userID, Name: "Coffee", Enabled: true, Priority: 10,
		Conditions:     []domain.RuleCondition{{Type: domain.ConditionMerchantContains, Value: "coffee"}},
		ConditionLogic: "AND", TargetKind: strPtr("pfc"), TargetPFCPrimary: strPtr("FOOD_AND_DRINK"),
		TagBehavior: "add", Source: "manual",
	}}
	mockRepo.On("GetEnabledByUserID", ctx, userID).Return(rules, nil)
	// IncrementMatchCount is NOT called by ApplyRulesToBatch (deferred to EmitBatchSideEffects).

	merchant := "Blue Bottle Coffee"
	txns := []domain.Transaction{{ID: "tx-1", UserID: userID, MerchantName: &merchant}}

	_, err := service.ApplyRulesToBatch(ctx, txns, userID)
	require.NoError(t, err)
	assert.True(t, txns[0].Reviewed, "batch category match should mark reviewed")
	require.NotNil(t, txns[0].ReviewedAt)
	mockRepo.AssertNotCalled(t, "IncrementMatchCount", mock.Anything, mock.Anything)
}

// TestRuleApplicationService_ApplyRulesToBatch_SkipsReviewed verifies reviewed rows
// are not recategorized by the batch path (Issue #1203).
func TestRuleApplicationService_ApplyRulesToBatch_SkipsReviewed(t *testing.T) {
	mockRepo := new(mockRuleApplicationRepository)
	service := NewRuleApplicationService(mockRepo)
	ctx := context.Background()
	userID := "user-123"

	rules := []*domain.CategorizationRule{{
		ID: "rule-1", UserID: userID, Name: "Coffee", Enabled: true, Priority: 10,
		Conditions:     []domain.RuleCondition{{Type: domain.ConditionMerchantContains, Value: "coffee"}},
		ConditionLogic: "AND", TargetKind: strPtr("pfc"), TargetPFCPrimary: strPtr("FOOD_AND_DRINK"),
		TagBehavior: "add", Source: "manual",
	}}
	mockRepo.On("GetEnabledByUserID", ctx, userID).Return(rules, nil)

	merchant := "Blue Bottle Coffee"
	txns := []domain.Transaction{{ID: "tx-1", UserID: userID, MerchantName: &merchant, Reviewed: true}}

	_, err := service.ApplyRulesToBatch(ctx, txns, userID)
	require.NoError(t, err)
	assert.Nil(t, txns[0].CategoryOverrideKind, "reviewed transaction must not be recategorized in batch")
}

// TestRuleApplicationService_NoRules tests graceful handling when user has no rules
func TestRuleApplicationService_NoRules(t *testing.T) {
	mockRepo := new(mockRuleApplicationRepository)
	service := NewRuleApplicationService(mockRepo)
	ctx := context.Background()
	userID := "user-123"

	mockRepo.On("GetEnabledByUserID", ctx, userID).Return([]*domain.CategorizationRule{}, nil)

	merchantName := "Starbucks"
	tx := &domain.Transaction{
		ID:           "tx-123",
		UserID:       userID,
		MerchantName: &merchantName,
	}

	err := service.ApplyRulesToTransaction(ctx, tx)

	require.NoError(t, err)
	// Transaction should remain unchanged
	assert.Nil(t, tx.CategoryOverrideKind)
}

// TestRuleApplicationService_PreserveManualTags tests that manual tags are preserved
func TestRuleApplicationService_PreserveManualTags(t *testing.T) {
	mockRepo := new(mockRuleApplicationRepository)
	service := NewRuleApplicationService(mockRepo)
	ctx := context.Background()
	userID := "user-123"

	rules := []*domain.CategorizationRule{
		{
			ID:             "rule-1",
			UserID:         userID,
			Name:           "Coffee Rule",
			Enabled:        true,
			Priority:       10,
			Conditions:     []domain.RuleCondition{{Type: domain.ConditionMerchantContains, Value: "coffee"}},
			ConditionLogic: "AND",
			TargetTags:     []string{"auto-tag"},
			TagBehavior:    "add",
			Source:         "manual",
		},
	}

	mockRepo.On("GetEnabledByUserID", ctx, userID).Return(rules, nil)
	mockRepo.On("IncrementMatchCount", ctx, "rule-1").Return(nil)

	merchantName := "Coffee Shop"
	tx := &domain.Transaction{
		ID:           "tx-123",
		UserID:       userID,
		MerchantName: &merchantName,
		Tags:         []string{"existing-manual-tag"},
		TagsSource: []domain.TagSource{
			{Tag: "existing-manual-tag", Source: "manual"},
		},
	}

	err := service.ApplyRulesToTransaction(ctx, tx)

	require.NoError(t, err)
	// Both manual tag and rule tag should be present
	assert.Len(t, tx.Tags, 2)
	assert.Contains(t, tx.Tags, "existing-manual-tag")
	assert.Contains(t, tx.Tags, "auto-tag")
}

// TestRuleApplicationService_ReplaceTagBehavior tests "replace" tag behavior
func TestRuleApplicationService_ReplaceTagBehavior(t *testing.T) {
	mockRepo := new(mockRuleApplicationRepository)
	service := NewRuleApplicationService(mockRepo)
	ctx := context.Background()
	userID := "user-123"

	rules := []*domain.CategorizationRule{
		{
			ID:             "rule-1",
			UserID:         userID,
			Name:           "Coffee Rule",
			Enabled:        true,
			Priority:       10,
			Conditions:     []domain.RuleCondition{{Type: domain.ConditionMerchantContains, Value: "coffee"}},
			ConditionLogic: "AND",
			TargetTags:     []string{"replaced-tag"},
			TagBehavior:    "replace",
			Source:         "manual",
		},
	}

	mockRepo.On("GetEnabledByUserID", ctx, userID).Return(rules, nil)
	mockRepo.On("IncrementMatchCount", ctx, "rule-1").Return(nil)

	merchantName := "Coffee Shop"
	tx := &domain.Transaction{
		ID:           "tx-123",
		UserID:       userID,
		MerchantName: &merchantName,
		Tags:         []string{"old-rule-tag"},
		TagsSource: []domain.TagSource{
			{Tag: "old-rule-tag", Source: "rule", RuleID: strPtr("old-rule-id")},
		},
	}

	err := service.ApplyRulesToTransaction(ctx, tx)

	require.NoError(t, err)
	// Replace behavior should only replace non-manual tags
	assert.Len(t, tx.Tags, 1)
	assert.Contains(t, tx.Tags, "replaced-tag")
}

func strPtr(s string) *string {
	return &s
}

// ---------------------------------------------------------------------------
// mockAnalyticsForRuleService — test double for phclient.Analytics.
// Pattern from internal/queue/jobs/email_change_propagation_test.go.
// ---------------------------------------------------------------------------

type mockAnalyticsForRuleService struct {
	mock.Mock
}

func (m *mockAnalyticsForRuleService) Capture(distinctID, event string, properties map[string]interface{}) {
	m.Called(distinctID, event, properties)
}

func (m *mockAnalyticsForRuleService) Identify(distinctID string, properties map[string]interface{}) {
	m.Called(distinctID, properties)
}

func (m *mockAnalyticsForRuleService) Close() {
	m.Called()
}

// ---------------------------------------------------------------------------
// Issue #1213 — RED tests: ApplyRulesToBatch returns BatchRuleApplication
// slice and emits NO side-effects; EmitBatchSideEffects gates on insertedIDs.
// ---------------------------------------------------------------------------

// TestRuleApplicationService_ApplyRulesToBatch_ReturnsBatchApplications_NoSideEffects
// verifies the new contract: ApplyRulesToBatch returns one BatchRuleApplication
// per matched transaction and does NOT call analytics.Capture or
// IncrementMatchCount (those are deferred to EmitBatchSideEffects).
func TestRuleApplicationService_ApplyRulesToBatch_ReturnsBatchApplications_NoSideEffects(t *testing.T) {
	mockRepo := new(mockRuleApplicationRepository)
	mockAn := new(mockAnalyticsForRuleService)
	service := NewRuleApplicationService(mockRepo)
	service.SetAnalytics(mockAn)
	ctx := context.Background()
	userID := "user-1213"

	rules := []*domain.CategorizationRule{{
		ID: "rule-cat", UserID: userID, Name: "Coffee", Enabled: true, Priority: 10,
		Conditions:     []domain.RuleCondition{{Type: domain.ConditionMerchantContains, Value: "coffee"}},
		ConditionLogic: "AND", TargetKind: strPtr("pfc"), TargetPFCPrimary: strPtr("FOOD_AND_DRINK"),
		TagBehavior: "add", Source: "manual",
	}}
	mockRepo.On("GetEnabledByUserID", ctx, userID).Return(rules, nil)
	// IncrementMatchCount must NOT be called — emitting counts is deferred.

	merchant := "Blue Bottle Coffee"
	txns := []domain.Transaction{{ID: "tx-fresh", UserID: userID, MerchantName: &merchant}}

	applications, err := service.ApplyRulesToBatch(ctx, txns, userID)

	require.NoError(t, err)
	require.Len(t, applications, 1, "one application per matched transaction")
	assert.Equal(t, "tx-fresh", applications[0].TransactionID)
	assert.True(t, applications[0].CategoryApplied)
	assert.Equal(t, "rule-cat", applications[0].CategoryRuleID)
	assert.Contains(t, applications[0].MatchedRuleIDs, "rule-cat")

	// No analytics, no match-count increments in the apply phase.
	mockAn.AssertNotCalled(t, "Capture", mock.Anything, mock.Anything, mock.Anything)
	mockRepo.AssertNotCalled(t, "IncrementMatchCount", mock.Anything, mock.Anything)
	mockRepo.AssertExpectations(t)
}

// TestRuleApplicationService_EmitBatchSideEffects_OnlyEmitsForInsertedRows
// verifies that analytics and match-count increments fire only for rows whose
// ID appears in insertedIDs (i.e. were genuinely inserted, not conflict-preserved).
func TestRuleApplicationService_EmitBatchSideEffects_OnlyEmitsForInsertedRows(t *testing.T) {
	mockRepo := new(mockRuleApplicationRepository)
	mockAn := new(mockAnalyticsForRuleService)
	service := NewRuleApplicationService(mockRepo)
	service.SetAnalytics(mockAn)
	ctx := context.Background()
	userID := "user-1213"

	applications := []BatchRuleApplication{
		{TransactionID: "tx-inserted", MatchedRuleIDs: []string{"rule-1"}, CategoryApplied: true, CategoryRuleID: "rule-1"},
		{TransactionID: "tx-conflict", MatchedRuleIDs: []string{"rule-1"}, CategoryApplied: true, CategoryRuleID: "rule-1"},
	}
	insertedIDs := map[string]bool{"tx-inserted": true}

	mockAn.On("Capture", userID, mock.AnythingOfType("string"), mock.MatchedBy(func(props map[string]interface{}) bool {
		return props["transaction_id"] == "tx-inserted"
	})).Return()
	mockRepo.On("IncrementMatchCount", ctx, "rule-1").Return(nil).Once()

	service.EmitBatchSideEffects(ctx, userID, applications, insertedIDs)

	// Only one Capture and one IncrementMatchCount for tx-inserted.
	mockAn.AssertNumberOfCalls(t, "Capture", 1)
	mockRepo.AssertNumberOfCalls(t, "IncrementMatchCount", 1)
	mockAn.AssertExpectations(t)
	mockRepo.AssertExpectations(t)
}

// TestRuleApplicationService_EmitBatchSideEffects_TagOnlyInserted_CountsButNoAnalytics
// verifies that a tag-only match in an inserted row increments the match count
// but does NOT emit an auto_categorization_executed event.
func TestRuleApplicationService_EmitBatchSideEffects_TagOnlyInserted_CountsButNoAnalytics(t *testing.T) {
	mockRepo := new(mockRuleApplicationRepository)
	mockAn := new(mockAnalyticsForRuleService)
	service := NewRuleApplicationService(mockRepo)
	service.SetAnalytics(mockAn)
	ctx := context.Background()
	userID := "user-1213"

	// Tag-only: CategoryApplied=false, CategoryRuleID="", but MatchedRuleIDs present.
	applications := []BatchRuleApplication{
		{TransactionID: "tx-tag-only", MatchedRuleIDs: []string{"rule-tag"}, CategoryApplied: false, CategoryRuleID: ""},
	}
	insertedIDs := map[string]bool{"tx-tag-only": true}

	mockRepo.On("IncrementMatchCount", ctx, "rule-tag").Return(nil).Once()

	service.EmitBatchSideEffects(ctx, userID, applications, insertedIDs)

	mockAn.AssertNotCalled(t, "Capture", mock.Anything, mock.Anything, mock.Anything)
	mockRepo.AssertExpectations(t)
}

// TestRuleApplicationService_EmitBatchSideEffects_MixedBatch verifies the full
// combination: some rows inserted, some conflict-preserved; only the inserted
// row's side-effects fire.
func TestRuleApplicationService_EmitBatchSideEffects_MixedBatch(t *testing.T) {
	mockRepo := new(mockRuleApplicationRepository)
	mockAn := new(mockAnalyticsForRuleService)
	service := NewRuleApplicationService(mockRepo)
	service.SetAnalytics(mockAn)
	ctx := context.Background()
	userID := "user-1213"

	applications := []BatchRuleApplication{
		// inserted — category rule applied
		{TransactionID: "tx-new", MatchedRuleIDs: []string{"rule-cat"}, CategoryApplied: true, CategoryRuleID: "rule-cat"},
		// conflict-preserved — same rule matched but upsert preserved existing fields
		{TransactionID: "tx-old", MatchedRuleIDs: []string{"rule-cat"}, CategoryApplied: true, CategoryRuleID: "rule-cat"},
		// inserted but no rule matched — not in applications at all (represented by absent application)
	}
	insertedIDs := map[string]bool{"tx-new": true} // tx-old NOT inserted

	mockAn.On("Capture", userID, mock.AnythingOfType("string"), mock.MatchedBy(func(props map[string]interface{}) bool {
		return props["transaction_id"] == "tx-new"
	})).Return()
	mockRepo.On("IncrementMatchCount", ctx, "rule-cat").Return(nil).Once()

	service.EmitBatchSideEffects(ctx, userID, applications, insertedIDs)

	mockAn.AssertNumberOfCalls(t, "Capture", 1)
	mockRepo.AssertNumberOfCalls(t, "IncrementMatchCount", 1)
	mockAn.AssertExpectations(t)
	mockRepo.AssertExpectations(t)
}

// TestRuleApplicationService_EmitBatchSideEffects_UsesCategoryRuleID verifies
// that auto_categorization_executed emits the CategoryRuleID (the rule that
// actually applied the category), not MatchedRuleIDs[0] (which may be a
// higher-priority tag-only rule that matched earlier).
func TestRuleApplicationService_EmitBatchSideEffects_UsesCategoryRuleID(t *testing.T) {
	mockRepo := new(mockRuleApplicationRepository)
	mockAn := new(mockAnalyticsForRuleService)
	service := NewRuleApplicationService(mockRepo)
	service.SetAnalytics(mockAn)
	ctx := context.Background()
	userID := "user-1213"

	// rule-tag-high matched first (priority 20), rule-cat-low set the category (priority 10).
	applications := []BatchRuleApplication{{
		TransactionID:   "tx-1",
		MatchedRuleIDs:  []string{"rule-tag-high", "rule-cat-low"}, // tag rule is [0]
		CategoryApplied: true,
		CategoryRuleID:  "rule-cat-low", // engine sets this to the actual category rule
	}}
	insertedIDs := map[string]bool{"tx-1": true}

	// Expect analytics with rule_id = "rule-cat-low", NOT "rule-tag-high".
	mockAn.On("Capture", userID, mock.AnythingOfType("string"), mock.MatchedBy(func(props map[string]interface{}) bool {
		return props["rule_id"] == "rule-cat-low"
	})).Return()
	mockRepo.On("IncrementMatchCount", ctx, "rule-tag-high").Return(nil).Once()
	mockRepo.On("IncrementMatchCount", ctx, "rule-cat-low").Return(nil).Once()

	service.EmitBatchSideEffects(ctx, userID, applications, insertedIDs)

	mockAn.AssertNumberOfCalls(t, "Capture", 1)
	mockAn.AssertExpectations(t)
	mockRepo.AssertExpectations(t)
}

// TestRuleApplicationService_ApplyRulesToTransaction_UsesCategoryRuleID_ForAnalytics
// verifies that the single-transaction path emits auto_categorization_executed using
// CategoryRuleID (the rule that actually set the category), NOT MatchedRuleIDs[0]
// which may be a higher-priority tag-only rule (Issue #1213).
// This is the single-transaction counterpart of EmitBatchSideEffects_UsesCategoryRuleID.
func TestRuleApplicationService_ApplyRulesToTransaction_UsesCategoryRuleID_ForAnalytics(t *testing.T) {
	mockRepo := new(mockRuleApplicationRepository)
	mockAn := new(mockAnalyticsForRuleService)
	service := NewRuleApplicationService(mockRepo)
	service.SetAnalytics(mockAn)
	ctx := context.Background()
	userID := "user-1213"

	// rule-tag-high: priority 20, tag-only — matches first, so MatchedRuleIDs[0] = "rule-tag-high" (WRONG attribution)
	// rule-cat-low:  priority 10, category — CategoryRuleID = "rule-cat-low" (CORRECT attribution)
	coffeeCond := []domain.RuleCondition{{Type: domain.ConditionMerchantContains, Value: "coffee"}}
	rules := []*domain.CategorizationRule{
		{
			ID: "rule-tag-high", UserID: userID, Name: "Tag rule", Enabled: true, Priority: 20,
			Conditions: coffeeCond, ConditionLogic: "AND", TargetTags: []string{"morning"}, TagBehavior: "add", Source: "manual",
		},
		{
			ID: "rule-cat-low", UserID: userID, Name: "Category rule", Enabled: true, Priority: 10,
			Conditions: coffeeCond, ConditionLogic: "AND",
			TargetKind: strPtr("pfc"), TargetPFCPrimary: strPtr("FOOD_AND_DRINK"),
			TagBehavior: "add", Source: "manual",
		},
	}
	mockRepo.On("GetEnabledByUserID", ctx, userID).Return(rules, nil)
	mockRepo.On("IncrementMatchCount", ctx, "rule-tag-high").Return(nil).Once()
	mockRepo.On("IncrementMatchCount", ctx, "rule-cat-low").Return(nil).Once()

	// Expect analytics with rule_id = "rule-cat-low" (CategoryRuleID), NOT "rule-tag-high" (MatchedRuleIDs[0]).
	mockAn.On("Capture", userID, phclient.EventAutoCategorizationExecuted, mock.MatchedBy(func(props map[string]interface{}) bool {
		return props["rule_id"] == "rule-cat-low"
	})).Return()

	merchant := "Blue Bottle Coffee"
	err := service.ApplyRulesToTransaction(ctx, &domain.Transaction{ID: "tx-single", UserID: userID, MerchantName: &merchant})
	require.NoError(t, err)

	mockAn.AssertNumberOfCalls(t, "Capture", 1)
	mockAn.AssertExpectations(t)
	mockRepo.AssertExpectations(t)
}

// TestRuleApplicationService_ClearCategoryUncategorized tests that rules with
// TargetKind="clear" remove the category override.
// Issue #404: TargetKind="clear" represents "clear category" action.
func TestRuleApplicationService_ClearCategoryUncategorized(t *testing.T) {
	mockRepo := new(mockRuleApplicationRepository)
	service := NewRuleApplicationService(mockRepo)
	ctx := context.Background()
	userID := "user-123"

	// Rule with TargetKind="clear" = "clear category" action
	rules := []*domain.CategorizationRule{
		{
			ID:             "rule-clear",
			UserID:         userID,
			Name:           "Clear Category Rule",
			Enabled:        true,
			Priority:       10,
			Conditions:     []domain.RuleCondition{{Type: domain.ConditionMerchantContains, Value: "spam"}},
			ConditionLogic: "AND",
			TargetKind:     strPtr("clear"),
			TargetTags:     []string{"cleared"},
			TagBehavior:    "add",
			Source:         "manual",
		},
	}

	mockRepo.On("GetEnabledByUserID", ctx, userID).Return(rules, nil)
	mockRepo.On("IncrementMatchCount", ctx, "rule-clear").Return(nil)

	merchantName := "Spam Merchant"
	existingKind := "pfc"
	existingPrimary := "GENERAL_MERCHANDISE"
	tx := &domain.Transaction{
		ID:                   "tx-123",
		UserID:               userID,
		MerchantName:         &merchantName,
		CategoryOverrideKind: &existingKind,
		PFCPrimaryOverride:   &existingPrimary,
	}

	err := service.ApplyRulesToTransaction(ctx, tx)

	require.NoError(t, err)
	// CategoryOverrideKind should be cleared to nil
	assert.Nil(t, tx.CategoryOverrideKind, "CategoryOverrideKind should be cleared")
	assert.Nil(t, tx.PFCPrimaryOverride, "PFCPrimaryOverride should be cleared")
	assert.Equal(t, domain.CategorySourceRule, tx.CategorySourceField)
	// Tags should still be applied
	assert.Contains(t, tx.Tags, "cleared")
	mockRepo.AssertExpectations(t)
}

// TestRuleApplicationService_ClearCategoryUncategorized_Batch tests that batch
// processing also handles clear category rules correctly.
func TestRuleApplicationService_ClearCategoryUncategorized_Batch(t *testing.T) {
	mockRepo := new(mockRuleApplicationRepository)
	service := NewRuleApplicationService(mockRepo)
	ctx := context.Background()
	userID := "user-123"

	// Rule with TargetKind="clear" = "clear category" action
	rules := []*domain.CategorizationRule{
		{
			ID:             "rule-clear",
			UserID:         userID,
			Name:           "Clear Category Rule",
			Enabled:        true,
			Priority:       10,
			Conditions:     []domain.RuleCondition{{Type: domain.ConditionMerchantContains, Value: "spam"}},
			ConditionLogic: "AND",
			TargetKind:     strPtr("clear"),
			TagBehavior:    "add",
			Source:         "manual",
		},
	}

	mockRepo.On("GetEnabledByUserID", ctx, userID).Return(rules, nil)
	// IncrementMatchCount is NOT called by ApplyRulesToBatch (deferred to EmitBatchSideEffects).

	merchant1 := "Spam Company"
	merchant2 := "Normal Store"
	existingKind := "pfc"
	existingPrimary := "GENERAL_MERCHANDISE"
	txns := []domain.Transaction{
		{ID: "tx-1", UserID: userID, MerchantName: &merchant1, CategoryOverrideKind: &existingKind, PFCPrimaryOverride: &existingPrimary},
		{ID: "tx-2", UserID: userID, MerchantName: &merchant2, CategoryOverrideKind: &existingKind, PFCPrimaryOverride: &existingPrimary},
	}

	_, err := service.ApplyRulesToBatch(ctx, txns, userID)

	require.NoError(t, err)
	// First transaction should have category cleared
	assert.Nil(t, txns[0].CategoryOverrideKind, "CategoryOverrideKind should be cleared for matching transaction")
	assert.Equal(t, domain.CategorySourceRule, txns[0].CategorySourceField)
	// Second transaction should NOT match (no "spam" in name)
	assert.Equal(t, "GENERAL_MERCHANDISE", *txns[1].PFCPrimaryOverride, "Non-matching transaction should keep original category")
	mockRepo.AssertNotCalled(t, "IncrementMatchCount", mock.Anything, mock.Anything)
}

// TestRuleApplicationService_NoCategoryActionNil tests that rules with nil
// TargetKind (no category action) do not modify the category at all.
func TestRuleApplicationService_NoCategoryActionNil(t *testing.T) {
	mockRepo := new(mockRuleApplicationRepository)
	service := NewRuleApplicationService(mockRepo)
	ctx := context.Background()
	userID := "user-123"

	// Rule with nil TargetKind = no category action
	rules := []*domain.CategorizationRule{
		{
			ID:             "rule-tags-only",
			UserID:         userID,
			Name:           "Tags Only Rule",
			Enabled:        true,
			Priority:       10,
			Conditions:     []domain.RuleCondition{{Type: domain.ConditionMerchantContains, Value: "store"}},
			ConditionLogic: "AND",
			TargetTags:     []string{"tagged"},
			TagBehavior:    "add",
			Source:         "manual",
		},
	}

	mockRepo.On("GetEnabledByUserID", ctx, userID).Return(rules, nil)
	mockRepo.On("IncrementMatchCount", ctx, "rule-tags-only").Return(nil)

	merchantName := "Some Store"
	existingKind := "pfc"
	existingPrimary := "GENERAL_MERCHANDISE"
	tx := &domain.Transaction{
		ID:                   "tx-123",
		UserID:               userID,
		MerchantName:         &merchantName,
		CategoryOverrideKind: &existingKind,
		PFCPrimaryOverride:   &existingPrimary,
	}

	err := service.ApplyRulesToTransaction(ctx, tx)

	require.NoError(t, err)
	// Category should NOT be modified (nil TargetKind = no action)
	assert.Equal(t, "GENERAL_MERCHANDISE", *tx.PFCPrimaryOverride, "Category should remain unchanged with nil TargetKind")
	// Tags should still be applied
	assert.Contains(t, tx.Tags, "tagged")
	mockRepo.AssertExpectations(t)
}

// TestRuleApplicationService_ClearCategoryOnlyAction tests that a rule with only
// TargetKind="clear" (no tags) still works as a valid action.
func TestRuleApplicationService_ClearCategoryOnlyAction(t *testing.T) {
	mockRepo := new(mockRuleApplicationRepository)
	service := NewRuleApplicationService(mockRepo)
	ctx := context.Background()
	userID := "user-123"

	// Rule with ONLY TargetKind="clear", no tags
	rules := []*domain.CategorizationRule{
		{
			ID:             "rule-clear-only",
			UserID:         userID,
			Name:           "Clear Only Rule",
			Enabled:        true,
			Priority:       10,
			Conditions:     []domain.RuleCondition{{Type: domain.ConditionMerchantContains, Value: "junk"}},
			ConditionLogic: "AND",
			TargetKind:     strPtr("clear"),
			TagBehavior:    "add",
			Source:         "manual",
		},
	}

	mockRepo.On("GetEnabledByUserID", ctx, userID).Return(rules, nil)
	mockRepo.On("IncrementMatchCount", ctx, "rule-clear-only").Return(nil)

	merchantName := "Junk Mail Inc"
	existingKind := "pfc"
	existingPrimary := "ENTERTAINMENT"
	tx := &domain.Transaction{
		ID:                   "tx-123",
		UserID:               userID,
		MerchantName:         &merchantName,
		CategoryOverrideKind: &existingKind,
		PFCPrimaryOverride:   &existingPrimary,
	}

	err := service.ApplyRulesToTransaction(ctx, tx)

	require.NoError(t, err)
	// CategoryOverrideKind should be cleared to nil
	assert.Nil(t, tx.CategoryOverrideKind, "CategoryOverrideKind should be cleared")
	assert.Nil(t, tx.PFCPrimaryOverride, "PFCPrimaryOverride should be cleared")
	assert.Equal(t, domain.CategorySourceRule, tx.CategorySourceField)
	mockRepo.AssertExpectations(t)
}
