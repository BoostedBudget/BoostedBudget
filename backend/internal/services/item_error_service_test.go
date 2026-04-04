package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
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

	mockEmailService := new(MockEmailService)
	mockUserRepo := new(MockUserRepository)

	service := NewItemErrorService(
		mockLinkedItemRepo,
		mockNotificationRepo,
		mockUserRepo,
		mockPlaidService,
		mockEmailService,
		nil,
	)

	// Existing LinkedItem that needs re-authentication
	linkedItem := &domain.LinkedItem{
		ID:                   "linked-item-123",
		UserID:               "user-456",
		ItemID:               "plaid-item-789",
		AccessToken: "decrypted-access-token", // Decrypted by repo layer
		InstitutionID:        "ins_1",
		Status:               "active", // Currently active, will become "login_required"
	}



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

	// Mock: Fetch user email
	mockUserRepo.On("GetUserByID", ctx, "user-456").
		Return(&domain.User{ID: "user-456", Email: "real@user.com"}, nil)

	// Mock: Send email notification with real user email
	mockEmailService.On("SendNotificationEmail", ctx, "real@user.com", mock.Anything).
		Return(nil)

	// When: HandleLoginRequired is called
	err := service.HandleLoginRequired(ctx, linkedItem, "ITEM_LOGIN_REQUIRED", "Credentials changed, please re-authenticate")

	// Then: All operations succeed
	require.NoError(t, err)

	mockPlaidService.AssertExpectations(t)
	mockLinkedItemRepo.AssertExpectations(t)
	mockNotificationRepo.AssertExpectations(t)
	mockUserRepo.AssertExpectations(t)
	mockEmailService.AssertExpectations(t)
}

// TestHandleLoginRequired_LinkTokenCreationFailure tests handling of link token creation failure
func TestHandleLoginRequired_LinkTokenCreationFailure(t *testing.T) {
	ctx := context.Background()
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	mockNotificationRepo := new(MockNotificationRepositoryForItemError)
	mockPlaidService := new(MockPlaidService)

	mockEmailService := new(MockEmailService)
	mockUserRepo := new(MockUserRepository)

	service := NewItemErrorService(
		mockLinkedItemRepo,
		mockNotificationRepo,
		mockUserRepo,
		mockPlaidService,
		mockEmailService,
		nil,
	)

	linkedItem := &domain.LinkedItem{
		ID:                   "linked-item-123",
		UserID:               "user-456",
		ItemID:               "plaid-item-789",
		AccessToken: "decrypted-access-token",
		Status:               "active",
	}



	// Mock: Link token creation fails (e.g., Plaid API error)
	mockPlaidService.On("CreateUpdateModeLinkToken", ctx, "user-456", "decrypted-access-token").
		Return(nil, errors.New("plaid api error"))

	// When: HandleLoginRequired is called
	err := service.HandleLoginRequired(ctx, linkedItem, "ITEM_LOGIN_REQUIRED", "Test error")

	// Then: Returns error (critical failure)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "link token")

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

	mockEmailService := new(MockEmailService)
	mockUserRepo := new(MockUserRepository)

	service := NewItemErrorService(
		mockLinkedItemRepo,
		mockNotificationRepo,
		mockUserRepo,
		mockPlaidService,
		mockEmailService,
		nil,
	)

	linkedItem := &domain.LinkedItem{
		ID:                   "linked-item-123",
		UserID:               "user-456",
		ItemID:               "plaid-item-789",
		AccessToken: "decrypted-access-token",
		Status:               "active",
	}



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

	mockEmailService := new(MockEmailService)
	mockUserRepo := new(MockUserRepository)

	service := NewItemErrorService(
		mockLinkedItemRepo,
		mockNotificationRepo,
		mockUserRepo,
		mockPlaidService,
		mockEmailService,
		nil,
	)

	linkedItem := &domain.LinkedItem{
		ID:                   "linked-item-123",
		UserID:               "user-456",
		ItemID:               "plaid-item-789",
		AccessToken: "decrypted-access-token",
		Status:               "active",
	}



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

	mockEmailService := new(MockEmailService)
	mockUserRepo := new(MockUserRepository)

	service := NewItemErrorService(
		mockLinkedItemRepo,
		mockNotificationRepo,
		mockUserRepo,
		mockPlaidService,
		mockEmailService,
		nil,
	)

	linkedItem := &domain.LinkedItem{
		ID:                   "linked-item-123",
		UserID:               "user-456",
		ItemID:               "plaid-item-789",
		AccessToken: "decrypted-access-token",
		Status:               "active",
	}



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

	// Mock: Fetch user email
	mockUserRepo.On("GetUserByID", ctx, "user-456").
		Return(&domain.User{ID: "user-456", Email: "real@user.com"}, nil)

	// Mock: Email sending FAILS (graceful degradation)
	mockEmailService.On("SendNotificationEmail", ctx, mock.Anything, mock.Anything).
		Return(errors.New("email service error"))

	// When: HandleLoginRequired is called
	err := service.HandleLoginRequired(ctx, linkedItem, "ITEM_LOGIN_REQUIRED", "Test error")

	// Then: Should succeed despite email failure (graceful degradation)
	require.NoError(t, err, "Email failure should not block webhook processing")

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

// ============================================================================
// HandleError Tests
// ============================================================================

// TestHandleError_Success tests successful ERROR webhook processing
func TestHandleError_Success(t *testing.T) {
	// Given: Mocked dependencies
	ctx := context.Background()
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	mockNotificationRepo := new(MockNotificationRepositoryForItemError)
	mockPlaidService := new(MockPlaidService)

	mockEmailService := new(MockEmailService)
	mockUserRepo := new(MockUserRepository)

	service := NewItemErrorService(
		mockLinkedItemRepo,
		mockNotificationRepo,
		mockUserRepo,
		mockPlaidService,
		mockEmailService,
		nil,
	)

	// Existing LinkedItem with a general error
	linkedItem := &domain.LinkedItem{
		ID:                   "linked-item-123",
		UserID:               "user-456",
		ItemID:               "plaid-item-789",
		AccessToken: "decrypted-access-token",
		InstitutionID:        "ins_1",
		InstitutionName:      "Test Bank",
		Status:               "active", // Currently active, will become "error"
	}

	// Mock: Update LinkedItem status to "error"
	mockLinkedItemRepo.On("Update", ctx, mock.MatchedBy(func(item *domain.LinkedItem) bool {
		return item.ID == "linked-item-123" &&
			item.Status == "error" &&
			*item.ErrorCode == "ITEM_ERROR" &&
			*item.ErrorMessage == "Connection error, please try again later" &&
			item.LastErrorAt != nil
	})).Return(nil)

	// Mock: Create notification (no action URL for general errors)
	mockNotificationRepo.On("Create", ctx, mock.MatchedBy(func(n *domain.Notification) bool {
		return n.UserID == "user-456" &&
			n.Type == "error" &&
			n.Title == "Connection Error" &&
			n.ActionURL == nil && // No action URL for general errors
			n.ActionLabel == nil
	})).Return(nil)

	// Mock: Fetch user email
	mockUserRepo.On("GetUserByID", ctx, "user-456").
		Return(&domain.User{ID: "user-456", Email: "real@user.com"}, nil)

	// Mock: Send email notification with real user email
	mockEmailService.On("SendNotificationEmail", ctx, "real@user.com", mock.Anything).
		Return(nil)

	// When: HandleError is called
	err := service.HandleError(ctx, linkedItem, "ITEM_ERROR", "Connection error, please try again later")

	// Then: All operations succeed
	require.NoError(t, err)
	mockLinkedItemRepo.AssertExpectations(t)
	mockNotificationRepo.AssertExpectations(t)
	mockUserRepo.AssertExpectations(t)
	mockEmailService.AssertExpectations(t)
	// PlaidService and EncryptionService should NOT be called (no link token needed)
	mockPlaidService.AssertNotCalled(t, "CreateUpdateModeLinkToken")

}

// TestHandleError_DatabaseUpdateFailure tests handling of LinkedItem update failure
func TestHandleError_DatabaseUpdateFailure(t *testing.T) {
	ctx := context.Background()
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	mockNotificationRepo := new(MockNotificationRepositoryForItemError)
	mockPlaidService := new(MockPlaidService)

	mockEmailService := new(MockEmailService)
	mockUserRepo := new(MockUserRepository)

	service := NewItemErrorService(
		mockLinkedItemRepo,
		mockNotificationRepo,
		mockUserRepo,
		mockPlaidService,
		mockEmailService,
		nil,
	)

	linkedItem := &domain.LinkedItem{
		ID:              "linked-item-123",
		UserID:          "user-456",
		ItemID:          "plaid-item-789",
		InstitutionName: "Test Bank",
		Status:          "active",
	}

	// Mock: Database update fails
	mockLinkedItemRepo.On("Update", ctx, mock.Anything).
		Return(errors.New("database error"))

	// When: HandleError is called
	err := service.HandleError(ctx, linkedItem, "ITEM_ERROR", "Test error")

	// Then: Returns error (critical failure)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "update")
	mockLinkedItemRepo.AssertExpectations(t)
	// Notification should NOT be created if DB update fails
	mockNotificationRepo.AssertNotCalled(t, "Create")
}

// TestHandleError_NotificationFailureNonBlocking tests graceful degradation for notification failures
func TestHandleError_NotificationFailureNonBlocking(t *testing.T) {
	ctx := context.Background()
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	mockNotificationRepo := new(MockNotificationRepositoryForItemError)
	mockPlaidService := new(MockPlaidService)

	mockEmailService := new(MockEmailService)
	mockUserRepo := new(MockUserRepository)

	service := NewItemErrorService(
		mockLinkedItemRepo,
		mockNotificationRepo,
		mockUserRepo,
		mockPlaidService,
		mockEmailService,
		nil,
	)

	linkedItem := &domain.LinkedItem{
		ID:              "linked-item-123",
		UserID:          "user-456",
		ItemID:          "plaid-item-789",
		InstitutionName: "Test Bank",
		Status:          "active",
	}

	// Mock: Database update succeeds
	mockLinkedItemRepo.On("Update", ctx, mock.Anything).Return(nil)

	// Mock: Notification creation FAILS (graceful degradation)
	mockNotificationRepo.On("Create", ctx, mock.Anything).
		Return(errors.New("notification database error"))

	// When: HandleError is called
	err := service.HandleError(ctx, linkedItem, "ITEM_ERROR", "Test error")

	// Then: Should succeed despite notification failure (graceful degradation)
	require.NoError(t, err, "Notification failure should not block webhook processing")
	mockLinkedItemRepo.AssertExpectations(t)
	mockNotificationRepo.AssertExpectations(t)
}

// TestHandleError_EmailFailureNonBlocking tests graceful degradation for email failures
func TestHandleError_EmailFailureNonBlocking(t *testing.T) {
	ctx := context.Background()
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	mockNotificationRepo := new(MockNotificationRepositoryForItemError)
	mockPlaidService := new(MockPlaidService)

	mockEmailService := new(MockEmailService)
	mockUserRepo := new(MockUserRepository)

	service := NewItemErrorService(
		mockLinkedItemRepo,
		mockNotificationRepo,
		mockUserRepo,
		mockPlaidService,
		mockEmailService,
		nil,
	)

	linkedItem := &domain.LinkedItem{
		ID:              "linked-item-123",
		UserID:          "user-456",
		ItemID:          "plaid-item-789",
		InstitutionName: "Test Bank",
		Status:          "active",
	}

	// Mock: Database update succeeds
	mockLinkedItemRepo.On("Update", ctx, mock.Anything).Return(nil)

	// Mock: Notification creation succeeds
	mockNotificationRepo.On("Create", ctx, mock.Anything).Return(nil)

	// Mock: Fetch user email
	mockUserRepo.On("GetUserByID", ctx, "user-456").
		Return(&domain.User{ID: "user-456", Email: "real@user.com"}, nil)

	// Mock: Email sending FAILS (graceful degradation)
	mockEmailService.On("SendNotificationEmail", ctx, mock.Anything, mock.Anything).
		Return(errors.New("email service error"))

	// When: HandleError is called
	err := service.HandleError(ctx, linkedItem, "ITEM_ERROR", "Test error")

	// Then: Should succeed despite email failure (graceful degradation)
	require.NoError(t, err, "Email failure should not block webhook processing")
	mockLinkedItemRepo.AssertExpectations(t)
	mockNotificationRepo.AssertExpectations(t)
	mockEmailService.AssertExpectations(t)
}

// ============================================================================
// HandlePendingExpiration Tests
// ============================================================================

// TestHandlePendingExpiration_Success tests successful PENDING_EXPIRATION webhook processing
func TestHandlePendingExpiration_Success(t *testing.T) {
	// Given: Mocked dependencies
	ctx := context.Background()
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	mockNotificationRepo := new(MockNotificationRepositoryForItemError)
	mockPlaidService := new(MockPlaidService)

	mockEmailService := new(MockEmailService)
	mockUserRepo := new(MockUserRepository)

	service := NewItemErrorService(
		mockLinkedItemRepo,
		mockNotificationRepo,
		mockUserRepo,
		mockPlaidService,
		mockEmailService,
		nil,
	)

	// Existing LinkedItem that is expiring soon
	linkedItem := &domain.LinkedItem{
		ID:                   "linked-item-123",
		UserID:               "user-456",
		ItemID:               "plaid-item-789",
		AccessToken: "decrypted-access-token",
		InstitutionID:        "ins_1",
		InstitutionName:      "Test Bank",
		Status:               "active", // Currently active, will become "pending_expiration"
	}



	// Mock: Create update mode link token for re-authentication
	linkTokenResponse := &domain.LinkTokenResponse{
		LinkToken:  "link-token-update-456",
		Expiration: time.Now().Add(30 * time.Minute),
		RequestID:  "req-456",
	}
	mockPlaidService.On("CreateUpdateModeLinkToken", ctx, "user-456", "decrypted-access-token").
		Return(linkTokenResponse, nil)

	// Mock: Update LinkedItem status to "pending_expiration"
	mockLinkedItemRepo.On("Update", ctx, mock.MatchedBy(func(item *domain.LinkedItem) bool {
		return item.ID == "linked-item-123" &&
			item.Status == "pending_expiration" &&
			*item.ErrorCode == "ITEM_PENDING_EXPIRATION" &&
			*item.ErrorMessage == "Access expiring soon, please re-authenticate" &&
			item.LastErrorAt != nil
	})).Return(nil)

	// Mock: Create notification with action URL
	mockNotificationRepo.On("Create", ctx, mock.MatchedBy(func(n *domain.Notification) bool {
		return n.UserID == "user-456" &&
			n.Type == "pending_expiration" &&
			n.Title == "Bank Connection Expiring Soon" &&
			n.ActionURL != nil &&
			n.ActionLabel != nil
	})).Return(nil)

	// Mock: Fetch user email
	mockUserRepo.On("GetUserByID", ctx, "user-456").
		Return(&domain.User{ID: "user-456", Email: "real@user.com"}, nil)

	// Mock: Send email notification with real user email
	mockEmailService.On("SendNotificationEmail", ctx, "real@user.com", mock.Anything).
		Return(nil)

	// When: HandlePendingExpiration is called
	err := service.HandlePendingExpiration(ctx, linkedItem, "ITEM_PENDING_EXPIRATION", "Access expiring soon, please re-authenticate")

	// Then: All operations succeed
	require.NoError(t, err)

	mockPlaidService.AssertExpectations(t)
	mockLinkedItemRepo.AssertExpectations(t)
	mockNotificationRepo.AssertExpectations(t)
	mockUserRepo.AssertExpectations(t)
	mockEmailService.AssertExpectations(t)
}

// TestHandlePendingExpiration_LinkTokenCreationFailure tests handling of link token creation failure
func TestHandlePendingExpiration_LinkTokenCreationFailure(t *testing.T) {
	ctx := context.Background()
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	mockNotificationRepo := new(MockNotificationRepositoryForItemError)
	mockPlaidService := new(MockPlaidService)

	mockEmailService := new(MockEmailService)
	mockUserRepo := new(MockUserRepository)

	service := NewItemErrorService(
		mockLinkedItemRepo,
		mockNotificationRepo,
		mockUserRepo,
		mockPlaidService,
		mockEmailService,
		nil,
	)

	linkedItem := &domain.LinkedItem{
		ID:                   "linked-item-123",
		UserID:               "user-456",
		ItemID:               "plaid-item-789",
		AccessToken: "decrypted-access-token",
		Status:               "active",
	}



	// Mock: Link token creation fails
	mockPlaidService.On("CreateUpdateModeLinkToken", ctx, "user-456", "decrypted-access-token").
		Return(nil, errors.New("plaid api error"))

	// When: HandlePendingExpiration is called
	err := service.HandlePendingExpiration(ctx, linkedItem, "ITEM_PENDING_EXPIRATION", "Test error")

	// Then: Returns error (critical failure)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "link token")

	mockPlaidService.AssertExpectations(t)
	// DB and notification services should NOT be called
	mockLinkedItemRepo.AssertNotCalled(t, "Update")
	mockNotificationRepo.AssertNotCalled(t, "Create")
}

// TestHandlePendingExpiration_DatabaseUpdateFailure tests handling of LinkedItem update failure
func TestHandlePendingExpiration_DatabaseUpdateFailure(t *testing.T) {
	ctx := context.Background()
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	mockNotificationRepo := new(MockNotificationRepositoryForItemError)
	mockPlaidService := new(MockPlaidService)

	mockEmailService := new(MockEmailService)
	mockUserRepo := new(MockUserRepository)

	service := NewItemErrorService(
		mockLinkedItemRepo,
		mockNotificationRepo,
		mockUserRepo,
		mockPlaidService,
		mockEmailService,
		nil,
	)

	linkedItem := &domain.LinkedItem{
		ID:                   "linked-item-123",
		UserID:               "user-456",
		ItemID:               "plaid-item-789",
		AccessToken: "decrypted-access-token",
		Status:               "active",
	}



	// Mock: Link token creation succeeds
	linkTokenResponse := &domain.LinkTokenResponse{
		LinkToken:  "link-token-update-456",
		Expiration: time.Now().Add(30 * time.Minute),
		RequestID:  "req-456",
	}
	mockPlaidService.On("CreateUpdateModeLinkToken", ctx, "user-456", "decrypted-access-token").
		Return(linkTokenResponse, nil)

	// Mock: Database update fails
	mockLinkedItemRepo.On("Update", ctx, mock.Anything).
		Return(errors.New("database error"))

	// When: HandlePendingExpiration is called
	err := service.HandlePendingExpiration(ctx, linkedItem, "ITEM_PENDING_EXPIRATION", "Test error")

	// Then: Returns error (critical failure)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "update")

	mockPlaidService.AssertExpectations(t)
	mockLinkedItemRepo.AssertExpectations(t)
	// Notification should NOT be created if DB update fails
	mockNotificationRepo.AssertNotCalled(t, "Create")
}

// TestHandlePendingExpiration_NotificationFailureNonBlocking tests graceful degradation for notification failures
func TestHandlePendingExpiration_NotificationFailureNonBlocking(t *testing.T) {
	ctx := context.Background()
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	mockNotificationRepo := new(MockNotificationRepositoryForItemError)
	mockPlaidService := new(MockPlaidService)

	mockEmailService := new(MockEmailService)
	mockUserRepo := new(MockUserRepository)

	service := NewItemErrorService(
		mockLinkedItemRepo,
		mockNotificationRepo,
		mockUserRepo,
		mockPlaidService,
		mockEmailService,
		nil,
	)

	linkedItem := &domain.LinkedItem{
		ID:                   "linked-item-123",
		UserID:               "user-456",
		ItemID:               "plaid-item-789",
		AccessToken: "decrypted-access-token",
		Status:               "active",
	}



	// Mock: Link token creation succeeds
	linkTokenResponse := &domain.LinkTokenResponse{
		LinkToken:  "link-token-update-456",
		Expiration: time.Now().Add(30 * time.Minute),
		RequestID:  "req-456",
	}
	mockPlaidService.On("CreateUpdateModeLinkToken", ctx, "user-456", "decrypted-access-token").
		Return(linkTokenResponse, nil)

	// Mock: Database update succeeds
	mockLinkedItemRepo.On("Update", ctx, mock.Anything).Return(nil)

	// Mock: Notification creation FAILS (graceful degradation)
	mockNotificationRepo.On("Create", ctx, mock.Anything).
		Return(errors.New("notification database error"))

	// When: HandlePendingExpiration is called
	err := service.HandlePendingExpiration(ctx, linkedItem, "ITEM_PENDING_EXPIRATION", "Test error")

	// Then: Should succeed despite notification failure (graceful degradation)
	require.NoError(t, err, "Notification failure should not block webhook processing")

	mockPlaidService.AssertExpectations(t)
	mockLinkedItemRepo.AssertExpectations(t)
	mockNotificationRepo.AssertExpectations(t)
}

// TestHandlePendingExpiration_EmailFailureNonBlocking tests graceful degradation for email failures
func TestHandlePendingExpiration_EmailFailureNonBlocking(t *testing.T) {
	ctx := context.Background()
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	mockNotificationRepo := new(MockNotificationRepositoryForItemError)
	mockPlaidService := new(MockPlaidService)

	mockEmailService := new(MockEmailService)
	mockUserRepo := new(MockUserRepository)

	service := NewItemErrorService(
		mockLinkedItemRepo,
		mockNotificationRepo,
		mockUserRepo,
		mockPlaidService,
		mockEmailService,
		nil,
	)

	linkedItem := &domain.LinkedItem{
		ID:                   "linked-item-123",
		UserID:               "user-456",
		ItemID:               "plaid-item-789",
		AccessToken: "decrypted-access-token",
		Status:               "active",
	}



	// Mock: Link token creation succeeds
	linkTokenResponse := &domain.LinkTokenResponse{
		LinkToken:  "link-token-update-456",
		Expiration: time.Now().Add(30 * time.Minute),
		RequestID:  "req-456",
	}
	mockPlaidService.On("CreateUpdateModeLinkToken", ctx, "user-456", "decrypted-access-token").
		Return(linkTokenResponse, nil)

	// Mock: Database update succeeds
	mockLinkedItemRepo.On("Update", ctx, mock.Anything).Return(nil)

	// Mock: Notification creation succeeds
	mockNotificationRepo.On("Create", ctx, mock.Anything).Return(nil)

	// Mock: Fetch user email
	mockUserRepo.On("GetUserByID", ctx, "user-456").
		Return(&domain.User{ID: "user-456", Email: "real@user.com"}, nil)

	// Mock: Email sending FAILS (graceful degradation)
	mockEmailService.On("SendNotificationEmail", ctx, mock.Anything, mock.Anything).
		Return(errors.New("email service error"))

	// When: HandlePendingExpiration is called
	err := service.HandlePendingExpiration(ctx, linkedItem, "ITEM_PENDING_EXPIRATION", "Test error")

	// Then: Should succeed despite email failure (graceful degradation)
	require.NoError(t, err, "Email failure should not block webhook processing")

	mockPlaidService.AssertExpectations(t)
	mockLinkedItemRepo.AssertExpectations(t)
	mockNotificationRepo.AssertExpectations(t)
	mockEmailService.AssertExpectations(t)
}

// ============================================================================
// HandlePermissionRevoked Tests
// ============================================================================

// TestHandlePermissionRevoked_Success tests successful USER_PERMISSION_REVOKED webhook processing
func TestHandlePermissionRevoked_Success(t *testing.T) {
	// Given: Mocked dependencies
	ctx := context.Background()
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	mockNotificationRepo := new(MockNotificationRepositoryForItemError)
	mockPlaidService := new(MockPlaidService)

	mockEmailService := new(MockEmailService)
	mockUserRepo := new(MockUserRepository)

	service := NewItemErrorService(
		mockLinkedItemRepo,
		mockNotificationRepo,
		mockUserRepo,
		mockPlaidService,
		mockEmailService,
		nil,
	)

	// Existing LinkedItem that has been revoked by the user
	linkedItem := &domain.LinkedItem{
		ID:                   "linked-item-123",
		UserID:               "user-456",
		ItemID:               "plaid-item-789",
		AccessToken: "decrypted-access-token",
		InstitutionID:        "ins_1",
		InstitutionName:      "Test Bank",
		Status:               "active", // Currently active, will become "revoked"
	}



	// Mock: Create update mode link token for re-linking
	linkTokenResponse := &domain.LinkTokenResponse{
		LinkToken:  "link-token-update-789",
		Expiration: time.Now().Add(30 * time.Minute),
		RequestID:  "req-789",
	}
	mockPlaidService.On("CreateUpdateModeLinkToken", ctx, "user-456", "decrypted-access-token").
		Return(linkTokenResponse, nil)

	// Mock: Update LinkedItem status to "revoked"
	mockLinkedItemRepo.On("Update", ctx, mock.MatchedBy(func(item *domain.LinkedItem) bool {
		return item.ID == "linked-item-123" &&
			item.Status == "revoked" &&
			*item.ErrorCode == "ITEM_PERMISSION_REVOKED" &&
			*item.ErrorMessage == "User revoked access at bank" &&
			item.LastErrorAt != nil
	})).Return(nil)

	// Mock: Create notification with action URL
	mockNotificationRepo.On("Create", ctx, mock.MatchedBy(func(n *domain.Notification) bool {
		return n.UserID == "user-456" &&
			n.Type == "permission_revoked" &&
			n.Title == "Bank Access Revoked" &&
			n.ActionURL != nil &&
			n.ActionLabel != nil
	})).Return(nil)

	// Mock: Fetch user email
	mockUserRepo.On("GetUserByID", ctx, "user-456").
		Return(&domain.User{ID: "user-456", Email: "real@user.com"}, nil)

	// Mock: Send email notification with real user email
	mockEmailService.On("SendNotificationEmail", ctx, "real@user.com", mock.Anything).
		Return(nil)

	// When: HandlePermissionRevoked is called
	err := service.HandlePermissionRevoked(ctx, linkedItem, "ITEM_PERMISSION_REVOKED", "User revoked access at bank")

	// Then: All operations succeed
	require.NoError(t, err)

	mockPlaidService.AssertExpectations(t)
	mockLinkedItemRepo.AssertExpectations(t)
	mockNotificationRepo.AssertExpectations(t)
	mockUserRepo.AssertExpectations(t)
	mockEmailService.AssertExpectations(t)
}

// TestHandlePermissionRevoked_LinkTokenCreationFailure tests handling of link token creation failure
func TestHandlePermissionRevoked_LinkTokenCreationFailure(t *testing.T) {
	ctx := context.Background()
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	mockNotificationRepo := new(MockNotificationRepositoryForItemError)
	mockPlaidService := new(MockPlaidService)

	mockEmailService := new(MockEmailService)
	mockUserRepo := new(MockUserRepository)

	service := NewItemErrorService(
		mockLinkedItemRepo,
		mockNotificationRepo,
		mockUserRepo,
		mockPlaidService,
		mockEmailService,
		nil,
	)

	linkedItem := &domain.LinkedItem{
		ID:                   "linked-item-123",
		UserID:               "user-456",
		ItemID:               "plaid-item-789",
		AccessToken: "decrypted-access-token",
		Status:               "active",
	}



	// Mock: Link token creation fails
	mockPlaidService.On("CreateUpdateModeLinkToken", ctx, "user-456", "decrypted-access-token").
		Return(nil, errors.New("plaid api error"))

	// When: HandlePermissionRevoked is called
	err := service.HandlePermissionRevoked(ctx, linkedItem, "ITEM_PERMISSION_REVOKED", "Test error")

	// Then: Returns error (critical failure)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "link token")

	mockPlaidService.AssertExpectations(t)
	// DB and notification services should NOT be called
	mockLinkedItemRepo.AssertNotCalled(t, "Update")
	mockNotificationRepo.AssertNotCalled(t, "Create")
}

// TestHandlePermissionRevoked_DatabaseUpdateFailure tests handling of LinkedItem update failure
func TestHandlePermissionRevoked_DatabaseUpdateFailure(t *testing.T) {
	ctx := context.Background()
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	mockNotificationRepo := new(MockNotificationRepositoryForItemError)
	mockPlaidService := new(MockPlaidService)

	mockEmailService := new(MockEmailService)
	mockUserRepo := new(MockUserRepository)

	service := NewItemErrorService(
		mockLinkedItemRepo,
		mockNotificationRepo,
		mockUserRepo,
		mockPlaidService,
		mockEmailService,
		nil,
	)

	linkedItem := &domain.LinkedItem{
		ID:                   "linked-item-123",
		UserID:               "user-456",
		ItemID:               "plaid-item-789",
		AccessToken: "decrypted-access-token",
		Status:               "active",
	}



	// Mock: Link token creation succeeds
	linkTokenResponse := &domain.LinkTokenResponse{
		LinkToken:  "link-token-update-789",
		Expiration: time.Now().Add(30 * time.Minute),
		RequestID:  "req-789",
	}
	mockPlaidService.On("CreateUpdateModeLinkToken", ctx, "user-456", "decrypted-access-token").
		Return(linkTokenResponse, nil)

	// Mock: Database update fails
	mockLinkedItemRepo.On("Update", ctx, mock.Anything).
		Return(errors.New("database error"))

	// When: HandlePermissionRevoked is called
	err := service.HandlePermissionRevoked(ctx, linkedItem, "ITEM_PERMISSION_REVOKED", "Test error")

	// Then: Returns error (critical failure)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "update")

	mockPlaidService.AssertExpectations(t)
	mockLinkedItemRepo.AssertExpectations(t)
	// Notification should NOT be created if DB update fails
	mockNotificationRepo.AssertNotCalled(t, "Create")
}

// TestHandlePermissionRevoked_NotificationFailureNonBlocking tests graceful degradation for notification failures
func TestHandlePermissionRevoked_NotificationFailureNonBlocking(t *testing.T) {
	ctx := context.Background()
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	mockNotificationRepo := new(MockNotificationRepositoryForItemError)
	mockPlaidService := new(MockPlaidService)

	mockEmailService := new(MockEmailService)
	mockUserRepo := new(MockUserRepository)

	service := NewItemErrorService(
		mockLinkedItemRepo,
		mockNotificationRepo,
		mockUserRepo,
		mockPlaidService,
		mockEmailService,
		nil,
	)

	linkedItem := &domain.LinkedItem{
		ID:                   "linked-item-123",
		UserID:               "user-456",
		ItemID:               "plaid-item-789",
		AccessToken: "decrypted-access-token",
		Status:               "active",
	}



	// Mock: Link token creation succeeds
	linkTokenResponse := &domain.LinkTokenResponse{
		LinkToken:  "link-token-update-789",
		Expiration: time.Now().Add(30 * time.Minute),
		RequestID:  "req-789",
	}
	mockPlaidService.On("CreateUpdateModeLinkToken", ctx, "user-456", "decrypted-access-token").
		Return(linkTokenResponse, nil)

	// Mock: Database update succeeds
	mockLinkedItemRepo.On("Update", ctx, mock.Anything).Return(nil)

	// Mock: Notification creation FAILS (graceful degradation)
	mockNotificationRepo.On("Create", ctx, mock.Anything).
		Return(errors.New("notification database error"))

	// When: HandlePermissionRevoked is called
	err := service.HandlePermissionRevoked(ctx, linkedItem, "ITEM_PERMISSION_REVOKED", "Test error")

	// Then: Should succeed despite notification failure (graceful degradation)
	require.NoError(t, err, "Notification failure should not block webhook processing")

	mockPlaidService.AssertExpectations(t)
	mockLinkedItemRepo.AssertExpectations(t)
	mockNotificationRepo.AssertExpectations(t)
}

// TestHandlePermissionRevoked_EmailFailureNonBlocking tests graceful degradation for email failures
func TestHandlePermissionRevoked_EmailFailureNonBlocking(t *testing.T) {
	ctx := context.Background()
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	mockNotificationRepo := new(MockNotificationRepositoryForItemError)
	mockPlaidService := new(MockPlaidService)

	mockEmailService := new(MockEmailService)
	mockUserRepo := new(MockUserRepository)

	service := NewItemErrorService(
		mockLinkedItemRepo,
		mockNotificationRepo,
		mockUserRepo,
		mockPlaidService,
		mockEmailService,
		nil,
	)

	linkedItem := &domain.LinkedItem{
		ID:                   "linked-item-123",
		UserID:               "user-456",
		ItemID:               "plaid-item-789",
		AccessToken: "decrypted-access-token",
		Status:               "active",
	}



	// Mock: Link token creation succeeds
	linkTokenResponse := &domain.LinkTokenResponse{
		LinkToken:  "link-token-update-789",
		Expiration: time.Now().Add(30 * time.Minute),
		RequestID:  "req-789",
	}
	mockPlaidService.On("CreateUpdateModeLinkToken", ctx, "user-456", "decrypted-access-token").
		Return(linkTokenResponse, nil)

	// Mock: Database update succeeds
	mockLinkedItemRepo.On("Update", ctx, mock.Anything).Return(nil)

	// Mock: Notification creation succeeds
	mockNotificationRepo.On("Create", ctx, mock.Anything).Return(nil)

	// Mock: Fetch user email
	mockUserRepo.On("GetUserByID", ctx, "user-456").
		Return(&domain.User{ID: "user-456", Email: "real@user.com"}, nil)

	// Mock: Email sending FAILS (graceful degradation)
	mockEmailService.On("SendNotificationEmail", ctx, mock.Anything, mock.Anything).
		Return(errors.New("email service error"))

	// When: HandlePermissionRevoked is called
	err := service.HandlePermissionRevoked(ctx, linkedItem, "ITEM_PERMISSION_REVOKED", "Test error")

	// Then: Should succeed despite email failure (graceful degradation)
	require.NoError(t, err, "Email failure should not block webhook processing")

	mockPlaidService.AssertExpectations(t)
	mockLinkedItemRepo.AssertExpectations(t)
	mockNotificationRepo.AssertExpectations(t)
	mockEmailService.AssertExpectations(t)
}

// ============================================================================
// ProcessWebhook Tests
// ============================================================================

// TestProcessWebhook_LoginRequired tests routing to HandleLoginRequired
func TestProcessWebhook_LoginRequired(t *testing.T) {
	ctx := context.Background()
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	mockNotificationRepo := new(MockNotificationRepositoryForItemError)
	mockPlaidService := new(MockPlaidService)

	mockEmailService := new(MockEmailService)
	mockUserRepo := new(MockUserRepository)

	service := NewItemErrorService(
		mockLinkedItemRepo,
		mockNotificationRepo,
		mockUserRepo,
		mockPlaidService,
		mockEmailService,
		nil,
	)

	// Mock: Lookup LinkedItem by itemID
	linkedItem := &domain.LinkedItem{
		ID:                   "linked-item-123",
		UserID:               "user-456",
		ItemID:               "plaid-item-789",
		AccessToken: "decrypted-access-token",
		InstitutionID:        "ins_1",
		InstitutionName:      "Test Bank",
		Status:               "active",
	}
	mockLinkedItemRepo.On("GetByItemID", ctx, "plaid-item-789").Return(linkedItem, nil)

	// Mock: All operations for HandleLoginRequired

	linkTokenResponse := &domain.LinkTokenResponse{
		LinkToken:  "link-token-123",
		Expiration: time.Now().Add(30 * time.Minute),
		RequestID:  "req-123",
	}
	mockPlaidService.On("CreateUpdateModeLinkToken", ctx, "user-456", "decrypted-access-token").Return(linkTokenResponse, nil)
	mockLinkedItemRepo.On("Update", ctx, mock.Anything).Return(nil)
	mockNotificationRepo.On("Create", ctx, mock.Anything).Return(nil)
	mockUserRepo.On("GetUserByID", ctx, "user-456").
		Return(&domain.User{ID: "user-456", Email: "real@user.com"}, nil)
	mockEmailService.On("SendNotificationEmail", ctx, mock.Anything, mock.Anything).Return(nil)

	// When: ProcessWebhook is called with ITEM_LOGIN_REQUIRED
	err := service.ProcessWebhook(ctx, "plaid-item-789", "ITEM_LOGIN_REQUIRED", "ITEM_LOGIN_REQUIRED", "Credentials changed")

	// Then: Routes to HandleLoginRequired and succeeds
	require.NoError(t, err)
	mockLinkedItemRepo.AssertExpectations(t)

	mockPlaidService.AssertExpectations(t)
	mockNotificationRepo.AssertExpectations(t)
	mockEmailService.AssertExpectations(t)
}

// TestProcessWebhook_Error tests routing to HandleError
func TestProcessWebhook_Error(t *testing.T) {
	ctx := context.Background()
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	mockNotificationRepo := new(MockNotificationRepositoryForItemError)
	mockPlaidService := new(MockPlaidService)

	mockEmailService := new(MockEmailService)
	mockUserRepo := new(MockUserRepository)

	service := NewItemErrorService(
		mockLinkedItemRepo,
		mockNotificationRepo,
		mockUserRepo,
		mockPlaidService,
		mockEmailService,
		nil,
	)

	// Mock: Lookup LinkedItem by itemID
	linkedItem := &domain.LinkedItem{
		ID:              "linked-item-123",
		UserID:          "user-456",
		ItemID:          "plaid-item-789",
		InstitutionName: "Test Bank",
		Status:          "active",
	}
	mockLinkedItemRepo.On("GetByItemID", ctx, "plaid-item-789").Return(linkedItem, nil)

	// Mock: All operations for HandleError (no decryption/link token)
	mockLinkedItemRepo.On("Update", ctx, mock.Anything).Return(nil)
	mockNotificationRepo.On("Create", ctx, mock.Anything).Return(nil)
	mockUserRepo.On("GetUserByID", ctx, "user-456").
		Return(&domain.User{ID: "user-456", Email: "real@user.com"}, nil)
	mockEmailService.On("SendNotificationEmail", ctx, mock.Anything, mock.Anything).Return(nil)

	// When: ProcessWebhook is called with ERROR
	err := service.ProcessWebhook(ctx, "plaid-item-789", "ERROR", "ITEM_ERROR", "Connection error")

	// Then: Routes to HandleError and succeeds
	require.NoError(t, err)
	mockLinkedItemRepo.AssertExpectations(t)
	mockNotificationRepo.AssertExpectations(t)
	mockEmailService.AssertExpectations(t)
	// HandleError does NOT call encryption or plaid service

	mockPlaidService.AssertNotCalled(t, "CreateUpdateModeLinkToken")
}

// TestProcessWebhook_PendingExpiration tests routing to HandlePendingExpiration
func TestProcessWebhook_PendingExpiration(t *testing.T) {
	ctx := context.Background()
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	mockNotificationRepo := new(MockNotificationRepositoryForItemError)
	mockPlaidService := new(MockPlaidService)

	mockEmailService := new(MockEmailService)
	mockUserRepo := new(MockUserRepository)

	service := NewItemErrorService(
		mockLinkedItemRepo,
		mockNotificationRepo,
		mockUserRepo,
		mockPlaidService,
		mockEmailService,
		nil,
	)

	// Mock: Lookup LinkedItem by itemID
	linkedItem := &domain.LinkedItem{
		ID:                   "linked-item-123",
		UserID:               "user-456",
		ItemID:               "plaid-item-789",
		AccessToken: "decrypted-access-token",
		InstitutionID:        "ins_1",
		InstitutionName:      "Test Bank",
		Status:               "active",
	}
	mockLinkedItemRepo.On("GetByItemID", ctx, "plaid-item-789").Return(linkedItem, nil)

	// Mock: All operations for HandlePendingExpiration

	linkTokenResponse := &domain.LinkTokenResponse{
		LinkToken:  "link-token-456",
		Expiration: time.Now().Add(30 * time.Minute),
		RequestID:  "req-456",
	}
	mockPlaidService.On("CreateUpdateModeLinkToken", ctx, "user-456", "decrypted-access-token").Return(linkTokenResponse, nil)
	mockLinkedItemRepo.On("Update", ctx, mock.Anything).Return(nil)
	mockNotificationRepo.On("Create", ctx, mock.Anything).Return(nil)
	mockUserRepo.On("GetUserByID", ctx, "user-456").
		Return(&domain.User{ID: "user-456", Email: "real@user.com"}, nil)
	mockEmailService.On("SendNotificationEmail", ctx, mock.Anything, mock.Anything).Return(nil)

	// When: ProcessWebhook is called with PENDING_EXPIRATION
	err := service.ProcessWebhook(ctx, "plaid-item-789", "PENDING_EXPIRATION", "ITEM_PENDING_EXPIRATION", "Access expiring soon")

	// Then: Routes to HandlePendingExpiration and succeeds
	require.NoError(t, err)
	mockLinkedItemRepo.AssertExpectations(t)

	mockPlaidService.AssertExpectations(t)
	mockNotificationRepo.AssertExpectations(t)
	mockEmailService.AssertExpectations(t)
}

// TestProcessWebhook_PermissionRevoked tests routing to HandlePermissionRevoked
func TestProcessWebhook_PermissionRevoked(t *testing.T) {
	ctx := context.Background()
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	mockNotificationRepo := new(MockNotificationRepositoryForItemError)
	mockPlaidService := new(MockPlaidService)

	mockEmailService := new(MockEmailService)
	mockUserRepo := new(MockUserRepository)

	service := NewItemErrorService(
		mockLinkedItemRepo,
		mockNotificationRepo,
		mockUserRepo,
		mockPlaidService,
		mockEmailService,
		nil,
	)

	// Mock: Lookup LinkedItem by itemID
	linkedItem := &domain.LinkedItem{
		ID:                   "linked-item-123",
		UserID:               "user-456",
		ItemID:               "plaid-item-789",
		AccessToken: "decrypted-access-token",
		InstitutionID:        "ins_1",
		InstitutionName:      "Test Bank",
		Status:               "active",
	}
	mockLinkedItemRepo.On("GetByItemID", ctx, "plaid-item-789").Return(linkedItem, nil)

	// Mock: All operations for HandlePermissionRevoked

	linkTokenResponse := &domain.LinkTokenResponse{
		LinkToken:  "link-token-789",
		Expiration: time.Now().Add(30 * time.Minute),
		RequestID:  "req-789",
	}
	mockPlaidService.On("CreateUpdateModeLinkToken", ctx, "user-456", "decrypted-access-token").Return(linkTokenResponse, nil)
	mockLinkedItemRepo.On("Update", ctx, mock.Anything).Return(nil)
	mockNotificationRepo.On("Create", ctx, mock.Anything).Return(nil)
	mockUserRepo.On("GetUserByID", ctx, "user-456").
		Return(&domain.User{ID: "user-456", Email: "real@user.com"}, nil)
	mockEmailService.On("SendNotificationEmail", ctx, mock.Anything, mock.Anything).Return(nil)

	// When: ProcessWebhook is called with USER_PERMISSION_REVOKED
	err := service.ProcessWebhook(ctx, "plaid-item-789", "USER_PERMISSION_REVOKED", "ITEM_PERMISSION_REVOKED", "User revoked access")

	// Then: Routes to HandlePermissionRevoked and succeeds
	require.NoError(t, err)
	mockLinkedItemRepo.AssertExpectations(t)

	mockPlaidService.AssertExpectations(t)
	mockNotificationRepo.AssertExpectations(t)
	mockEmailService.AssertExpectations(t)
}

// TestProcessWebhook_ItemError_LinkedItemNotFound tests handling when LinkedItem lookup fails
func TestProcessWebhook_ItemError_LinkedItemNotFound(t *testing.T) {
	ctx := context.Background()
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	mockNotificationRepo := new(MockNotificationRepositoryForItemError)
	mockPlaidService := new(MockPlaidService)

	mockEmailService := new(MockEmailService)
	mockUserRepo := new(MockUserRepository)

	service := NewItemErrorService(
		mockLinkedItemRepo,
		mockNotificationRepo,
		mockUserRepo,
		mockPlaidService,
		mockEmailService,
		nil,
	)

	// Mock: LinkedItem not found
	mockLinkedItemRepo.On("GetByItemID", ctx, "unknown-item-id").
		Return(nil, repository.ErrLinkedItemNotFound)

	// When: ProcessWebhook is called with unknown itemID
	err := service.ProcessWebhook(ctx, "unknown-item-id", "ITEM_LOGIN_REQUIRED", "ITEM_LOGIN_REQUIRED", "Test error")

	// Then: Returns error (critical failure - cannot process webhook without LinkedItem)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "linked item not found")
	mockLinkedItemRepo.AssertExpectations(t)
	// No handlers should be called if LinkedItem not found

	mockPlaidService.AssertNotCalled(t, "CreateUpdateModeLinkToken")
	mockNotificationRepo.AssertNotCalled(t, "Create")
	mockEmailService.AssertNotCalled(t, "SendNotificationEmail")
}

// TestProcessWebhook_ItemError_UnknownErrorType tests handling of unknown/unsupported error types
func TestProcessWebhook_ItemError_UnknownErrorType(t *testing.T) {
	ctx := context.Background()
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	mockNotificationRepo := new(MockNotificationRepositoryForItemError)
	mockPlaidService := new(MockPlaidService)

	mockEmailService := new(MockEmailService)
	mockUserRepo := new(MockUserRepository)

	service := NewItemErrorService(
		mockLinkedItemRepo,
		mockNotificationRepo,
		mockUserRepo,
		mockPlaidService,
		mockEmailService,
		nil,
	)

	// Mock: Lookup LinkedItem by itemID
	linkedItem := &domain.LinkedItem{
		ID:     "linked-item-123",
		UserID: "user-456",
		ItemID: "plaid-item-789",
		Status: "active",
	}
	mockLinkedItemRepo.On("GetByItemID", ctx, "plaid-item-789").Return(linkedItem, nil)

	// When: ProcessWebhook is called with unknown error type
	err := service.ProcessWebhook(ctx, "plaid-item-789", "UNKNOWN_ERROR_TYPE", "UNKNOWN_CODE", "Test error")

	// Then: Returns error (unsupported error type)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported error type")
	mockLinkedItemRepo.AssertExpectations(t)
	// No handlers should be called for unknown error type

	mockPlaidService.AssertNotCalled(t, "CreateUpdateModeLinkToken")
	mockNotificationRepo.AssertNotCalled(t, "Create")
	mockEmailService.AssertNotCalled(t, "SendNotificationEmail")
}

// ============================================================================
// User Lookup Failure Tests (Graceful Degradation)
// ============================================================================

// TestHandleLoginRequired_UserLookupFailureNonBlocking tests graceful degradation when user lookup fails
func TestHandleLoginRequired_UserLookupFailureNonBlocking(t *testing.T) {
	ctx := context.Background()
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	mockNotificationRepo := new(MockNotificationRepositoryForItemError)
	mockPlaidService := new(MockPlaidService)

	mockEmailService := new(MockEmailService)
	mockUserRepo := new(MockUserRepository)

	service := NewItemErrorService(
		mockLinkedItemRepo,
		mockNotificationRepo,
		mockUserRepo,
		mockPlaidService,
		mockEmailService,
		nil,
	)

	linkedItem := &domain.LinkedItem{
		ID:                   "linked-item-123",
		UserID:               "user-456",
		ItemID:               "plaid-item-789",
		AccessToken: "decrypted-access-token",
		InstitutionName:      "Test Bank",
		Status:               "active",
	}


	linkTokenResponse := &domain.LinkTokenResponse{LinkToken: "link-token-123", Expiration: time.Now().Add(30 * time.Minute), RequestID: "req-123"}
	mockPlaidService.On("CreateUpdateModeLinkToken", ctx, "user-456", "decrypted-access-token").Return(linkTokenResponse, nil)
	mockLinkedItemRepo.On("Update", ctx, mock.Anything).Return(nil)
	mockNotificationRepo.On("Create", ctx, mock.Anything).Return(nil)

	// Mock: User lookup FAILS (graceful degradation - email skipped)
	mockUserRepo.On("GetUserByID", ctx, "user-456").Return(nil, errors.New("user not found"))

	err := service.HandleLoginRequired(ctx, linkedItem, "ITEM_LOGIN_REQUIRED", "Test error")

	// Then: Should succeed despite user lookup failure (in-app notification was created)
	require.NoError(t, err, "User lookup failure should not block webhook processing")
	mockNotificationRepo.AssertExpectations(t)
	mockUserRepo.AssertExpectations(t)
	// Email should NOT be sent if user lookup fails
	mockEmailService.AssertNotCalled(t, "SendNotificationEmail")
}

// TestHandleError_UserLookupFailureNonBlocking tests graceful degradation when user lookup fails
func TestHandleError_UserLookupFailureNonBlocking(t *testing.T) {
	ctx := context.Background()
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	mockNotificationRepo := new(MockNotificationRepositoryForItemError)
	mockPlaidService := new(MockPlaidService)

	mockEmailService := new(MockEmailService)
	mockUserRepo := new(MockUserRepository)

	service := NewItemErrorService(
		mockLinkedItemRepo,
		mockNotificationRepo,
		mockUserRepo,
		mockPlaidService,
		mockEmailService,
		nil,
	)

	linkedItem := &domain.LinkedItem{
		ID:              "linked-item-123",
		UserID:          "user-456",
		ItemID:          "plaid-item-789",
		InstitutionName: "Test Bank",
		Status:          "active",
	}

	mockLinkedItemRepo.On("Update", ctx, mock.Anything).Return(nil)
	mockNotificationRepo.On("Create", ctx, mock.Anything).Return(nil)

	// Mock: User lookup FAILS (graceful degradation - email skipped)
	mockUserRepo.On("GetUserByID", ctx, "user-456").Return(nil, errors.New("user not found"))

	err := service.HandleError(ctx, linkedItem, "ITEM_ERROR", "Test error")

	// Then: Should succeed despite user lookup failure (in-app notification was created)
	require.NoError(t, err, "User lookup failure should not block webhook processing")
	mockNotificationRepo.AssertExpectations(t)
	mockUserRepo.AssertExpectations(t)
	mockEmailService.AssertNotCalled(t, "SendNotificationEmail")
}

// TestHandlePendingExpiration_UserLookupFailureNonBlocking tests graceful degradation when user lookup fails
func TestHandlePendingExpiration_UserLookupFailureNonBlocking(t *testing.T) {
	ctx := context.Background()
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	mockNotificationRepo := new(MockNotificationRepositoryForItemError)
	mockPlaidService := new(MockPlaidService)

	mockEmailService := new(MockEmailService)
	mockUserRepo := new(MockUserRepository)

	service := NewItemErrorService(
		mockLinkedItemRepo,
		mockNotificationRepo,
		mockUserRepo,
		mockPlaidService,
		mockEmailService,
		nil,
	)

	linkedItem := &domain.LinkedItem{
		ID:                   "linked-item-123",
		UserID:               "user-456",
		ItemID:               "plaid-item-789",
		AccessToken: "decrypted-access-token",
		InstitutionName:      "Test Bank",
		Status:               "active",
	}


	linkTokenResponse := &domain.LinkTokenResponse{LinkToken: "link-token-456", Expiration: time.Now().Add(30 * time.Minute), RequestID: "req-456"}
	mockPlaidService.On("CreateUpdateModeLinkToken", ctx, "user-456", "decrypted-access-token").Return(linkTokenResponse, nil)
	mockLinkedItemRepo.On("Update", ctx, mock.Anything).Return(nil)
	mockNotificationRepo.On("Create", ctx, mock.Anything).Return(nil)

	// Mock: User lookup FAILS (graceful degradation - email skipped)
	mockUserRepo.On("GetUserByID", ctx, "user-456").Return(nil, errors.New("user not found"))

	err := service.HandlePendingExpiration(ctx, linkedItem, "ITEM_PENDING_EXPIRATION", "Test error")

	// Then: Should succeed despite user lookup failure (in-app notification was created)
	require.NoError(t, err, "User lookup failure should not block webhook processing")
	mockNotificationRepo.AssertExpectations(t)
	mockUserRepo.AssertExpectations(t)
	mockEmailService.AssertNotCalled(t, "SendNotificationEmail")
}

// TestHandlePermissionRevoked_UserLookupFailureNonBlocking tests graceful degradation when user lookup fails
func TestHandlePermissionRevoked_UserLookupFailureNonBlocking(t *testing.T) {
	ctx := context.Background()
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	mockNotificationRepo := new(MockNotificationRepositoryForItemError)
	mockPlaidService := new(MockPlaidService)

	mockEmailService := new(MockEmailService)
	mockUserRepo := new(MockUserRepository)

	service := NewItemErrorService(
		mockLinkedItemRepo,
		mockNotificationRepo,
		mockUserRepo,
		mockPlaidService,
		mockEmailService,
		nil,
	)

	linkedItem := &domain.LinkedItem{
		ID:                   "linked-item-123",
		UserID:               "user-456",
		ItemID:               "plaid-item-789",
		AccessToken: "decrypted-access-token",
		InstitutionName:      "Test Bank",
		Status:               "active",
	}


	linkTokenResponse := &domain.LinkTokenResponse{LinkToken: "link-token-789", Expiration: time.Now().Add(30 * time.Minute), RequestID: "req-789"}
	mockPlaidService.On("CreateUpdateModeLinkToken", ctx, "user-456", "decrypted-access-token").Return(linkTokenResponse, nil)
	mockLinkedItemRepo.On("Update", ctx, mock.Anything).Return(nil)
	mockNotificationRepo.On("Create", ctx, mock.Anything).Return(nil)

	// Mock: User lookup FAILS (graceful degradation - email skipped)
	mockUserRepo.On("GetUserByID", ctx, "user-456").Return(nil, errors.New("user not found"))

	err := service.HandlePermissionRevoked(ctx, linkedItem, "ITEM_PERMISSION_REVOKED", "Test error")

	// Then: Should succeed despite user lookup failure (in-app notification was created)
	require.NoError(t, err, "User lookup failure should not block webhook processing")
	mockNotificationRepo.AssertExpectations(t)
	mockUserRepo.AssertExpectations(t)
	mockEmailService.AssertNotCalled(t, "SendNotificationEmail")
}
