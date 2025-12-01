package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// Mock service for testing
type mockCategorizationRuleService struct {
	mock.Mock
}

func (m *mockCategorizationRuleService) CreateRule(ctx context.Context, userID string, req *domain.CreateCategorizationRuleRequest) (*domain.CategorizationRule, error) {
	args := m.Called(ctx, userID, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.CategorizationRule), args.Error(1)
}

func (m *mockCategorizationRuleService) GetUserRules(ctx context.Context, userID string) ([]*domain.CategorizationRule, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.CategorizationRule), args.Error(1)
}

func (m *mockCategorizationRuleService) GetRuleByID(ctx context.Context, userID, ruleID string) (*domain.CategorizationRule, error) {
	args := m.Called(ctx, userID, ruleID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.CategorizationRule), args.Error(1)
}

func (m *mockCategorizationRuleService) UpdateRule(ctx context.Context, userID, ruleID string, req *domain.UpdateCategorizationRuleRequest) (*domain.CategorizationRule, error) {
	args := m.Called(ctx, userID, ruleID, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.CategorizationRule), args.Error(1)
}

func (m *mockCategorizationRuleService) DeleteRule(ctx context.Context, userID, ruleID string) error {
	args := m.Called(ctx, userID, ruleID)
	return args.Error(0)
}

func (m *mockCategorizationRuleService) ToggleRule(ctx context.Context, userID, ruleID string, enabled bool) (*domain.CategorizationRule, error) {
	args := m.Called(ctx, userID, ruleID, enabled)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.CategorizationRule), args.Error(1)
}

func (m *mockCategorizationRuleService) GetEnabledRules(ctx context.Context, userID string) ([]*domain.CategorizationRule, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.CategorizationRule), args.Error(1)
}

// setupRuleHandler creates test fixtures for handler tests
func setupRuleHandler(t *testing.T) (*echo.Echo, *CategorizationRuleHandler, *mockCategorizationRuleService) {
	e := echo.New()
	mockService := new(mockCategorizationRuleService)
	handler := NewCategorizationRuleHandler(mockService)
	return e, handler, mockService
}

// setUserIDContext sets the user ID in the echo context
func setUserIDContext(c echo.Context, userID string) {
	c.Set("user_id", userID)
}

// TestCategorizationRuleHandler_CreateRule tests POST /rules
func TestCategorizationRuleHandler_CreateRule(t *testing.T) {
	e, handler, mockService := setupRuleHandler(t)
	userID := "user-123"

	req := domain.CreateCategorizationRuleRequest{
		Name: "Coffee Shops",
		Conditions: []domain.RuleCondition{
			{Type: domain.ConditionMerchantContains, Value: "starbucks"},
		},
		ConditionLogic: "AND",
		TargetCategory: []string{"Food & Drink", "Coffee"},
		TagBehavior:    "add",
	}

	expectedRule := &domain.CategorizationRule{
		ID:             "rule-123",
		UserID:         userID,
		Name:           "Coffee Shops",
		Enabled:        true,
		TargetCategory: []string{"Food & Drink", "Coffee"},
	}

	mockService.On("CreateRule", mock.Anything, userID, mock.AnythingOfType("*domain.CreateCategorizationRuleRequest")).
		Return(expectedRule, nil)

	body, _ := json.Marshal(req)
	httpReq := httptest.NewRequest(http.MethodPost, "/api/v1/rules", bytes.NewReader(body))
	httpReq.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(httpReq, rec)
	setUserIDContext(c, userID)

	err := handler.CreateRule(c)

	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, rec.Code)

	var result domain.CategorizationRule
	err = json.Unmarshal(rec.Body.Bytes(), &result)
	require.NoError(t, err)
	assert.Equal(t, "rule-123", result.ID)
	mockService.AssertExpectations(t)
}

// TestCategorizationRuleHandler_CreateRule_LimitExceeded tests limit exceeded error
func TestCategorizationRuleHandler_CreateRule_LimitExceeded(t *testing.T) {
	e, handler, mockService := setupRuleHandler(t)
	userID := "user-123"

	req := domain.CreateCategorizationRuleRequest{
		Name: "Coffee Shops",
		Conditions: []domain.RuleCondition{
			{Type: domain.ConditionMerchantContains, Value: "starbucks"},
		},
		ConditionLogic: "AND",
		TargetCategory: []string{"Food & Drink"},
		TagBehavior:    "add",
	}

	mockService.On("CreateRule", mock.Anything, userID, mock.AnythingOfType("*domain.CreateCategorizationRuleRequest")).
		Return(nil, domain.ErrRuleLimitExceeded)

	body, _ := json.Marshal(req)
	httpReq := httptest.NewRequest(http.MethodPost, "/api/v1/rules", bytes.NewReader(body))
	httpReq.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(httpReq, rec)
	setUserIDContext(c, userID)

	err := handler.CreateRule(c)

	require.Error(t, err)
	httpErr, ok := err.(*echo.HTTPError)
	require.True(t, ok)
	assert.Equal(t, http.StatusForbidden, httpErr.Code)
}

// TestCategorizationRuleHandler_GetUserRules tests GET /rules
func TestCategorizationRuleHandler_GetUserRules(t *testing.T) {
	e, handler, mockService := setupRuleHandler(t)
	userID := "user-123"

	expectedRules := []*domain.CategorizationRule{
		{ID: "rule-1", UserID: userID, Name: "Rule 1", Priority: 10},
		{ID: "rule-2", UserID: userID, Name: "Rule 2", Priority: 5},
	}

	mockService.On("GetUserRules", mock.Anything, userID).Return(expectedRules, nil)

	httpReq := httptest.NewRequest(http.MethodGet, "/api/v1/rules", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(httpReq, rec)
	setUserIDContext(c, userID)

	err := handler.GetUserRules(c)

	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var result []*domain.CategorizationRule
	err = json.Unmarshal(rec.Body.Bytes(), &result)
	require.NoError(t, err)
	assert.Len(t, result, 2)
	mockService.AssertExpectations(t)
}

// TestCategorizationRuleHandler_GetUserRules_Empty tests empty rules list
func TestCategorizationRuleHandler_GetUserRules_Empty(t *testing.T) {
	e, handler, mockService := setupRuleHandler(t)
	userID := "user-123"

	mockService.On("GetUserRules", mock.Anything, userID).Return([]*domain.CategorizationRule{}, nil)

	httpReq := httptest.NewRequest(http.MethodGet, "/api/v1/rules", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(httpReq, rec)
	setUserIDContext(c, userID)

	err := handler.GetUserRules(c)

	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "[]\n", rec.Body.String())
}

// TestCategorizationRuleHandler_GetRuleByID tests GET /rules/:id
func TestCategorizationRuleHandler_GetRuleByID(t *testing.T) {
	e, handler, mockService := setupRuleHandler(t)
	userID := "user-123"
	ruleID := "rule-123"

	expectedRule := &domain.CategorizationRule{
		ID:     ruleID,
		UserID: userID,
		Name:   "Test Rule",
	}

	mockService.On("GetRuleByID", mock.Anything, userID, ruleID).Return(expectedRule, nil)

	httpReq := httptest.NewRequest(http.MethodGet, "/api/v1/rules/"+ruleID, nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(httpReq, rec)
	c.SetParamNames("id")
	c.SetParamValues(ruleID)
	setUserIDContext(c, userID)

	err := handler.GetRuleByID(c)

	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	mockService.AssertExpectations(t)
}

// TestCategorizationRuleHandler_GetRuleByID_NotFound tests not found error
func TestCategorizationRuleHandler_GetRuleByID_NotFound(t *testing.T) {
	e, handler, mockService := setupRuleHandler(t)
	userID := "user-123"
	ruleID := "rule-nonexistent"

	mockService.On("GetRuleByID", mock.Anything, userID, ruleID).Return(nil, domain.ErrRuleNotFound)

	httpReq := httptest.NewRequest(http.MethodGet, "/api/v1/rules/"+ruleID, nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(httpReq, rec)
	c.SetParamNames("id")
	c.SetParamValues(ruleID)
	setUserIDContext(c, userID)

	err := handler.GetRuleByID(c)

	require.Error(t, err)
	httpErr, ok := err.(*echo.HTTPError)
	require.True(t, ok)
	assert.Equal(t, http.StatusNotFound, httpErr.Code)
}

// TestCategorizationRuleHandler_UpdateRule tests PUT /rules/:id
func TestCategorizationRuleHandler_UpdateRule(t *testing.T) {
	e, handler, mockService := setupRuleHandler(t)
	userID := "user-123"
	ruleID := "rule-123"

	newName := "Updated Name"
	req := domain.UpdateCategorizationRuleRequest{
		Name: &newName,
	}

	expectedRule := &domain.CategorizationRule{
		ID:     ruleID,
		UserID: userID,
		Name:   newName,
	}

	mockService.On("UpdateRule", mock.Anything, userID, ruleID, mock.AnythingOfType("*domain.UpdateCategorizationRuleRequest")).
		Return(expectedRule, nil)

	body, _ := json.Marshal(req)
	httpReq := httptest.NewRequest(http.MethodPut, "/api/v1/rules/"+ruleID, bytes.NewReader(body))
	httpReq.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(httpReq, rec)
	c.SetParamNames("id")
	c.SetParamValues(ruleID)
	setUserIDContext(c, userID)

	err := handler.UpdateRule(c)

	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	mockService.AssertExpectations(t)
}

// TestCategorizationRuleHandler_DeleteRule tests DELETE /rules/:id
func TestCategorizationRuleHandler_DeleteRule(t *testing.T) {
	e, handler, mockService := setupRuleHandler(t)
	userID := "user-123"
	ruleID := "rule-123"

	mockService.On("DeleteRule", mock.Anything, userID, ruleID).Return(nil)

	httpReq := httptest.NewRequest(http.MethodDelete, "/api/v1/rules/"+ruleID, nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(httpReq, rec)
	c.SetParamNames("id")
	c.SetParamValues(ruleID)
	setUserIDContext(c, userID)

	err := handler.DeleteRule(c)

	require.NoError(t, err)
	assert.Equal(t, http.StatusNoContent, rec.Code)
	mockService.AssertExpectations(t)
}

// TestCategorizationRuleHandler_ToggleRule tests POST /rules/:id/toggle
func TestCategorizationRuleHandler_ToggleRule(t *testing.T) {
	e, handler, mockService := setupRuleHandler(t)
	userID := "user-123"
	ruleID := "rule-123"

	req := domain.ToggleCategorizationRuleRequest{
		Enabled: false,
	}

	expectedRule := &domain.CategorizationRule{
		ID:      ruleID,
		UserID:  userID,
		Enabled: false,
	}

	mockService.On("ToggleRule", mock.Anything, userID, ruleID, false).Return(expectedRule, nil)

	body, _ := json.Marshal(req)
	httpReq := httptest.NewRequest(http.MethodPost, "/api/v1/rules/"+ruleID+"/toggle", bytes.NewReader(body))
	httpReq.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(httpReq, rec)
	c.SetParamNames("id")
	c.SetParamValues(ruleID)
	setUserIDContext(c, userID)

	err := handler.ToggleRule(c)

	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var result domain.CategorizationRule
	err = json.Unmarshal(rec.Body.Bytes(), &result)
	require.NoError(t, err)
	assert.False(t, result.Enabled)
	mockService.AssertExpectations(t)
}
