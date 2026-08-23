package services

import (
	"context"
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// Mock repository for testing
type mockRuleRepository struct {
	mock.Mock
}

func (m *mockRuleRepository) Create(ctx context.Context, rule *domain.CategorizationRule) error {
	args := m.Called(ctx, rule)
	return args.Error(0)
}

func (m *mockRuleRepository) GetByID(ctx context.Context, id string) (*domain.CategorizationRule, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.CategorizationRule), args.Error(1)
}

func (m *mockRuleRepository) GetByUserID(ctx context.Context, userID string) ([]*domain.CategorizationRule, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.CategorizationRule), args.Error(1)
}

func (m *mockRuleRepository) GetEnabledByUserID(ctx context.Context, userID string) ([]*domain.CategorizationRule, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.CategorizationRule), args.Error(1)
}

func (m *mockRuleRepository) GetByName(ctx context.Context, userID, name string) (*domain.CategorizationRule, error) {
	args := m.Called(ctx, userID, name)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.CategorizationRule), args.Error(1)
}

func (m *mockRuleRepository) Update(ctx context.Context, rule *domain.CategorizationRule) error {
	args := m.Called(ctx, rule)
	return args.Error(0)
}

func (m *mockRuleRepository) Delete(ctx context.Context, id string) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *mockRuleRepository) CountByUserID(ctx context.Context, userID string) (int, error) {
	args := m.Called(ctx, userID)
	return args.Int(0), args.Error(1)
}

func (m *mockRuleRepository) IncrementMatchCount(ctx context.Context, ruleID string) error {
	args := m.Called(ctx, ruleID)
	return args.Error(0)
}

func (m *mockRuleRepository) IncrementOverrideCount(ctx context.Context, ruleID string) error {
	args := m.Called(ctx, ruleID)
	return args.Error(0)
}

// Helper to create a test request
func createTestRuleRequest() *domain.CreateCategorizationRuleRequest {
	return &domain.CreateCategorizationRuleRequest{
		Name: "Coffee Shops",
		Conditions: []domain.RuleCondition{
			{Type: domain.ConditionMerchantContains, Value: "starbucks"},
		},
		ConditionLogic:   "AND",
		TargetKind:       ptrStr("pfc"),
		TargetPFCPrimary: ptrStr("FOOD_AND_DRINK"),
		TagBehavior:      "add",
	}
}

// TestCategorizationRuleService_CreateRule tests successful rule creation
func TestCategorizationRuleService_CreateRule(t *testing.T) {
	mockRepo := new(mockRuleRepository)

	service := NewCategorizationRuleService(mockRepo, new(MockCustomCategoryRepository))
	ctx := context.Background()
	userID := "user-123"

	mockRepo.On("Create", ctx, mock.AnythingOfType("*domain.CategorizationRule")).Return(nil)

	req := createTestRuleRequest()
	rule, err := service.CreateRule(ctx, userID, req)

	require.NoError(t, err)
	assert.NotNil(t, rule)
	assert.Equal(t, userID, rule.UserID)
	assert.Equal(t, "Coffee Shops", rule.Name)
	assert.Equal(t, "manual", rule.Source)
	mockRepo.AssertExpectations(t)
}

// TestCategorizationRuleService_CreateRule_ManyRules tests creation with many existing rules (unlimited)
func TestCategorizationRuleService_CreateRule_ManyRules(t *testing.T) {
	mockRepo := new(mockRuleRepository)

	service := NewCategorizationRuleService(mockRepo, new(MockCustomCategoryRepository))
	ctx := context.Background()
	userID := "user-123"

	// Rules are unlimited for all tiers, so creating with many existing rules should succeed
	mockRepo.On("Create", ctx, mock.AnythingOfType("*domain.CategorizationRule")).Return(nil)

	req := createTestRuleRequest()
	rule, err := service.CreateRule(ctx, userID, req)

	require.NoError(t, err)
	assert.NotNil(t, rule)
	assert.Equal(t, userID, rule.UserID)
	mockRepo.AssertExpectations(t)
}

// TestCategorizationRuleService_GetUserRules tests retrieving all rules for a user
func TestCategorizationRuleService_GetUserRules(t *testing.T) {
	mockRepo := new(mockRuleRepository)
	service := NewCategorizationRuleService(mockRepo, new(MockCustomCategoryRepository))
	ctx := context.Background()
	userID := "user-123"

	expectedRules := []*domain.CategorizationRule{
		{ID: "rule-1", UserID: userID, Name: "Rule 1"},
		{ID: "rule-2", UserID: userID, Name: "Rule 2"},
	}

	mockRepo.On("GetByUserID", ctx, userID).Return(expectedRules, nil)

	rules, err := service.GetUserRules(ctx, userID)

	require.NoError(t, err)
	assert.Len(t, rules, 2)
	mockRepo.AssertExpectations(t)
}

// TestCategorizationRuleService_GetRuleByID tests retrieving a single rule
func TestCategorizationRuleService_GetRuleByID(t *testing.T) {
	mockRepo := new(mockRuleRepository)
	service := NewCategorizationRuleService(mockRepo, new(MockCustomCategoryRepository))
	ctx := context.Background()
	userID := "user-123"
	ruleID := "rule-123"

	expectedRule := &domain.CategorizationRule{
		ID:     ruleID,
		UserID: userID,
		Name:   "Test Rule",
	}

	mockRepo.On("GetByID", ctx, ruleID).Return(expectedRule, nil)

	rule, err := service.GetRuleByID(ctx, userID, ruleID)

	require.NoError(t, err)
	assert.Equal(t, ruleID, rule.ID)
	mockRepo.AssertExpectations(t)
}

// TestCategorizationRuleService_GetRuleByID_NotOwner tests access control
func TestCategorizationRuleService_GetRuleByID_NotOwner(t *testing.T) {
	mockRepo := new(mockRuleRepository)
	service := NewCategorizationRuleService(mockRepo, new(MockCustomCategoryRepository))
	ctx := context.Background()
	userID := "user-123"
	ruleID := "rule-123"

	expectedRule := &domain.CategorizationRule{
		ID:     ruleID,
		UserID: "other-user", // Different user
		Name:   "Test Rule",
	}

	mockRepo.On("GetByID", ctx, ruleID).Return(expectedRule, nil)

	rule, err := service.GetRuleByID(ctx, userID, ruleID)

	assert.Error(t, err)
	assert.Nil(t, rule)
	assert.Equal(t, domain.ErrRuleNotFound, err)
}

// TestCategorizationRuleService_UpdateRule tests successful rule update
func TestCategorizationRuleService_UpdateRule(t *testing.T) {
	mockRepo := new(mockRuleRepository)
	service := NewCategorizationRuleService(mockRepo, new(MockCustomCategoryRepository))
	ctx := context.Background()
	userID := "user-123"
	ruleID := "rule-123"

	existingRule := &domain.CategorizationRule{
		ID:               ruleID,
		UserID:           userID,
		Name:             "Old Name",
		Enabled:          true,
		Priority:         5,
		Conditions:       []domain.RuleCondition{{Type: domain.ConditionMerchantContains, Value: "test"}},
		ConditionLogic:   "AND",
		TargetKind:       ptrStr("pfc"),
		TargetPFCPrimary: ptrStr("FOOD_AND_DRINK"),
		TagBehavior:      "add",
		Source:           "manual",
	}

	mockRepo.On("GetByID", ctx, ruleID).Return(existingRule, nil)
	mockRepo.On("Update", ctx, mock.AnythingOfType("*domain.CategorizationRule")).Return(nil)

	newName := "New Name"
	req := &domain.UpdateCategorizationRuleRequest{
		Name: &newName,
	}

	rule, err := service.UpdateRule(ctx, userID, ruleID, req)

	require.NoError(t, err)
	assert.Equal(t, "New Name", rule.Name)
	mockRepo.AssertExpectations(t)
}

// TestCategorizationRuleService_DeleteRule tests successful rule deletion
func TestCategorizationRuleService_DeleteRule(t *testing.T) {
	mockRepo := new(mockRuleRepository)
	service := NewCategorizationRuleService(mockRepo, new(MockCustomCategoryRepository))
	ctx := context.Background()
	userID := "user-123"
	ruleID := "rule-123"

	existingRule := &domain.CategorizationRule{
		ID:     ruleID,
		UserID: userID,
	}

	mockRepo.On("GetByID", ctx, ruleID).Return(existingRule, nil)
	mockRepo.On("Delete", ctx, ruleID).Return(nil)

	err := service.DeleteRule(ctx, userID, ruleID)

	require.NoError(t, err)
	mockRepo.AssertExpectations(t)
}

// TestCategorizationRuleService_ToggleRule tests enabling/disabling a rule
func TestCategorizationRuleService_ToggleRule(t *testing.T) {
	mockRepo := new(mockRuleRepository)
	service := NewCategorizationRuleService(mockRepo, new(MockCustomCategoryRepository))
	ctx := context.Background()
	userID := "user-123"
	ruleID := "rule-123"

	existingRule := &domain.CategorizationRule{
		ID:               ruleID,
		UserID:           userID,
		Enabled:          true,
		Conditions:       []domain.RuleCondition{{Type: domain.ConditionMerchantContains, Value: "test"}},
		ConditionLogic:   "AND",
		TargetKind:       ptrStr("pfc"),
		TargetPFCPrimary: ptrStr("FOOD_AND_DRINK"),
		TagBehavior:      "add",
		Source:           "manual",
	}

	mockRepo.On("GetByID", ctx, ruleID).Return(existingRule, nil)
	mockRepo.On("Update", ctx, mock.AnythingOfType("*domain.CategorizationRule")).Return(nil)

	rule, err := service.ToggleRule(ctx, userID, ruleID, false)

	require.NoError(t, err)
	assert.False(t, rule.Enabled)
	mockRepo.AssertExpectations(t)
}

// TestCategorizationRuleService_GetRuleByID_PopulatesPFCDisplay asserts that GetRuleByID
// enriches TargetPFCPrimaryDisplay before returning.
func TestCategorizationRuleService_GetRuleByID_PopulatesPFCDisplay(t *testing.T) {
	mockRepo := new(mockRuleRepository)
	service := NewCategorizationRuleService(mockRepo, new(MockCustomCategoryRepository))
	ctx := context.Background()
	userID := "user-123"
	ruleID := "rule-pfc"

	rule := &domain.CategorizationRule{
		ID:               ruleID,
		UserID:           userID,
		Name:             "PFC Rule",
		TargetKind:       ptrStr("pfc"),
		TargetPFCPrimary: ptrStr("FOOD_AND_DRINK"),
		Conditions:       []domain.RuleCondition{{Type: domain.ConditionMerchantContains, Value: "test"}},
		ConditionLogic:   "AND",
		TagBehavior:      "add",
		Source:           "manual",
	}

	mockRepo.On("GetByID", ctx, ruleID).Return(rule, nil)

	result, err := service.GetRuleByID(ctx, userID, ruleID)

	require.NoError(t, err)
	require.NotNil(t, result.TargetPFCPrimaryDisplay, "TargetPFCPrimaryDisplay should be populated")
	assert.Equal(t, "Food And Drink", *result.TargetPFCPrimaryDisplay)
	mockRepo.AssertExpectations(t)
}

// TestCategorizationRuleService_GetUserRules_PopulatesPFCDisplay asserts that GetUserRules
// enriches each rule with display names.
func TestCategorizationRuleService_GetUserRules_PopulatesPFCDisplay(t *testing.T) {
	mockRepo := new(mockRuleRepository)
	service := NewCategorizationRuleService(mockRepo, new(MockCustomCategoryRepository))
	ctx := context.Background()
	userID := "user-123"

	rules := []*domain.CategorizationRule{
		{ID: "rule-1", UserID: userID, Name: "Rule", TargetKind: ptrStr("pfc"), TargetPFCPrimary: ptrStr("TRANSPORTATION")},
	}

	mockRepo.On("GetByUserID", ctx, userID).Return(rules, nil)

	result, err := service.GetUserRules(ctx, userID)

	require.NoError(t, err)
	require.Len(t, result, 1)
	require.NotNil(t, result[0].TargetPFCPrimaryDisplay, "TargetPFCPrimaryDisplay should be populated")
	assert.Equal(t, "Transportation", *result[0].TargetPFCPrimaryDisplay)
	mockRepo.AssertExpectations(t)
}

// TestCategorizationRuleService_GetEnabledRules tests retrieving enabled rules only
func TestCategorizationRuleService_GetEnabledRules(t *testing.T) {
	mockRepo := new(mockRuleRepository)
	service := NewCategorizationRuleService(mockRepo, new(MockCustomCategoryRepository))
	ctx := context.Background()
	userID := "user-123"

	expectedRules := []*domain.CategorizationRule{
		{ID: "rule-1", UserID: userID, Name: "Enabled Rule", Enabled: true},
	}

	mockRepo.On("GetEnabledByUserID", ctx, userID).Return(expectedRules, nil)

	rules, err := service.GetEnabledRules(ctx, userID)

	require.NoError(t, err)
	assert.Len(t, rules, 1)
	assert.True(t, rules[0].Enabled)
	mockRepo.AssertExpectations(t)
}

// TestCategorizationRuleService_GetRuleByID_PopulatesTargetCustomCategoryDisplay asserts enrichment
// of TargetCustomCategoryDisplay for custom-target rules.
func TestCategorizationRuleService_GetRuleByID_PopulatesTargetCustomCategoryDisplay(t *testing.T) {
	mockRepo := new(mockRuleRepository)
	mockCategoryRepo := new(MockCustomCategoryRepository)
	service := NewCategorizationRuleService(mockRepo, mockCategoryRepo)
	ctx := context.Background()
	userID := "user-123"
	ruleID := "rule-custom"
	catID := "cat-uuid-rule"

	rule := &domain.CategorizationRule{
		ID:                     ruleID,
		UserID:                 userID,
		Name:                   "Custom Rule",
		TargetKind:             ptrStr("custom"),
		TargetCustomCategoryID: &catID,
		Conditions:             []domain.RuleCondition{{Type: domain.ConditionMerchantContains, Value: "test"}},
		ConditionLogic:         "AND",
		TagBehavior:            "add",
		Source:                 "manual",
	}

	mockRepo.On("GetByID", ctx, ruleID).Return(rule, nil)
	mockCategoryRepo.On("GetNamesByIDs", ctx, userID, []string{catID}).
		Return(map[string]string{catID: "Coffee Shops"}, nil)

	result, err := service.GetRuleByID(ctx, userID, ruleID)

	require.NoError(t, err)
	require.NotNil(t, result.TargetCustomCategoryDisplay)
	assert.Equal(t, "Coffee Shops", *result.TargetCustomCategoryDisplay)
	mockRepo.AssertExpectations(t)
	mockCategoryRepo.AssertExpectations(t)
}

// TestCategorizationRuleService_GetUserRules_PopulatesTargetCustomCategoryDisplay asserts enrichment
// in list results.
func TestCategorizationRuleService_GetUserRules_PopulatesTargetCustomCategoryDisplay(t *testing.T) {
	mockRepo := new(mockRuleRepository)
	mockCategoryRepo := new(MockCustomCategoryRepository)
	service := NewCategorizationRuleService(mockRepo, mockCategoryRepo)
	ctx := context.Background()
	userID := "user-123"
	catID := "cat-uuid-list"

	rules := []*domain.CategorizationRule{
		{
			ID:                     "rule-1",
			UserID:                 userID,
			Name:                   "Rule",
			TargetKind:             ptrStr("custom"),
			TargetCustomCategoryID: &catID,
		},
	}

	mockRepo.On("GetByUserID", ctx, userID).Return(rules, nil)
	mockCategoryRepo.On("GetNamesByIDs", ctx, userID, []string{catID}).
		Return(map[string]string{catID: "Electronics"}, nil)

	result, err := service.GetUserRules(ctx, userID)

	require.NoError(t, err)
	require.Len(t, result, 1)
	require.NotNil(t, result[0].TargetCustomCategoryDisplay)
	assert.Equal(t, "Electronics", *result[0].TargetCustomCategoryDisplay)
	mockRepo.AssertExpectations(t)
	mockCategoryRepo.AssertExpectations(t)
}

func TestCategorizationRuleService_CreateRule_PopulatesTargetCustomCategoryDisplay(t *testing.T) {
	mockRepo := new(mockRuleRepository)
	mockCategoryRepo := new(MockCustomCategoryRepository)
	service := NewCategorizationRuleService(mockRepo, mockCategoryRepo)
	ctx := context.Background()
	userID := "user-123"
	catID := "cat-uuid-create"

	mockRepo.On("Create", ctx, mock.AnythingOfType("*domain.CategorizationRule")).Return(nil)
	// CreateRule validates through the LIVE variant (#1569 Task 9) and enriches the response
	// through the tombstone-blind one.
	mockCategoryRepo.On("GetLiveNamesByIDs", ctx, userID, []string{catID}).
		Return(map[string]string{catID: "Travel"}, nil)
	mockCategoryRepo.On("GetNamesByIDs", ctx, userID, []string{catID}).
		Return(map[string]string{catID: "Travel"}, nil)

	req := &domain.CreateCategorizationRuleRequest{
		Name:                   "Custom Create Rule",
		Conditions:             []domain.RuleCondition{{Type: domain.ConditionMerchantContains, Value: "airline"}},
		ConditionLogic:         "AND",
		TargetKind:             ptrStr("custom"),
		TargetCustomCategoryID: &catID,
		TagBehavior:            "add",
	}

	rule, err := service.CreateRule(ctx, userID, req)

	require.NoError(t, err)
	require.NotNil(t, rule.TargetCustomCategoryDisplay)
	assert.Equal(t, "Travel", *rule.TargetCustomCategoryDisplay)
	mockRepo.AssertExpectations(t)
	mockCategoryRepo.AssertExpectations(t)
}

func TestCategorizationRuleService_UpdateRule_PopulatesTargetCustomCategoryDisplay(t *testing.T) {
	mockRepo := new(mockRuleRepository)
	mockCategoryRepo := new(MockCustomCategoryRepository)
	service := NewCategorizationRuleService(mockRepo, mockCategoryRepo)
	ctx := context.Background()
	userID := "user-123"
	ruleID := "rule-update"
	catID := "cat-uuid-update"

	existing := &domain.CategorizationRule{
		ID:                     ruleID,
		UserID:                 userID,
		Name:                   "Old Name",
		Enabled:                true,
		TargetKind:             ptrStr("custom"),
		TargetCustomCategoryID: &catID,
		Conditions:             []domain.RuleCondition{{Type: domain.ConditionMerchantContains, Value: "store"}},
		ConditionLogic:         "AND",
		TagBehavior:            "add",
		Source:                 "manual",
	}

	mockRepo.On("GetByID", ctx, ruleID).Return(existing, nil)
	mockRepo.On("Update", ctx, mock.AnythingOfType("*domain.CategorizationRule")).Return(nil)
	mockCategoryRepo.On("GetNamesByIDs", ctx, userID, []string{catID}).
		Return(map[string]string{catID: "Shopping"}, nil)

	newName := "New Name"
	req := &domain.UpdateCategorizationRuleRequest{Name: &newName}

	rule, err := service.UpdateRule(ctx, userID, ruleID, req)

	require.NoError(t, err)
	require.NotNil(t, rule.TargetCustomCategoryDisplay)
	assert.Equal(t, "Shopping", *rule.TargetCustomCategoryDisplay)
	mockRepo.AssertExpectations(t)
	mockCategoryRepo.AssertExpectations(t)
}

func TestCategorizationRuleService_ToggleRule_PopulatesTargetCustomCategoryDisplay(t *testing.T) {
	mockRepo := new(mockRuleRepository)
	mockCategoryRepo := new(MockCustomCategoryRepository)
	service := NewCategorizationRuleService(mockRepo, mockCategoryRepo)
	ctx := context.Background()
	userID := "user-123"
	ruleID := "rule-toggle"
	catID := "cat-uuid-toggle"

	existing := &domain.CategorizationRule{
		ID:                     ruleID,
		UserID:                 userID,
		Enabled:                true,
		TargetKind:             ptrStr("custom"),
		TargetCustomCategoryID: &catID,
		Conditions:             []domain.RuleCondition{{Type: domain.ConditionMerchantContains, Value: "shop"}},
		ConditionLogic:         "AND",
		TagBehavior:            "add",
		Source:                 "manual",
	}

	mockRepo.On("GetByID", ctx, ruleID).Return(existing, nil)
	mockRepo.On("Update", ctx, mock.AnythingOfType("*domain.CategorizationRule")).Return(nil)
	mockCategoryRepo.On("GetNamesByIDs", ctx, userID, []string{catID}).
		Return(map[string]string{catID: "Dining"}, nil)

	rule, err := service.ToggleRule(ctx, userID, ruleID, false)

	require.NoError(t, err)
	require.NotNil(t, rule.TargetCustomCategoryDisplay)
	assert.Equal(t, "Dining", *rule.TargetCustomCategoryDisplay)
	mockRepo.AssertExpectations(t)
	mockCategoryRepo.AssertExpectations(t)
}

func TestCategorizationRuleService_GetEnabledRules_PopulatesTargetCustomCategoryDisplay(t *testing.T) {
	mockRepo := new(mockRuleRepository)
	mockCategoryRepo := new(MockCustomCategoryRepository)
	service := NewCategorizationRuleService(mockRepo, mockCategoryRepo)
	ctx := context.Background()
	userID := "user-123"
	catID := "cat-uuid-enabled"

	rules := []*domain.CategorizationRule{
		{
			ID:                     "rule-enabled",
			UserID:                 userID,
			Name:                   "Enabled Rule",
			Enabled:                true,
			TargetKind:             ptrStr("custom"),
			TargetCustomCategoryID: &catID,
		},
	}

	mockRepo.On("GetEnabledByUserID", ctx, userID).Return(rules, nil)
	mockCategoryRepo.On("GetNamesByIDs", ctx, userID, []string{catID}).
		Return(map[string]string{catID: "Utilities"}, nil)

	result, err := service.GetEnabledRules(ctx, userID)

	require.NoError(t, err)
	require.Len(t, result, 1)
	require.NotNil(t, result[0].TargetCustomCategoryDisplay)
	assert.Equal(t, "Utilities", *result[0].TargetCustomCategoryDisplay)
	mockRepo.AssertExpectations(t)
	mockCategoryRepo.AssertExpectations(t)
}

// =============================================================================
// Issue #1540: target_custom_category_id ownership validation (IDOR)
// =============================================================================

// TestCategorizationRuleService_CreateRule_RejectsCrossUserCustomCategory verifies that
// CreateRule rejects a target_custom_category_id belonging to another user before persisting.
func TestCategorizationRuleService_CreateRule_RejectsCrossUserCustomCategory(t *testing.T) {
	mockRepo := new(mockRuleRepository)
	mockCategoryRepo := new(MockCustomCategoryRepository)
	service := NewCategorizationRuleService(mockRepo, mockCategoryRepo)
	ctx := context.Background()
	userID := "user-123"
	otherUsersCategoryID := "other-user-category-id"

	req := createTestRuleRequest()
	req.TargetKind = ptrStr("custom")
	req.TargetPFCPrimary = nil
	req.TargetCustomCategoryID = &otherUsersCategoryID

	// GetLiveNamesByIDs is user-scoped AND tombstone-filtered (#1569 Task 9); a category owned
	// by someone else is absent from the map.
	mockCategoryRepo.On("GetLiveNamesByIDs", ctx, userID, []string{otherUsersCategoryID}).
		Return(map[string]string{}, nil)

	rule, err := service.CreateRule(ctx, userID, req)

	assert.ErrorIs(t, err, domain.ErrCategoryNotFound)
	assert.Nil(t, rule)
	mockRepo.AssertNotCalled(t, "Create", mock.Anything, mock.Anything)
	mockCategoryRepo.AssertExpectations(t)
}

// TestCategorizationRuleService_UpdateRule_RejectsCrossUserCustomCategory mirrors the
// CreateRule case for UpdateRule.
func TestCategorizationRuleService_UpdateRule_RejectsCrossUserCustomCategory(t *testing.T) {
	mockRepo := new(mockRuleRepository)
	mockCategoryRepo := new(MockCustomCategoryRepository)
	service := NewCategorizationRuleService(mockRepo, mockCategoryRepo)
	ctx := context.Background()
	userID := "user-123"
	ruleID := "rule-123"
	otherUsersCategoryID := "other-user-category-id"

	existingRule := &domain.CategorizationRule{
		ID:               ruleID,
		UserID:           userID,
		Name:             "Old Name",
		Enabled:          true,
		Priority:         5,
		Conditions:       []domain.RuleCondition{{Type: domain.ConditionMerchantContains, Value: "test"}},
		ConditionLogic:   "AND",
		TargetKind:       ptrStr("pfc"),
		TargetPFCPrimary: ptrStr("FOOD_AND_DRINK"),
		TagBehavior:      "add",
		Source:           "manual",
	}

	mockRepo.On("GetByID", ctx, ruleID).Return(existingRule, nil)
	mockCategoryRepo.On("GetLiveNamesByIDs", ctx, userID, []string{otherUsersCategoryID}).
		Return(map[string]string{}, nil)

	req := &domain.UpdateCategorizationRuleRequest{
		TargetCustomCategoryID: &otherUsersCategoryID,
	}

	rule, err := service.UpdateRule(ctx, userID, ruleID, req)

	assert.ErrorIs(t, err, domain.ErrCategoryNotFound)
	assert.Nil(t, rule)
	mockRepo.AssertNotCalled(t, "Update", mock.Anything, mock.Anything)
	mockCategoryRepo.AssertExpectations(t)
}

// ---------------------------------------------------------------------------------------------
// Issue #1569 Task 9, review finding I1: a rule DeleteWithReassignment disabled must not be
// ARMED AGAIN onto the tombstoned category it still names.
//
// The liveness gate used to live inside `if req.TargetCustomCategoryID != nil`, so
// `PATCH /api/v1/rules/{id}` with `{"enabled": true}` and no target field skipped it entirely and
// put the rule straight back into GetEnabledByUserID with target_custom_category_id pointing at a
// tombstoned row — defeating step 2's entire justification in one API call. ToggleRule, the other
// re-enable path, had no gate at all.
//
// Both tests assert the repository is never asked to persist the change: refusing after the write
// would be no protection.
// ---------------------------------------------------------------------------------------------

// ruleWithTombstonedTarget builds an already-DISABLED rule that still names a custom category —
// exactly the state DeleteWithReassignment leaves behind.
func ruleWithTombstonedTarget(userID, ruleID, categoryID string) *domain.CategorizationRule {
	return &domain.CategorizationRule{
		ID:                     ruleID,
		UserID:                 userID,
		Name:                   "File Blue Bottle as Coffee",
		Enabled:                false,
		Priority:               5,
		Conditions:             []domain.RuleCondition{{Type: domain.ConditionMerchantContains, Value: "Blue Bottle"}},
		ConditionLogic:         "AND",
		TargetKind:             ptrStr("custom"),
		TargetCustomCategoryID: &categoryID,
		TagBehavior:            "add",
		Source:                 "manual",
	}
}

func TestCategorizationRuleService_UpdateRule_RefusesToArmOntoATombstonedCategory(t *testing.T) {
	mockRepo := new(mockRuleRepository)
	mockCategoryRepo := new(MockCustomCategoryRepository)
	service := NewCategorizationRuleService(mockRepo, mockCategoryRepo)
	ctx := context.Background()
	userID, ruleID, categoryID := "user-123", "rule-123", "category-123"

	mockRepo.On("GetByID", ctx, ruleID).
		Return(ruleWithTombstonedTarget(userID, ruleID, categoryID), nil)
	// The category is gone, so the LIVE lookup returns nothing for it.
	mockCategoryRepo.On("GetLiveNamesByIDs", ctx, userID, []string{categoryID}).
		Return(map[string]string{}, nil)

	enabled := true
	// No TargetCustomCategoryID: this is the request shape that used to skip the gate.
	rule, err := service.UpdateRule(ctx, userID, ruleID, &domain.UpdateCategorizationRuleRequest{
		Enabled: &enabled,
	})

	assert.ErrorIs(t, err, domain.ErrCategoryNotFound)
	assert.Nil(t, rule)
	mockRepo.AssertNotCalled(t, "Update", mock.Anything, mock.Anything)
	mockCategoryRepo.AssertExpectations(t)
}

// Disabling is always allowed — the gate is about ARMING, not about touching the rule at all. A
// user must still be able to edit or switch off a rule whose category has gone.
func TestCategorizationRuleService_UpdateRule_DisablingNeverConsultsTheCategory(t *testing.T) {
	mockRepo := new(mockRuleRepository)
	mockCategoryRepo := new(MockCustomCategoryRepository)
	service := NewCategorizationRuleService(mockRepo, mockCategoryRepo)
	ctx := context.Background()
	userID, ruleID, categoryID := "user-123", "rule-123", "category-123"

	existing := ruleWithTombstonedTarget(userID, ruleID, categoryID)
	existing.Enabled = true
	mockRepo.On("GetByID", ctx, ruleID).Return(existing, nil)
	mockRepo.On("Update", ctx, mock.AnythingOfType("*domain.CategorizationRule")).Return(nil)
	// Enrichment still resolves the historical name through the tombstone-BLIND variant.
	mockCategoryRepo.On("GetNamesByIDs", ctx, userID, []string{categoryID}).
		Return(map[string]string{categoryID: "Coffee"}, nil)

	disabled := false
	rule, err := service.UpdateRule(ctx, userID, ruleID, &domain.UpdateCategorizationRuleRequest{
		Enabled: &disabled,
	})

	require.NoError(t, err)
	require.NotNil(t, rule)
	assert.False(t, rule.Enabled)
	mockCategoryRepo.AssertNotCalled(t, "GetLiveNamesByIDs", mock.Anything, mock.Anything, mock.Anything)
}

func TestCategorizationRuleService_ToggleRule_RefusesToArmOntoATombstonedCategory(t *testing.T) {
	mockRepo := new(mockRuleRepository)
	mockCategoryRepo := new(MockCustomCategoryRepository)
	service := NewCategorizationRuleService(mockRepo, mockCategoryRepo)
	ctx := context.Background()
	userID, ruleID, categoryID := "user-123", "rule-123", "category-123"

	mockRepo.On("GetByID", ctx, ruleID).
		Return(ruleWithTombstonedTarget(userID, ruleID, categoryID), nil)
	mockCategoryRepo.On("GetLiveNamesByIDs", ctx, userID, []string{categoryID}).
		Return(map[string]string{}, nil)

	rule, err := service.ToggleRule(ctx, userID, ruleID, true)

	assert.ErrorIs(t, err, domain.ErrCategoryNotFound)
	assert.Nil(t, rule)
	mockRepo.AssertNotCalled(t, "Update", mock.Anything, mock.Anything)
	mockCategoryRepo.AssertExpectations(t)
}
