package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/maxnussbaum/BoostedBudget/internal/testsupport/mockassert"
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
		ID:            "linked-item-123",
		UserID:        "user-456",
		ItemID:        "plaid-item-789",
		AccessToken:   "decrypted-access-token", // Decrypted by repo layer
		InstitutionID: "ins_1",
		Status:        "active", // Currently active, will become "login_required"
	}

	// Mock: Update LinkedItem status to "login_required"
	mockLinkedItemRepo.On("TransitionErrorStatus", ctx, "linked-item-123", domain.LinkedItemStatusLoginRequired,
		"ITEM_LOGIN_REQUIRED", "Credentials changed, please re-authenticate", terminalLinkedItemStatuses).
		Return(true, nil)

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

	// #1609: a Plaid outage must not stop the user being told their connection broke — the
	// update-mode token is now minted on demand by the reconnect endpoint, not eagerly here.
	mockassert.NoCallTo(t, &mockPlaidService.Mock, "CreateUpdateModeLinkToken")
	mockLinkedItemRepo.AssertExpectations(t)
	mockNotificationRepo.AssertExpectations(t)
	mockUserRepo.AssertExpectations(t)
	mockEmailService.AssertExpectations(t)
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
		ID:          "linked-item-123",
		UserID:      "user-456",
		ItemID:      "plaid-item-789",
		AccessToken: "decrypted-access-token",
		Status:      "active",
	}

	// Mock: Database update fails
	mockLinkedItemRepo.On("TransitionErrorStatus", ctx, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(false, errors.New("database error"))

	// When: HandleLoginRequired is called
	err := service.HandleLoginRequired(ctx, linkedItem, "ITEM_LOGIN_REQUIRED", "Test error")

	// Then: Returns error (critical failure)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "update")

	mockassert.NoCallTo(t, &mockPlaidService.Mock, "CreateUpdateModeLinkToken")
	mockLinkedItemRepo.AssertExpectations(t)
	// Notification should NOT be created if DB update fails
	mockassert.NoCallTo(t, &mockNotificationRepo.Mock, "Create")
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
		ID:          "linked-item-123",
		UserID:      "user-456",
		ItemID:      "plaid-item-789",
		AccessToken: "decrypted-access-token",
		Status:      "active",
	}

	// Mock: Database update succeeds
	mockLinkedItemRepo.On("TransitionErrorStatus", ctx, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(true, nil)

	// Mock: Notification creation FAILS (graceful degradation)
	mockNotificationRepo.On("Create", ctx, mock.Anything).
		Return(errors.New("notification database error"))

	// Mock: Email sending should still be attempted but is NOT called due to notification creation failure
	// (Implementation choice: only send email if notification is created)

	// When: HandleLoginRequired is called
	err := service.HandleLoginRequired(ctx, linkedItem, "ITEM_LOGIN_REQUIRED", "Test error")

	// Then: Should succeed despite notification failure (graceful degradation)
	// Critical operations (DB update) succeeded
	require.NoError(t, err, "Notification failure should not block webhook processing")

	mockassert.NoCallTo(t, &mockPlaidService.Mock, "CreateUpdateModeLinkToken")
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
		ID:          "linked-item-123",
		UserID:      "user-456",
		ItemID:      "plaid-item-789",
		AccessToken: "decrypted-access-token",
		Status:      "active",
	}

	// Mock: Database update succeeds
	mockLinkedItemRepo.On("TransitionErrorStatus", ctx, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(true, nil)

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

	mockassert.NoCallTo(t, &mockPlaidService.Mock, "CreateUpdateModeLinkToken")
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

func (m *MockNotificationRepositoryForItemError) MarkAllAsRead(ctx context.Context, userID string) (int64, error) {
	args := m.Called(ctx, userID)
	return args.Get(0).(int64), args.Error(1)
}

func (m *MockNotificationRepositoryForItemError) GetOpenByTransferSuggestionID(ctx context.Context, transferSuggestionID string, notificationType string) (*domain.Notification, error) {
	args := m.Called(ctx, transferSuggestionID, notificationType)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Notification), args.Error(1)
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
		ID:              "linked-item-123",
		UserID:          "user-456",
		ItemID:          "plaid-item-789",
		AccessToken:     "decrypted-access-token",
		InstitutionID:   "ins_1",
		InstitutionName: "Test Bank",
		Status:          "active", // Currently active, will become "error"
	}

	// Mock: Update LinkedItem status to "error"
	mockLinkedItemRepo.On("TransitionErrorStatus", ctx, "linked-item-123", domain.LinkedItemStatusError,
		"ITEM_ERROR", "Connection error, please try again later", terminalLinkedItemStatuses).
		Return(true, nil)

	// Mock: Create notification WITH an action URL (Issue #1609 P3-T8, M5: 'error' IS a
	// repairableStatuses member, so the one state most likely to be reached by a sync-discovered
	// failure must offer a way to act, the same /accounts/reconnect CTA the login-required path
	// uses).
	mockNotificationRepo.On("Create", ctx, mock.MatchedBy(func(n *domain.Notification) bool {
		return n.UserID == "user-456" &&
			n.Type == "item_error" &&
			n.Title == "Connection Error" &&
			n.ActionURL != nil && *n.ActionURL == "/accounts/reconnect?item_id=plaid-item-789" &&
			n.ActionLabel != nil && *n.ActionLabel == "Reconnect Bank"
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
	mockassert.NoCallTo(t, &mockPlaidService.Mock, "CreateUpdateModeLinkToken")

}

// TestHandleError_EmptyMessage_NoDanglingSentence is a regression test for Issue #1609 P3-T3:
// webhook_processing.go's processItemWebhook has no ErrorMessage field to forward (only ErrorCode
// exists on domain.WebhookEvent/webhook_events), so it passes errorMessage="" whenever the stored
// webhook payload doesn't yield one. HandleError's notification template must tolerate that and
// NOT render a colon followed immediately by a period (the "with Test Bank: ." dangling sentence),
// since an absent message is a legitimate, expected input here — not a bug to paper over.
func TestHandleError_EmptyMessage_NoDanglingSentence(t *testing.T) {
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

	mockLinkedItemRepo.On("TransitionErrorStatus", ctx, "linked-item-123", domain.LinkedItemStatusError,
		"ITEM_ERROR", "", terminalLinkedItemStatuses).
		Return(true, nil)

	var capturedNotification *domain.Notification
	mockNotificationRepo.On("Create", ctx, mock.MatchedBy(func(n *domain.Notification) bool {
		capturedNotification = n
		return true
	})).Return(nil)

	mockUserRepo.On("GetUserByID", ctx, "user-456").
		Return(&domain.User{ID: "user-456", Email: "real@user.com"}, nil)
	mockEmailService.On("SendNotificationEmail", ctx, "real@user.com", mock.Anything).
		Return(nil)

	err := service.HandleError(ctx, linkedItem, "ITEM_ERROR", "")

	require.NoError(t, err)
	require.NotNil(t, capturedNotification)
	assert.NotContains(t, capturedNotification.Message, ": .",
		"an empty error message must not leave a dangling colon-period in the notification text")
	assert.Contains(t, capturedNotification.Message, "Test Bank")
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
	mockLinkedItemRepo.On("TransitionErrorStatus", ctx, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(false, errors.New("database error"))

	// When: HandleError is called
	err := service.HandleError(ctx, linkedItem, "ITEM_ERROR", "Test error")

	// Then: Returns error (critical failure)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "update")
	mockLinkedItemRepo.AssertExpectations(t)
	// Notification should NOT be created if DB update fails
	mockassert.NoCallTo(t, &mockNotificationRepo.Mock, "Create")
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
	mockLinkedItemRepo.On("TransitionErrorStatus", ctx, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(true, nil)

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
	mockLinkedItemRepo.On("TransitionErrorStatus", ctx, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(true, nil)

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

// TestHandleError_NotificationType_IsItemError is a regression test verifying that
// HandleError creates a notification with Type "item_error" (not the historical bug "error").
func TestHandleError_NotificationType_IsItemError(t *testing.T) {
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

	mockLinkedItemRepo.On("TransitionErrorStatus", ctx, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(true, nil)

	// Capture the notification to verify its Type field
	var capturedNotification *domain.Notification
	mockNotificationRepo.On("Create", ctx, mock.MatchedBy(func(n *domain.Notification) bool {
		capturedNotification = n
		return true
	})).Return(nil)

	mockUserRepo.On("GetUserByID", ctx, "user-456").
		Return(&domain.User{ID: "user-456", Email: "real@user.com"}, nil)
	mockEmailService.On("SendNotificationEmail", ctx, "real@user.com", mock.Anything).
		Return(nil)

	err := service.HandleError(ctx, linkedItem, "ITEM_ERROR", "Connection error")

	require.NoError(t, err)
	require.NotNil(t, capturedNotification)
	assert.Equal(t, "item_error", capturedNotification.Type, "HandleError must use type 'item_error', not 'error'")
	// Issue #1609 P3-T8, M5: this is now "action_required", not "informational" — the notification
	// carries an ActionURL/ActionLabel (the /accounts/reconnect CTA), matching login_required.
	assert.Equal(t, "action_required", capturedNotification.InteractionPattern)

	mockLinkedItemRepo.AssertExpectations(t)
	mockNotificationRepo.AssertExpectations(t)
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
		ID:              "linked-item-123",
		UserID:          "user-456",
		ItemID:          "plaid-item-789",
		AccessToken:     "decrypted-access-token",
		InstitutionID:   "ins_1",
		InstitutionName: "Test Bank",
		Status:          "active", // Currently active, will become "pending_expiration"
	}

	// Mock: Update LinkedItem status to "pending_expiration"
	mockLinkedItemRepo.On("TransitionErrorStatus", ctx, "linked-item-123", domain.LinkedItemStatusPendingExpiration,
		"ITEM_PENDING_EXPIRATION", "Access expiring soon, please re-authenticate", terminalLinkedItemStatuses).
		Return(true, nil)

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

	// #1609: a Plaid outage must not stop the user being told their connection broke — the
	// update-mode token is now minted on demand by the reconnect endpoint, not eagerly here.
	mockassert.NoCallTo(t, &mockPlaidService.Mock, "CreateUpdateModeLinkToken")
	mockLinkedItemRepo.AssertExpectations(t)
	mockNotificationRepo.AssertExpectations(t)
	mockUserRepo.AssertExpectations(t)
	mockEmailService.AssertExpectations(t)
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
		ID:          "linked-item-123",
		UserID:      "user-456",
		ItemID:      "plaid-item-789",
		AccessToken: "decrypted-access-token",
		Status:      "active",
	}

	// Mock: Database update fails
	mockLinkedItemRepo.On("TransitionErrorStatus", ctx, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(false, errors.New("database error"))

	// When: HandlePendingExpiration is called
	err := service.HandlePendingExpiration(ctx, linkedItem, "ITEM_PENDING_EXPIRATION", "Test error")

	// Then: Returns error (critical failure)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "update")

	mockassert.NoCallTo(t, &mockPlaidService.Mock, "CreateUpdateModeLinkToken")
	mockLinkedItemRepo.AssertExpectations(t)
	// Notification should NOT be created if DB update fails
	mockassert.NoCallTo(t, &mockNotificationRepo.Mock, "Create")
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
		ID:          "linked-item-123",
		UserID:      "user-456",
		ItemID:      "plaid-item-789",
		AccessToken: "decrypted-access-token",
		Status:      "active",
	}

	// Mock: Database update succeeds
	mockLinkedItemRepo.On("TransitionErrorStatus", ctx, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(true, nil)

	// Mock: Notification creation FAILS (graceful degradation)
	mockNotificationRepo.On("Create", ctx, mock.Anything).
		Return(errors.New("notification database error"))

	// When: HandlePendingExpiration is called
	err := service.HandlePendingExpiration(ctx, linkedItem, "ITEM_PENDING_EXPIRATION", "Test error")

	// Then: Should succeed despite notification failure (graceful degradation)
	require.NoError(t, err, "Notification failure should not block webhook processing")

	mockassert.NoCallTo(t, &mockPlaidService.Mock, "CreateUpdateModeLinkToken")
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
		ID:          "linked-item-123",
		UserID:      "user-456",
		ItemID:      "plaid-item-789",
		AccessToken: "decrypted-access-token",
		Status:      "active",
	}

	// Mock: Database update succeeds
	mockLinkedItemRepo.On("TransitionErrorStatus", ctx, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(true, nil)

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

	mockassert.NoCallTo(t, &mockPlaidService.Mock, "CreateUpdateModeLinkToken")
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
		ID:              "linked-item-123",
		UserID:          "user-456",
		ItemID:          "plaid-item-789",
		AccessToken:     "decrypted-access-token",
		InstitutionID:   "ins_1",
		InstitutionName: "Test Bank",
		Status:          "active", // Currently active, will become "revoked"
	}

	// Mock: Update LinkedItem status to "revoked"
	mockLinkedItemRepo.On("TransitionErrorStatus", ctx, "linked-item-123", domain.LinkedItemStatusRevoked,
		"ITEM_PERMISSION_REVOKED", "User revoked access at bank", terminalLinkedItemStatuses).
		Return(true, nil)

	// Mock: Create notification with action URL. Issue #1609 round 2, BLOCKING 1: routes to
	// /accounts (the normal, non-update-mode Link flow), NOT /accounts/reconnect — a revoked
	// Item cannot be repaired via update mode (see ErrLinkedItemNotRepairable).
	mockNotificationRepo.On("Create", ctx, mock.MatchedBy(func(n *domain.Notification) bool {
		return n.UserID == "user-456" &&
			n.Type == "permission_revoked" &&
			n.Title == "Bank Access Revoked" &&
			n.ActionURL != nil && *n.ActionURL == "/accounts" &&
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

	// #1609: a Plaid outage must not stop the user being told their connection broke — the
	// update-mode token is now minted on demand by the reconnect endpoint, not eagerly here.
	mockassert.NoCallTo(t, &mockPlaidService.Mock, "CreateUpdateModeLinkToken")
	mockLinkedItemRepo.AssertExpectations(t)
	mockNotificationRepo.AssertExpectations(t)
	mockUserRepo.AssertExpectations(t)
	mockEmailService.AssertExpectations(t)
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
		ID:          "linked-item-123",
		UserID:      "user-456",
		ItemID:      "plaid-item-789",
		AccessToken: "decrypted-access-token",
		Status:      "active",
	}

	// Mock: Database update fails
	mockLinkedItemRepo.On("TransitionErrorStatus", ctx, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(false, errors.New("database error"))

	// When: HandlePermissionRevoked is called
	err := service.HandlePermissionRevoked(ctx, linkedItem, "ITEM_PERMISSION_REVOKED", "Test error")

	// Then: Returns error (critical failure)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "update")

	mockassert.NoCallTo(t, &mockPlaidService.Mock, "CreateUpdateModeLinkToken")
	mockLinkedItemRepo.AssertExpectations(t)
	// Notification should NOT be created if DB update fails
	mockassert.NoCallTo(t, &mockNotificationRepo.Mock, "Create")
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
		ID:          "linked-item-123",
		UserID:      "user-456",
		ItemID:      "plaid-item-789",
		AccessToken: "decrypted-access-token",
		Status:      "active",
	}

	// Mock: Database update succeeds
	mockLinkedItemRepo.On("TransitionErrorStatus", ctx, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(true, nil)

	// Mock: Notification creation FAILS (graceful degradation)
	mockNotificationRepo.On("Create", ctx, mock.Anything).
		Return(errors.New("notification database error"))

	// When: HandlePermissionRevoked is called
	err := service.HandlePermissionRevoked(ctx, linkedItem, "ITEM_PERMISSION_REVOKED", "Test error")

	// Then: Should succeed despite notification failure (graceful degradation)
	require.NoError(t, err, "Notification failure should not block webhook processing")

	mockassert.NoCallTo(t, &mockPlaidService.Mock, "CreateUpdateModeLinkToken")
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
		ID:          "linked-item-123",
		UserID:      "user-456",
		ItemID:      "plaid-item-789",
		AccessToken: "decrypted-access-token",
		Status:      "active",
	}

	// Mock: Database update succeeds
	mockLinkedItemRepo.On("TransitionErrorStatus", ctx, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(true, nil)

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

	mockassert.NoCallTo(t, &mockPlaidService.Mock, "CreateUpdateModeLinkToken")
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
		ID:              "linked-item-123",
		UserID:          "user-456",
		ItemID:          "plaid-item-789",
		AccessToken:     "decrypted-access-token",
		InstitutionID:   "ins_1",
		InstitutionName: "Test Bank",
		Status:          "active",
	}
	mockLinkedItemRepo.On("GetByItemID", ctx, "plaid-item-789").Return(linkedItem, nil)

	// Mock: All operations for HandleLoginRequired
	mockLinkedItemRepo.On("TransitionErrorStatus", ctx, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(true, nil)
	mockNotificationRepo.On("Create", ctx, mock.Anything).Return(nil)
	mockUserRepo.On("GetUserByID", ctx, "user-456").
		Return(&domain.User{ID: "user-456", Email: "real@user.com"}, nil)
	mockEmailService.On("SendNotificationEmail", ctx, mock.Anything, mock.Anything).Return(nil)

	// When: ProcessWebhook is called with ITEM_LOGIN_REQUIRED
	err := service.ProcessWebhook(ctx, "plaid-item-789", "ITEM_LOGIN_REQUIRED", "ITEM_LOGIN_REQUIRED", "Credentials changed")

	// Then: Routes to HandleLoginRequired and succeeds
	require.NoError(t, err)
	mockLinkedItemRepo.AssertExpectations(t)

	mockassert.NoCallTo(t, &mockPlaidService.Mock, "CreateUpdateModeLinkToken")
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
	mockLinkedItemRepo.On("TransitionErrorStatus", ctx, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(true, nil)
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

	mockassert.NoCallTo(t, &mockPlaidService.Mock, "CreateUpdateModeLinkToken")
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
		ID:              "linked-item-123",
		UserID:          "user-456",
		ItemID:          "plaid-item-789",
		AccessToken:     "decrypted-access-token",
		InstitutionID:   "ins_1",
		InstitutionName: "Test Bank",
		Status:          "active",
	}
	mockLinkedItemRepo.On("GetByItemID", ctx, "plaid-item-789").Return(linkedItem, nil)

	// Mock: All operations for HandlePendingExpiration
	mockLinkedItemRepo.On("TransitionErrorStatus", ctx, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(true, nil)
	mockNotificationRepo.On("Create", ctx, mock.Anything).Return(nil)
	mockUserRepo.On("GetUserByID", ctx, "user-456").
		Return(&domain.User{ID: "user-456", Email: "real@user.com"}, nil)
	mockEmailService.On("SendNotificationEmail", ctx, mock.Anything, mock.Anything).Return(nil)

	// When: ProcessWebhook is called with PENDING_EXPIRATION
	err := service.ProcessWebhook(ctx, "plaid-item-789", "PENDING_EXPIRATION", "ITEM_PENDING_EXPIRATION", "Access expiring soon")

	// Then: Routes to HandlePendingExpiration and succeeds
	require.NoError(t, err)
	mockLinkedItemRepo.AssertExpectations(t)

	mockassert.NoCallTo(t, &mockPlaidService.Mock, "CreateUpdateModeLinkToken")
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
		ID:              "linked-item-123",
		UserID:          "user-456",
		ItemID:          "plaid-item-789",
		AccessToken:     "decrypted-access-token",
		InstitutionID:   "ins_1",
		InstitutionName: "Test Bank",
		Status:          "active",
	}
	mockLinkedItemRepo.On("GetByItemID", ctx, "plaid-item-789").Return(linkedItem, nil)

	// Mock: All operations for HandlePermissionRevoked
	mockLinkedItemRepo.On("TransitionErrorStatus", ctx, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(true, nil)
	mockNotificationRepo.On("Create", ctx, mock.Anything).Return(nil)
	mockUserRepo.On("GetUserByID", ctx, "user-456").
		Return(&domain.User{ID: "user-456", Email: "real@user.com"}, nil)
	mockEmailService.On("SendNotificationEmail", ctx, mock.Anything, mock.Anything).Return(nil)

	// When: ProcessWebhook is called with USER_PERMISSION_REVOKED
	err := service.ProcessWebhook(ctx, "plaid-item-789", "USER_PERMISSION_REVOKED", "ITEM_PERMISSION_REVOKED", "User revoked access")

	// Then: Routes to HandlePermissionRevoked and succeeds
	require.NoError(t, err)
	mockLinkedItemRepo.AssertExpectations(t)

	mockassert.NoCallTo(t, &mockPlaidService.Mock, "CreateUpdateModeLinkToken")
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

	mockassert.NoCallTo(t, &mockPlaidService.Mock, "CreateUpdateModeLinkToken")
	mockassert.NoCallTo(t, &mockNotificationRepo.Mock, "Create")
	mockassert.NoCallTo(t, &mockEmailService.Mock, "SendNotificationEmail")
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

	mockassert.NoCallTo(t, &mockPlaidService.Mock, "CreateUpdateModeLinkToken")
	mockassert.NoCallTo(t, &mockNotificationRepo.Mock, "Create")
	mockassert.NoCallTo(t, &mockEmailService.Mock, "SendNotificationEmail")
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
		ID:              "linked-item-123",
		UserID:          "user-456",
		ItemID:          "plaid-item-789",
		AccessToken:     "decrypted-access-token",
		InstitutionName: "Test Bank",
		Status:          "active",
	}

	mockLinkedItemRepo.On("TransitionErrorStatus", ctx, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(true, nil)
	mockNotificationRepo.On("Create", ctx, mock.Anything).Return(nil)

	// Mock: User lookup FAILS (graceful degradation - email skipped)
	mockUserRepo.On("GetUserByID", ctx, "user-456").Return(nil, errors.New("user not found"))

	err := service.HandleLoginRequired(ctx, linkedItem, "ITEM_LOGIN_REQUIRED", "Test error")

	// Then: Should succeed despite user lookup failure (in-app notification was created)
	require.NoError(t, err, "User lookup failure should not block webhook processing")
	mockNotificationRepo.AssertExpectations(t)
	mockUserRepo.AssertExpectations(t)
	// Email should NOT be sent if user lookup fails
	mockassert.NoCallTo(t, &mockEmailService.Mock, "SendNotificationEmail")
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

	mockLinkedItemRepo.On("TransitionErrorStatus", ctx, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(true, nil)
	mockNotificationRepo.On("Create", ctx, mock.Anything).Return(nil)

	// Mock: User lookup FAILS (graceful degradation - email skipped)
	mockUserRepo.On("GetUserByID", ctx, "user-456").Return(nil, errors.New("user not found"))

	err := service.HandleError(ctx, linkedItem, "ITEM_ERROR", "Test error")

	// Then: Should succeed despite user lookup failure (in-app notification was created)
	require.NoError(t, err, "User lookup failure should not block webhook processing")
	mockNotificationRepo.AssertExpectations(t)
	mockUserRepo.AssertExpectations(t)
	mockassert.NoCallTo(t, &mockEmailService.Mock, "SendNotificationEmail")
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
		ID:              "linked-item-123",
		UserID:          "user-456",
		ItemID:          "plaid-item-789",
		AccessToken:     "decrypted-access-token",
		InstitutionName: "Test Bank",
		Status:          "active",
	}

	mockLinkedItemRepo.On("TransitionErrorStatus", ctx, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(true, nil)
	mockNotificationRepo.On("Create", ctx, mock.Anything).Return(nil)

	// Mock: User lookup FAILS (graceful degradation - email skipped)
	mockUserRepo.On("GetUserByID", ctx, "user-456").Return(nil, errors.New("user not found"))

	err := service.HandlePendingExpiration(ctx, linkedItem, "ITEM_PENDING_EXPIRATION", "Test error")

	// Then: Should succeed despite user lookup failure (in-app notification was created)
	require.NoError(t, err, "User lookup failure should not block webhook processing")
	mockNotificationRepo.AssertExpectations(t)
	mockUserRepo.AssertExpectations(t)
	mockassert.NoCallTo(t, &mockEmailService.Mock, "SendNotificationEmail")
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
		ID:              "linked-item-123",
		UserID:          "user-456",
		ItemID:          "plaid-item-789",
		AccessToken:     "decrypted-access-token",
		InstitutionName: "Test Bank",
		Status:          "active",
	}

	mockLinkedItemRepo.On("TransitionErrorStatus", ctx, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(true, nil)
	mockNotificationRepo.On("Create", ctx, mock.Anything).Return(nil)

	// Mock: User lookup FAILS (graceful degradation - email skipped)
	mockUserRepo.On("GetUserByID", ctx, "user-456").Return(nil, errors.New("user not found"))

	err := service.HandlePermissionRevoked(ctx, linkedItem, "ITEM_PERMISSION_REVOKED", "Test error")

	// Then: Should succeed despite user lookup failure (in-app notification was created)
	require.NoError(t, err, "User lookup failure should not block webhook processing")
	mockNotificationRepo.AssertExpectations(t)
	mockUserRepo.AssertExpectations(t)
	mockassert.NoCallTo(t, &mockEmailService.Mock, "SendNotificationEmail")
}

// ============================================================================
// CreateReconnectLinkToken Tests (Issue #1609)
// ============================================================================

func newReconnectTestService() (ItemErrorService, *MockLinkedItemRepository, *MockPlaidService) {
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
	return service, mockLinkedItemRepo, mockPlaidService
}

func TestCreateReconnectLinkToken_Success(t *testing.T) {
	ctx := context.Background()
	service, mockLinkedItemRepo, mockPlaidService := newReconnectTestService()

	linkedItem := &domain.LinkedItem{
		ID:          "linked-item-123",
		UserID:      "user-456",
		ItemID:      "plaid-item-789",
		AccessToken: "decrypted-access-token",
		Status:      domain.LinkedItemStatusLoginRequired,
	}
	mockLinkedItemRepo.On("GetByItemID", ctx, "plaid-item-789").Return(linkedItem, nil)

	linkTokenResponse := &domain.LinkTokenResponse{LinkToken: "link-token-fresh"}
	mockPlaidService.On("CreateUpdateModeLinkToken", ctx, "user-456", "decrypted-access-token").
		Return(linkTokenResponse, nil)

	resp, err := service.CreateReconnectLinkToken(ctx, "user-456", "plaid-item-789")

	require.NoError(t, err)
	assert.Equal(t, "link-token-fresh", resp.LinkToken)
	mockPlaidService.AssertExpectations(t)
}

func TestCreateReconnectLinkToken_WrongUser_ReturnsNotFound(t *testing.T) {
	ctx := context.Background()
	service, mockLinkedItemRepo, mockPlaidService := newReconnectTestService()

	linkedItem := &domain.LinkedItem{
		ID:          "linked-item-123",
		UserID:      "user-456", // owned by a different user
		ItemID:      "plaid-item-789",
		AccessToken: "decrypted-access-token",
		Status:      domain.LinkedItemStatusLoginRequired,
	}
	mockLinkedItemRepo.On("GetByItemID", ctx, "plaid-item-789").Return(linkedItem, nil)

	_, err := service.CreateReconnectLinkToken(ctx, "attacker-999", "plaid-item-789")

	require.Error(t, err)
	assert.ErrorIs(t, err, repository.ErrLinkedItemNotFound)
	mockassert.NoCallTo(t, &mockPlaidService.Mock, "CreateUpdateModeLinkToken")
}

func TestCreateReconnectLinkToken_ActiveItem_ReturnsNotRepairable(t *testing.T) {
	ctx := context.Background()
	service, mockLinkedItemRepo, mockPlaidService := newReconnectTestService()

	linkedItem := &domain.LinkedItem{
		ID:          "linked-item-123",
		UserID:      "user-456",
		ItemID:      "plaid-item-789",
		AccessToken: "decrypted-access-token",
		Status:      domain.LinkedItemStatusActive, // nothing broken
	}
	mockLinkedItemRepo.On("GetByItemID", ctx, "plaid-item-789").Return(linkedItem, nil)

	_, err := service.CreateReconnectLinkToken(ctx, "user-456", "plaid-item-789")

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrLinkedItemNotRepairable)
	mockassert.NoCallTo(t, &mockPlaidService.Mock, "CreateUpdateModeLinkToken")
}

func TestCreateReconnectLinkToken_DisconnectedItem_ReturnsNotRepairable(t *testing.T) {
	ctx := context.Background()
	service, mockLinkedItemRepo, mockPlaidService := newReconnectTestService()

	linkedItem := &domain.LinkedItem{
		ID:          "linked-item-123",
		UserID:      "user-456",
		ItemID:      "plaid-item-789",
		AccessToken: "decrypted-access-token",
		Status:      domain.LinkedItemStatusDisconnected, // user deliberately disconnected
	}
	mockLinkedItemRepo.On("GetByItemID", ctx, "plaid-item-789").Return(linkedItem, nil)

	_, err := service.CreateReconnectLinkToken(ctx, "user-456", "plaid-item-789")

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrLinkedItemNotRepairable)
	mockassert.NoCallTo(t, &mockPlaidService.Mock, "CreateUpdateModeLinkToken")
}

func TestCreateReconnectLinkToken_ReplacedItem_ReturnsNotRepairable(t *testing.T) {
	ctx := context.Background()
	service, mockLinkedItemRepo, mockPlaidService := newReconnectTestService()

	linkedItem := &domain.LinkedItem{
		ID:          "linked-item-123",
		UserID:      "user-456",
		ItemID:      "plaid-item-789",
		AccessToken: "decrypted-access-token",
		Status:      domain.LinkedItemStatusReplaced, // superseded by a re-link
	}
	mockLinkedItemRepo.On("GetByItemID", ctx, "plaid-item-789").Return(linkedItem, nil)

	_, err := service.CreateReconnectLinkToken(ctx, "user-456", "plaid-item-789")

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrLinkedItemNotRepairable)
	mockassert.NoCallTo(t, &mockPlaidService.Mock, "CreateUpdateModeLinkToken")
}

// TestCreateReconnectLinkToken_RevokedItem_ReturnsNotRepairable pins BLOCKING 1 (Issue #1609
// round 2): Plaid says a revoked Item cannot be restored, so the mint endpoint must refuse to
// hand out an update-mode token for one at all — never mind what RepairItem would later do.
func TestCreateReconnectLinkToken_RevokedItem_ReturnsNotRepairable(t *testing.T) {
	ctx := context.Background()
	service, mockLinkedItemRepo, mockPlaidService := newReconnectTestService()

	linkedItem := &domain.LinkedItem{
		ID:          "linked-item-123",
		UserID:      "user-456",
		ItemID:      "plaid-item-789",
		AccessToken: "decrypted-access-token",
		Status:      domain.LinkedItemStatusRevoked, // permanently unrecoverable per Plaid
	}
	mockLinkedItemRepo.On("GetByItemID", ctx, "plaid-item-789").Return(linkedItem, nil)

	_, err := service.CreateReconnectLinkToken(ctx, "user-456", "plaid-item-789")

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrLinkedItemNotRepairable)
	mockassert.NoCallTo(t, &mockPlaidService.Mock, "CreateUpdateModeLinkToken")
}

func TestCreateReconnectLinkToken_ItemNotFound(t *testing.T) {
	ctx := context.Background()
	service, mockLinkedItemRepo, mockPlaidService := newReconnectTestService()

	mockLinkedItemRepo.On("GetByItemID", ctx, "unknown-item").Return(nil, repository.ErrLinkedItemNotFound)

	_, err := service.CreateReconnectLinkToken(ctx, "user-456", "unknown-item")

	require.Error(t, err)
	assert.ErrorIs(t, err, repository.ErrLinkedItemNotFound)
	mockassert.NoCallTo(t, &mockPlaidService.Mock, "CreateUpdateModeLinkToken")
}

// ============================================================================
// RepairItem Tests (Issue #1609)
// ============================================================================

func TestRepairItem_Success_ClearsErrorStateAndReactivates(t *testing.T) {
	ctx := context.Background()
	service, mockLinkedItemRepo, _ := newReconnectTestService()

	errCode := "ITEM_LOGIN_REQUIRED"
	errMsg := "Credentials changed"
	lastErrorAt := time.Now().Add(-time.Hour)
	linkedItem := &domain.LinkedItem{
		ID:           "linked-item-123",
		UserID:       "user-456",
		ItemID:       "plaid-item-789",
		Status:       domain.LinkedItemStatusLoginRequired,
		ErrorCode:    &errCode,
		ErrorMessage: &errMsg,
		LastErrorAt:  &lastErrorAt,
	}
	mockLinkedItemRepo.On("GetByItemID", ctx, "plaid-item-789").Return(linkedItem, nil)
	mockLinkedItemRepo.On("RepairStatus", ctx, "linked-item-123", "user-456", repairableStatuses).
		Return(true, nil)

	err := service.RepairItem(ctx, "user-456", "plaid-item-789")

	require.NoError(t, err)
	mockLinkedItemRepo.AssertExpectations(t)
}

func TestRepairItem_WrongUser_ReturnsNotFound(t *testing.T) {
	ctx := context.Background()
	service, mockLinkedItemRepo, _ := newReconnectTestService()

	linkedItem := &domain.LinkedItem{
		ID:     "linked-item-123",
		UserID: "user-456",
		ItemID: "plaid-item-789",
		Status: domain.LinkedItemStatusLoginRequired,
	}
	mockLinkedItemRepo.On("GetByItemID", ctx, "plaid-item-789").Return(linkedItem, nil)

	err := service.RepairItem(ctx, "attacker-999", "plaid-item-789")

	require.Error(t, err)
	assert.ErrorIs(t, err, repository.ErrLinkedItemNotFound)
	mockassert.NoCallTo(t, &mockLinkedItemRepo.Mock, "RepairStatus")
}

func TestRepairItem_ActiveItem_ReturnsNotRepairable(t *testing.T) {
	ctx := context.Background()
	service, mockLinkedItemRepo, _ := newReconnectTestService()

	linkedItem := &domain.LinkedItem{
		ID:     "linked-item-123",
		UserID: "user-456",
		ItemID: "plaid-item-789",
		Status: domain.LinkedItemStatusActive,
	}
	mockLinkedItemRepo.On("GetByItemID", ctx, "plaid-item-789").Return(linkedItem, nil)

	err := service.RepairItem(ctx, "user-456", "plaid-item-789")

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrLinkedItemNotRepairable)
	mockassert.NoCallTo(t, &mockLinkedItemRepo.Mock, "RepairStatus")
}

// TestRepairItem_RevokedItem_ReturnsNotRepairable pins BLOCKING 1 (Issue #1609 round 2): Plaid
// says a revoked Item cannot be restored — update-mode repair must never mark it 'active'.
func TestRepairItem_RevokedItem_ReturnsNotRepairable(t *testing.T) {
	ctx := context.Background()
	service, mockLinkedItemRepo, _ := newReconnectTestService()

	linkedItem := &domain.LinkedItem{
		ID:     "linked-item-123",
		UserID: "user-456",
		ItemID: "plaid-item-789",
		Status: domain.LinkedItemStatusRevoked,
	}
	mockLinkedItemRepo.On("GetByItemID", ctx, "plaid-item-789").Return(linkedItem, nil)

	err := service.RepairItem(ctx, "user-456", "plaid-item-789")

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrLinkedItemNotRepairable)
	mockassert.NoCallTo(t, &mockLinkedItemRepo.Mock, "RepairStatus")
}

// TestRepairItem_ConcurrentStateChange_ReturnsNotRepairable pins BLOCKING 2 (Issue #1609 round
// 2): the pre-check in Go read a status that was still repairable, but RepairStatus's atomic
// UPDATE — re-checking status fresh, in the same statement — found the row no longer matched
// (e.g. a concurrent disconnect landed between the read and the write) and correctly affected
// zero rows. RepairItem must treat that the same as "not repairable", not as success.
func TestRepairItem_ConcurrentStateChange_ReturnsNotRepairable(t *testing.T) {
	ctx := context.Background()
	service, mockLinkedItemRepo, _ := newReconnectTestService()

	linkedItem := &domain.LinkedItem{
		ID:     "linked-item-123",
		UserID: "user-456",
		ItemID: "plaid-item-789",
		Status: domain.LinkedItemStatusLoginRequired, // stale read
	}
	mockLinkedItemRepo.On("GetByItemID", ctx, "plaid-item-789").Return(linkedItem, nil)
	mockLinkedItemRepo.On("RepairStatus", ctx, "linked-item-123", "user-456", repairableStatuses).
		Return(false, nil) // concurrent disconnect/re-link won the race

	err := service.RepairItem(ctx, "user-456", "plaid-item-789")

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrLinkedItemNotRepairable)
}

func TestRepairItem_RepairStatusError_IsWrapped(t *testing.T) {
	ctx := context.Background()
	service, mockLinkedItemRepo, _ := newReconnectTestService()

	linkedItem := &domain.LinkedItem{
		ID:     "linked-item-123",
		UserID: "user-456",
		ItemID: "plaid-item-789",
		Status: domain.LinkedItemStatusLoginRequired,
	}
	dbErr := errors.New("connection reset")
	mockLinkedItemRepo.On("GetByItemID", ctx, "plaid-item-789").Return(linkedItem, nil)
	mockLinkedItemRepo.On("RepairStatus", ctx, "linked-item-123", "user-456", repairableStatuses).
		Return(false, dbErr)

	err := service.RepairItem(ctx, "user-456", "plaid-item-789")

	require.Error(t, err)
	assert.ErrorIs(t, err, dbErr)
}

// ============================================================================
// HandleLoginRepaired Tests (LOGIN_REPAIRED webhook, Issue #1609)
// ============================================================================

func TestHandleLoginRepaired_ClearsErrorState(t *testing.T) {
	ctx := context.Background()
	service, mockLinkedItemRepo, _ := newReconnectTestService()

	errCode := "ITEM_LOGIN_REQUIRED"
	linkedItem := &domain.LinkedItem{
		ID:        "linked-item-123",
		UserID:    "user-456",
		ItemID:    "plaid-item-789",
		Status:    domain.LinkedItemStatusLoginRequired,
		ErrorCode: &errCode,
	}
	mockLinkedItemRepo.On(
		"RepairStatus", ctx, "linked-item-123", "", []domain.LinkedItemStatus{domain.LinkedItemStatusLoginRequired},
	).Return(true, nil)

	err := service.HandleLoginRepaired(ctx, linkedItem)

	require.NoError(t, err)
	mockLinkedItemRepo.AssertExpectations(t)
}

func TestHandleLoginRepaired_DisconnectedItem_IsNoOp(t *testing.T) {
	ctx := context.Background()
	service, mockLinkedItemRepo, _ := newReconnectTestService()

	linkedItem := &domain.LinkedItem{
		ID:     "linked-item-123",
		UserID: "user-456",
		ItemID: "plaid-item-789",
		Status: domain.LinkedItemStatusDisconnected,
	}
	// The atomic predicate (status = login_required) is what actually rejects this — not a Go
	// pre-check — so RepairStatus is still called, and reports no rows affected.
	mockLinkedItemRepo.On(
		"RepairStatus", ctx, "linked-item-123", "", []domain.LinkedItemStatus{domain.LinkedItemStatusLoginRequired},
	).Return(false, nil)

	// Then: no-op, NOT an error — a webhook the user did not act on must not resurrect a
	// deliberately disconnected item, and must not be retried/DLQ'd either.
	err := service.HandleLoginRepaired(ctx, linkedItem)

	require.NoError(t, err)
	mockLinkedItemRepo.AssertExpectations(t)
}

// TestHandleLoginRepaired_CurrentErrorStatus_IsNotClobbered pins should-fix 1: Plaid documents
// LOGIN_REPAIRED as meaning the Item specifically exited ITEM_LOGIN_REQUIRED, not "any error
// cleared". A delayed/duplicate webhook must not clear a CURRENT 'error' or 'pending_expiration'
// that arose after the login issue was already fixed — the predicate is scoped to
// login_required alone (not the wider repairableStatuses RepairItem uses).
func TestHandleLoginRepaired_CurrentErrorStatus_IsNotClobbered(t *testing.T) {
	ctx := context.Background()
	service, mockLinkedItemRepo, _ := newReconnectTestService()

	linkedItem := &domain.LinkedItem{
		ID:     "linked-item-123",
		UserID: "user-456",
		ItemID: "plaid-item-789",
		Status: domain.LinkedItemStatusError, // a NEW, different error since LOGIN_REPAIRED fired
	}
	mockLinkedItemRepo.On(
		"RepairStatus", ctx, "linked-item-123", "", []domain.LinkedItemStatus{domain.LinkedItemStatusLoginRequired},
	).Return(false, nil)

	err := service.HandleLoginRepaired(ctx, linkedItem)

	require.NoError(t, err)
	mockLinkedItemRepo.AssertExpectations(t)
}

func TestHandleLoginRepaired_RepairStatusError_IsWrapped(t *testing.T) {
	ctx := context.Background()
	service, mockLinkedItemRepo, _ := newReconnectTestService()

	linkedItem := &domain.LinkedItem{
		ID:     "linked-item-123",
		UserID: "user-456",
		ItemID: "plaid-item-789",
		Status: domain.LinkedItemStatusLoginRequired,
	}
	dbErr := errors.New("connection reset")
	mockLinkedItemRepo.On(
		"RepairStatus", ctx, "linked-item-123", "", []domain.LinkedItemStatus{domain.LinkedItemStatusLoginRequired},
	).Return(false, dbErr)

	err := service.HandleLoginRepaired(ctx, linkedItem)

	require.Error(t, err)
	assert.ErrorIs(t, err, dbErr)
}

func TestProcessWebhook_LoginRepaired_RoutesToHandleLoginRepaired(t *testing.T) {
	ctx := context.Background()
	service, mockLinkedItemRepo, _ := newReconnectTestService()

	linkedItem := &domain.LinkedItem{
		ID:     "linked-item-123",
		UserID: "user-456",
		ItemID: "plaid-item-789",
		Status: domain.LinkedItemStatusLoginRequired,
	}
	mockLinkedItemRepo.On("GetByItemID", ctx, "plaid-item-789").Return(linkedItem, nil)
	mockLinkedItemRepo.On(
		"RepairStatus", ctx, "linked-item-123", "", []domain.LinkedItemStatus{domain.LinkedItemStatusLoginRequired},
	).Return(true, nil)

	err := service.ProcessWebhook(ctx, "plaid-item-789", "LOGIN_REPAIRED", "", "")

	require.NoError(t, err)
	mockLinkedItemRepo.AssertExpectations(t)
}

// TestHandleLoginRequired_AlreadyInThatStatus_SkipsDuplicateNotification is the dedup hazard test
// (Issue #1609 P3-T2, hazard 1): a webhook and a sync producer discovering the same failure for
// the same item within seconds of each other must not each insert a notification. TransitionErrorStatus
// reports false when the item's FRESH status already equals the target — this test asserts that a
// false result short-circuits BEFORE any notification/email work happens, for exactly ONE call
// (the second producer's), proving the dedup contract at the service layer rather than merely
// trusting the repository's atomicity.
func TestHandleLoginRequired_AlreadyInThatStatus_SkipsDuplicateNotification(t *testing.T) {
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
		ID:     "linked-item-123",
		UserID: "user-456",
		ItemID: "plaid-item-789",
		Status: domain.LinkedItemStatusLoginRequired, // A first producer already transitioned this.
	}

	// The atomic transition reports false: the item's FRESH status already equals the target
	// (login_required), so this is treated as a duplicate signal from a second producer.
	mockLinkedItemRepo.On("TransitionErrorStatus", ctx, "linked-item-123", domain.LinkedItemStatusLoginRequired,
		"ITEM_LOGIN_REQUIRED", "Credentials changed, please re-authenticate", terminalLinkedItemStatuses).
		Return(false, nil)

	err := service.HandleLoginRequired(ctx, linkedItem, "ITEM_LOGIN_REQUIRED", "Credentials changed, please re-authenticate")

	require.NoError(t, err, "a duplicate signal is a no-op, not an error")
	mockLinkedItemRepo.AssertExpectations(t)
	// The whole point of the dedup: neither the notification nor the email step ever runs.
	mockassert.NoCallTo(t, &mockNotificationRepo.Mock, "Create")
	mockassert.NoCallTo(t, &mockUserRepo.Mock, "GetUserByID")
	mockassert.NoCallTo(t, &mockEmailService.Mock, "SendNotificationEmail")
}

// TestHandleError_TerminalStatus_SkipsNotification asserts the OTHER reason TransitionErrorStatus
// can report false: the item's current status is one of terminalLinkedItemStatuses (disconnected,
// replaced, revoked) — a background sync error must never resurrect/overwrite a state the user (or
// another deliberate flow) already produced. Distinct from the dedup case above: here the target
// status differs from the current one, but the transition is still refused because the source
// status is terminal.
func TestHandleError_TerminalStatus_SkipsNotification(t *testing.T) {
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
		ID:     "linked-item-123",
		UserID: "user-456",
		ItemID: "plaid-item-789",
		Status: domain.LinkedItemStatusDisconnected, // User deliberately disconnected this item.
	}

	mockLinkedItemRepo.On("TransitionErrorStatus", ctx, "linked-item-123", domain.LinkedItemStatusError,
		"ITEM_ERROR", "Connection error, please try again later", terminalLinkedItemStatuses).
		Return(false, nil)

	err := service.HandleError(ctx, linkedItem, "ITEM_ERROR", "Connection error, please try again later")

	require.NoError(t, err, "a stale error signal for a disconnected item is a no-op, not an error")
	mockLinkedItemRepo.AssertExpectations(t)
	mockassert.NoCallTo(t, &mockNotificationRepo.Mock, "Create")
	mockassert.NoCallTo(t, &mockUserRepo.Mock, "GetUserByID")
	mockassert.NoCallTo(t, &mockEmailService.Mock, "SendNotificationEmail")
}
