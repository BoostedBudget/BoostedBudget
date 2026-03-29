package services

import (
	"context"
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
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
			ID:             "rule-1",
			UserID:         userID,
			Name:           "Coffee Rule",
			Enabled:        true,
			Priority:       10,
			Conditions:     []domain.RuleCondition{{Type: domain.ConditionMerchantContains, Value: "starbucks"}},
			ConditionLogic: "AND",
			TargetCategory: []string{"Food & Drink", "Coffee"},
			TagBehavior:    "add",
			Source:         "manual",
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
	// Category should be applied as JSON string
	assert.NotNil(t, tx.CategoryOverride)
	assert.Contains(t, *tx.CategoryOverride, "Coffee")
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
			ID:             "rule-1",
			UserID:         userID,
			Name:           "Coffee Rule",
			Enabled:        true,
			Priority:       10,
			Conditions:     []domain.RuleCondition{{Type: domain.ConditionMerchantContains, Value: "starbucks"}},
			ConditionLogic: "AND",
			TargetCategory: []string{"Food & Drink", "Coffee"},
			TagBehavior:    "add",
			Source:         "manual",
		},
	}

	mockRepo.On("GetEnabledByUserID", ctx, userID).Return(rules, nil)

	// Transaction with manual override - should NOT be changed
	merchantName := "Starbucks Coffee"
	userCategory := `["My Custom Category"]`
	tx := &domain.Transaction{
		ID:                  "tx-123",
		UserID:              userID,
		MerchantName:        &merchantName,
		CategorySourceField: domain.CategorySourceManual,
		CategoryOverride:    &userCategory,
	}

	err := service.ApplyRulesToTransaction(ctx, tx)

	require.NoError(t, err)
	// Category should NOT be changed since it was manually set
	assert.Equal(t, domain.CategorySourceManual, tx.CategorySourceField)
	assert.Equal(t, `["My Custom Category"]`, *tx.CategoryOverride)
	// IncrementMatchCount should NOT be called since we skipped the rule
	mockRepo.AssertNotCalled(t, "IncrementMatchCount", mock.Anything, mock.Anything)
}

// TestRuleApplicationService_ApplyTags tests that tags are applied from all matching rules
func TestRuleApplicationService_ApplyTags(t *testing.T) {
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
			TargetCategory: []string{"Food & Drink"},
			TargetTags:     []string{"coffee", "morning"},
			TagBehavior:    "add",
			Source:         "manual",
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
			ID:             "rule-1",
			UserID:         userID,
			Name:           "Coffee Rule",
			Enabled:        true,
			Priority:       10,
			Conditions:     []domain.RuleCondition{{Type: domain.ConditionMerchantContains, Value: "coffee"}},
			ConditionLogic: "AND",
			TargetCategory: []string{"Food & Drink", "Coffee"},
			TagBehavior:    "add",
			Source:         "manual",
		},
	}

	mockRepo.On("GetEnabledByUserID", ctx, userID).Return(rules, nil)
	mockRepo.On("IncrementMatchCount", ctx, "rule-1").Return(nil)

	merchant1 := "Starbucks Coffee"
	merchant2 := "Walmart"
	txns := []domain.Transaction{
		{ID: "tx-1", UserID: userID, MerchantName: &merchant1},
		{ID: "tx-2", UserID: userID, MerchantName: &merchant2},
	}

	err := service.ApplyRulesToBatch(ctx, txns, userID)

	require.NoError(t, err)
	// First transaction should match
	assert.Equal(t, domain.CategorySourceRule, txns[0].CategorySourceField)
	// Second transaction should NOT match (no coffee in name)
	assert.Equal(t, domain.CategorySource(""), txns[1].CategorySourceField)
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
	assert.Nil(t, tx.CategoryOverride)
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

// TestRuleApplicationService_ClearCategoryUncategorized tests that rules with empty
// TargetCategory [] clear the category (set to uncategorized).
// Issue #404: Empty array represents "clear category" action.
func TestRuleApplicationService_ClearCategoryUncategorized(t *testing.T) {
	mockRepo := new(mockRuleApplicationRepository)
	service := NewRuleApplicationService(mockRepo)
	ctx := context.Background()
	userID := "user-123"

	// Rule with empty TargetCategory = "clear category" action
	rules := []*domain.CategorizationRule{
		{
			ID:             "rule-clear",
			UserID:         userID,
			Name:           "Clear Category Rule",
			Enabled:        true,
			Priority:       10,
			Conditions:     []domain.RuleCondition{{Type: domain.ConditionMerchantContains, Value: "spam"}},
			ConditionLogic: "AND",
			TargetCategory: []string{}, // Empty array = clear category
			TargetTags:     []string{"cleared"},
			TagBehavior:    "add",
			Source:         "manual",
		},
	}

	mockRepo.On("GetEnabledByUserID", ctx, userID).Return(rules, nil)
	mockRepo.On("IncrementMatchCount", ctx, "rule-clear").Return(nil)

	merchantName := "Spam Merchant"
	existingCategory := `["Shopping","Electronics"]`
	tx := &domain.Transaction{
		ID:               "tx-123",
		UserID:           userID,
		MerchantName:     &merchantName,
		CategoryOverride: &existingCategory,
	}

	err := service.ApplyRulesToTransaction(ctx, tx)

	require.NoError(t, err)
	// CategoryOverride should be set to empty JSON array "[]"
	require.NotNil(t, tx.CategoryOverride, "CategoryOverride should not be nil")
	assert.Equal(t, "[]", *tx.CategoryOverride, "Category should be cleared to empty array")
	assert.Equal(t, domain.CategorySourceRule, tx.CategorySourceField)
	// Tags should still be applied
	assert.Contains(t, tx.Tags, "cleared")
	mockRepo.AssertExpectations(t)
}

// TestRuleApplicationService_ClearCategoryUncategorized_Batch tests that batch
// processing also handles clear category rules correctly.
// Issue #404: Empty array represents "clear category" action.
func TestRuleApplicationService_ClearCategoryUncategorized_Batch(t *testing.T) {
	mockRepo := new(mockRuleApplicationRepository)
	service := NewRuleApplicationService(mockRepo)
	ctx := context.Background()
	userID := "user-123"

	// Rule with empty TargetCategory = "clear category" action
	rules := []*domain.CategorizationRule{
		{
			ID:             "rule-clear",
			UserID:         userID,
			Name:           "Clear Category Rule",
			Enabled:        true,
			Priority:       10,
			Conditions:     []domain.RuleCondition{{Type: domain.ConditionMerchantContains, Value: "spam"}},
			ConditionLogic: "AND",
			TargetCategory: []string{}, // Empty array = clear category
			TagBehavior:    "add",
			Source:         "manual",
		},
	}

	mockRepo.On("GetEnabledByUserID", ctx, userID).Return(rules, nil)
	mockRepo.On("IncrementMatchCount", ctx, "rule-clear").Return(nil)

	merchant1 := "Spam Company"
	merchant2 := "Normal Store"
	existingCategory := `["Shopping"]`
	txns := []domain.Transaction{
		{ID: "tx-1", UserID: userID, MerchantName: &merchant1, CategoryOverride: &existingCategory},
		{ID: "tx-2", UserID: userID, MerchantName: &merchant2, CategoryOverride: &existingCategory},
	}

	err := service.ApplyRulesToBatch(ctx, txns, userID)

	require.NoError(t, err)
	// First transaction should have category cleared
	require.NotNil(t, txns[0].CategoryOverride)
	assert.Equal(t, "[]", *txns[0].CategoryOverride, "Category should be cleared to empty array")
	assert.Equal(t, domain.CategorySourceRule, txns[0].CategorySourceField)
	// Second transaction should NOT match (no "spam" in name)
	assert.Equal(t, `["Shopping"]`, *txns[1].CategoryOverride, "Non-matching transaction should keep original category")
}

// TestRuleApplicationService_NoCategoryActionNil tests that rules with nil
// TargetCategory (not empty array) do not modify the category at all.
// Issue #404: Distinguish nil (no action) from [] (clear action).
func TestRuleApplicationService_NoCategoryActionNil(t *testing.T) {
	mockRepo := new(mockRuleApplicationRepository)
	service := NewRuleApplicationService(mockRepo)
	ctx := context.Background()
	userID := "user-123"

	// Rule with nil TargetCategory = no category action
	rules := []*domain.CategorizationRule{
		{
			ID:             "rule-tags-only",
			UserID:         userID,
			Name:           "Tags Only Rule",
			Enabled:        true,
			Priority:       10,
			Conditions:     []domain.RuleCondition{{Type: domain.ConditionMerchantContains, Value: "store"}},
			ConditionLogic: "AND",
			TargetCategory: nil, // nil = no category action
			TargetTags:     []string{"tagged"},
			TagBehavior:    "add",
			Source:         "manual",
		},
	}

	mockRepo.On("GetEnabledByUserID", ctx, userID).Return(rules, nil)
	mockRepo.On("IncrementMatchCount", ctx, "rule-tags-only").Return(nil)

	merchantName := "Some Store"
	existingCategory := `["Shopping"]`
	tx := &domain.Transaction{
		ID:               "tx-123",
		UserID:           userID,
		MerchantName:     &merchantName,
		CategoryOverride: &existingCategory,
	}

	err := service.ApplyRulesToTransaction(ctx, tx)

	require.NoError(t, err)
	// Category should NOT be modified (nil = no action)
	assert.Equal(t, `["Shopping"]`, *tx.CategoryOverride, "Category should remain unchanged with nil TargetCategory")
	// Tags should still be applied
	assert.Contains(t, tx.Tags, "tagged")
	mockRepo.AssertExpectations(t)
}

// TestRuleApplicationService_ClearCategoryOnlyAction tests that a rule with only
// empty TargetCategory (no tags) still works as a valid action.
// Issue #404: Empty array [] is a valid action even without tags.
func TestRuleApplicationService_ClearCategoryOnlyAction(t *testing.T) {
	mockRepo := new(mockRuleApplicationRepository)
	service := NewRuleApplicationService(mockRepo)
	ctx := context.Background()
	userID := "user-123"

	// Rule with ONLY empty TargetCategory, no tags
	rules := []*domain.CategorizationRule{
		{
			ID:             "rule-clear-only",
			UserID:         userID,
			Name:           "Clear Only Rule",
			Enabled:        true,
			Priority:       10,
			Conditions:     []domain.RuleCondition{{Type: domain.ConditionMerchantContains, Value: "junk"}},
			ConditionLogic: "AND",
			TargetCategory: []string{}, // Empty array = clear category
			TargetTags:     nil,        // No tag action
			TagBehavior:    "add",
			Source:         "manual",
		},
	}

	mockRepo.On("GetEnabledByUserID", ctx, userID).Return(rules, nil)
	mockRepo.On("IncrementMatchCount", ctx, "rule-clear-only").Return(nil)

	merchantName := "Junk Mail Inc"
	existingCategory := `["Entertainment"]`
	tx := &domain.Transaction{
		ID:               "tx-123",
		UserID:           userID,
		MerchantName:     &merchantName,
		CategoryOverride: &existingCategory,
	}

	err := service.ApplyRulesToTransaction(ctx, tx)

	require.NoError(t, err)
	// CategoryOverride should be set to empty JSON array "[]"
	require.NotNil(t, tx.CategoryOverride, "CategoryOverride should not be nil")
	assert.Equal(t, "[]", *tx.CategoryOverride, "Category should be cleared to empty array")
	assert.Equal(t, domain.CategorySourceRule, tx.CategorySourceField)
	mockRepo.AssertExpectations(t)
}
