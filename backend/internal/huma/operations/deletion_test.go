package operations

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockDeletionService is a mock implementation of services.DeletionService
type mockDeletionService struct {
	cancellationToken string
	scheduledAt       time.Time
	requestErr        error
	cancelErr         error
}

func (m *mockDeletionService) RequestDeletion(ctx context.Context, userID, email string) (string, time.Time, error) {
	if m.requestErr != nil {
		return "", time.Time{}, m.requestErr
	}
	return m.cancellationToken, m.scheduledAt, nil
}

func (m *mockDeletionService) CancelDeletion(ctx context.Context, token string) error {
	return m.cancelErr
}

func (m *mockDeletionService) ExecuteDeletion(ctx context.Context, userID string) error {
	return nil
}

// Issue #668: Plaid Item Management for Cost Optimization
func (m *mockDeletionService) SetPlaidCleanupService(svc services.PlaidItemCleanupService) {
	// No-op for mock
}

// TestRequestDeletion_Success tests successful deletion request.
func TestRequestDeletion_Success(t *testing.T) {
	scheduledAt := time.Now().Add(30 * 24 * time.Hour)
	mockService := &mockDeletionService{
		cancellationToken: "test-token-123",
		scheduledAt:       scheduledAt,
	}

	handler := NewDeletionHandler(mockService)

	input := &RequestDeletionInput{
		AuthenticatedWithEmailInput: inputs.AuthenticatedWithEmailInput{
			AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
			Email:              "test@example.com",
		},
	}

	output, err := handler.RequestDeletion(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, "Account deletion scheduled", output.Body.Message)
	assert.Equal(t, "test-token-123", output.Body.CancellationToken)
	assert.Contains(t, output.Body.ScheduledAt, scheduledAt.Format("2006-01-02"))
}

// TestRequestDeletion_ServiceError tests error handling for deletion request.
func TestRequestDeletion_ServiceError(t *testing.T) {
	mockService := &mockDeletionService{
		requestErr: errors.New("service error"),
	}

	handler := NewDeletionHandler(mockService)

	input := &RequestDeletionInput{
		AuthenticatedWithEmailInput: inputs.AuthenticatedWithEmailInput{
			AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
			Email:              "test@example.com",
		},
	}

	output, err := handler.RequestDeletion(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "account deletion")
}

// TestCancelDeletion_Success tests successful deletion cancellation.
func TestCancelDeletion_Success(t *testing.T) {
	mockService := &mockDeletionService{}

	handler := NewDeletionHandler(mockService)

	input := &CancelDeletionInput{
		Token: "valid-token-123",
	}

	output, err := handler.CancelDeletion(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, "Account deletion cancelled", output.Body.Message)
}

// TestCancelDeletion_EmptyToken tests error when token is empty.
func TestCancelDeletion_EmptyToken(t *testing.T) {
	mockService := &mockDeletionService{}

	handler := NewDeletionHandler(mockService)

	input := &CancelDeletionInput{
		Token: "",
	}

	output, err := handler.CancelDeletion(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "token required")
}

// TestCancelDeletion_InvalidToken tests error when token is invalid.
func TestCancelDeletion_InvalidToken(t *testing.T) {
	mockService := &mockDeletionService{
		cancelErr: errors.New("invalid token"),
	}

	handler := NewDeletionHandler(mockService)

	input := &CancelDeletionInput{
		Token: "invalid-token",
	}

	output, err := handler.CancelDeletion(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Invalid or expired")
}

// TestDeletionRouteRegistration tests that routes are properly registered.
func TestDeletionRouteRegistration(t *testing.T) {
	mockService := &mockDeletionService{
		cancellationToken: "test-token",
		scheduledAt:       time.Now().Add(30 * 24 * time.Hour),
	}

	_, api := humatest.New(t)
	handler := NewDeletionHandler(mockService)
	RegisterDeletionRoutes(api, handler)

	// Test POST /api/v1/user/delete (without auth, should fail with 401)
	resp := api.Post("/api/v1/user/delete", "application/json", map[string]string{})
	assert.Equal(t, http.StatusUnauthorized, resp.Code)

	// Test GET /api/v1/user/delete/cancel/{token} (public endpoint, should succeed)
	resp = api.Get("/api/v1/user/delete/cancel/test-token")
	assert.Equal(t, http.StatusOK, resp.Code)
}
