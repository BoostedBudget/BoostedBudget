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
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
	"github.com/plaid/plaid-go/v23/plaid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockPlaidServiceForPlaid is a mock implementation of services.PlaidService
type mockPlaidServiceForPlaid struct {
	linkToken *domain.LinkTokenResponse
	createErr error
}

func (m *mockPlaidServiceForPlaid) GetClient() *plaid.APIClient {
	return nil
}

func (m *mockPlaidServiceForPlaid) CreateLinkToken(ctx context.Context, userID string) (*domain.LinkTokenResponse, error) {
	if m.createErr != nil {
		return nil, m.createErr
	}
	return m.linkToken, nil
}

func (m *mockPlaidServiceForPlaid) CreateUpdateModeLinkToken(ctx context.Context, userID string, accessToken string) (*domain.LinkTokenResponse, error) {
	return m.linkToken, nil
}

func (m *mockPlaidServiceForPlaid) RemoveItem(ctx context.Context, accessToken string) error {
	return nil
}

func (m *mockPlaidServiceForPlaid) TransactionsSync(ctx context.Context, accessToken string, cursor string, daysRequested int) (*plaid.TransactionsSyncResponse, error) {
	return nil, nil
}

func (m *mockPlaidServiceForPlaid) FireWebhook(ctx context.Context, accessToken string, webhookCode string) (*plaid.SandboxItemFireWebhookResponse, error) {
	return nil, nil
}

// mockAccountServiceForPlaid is a mock implementation of services.AccountService
type mockAccountServiceForPlaid struct {
	accounts      []domain.Account
	account       *domain.Account
	linkResult    *services.LinkAccountsResult
	getErr        error
	linkErr       error
	updateErr     error
	deleteErr     error
	disconnectErr error
	reorderErr    error
}

func (m *mockAccountServiceForPlaid) LinkAccounts(ctx context.Context, userID, publicToken, institutionID, institutionName string) (*services.LinkAccountsResult, error) {
	if m.linkErr != nil {
		return nil, m.linkErr
	}
	return m.linkResult, nil
}

func (m *mockAccountServiceForPlaid) GetAccounts(ctx context.Context, userID string) ([]domain.Account, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	return m.accounts, nil
}

func (m *mockAccountServiceForPlaid) GetAccountsFiltered(ctx context.Context, userID string, includeHidden bool) ([]domain.Account, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	return m.accounts, nil
}

func (m *mockAccountServiceForPlaid) UpdateAccount(ctx context.Context, accountID, userID string, req *domain.UpdateAccountRequest) (*domain.Account, error) {
	if m.updateErr != nil {
		return nil, m.updateErr
	}
	return m.account, nil
}

func (m *mockAccountServiceForPlaid) ReorderAccounts(ctx context.Context, userID string, req *domain.ReorderAccountsRequest) error {
	return m.reorderErr
}

func (m *mockAccountServiceForPlaid) DeleteAccount(ctx context.Context, accountID, userID string) error {
	return m.deleteErr
}

func (m *mockAccountServiceForPlaid) DisconnectInstitution(ctx context.Context, linkedItemID, userID string) error {
	return m.disconnectErr
}

func (m *mockAccountServiceForPlaid) SetJobQueue(jq queue.JobQueue) {
	// no-op for tests
}

// Helper function to create test account
func createTestAccountForPlaid() domain.Account {
	officialName := "Premier Checking"
	subtype := "checking"
	currentBalance := 1500.00
	availableBalance := 1400.00
	displayOrder := 0

	return domain.Account{
		ID:               "acc-123",
		UserID:           "user-123",
		PlaidAccountID:   "plaid-acc-123",
		Name:             "Checking Account",
		OfficialName:     &officialName,
		Type:             "depository",
		Subtype:          &subtype,
		CurrentBalance:   &currentBalance,
		AvailableBalance: &availableBalance,
		CurrencyCode:     "USD",
		IsHidden:         false,
		DisplayOrder:     &displayOrder,
	}
}

// TestCreateLinkToken_Success tests successful link token creation.
func TestCreateLinkToken_Success(t *testing.T) {
	mockPlaid := &mockPlaidServiceForPlaid{
		linkToken: &domain.LinkTokenResponse{
			LinkToken:  "link-sandbox-abc123",
			Expiration: time.Now().Add(30 * time.Minute),
			RequestID:  "req-123",
		},
	}
	mockAccount := &mockAccountServiceForPlaid{}

	handler := NewPlaidHandler(mockPlaid, mockAccount)

	input := &CreateLinkTokenInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}

	output, err := handler.CreateLinkToken(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, "link-sandbox-abc123", output.Body.LinkToken)
}

// TestCreateLinkToken_Error tests error handling for link token creation.
func TestCreateLinkToken_Error(t *testing.T) {
	mockPlaid := &mockPlaidServiceForPlaid{
		createErr: assert.AnError,
	}
	mockAccount := &mockAccountServiceForPlaid{}

	handler := NewPlaidHandler(mockPlaid, mockAccount)

	input := &CreateLinkTokenInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}

	output, err := handler.CreateLinkToken(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
}

// TestExchangePublicToken_Success tests successful public token exchange.
func TestExchangePublicToken_Success(t *testing.T) {
	mockPlaid := &mockPlaidServiceForPlaid{}
	mockAccount := &mockAccountServiceForPlaid{
		linkResult: &services.LinkAccountsResult{
			LinkedItem: &domain.LinkedItem{
				ID:     "item-123",
				ItemID: "plaid-item-123",
			},
			Accounts: []domain.Account{createTestAccountForPlaid()},
		},
	}

	handler := NewPlaidHandler(mockPlaid, mockAccount)

	input := &ExchangePublicTokenInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}
	input.Body.PublicToken = "public-sandbox-abc123"
	input.Body.InstitutionID = "ins_123"
	input.Body.InstitutionName = "Chase"

	output, err := handler.ExchangePublicToken(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.True(t, output.Body.Success)
	assert.Equal(t, "plaid-item-123", output.Body.ItemID)
	assert.Equal(t, 1, output.Body.Accounts)
}

// TestExchangePublicToken_AccountLimitExceeded tests account limit error.
func TestExchangePublicToken_AccountLimitExceeded(t *testing.T) {
	mockPlaid := &mockPlaidServiceForPlaid{}
	mockAccount := &mockAccountServiceForPlaid{
		linkErr: services.ErrAccountLimitExceeded,
	}

	handler := NewPlaidHandler(mockPlaid, mockAccount)

	input := &ExchangePublicTokenInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}
	input.Body.PublicToken = "public-sandbox-abc123"
	input.Body.InstitutionID = "ins_123"
	input.Body.InstitutionName = "Chase"

	output, err := handler.ExchangePublicToken(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Account limit exceeded")
}

// TestExchangePublicToken_InvalidPublicToken tests invalid token error.
func TestExchangePublicToken_InvalidPublicToken(t *testing.T) {
	mockPlaid := &mockPlaidServiceForPlaid{}
	mockAccount := &mockAccountServiceForPlaid{
		linkErr: services.ErrInvalidPublicToken,
	}

	handler := NewPlaidHandler(mockPlaid, mockAccount)

	input := &ExchangePublicTokenInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}
	input.Body.PublicToken = "invalid-token"
	input.Body.InstitutionID = "ins_123"
	input.Body.InstitutionName = "Chase"

	output, err := handler.ExchangePublicToken(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Invalid or expired public token")
}

// TestExchangePublicToken_PlaidAPIFailure tests Plaid API error.
func TestExchangePublicToken_PlaidAPIFailure(t *testing.T) {
	mockPlaid := &mockPlaidServiceForPlaid{}
	mockAccount := &mockAccountServiceForPlaid{
		linkErr: services.ErrPlaidAPIFailure,
	}

	handler := NewPlaidHandler(mockPlaid, mockAccount)

	input := &ExchangePublicTokenInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}
	input.Body.PublicToken = "public-sandbox-abc123"
	input.Body.InstitutionID = "ins_123"
	input.Body.InstitutionName = "Chase"

	output, err := handler.ExchangePublicToken(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Bank service temporarily unavailable")
}

// TestGetAccounts_Success tests successful account retrieval.
func TestGetAccounts_Success(t *testing.T) {
	mockPlaid := &mockPlaidServiceForPlaid{}
	mockAccount := &mockAccountServiceForPlaid{
		accounts: []domain.Account{createTestAccountForPlaid()},
	}

	handler := NewPlaidHandler(mockPlaid, mockAccount)

	input := &GetAccountsInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}

	output, err := handler.GetAccounts(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Len(t, output.Body.Accounts, 1)
	assert.Equal(t, 1, output.Body.TotalCount)
}

// TestGetAccounts_Empty tests empty accounts list.
func TestGetAccounts_Empty(t *testing.T) {
	mockPlaid := &mockPlaidServiceForPlaid{}
	mockAccount := &mockAccountServiceForPlaid{
		accounts: []domain.Account{},
	}

	handler := NewPlaidHandler(mockPlaid, mockAccount)

	input := &GetAccountsInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}

	output, err := handler.GetAccounts(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Empty(t, output.Body.Accounts)
	assert.Equal(t, 0, output.Body.TotalCount)
}

// TestGetAccounts_IncludeHidden tests include hidden filter.
func TestGetAccounts_IncludeHidden(t *testing.T) {
	mockPlaid := &mockPlaidServiceForPlaid{}
	mockAccount := &mockAccountServiceForPlaid{
		accounts: []domain.Account{createTestAccountForPlaid()},
	}

	handler := NewPlaidHandler(mockPlaid, mockAccount)

	input := &GetAccountsInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		IncludeHidden:      "true",
	}

	output, err := handler.GetAccounts(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
}

// TestUpdateAccount_Success tests successful account update.
func TestUpdatePlaidAccount_Success(t *testing.T) {
	mockPlaid := &mockPlaidServiceForPlaid{}
	mockAccount := &mockAccountServiceForPlaid{
		account: &domain.Account{
			ID:       "acc-123",
			UserID:   "user-123",
			Name:     "Updated Account Name",
			IsHidden: true,
		},
	}

	handler := NewPlaidHandler(mockPlaid, mockAccount)

	input := &UpdateAccountInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		ID:                 "acc-123",
	}
	newName := "Updated Account Name"
	isHidden := true
	input.Body = domain.UpdateAccountRequest{
		Nickname: &newName,
		IsHidden: &isHidden,
	}

	output, err := handler.UpdateAccount(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, "Updated Account Name", output.Body.Account.Name)
}

// TestUpdatePlaidAccount_NotFound tests error when account not found.
func TestUpdatePlaidAccount_NotFound(t *testing.T) {
	mockPlaid := &mockPlaidServiceForPlaid{}
	mockAccount := &mockAccountServiceForPlaid{
		updateErr: repository.ErrAccountNotFound,
	}

	handler := NewPlaidHandler(mockPlaid, mockAccount)

	input := &UpdateAccountInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		ID:                 "nonexistent",
	}

	output, err := handler.UpdateAccount(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
}

// TestDeletePlaidAccount_Success tests successful account deletion.
func TestDeletePlaidAccount_Success(t *testing.T) {
	mockPlaid := &mockPlaidServiceForPlaid{}
	mockAccount := &mockAccountServiceForPlaid{}

	handler := NewPlaidHandler(mockPlaid, mockAccount)

	input := &DeleteAccountInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		ID:                 "acc-123",
	}

	_, err := handler.DeleteAccount(context.Background(), input)

	require.NoError(t, err)
}

// TestDeletePlaidAccount_NotFound tests error when account not found for deletion.
func TestDeletePlaidAccount_NotFound(t *testing.T) {
	mockPlaid := &mockPlaidServiceForPlaid{}
	mockAccount := &mockAccountServiceForPlaid{
		deleteErr: repository.ErrAccountNotFound,
	}

	handler := NewPlaidHandler(mockPlaid, mockAccount)

	input := &DeleteAccountInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		ID:                 "nonexistent",
	}

	_, err := handler.DeleteAccount(context.Background(), input)

	require.Error(t, err)
}

// TestDisconnectInstitution_Success tests successful institution disconnection.
func TestDisconnectInstitution_Success(t *testing.T) {
	mockPlaid := &mockPlaidServiceForPlaid{}
	mockAccount := &mockAccountServiceForPlaid{}

	handler := NewPlaidHandler(mockPlaid, mockAccount)

	input := &DisconnectInstitutionInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		ID:                 "item-123",
	}

	_, err := handler.DisconnectInstitution(context.Background(), input)

	require.NoError(t, err)
}

// TestDisconnectInstitution_Error tests error handling for institution disconnection.
func TestDisconnectInstitution_Error(t *testing.T) {
	mockPlaid := &mockPlaidServiceForPlaid{}
	mockAccount := &mockAccountServiceForPlaid{
		disconnectErr: assert.AnError,
	}

	handler := NewPlaidHandler(mockPlaid, mockAccount)

	input := &DisconnectInstitutionInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		ID:                 "item-123",
	}

	_, err := handler.DisconnectInstitution(context.Background(), input)

	require.Error(t, err)
}

// TestReorderAccounts_Success tests successful account reordering.
func TestReorderAccounts_Success(t *testing.T) {
	mockPlaid := &mockPlaidServiceForPlaid{}
	mockAccount := &mockAccountServiceForPlaid{}

	handler := NewPlaidHandler(mockPlaid, mockAccount)

	input := &ReorderAccountsInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}
	input.Body = domain.ReorderAccountsRequest{
		AccountOrders: []domain.AccountOrder{
			{AccountID: "acc-1", DisplayOrder: 0},
			{AccountID: "acc-2", DisplayOrder: 1},
		},
	}

	output, err := handler.ReorderAccounts(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.True(t, output.Body.Success)
}

// TestReorderAccounts_Error tests error handling for account reordering.
func TestReorderAccounts_Error(t *testing.T) {
	mockPlaid := &mockPlaidServiceForPlaid{}
	mockAccount := &mockAccountServiceForPlaid{
		reorderErr: assert.AnError,
	}

	handler := NewPlaidHandler(mockPlaid, mockAccount)

	input := &ReorderAccountsInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}
	input.Body = domain.ReorderAccountsRequest{
		AccountOrders: []domain.AccountOrder{
			{AccountID: "acc-1", DisplayOrder: 0},
		},
	}

	output, err := handler.ReorderAccounts(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
}

// TestPlaidRouteRegistration tests that routes are properly registered.
func TestPlaidRouteRegistration(t *testing.T) {
	mockPlaid := &mockPlaidServiceForPlaid{
		linkToken: &domain.LinkTokenResponse{
			LinkToken:  "link-sandbox-abc123",
			Expiration: time.Now().Add(30 * time.Minute),
			RequestID:  "req-123",
		},
	}
	mockAccount := &mockAccountServiceForPlaid{
		accounts: []domain.Account{createTestAccountForPlaid()},
	}

	_, api := humatest.New(t)
	handler := NewPlaidHandler(mockPlaid, mockAccount)
	RegisterPlaidRoutes(api, handler)

	// Test GET /api/v1/accounts (without auth, should fail with 401)
	resp := api.Get("/api/v1/accounts")
	assert.Equal(t, http.StatusUnauthorized, resp.Code)

	// Verify the response contains expected error message
	body := resp.Body.String()
	assert.Contains(t, body, "User not authenticated")
}

// TestPlaidResponseJSON tests JSON serialization of response.
func TestPlaidResponseJSON(t *testing.T) {
	mockPlaid := &mockPlaidServiceForPlaid{}
	mockAccount := &mockAccountServiceForPlaid{}

	_, api := humatest.New(t)
	handler := NewPlaidHandler(mockPlaid, mockAccount)
	RegisterPlaidRoutes(api, handler)

	resp := api.Get("/api/v1/accounts")
	assert.Equal(t, http.StatusUnauthorized, resp.Code)

	// Verify error response is valid JSON
	var errorResp map[string]interface{}
	err := json.Unmarshal(resp.Body.Bytes(), &errorResp)
	require.NoError(t, err, "Error response should be valid JSON")
}
