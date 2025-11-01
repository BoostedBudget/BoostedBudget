// +build integration

package integration

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/handlers"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
	"github.com/maxnussbaum/BoostedBudget/internal/workers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// PHASE 9: Integration Tests for Item Error Handler + Notification Flow
// Issue #257: Item Error Handler - Process ITEM Webhooks and User Notifications
// ============================================================================

// TestItemErrorNotificationFlow_LoginRequired tests the full flow from webhook to notification
// Flow: Webhook received → ItemErrorService processes → Notification created → User retrieves via API
func TestItemErrorNotificationFlow_LoginRequired(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Setup: Create test database and repositories
	db := setupTestDB(t)
	defer db.Close()

	ctx := context.Background()
	userID := "test-user-123"
	itemID := "test-item-456"

	// Initialize repositories
	webhookRepo := repository.NewWebhookRepository(db)
	linkedItemRepo := repository.NewLinkedItemRepository(db)
	notificationRepo := repository.NewNotificationRepository(db)

	// Create test linked item
	linkedItem := &domain.LinkedItem{
		ID:                 "linked-item-1",
		UserID:             userID,
		ItemID:             itemID,
		AccessToken:        "encrypted-access-token",
		InstitutionID:      "ins_123",
		InstitutionName:    "Chase Bank",
		Status:             "active",
		ConsentExpiresAt:   nil,
		LastSuccessfulSync: nil,
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
	}
	err := linkedItemRepo.Create(ctx, linkedItem)
	require.NoError(t, err)

	// Create webhook event
	webhookPayload := map[string]interface{}{
		"webhook_type": "ITEM",
		"webhook_code": "ITEM_LOGIN_REQUIRED",
		"item_id":      itemID,
		"error": map[string]string{
			"error_type":    "ITEM_LOGIN_REQUIRED",
			"error_code":    "ITEM_LOGIN_REQUIRED",
			"error_message": "User credentials changed - please reconnect",
		},
	}
	payloadBytes, _ := json.Marshal(webhookPayload)

	webhook := &domain.WebhookEvent{
		ID:          "webhook-test-1",
		WebhookType: "ITEM",
		ItemID:      &itemID,
		Payload:     payloadBytes,
		Processed:   false,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	err = webhookRepo.Create(ctx, webhook)
	require.NoError(t, err)

	// Initialize services
	mockPlaidService := &mockPlaidServiceForIntegration{}
	mockEncryptionService := &mockEncryptionServiceForIntegration{}
	mockEmailService := services.NewEmailServiceMock("", "", "http://localhost:3000")

	itemErrorService := services.NewItemErrorService(
		linkedItemRepo,
		notificationRepo,
		mockPlaidService,
		mockEncryptionService,
		mockEmailService,
	)

	// Step 1: Process webhook via ItemErrorService
	err = itemErrorService.ProcessWebhook(ctx, itemID, "ITEM_LOGIN_REQUIRED", "ITEM_LOGIN_REQUIRED", "User credentials changed - please reconnect")
	require.NoError(t, err)

	// Step 2: Verify notification was created
	notifications, err := notificationRepo.GetByUserID(ctx, userID, 10)
	require.NoError(t, err)
	require.Len(t, notifications, 1)

	notification := notifications[0]
	assert.Equal(t, userID, notification.UserID)
	assert.Equal(t, "login_required", notification.Type)
	assert.Contains(t, notification.Title, "Reconnect")
	assert.Contains(t, notification.Message, "Chase Bank")
	assert.False(t, notification.Read)
	assert.False(t, notification.SentEmail)

	// Step 3: Verify linked item status was updated
	updatedItem, err := linkedItemRepo.GetByItemID(ctx, itemID)
	require.NoError(t, err)
	assert.Equal(t, "login_required", updatedItem.Status)
	assert.NotNil(t, updatedItem.LastErrorCode)
	assert.Equal(t, "ITEM_LOGIN_REQUIRED", *updatedItem.LastErrorCode)

	// Step 4: Test NotificationHandler API - Get notifications
	notificationHandler := handlers.NewNotificationHandler(notificationRepo)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/notifications", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", userID)

	err = notificationHandler.GetNotifications(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var apiNotifications []domain.Notification
	err = json.Unmarshal(rec.Body.Bytes(), &apiNotifications)
	require.NoError(t, err)
	require.Len(t, apiNotifications, 1)
	assert.Equal(t, notification.ID, apiNotifications[0].ID)

	// Step 5: Mark notification as read via API
	req2 := httptest.NewRequest(http.MethodPatch, "/api/v1/notifications/"+notification.ID+"/read", nil)
	rec2 := httptest.NewRecorder()
	c2 := e.NewContext(req2, rec2)
	c2.Set("user_id", userID)
	c2.SetParamNames("id")
	c2.SetParamValues(notification.ID)

	err = notificationHandler.MarkNotificationAsRead(c2)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec2.Code)

	// Step 6: Verify notification was marked as read
	updatedNotifications, err := notificationRepo.GetByUserID(ctx, userID, 10)
	require.NoError(t, err)
	require.Len(t, updatedNotifications, 1)
	assert.True(t, updatedNotifications[0].Read)

	// Step 7: Verify unread count is 0
	count, err := notificationRepo.GetUnreadCount(ctx, userID)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

// TestWebhookProcessor_Integration tests the background worker processing ITEM webhooks
func TestWebhookProcessor_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Setup: Create test database and repositories
	db := setupTestDB(t)
	defer db.Close()

	ctx := context.Background()
	userID := "test-user-789"
	itemID := "test-item-999"

	// Initialize repositories
	webhookRepo := repository.NewWebhookRepository(db)
	linkedItemRepo := repository.NewLinkedItemRepository(db)
	notificationRepo := repository.NewNotificationRepository(db)
	transactionRepo := repository.NewTransactionRepository(db)
	accountRepo := repository.NewAccountRepository(db)

	// Create test linked item
	linkedItem := &domain.LinkedItem{
		ID:                 "linked-item-2",
		UserID:             userID,
		ItemID:             itemID,
		AccessToken:        "encrypted-access-token",
		InstitutionID:      "ins_456",
		InstitutionName:    "Bank of America",
		Status:             "active",
		ConsentExpiresAt:   nil,
		LastSuccessfulSync: nil,
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
	}
	err := linkedItemRepo.Create(ctx, linkedItem)
	require.NoError(t, err)

	// Create ITEM webhook
	webhookPayload := map[string]interface{}{
		"webhook_type": "ITEM",
		"webhook_code": "ERROR",
		"item_id":      itemID,
		"error": map[string]string{
			"error_type":    "ITEM_ERROR",
			"error_code":    "INSTITUTION_DOWN",
			"error_message": "The financial institution is temporarily unavailable",
		},
	}
	payloadBytes, _ := json.Marshal(webhookPayload)

	webhook := &domain.WebhookEvent{
		ID:          "webhook-test-2",
		WebhookType: "ITEM",
		ItemID:      &itemID,
		Payload:     payloadBytes,
		Processed:   false,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	err = webhookRepo.Create(ctx, webhook)
	require.NoError(t, err)

	// Initialize services
	mockPlaidService := &mockPlaidServiceForIntegration{}
	mockEncryptionService := &mockEncryptionServiceForIntegration{}
	mockEmailService := services.NewEmailServiceMock("", "", "http://localhost:3000")

	transactionSyncService := services.NewTransactionSyncService(
		webhookRepo,
		linkedItemRepo,
		transactionRepo,
		accountRepo,
		mockPlaidService,
		mockEncryptionService,
	)

	itemErrorService := services.NewItemErrorService(
		linkedItemRepo,
		notificationRepo,
		mockPlaidService,
		mockEncryptionService,
		mockEmailService,
	)

	// Create webhook processor
	processor := workers.NewWebhookProcessor(
		transactionSyncService,
		itemErrorService,
		webhookRepo,
		30*time.Second,
		10,
	)

	// Process webhooks
	err = processor.ProcessPendingWebhooks(ctx)
	require.NoError(t, err)

	// Verify notification was created
	notifications, err := notificationRepo.GetByUserID(ctx, userID, 10)
	require.NoError(t, err)
	require.Len(t, notifications, 1)

	notification := notifications[0]
	assert.Equal(t, userID, notification.UserID)
	assert.Equal(t, "error", notification.Type)
	assert.Contains(t, notification.Title, "Connection Issue")
	assert.Contains(t, notification.Message, "Bank of America")

	// Verify linked item status was updated
	updatedItem, err := linkedItemRepo.GetByItemID(ctx, itemID)
	require.NoError(t, err)
	assert.Equal(t, "error", updatedItem.Status)
}

// ============================================================================
// Test Helpers and Mocks
// ============================================================================

func setupTestDB(t *testing.T) *sql.DB {
	// TODO: Setup real test database connection
	// For now, skip integration tests if database not available
	t.Skip("Integration tests require database setup")
	return nil
}

// mockPlaidServiceForIntegration provides a mock Plaid service for integration tests
type mockPlaidServiceForIntegration struct{}

func (m *mockPlaidServiceForIntegration) CreateLinkToken(ctx context.Context, userID string, products []string) (*domain.LinkTokenResponse, error) {
	return &domain.LinkTokenResponse{
		LinkToken:  "test-link-token",
		Expiration: time.Now().Add(4 * time.Hour),
	}, nil
}

func (m *mockPlaidServiceForIntegration) CreateUpdateModeLinkToken(ctx context.Context, userID string, accessToken string) (*domain.LinkTokenResponse, error) {
	return &domain.LinkTokenResponse{
		LinkToken:  "test-update-link-token",
		Expiration: time.Now().Add(4 * time.Hour),
	}, nil
}

func (m *mockPlaidServiceForIntegration) ExchangePublicToken(ctx context.Context, publicToken string) (*domain.TokenExchangeResponse, error) {
	return nil, nil
}

func (m *mockPlaidServiceForIntegration) GetAccounts(ctx context.Context, accessToken string) ([]domain.Account, error) {
	return nil, nil
}

func (m *mockPlaidServiceForIntegration) GetInstitution(ctx context.Context, institutionID string) (*domain.Institution, error) {
	return nil, nil
}

func (m *mockPlaidServiceForIntegration) RemoveItem(ctx context.Context, accessToken string) error {
	return nil
}

func (m *mockPlaidServiceForIntegration) GetClient() interface{} {
	return nil
}

// mockEncryptionServiceForIntegration provides a mock encryption service
type mockEncryptionServiceForIntegration struct{}

func (m *mockEncryptionServiceForIntegration) Encrypt(plaintext string) (string, error) {
	return "encrypted:" + plaintext, nil
}

func (m *mockEncryptionServiceForIntegration) Decrypt(ciphertext string) (string, error) {
	return "decrypted:" + ciphertext, nil
}
