package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// ============================================================================
// Issue #242: Sync Stats Handler Tests (TDD RED Phase)
// ============================================================================

// MockSyncLimitService mocks the SyncLimitService interface
type MockSyncLimitService struct {
	mock.Mock
}

func (m *MockSyncLimitService) CheckSyncLimit(ctx context.Context, userID, plaidAccountID string) (bool, string, error) {
	args := m.Called(ctx, userID, plaidAccountID)
	return args.Bool(0), args.String(1), args.Error(2)
}

func (m *MockSyncLimitService) RecordSync(ctx context.Context, userID, accountID, plaidAccountID string, tier domain.TierName, daysHistorical, transactionCount int) error {
	args := m.Called(ctx, userID, accountID, plaidAccountID, tier, daysHistorical, transactionCount)
	return args.Error(0)
}

func (m *MockSyncLimitService) GetSyncStats(ctx context.Context, userID string) (*domain.SyncStats, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.SyncStats), args.Error(1)
}

func (m *MockSyncLimitService) GetHistoricalDepthLimit(ctx context.Context, userID string) (int, error) {
	args := m.Called(ctx, userID)
	return args.Int(0), args.Error(1)
}

// TestGetSyncStats_Success tests successful retrieval of sync statistics
func TestGetSyncStats_Success(t *testing.T) {
	// Setup
	mockService := new(MockSyncLimitService)
	handler := NewSyncStatsHandler(mockService)

	// Create Echo context with authenticated user
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sync-stats", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Set user ID in context (simulates Auth0 middleware)
	userID := "user-123"
	c.Set("user_id", userID)

	// Mock response
	resetDate := time.Date(2025, 12, 1, 0, 0, 0, 0, time.UTC)
	expectedStats := &domain.SyncStats{
		UserID:         userID,
		Month:          "2025-11",
		AccountsSynced: 2,
		AccountsLimit:  3,
		CanSyncMore:    true,
		ResetDate:      resetDate,
	}
	mockService.On("GetSyncStats", mock.Anything, userID).Return(expectedStats, nil)

	// Execute
	err := handler.GetSyncStats(c)

	// Assert
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"accounts_synced":2`)
	assert.Contains(t, rec.Body.String(), `"accounts_limit":3`)
	assert.Contains(t, rec.Body.String(), `"can_sync_more":true`)
	mockService.AssertExpectations(t)
}

// TestGetSyncStats_Unauthorized tests missing user authentication
func TestGetSyncStats_Unauthorized(t *testing.T) {
	// Setup
	mockService := new(MockSyncLimitService)
	handler := NewSyncStatsHandler(mockService)

	// Create Echo context WITHOUT user ID
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sync-stats", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	// Note: No c.Set("user_id", ...) - simulates missing auth

	// Execute
	err := handler.GetSyncStats(c)

	// Assert
	assert.Error(t, err)
	httpErr, ok := err.(*echo.HTTPError)
	assert.True(t, ok)
	assert.Equal(t, http.StatusUnauthorized, httpErr.Code)
	assert.Contains(t, httpErr.Message, "user not authenticated")
}

// TestGetSyncStats_ServiceError tests service layer error handling
func TestGetSyncStats_ServiceError(t *testing.T) {
	// Setup
	mockService := new(MockSyncLimitService)
	handler := NewSyncStatsHandler(mockService)

	// Create Echo context
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sync-stats", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	userID := "user-123"
	c.Set("user_id", userID)

	// Mock service error
	mockService.On("GetSyncStats", mock.Anything, userID).
		Return(nil, errors.New("database connection failed"))

	// Execute
	err := handler.GetSyncStats(c)

	// Assert
	assert.Error(t, err)
	httpErr, ok := err.(*echo.HTTPError)
	assert.True(t, ok)
	assert.Equal(t, http.StatusInternalServerError, httpErr.Code)
	mockService.AssertExpectations(t)
}

// TestGetSyncStats_FreeTierAtLimit tests free tier user at limit
func TestGetSyncStats_FreeTierAtLimit(t *testing.T) {
	// Setup
	mockService := new(MockSyncLimitService)
	handler := NewSyncStatsHandler(mockService)

	// Create Echo context
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sync-stats", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	userID := "user-free"
	c.Set("user_id", userID)

	// Mock free tier at limit (3 of 3 accounts synced)
	resetDate := time.Date(2025, 12, 1, 0, 0, 0, 0, time.UTC)
	expectedStats := &domain.SyncStats{
		UserID:         userID,
		Month:          "2025-11",
		AccountsSynced: 3,
		AccountsLimit:  3,
		CanSyncMore:    false, // At limit!
		ResetDate:      resetDate,
	}
	mockService.On("GetSyncStats", mock.Anything, userID).Return(expectedStats, nil)

	// Execute
	err := handler.GetSyncStats(c)

	// Assert
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"accounts_synced":3`)
	assert.Contains(t, rec.Body.String(), `"accounts_limit":3`)
	assert.Contains(t, rec.Body.String(), `"can_sync_more":false`)
	mockService.AssertExpectations(t)
}

// TestGetSyncStats_UnlimitedTier tests unlimited tier (Premium/Premium+)
func TestGetSyncStats_UnlimitedTier(t *testing.T) {
	// Setup
	mockService := new(MockSyncLimitService)
	handler := NewSyncStatsHandler(mockService)

	// Create Echo context
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sync-stats", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	userID := "user-premium"
	c.Set("user_id", userID)

	// Mock unlimited tier (AccountsLimit = -1)
	resetDate := time.Date(2025, 12, 1, 0, 0, 0, 0, time.UTC)
	expectedStats := &domain.SyncStats{
		UserID:         userID,
		Month:          "2025-11",
		AccountsSynced: 10,
		AccountsLimit:  -1,       // Unlimited!
		CanSyncMore:    true,
		ResetDate:      resetDate,
	}
	mockService.On("GetSyncStats", mock.Anything, userID).Return(expectedStats, nil)

	// Execute
	err := handler.GetSyncStats(c)

	// Assert
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"accounts_synced":10`)
	assert.Contains(t, rec.Body.String(), `"accounts_limit":-1`)
	assert.Contains(t, rec.Body.String(), `"can_sync_more":true`)
	mockService.AssertExpectations(t)
}
