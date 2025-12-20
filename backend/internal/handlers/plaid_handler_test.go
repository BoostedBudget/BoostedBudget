package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/middleware"
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

func (m *mockPlaidService) TransactionsSync(ctx context.Context, accessToken string, cursor string, daysRequested int) (*plaid.TransactionsSyncResponse, error) {
	return nil, errors.New("not implemented")
}

func (m *mockPlaidService) CreateUpdateModeLinkToken(ctx context.Context, userID string, accessToken string) (*domain.LinkTokenResponse, error) {
	return nil, errors.New("not implemented")
}

func (m *mockPlaidService) FireWebhook(ctx context.Context, accessToken string, webhookCode string) (*plaid.SandboxItemFireWebhookResponse, error) {
	return nil, errors.New("not implemented")
}

// mockAccountService is a mock implementation of AccountService for testing
type mockAccountService struct {
	linkAccountsFunc           func(ctx context.Context, userID, publicToken, institutionID, institutionName string) (*services.LinkAccountsResult, error)
	getAccountsFunc            func(ctx context.Context, userID string) ([]domain.Account, error)
	getAccountsFilteredFunc    func(ctx context.Context, userID string, includeHidden bool) ([]domain.Account, error)
	updateAccountFunc          func(ctx context.Context, accountID, userID string, req *domain.UpdateAccountRequest) (*domain.Account, error)
	reorderAccountsFunc        func(ctx context.Context, userID string, req *domain.ReorderAccountsRequest) error
	deleteAccountFunc          func(ctx context.Context, accountID, userID string) error
	disconnectInstitutionFunc  func(ctx context.Context, linkedItemID, userID string) error
}

func (m *mockAccountService) LinkAccounts(ctx context.Context, userID, publicToken, institutionID, institutionName string) (*services.LinkAccountsResult, error) {
	if m.linkAccountsFunc != nil {
		return m.linkAccountsFunc(ctx, userID, publicToken, institutionID, institutionName)
	}
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

func (m *mockAccountService) GetAccountsFiltered(ctx context.Context, userID string, includeHidden bool) ([]domain.Account, error) {
	if m.getAccountsFilteredFunc != nil {
		return m.getAccountsFilteredFunc(ctx, userID, includeHidden)
	}
	return nil, errors.New("not implemented")
}

func (m *mockAccountService) UpdateAccount(ctx context.Context, accountID, userID string, req *domain.UpdateAccountRequest) (*domain.Account, error) {
	if m.updateAccountFunc != nil {
		return m.updateAccountFunc(ctx, accountID, userID, req)
	}
	return nil, errors.New("not implemented")
}

func (m *mockAccountService) ReorderAccounts(ctx context.Context, userID string, req *domain.ReorderAccountsRequest) error {
	if m.reorderAccountsFunc != nil {
		return m.reorderAccountsFunc(ctx, userID, req)
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
		getAccountsFilteredFunc: func(ctx context.Context, userID string, includeHidden bool) ([]domain.Account, error) {
			assert.Equal(t, "auth0|test-user-123", userID, "Should pass correct user ID")
			assert.False(t, includeHidden, "Should not include hidden by default")
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
		getAccountsFilteredFunc: func(ctx context.Context, userID string, includeHidden bool) ([]domain.Account, error) {
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
		getAccountsFilteredFunc: func(ctx context.Context, userID string, includeHidden bool) ([]domain.Account, error) {
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

// TestExchangePublicToken_FreeTierLimitExceeded tests that free tier users are blocked when exceeding account limit
// This test validates Scenario #4 (tier-limit-enforcement):
// - Free tier users have a 3-account limit
// - Attempting to link a 4th account should return 403 Forbidden
// - Error message should indicate tier limit exceeded
func TestExchangePublicToken_FreeTierLimitExceeded(t *testing.T) {
	// Given: Mock service that returns account limit exceeded error (free tier at 3-account limit)
	mockService := &mockAccountService{
		linkAccountsFunc: func(ctx context.Context, userID, publicToken, institutionID, institutionName string) (*services.LinkAccountsResult, error) {
			assert.Equal(t, "auth0|free-user-123", userID, "Should pass correct user ID")
			assert.Equal(t, "public-sandbox-test-token", publicToken, "Should pass public token")
			assert.Equal(t, "ins_3", institutionID, "Should pass institution ID")
			assert.Equal(t, "Chase", institutionName, "Should pass institution name")

			// Service layer detects free tier user already has 3 accounts
			return nil, services.ErrAccountLimitExceeded
		},
	}

	handler := NewPlaidHandler(nil, mockService)

	// When: Free tier user with 3 accounts attempts to exchange token for 4th account
	e := echo.New()
	e.Validator = middleware.NewValidator()
	reqBody := `{"public_token":"public-sandbox-test-token","institution_id":"ins_3","institution_name":"Chase"}`
	req := httptest.NewRequest(http.MethodPost, "/plaid/exchange-token", strings.NewReader(reqBody))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", "auth0|free-user-123") // Free tier user attempting to add 4th account

	err := handler.ExchangePublicToken(c)

	// Then: Returns 403 Forbidden with tier limit error message
	require.Error(t, err, "Expected error from handler")
	httpErr := err.(*echo.HTTPError)
	assert.Equal(t, http.StatusForbidden, httpErr.Code, "Expected 403 Forbidden for tier limit")
	assert.Contains(t, httpErr.Message, "Account limit exceeded", "Error message should mention limit")
	assert.Contains(t, httpErr.Message, "subscription tier", "Error message should mention subscription")
}

// TestExchangePublicToken_PremiumAllowsMoreAccounts tests that premium tier users can exceed free tier limit
// This test validates Scenario #4 (tier-limit-enforcement):
// - Premium tier users have higher account limits (Premium: 30, Premium+: unlimited)
// - Premium users should successfully link accounts beyond free tier's 3-account limit
// - Response should include linked item ID and account count
func TestExchangePublicToken_PremiumAllowsMoreAccounts(t *testing.T) {
	// Given: Mock service that successfully links accounts for premium user (already has 5 accounts, adding more)
	expectedItemID := "item-premium-" + uuid.New().String()
	expectedAccounts := []domain.Account{
		{ID: uuid.New().String(), Name: "Premium Checking", Type: "depository"},
		{ID: uuid.New().String(), Name: "Premium Savings", Type: "depository"},
		{ID: uuid.New().String(), Name: "Premium Credit", Type: "credit"},
	}

	mockService := &mockAccountService{
		linkAccountsFunc: func(ctx context.Context, userID, publicToken, institutionID, institutionName string) (*services.LinkAccountsResult, error) {
			assert.Equal(t, "auth0|premium-user-456", userID, "Should pass correct user ID")
			assert.Equal(t, "public-sandbox-premium-token", publicToken, "Should pass public token")
			assert.Equal(t, "ins_3", institutionID, "Should pass institution ID")
			assert.Equal(t, "Chase", institutionName, "Should pass institution name")

			// Service allows premium user to link accounts beyond free tier limit
			return &services.LinkAccountsResult{
				LinkedItem: &domain.LinkedItem{
					ItemID:          expectedItemID,
					InstitutionID:   institutionID,
					InstitutionName: institutionName,
				},
				Accounts: expectedAccounts,
			}, nil
		},
	}

	handler := NewPlaidHandler(nil, mockService)

	// When: Premium tier user (already has 5 accounts) exchanges token for 3 more accounts
	e := echo.New()
	e.Validator = middleware.NewValidator()
	reqBody := `{"public_token":"public-sandbox-premium-token","institution_id":"ins_3","institution_name":"Chase"}`
	req := httptest.NewRequest(http.MethodPost, "/plaid/exchange-token", strings.NewReader(reqBody))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", "auth0|premium-user-456") // Premium user, no limit constraint

	err := handler.ExchangePublicToken(c)

	// Then: Returns 200 OK with successful link response
	require.NoError(t, err, "Premium user should successfully link accounts")
	assert.Equal(t, http.StatusOK, rec.Code, "Expected 200 OK for premium user")

	// Verify response body
	var response ExchangePublicTokenResponse
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	require.NoError(t, err, "Should parse response body")
	assert.True(t, response.Success, "Success should be true")
	assert.Equal(t, expectedItemID, response.ItemID, "Should return correct item ID")
	assert.Equal(t, 3, response.Accounts, "Should return count of linked accounts")
}

// ===========================================================================
// Account Customization Handler Tests (Issue #307)
// ===========================================================================

// TestGetAccounts_IncludeHidden tests that hidden accounts are included when param is set
func TestGetAccounts_IncludeHidden(t *testing.T) {
	// Given: Mock service that returns all accounts including hidden
	allAccounts := []domain.Account{
		{ID: uuid.New().String(), Name: "Visible Account", IsHidden: false},
		{ID: uuid.New().String(), Name: "Hidden Account", IsHidden: true},
	}

	mockService := &mockAccountService{
		getAccountsFilteredFunc: func(ctx context.Context, userID string, includeHidden bool) ([]domain.Account, error) {
			assert.True(t, includeHidden, "Should include hidden when param is true")
			return allAccounts, nil
		},
	}

	handler := NewPlaidHandler(nil, mockService)

	// When: Making GET request with include_hidden=true
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/accounts?include_hidden=true", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", "auth0|test-user-123")

	err := handler.GetAccounts(c)

	// Then: Returns 200 with all accounts including hidden
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "Visible Account")
	assert.Contains(t, rec.Body.String(), "Hidden Account")
	assert.Contains(t, rec.Body.String(), "\"total_count\":2")
}

// TestUpdateAccount_Success tests successful account customization update
func TestUpdateAccount_Success(t *testing.T) {
	accountID := uuid.New().String()
	nickname := "My Primary Account"

	updatedAccount := &domain.Account{
		ID:       accountID,
		Name:     "Chase Checking",
		Nickname: &nickname,
	}

	mockService := &mockAccountService{
		updateAccountFunc: func(ctx context.Context, acctID, userID string, req *domain.UpdateAccountRequest) (*domain.Account, error) {
			assert.Equal(t, accountID, acctID)
			assert.Equal(t, "auth0|test-user-123", userID)
			assert.NotNil(t, req.Nickname)
			return updatedAccount, nil
		},
	}

	handler := NewPlaidHandler(nil, mockService)

	e := echo.New()
	reqBody := `{"nickname":"My Primary Account"}`
	req := httptest.NewRequest(http.MethodPatch, "/accounts/"+accountID, strings.NewReader(reqBody))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", "auth0|test-user-123")
	c.SetParamNames("id")
	c.SetParamValues(accountID)

	err := handler.UpdateAccount(c)

	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "My Primary Account")
}

// TestUpdateAccount_Unauthorized tests update without authentication
func TestUpdateAccount_Unauthorized(t *testing.T) {
	mockService := &mockAccountService{}
	handler := NewPlaidHandler(nil, mockService)

	e := echo.New()
	accountID := uuid.New().String()
	req := httptest.NewRequest(http.MethodPatch, "/accounts/"+accountID, strings.NewReader(`{}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues(accountID)

	err := handler.UpdateAccount(c)

	require.Error(t, err)
	httpErr := err.(*echo.HTTPError)
	assert.Equal(t, http.StatusUnauthorized, httpErr.Code)
}

// TestUpdateAccount_NotFound tests update of non-existent account
func TestUpdateAccount_NotFound(t *testing.T) {
	accountID := uuid.New().String()

	mockService := &mockAccountService{
		updateAccountFunc: func(ctx context.Context, acctID, userID string, req *domain.UpdateAccountRequest) (*domain.Account, error) {
			return nil, repository.ErrAccountNotFound
		},
	}

	handler := NewPlaidHandler(nil, mockService)

	e := echo.New()
	req := httptest.NewRequest(http.MethodPatch, "/accounts/"+accountID, strings.NewReader(`{"nickname":"Test"}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", "auth0|test-user-123")
	c.SetParamNames("id")
	c.SetParamValues(accountID)

	err := handler.UpdateAccount(c)

	require.Error(t, err)
	httpErr := err.(*echo.HTTPError)
	assert.Equal(t, http.StatusNotFound, httpErr.Code)
}

// TestUpdateAccount_ValidationError tests update with invalid color
func TestUpdateAccount_ValidationError(t *testing.T) {
	accountID := uuid.New().String()

	mockService := &mockAccountService{
		updateAccountFunc: func(ctx context.Context, acctID, userID string, req *domain.UpdateAccountRequest) (*domain.Account, error) {
			return nil, errors.New("custom_color must be a valid hex color (format: #RRGGBB)")
		},
	}

	handler := NewPlaidHandler(nil, mockService)

	e := echo.New()
	req := httptest.NewRequest(http.MethodPatch, "/accounts/"+accountID, strings.NewReader(`{"custom_color":"invalid"}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", "auth0|test-user-123")
	c.SetParamNames("id")
	c.SetParamValues(accountID)

	err := handler.UpdateAccount(c)

	require.Error(t, err)
	httpErr := err.(*echo.HTTPError)
	assert.Equal(t, http.StatusBadRequest, httpErr.Code)
}

// TestReorderAccounts_Success tests successful bulk reorder
func TestReorderAccounts_Success(t *testing.T) {
	mockService := &mockAccountService{
		reorderAccountsFunc: func(ctx context.Context, userID string, req *domain.ReorderAccountsRequest) error {
			assert.Equal(t, "auth0|test-user-123", userID)
			assert.Len(t, req.AccountOrders, 2)
			return nil
		},
	}

	handler := NewPlaidHandler(nil, mockService)

	e := echo.New()
	reqBody := `{"account_orders":[{"account_id":"acc-1","display_order":0},{"account_id":"acc-2","display_order":1}]}`
	req := httptest.NewRequest(http.MethodPatch, "/accounts/reorder", strings.NewReader(reqBody))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", "auth0|test-user-123")

	err := handler.ReorderAccounts(c)

	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"success":true`)
}

// TestReorderAccounts_Unauthorized tests reorder without authentication
func TestReorderAccounts_Unauthorized(t *testing.T) {
	mockService := &mockAccountService{}
	handler := NewPlaidHandler(nil, mockService)

	e := echo.New()
	req := httptest.NewRequest(http.MethodPatch, "/accounts/reorder", strings.NewReader(`{}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := handler.ReorderAccounts(c)

	require.Error(t, err)
	httpErr := err.(*echo.HTTPError)
	assert.Equal(t, http.StatusUnauthorized, httpErr.Code)
}

// TestReorderAccounts_EmptyList tests reorder with empty list
func TestReorderAccounts_EmptyList(t *testing.T) {
	mockService := &mockAccountService{
		reorderAccountsFunc: func(ctx context.Context, userID string, req *domain.ReorderAccountsRequest) error {
			return errors.New("account orders cannot be empty")
		},
	}

	handler := NewPlaidHandler(nil, mockService)

	e := echo.New()
	reqBody := `{"account_orders":[]}`
	req := httptest.NewRequest(http.MethodPatch, "/accounts/reorder", strings.NewReader(reqBody))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", "auth0|test-user-123")

	err := handler.ReorderAccounts(c)

	require.Error(t, err)
	httpErr := err.(*echo.HTTPError)
	assert.Equal(t, http.StatusBadRequest, httpErr.Code)
}
