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

// mockDashboardPreferencesService is a mock implementation of services.DashboardPreferencesService
type mockDashboardPreferencesService struct {
	preferencesResponse *domain.DashboardPreferencesResponse
	getErr              error
	saveErr             error
	resetErr            error
}

func (m *mockDashboardPreferencesService) GetPreferences(ctx context.Context, userID string) (*domain.DashboardPreferencesResponse, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	return m.preferencesResponse, nil
}

func (m *mockDashboardPreferencesService) SavePreferences(ctx context.Context, userID string, layout domain.DashboardLayout) (*domain.DashboardPreferencesResponse, error) {
	if m.saveErr != nil {
		return nil, m.saveErr
	}
	return m.preferencesResponse, nil
}

func (m *mockDashboardPreferencesService) ResetToDefault(ctx context.Context, userID string) (*domain.DashboardPreferencesResponse, error) {
	if m.resetErr != nil {
		return nil, m.resetErr
	}
	return m.preferencesResponse, nil
}

// Helper function to create test preferences response
func createTestPreferencesResponse() *domain.DashboardPreferencesResponse {
	return &domain.DashboardPreferencesResponse{
		Layout: domain.DashboardLayout{
			Widgets: []domain.WidgetConfig{
				{
					ID:   "widget-1",
					Type: "balance-chart",
					X:    0,
					Y:    0,
					W:    6,
					H:    4,
				},
			},
			Version: 1,
		},
		UpdatedAt: time.Now(),
	}
}

// TestGetPreferences_Success tests successful retrieval of preferences.
func TestGetPreferences_Success(t *testing.T) {
	mockService := &mockDashboardPreferencesService{
		preferencesResponse: createTestPreferencesResponse(),
	}

	handler := NewDashboardPreferencesHandler(mockService)

	input := &GetDashboardPreferencesInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}

	output, err := handler.GetPreferences(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, 1, output.Body.Layout.Version)
	assert.Len(t, output.Body.Layout.Widgets, 1)
}

// TestGetPreferences_Error tests error handling for get preferences.
func TestGetPreferences_Error(t *testing.T) {
	mockService := &mockDashboardPreferencesService{
		getErr: errors.New("database error"),
	}

	handler := NewDashboardPreferencesHandler(mockService)

	input := &GetDashboardPreferencesInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}

	output, err := handler.GetPreferences(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
}

// TestSavePreferences_Success tests successful saving of preferences.
func TestSavePreferences_Success(t *testing.T) {
	mockService := &mockDashboardPreferencesService{
		preferencesResponse: createTestPreferencesResponse(),
	}

	handler := NewDashboardPreferencesHandler(mockService)

	input := &SaveDashboardPreferencesInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}
	input.Body.Layout = domain.DashboardLayout{
		Widgets: []domain.WidgetConfig{
			{
				ID:   "widget-1",
				Type: "balance-chart",
				X:    0,
				Y:    0,
				W:    6,
				H:    4,
			},
		},
		Version: 2,
	}

	output, err := handler.SavePreferences(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
}

// TestSavePreferences_Error tests error handling for save preferences.
func TestSavePreferences_Error(t *testing.T) {
	mockService := &mockDashboardPreferencesService{
		saveErr: errors.New("validation error"),
	}

	handler := NewDashboardPreferencesHandler(mockService)

	input := &SaveDashboardPreferencesInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}
	input.Body.Layout = domain.DashboardLayout{
		Widgets: []domain.WidgetConfig{},
		Version: 1,
	}

	output, err := handler.SavePreferences(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
}

// TestResetPreferences_Success tests successful reset of preferences.
func TestResetPreferences_Success(t *testing.T) {
	mockService := &mockDashboardPreferencesService{
		preferencesResponse: createTestPreferencesResponse(),
	}

	handler := NewDashboardPreferencesHandler(mockService)

	input := &ResetDashboardPreferencesInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}

	output, err := handler.ResetPreferences(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
}

// TestResetPreferences_Error tests error handling for reset preferences.
func TestResetPreferences_Error(t *testing.T) {
	mockService := &mockDashboardPreferencesService{
		resetErr: errors.New("database error"),
	}

	handler := NewDashboardPreferencesHandler(mockService)

	input := &ResetDashboardPreferencesInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}

	output, err := handler.ResetPreferences(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
}

// TestDashboardPreferencesRouteRegistration tests that routes are properly registered.
func TestDashboardPreferencesRouteRegistration(t *testing.T) {
	mockService := &mockDashboardPreferencesService{
		preferencesResponse: createTestPreferencesResponse(),
	}

	_, api := humatest.New(t)
	handler := NewDashboardPreferencesHandler(mockService)
	RegisterDashboardPreferencesRoutes(api, handler)

	// Test GET /api/v1/dashboard/preferences (without auth, should fail with 401)
	resp := api.Get("/api/v1/dashboard/preferences")
	assert.Equal(t, http.StatusUnauthorized, resp.Code)

	// Verify the response contains expected error message
	body := resp.Body.String()
	assert.Contains(t, body, "User not authenticated")
}

// TestDashboardPreferencesResponseJSON tests JSON serialization of response.
func TestDashboardPreferencesResponseJSON(t *testing.T) {
	mockService := &mockDashboardPreferencesService{}

	_, api := humatest.New(t)
	handler := NewDashboardPreferencesHandler(mockService)
	RegisterDashboardPreferencesRoutes(api, handler)

	resp := api.Get("/api/v1/dashboard/preferences")
	assert.Equal(t, http.StatusUnauthorized, resp.Code)

	// Verify error response is valid JSON
	var errorResp map[string]interface{}
	err := json.Unmarshal(resp.Body.Bytes(), &errorResp)
	require.NoError(t, err, "Error response should be valid JSON")
}
