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
	"github.com/plaid/plaid-go/v23/plaid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockPlaidService is a mock implementation of PlaidService for testing
type mockPlaidService struct {
	createLinkTokenFunc func(ctx context.Context, userID string) (*domain.LinkTokenResponse, error)
}

func (m *mockPlaidService) GetClient() *plaid.APIClient {
	return nil
}

func (m *mockPlaidService) CreateLinkToken(ctx context.Context, userID string) (*domain.LinkTokenResponse, error) {
	if m.createLinkTokenFunc != nil {
		return m.createLinkTokenFunc(ctx, userID)
	}
	return nil, errors.New("not implemented")
}

// TestCreateLinkToken_Success tests successful link token creation with authenticated user
func TestCreateLinkToken_Success(t *testing.T) {
	// Given: Mock service that returns a valid link token
	expectedToken := &domain.LinkTokenResponse{
		LinkToken:  "link-sandbox-test-token-12345",
		Expiration: time.Now().Add(30 * time.Minute),
		RequestID:  "test-request-id",
	}

	mockService := &mockPlaidService{
		createLinkTokenFunc: func(ctx context.Context, userID string) (*domain.LinkTokenResponse, error) {
			assert.Equal(t, "auth0|test-user-123", userID, "Should pass correct user ID")
			return expectedToken, nil
		},
	}

	handler := NewPlaidHandler(mockService)

	// When: Making POST request with authenticated user
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/plaid/link-token", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Simulate Auth0 middleware setting user_id
	c.Set("user_id", "auth0|test-user-123")

	err := handler.CreateLinkToken(c)

	// Then: Returns 200 with link token
	require.NoError(t, err, "Expected no error from handler")
	assert.Equal(t, http.StatusOK, rec.Code, "Expected 200 OK")

	// Verify response contains link token
	assert.Contains(t, rec.Body.String(), "link-sandbox-test-token-12345", "Response should contain link token")
	assert.Contains(t, rec.Body.String(), "test-request-id", "Response should contain request ID")
}

// TestCreateLinkToken_Unauthorized tests that requests without authentication are rejected
func TestCreateLinkToken_Unauthorized(t *testing.T) {
	// Given: Handler with mock service
	mockService := &mockPlaidService{}
	handler := NewPlaidHandler(mockService)

	// When: Making POST request WITHOUT user_id in context (not authenticated)
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/plaid/link-token", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	// Note: NOT setting user_id in context

	err := handler.CreateLinkToken(c)

	// Then: Returns 401 Unauthorized error
	require.Error(t, err, "Expected error when user not authenticated")
	httpErr, ok := err.(*echo.HTTPError)
	require.True(t, ok, "Expected echo.HTTPError")
	assert.Equal(t, http.StatusUnauthorized, httpErr.Code, "Expected 401 Unauthorized")
}

// TestCreateLinkToken_ServiceError tests handling of service-layer failures
func TestCreateLinkToken_ServiceError(t *testing.T) {
	// Given: Mock service that returns an error
	mockService := &mockPlaidService{
		createLinkTokenFunc: func(ctx context.Context, userID string) (*domain.LinkTokenResponse, error) {
			return nil, errors.New("plaid API error: rate limit exceeded")
		},
	}

	handler := NewPlaidHandler(mockService)

	// When: Making POST request with authenticated user
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/plaid/link-token", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", "auth0|test-user-123")

	err := handler.CreateLinkToken(c)

	// Then: Returns 500 Internal Server Error
	require.Error(t, err, "Expected error when service fails")
	httpErr, ok := err.(*echo.HTTPError)
	require.True(t, ok, "Expected echo.HTTPError")
	assert.Equal(t, http.StatusInternalServerError, httpErr.Code, "Expected 500 Internal Server Error")
}
