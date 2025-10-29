package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
	"github.com/plaid/plaid-go/v23/plaid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockPlaidService is a mock implementation of PlaidService for testing
type mockPlaidService struct {
	createLinkTokenFunc func(ctx context.Context, userID string) (*domain.LinkTokenResponse, error)
	removeItemFunc      func(ctx context.Context, accessToken string) error
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

func (m *mockPlaidService) RemoveItem(ctx context.Context, accessToken string) error {
	if m.removeItemFunc != nil {
		return m.removeItemFunc(ctx, accessToken)
	}
	return errors.New("not implemented")
}

func (m *mockPlaidService) TransactionsSync(ctx context.Context, accessToken string, cursor string) (*plaid.TransactionsSyncResponse, error) {
	return nil, errors.New("not implemented")
}

// mockAccountService is a mock implementation of AccountService for testing
type mockAccountService struct {
	getAccountsFunc          func(ctx context.Context, userID string) ([]domain.Account, error)
	deleteAccountFunc        func(ctx context.Context, accountID, userID string) error
	disconnectInstitutionFunc func(ctx context.Context, linkedItemID, userID string) error
}

func (m *mockAccountService) LinkAccounts(ctx context.Context, userID, publicToken, institutionID, institutionName string) (*services.LinkAccountsResult, error) {
	return nil, errors.New("not implemented")
}

func (m *mockAccountService) GetAccounts(ctx context.Context, userID string) ([]domain.Account, error) {
	if m.getAccountsFunc != nil {
		return m.getAccountsFunc(ctx, userID)
	}
	return nil, errors.New("not implemented")
}

func (m *mockAccountService) DeleteAccount(ctx context.Context, accountID, userID string) error {
	if m.deleteAccountFunc != nil {
		return m.deleteAccountFunc(ctx, accountID, userID)
	}
	return errors.New("not implemented")
}

func (m *mockAccountService) DisconnectInstitution(ctx context.Context, linkedItemID, userID string) error {
	if m.disconnectInstitutionFunc != nil {
		return m.disconnectInstitutionFunc(ctx, linkedItemID, userID)
	}
	return errors.New("not implemented")
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

	handler := NewPlaidHandler(mockService, nil) // nil AccountService - not needed for this test

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
	handler := NewPlaidHandler(mockService, nil) // nil AccountService - not needed for this test

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

	handler := NewPlaidHandler(mockService, nil) // nil AccountService - not needed for this test

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

// TestGetAccounts_Success tests successful retrieval of linked accounts
func TestGetAccounts_Success(t *testing.T) {
	// Given: Mock service that returns accounts with institution names
	expectedAccounts := []domain.Account{
		{
			ID:              uuid.New().String(),
			UserID:          "auth0|test-user-123",
			PlaidAccountID:  "plaid-acc-1",
			Name:            "Chase Checking",
			Type:            "depository",
			CurrencyCode:    "USD",
			InstitutionName: "Chase",
		},
		{
			ID:              uuid.New().String(),
			UserID:          "auth0|test-user-123",
			PlaidAccountID:  "plaid-acc-2",
			Name:            "Wells Fargo Savings",
			Type:            "depository",
			CurrencyCode:    "USD",
			InstitutionName: "Wells Fargo",
		},
	}

	mockService := &mockAccountService{
		getAccountsFunc: func(ctx context.Context, userID string) ([]domain.Account, error) {
			assert.Equal(t, "auth0|test-user-123", userID, "Should pass correct user ID")
			return expectedAccounts, nil
		},
	}

	handler := NewPlaidHandler(nil, mockService) // nil PlaidService - not needed for this test

	// When: Making GET request with authenticated user
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/accounts", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Simulate Auth0 middleware setting user_id
	c.Set("user_id", "auth0|test-user-123")

	err := handler.GetAccounts(c)

	// Then: Returns 200 with accounts
	require.NoError(t, err, "Expected no error from handler")
	assert.Equal(t, http.StatusOK, rec.Code, "Expected 200 OK")

	// Verify response contains accounts and institution names
	assert.Contains(t, rec.Body.String(), "Chase Checking", "Response should contain account name")
	assert.Contains(t, rec.Body.String(), "Chase", "Response should contain institution name")
	assert.Contains(t, rec.Body.String(), "Wells Fargo", "Response should contain institution name")
	assert.Contains(t, rec.Body.String(), "\"total_count\":2", "Response should contain total count")
}

// TestGetAccounts_Unauthorized tests that requests without authentication are rejected
func TestGetAccounts_Unauthorized(t *testing.T) {
	// Given: Handler with mock service
	mockService := &mockAccountService{}
	handler := NewPlaidHandler(nil, mockService)

	// When: Making GET request WITHOUT user_id in context (not authenticated)
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/accounts", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	// Note: NOT setting user_id in context

	err := handler.GetAccounts(c)

	// Then: Returns 401 Unauthorized error
	require.Error(t, err, "Expected error when user not authenticated")
	httpErr, ok := err.(*echo.HTTPError)
	require.True(t, ok, "Expected echo.HTTPError")
	assert.Equal(t, http.StatusUnauthorized, httpErr.Code, "Expected 401 Unauthorized")
}

// TestGetAccounts_ServiceError tests handling of service-layer failures
func TestGetAccounts_ServiceError(t *testing.T) {
	// Given: Mock service that returns an error
	mockService := &mockAccountService{
		getAccountsFunc: func(ctx context.Context, userID string) ([]domain.Account, error) {
			return nil, errors.New("database connection failed")
		},
	}

	handler := NewPlaidHandler(nil, mockService)

	// When: Making GET request with authenticated user
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/accounts", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", "auth0|test-user-123")

	err := handler.GetAccounts(c)

	// Then: Returns 500 Internal Server Error
	require.Error(t, err, "Expected error when service fails")
	httpErr, ok := err.(*echo.HTTPError)
	require.True(t, ok, "Expected echo.HTTPError")
	assert.Equal(t, http.StatusInternalServerError, httpErr.Code, "Expected 500 Internal Server Error")
}

// TestGetAccounts_EmptyResponse tests when user has no linked accounts
func TestGetAccounts_EmptyResponse(t *testing.T) {
	// Given: Mock service that returns empty accounts slice
	mockService := &mockAccountService{
		getAccountsFunc: func(ctx context.Context, userID string) ([]domain.Account, error) {
			return []domain.Account{}, nil
		},
	}

	handler := NewPlaidHandler(nil, mockService)

	// When: Making GET request with authenticated user
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/accounts", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", "auth0|test-user-123")

	err := handler.GetAccounts(c)

	// Then: Returns 200 with empty accounts array
	require.NoError(t, err, "Expected no error from handler")
	assert.Equal(t, http.StatusOK, rec.Code, "Expected 200 OK")
	assert.Contains(t, rec.Body.String(), "\"total_count\":0", "Response should indicate zero accounts")
	assert.Contains(t, rec.Body.String(), "\"accounts\":[]", "Response should contain empty accounts array")
}

// TestDeleteAccount_Success tests successful account deletion with authenticated user
func TestDeleteAccount_Success(t *testing.T) {
	// Given: Mock service that successfully deletes account
	accountID := uuid.New().String()
	userID := "auth0|test-user-123"

	mockService := &mockAccountService{
		deleteAccountFunc: func(ctx context.Context, acctID, uid string) error {
			assert.Equal(t, accountID, acctID, "Should pass correct account ID")
			assert.Equal(t, userID, uid, "Should pass correct user ID")
			return nil
		},
	}

	handler := NewPlaidHandler(nil, mockService)

	// When: Making DELETE request with authenticated user
	e := echo.New()
	req := httptest.NewRequest(http.MethodDelete, "/accounts/"+accountID, nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", userID)
	c.SetParamNames("id")
	c.SetParamValues(accountID)

	err := handler.DeleteAccount(c)

	// Then: Returns 204 No Content
	require.NoError(t, err, "Expected no error from handler")
	assert.Equal(t, http.StatusNoContent, rec.Code, "Expected 204 No Content")
}

// TestDeleteAccount_Unauthorized tests that requests without authentication are rejected
func TestDeleteAccount_Unauthorized(t *testing.T) {
	// Given: Handler with mock service
	mockService := &mockAccountService{}
	handler := NewPlaidHandler(nil, mockService)

	// When: Making DELETE request WITHOUT user_id in context (not authenticated)
	e := echo.New()
	accountID := uuid.New().String()
	req := httptest.NewRequest(http.MethodDelete, "/accounts/"+accountID, nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues(accountID)
	// NOTE: NOT setting user_id in context

	err := handler.DeleteAccount(c)

	// Then: Returns 401 Unauthorized
	require.Error(t, err, "Expected error from handler")
	httpErr := err.(*echo.HTTPError)
	assert.Equal(t, http.StatusUnauthorized, httpErr.Code, "Expected 401 Unauthorized")
}

// TestDeleteAccount_AccountNotFound tests deletion of non-existent account
func TestDeleteAccount_AccountNotFound(t *testing.T) {
	// Given: Mock service that returns account not found error
	accountID := uuid.New().String()
	mockService := &mockAccountService{
		deleteAccountFunc: func(ctx context.Context, acctID, uid string) error {
			return repository.ErrAccountNotFound
		},
	}

	handler := NewPlaidHandler(nil, mockService)

	// When: Making DELETE request
	e := echo.New()
	req := httptest.NewRequest(http.MethodDelete, "/accounts/"+accountID, nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", "auth0|test-user-123")
	c.SetParamNames("id")
	c.SetParamValues(accountID)

	err := handler.DeleteAccount(c)

	// Then: Returns 404 Not Found
	require.Error(t, err, "Expected error from handler")
	httpErr := err.(*echo.HTTPError)
	assert.Equal(t, http.StatusNotFound, httpErr.Code, "Expected 404 Not Found")
}

// TestDeleteAccount_InternalError tests handling of service errors
func TestDeleteAccount_InternalError(t *testing.T) {
	// Given: Mock service that returns generic error
	accountID := uuid.New().String()
	mockService := &mockAccountService{
		deleteAccountFunc: func(ctx context.Context, acctID, uid string) error {
			return errors.New("database connection failed")
		},
	}

	handler := NewPlaidHandler(nil, mockService)

	// When: Making DELETE request
	e := echo.New()
	req := httptest.NewRequest(http.MethodDelete, "/accounts/"+accountID, nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", "auth0|test-user-123")
	c.SetParamNames("id")
	c.SetParamValues(accountID)

	err := handler.DeleteAccount(c)

	// Then: Returns 500 Internal Server Error
	require.Error(t, err, "Expected error from handler")
	httpErr := err.(*echo.HTTPError)
	assert.Equal(t, http.StatusInternalServerError, httpErr.Code, "Expected 500 Internal Server Error")
}

// TestDisconnectInstitution_Success tests successful institution disconnection with authenticated user
func TestDisconnectInstitution_Success(t *testing.T) {
	// Given: Mock service that successfully disconnects institution
	linkedItemID := uuid.New().String()
	userID := "auth0|test-user-123"

	mockService := &mockAccountService{
		disconnectInstitutionFunc: func(ctx context.Context, itemID, uid string) error {
			assert.Equal(t, linkedItemID, itemID, "Should pass correct linked item ID")
			assert.Equal(t, userID, uid, "Should pass correct user ID")
			return nil
		},
	}

	handler := NewPlaidHandler(nil, mockService)

	// When: Making DELETE request with authenticated user
	e := echo.New()
	req := httptest.NewRequest(http.MethodDelete, "/linked-items/"+linkedItemID, nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", userID)
	c.SetParamNames("id")
	c.SetParamValues(linkedItemID)

	err := handler.DisconnectInstitution(c)

	// Then: Returns 204 No Content
	require.NoError(t, err, "Expected no error from handler")
	assert.Equal(t, http.StatusNoContent, rec.Code, "Expected 204 No Content")
}

// TestDisconnectInstitution_Unauthorized tests that requests without authentication are rejected
func TestDisconnectInstitution_Unauthorized(t *testing.T) {
	// Given: Handler with mock service
	mockService := &mockAccountService{}
	handler := NewPlaidHandler(nil, mockService)

	// When: Making DELETE request WITHOUT user_id in context (not authenticated)
	e := echo.New()
	linkedItemID := uuid.New().String()
	req := httptest.NewRequest(http.MethodDelete, "/linked-items/"+linkedItemID, nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues(linkedItemID)
	// NOTE: NOT setting user_id in context

	err := handler.DisconnectInstitution(c)

	// Then: Returns 401 Unauthorized
	require.Error(t, err, "Expected error from handler")
	httpErr := err.(*echo.HTTPError)
	assert.Equal(t, http.StatusUnauthorized, httpErr.Code, "Expected 401 Unauthorized")
}

// TestDisconnectInstitution_LinkedItemNotFound tests disconnection of non-existent institution
func TestDisconnectInstitution_LinkedItemNotFound(t *testing.T) {
	// Given: Mock service that returns linked item not found error
	linkedItemID := uuid.New().String()
	mockService := &mockAccountService{
		disconnectInstitutionFunc: func(ctx context.Context, itemID, uid string) error {
			return repository.ErrLinkedItemNotFound
		},
	}

	handler := NewPlaidHandler(nil, mockService)

	// When: Making DELETE request
	e := echo.New()
	req := httptest.NewRequest(http.MethodDelete, "/linked-items/"+linkedItemID, nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", "auth0|test-user-123")
	c.SetParamNames("id")
	c.SetParamValues(linkedItemID)

	err := handler.DisconnectInstitution(c)

	// Then: Returns 404 Not Found
	require.Error(t, err, "Expected error from handler")
	httpErr := err.(*echo.HTTPError)
	assert.Equal(t, http.StatusNotFound, httpErr.Code, "Expected 404 Not Found")
}

// TestDisconnectInstitution_InternalError tests handling of service errors
func TestDisconnectInstitution_InternalError(t *testing.T) {
	// Given: Mock service that returns generic error
	linkedItemID := uuid.New().String()
	mockService := &mockAccountService{
		disconnectInstitutionFunc: func(ctx context.Context, itemID, uid string) error {
			return errors.New("database connection failed")
		},
	}

	handler := NewPlaidHandler(nil, mockService)

	// When: Making DELETE request
	e := echo.New()
	req := httptest.NewRequest(http.MethodDelete, "/linked-items/"+linkedItemID, nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", "auth0|test-user-123")
	c.SetParamNames("id")
	c.SetParamValues(linkedItemID)

	err := handler.DisconnectInstitution(c)

	// Then: Returns 500 Internal Server Error
	require.Error(t, err, "Expected error from handler")
	httpErr := err.(*echo.HTTPError)
	assert.Equal(t, http.StatusInternalServerError, httpErr.Code, "Expected 500 Internal Server Error")
}
