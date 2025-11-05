package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/middleware"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/plaid/plaid-go/v23/plaid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Mock implementations for testing

type mockAdminPlaidService struct {
	FireWebhookFunc func(ctx context.Context, accessToken string, webhookCode string) (*plaid.SandboxItemFireWebhookResponse, error)
}

func (m *mockAdminPlaidService) GetClient() *plaid.APIClient {
	return nil
}

func (m *mockAdminPlaidService) CreateLinkToken(ctx context.Context, userID string) (*domain.LinkTokenResponse, error) {
	return nil, nil
}

func (m *mockAdminPlaidService) CreateUpdateModeLinkToken(ctx context.Context, userID string, accessToken string) (*domain.LinkTokenResponse, error) {
	return nil, nil
}

func (m *mockAdminPlaidService) RemoveItem(ctx context.Context, accessToken string) error {
	return nil
}

func (m *mockAdminPlaidService) TransactionsSync(ctx context.Context, accessToken string, cursor string, daysRequested int) (*plaid.TransactionsSyncResponse, error) {
	return nil, nil
}

func (m *mockAdminPlaidService) FireWebhook(ctx context.Context, accessToken string, webhookCode string) (*plaid.SandboxItemFireWebhookResponse, error) {
	if m.FireWebhookFunc != nil {
		return m.FireWebhookFunc(ctx, accessToken, webhookCode)
	}
	return nil, errors.New("FireWebhookFunc not implemented")
}

type mockLinkedItemRepository struct {
	GetByIDFunc func(ctx context.Context, id string) (*domain.LinkedItem, error)
}

func (m *mockLinkedItemRepository) Create(ctx context.Context, item *domain.LinkedItem) error {
	return nil
}

func (m *mockLinkedItemRepository) GetByID(ctx context.Context, id string) (*domain.LinkedItem, error) {
	if m.GetByIDFunc != nil {
		return m.GetByIDFunc(ctx, id)
	}
	return nil, repository.ErrLinkedItemNotFound
}

func (m *mockLinkedItemRepository) GetByUserID(ctx context.Context, userID string) ([]domain.LinkedItem, error) {
	return nil, nil
}

func (m *mockLinkedItemRepository) GetByItemID(ctx context.Context, itemID string) (*domain.LinkedItem, error) {
	return nil, nil
}

func (m *mockLinkedItemRepository) UpdateCursor(ctx context.Context, id, cursor string) error {
	return nil
}

func (m *mockLinkedItemRepository) Update(ctx context.Context, item *domain.LinkedItem) error {
	return nil
}

func (m *mockLinkedItemRepository) Delete(ctx context.Context, id string) error {
	return nil
}

func (m *mockLinkedItemRepository) GetByIDAndUser(ctx context.Context, id, userID string) (*domain.LinkedItem, error) {
	return nil, nil
}

func (m *mockLinkedItemRepository) DeleteByUser(ctx context.Context, id, userID string) error {
	return nil
}

type mockEncryptionService struct {
	DecryptFunc func(ciphertext string) (string, error)
}

func (m *mockEncryptionService) Encrypt(plaintext string) (string, error) {
	return plaintext + "_encrypted", nil
}

func (m *mockEncryptionService) Decrypt(ciphertext string) (string, error) {
	if m.DecryptFunc != nil {
		return m.DecryptFunc(ciphertext)
	}
	return "decrypted_access_token", nil
}

// TestFireWebhook_Success tests successful webhook firing with valid request
func TestFireWebhook_Success(t *testing.T) {
	// Arrange
	e := echo.New()
	e.Validator = middleware.NewValidator()
	requestBody := map[string]string{
		"item_id":      "550e8400-e29b-41d4-a716-446655440000",
		"webhook_code": "DEFAULT_UPDATE",
	}
	bodyBytes, _ := json.Marshal(requestBody)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/plaid/fire-webhook", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Mock dependencies - all succeed
	linkedItem := &domain.LinkedItem{
		ID:                   "550e8400-e29b-41d4-a716-446655440000",
		UserID:               "auth0|user123",
		ItemID:               "plaid-item-id",
		AccessTokenEncrypted: "encrypted_token",
		InstitutionName:      "Chase Bank",
		Provider:             "plaid",
		Status:               "active",
		CreatedAt:            time.Now(),
		UpdatedAt:            time.Now(),
	}

	mockRepo := &mockLinkedItemRepository{
		GetByIDFunc: func(ctx context.Context, id string) (*domain.LinkedItem, error) {
			return linkedItem, nil
		},
	}

	webhookID := "wh_test_123"
	mockPlaid := &mockAdminPlaidService{
		FireWebhookFunc: func(ctx context.Context, accessToken string, webhookCode string) (*plaid.SandboxItemFireWebhookResponse, error) {
			assert.Equal(t, "decrypted_access_token", accessToken)
			assert.Equal(t, "DEFAULT_UPDATE", webhookCode)
			response := plaid.NewSandboxItemFireWebhookResponse(true, webhookID)
			return response, nil
		},
	}

	mockEncryption := &mockEncryptionService{
		DecryptFunc: func(ciphertext string) (string, error) {
			return "decrypted_access_token", nil
		},
	}

	handler := &AdminHandler{
		plaidService:      mockPlaid,
		linkedItemRepo:    mockRepo,
		encryptionService: mockEncryption,
	}

	// Act
	err := handler.FireWebhook(c)

	// Assert
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var response map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	require.NoError(t, err)

	assert.Equal(t, true, response["webhook_fired"])
	assert.Equal(t, webhookID, response["request_id"])
	assert.Equal(t, "550e8400-e29b-41d4-a716-446655440000", response["item_id"])
	assert.Equal(t, "DEFAULT_UPDATE", response["webhook_code"])
}

// TestFireWebhook_InvalidJSON tests error handling for invalid JSON body
func TestFireWebhook_InvalidJSON(t *testing.T) {
	// Arrange
	e := echo.New()
	e.Validator = middleware.NewValidator()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/plaid/fire-webhook", bytes.NewReader([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	handler := &AdminHandler{
		plaidService:      &mockAdminPlaidService{},
		linkedItemRepo:    &mockLinkedItemRepository{},
		encryptionService: &mockEncryptionService{},
	}

	// Act
	err := handler.FireWebhook(c)

	// Assert
	assert.Error(t, err)
	httpErr, ok := err.(*echo.HTTPError)
	require.True(t, ok)
	assert.Equal(t, http.StatusBadRequest, httpErr.Code)
}

// TestFireWebhook_MissingItemID tests validation error for missing item_id
func TestFireWebhook_MissingItemID(t *testing.T) {
	// Arrange
	e := echo.New()
	requestBody := map[string]string{
		"webhook_code": "DEFAULT_UPDATE",
		// item_id is missing!
	}
	bodyBytes, _ := json.Marshal(requestBody)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/plaid/fire-webhook", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Add validator to context (Echo requires this for validation)
	e.Validator = middleware.NewValidator()

	handler := &AdminHandler{
		plaidService:      &mockAdminPlaidService{},
		linkedItemRepo:    &mockLinkedItemRepository{},
		encryptionService: &mockEncryptionService{},
	}

	// Act
	err := handler.FireWebhook(c)

	// Assert
	assert.Error(t, err)
	httpErr, ok := err.(*echo.HTTPError)
	require.True(t, ok)
	assert.Equal(t, http.StatusBadRequest, httpErr.Code)
}

// TestFireWebhook_MissingWebhookCode tests validation error for missing webhook_code
func TestFireWebhook_MissingWebhookCode(t *testing.T) {
	// Arrange
	e := echo.New()
	requestBody := map[string]string{
		"item_id": "550e8400-e29b-41d4-a716-446655440000",
		// webhook_code is missing!
	}
	bodyBytes, _ := json.Marshal(requestBody)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/plaid/fire-webhook", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Add validator to context
	e.Validator = middleware.NewValidator()

	handler := &AdminHandler{
		plaidService:      &mockAdminPlaidService{},
		linkedItemRepo:    &mockLinkedItemRepository{},
		encryptionService: &mockEncryptionService{},
	}

	// Act
	err := handler.FireWebhook(c)

	// Assert
	assert.Error(t, err)
	httpErr, ok := err.(*echo.HTTPError)
	require.True(t, ok)
	assert.Equal(t, http.StatusBadRequest, httpErr.Code)
}

// TestFireWebhook_InvalidWebhookCode tests validation error for invalid webhook_code
func TestFireWebhook_InvalidWebhookCode(t *testing.T) {
	// Arrange
	e := echo.New()
	requestBody := map[string]string{
		"item_id":      "550e8400-e29b-41d4-a716-446655440000",
		"webhook_code": "INVALID_CODE", // Not one of the allowed codes
	}
	bodyBytes, _ := json.Marshal(requestBody)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/plaid/fire-webhook", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Add validator to context
	e.Validator = middleware.NewValidator()

	handler := &AdminHandler{
		plaidService:      &mockAdminPlaidService{},
		linkedItemRepo:    &mockLinkedItemRepository{},
		encryptionService: &mockEncryptionService{},
	}

	// Act
	err := handler.FireWebhook(c)

	// Assert
	assert.Error(t, err)
	httpErr, ok := err.(*echo.HTTPError)
	require.True(t, ok)
	assert.Equal(t, http.StatusBadRequest, httpErr.Code)
}

// TestFireWebhook_ItemNotFound tests error handling when linked item doesn't exist
func TestFireWebhook_ItemNotFound(t *testing.T) {
	// Arrange
	e := echo.New()
	e.Validator = middleware.NewValidator()
	requestBody := map[string]string{
		"item_id":      "550e8400-e29b-41d4-a716-446655440000",
		"webhook_code": "DEFAULT_UPDATE",
	}
	bodyBytes, _ := json.Marshal(requestBody)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/plaid/fire-webhook", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Mock repository to return not found error
	mockRepo := &mockLinkedItemRepository{
		GetByIDFunc: func(ctx context.Context, id string) (*domain.LinkedItem, error) {
			return nil, repository.ErrLinkedItemNotFound
		},
	}

	handler := &AdminHandler{
		plaidService:      &mockAdminPlaidService{},
		linkedItemRepo:    mockRepo,
		encryptionService: &mockEncryptionService{},
	}

	// Act
	err := handler.FireWebhook(c)

	// Assert
	assert.Error(t, err)
	httpErr, ok := err.(*echo.HTTPError)
	require.True(t, ok)
	assert.Equal(t, http.StatusNotFound, httpErr.Code)
	assert.Contains(t, httpErr.Message.(string), "Item not found")
}

// TestFireWebhook_DecryptionFails tests error handling when access token decryption fails
func TestFireWebhook_DecryptionFails(t *testing.T) {
	// Arrange
	e := echo.New()
	e.Validator = middleware.NewValidator()
	requestBody := map[string]string{
		"item_id":      "550e8400-e29b-41d4-a716-446655440000",
		"webhook_code": "DEFAULT_UPDATE",
	}
	bodyBytes, _ := json.Marshal(requestBody)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/plaid/fire-webhook", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Mock dependencies
	linkedItem := &domain.LinkedItem{
		ID:                   "550e8400-e29b-41d4-a716-446655440000",
		UserID:               "auth0|user123",
		AccessTokenEncrypted: "corrupted_token",
		InstitutionName:      "Chase Bank",
		Provider:             "plaid",
		Status:               "active",
		CreatedAt:            time.Now(),
		UpdatedAt:            time.Now(),
	}

	mockRepo := &mockLinkedItemRepository{
		GetByIDFunc: func(ctx context.Context, id string) (*domain.LinkedItem, error) {
			return linkedItem, nil
		},
	}

	// Mock encryption service to return error
	mockEncryption := &mockEncryptionService{
		DecryptFunc: func(ciphertext string) (string, error) {
			return "", errors.New("decryption failed: invalid ciphertext")
		},
	}

	handler := &AdminHandler{
		plaidService:      &mockAdminPlaidService{},
		linkedItemRepo:    mockRepo,
		encryptionService: mockEncryption,
	}

	// Act
	err := handler.FireWebhook(c)

	// Assert
	assert.Error(t, err)
	httpErr, ok := err.(*echo.HTTPError)
	require.True(t, ok)
	assert.Equal(t, http.StatusInternalServerError, httpErr.Code)
	assert.Contains(t, httpErr.Message.(string), "Failed to decrypt access token")
}

// TestFireWebhook_PlaidServiceError tests error handling when Plaid API call fails
func TestFireWebhook_PlaidServiceError(t *testing.T) {
	// Arrange
	e := echo.New()
	e.Validator = middleware.NewValidator()
	requestBody := map[string]string{
		"item_id":      "550e8400-e29b-41d4-a716-446655440000",
		"webhook_code": "DEFAULT_UPDATE",
	}
	bodyBytes, _ := json.Marshal(requestBody)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/plaid/fire-webhook", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Mock dependencies
	linkedItem := &domain.LinkedItem{
		ID:                   "550e8400-e29b-41d4-a716-446655440000",
		UserID:               "auth0|user123",
		AccessTokenEncrypted: "encrypted_token",
		InstitutionName:      "Chase Bank",
		Provider:             "plaid",
		Status:               "active",
		CreatedAt:            time.Now(),
		UpdatedAt:            time.Now(),
	}

	mockRepo := &mockLinkedItemRepository{
		GetByIDFunc: func(ctx context.Context, id string) (*domain.LinkedItem, error) {
			return linkedItem, nil
		},
	}

	// Mock Plaid service to return error
	mockPlaid := &mockAdminPlaidService{
		FireWebhookFunc: func(ctx context.Context, accessToken string, webhookCode string) (*plaid.SandboxItemFireWebhookResponse, error) {
			return nil, errors.New("plaid API error: INVALID_ACCESS_TOKEN")
		},
	}

	mockEncryption := &mockEncryptionService{
		DecryptFunc: func(ciphertext string) (string, error) {
			return "decrypted_access_token", nil
		},
	}

	handler := &AdminHandler{
		plaidService:      mockPlaid,
		linkedItemRepo:    mockRepo,
		encryptionService: mockEncryption,
	}

	// Act
	err := handler.FireWebhook(c)

	// Assert
	assert.Error(t, err)
	httpErr, ok := err.(*echo.HTTPError)
	require.True(t, ok)
	assert.Equal(t, http.StatusInternalServerError, httpErr.Code)
	assert.Contains(t, httpErr.Message.(string), "Failed to fire webhook")
}
