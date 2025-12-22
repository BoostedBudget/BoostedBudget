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

// mockSyncLimitService is a mock implementation of services.SyncLimitService
type mockSyncLimitService struct {
	statsResponse *domain.SyncStats
	statsErr      error
}

func (m *mockSyncLimitService) CheckSyncLimit(ctx context.Context, userID, plaidAccountID string) (bool, string, error) {
	return true, "", nil
}

func (m *mockSyncLimitService) RecordSync(ctx context.Context, userID, accountID, plaidAccountID string, tier domain.TierName, daysHistorical, transactionCount int) error {
	return nil
}

func (m *mockSyncLimitService) GetSyncStats(ctx context.Context, userID string) (*domain.SyncStats, error) {
	if m.statsErr != nil {
		return nil, m.statsErr
	}
	return m.statsResponse, nil
}

func (m *mockSyncLimitService) GetHistoricalDepthLimit(ctx context.Context, userID string) (int, error) {
	return 30, nil
}

// Helper function to create test sync stats
func createTestSyncStats() *domain.SyncStats {
	return &domain.SyncStats{
		UserID:            "user-123",
		Month:             "2025-11",
		AccountsSynced:    2,
		AccountsLimit:     3,
		TotalTransactions: 150,
		CanSyncMore:       true,
		ResetDate:         time.Date(2025, 12, 1, 0, 0, 0, 0, time.UTC),
	}
}

// TestGetSyncStats_Success tests successful retrieval of sync statistics.
func TestGetSyncStats_Success(t *testing.T) {
	mockService := &mockSyncLimitService{
		statsResponse: createTestSyncStats(),
	}

	handler := NewSyncStatsHandler(mockService)

	input := &GetSyncStatsInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}

	output, err := handler.GetSyncStats(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, "user-123", output.Body.UserID)
	assert.Equal(t, "2025-11", output.Body.Month)
	assert.Equal(t, 2, output.Body.AccountsSynced)
	assert.Equal(t, 3, output.Body.AccountsLimit)
	assert.Equal(t, 150, output.Body.TotalTransactions)
	assert.True(t, output.Body.CanSyncMore)
}

// TestGetSyncStats_ServiceError tests error handling for service errors.
func TestGetSyncStats_ServiceError(t *testing.T) {
	mockService := &mockSyncLimitService{
		statsErr: errors.New("database error"),
	}

	handler := NewSyncStatsHandler(mockService)

	input := &GetSyncStatsInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}

	output, err := handler.GetSyncStats(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
}

// TestGetSyncStats_FreeTierAtLimit tests free tier user at sync limit.
func TestGetSyncStats_FreeTierAtLimit(t *testing.T) {
	mockService := &mockSyncLimitService{
		statsResponse: &domain.SyncStats{
			UserID:            "user-free",
			Month:             "2025-11",
			AccountsSynced:    3,
			AccountsLimit:     3,
			TotalTransactions: 200,
			CanSyncMore:       false, // At limit!
			ResetDate:         time.Date(2025, 12, 1, 0, 0, 0, 0, time.UTC),
		},
	}

	handler := NewSyncStatsHandler(mockService)

	input := &GetSyncStatsInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-free"},
	}

	output, err := handler.GetSyncStats(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, 3, output.Body.AccountsSynced)
	assert.Equal(t, 3, output.Body.AccountsLimit)
	assert.False(t, output.Body.CanSyncMore)
}

// TestGetSyncStats_UnlimitedTier tests unlimited tier (Premium+).
func TestGetSyncStats_UnlimitedTier(t *testing.T) {
	mockService := &mockSyncLimitService{
		statsResponse: &domain.SyncStats{
			UserID:            "user-premium",
			Month:             "2025-11",
			AccountsSynced:    10,
			AccountsLimit:     -1, // Unlimited!
			TotalTransactions: 500,
			CanSyncMore:       true,
			ResetDate:         time.Date(2025, 12, 1, 0, 0, 0, 0, time.UTC),
		},
	}

	handler := NewSyncStatsHandler(mockService)

	input := &GetSyncStatsInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-premium"},
	}

	output, err := handler.GetSyncStats(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, 10, output.Body.AccountsSynced)
	assert.Equal(t, -1, output.Body.AccountsLimit)
	assert.True(t, output.Body.CanSyncMore)
}

// TestSyncStatsRouteRegistration tests that routes are properly registered.
func TestSyncStatsRouteRegistration(t *testing.T) {
	mockService := &mockSyncLimitService{
		statsResponse: createTestSyncStats(),
	}

	_, api := humatest.New(t)
	handler := NewSyncStatsHandler(mockService)
	RegisterSyncStatsRoutes(api, handler)

	// Test GET /api/v1/sync-stats (without auth, should fail with 401)
	resp := api.Get("/api/v1/sync-stats")
	assert.Equal(t, http.StatusUnauthorized, resp.Code)

	// Verify the response contains expected error message
	body := resp.Body.String()
	assert.Contains(t, body, "User not authenticated")
}

// TestSyncStatsResponseJSON tests JSON serialization of response.
func TestSyncStatsResponseJSON(t *testing.T) {
	mockService := &mockSyncLimitService{}

	_, api := humatest.New(t)
	handler := NewSyncStatsHandler(mockService)
	RegisterSyncStatsRoutes(api, handler)

	resp := api.Get("/api/v1/sync-stats")
	assert.Equal(t, http.StatusUnauthorized, resp.Code)

	// Verify error response is valid JSON
	var errorResp map[string]interface{}
	err := json.Unmarshal(resp.Body.Bytes(), &errorResp)
	require.NoError(t, err, "Error response should be valid JSON")
}
