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
	phclient "github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
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

func (m *mockSavingsGoalService) SetBudgetRepo(_ services.GoalAutoCreateBudgetRepo) {}

func (m *mockSavingsGoalService) SetProgressService(_ services.GoalProgressRecomputer) {}

func (m *mockSavingsGoalService) SetBasisSwitchContribRepo(_ services.GoalBasisSwitchContribRepo) {}

func (m *mockSavingsGoalService) SetAnalytics(_ phclient.Analytics) {}

// mockSavingsProgressService is a mock implementation of services.SavingsProgressService.
type mockSavingsProgressService struct {
	summary    *domain.SavingsProgressSummary
	summaryErr error
}

func (m *mockSavingsProgressService) RecomputeGoal(_ context.Context, _ *domain.SavingsGoal) error {
	return nil
}

func (m *mockSavingsProgressService) RecomputeUser(_ context.Context, _ string) error {
	return nil
}

func (m *mockSavingsProgressService) GetProgressSummary(_ context.Context, _ string) (*domain.SavingsProgressSummary, error) {
	if m.summaryErr != nil {
		return nil, m.summaryErr
	}
	return m.summary, nil
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

// TestCreateSavingsGoalRequest_DeadlineDecoding tests JSON unmarshaling of deadline
// field (RFC3339 format required, bare YYYY-MM-DD rejected).
func TestCreateSavingsGoalRequest_DeadlineDecoding(t *testing.T) {
	t.Run("rejects bare YYYY-MM-DD date string", func(t *testing.T) {
		var req domain.CreateSavingsGoalRequest
		err := json.Unmarshal([]byte(`{"name":"Test","target_amount":1000,"account_id":"550e8400-e29b-41d4-a716-446655440000","deadline":"2026-12-31"}`), &req)
		assert.Error(t, err, "bare YYYY-MM-DD should fail to unmarshal into *time.Time")
	})

	t.Run("accepts RFC3339 datetime string", func(t *testing.T) {
		var req domain.CreateSavingsGoalRequest
		err := json.Unmarshal([]byte(`{"name":"Test","target_amount":1000,"account_id":"550e8400-e29b-41d4-a716-446655440000","deadline":"2026-12-31T00:00:00Z"}`), &req)
		require.NoError(t, err)
		require.NotNil(t, req.Deadline)
		assert.Equal(t, 2026, req.Deadline.Year())
		assert.Equal(t, 12, int(req.Deadline.Month()))
		assert.Equal(t, 31, req.Deadline.Day())
	})

	t.Run("accepts missing deadline as nil", func(t *testing.T) {
		var req domain.CreateSavingsGoalRequest
		err := json.Unmarshal([]byte(`{"name":"Test","target_amount":1000,"account_id":"550e8400-e29b-41d4-a716-446655440000"}`), &req)
		require.NoError(t, err)
		assert.Nil(t, req.Deadline)
	})
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

// TestGetProgressSummary_Success tests the GetProgressSummary handler.
func TestGetProgressSummary_Success(t *testing.T) {
	pct := 46.67
	summary := &domain.SavingsProgressSummary{
		Goals: []domain.SavingsGoalProgressItem{
			{
				GoalID:          "goal-1",
				GoalName:        "Vacation",
				CurrentAmount:   700,
				TargetAmount:    1500,
				PercentComplete: pct,
				ProgressBasis:   domain.ProgressBasisContributions,
				ComputedFrom:    domain.ComputedFromConfirmedContribs,
				Freshness:       domain.FreshnessNotApplicable,
			},
		},
		TotalSaved:             700,
		TotalTarget:            1500,
		OverallPercentComplete: pct,
		GoalCount:              1,
		CompletedGoalCount:     0,
	}

	mockGoalSvc := &mockSavingsGoalService{}
	mockProgSvc := &mockSavingsProgressService{summary: summary}

	handler := NewSavingsGoalHandler(mockGoalSvc, SavingsGoalHandlerDeps{
		ProgressService: mockProgSvc,
	})

	input := &GetSavingsProgressSummaryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}

	output, err := handler.GetProgressSummary(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	require.NotNil(t, output.Body)
	assert.Equal(t, 1, output.Body.GoalCount)
	assert.Equal(t, 700.0, output.Body.TotalSaved)
	assert.Equal(t, 1500.0, output.Body.TotalTarget)
	assert.Equal(t, pct, output.Body.OverallPercentComplete)
	assert.Len(t, output.Body.Goals, 1)
	assert.Equal(t, domain.ComputedFromConfirmedContribs, output.Body.Goals[0].ComputedFrom)
	assert.Equal(t, domain.FreshnessNotApplicable, output.Body.Goals[0].Freshness)
}

// TestGetProgressSummary_NoService returns 503 when progressService is nil.
func TestGetProgressSummary_NoService(t *testing.T) {
	mockGoalSvc := &mockSavingsGoalService{}
	// No ProgressService set
	handler := NewSavingsGoalHandler(mockGoalSvc)

	input := &GetSavingsProgressSummaryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}

	output, err := handler.GetProgressSummary(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
}

// TestGetProgressSummary_ServiceError propagates service errors.
func TestGetProgressSummary_ServiceError(t *testing.T) {
	mockGoalSvc := &mockSavingsGoalService{}
	mockProgSvc := &mockSavingsProgressService{summaryErr: domain.ErrSavingsGoalNotFound}

	handler := NewSavingsGoalHandler(mockGoalSvc, SavingsGoalHandlerDeps{
		ProgressService: mockProgSvc,
	})

	input := &GetSavingsProgressSummaryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}

	output, err := handler.GetProgressSummary(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
}

// TestProgressSummaryRouteRegistration verifies the route is registered and requires auth.
func TestProgressSummaryRouteRegistration(t *testing.T) {
	_, api := humatest.New(t)
	handler := NewSavingsGoalHandler(&mockSavingsGoalService{})
	RegisterSavingsGoalRoutes(api, handler)

	resp := api.Get("/api/v1/savings-goals/progress-summary")
	assert.Equal(t, http.StatusUnauthorized, resp.Code)
}
