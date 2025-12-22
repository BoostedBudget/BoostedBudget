package operations

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	plaid "github.com/plaid/plaid-go/v23/plaid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockPlaidServiceForAdmin is a mock implementation of services.PlaidService for admin tests
type mockPlaidServiceForAdmin struct {
	fireWebhookResp *plaid.SandboxItemFireWebhookResponse
	fireWebhookErr  error
}

func (m *mockPlaidServiceForAdmin) GetClient() *plaid.APIClient {
	return nil
}

func (m *mockPlaidServiceForAdmin) CreateLinkToken(ctx context.Context, userID string) (*domain.LinkTokenResponse, error) {
	return nil, nil
}

func (m *mockPlaidServiceForAdmin) CreateUpdateModeLinkToken(ctx context.Context, userID, accessToken string) (*domain.LinkTokenResponse, error) {
	return nil, nil
}

func (m *mockPlaidServiceForAdmin) ExchangePublicToken(ctx context.Context, publicToken string) (*plaid.ItemPublicTokenExchangeResponse, error) {
	return nil, nil
}

func (m *mockPlaidServiceForAdmin) GetAccounts(ctx context.Context, accessToken string) ([]plaid.AccountBase, error) {
	return nil, nil
}

func (m *mockPlaidServiceForAdmin) GetInstitution(ctx context.Context, institutionID string) (*plaid.Institution, error) {
	return nil, nil
}

func (m *mockPlaidServiceForAdmin) RemoveItem(ctx context.Context, accessToken string) error {
	return nil
}

func (m *mockPlaidServiceForAdmin) TransactionsSync(ctx context.Context, accessToken string, cursor string, daysRequested int) (*plaid.TransactionsSyncResponse, error) {
	return nil, nil
}

func (m *mockPlaidServiceForAdmin) FireWebhook(ctx context.Context, accessToken string, webhookCode string) (*plaid.SandboxItemFireWebhookResponse, error) {
	if m.fireWebhookErr != nil {
		return nil, m.fireWebhookErr
	}
	return m.fireWebhookResp, nil
}

// mockLinkedItemRepo is a mock implementation of repository.LinkedItemRepository
type mockLinkedItemRepo struct {
	item    *domain.LinkedItem
	getErr  error
	byItems map[string]*domain.LinkedItem
}

func (m *mockLinkedItemRepo) Create(ctx context.Context, item *domain.LinkedItem) error {
	return nil
}

func (m *mockLinkedItemRepo) GetByID(ctx context.Context, id string) (*domain.LinkedItem, error) {
	return m.item, m.getErr
}

func (m *mockLinkedItemRepo) GetByIDAndUser(ctx context.Context, id, userID string) (*domain.LinkedItem, error) {
	return m.item, m.getErr
}

func (m *mockLinkedItemRepo) GetByUserID(ctx context.Context, userID string) ([]domain.LinkedItem, error) {
	return nil, nil
}

func (m *mockLinkedItemRepo) GetByItemID(ctx context.Context, itemID string) (*domain.LinkedItem, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	if m.byItems != nil {
		if item, ok := m.byItems[itemID]; ok {
			return item, nil
		}
		return nil, repository.ErrLinkedItemNotFound
	}
	return m.item, nil
}

func (m *mockLinkedItemRepo) UpdateCursor(ctx context.Context, id, cursor string) error {
	return nil
}

func (m *mockLinkedItemRepo) Update(ctx context.Context, item *domain.LinkedItem) error {
	return nil
}

func (m *mockLinkedItemRepo) Delete(ctx context.Context, id string) error {
	return nil
}

func (m *mockLinkedItemRepo) DeleteByUser(ctx context.Context, id, userID string) error {
	return nil
}

// mockEncryptionService is a mock implementation of services.EncryptionService
type mockEncryptionService struct {
	decryptedToken string
	decryptErr     error
}

func (m *mockEncryptionService) Encrypt(plaintext string) (string, error) {
	return plaintext, nil
}

func (m *mockEncryptionService) Decrypt(ciphertext string) (string, error) {
	if m.decryptErr != nil {
		return "", m.decryptErr
	}
	return m.decryptedToken, nil
}

// Helper function to create test linked item
func createTestLinkedItem() *domain.LinkedItem {
	return &domain.LinkedItem{
		ID:                   "linked-item-123",
		UserID:               "user-123",
		ItemID:               "plaid-item-123",
		AccessTokenEncrypted: "encrypted-token",
		InstitutionID:        "ins_123",
		InstitutionName:      "Test Bank",
	}
}

// TestFireWebhook_Success tests successful webhook firing.
func TestFireWebhook_Success(t *testing.T) {
	mockPlaid := &mockPlaidServiceForAdmin{
		fireWebhookResp: &plaid.SandboxItemFireWebhookResponse{
			WebhookFired: true,
			RequestId:    "req-123",
		},
	}
	mockRepo := &mockLinkedItemRepo{
		item: createTestLinkedItem(),
	}
	mockEncrypt := &mockEncryptionService{
		decryptedToken: "decrypted-access-token",
	}

	handler := NewAdminHandler(mockPlaid, mockRepo, mockEncrypt)

	input := &FireWebhookInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}
	input.Body = FireWebhookRequestBody{
		ItemID:      "plaid-item-123",
		WebhookCode: "DEFAULT_UPDATE",
	}

	output, err := handler.FireWebhook(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.True(t, output.Body.WebhookFired)
	assert.Equal(t, "req-123", output.Body.RequestID)
	assert.Equal(t, "plaid-item-123", output.Body.ItemID)
	assert.Equal(t, "DEFAULT_UPDATE", output.Body.WebhookCode)
}

// TestFireWebhook_ItemNotFound tests 404 when linked item not found.
func TestFireWebhook_ItemNotFound(t *testing.T) {
	mockPlaid := &mockPlaidServiceForAdmin{}
	mockRepo := &mockLinkedItemRepo{
		getErr: repository.ErrLinkedItemNotFound,
	}
	mockEncrypt := &mockEncryptionService{}

	handler := NewAdminHandler(mockPlaid, mockRepo, mockEncrypt)

	input := &FireWebhookInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}
	input.Body = FireWebhookRequestBody{
		ItemID:      "nonexistent-item",
		WebhookCode: "DEFAULT_UPDATE",
	}

	output, err := handler.FireWebhook(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Item not found")
}

// TestFireWebhook_DecryptionError tests 500 when decryption fails.
func TestFireWebhook_DecryptionError(t *testing.T) {
	mockPlaid := &mockPlaidServiceForAdmin{}
	mockRepo := &mockLinkedItemRepo{
		item: createTestLinkedItem(),
	}
	mockEncrypt := &mockEncryptionService{
		decryptErr: errors.New("decryption failed"),
	}

	handler := NewAdminHandler(mockPlaid, mockRepo, mockEncrypt)

	input := &FireWebhookInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}
	input.Body = FireWebhookRequestBody{
		ItemID:      "plaid-item-123",
		WebhookCode: "DEFAULT_UPDATE",
	}

	output, err := handler.FireWebhook(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decrypt access token")
}

// TestFireWebhook_PlaidAPIError tests 500 when Plaid API fails.
func TestFireWebhook_PlaidAPIError(t *testing.T) {
	mockPlaid := &mockPlaidServiceForAdmin{
		fireWebhookErr: errors.New("plaid api error"),
	}
	mockRepo := &mockLinkedItemRepo{
		item: createTestLinkedItem(),
	}
	mockEncrypt := &mockEncryptionService{
		decryptedToken: "decrypted-access-token",
	}

	handler := NewAdminHandler(mockPlaid, mockRepo, mockEncrypt)

	input := &FireWebhookInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}
	input.Body = FireWebhookRequestBody{
		ItemID:      "plaid-item-123",
		WebhookCode: "DEFAULT_UPDATE",
	}

	output, err := handler.FireWebhook(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "fire webhook")
}

// TestAdminRouteRegistration tests that routes are properly registered.
func TestAdminRouteRegistration(t *testing.T) {
	mockPlaid := &mockPlaidServiceForAdmin{}
	mockRepo := &mockLinkedItemRepo{}
	mockEncrypt := &mockEncryptionService{}

	_, api := humatest.New(t)
	handler := NewAdminHandler(mockPlaid, mockRepo, mockEncrypt)
	RegisterAdminRoutes(api, handler)

	// Test POST /api/v1/admin/plaid/fire-webhook (without auth, should fail with 401)
	resp := api.Post("/api/v1/admin/plaid/fire-webhook", "application/json", map[string]string{
		"item_id":      "test-item-id",
		"webhook_code": "DEFAULT_UPDATE",
	})
	assert.Equal(t, http.StatusUnauthorized, resp.Code)
}
