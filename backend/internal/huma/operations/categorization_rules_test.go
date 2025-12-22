package operations

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockCategorizationRuleService is a mock implementation of services.CategorizationRuleService
type mockCategorizationRuleService struct {
	rules       []*domain.CategorizationRule
	rule        *domain.CategorizationRule
	createErr   error
	getErr      error
	updateErr   error
	deleteErr   error
	toggleErr   error
}

func (m *mockCategorizationRuleService) GetUserRules(ctx context.Context, userID string) ([]*domain.CategorizationRule, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	return m.rules, nil
}

func (m *mockCategorizationRuleService) GetRuleByID(ctx context.Context, userID, ruleID string) (*domain.CategorizationRule, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	return m.rule, nil
}

func (m *mockCategorizationRuleService) CreateRule(ctx context.Context, userID string, req *domain.CreateCategorizationRuleRequest) (*domain.CategorizationRule, error) {
	if m.createErr != nil {
		return nil, m.createErr
	}
	return m.rule, nil
}

func (m *mockCategorizationRuleService) UpdateRule(ctx context.Context, userID, ruleID string, req *domain.UpdateCategorizationRuleRequest) (*domain.CategorizationRule, error) {
	if m.updateErr != nil {
		return nil, m.updateErr
	}
	return m.rule, nil
}

func (m *mockCategorizationRuleService) DeleteRule(ctx context.Context, userID, ruleID string) error {
	return m.deleteErr
}

func (m *mockCategorizationRuleService) ToggleRule(ctx context.Context, userID, ruleID string, enabled bool) (*domain.CategorizationRule, error) {
	if m.toggleErr != nil {
		return nil, m.toggleErr
	}
	// Update the enabled status in the mock
	if m.rule != nil {
		m.rule.Enabled = enabled
	}
	return m.rule, nil
}

func (m *mockCategorizationRuleService) GetEnabledRules(ctx context.Context, userID string) ([]*domain.CategorizationRule, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	var enabledRules []*domain.CategorizationRule
	for _, r := range m.rules {
		if r.Enabled {
			enabledRules = append(enabledRules, r)
		}
	}
	return enabledRules, nil
}

// Helper to create a test rule
func createTestRule(userID, name string) *domain.CategorizationRule {
	now := time.Now()
	return &domain.CategorizationRule{
		ID:             "rule-123",
		UserID:         userID,
		Name:           name,
		Enabled:        true,
		Priority:       1,
		Conditions:     []domain.RuleCondition{{Type: domain.ConditionMerchantContains, Value: "grocery"}},
		ConditionLogic: "AND",
		TargetTags:     []string{"groceries"},
		TagBehavior:    "add",
		Source:         "manual",
		MatchCount:     0,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
}

// TestGetUserRules_Success tests successful retrieval of user rules.
func TestGetUserRules_Success(t *testing.T) {
	userID := "user-123"
	mockService := &mockCategorizationRuleService{
		rules: []*domain.CategorizationRule{
			createTestRule(userID, "Grocery Rule"),
			createTestRule(userID, "Entertainment Rule"),
		},
	}

	handler := NewCategorizationRuleHandler(mockService)

	input := &GetUserRulesInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
	}

	output, err := handler.GetUserRules(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Len(t, output.Body, 2)
	assert.Equal(t, "Grocery Rule", output.Body[0].Name)
	assert.Equal(t, "Entertainment Rule", output.Body[1].Name)
}

// TestGetUserRules_EmptyList tests that empty array is returned instead of null.
func TestGetUserRules_EmptyList(t *testing.T) {
	mockService := &mockCategorizationRuleService{
		rules: nil,
	}

	handler := NewCategorizationRuleHandler(mockService)

	input := &GetUserRulesInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}

	output, err := handler.GetUserRules(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.NotNil(t, output.Body, "Should return empty array, not nil")
	assert.Len(t, output.Body, 0)
}

// TestGetRuleByID_Success tests successful retrieval of a rule by ID.
func TestGetRuleByID_Success(t *testing.T) {
	userID := "user-123"
	mockService := &mockCategorizationRuleService{
		rule: createTestRule(userID, "Grocery Rule"),
	}

	handler := NewCategorizationRuleHandler(mockService)

	input := &GetRuleByIDInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
		ID:                 "rule-123",
	}

	output, err := handler.GetRuleByID(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, "Grocery Rule", output.Body.Name)
	assert.True(t, output.Body.Enabled)
}

// TestGetRuleByID_NotFound tests 404 when rule doesn't exist.
func TestGetRuleByID_NotFound(t *testing.T) {
	mockService := &mockCategorizationRuleService{
		getErr: domain.ErrRuleNotFound,
	}

	handler := NewCategorizationRuleHandler(mockService)

	input := &GetRuleByIDInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		ID:                 "nonexistent-rule",
	}

	output, err := handler.GetRuleByID(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// TestCreateRule_Success tests successful rule creation.
func TestCreateRule_Success(t *testing.T) {
	userID := "user-123"
	mockService := &mockCategorizationRuleService{
		rule: createTestRule(userID, "New Rule"),
	}

	handler := NewCategorizationRuleHandler(mockService)

	input := &CreateRuleInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
		Body: domain.CreateCategorizationRuleRequest{
			Name:       "New Rule",
			Conditions: []domain.RuleCondition{{Type: domain.ConditionMerchantContains, Value: "test"}},
			TargetTags: []string{"test"},
		},
	}

	output, err := handler.CreateRule(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, "New Rule", output.Body.Name)
}

// TestCreateRule_LimitExceeded tests 403 when rule limit is exceeded.
func TestCreateRule_LimitExceeded(t *testing.T) {
	mockService := &mockCategorizationRuleService{
		createErr: domain.ErrRuleLimitExceeded,
	}

	handler := NewCategorizationRuleHandler(mockService)

	input := &CreateRuleInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		Body: domain.CreateCategorizationRuleRequest{
			Name:       "New Rule",
			Conditions: []domain.RuleCondition{{Type: domain.ConditionMerchantContains, Value: "test"}},
			TargetTags: []string{"test"},
		},
	}

	output, err := handler.CreateRule(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
}

// TestCreateRule_DuplicateName tests 409 when rule name already exists.
func TestCreateRule_DuplicateName(t *testing.T) {
	mockService := &mockCategorizationRuleService{
		createErr: domain.ErrRuleAlreadyExists,
	}

	handler := NewCategorizationRuleHandler(mockService)

	input := &CreateRuleInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		Body: domain.CreateCategorizationRuleRequest{
			Name:       "Existing Rule",
			Conditions: []domain.RuleCondition{{Type: domain.ConditionMerchantContains, Value: "test"}},
			TargetTags: []string{"test"},
		},
	}

	output, err := handler.CreateRule(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
}

// TestCreateRule_ValidationError tests 400 for validation errors.
func TestCreateRule_ValidationError(t *testing.T) {
	mockService := &mockCategorizationRuleService{
		createErr: errors.New("name is required"),
	}

	handler := NewCategorizationRuleHandler(mockService)

	input := &CreateRuleInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		Body: domain.CreateCategorizationRuleRequest{
			Name:       "",
			Conditions: []domain.RuleCondition{},
		},
	}

	output, err := handler.CreateRule(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "name is required")
}

// TestUpdateRule_Success tests successful rule update.
func TestUpdateRule_Success(t *testing.T) {
	userID := "user-123"
	updatedRule := createTestRule(userID, "Updated Name")
	mockService := &mockCategorizationRuleService{
		rule: updatedRule,
	}

	handler := NewCategorizationRuleHandler(mockService)

	newName := "Updated Name"
	input := &UpdateRuleInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
		ID:                 "rule-123",
		Body: domain.UpdateCategorizationRuleRequest{
			Name: &newName,
		},
	}

	output, err := handler.UpdateRule(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, "Updated Name", output.Body.Name)
}

// TestUpdateRule_NotFound tests 404 when updating nonexistent rule.
func TestUpdateRule_NotFound(t *testing.T) {
	mockService := &mockCategorizationRuleService{
		updateErr: domain.ErrRuleNotFound,
	}

	handler := NewCategorizationRuleHandler(mockService)

	newName := "Updated Name"
	input := &UpdateRuleInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		ID:                 "nonexistent-rule",
		Body: domain.UpdateCategorizationRuleRequest{
			Name: &newName,
		},
	}

	output, err := handler.UpdateRule(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// TestDeleteRule_Success tests successful rule deletion.
func TestDeleteRule_Success(t *testing.T) {
	mockService := &mockCategorizationRuleService{
		deleteErr: nil,
	}

	handler := NewCategorizationRuleHandler(mockService)

	input := &DeleteRuleInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		ID:                 "rule-123",
	}

	output, err := handler.DeleteRule(context.Background(), input)

	require.NoError(t, err)
	assert.Nil(t, output)
}

// TestDeleteRule_NotFound tests 404 when deleting nonexistent rule.
func TestDeleteRule_NotFound(t *testing.T) {
	mockService := &mockCategorizationRuleService{
		deleteErr: domain.ErrRuleNotFound,
	}

	handler := NewCategorizationRuleHandler(mockService)

	input := &DeleteRuleInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		ID:                 "nonexistent-rule",
	}

	output, err := handler.DeleteRule(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// TestToggleRule_Success tests successful rule toggle.
func TestToggleRule_Success(t *testing.T) {
	userID := "user-123"
	rule := createTestRule(userID, "Test Rule")
	rule.Enabled = true
	mockService := &mockCategorizationRuleService{
		rule: rule,
	}

	handler := NewCategorizationRuleHandler(mockService)

	input := &ToggleRuleInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
		ID:                 "rule-123",
		Body: domain.ToggleCategorizationRuleRequest{
			Enabled: false,
		},
	}

	output, err := handler.ToggleRule(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.False(t, output.Body.Enabled)
}

// TestToggleRule_NotFound tests 404 when toggling nonexistent rule.
func TestToggleRule_NotFound(t *testing.T) {
	mockService := &mockCategorizationRuleService{
		toggleErr: domain.ErrRuleNotFound,
	}

	handler := NewCategorizationRuleHandler(mockService)

	input := &ToggleRuleInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		ID:                 "nonexistent-rule",
		Body: domain.ToggleCategorizationRuleRequest{
			Enabled: true,
		},
	}

	output, err := handler.ToggleRule(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// TestCategorizationRuleRouteRegistration tests that routes are properly registered.
func TestCategorizationRuleRouteRegistration(t *testing.T) {
	mockService := &mockCategorizationRuleService{
		rules: []*domain.CategorizationRule{},
	}

	_, api := humatest.New(t)
	handler := NewCategorizationRuleHandler(mockService)
	RegisterCategorizationRuleRoutes(api, handler)

	// Test GET /api/v1/auto-categorization (without auth, should fail with 401)
	resp := api.Get("/api/v1/auto-categorization")
	assert.Equal(t, http.StatusUnauthorized, resp.Code)

	// Verify the response contains expected error message
	body := resp.Body.String()
	assert.Contains(t, body, "User not authenticated")
}

// TestCategorizationRuleResponseJSON tests JSON serialization of response.
func TestCategorizationRuleResponseJSON(t *testing.T) {
	mockService := &mockCategorizationRuleService{
		rules: []*domain.CategorizationRule{},
	}

	_, api := humatest.New(t)
	handler := NewCategorizationRuleHandler(mockService)
	RegisterCategorizationRuleRoutes(api, handler)

	resp := api.Get("/api/v1/auto-categorization")
	assert.Equal(t, http.StatusUnauthorized, resp.Code)

	// Verify error response is valid JSON
	var errorResp map[string]interface{}
	err := json.Unmarshal(resp.Body.Bytes(), &errorResp)
	require.NoError(t, err, "Error response should be valid JSON")
}
