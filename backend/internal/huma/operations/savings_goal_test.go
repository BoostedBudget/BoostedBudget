package operations

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockSavingsGoalService is a mock implementation of services.SavingsGoalService
type mockSavingsGoalService struct {
	goal        *domain.SavingsGoal
	goals       []*domain.SavingsGoal
	createErr   error
	getErr      error
	getGoalsErr error
	updateErr   error
	deleteErr   error
}

func (m *mockSavingsGoalService) CreateGoal(ctx context.Context, userID string, req *domain.CreateSavingsGoalRequest) (*domain.SavingsGoal, error) {
	if m.createErr != nil {
		return nil, m.createErr
	}
	return m.goal, nil
}

func (m *mockSavingsGoalService) GetGoal(ctx context.Context, userID, goalID string) (*domain.SavingsGoal, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	return m.goal, nil
}

func (m *mockSavingsGoalService) GetGoals(ctx context.Context, userID string) ([]*domain.SavingsGoal, error) {
	if m.getGoalsErr != nil {
		return nil, m.getGoalsErr
	}
	return m.goals, nil
}

func (m *mockSavingsGoalService) GetActiveGoals(ctx context.Context, userID string) ([]*domain.SavingsGoal, error) {
	if m.getGoalsErr != nil {
		return nil, m.getGoalsErr
	}
	return m.goals, nil
}

func (m *mockSavingsGoalService) UpdateGoal(ctx context.Context, userID, goalID string, req *domain.UpdateSavingsGoalRequest) (*domain.SavingsGoal, error) {
	if m.updateErr != nil {
		return nil, m.updateErr
	}
	return m.goal, nil
}

func (m *mockSavingsGoalService) DeleteGoal(ctx context.Context, userID, goalID string) error {
	return m.deleteErr
}

// Helper function to create test savings goal
func createTestSavingsGoal() *domain.SavingsGoal {
	now := time.Now()
	deadline := now.AddDate(0, 6, 0)
	return &domain.SavingsGoal{
		ID:            "goal-123",
		UserID:        "user-123",
		Name:          "Emergency Fund",
		TargetAmount:  5000.00,
		CurrentAmount: 2000.00,
		CurrencyCode:  "USD",
		AccountID:     "acc-123",
		Deadline:      &deadline,
		IsCompleted:   false,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
}

// TestCreateSavingsGoal_Success tests successful savings goal creation.
func TestCreateSavingsGoal_Success(t *testing.T) {
	mockService := &mockSavingsGoalService{
		goal: createTestSavingsGoal(),
	}

	handler := NewSavingsGoalHandler(mockService)

	input := &CreateSavingsGoalInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}
	input.Body = domain.CreateSavingsGoalRequest{
		Name:         "Emergency Fund",
		TargetAmount: 5000.00,
		CurrencyCode: "USD",
	}

	output, err := handler.CreateGoal(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, "goal-123", output.Body.ID)
	assert.Equal(t, "Emergency Fund", output.Body.Name)
	assert.Equal(t, 5000.00, output.Body.TargetAmount)
}

// TestCreateSavingsGoal_Error tests error handling for savings goal creation.
func TestCreateSavingsGoal_Error(t *testing.T) {
	mockService := &mockSavingsGoalService{
		createErr: domain.ErrSavingsGoalAlreadyExists,
	}

	handler := NewSavingsGoalHandler(mockService)

	input := &CreateSavingsGoalInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}
	input.Body = domain.CreateSavingsGoalRequest{
		Name:         "Emergency Fund",
		TargetAmount: 5000.00,
	}

	output, err := handler.CreateGoal(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
}

// TestGetSavingsGoals_Success tests successful retrieval of savings goals.
func TestGetSavingsGoals_Success(t *testing.T) {
	mockService := &mockSavingsGoalService{
		goals: []*domain.SavingsGoal{createTestSavingsGoal()},
	}

	handler := NewSavingsGoalHandler(mockService)

	input := &GetSavingsGoalsInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}

	output, err := handler.GetGoals(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Len(t, output.Body, 1)
}

// TestGetSavingsGoals_Empty tests empty savings goal list returns empty array.
func TestGetSavingsGoals_Empty(t *testing.T) {
	mockService := &mockSavingsGoalService{
		goals: nil,
	}

	handler := NewSavingsGoalHandler(mockService)

	input := &GetSavingsGoalsInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}

	output, err := handler.GetGoals(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.NotNil(t, output.Body)
	assert.Len(t, output.Body, 0)
}

// TestGetActiveSavingsGoals_Success tests successful retrieval of active savings goals.
func TestGetActiveSavingsGoals_Success(t *testing.T) {
	mockService := &mockSavingsGoalService{
		goals: []*domain.SavingsGoal{createTestSavingsGoal()},
	}

	handler := NewSavingsGoalHandler(mockService)

	input := &GetActiveSavingsGoalsInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}

	output, err := handler.GetActiveGoals(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Len(t, output.Body, 1)
}

// TestGetSavingsGoal_Success tests successful retrieval of a specific savings goal.
func TestGetSavingsGoal_Success(t *testing.T) {
	mockService := &mockSavingsGoalService{
		goal: createTestSavingsGoal(),
	}

	handler := NewSavingsGoalHandler(mockService)

	input := &GetSavingsGoalInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		ID:                 "goal-123",
	}

	output, err := handler.GetGoal(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, "goal-123", output.Body.ID)
}

// TestGetSavingsGoal_NotFound tests error when savings goal is not found.
func TestGetSavingsGoal_NotFound(t *testing.T) {
	mockService := &mockSavingsGoalService{
		getErr: domain.ErrSavingsGoalNotFound,
	}

	handler := NewSavingsGoalHandler(mockService)

	input := &GetSavingsGoalInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		ID:                 "nonexistent",
	}

	output, err := handler.GetGoal(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
}

// TestUpdateSavingsGoal_Success tests successful savings goal update.
func TestUpdateSavingsGoal_Success(t *testing.T) {
	mockService := &mockSavingsGoalService{
		goal: createTestSavingsGoal(),
	}

	handler := NewSavingsGoalHandler(mockService)

	newName := "Updated Goal"
	input := &UpdateSavingsGoalInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		ID:                 "goal-123",
	}
	input.Body = domain.UpdateSavingsGoalRequest{
		Name: &newName,
	}

	output, err := handler.UpdateGoal(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
}

// TestDeleteSavingsGoal_Success tests successful savings goal deletion.
func TestDeleteSavingsGoal_Success(t *testing.T) {
	mockService := &mockSavingsGoalService{}

	handler := NewSavingsGoalHandler(mockService)

	input := &DeleteSavingsGoalInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		ID:                 "goal-123",
	}

	_, err := handler.DeleteGoal(context.Background(), input)

	require.NoError(t, err)
}

// TestDeleteSavingsGoal_NotFound tests error when savings goal is not found for deletion.
func TestDeleteSavingsGoal_NotFound(t *testing.T) {
	mockService := &mockSavingsGoalService{
		deleteErr: domain.ErrSavingsGoalNotFound,
	}

	handler := NewSavingsGoalHandler(mockService)

	input := &DeleteSavingsGoalInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		ID:                 "nonexistent",
	}

	_, err := handler.DeleteGoal(context.Background(), input)

	require.Error(t, err)
}

// TestSavingsGoalRouteRegistration tests that routes are properly registered.
func TestSavingsGoalRouteRegistration(t *testing.T) {
	mockService := &mockSavingsGoalService{
		goal:  createTestSavingsGoal(),
		goals: []*domain.SavingsGoal{createTestSavingsGoal()},
	}

	_, api := humatest.New(t)
	handler := NewSavingsGoalHandler(mockService)
	RegisterSavingsGoalRoutes(api, handler)

	// Test GET /api/v1/savings-goals (without auth, should fail with 401)
	resp := api.Get("/api/v1/savings-goals")
	assert.Equal(t, http.StatusUnauthorized, resp.Code)

	// Verify the response contains expected error message
	body := resp.Body.String()
	assert.Contains(t, body, "User not authenticated")
}

// TestSavingsGoalResponseJSON tests JSON serialization of response.
func TestSavingsGoalResponseJSON(t *testing.T) {
	mockService := &mockSavingsGoalService{}

	_, api := humatest.New(t)
	handler := NewSavingsGoalHandler(mockService)
	RegisterSavingsGoalRoutes(api, handler)

	resp := api.Get("/api/v1/savings-goals")
	assert.Equal(t, http.StatusUnauthorized, resp.Code)

	// Verify error response is valid JSON
	var errorResp map[string]interface{}
	err := json.Unmarshal(resp.Body.Bytes(), &errorResp)
	require.NoError(t, err, "Error response should be valid JSON")
}
