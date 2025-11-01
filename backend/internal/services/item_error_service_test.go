package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// TestHandleLoginRequired_Success tests successful LOGIN_REQUIRED webhook processing
// Issue #257: Item Error Handler - Process ITEM Webhooks and User Notifications
func TestHandleLoginRequired_Success(t *testing.T) {
	// Given: Mocked dependencies
	ctx := context.Background()
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	mockNotificationRepo := new(MockNotificationRepositoryForItemError)
	mockPlaidService := new(MockPlaidService)
	mockEncryptionService := new(MockEncryptionService)
	mockEmailService := new(MockEmailService)

	service := NewItemErrorService(
		mockLinkedItemRepo,
		mockNotificationRepo,
		mockPlaidService,
		mockEncryptionService,
		mockEmailService,
	)

	// Existing LinkedItem that needs re-authentication
	linkedItem := &domain.LinkedItem{
		ID:                   "linked-item-123",
		UserID:               "user-456",
		ItemID:               "plaid-item-789",
		AccessTokenEncrypted: "encrypted-access-token", // Encrypted
		InstitutionID:        "ins_1",
		Status:               "active", // Currently active, will become "login_required"
	}

	// Mock: Decrypt access token
	mockEncryptionService.On("Decrypt", "encrypted-access-token").
		Return("decrypted-access-token", nil)

	// Mock: Create update mode link token for re-authentication
	linkTokenResponse := &domain.LinkTokenResponse{
		LinkToken:  "link-token-update-123",
		Expiration: time.Now().Add(30 * time.Minute),
		RequestID:  "req-123",
	}
	mockPlaidService.On("CreateUpdateModeLinkToken", ctx, "user-456", "decrypted-access-token").
		Return(linkTokenResponse, nil)

	// Mock: Update LinkedItem status to "login_required"
	mockLinkedItemRepo.On("Update", ctx, mock.MatchedBy(func(item *domain.LinkedItem) bool {
		return item.ID == "linked-item-123" &&
			item.Status == "login_required" &&
			*item.ErrorCode == "ITEM_LOGIN_REQUIRED" &&
			*item.ErrorMessage == "Credentials changed, please re-authenticate" &&
			item.LastErrorAt != nil
	})).Return(nil)

	// Mock: Create notification with action URL
	mockNotificationRepo.On("Create", ctx, mock.MatchedBy(func(n *domain.Notification) bool {
		return n.UserID == "user-456" &&
			n.Type == "login_required" &&
			n.Title == "Reconnect Your Bank" &&
			n.ActionURL != nil &&
			n.ActionLabel != nil
	})).Return(nil)

	// Mock: Send email notification (stub, non-blocking)
	mockEmailService.On("SendNotificationEmail", ctx, mock.Anything, mock.Anything).
		Return(nil)

	// When: HandleLoginRequired is called
	err := service.HandleLoginRequired(ctx, linkedItem, "ITEM_LOGIN_REQUIRED", "Credentials changed, please re-authenticate")

	// Then: All operations succeed
	require.NoError(t, err)
	mockEncryptionService.AssertExpectations(t)
	mockPlaidService.AssertExpectations(t)
	mockLinkedItemRepo.AssertExpectations(t)
	mockNotificationRepo.AssertExpectations(t)
	mockEmailService.AssertExpectations(t)
}

// TestHandleLoginRequired_DecryptionFailure tests handling of access token decryption failure
func TestHandleLoginRequired_DecryptionFailure(t *testing.T) {
	ctx := context.Background()
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	mockNotificationRepo := new(MockNotificationRepositoryForItemError)
	mockPlaidService := new(MockPlaidService)
	mockEncryptionService := new(MockEncryptionService)
	mockEmailService := new(MockEmailService)

	service := NewItemErrorService(
		mockLinkedItemRepo,
		mockNotificationRepo,
		mockPlaidService,
		mockEncryptionService,
		mockEmailService,
	)

	linkedItem := &domain.LinkedItem{
		ID:                   "linked-item-123",
		UserID:               "user-456",
		ItemID:               "plaid-item-789",
		AccessTokenEncrypted: "encrypted-access-token",
		Status:               "active",
	}

	// Mock: Decryption fails
	mockEncryptionService.On("Decrypt", "encrypted-access-token").
		Return("", errors.New("decryption failed"))

	// When: HandleLoginRequired is called
	err := service.HandleLoginRequired(ctx, linkedItem, "ITEM_LOGIN_REQUIRED", "Test error")

	// Then: Returns error (critical failure)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decrypt")
	mockEncryptionService.AssertExpectations(t)
	// Other services should NOT be called
	mockPlaidService.AssertNotCalled(t, "CreateUpdateModeLinkToken")
	mockLinkedItemRepo.AssertNotCalled(t, "Update")
}

// TestHandleLoginRequired_LinkTokenCreationFailure tests handling of link token creation failure
func TestHandleLoginRequired_LinkTokenCreationFailure(t *testing.T) {
	ctx := context.Background()
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	mockNotificationRepo := new(MockNotificationRepositoryForItemError)
	mockPlaidService := new(MockPlaidService)
	mockEncryptionService := new(MockEncryptionService)
	mockEmailService := new(MockEmailService)

	service := NewItemErrorService(
		mockLinkedItemRepo,
		mockNotificationRepo,
		mockPlaidService,
		mockEncryptionService,
		mockEmailService,
	)

	linkedItem := &domain.LinkedItem{
		ID:                   "linked-item-123",
		UserID:               "user-456",
		ItemID:               "plaid-item-789",
		AccessTokenEncrypted: "encrypted-access-token",
		Status:               "active",
	}

	// Mock: Decrypt succeeds
	mockEncryptionService.On("Decrypt", "encrypted-access-token").
		Return("decrypted-access-token", nil)

	// Mock: Link token creation fails (e.g., Plaid API error)
	mockPlaidService.On("CreateUpdateModeLinkToken", ctx, "user-456", "decrypted-access-token").
		Return(nil, errors.New("plaid api error"))

	// When: HandleLoginRequired is called
	err := service.HandleLoginRequired(ctx, linkedItem, "ITEM_LOGIN_REQUIRED", "Test error")

	// Then: Returns error (critical failure)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "link token")
	mockEncryptionService.AssertExpectations(t)
	mockPlaidService.AssertExpectations(t)
	// DB and notification services should NOT be called
	mockLinkedItemRepo.AssertNotCalled(t, "Update")
	mockNotificationRepo.AssertNotCalled(t, "Create")
}

// TestHandleLoginRequired_DatabaseUpdateFailure tests handling of LinkedItem update failure
func TestHandleLoginRequired_DatabaseUpdateFailure(t *testing.T) {
	ctx := context.Background()
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	mockNotificationRepo := new(MockNotificationRepositoryForItemError)
	mockPlaidService := new(MockPlaidService)
	mockEncryptionService := new(MockEncryptionService)
	mockEmailService := new(MockEmailService)

	service := NewItemErrorService(
		mockLinkedItemRepo,
		mockNotificationRepo,
		mockPlaidService,
		mockEncryptionService,
		mockEmailService,
	)

	linkedItem := &domain.LinkedItem{
		ID:                   "linked-item-123",
		UserID:               "user-456",
		ItemID:               "plaid-item-789",
		AccessTokenEncrypted: "encrypted-access-token",
		Status:               "active",
	}

	// Mock: Decrypt succeeds
	mockEncryptionService.On("Decrypt", "encrypted-access-token").
		Return("decrypted-access-token", nil)

	// Mock: Link token creation succeeds
	linkTokenResponse := &domain.LinkTokenResponse{
		LinkToken:  "link-token-update-123",
		Expiration: time.Now().Add(30 * time.Minute),
		RequestID:  "req-123",
	}
	mockPlaidService.On("CreateUpdateModeLinkToken", ctx, "user-456", "decrypted-access-token").
		Return(linkTokenResponse, nil)

	// Mock: Database update fails
	mockLinkedItemRepo.On("Update", ctx, mock.Anything).
		Return(errors.New("database error"))

	// When: HandleLoginRequired is called
	err := service.HandleLoginRequired(ctx, linkedItem, "ITEM_LOGIN_REQUIRED", "Test error")

	// Then: Returns error (critical failure)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "update")
	mockEncryptionService.AssertExpectations(t)
	mockPlaidService.AssertExpectations(t)
	mockLinkedItemRepo.AssertExpectations(t)
	// Notification should NOT be created if DB update fails
	mockNotificationRepo.AssertNotCalled(t, "Create")
}

// TestHandleLoginRequired_NotificationFailureNonBlocking tests graceful degradation for notification failures
func TestHandleLoginRequired_NotificationFailureNonBlocking(t *testing.T) {
	ctx := context.Background()
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	mockNotificationRepo := new(MockNotificationRepositoryForItemError)
	mockPlaidService := new(MockPlaidService)
	mockEncryptionService := new(MockEncryptionService)
	mockEmailService := new(MockEmailService)

	service := NewItemErrorService(
		mockLinkedItemRepo,
		mockNotificationRepo,
		mockPlaidService,
		mockEncryptionService,
		mockEmailService,
	)

	linkedItem := &domain.LinkedItem{
		ID:                   "linked-item-123",
		UserID:               "user-456",
		ItemID:               "plaid-item-789",
		AccessTokenEncrypted: "encrypted-access-token",
		Status:               "active",
	}

	// Mock: Decrypt succeeds
	mockEncryptionService.On("Decrypt", "encrypted-access-token").
		Return("decrypted-access-token", nil)

	// Mock: Link token creation succeeds
	linkTokenResponse := &domain.LinkTokenResponse{
		LinkToken:  "link-token-update-123",
		Expiration: time.Now().Add(30 * time.Minute),
		RequestID:  "req-123",
	}
	mockPlaidService.On("CreateUpdateModeLinkToken", ctx, "user-456", "decrypted-access-token").
		Return(linkTokenResponse, nil)

	// Mock: Database update succeeds
	mockLinkedItemRepo.On("Update", ctx, mock.Anything).Return(nil)

	// Mock: Notification creation FAILS (graceful degradation)
	mockNotificationRepo.On("Create", ctx, mock.Anything).
		Return(errors.New("notification database error"))

	// Mock: Email sending should still be attempted but is NOT called due to notification creation failure
	// (Implementation choice: only send email if notification is created)

	// When: HandleLoginRequired is called
	err := service.HandleLoginRequired(ctx, linkedItem, "ITEM_LOGIN_REQUIRED", "Test error")

	// Then: Should succeed despite notification failure (graceful degradation)
	// Critical operations (decrypt, link token, DB update) succeeded
	require.NoError(t, err, "Notification failure should not block webhook processing")
	mockEncryptionService.AssertExpectations(t)
	mockPlaidService.AssertExpectations(t)
	mockLinkedItemRepo.AssertExpectations(t)
	mockNotificationRepo.AssertExpectations(t)
}

// TestHandleLoginRequired_EmailFailureNonBlocking tests graceful degradation for email failures
func TestHandleLoginRequired_EmailFailureNonBlocking(t *testing.T) {
	ctx := context.Background()
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	mockNotificationRepo := new(MockNotificationRepositoryForItemError)
	mockPlaidService := new(MockPlaidService)
	mockEncryptionService := new(MockEncryptionService)
	mockEmailService := new(MockEmailService)

	service := NewItemErrorService(
		mockLinkedItemRepo,
		mockNotificationRepo,
		mockPlaidService,
		mockEncryptionService,
		mockEmailService,
	)

	linkedItem := &domain.LinkedItem{
		ID:                   "linked-item-123",
		UserID:               "user-456",
		ItemID:               "plaid-item-789",
		AccessTokenEncrypted: "encrypted-access-token",
		Status:               "active",
	}

	// Mock: Decrypt succeeds
	mockEncryptionService.On("Decrypt", "encrypted-access-token").
		Return("decrypted-access-token", nil)

	// Mock: Link token creation succeeds
	linkTokenResponse := &domain.LinkTokenResponse{
		LinkToken:  "link-token-update-123",
		Expiration: time.Now().Add(30 * time.Minute),
		RequestID:  "req-123",
	}
	mockPlaidService.On("CreateUpdateModeLinkToken", ctx, "user-456", "decrypted-access-token").
		Return(linkTokenResponse, nil)

	// Mock: Database update succeeds
	mockLinkedItemRepo.On("Update", ctx, mock.Anything).Return(nil)

	// Mock: Notification creation succeeds
	mockNotificationRepo.On("Create", ctx, mock.Anything).Return(nil)

	// Mock: Email sending FAILS (graceful degradation)
	mockEmailService.On("SendNotificationEmail", ctx, mock.Anything, mock.Anything).
		Return(errors.New("email service error"))

	// When: HandleLoginRequired is called
	err := service.HandleLoginRequired(ctx, linkedItem, "ITEM_LOGIN_REQUIRED", "Test error")

	// Then: Should succeed despite email failure (graceful degradation)
	require.NoError(t, err, "Email failure should not block webhook processing")
	mockEncryptionService.AssertExpectations(t)
	mockPlaidService.AssertExpectations(t)
	mockLinkedItemRepo.AssertExpectations(t)
	mockNotificationRepo.AssertExpectations(t)
	mockEmailService.AssertExpectations(t)
}

// Mock NotificationRepository for ItemErrorService tests
type MockNotificationRepositoryForItemError struct {
	mock.Mock
}

func (m *MockNotificationRepositoryForItemError) Create(ctx context.Context, notification *domain.Notification) error {
	args := m.Called(ctx, notification)
	return args.Error(0)
}

func (m *MockNotificationRepositoryForItemError) Update(ctx context.Context, notification *domain.Notification) error {
	args := m.Called(ctx, notification)
	return args.Error(0)
}

func (m *MockNotificationRepositoryForItemError) GetByUserID(ctx context.Context, userID string, limit int) ([]domain.Notification, error) {
	args := m.Called(ctx, userID, limit)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.Notification), args.Error(1)
}

func (m *MockNotificationRepositoryForItemError) MarkAsRead(ctx context.Context, notificationID string, userID string) error {
	args := m.Called(ctx, notificationID, userID)
	return args.Error(0)
}

func (m *MockNotificationRepositoryForItemError) GetUnreadCount(ctx context.Context, userID string) (int, error) {
	args := m.Called(ctx, userID)
	return args.Int(0), args.Error(1)
}

func (m *MockNotificationRepositoryForItemError) GetUnsentEmails(ctx context.Context, limit int) ([]domain.Notification, error) {
	args := m.Called(ctx, limit)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.Notification), args.Error(1)
}
