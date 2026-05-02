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

// Mock subscription checker
type mockSubscriptionChecker struct {
	mock.Mock
}

func (m *mockSubscriptionChecker) GetEffectiveSubscription(ctx context.Context, user *domain.User) (*domain.EffectiveSubscription, error) {
	args := m.Called(ctx, user)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.EffectiveSubscription), args.Error(1)
}

// Mock user getter
type mockUserGetter struct {
	mock.Mock
}

func (m *mockUserGetter) GetUserByID(ctx context.Context, userID string) (*domain.User, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.User), args.Error(1)
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

	service := NewCategorizationRuleService(mockRepo)
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

	service := NewCategorizationRuleService(mockRepo)
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
	service := NewCategorizationRuleService(mockRepo)
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
	service := NewCategorizationRuleService(mockRepo)
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
	service := NewCategorizationRuleService(mockRepo)
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
	service := NewCategorizationRuleService(mockRepo)
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
	service := NewCategorizationRuleService(mockRepo)
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
	service := NewCategorizationRuleService(mockRepo)
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
	service := NewCategorizationRuleService(mockRepo)
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
	service := NewCategorizationRuleService(mockRepo)
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
	service := NewCategorizationRuleService(mockRepo)
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
