package operations

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	phclient "github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
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
	purgeErr    error
	// purgeCalls records (userID, goalID) for each PurgeGoal call, so the handler test can prove
	// the route reaches the service rather than merely returning 204. Issue #1569, Task 8.
	purgeCalls [][2]string
	// #1569 Task 12: the deleted-goals listing that makes purge reachable.
	deletedGoals      []*domain.SavingsGoal
	deletedGoalsErr   error
	deletedGoalsCalls []string
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

// deletedGoals is what GetDeletedGoals returns; deletedGoalsErr short-circuits it.
func (m *mockSavingsGoalService) GetDeletedGoals(ctx context.Context, userID string) ([]*domain.SavingsGoal, error) {
	if m.deletedGoalsErr != nil {
		return nil, m.deletedGoalsErr
	}
	m.deletedGoalsCalls = append(m.deletedGoalsCalls, userID)
	return m.deletedGoals, nil
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

func (m *mockSavingsGoalService) PurgeGoal(ctx context.Context, userID, goalID string) error {
	m.purgeCalls = append(m.purgeCalls, [2]string{userID, goalID})
	return m.purgeErr
}

func (m *mockSavingsGoalService) SetAuditLogRepository(_ repository.AuditLogRepository) {}

func (m *mockSavingsGoalService) SetBudgetRepo(_ services.GoalAutoCreateBudgetRepo) {}

func (m *mockSavingsGoalService) SetProgressService(_ services.GoalProgressRecomputer) {}

func (m *mockSavingsGoalService) SetBasisSwitchContribRepo(_ services.GoalBasisSwitchContribRepo) {}

func (m *mockSavingsGoalService) SetAnalytics(_ phclient.Analytics) {}

// mockSavingsProgressService is a mock implementation of services.SavingsProgressService.
type mockSavingsProgressService struct {
	summary    *domain.SavingsProgressSummary
	summaryErr error
}

func (m *mockSavingsProgressService) RecomputeGoal(_ context.Context, goal *domain.SavingsGoal) (float64, error) {
	return goal.CurrentAmount, nil
}

func (m *mockSavingsProgressService) RecomputeGoalByID(_ context.Context, _, _ string) error {
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
	g := &domain.SavingsGoal{
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
	g.PopulateDateDisplay()
	return g
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

// TestPurgeSavingsGoal_ReachesTheServiceAndMapsThePrecondition covers the second, explicit half of
// the #1569 Task 8 two-step. DELETE is now non-destructive, so this is the only route that can
// actually erase a user's hand-logged contributions.
func TestPurgeSavingsGoal_ReachesTheServiceAndMapsThePrecondition(t *testing.T) {
	t.Run("success forwards the authenticated user and the path id", func(t *testing.T) {
		mockService := &mockSavingsGoalService{}
		handler := NewSavingsGoalHandler(mockService)

		_, err := handler.PurgeGoal(context.Background(), &PurgeSavingsGoalInput{
			AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
			ID:                 "goal-123",
		})

		require.NoError(t, err)
		require.Equal(t, [][2]string{{"user-123", "goal-123"}}, mockService.purgeCalls,
			"a 204 that never reached the service would be indistinguishable from a purge that "+
				"silently did nothing")
	})

	t.Run("a live goal is refused with 409, not 500", func(t *testing.T) {
		mockService := &mockSavingsGoalService{purgeErr: domain.ErrSavingsGoalNotDeleted}
		handler := NewSavingsGoalHandler(mockService)

		_, err := handler.PurgeGoal(context.Background(), &PurgeSavingsGoalInput{
			AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
			ID:                 "goal-123",
		})

		require.Error(t, err)
		var statusErr huma.StatusError
		require.ErrorAs(t, err, &statusErr)
		require.Equal(t, http.StatusConflict, statusErr.GetStatus(),
			"the precondition is a client-correctable conflict — the same shape PurgeAccount's "+
				"ErrAccountNotDisconnected already maps to")
	})
}

// TestCreateSavingsGoalRequest_DeadlineDecoding tests JSON unmarshaling of the deadline field.
//
// #1631 Batch F2 REVERSES this test's prior behavior: it used to assert that a bare YYYY-MM-DD
// date was REJECTED (RFC3339 required). That was the actual defect this batch fixes — the DB
// column is a Postgres DATE, so a caller sending the plain calendar day it is documented as
// should not 422. RFC3339 must ALSO keep working: the current frontend
// (CreateSavingsGoalDialog.tsx) sends `new Date(dateOnly).toISOString()`, an RFC3339 timestamp,
// and that must not regress. See domain/flexible_date.go.
func TestCreateSavingsGoalRequest_DeadlineDecoding(t *testing.T) {
	t.Run("accepts bare YYYY-MM-DD date string", func(t *testing.T) {
		var req domain.CreateSavingsGoalRequest
		err := json.Unmarshal([]byte(`{"name":"Test","target_amount":1000,"account_id":"550e8400-e29b-41d4-a716-446655440000","deadline":"2026-12-31"}`), &req)
		require.NoError(t, err, "a bare YYYY-MM-DD date must be accepted — it is the DATE column's own documented shape")
		require.NotNil(t, req.Deadline)
		asTime := time.Time(*req.Deadline)
		assert.Equal(t, 2026, asTime.Year())
		assert.Equal(t, 12, int(asTime.Month()))
		assert.Equal(t, 31, asTime.Day())
	})

	t.Run("accepts RFC3339 datetime string", func(t *testing.T) {
		var req domain.CreateSavingsGoalRequest
		err := json.Unmarshal([]byte(`{"name":"Test","target_amount":1000,"account_id":"550e8400-e29b-41d4-a716-446655440000","deadline":"2026-12-31T00:00:00Z"}`), &req)
		require.NoError(t, err)
		require.NotNil(t, req.Deadline)
		asTime := time.Time(*req.Deadline)
		assert.Equal(t, 2026, asTime.Year())
		assert.Equal(t, 12, int(asTime.Month()))
		assert.Equal(t, 31, asTime.Day())
	})

	t.Run("rejects garbage", func(t *testing.T) {
		var req domain.CreateSavingsGoalRequest
		err := json.Unmarshal([]byte(`{"name":"Test","target_amount":1000,"account_id":"550e8400-e29b-41d4-a716-446655440000","deadline":"not-a-date"}`), &req)
		assert.Error(t, err)
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

// =============================================================================
// Issue #1300 RG6: huma-ops progress_basis pass-through assertion (M23)
// =============================================================================

// capturingSavingsGoalService wraps mockSavingsGoalService and records the last request
// forwarded to CreateGoal, so tests can assert on what the handler passed through.
type capturingSavingsGoalService struct {
	mockSavingsGoalService
	lastCreateReq *domain.CreateSavingsGoalRequest
	lastUpdateReq *domain.UpdateSavingsGoalRequest
}

func (c *capturingSavingsGoalService) CreateGoal(_ context.Context, _ string, req *domain.CreateSavingsGoalRequest) (*domain.SavingsGoal, error) {
	c.lastCreateReq = req
	return c.goal, c.createErr
}

func (c *capturingSavingsGoalService) UpdateGoal(_ context.Context, _, _ string, req *domain.UpdateSavingsGoalRequest) (*domain.SavingsGoal, error) {
	c.lastUpdateReq = req
	return c.goal, c.updateErr
}

// TestCreateSavingsGoal_ProgressBasisPersistsToService verifies the huma handler
// correctly forwards progress_basis from the request body to the service layer.
func TestCreateSavingsGoal_ProgressBasisPersistsToService(t *testing.T) {
	svc := &capturingSavingsGoalService{
		mockSavingsGoalService: mockSavingsGoalService{goal: createTestSavingsGoal()},
	}
	handler := NewSavingsGoalHandler(svc)

	input := &CreateSavingsGoalInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}
	input.Body = domain.CreateSavingsGoalRequest{
		Name:          "Savings Goal",
		TargetAmount:  1000.00,
		CurrencyCode:  "USD",
		ProgressBasis: domain.ProgressBasisContributions,
	}

	output, err := handler.CreateGoal(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	require.NotNil(t, svc.lastCreateReq, "handler must forward request to service")
	assert.Equal(t, domain.ProgressBasisContributions, svc.lastCreateReq.ProgressBasis,
		"handler must pass progress_basis from request body through to service")
}

// TestUpdateSavingsGoal_ProgressBasisPersistsToService verifies the huma handler
// correctly forwards progress_basis from a PATCH request body to the service layer.
func TestUpdateSavingsGoal_ProgressBasisPersistsToService(t *testing.T) {
	svc := &capturingSavingsGoalService{
		mockSavingsGoalService: mockSavingsGoalService{goal: createTestSavingsGoal()},
	}
	handler := NewSavingsGoalHandler(svc)

	basis := domain.ProgressBasisBalance
	input := &UpdateSavingsGoalInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		ID:                 "goal-123",
	}
	input.Body = domain.UpdateSavingsGoalRequest{
		ProgressBasis: &basis,
	}

	output, err := handler.UpdateGoal(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	require.NotNil(t, svc.lastUpdateReq, "handler must forward update request to service")
	assert.Equal(t, &basis, svc.lastUpdateReq.ProgressBasis,
		"handler must pass progress_basis from PATCH body through to service")
}

// TestProgressSummaryRouteRegistration verifies the route is registered and requires auth.
func TestProgressSummaryRouteRegistration(t *testing.T) {
	_, api := humatest.New(t)
	handler := NewSavingsGoalHandler(&mockSavingsGoalService{})
	RegisterSavingsGoalRoutes(api, handler)

	resp := api.Get("/api/v1/savings-goals/progress-summary")
	assert.Equal(t, http.StatusUnauthorized, resp.Code)
}

// ── #1569 Task 12 gap 2: the deleted-goals listing ────────────────────────────────────────────
//
// purgeSavingsGoal shipped in task 8 with a precondition (the goal must already be deleted) and
// no way for any client to discover which goals satisfy it, because every other read filters
// `deleted_at IS NULL`. These tests pin the endpoint that closes that loop.

func TestGetDeletedSavingsGoals_ReturnsTheUsersTombstonedGoals(t *testing.T) {
	deletedAt := time.Now().Add(-48 * time.Hour)
	goal := createTestSavingsGoal()
	goal.DeletedAt = &deletedAt

	mockService := &mockSavingsGoalService{deletedGoals: []*domain.SavingsGoal{goal}}
	handler := NewSavingsGoalHandler(mockService)

	output, err := handler.GetDeletedGoals(context.Background(), &GetDeletedSavingsGoalsInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	})

	require.NoError(t, err)
	require.Len(t, output.Body, 1)
	assert.Equal(t, "goal-123", output.Body[0].ID)
	require.NotNil(t, output.Body[0].DeletedAt,
		"deleted_at must reach the client — it is the only thing distinguishing this listing")
	// The handler must scope to the AUTHENTICATED user, not to anything in the request.
	assert.Equal(t, []string{"user-123"}, mockService.deletedGoalsCalls)
}

func TestGetDeletedSavingsGoals_EmptyIsAnArrayNotNull(t *testing.T) {
	mockService := &mockSavingsGoalService{deletedGoals: nil}
	handler := NewSavingsGoalHandler(mockService)

	output, err := handler.GetDeletedGoals(context.Background(), &GetDeletedSavingsGoalsInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	})

	require.NoError(t, err)
	require.NotNil(t, output.Body, "a client mapping over the response must not receive null")
	assert.Empty(t, output.Body)
}

func TestGetDeletedSavingsGoals_Error(t *testing.T) {
	mockService := &mockSavingsGoalService{deletedGoalsErr: errors.New("database error")}
	handler := NewSavingsGoalHandler(mockService)

	_, err := handler.GetDeletedGoals(context.Background(), &GetDeletedSavingsGoalsInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	})

	require.Error(t, err)
}

// TestSavingsGoalDeletedRouteIsRegistered — #1569 Task 12 fix round 1 (I5).
//
// Deleting the whole huma.Register block for GET /savings-goals/deleted previously left this
// package green: the handler was unit-tested by direct call, so nothing noticed that the route it
// serves had stopped existing.
//
// ⚠ THE OBVIOUS VERSION OF THIS TEST DOES NOT WORK, and the first draft of it shipped broken.
// Asserting `api.Get("/api/v1/savings-goals/deleted")` returns 401-not-404 SURVIVES deleting the
// registration: with /deleted gone the request is matched by /savings-goals/{id} with
// id="deleted", auth runs first, and the response is an identical 401. Measured — the mutant was
// green. The status code cannot discriminate here, so the assertion is made against the registry
// itself.
func TestSavingsGoalDeletedRouteIsRegistered(t *testing.T) {
	_, api := humatest.New(t)
	RegisterSavingsGoalRoutes(api, NewSavingsGoalHandler(&mockSavingsGoalService{}))

	item, ok := api.OpenAPI().Paths["/api/v1/savings-goals/deleted"]
	require.True(t, ok, "GET /api/v1/savings-goals/deleted is not registered")
	require.NotNil(t, item.Get, "the path exists but has no GET operation")
	assert.Equal(t, "getDeletedSavingsGoals", item.Get.OperationID)
}

// TestSavingsGoalDeletedRoute_IsItsOwnOperation guards the failure the status-code test could not
// see: the listing being served by getSavingsGoal with id="deleted".
//
// The registration used to carry a "(must be before :id route)" comment copied from its
// neighbours. That claim is false for this router — a static segment wins over a parameter
// regardless of registration order — so it has been corrected rather than propagated. What this
// test holds is narrower and true: /deleted is a distinct path with a distinct operation from
// /{id}, and the handler behind it is the listing handler, not the single-goal one.
func TestSavingsGoalDeletedRoute_IsItsOwnOperation(t *testing.T) {
	deletedAt := time.Now()
	goal := createTestSavingsGoal()
	goal.DeletedAt = &deletedAt

	mockService := &mockSavingsGoalService{
		goal:         createTestSavingsGoal(), // what the {id} route would return
		deletedGoals: []*domain.SavingsGoal{goal},
	}
	handler := NewSavingsGoalHandler(mockService)

	_, api := humatest.New(t)
	RegisterSavingsGoalRoutes(api, handler)

	paths := api.OpenAPI().Paths
	deleted, ok := paths["/api/v1/savings-goals/deleted"]
	require.True(t, ok)
	byID, ok := paths["/api/v1/savings-goals/{id}"]
	require.True(t, ok, "the {id} route must still exist — otherwise this proves nothing")
	assert.NotEqual(t, byID.Get.OperationID, deleted.Get.OperationID,
		"the deleted listing is being served by the single-goal operation")

	// And the route must actually be WIRED to the listing handler. Distinct paths in the spec do
	// not prove that: pointing `}, handler.GetDeletedGoals)` at handler.GetGoals instead left this
	// package green until this half was added (measured). So drive the route end-to-end with an
	// authenticated context and check WHICH handler answered — the two mocks return disjoint sets.
	authed := func(ctx huma.Context, next func(huma.Context)) {
		next(huma.WithValue(ctx, inputs.UserIDKey, "user-123"))
	}
	_, authedAPI := humatest.New(t)
	authedAPI.UseMiddleware(authed)
	RegisterSavingsGoalRoutes(authedAPI, handler)

	resp := authedAPI.Get("/api/v1/savings-goals/deleted")
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())

	var body []*domain.SavingsGoal
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &body))
	require.Len(t, body, 1)
	require.NotNil(t, body[0].DeletedAt,
		"the live listing answered — its goals carry no deleted_at")
	assert.Equal(t, []string{"user-123"}, mockService.deletedGoalsCalls,
		"GetDeletedGoals was never reached, so some other handler served /deleted")
}

// TestSavingsGoalValidationNeverReturns5xx is the anti-Sentry-noise proof at the HTTP boundary for
// the savings-goal path — issue #1625's headline defect ("a validation failure surfaced as a 500
// with a Sentry event instead of a 4xx"), which was fixed for categorization rules but not here.
//
// Before this fix, SavingsGoal.Validate() returned bare errors.New(...) with no sentinel, so
// mapper.go's switch never matched them and every one fell through to the default 500 branch
// (which also fires sentry.CaptureException — see LoggingMapError). Asserting only require.Error
// would NOT catch that regression, since a 500 is still "an error" — this asserts the actual
// mapped HTTP status via huma.StatusError.GetStatus(), the same shape as
// TestRuleValidationNeverReturns5xx in categorization_rules_test.go.
//
// Uses the REAL service (services.NewSavingsGoalService) and REAL domain.SavingsGoal.Validate() —
// a mock would only prove the mock's shape. CreateGoal's Validate() call happens before any
// repository access, so nil goalRepo/accountGetter are safe for every case below.
func TestSavingsGoalValidationNeverReturns5xx(t *testing.T) {
	invalidBodies := []struct {
		name string
		body domain.CreateSavingsGoalRequest
	}{
		{"negative target_amount", domain.CreateSavingsGoalRequest{
			Name:         "vacation fund",
			TargetAmount: -5,
			AccountID:    "acc-1",
		}},
		{"over-long name", domain.CreateSavingsGoalRequest{
			Name:         strings.Repeat("a", 101),
			TargetAmount: 100,
			AccountID:    "acc-1",
		}},
		{"invalid basis/account combination: balance basis with no account_id", domain.CreateSavingsGoalRequest{
			Name:          "car fund",
			TargetAmount:  100,
			AccountID:     "",
			ProgressBasis: domain.ProgressBasisBalance,
		}},
	}

	for _, tt := range invalidBodies {
		t.Run(tt.name, func(t *testing.T) {
			svc := services.NewSavingsGoalService(nil, nil, slog.Default())
			handler := NewSavingsGoalHandler(svc)

			_, err := handler.CreateGoal(context.Background(), &CreateSavingsGoalInput{
				AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
				Body:               tt.body,
			})

			require.Error(t, err)
			assertSavingsGoalClientError(t, err)
		})
	}
}

// assertSavingsGoalClientError fails when the mapped HTTP status is >= 500. A 5xx here means the
// error reached the mapper's default branch, which also fires sentry.CaptureException — a client
// input mistake logged and billed as a server error.
func assertSavingsGoalClientError(t *testing.T, err error) {
	t.Helper()
	var statusErr interface{ GetStatus() int }
	if !errors.As(err, &statusErr) {
		t.Fatalf("error carries no HTTP status: %v", err)
	}
	status := statusErr.GetStatus()
	assert.Less(t, status, 500,
		"validation failure mapped to %d — it must be a 4xx, or client input pollutes Sentry as a server error: %v",
		status, err)
	assert.GreaterOrEqual(t, status, 400, "expected a client error, got %d", status)
}

// TestCreateGoalSchema_RejectsAtTheFrameworkForSingleFieldConstraints is the "generated artifact"
// half of #1625's second fix round. The sentinel/mapper tests above prove domain.Validate() never
// falls through to a 500; this test proves the OPENAPI SCHEMA ITSELF now carries the constraint
// for the two single-field cases (target_amount, name), so Huma rejects the request BEFORE the
// handler — and BEFORE domain.Validate() — ever runs, and an MCP agent reading the schema (or the
// spec-index built from it) can learn the constraint without a failed round trip.
//
// Goes through the real router (humatest + RegisterSavingsGoalRoutes), not a direct handler call,
// because the constraint under test lives in the Huma schema's request-body validation, which only
// runs on the actual HTTP path.
func TestCreateGoalSchema_RejectsAtTheFrameworkForSingleFieldConstraints(t *testing.T) {
	authed := func(ctx huma.Context, next func(huma.Context)) {
		next(huma.WithValue(ctx, inputs.UserIDKey, "user-123"))
	}
	_, api := humatest.New(t)
	api.UseMiddleware(authed)
	RegisterSavingsGoalRoutes(api, NewSavingsGoalHandler(&mockSavingsGoalService{goal: createTestSavingsGoal()}))

	t.Run("negative target_amount", func(t *testing.T) {
		resp := api.Post("/api/v1/savings-goals", map[string]interface{}{
			"name":          "vacation fund",
			"target_amount": -5,
			"account_id":    "acc-1",
		})
		assert.Equal(t, http.StatusUnprocessableEntity, resp.Code, resp.Body.String())
		assert.Contains(t, resp.Body.String(), "target_amount",
			"a schema rejection must still name the offending field — a framework 422 that says "+
				"nothing useful is a regression against #1625's theme even though the status is right")
	})

	t.Run("over-long name", func(t *testing.T) {
		resp := api.Post("/api/v1/savings-goals", map[string]interface{}{
			"name":          strings.Repeat("a", 101),
			"target_amount": 100,
			"account_id":    "acc-1",
		})
		assert.Equal(t, http.StatusUnprocessableEntity, resp.Code, resp.Body.String())
		assert.Contains(t, resp.Body.String(), "name",
			"a schema rejection must still name the offending field")
	})
}
