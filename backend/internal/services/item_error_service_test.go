package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/observability"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/maxnussbaum/BoostedBudget/internal/testsupport/mockassert"
	"github.com/prometheus/client_golang/prometheus/testutil"
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
	// Issue #1623: every failure assert ALSO stamps last_failure_asserted_at, via a second,
	// unconditional statement (see MarkFailureAsserted). Declared Maybe() here so tests that
	// never reach a Handle* method need not opt in; the stamp itself is asserted exactly, with
	// its real arguments, by TestHandleLoginRequired_StampsFailureAssertedEvenOnADuplicateSignal.
	mockLinkedItemRepo.On("MarkFailureAsserted", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	mockNotificationRepo := new(MockNotificationRepositoryForItemError)
	mockPlaidService := new(MockPlaidService)

	mockEmailService := new(MockEmailService)
	mockUserRepo := new(MockUserRepository)

	mockRepairNonceRepo := new(MockLinkedItemRepairNonceRepository)
	service := NewItemErrorService(
		mockLinkedItemRepo,
		mockNotificationRepo,
		mockUserRepo,
		mockPlaidService,
		mockEmailService,
		mockRepairNonceRepo,
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
	// Issue #1623: every failure assert ALSO stamps last_failure_asserted_at, via a second,
	// unconditional statement (see MarkFailureAsserted). Declared Maybe() here so tests that
	// never reach a Handle* method need not opt in; the stamp itself is asserted exactly, with
	// its real arguments, by TestHandleLoginRequired_StampsFailureAssertedEvenOnADuplicateSignal.
	mockLinkedItemRepo.On("MarkFailureAsserted", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	mockNotificationRepo := new(MockNotificationRepositoryForItemError)
	mockPlaidService := new(MockPlaidService)

	mockEmailService := new(MockEmailService)
	mockUserRepo := new(MockUserRepository)

	mockRepairNonceRepo := new(MockLinkedItemRepairNonceRepository)
	service := NewItemErrorService(
		mockLinkedItemRepo,
		mockNotificationRepo,
		mockUserRepo,
		mockPlaidService,
		mockEmailService,
		mockRepairNonceRepo,
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
	// Issue #1623: every failure assert ALSO stamps last_failure_asserted_at, via a second,
	// unconditional statement (see MarkFailureAsserted). Declared Maybe() here so tests that
	// never reach a Handle* method need not opt in; the stamp itself is asserted exactly, with
	// its real arguments, by TestHandleLoginRequired_StampsFailureAssertedEvenOnADuplicateSignal.
	mockLinkedItemRepo.On("MarkFailureAsserted", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	mockNotificationRepo := new(MockNotificationRepositoryForItemError)
	mockPlaidService := new(MockPlaidService)

	mockEmailService := new(MockEmailService)
	mockUserRepo := new(MockUserRepository)

	mockRepairNonceRepo := new(MockLinkedItemRepairNonceRepository)
	service := NewItemErrorService(
		mockLinkedItemRepo,
		mockNotificationRepo,
		mockUserRepo,
		mockPlaidService,
		mockEmailService,
		mockRepairNonceRepo,
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
	// Issue #1623: every failure assert ALSO stamps last_failure_asserted_at, via a second,
	// unconditional statement (see MarkFailureAsserted). Declared Maybe() here so tests that
	// never reach a Handle* method need not opt in; the stamp itself is asserted exactly, with
	// its real arguments, by TestHandleLoginRequired_StampsFailureAssertedEvenOnADuplicateSignal.
	mockLinkedItemRepo.On("MarkFailureAsserted", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	mockNotificationRepo := new(MockNotificationRepositoryForItemError)
	mockPlaidService := new(MockPlaidService)

	mockEmailService := new(MockEmailService)
	mockUserRepo := new(MockUserRepository)

	mockRepairNonceRepo := new(MockLinkedItemRepairNonceRepository)
	service := NewItemErrorService(
		mockLinkedItemRepo,
		mockNotificationRepo,
		mockUserRepo,
		mockPlaidService,
		mockEmailService,
		mockRepairNonceRepo,
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
	// Issue #1623: every failure assert ALSO stamps last_failure_asserted_at, via a second,
	// unconditional statement (see MarkFailureAsserted). Declared Maybe() here so tests that
	// never reach a Handle* method need not opt in; the stamp itself is asserted exactly, with
	// its real arguments, by TestHandleLoginRequired_StampsFailureAssertedEvenOnADuplicateSignal.
	mockLinkedItemRepo.On("MarkFailureAsserted", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	mockNotificationRepo := new(MockNotificationRepositoryForItemError)
	mockPlaidService := new(MockPlaidService)

	mockEmailService := new(MockEmailService)
	mockUserRepo := new(MockUserRepository)

	mockRepairNonceRepo := new(MockLinkedItemRepairNonceRepository)
	service := NewItemErrorService(
		mockLinkedItemRepo,
		mockNotificationRepo,
		mockUserRepo,
		mockPlaidService,
		mockEmailService,
		mockRepairNonceRepo,
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
	// Issue #1623: every failure assert ALSO stamps last_failure_asserted_at, via a second,
	// unconditional statement (see MarkFailureAsserted). Declared Maybe() here so tests that
	// never reach a Handle* method need not opt in; the stamp itself is asserted exactly, with
	// its real arguments, by TestHandleLoginRequired_StampsFailureAssertedEvenOnADuplicateSignal.
	mockLinkedItemRepo.On("MarkFailureAsserted", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	mockNotificationRepo := new(MockNotificationRepositoryForItemError)
	mockPlaidService := new(MockPlaidService)
	mockEmailService := new(MockEmailService)
	mockUserRepo := new(MockUserRepository)

	mockRepairNonceRepo := new(MockLinkedItemRepairNonceRepository)
	service := NewItemErrorService(
		mockLinkedItemRepo,
		mockNotificationRepo,
		mockUserRepo,
		mockPlaidService,
		mockEmailService,
		mockRepairNonceRepo,
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
	// Issue #1623: every failure assert ALSO stamps last_failure_asserted_at, via a second,
	// unconditional statement (see MarkFailureAsserted). Declared Maybe() here so tests that
	// never reach a Handle* method need not opt in; the stamp itself is asserted exactly, with
	// its real arguments, by TestHandleLoginRequired_StampsFailureAssertedEvenOnADuplicateSignal.
	mockLinkedItemRepo.On("MarkFailureAsserted", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	mockNotificationRepo := new(MockNotificationRepositoryForItemError)
	mockPlaidService := new(MockPlaidService)

	mockEmailService := new(MockEmailService)
	mockUserRepo := new(MockUserRepository)

	mockRepairNonceRepo := new(MockLinkedItemRepairNonceRepository)
	service := NewItemErrorService(
		mockLinkedItemRepo,
		mockNotificationRepo,
		mockUserRepo,
		mockPlaidService,
		mockEmailService,
		mockRepairNonceRepo,
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
	// Issue #1623: every failure assert ALSO stamps last_failure_asserted_at, via a second,
	// unconditional statement (see MarkFailureAsserted). Declared Maybe() here so tests that
	// never reach a Handle* method need not opt in; the stamp itself is asserted exactly, with
	// its real arguments, by TestHandleLoginRequired_StampsFailureAssertedEvenOnADuplicateSignal.
	mockLinkedItemRepo.On("MarkFailureAsserted", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	mockNotificationRepo := new(MockNotificationRepositoryForItemError)
	mockPlaidService := new(MockPlaidService)

	mockEmailService := new(MockEmailService)
	mockUserRepo := new(MockUserRepository)

	mockRepairNonceRepo := new(MockLinkedItemRepairNonceRepository)
	service := NewItemErrorService(
		mockLinkedItemRepo,
		mockNotificationRepo,
		mockUserRepo,
		mockPlaidService,
		mockEmailService,
		mockRepairNonceRepo,
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
	// Issue #1623: every failure assert ALSO stamps last_failure_asserted_at, via a second,
	// unconditional statement (see MarkFailureAsserted). Declared Maybe() here so tests that
	// never reach a Handle* method need not opt in; the stamp itself is asserted exactly, with
	// its real arguments, by TestHandleLoginRequired_StampsFailureAssertedEvenOnADuplicateSignal.
	mockLinkedItemRepo.On("MarkFailureAsserted", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	mockNotificationRepo := new(MockNotificationRepositoryForItemError)
	mockPlaidService := new(MockPlaidService)

	mockEmailService := new(MockEmailService)
	mockUserRepo := new(MockUserRepository)

	mockRepairNonceRepo := new(MockLinkedItemRepairNonceRepository)
	service := NewItemErrorService(
		mockLinkedItemRepo,
		mockNotificationRepo,
		mockUserRepo,
		mockPlaidService,
		mockEmailService,
		mockRepairNonceRepo,
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
	// Issue #1623: every failure assert ALSO stamps last_failure_asserted_at, via a second,
	// unconditional statement (see MarkFailureAsserted). Declared Maybe() here so tests that
	// never reach a Handle* method need not opt in; the stamp itself is asserted exactly, with
	// its real arguments, by TestHandleLoginRequired_StampsFailureAssertedEvenOnADuplicateSignal.
	mockLinkedItemRepo.On("MarkFailureAsserted", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	mockNotificationRepo := new(MockNotificationRepositoryForItemError)
	mockPlaidService := new(MockPlaidService)
	mockEmailService := new(MockEmailService)
	mockUserRepo := new(MockUserRepository)

	mockRepairNonceRepo := new(MockLinkedItemRepairNonceRepository)
	service := NewItemErrorService(
		mockLinkedItemRepo,
		mockNotificationRepo,
		mockUserRepo,
		mockPlaidService,
		mockEmailService,
		mockRepairNonceRepo,
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
	// Issue #1623: every failure assert ALSO stamps last_failure_asserted_at, via a second,
	// unconditional statement (see MarkFailureAsserted). Declared Maybe() here so tests that
	// never reach a Handle* method need not opt in; the stamp itself is asserted exactly, with
	// its real arguments, by TestHandleLoginRequired_StampsFailureAssertedEvenOnADuplicateSignal.
	mockLinkedItemRepo.On("MarkFailureAsserted", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	mockNotificationRepo := new(MockNotificationRepositoryForItemError)
	mockPlaidService := new(MockPlaidService)

	mockEmailService := new(MockEmailService)
	mockUserRepo := new(MockUserRepository)

	mockRepairNonceRepo := new(MockLinkedItemRepairNonceRepository)
	service := NewItemErrorService(
		mockLinkedItemRepo,
		mockNotificationRepo,
		mockUserRepo,
		mockPlaidService,
		mockEmailService,
		mockRepairNonceRepo,
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
	// Issue #1623: every failure assert ALSO stamps last_failure_asserted_at, via a second,
	// unconditional statement (see MarkFailureAsserted). Declared Maybe() here so tests that
	// never reach a Handle* method need not opt in; the stamp itself is asserted exactly, with
	// its real arguments, by TestHandleLoginRequired_StampsFailureAssertedEvenOnADuplicateSignal.
	mockLinkedItemRepo.On("MarkFailureAsserted", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	mockNotificationRepo := new(MockNotificationRepositoryForItemError)
	mockPlaidService := new(MockPlaidService)

	mockEmailService := new(MockEmailService)
	mockUserRepo := new(MockUserRepository)

	mockRepairNonceRepo := new(MockLinkedItemRepairNonceRepository)
	service := NewItemErrorService(
		mockLinkedItemRepo,
		mockNotificationRepo,
		mockUserRepo,
		mockPlaidService,
		mockEmailService,
		mockRepairNonceRepo,
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
	// Issue #1623: every failure assert ALSO stamps last_failure_asserted_at, via a second,
	// unconditional statement (see MarkFailureAsserted). Declared Maybe() here so tests that
	// never reach a Handle* method need not opt in; the stamp itself is asserted exactly, with
	// its real arguments, by TestHandleLoginRequired_StampsFailureAssertedEvenOnADuplicateSignal.
	mockLinkedItemRepo.On("MarkFailureAsserted", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	mockNotificationRepo := new(MockNotificationRepositoryForItemError)
	mockPlaidService := new(MockPlaidService)

	mockEmailService := new(MockEmailService)
	mockUserRepo := new(MockUserRepository)

	mockRepairNonceRepo := new(MockLinkedItemRepairNonceRepository)
	service := NewItemErrorService(
		mockLinkedItemRepo,
		mockNotificationRepo,
		mockUserRepo,
		mockPlaidService,
		mockEmailService,
		mockRepairNonceRepo,
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
	// Issue #1623: every failure assert ALSO stamps last_failure_asserted_at, via a second,
	// unconditional statement (see MarkFailureAsserted). Declared Maybe() here so tests that
	// never reach a Handle* method need not opt in; the stamp itself is asserted exactly, with
	// its real arguments, by TestHandleLoginRequired_StampsFailureAssertedEvenOnADuplicateSignal.
	mockLinkedItemRepo.On("MarkFailureAsserted", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	mockNotificationRepo := new(MockNotificationRepositoryForItemError)
	mockPlaidService := new(MockPlaidService)

	mockEmailService := new(MockEmailService)
	mockUserRepo := new(MockUserRepository)

	mockRepairNonceRepo := new(MockLinkedItemRepairNonceRepository)
	service := NewItemErrorService(
		mockLinkedItemRepo,
		mockNotificationRepo,
		mockUserRepo,
		mockPlaidService,
		mockEmailService,
		mockRepairNonceRepo,
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
	// Issue #1623: every failure assert ALSO stamps last_failure_asserted_at, via a second,
	// unconditional statement (see MarkFailureAsserted). Declared Maybe() here so tests that
	// never reach a Handle* method need not opt in; the stamp itself is asserted exactly, with
	// its real arguments, by TestHandleLoginRequired_StampsFailureAssertedEvenOnADuplicateSignal.
	mockLinkedItemRepo.On("MarkFailureAsserted", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	mockNotificationRepo := new(MockNotificationRepositoryForItemError)
	mockPlaidService := new(MockPlaidService)

	mockEmailService := new(MockEmailService)
	mockUserRepo := new(MockUserRepository)

	mockRepairNonceRepo := new(MockLinkedItemRepairNonceRepository)
	service := NewItemErrorService(
		mockLinkedItemRepo,
		mockNotificationRepo,
		mockUserRepo,
		mockPlaidService,
		mockEmailService,
		mockRepairNonceRepo,
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
	// Issue #1623: every failure assert ALSO stamps last_failure_asserted_at, via a second,
	// unconditional statement (see MarkFailureAsserted). Declared Maybe() here so tests that
	// never reach a Handle* method need not opt in; the stamp itself is asserted exactly, with
	// its real arguments, by TestHandleLoginRequired_StampsFailureAssertedEvenOnADuplicateSignal.
	mockLinkedItemRepo.On("MarkFailureAsserted", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	mockNotificationRepo := new(MockNotificationRepositoryForItemError)
	mockPlaidService := new(MockPlaidService)

	mockEmailService := new(MockEmailService)
	mockUserRepo := new(MockUserRepository)

	mockRepairNonceRepo := new(MockLinkedItemRepairNonceRepository)
	service := NewItemErrorService(
		mockLinkedItemRepo,
		mockNotificationRepo,
		mockUserRepo,
		mockPlaidService,
		mockEmailService,
		mockRepairNonceRepo,
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
	// Issue #1623: every failure assert ALSO stamps last_failure_asserted_at, via a second,
	// unconditional statement (see MarkFailureAsserted). Declared Maybe() here so tests that
	// never reach a Handle* method need not opt in; the stamp itself is asserted exactly, with
	// its real arguments, by TestHandleLoginRequired_StampsFailureAssertedEvenOnADuplicateSignal.
	mockLinkedItemRepo.On("MarkFailureAsserted", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	mockNotificationRepo := new(MockNotificationRepositoryForItemError)
	mockPlaidService := new(MockPlaidService)

	mockEmailService := new(MockEmailService)
	mockUserRepo := new(MockUserRepository)

	mockRepairNonceRepo := new(MockLinkedItemRepairNonceRepository)
	service := NewItemErrorService(
		mockLinkedItemRepo,
		mockNotificationRepo,
		mockUserRepo,
		mockPlaidService,
		mockEmailService,
		mockRepairNonceRepo,
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
	// Issue #1623: every failure assert ALSO stamps last_failure_asserted_at, via a second,
	// unconditional statement (see MarkFailureAsserted). Declared Maybe() here so tests that
	// never reach a Handle* method need not opt in; the stamp itself is asserted exactly, with
	// its real arguments, by TestHandleLoginRequired_StampsFailureAssertedEvenOnADuplicateSignal.
	mockLinkedItemRepo.On("MarkFailureAsserted", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	mockNotificationRepo := new(MockNotificationRepositoryForItemError)
	mockPlaidService := new(MockPlaidService)

	mockEmailService := new(MockEmailService)
	mockUserRepo := new(MockUserRepository)

	mockRepairNonceRepo := new(MockLinkedItemRepairNonceRepository)
	service := NewItemErrorService(
		mockLinkedItemRepo,
		mockNotificationRepo,
		mockUserRepo,
		mockPlaidService,
		mockEmailService,
		mockRepairNonceRepo,
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

// TestProcessWebhook_LoginRequired tests routing to HandleLoginRequired. Issue #1621: the
// production shape is webhook_code "ERROR" carrying error_code "ITEM_LOGIN_REQUIRED" — there is
// no Plaid webhook_code named "ITEM_LOGIN_REQUIRED" (that used to be this test's errorType, which
// exercised a case that could never match a real webhook).
func TestProcessWebhook_LoginRequired(t *testing.T) {
	ctx := context.Background()
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	// Issue #1623: every failure assert ALSO stamps last_failure_asserted_at, via a second,
	// unconditional statement (see MarkFailureAsserted). Declared Maybe() here so tests that
	// never reach a Handle* method need not opt in; the stamp itself is asserted exactly, with
	// its real arguments, by TestHandleLoginRequired_StampsFailureAssertedEvenOnADuplicateSignal.
	mockLinkedItemRepo.On("MarkFailureAsserted", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	mockNotificationRepo := new(MockNotificationRepositoryForItemError)
	mockPlaidService := new(MockPlaidService)

	mockEmailService := new(MockEmailService)
	mockUserRepo := new(MockUserRepository)

	mockRepairNonceRepo := new(MockLinkedItemRepairNonceRepository)
	service := NewItemErrorService(
		mockLinkedItemRepo,
		mockNotificationRepo,
		mockUserRepo,
		mockPlaidService,
		mockEmailService,
		mockRepairNonceRepo,
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

	// Mock: All operations for HandleLoginRequired. The status argument is pinned to
	// LinkedItemStatusLoginRequired (not mock.Anything) specifically so this test fails if
	// routing regresses to HandleError's LinkedItemStatusError instead (Issue #1621).
	mockLinkedItemRepo.On("TransitionErrorStatus", ctx, mock.Anything, domain.LinkedItemStatusLoginRequired, mock.Anything, mock.Anything, mock.Anything).Return(true, nil)
	mockNotificationRepo.On("Create", ctx, mock.Anything).Return(nil)
	mockUserRepo.On("GetUserByID", ctx, "user-456").
		Return(&domain.User{ID: "user-456", Email: "real@user.com"}, nil)
	mockEmailService.On("SendNotificationEmail", ctx, mock.Anything, mock.Anything).Return(nil)

	// When: ProcessWebhook is called with the real production shape — webhook_code "ERROR",
	// error_code "ITEM_LOGIN_REQUIRED"
	err := service.ProcessWebhook(ctx, "plaid-item-789", "ERROR", "ITEM_LOGIN_REQUIRED", "Credentials changed", testWebhookReceivedAt)

	// Then: Routes to HandleLoginRequired and succeeds
	require.NoError(t, err)
	mockLinkedItemRepo.AssertExpectations(t)

	mockassert.NoCallTo(t, &mockPlaidService.Mock, "CreateUpdateModeLinkToken")
	mockNotificationRepo.AssertExpectations(t)
	mockEmailService.AssertExpectations(t)
}

// TestProcessWebhook_InvalidAccessToken_RoutesToHandleLoginRequired tests routing parity with the
// sync path (plaid_permanent_error_notify.go): INVALID_ACCESS_TOKEN and ITEM_LOGIN_REQUIRED mean
// the same thing — only the user can fix this by re-authenticating — so the webhook path must
// route both through HandleLoginRequired the same way the sync path does, or the two producers of
// this signal diverge (Issue #1621).
func TestProcessWebhook_InvalidAccessToken_RoutesToHandleLoginRequired(t *testing.T) {
	ctx := context.Background()
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	// Issue #1623: every failure assert ALSO stamps last_failure_asserted_at, via a second,
	// unconditional statement (see MarkFailureAsserted). Declared Maybe() here so tests that
	// never reach a Handle* method need not opt in; the stamp itself is asserted exactly, with
	// its real arguments, by TestHandleLoginRequired_StampsFailureAssertedEvenOnADuplicateSignal.
	mockLinkedItemRepo.On("MarkFailureAsserted", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	mockNotificationRepo := new(MockNotificationRepositoryForItemError)
	mockPlaidService := new(MockPlaidService)

	mockEmailService := new(MockEmailService)
	mockUserRepo := new(MockUserRepository)

	mockRepairNonceRepo := new(MockLinkedItemRepairNonceRepository)
	service := NewItemErrorService(
		mockLinkedItemRepo,
		mockNotificationRepo,
		mockUserRepo,
		mockPlaidService,
		mockEmailService,
		mockRepairNonceRepo,
		nil,
	)

	linkedItem := &domain.LinkedItem{
		ID:              "linked-item-123",
		UserID:          "user-456",
		ItemID:          "plaid-item-789",
		InstitutionName: "Test Bank",
		Status:          "active",
	}
	mockLinkedItemRepo.On("GetByItemID", ctx, "plaid-item-789").Return(linkedItem, nil)
	mockLinkedItemRepo.On("TransitionErrorStatus", ctx, mock.Anything, domain.LinkedItemStatusLoginRequired, mock.Anything, mock.Anything, mock.Anything).Return(true, nil)
	mockNotificationRepo.On("Create", ctx, mock.Anything).Return(nil)
	mockUserRepo.On("GetUserByID", ctx, "user-456").
		Return(&domain.User{ID: "user-456", Email: "real@user.com"}, nil)
	mockEmailService.On("SendNotificationEmail", ctx, mock.Anything, mock.Anything).Return(nil)

	err := service.ProcessWebhook(ctx, "plaid-item-789", "ERROR", "INVALID_ACCESS_TOKEN", "Access token invalid", testWebhookReceivedAt)

	require.NoError(t, err)
	mockLinkedItemRepo.AssertExpectations(t)
	mockNotificationRepo.AssertExpectations(t)
	mockEmailService.AssertExpectations(t)
}

// TestProcessWebhook_PendingDisconnect_RoutesToHandlePendingExpiration tests that PENDING_DISCONNECT
// — live in production, absent from the vendored Plaid spec (Issue #1621) — gets the same
// treatment as PENDING_EXPIRATION rather than falling into the unknown-code default.
func TestProcessWebhook_PendingDisconnect_RoutesToHandlePendingExpiration(t *testing.T) {
	ctx := context.Background()
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	// Issue #1623: every failure assert ALSO stamps last_failure_asserted_at, via a second,
	// unconditional statement (see MarkFailureAsserted). Declared Maybe() here so tests that
	// never reach a Handle* method need not opt in; the stamp itself is asserted exactly, with
	// its real arguments, by TestHandleLoginRequired_StampsFailureAssertedEvenOnADuplicateSignal.
	mockLinkedItemRepo.On("MarkFailureAsserted", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	mockNotificationRepo := new(MockNotificationRepositoryForItemError)
	mockPlaidService := new(MockPlaidService)

	mockEmailService := new(MockEmailService)
	mockUserRepo := new(MockUserRepository)

	mockRepairNonceRepo := new(MockLinkedItemRepairNonceRepository)
	service := NewItemErrorService(
		mockLinkedItemRepo,
		mockNotificationRepo,
		mockUserRepo,
		mockPlaidService,
		mockEmailService,
		mockRepairNonceRepo,
		nil,
	)

	linkedItem := &domain.LinkedItem{
		ID:              "linked-item-123",
		UserID:          "user-456",
		ItemID:          "plaid-item-789",
		InstitutionName: "Test Bank",
		Status:          "active",
	}
	mockLinkedItemRepo.On("GetByItemID", ctx, "plaid-item-789").Return(linkedItem, nil)
	mockLinkedItemRepo.On("TransitionErrorStatus", ctx, mock.Anything, domain.LinkedItemStatusPendingExpiration, mock.Anything, mock.Anything, mock.Anything).Return(true, nil)
	mockNotificationRepo.On("Create", ctx, mock.Anything).Return(nil)
	mockUserRepo.On("GetUserByID", ctx, "user-456").
		Return(&domain.User{ID: "user-456", Email: "real@user.com"}, nil)
	mockEmailService.On("SendNotificationEmail", ctx, mock.Anything, mock.Anything).Return(nil)

	err := service.ProcessWebhook(ctx, "plaid-item-789", "PENDING_DISCONNECT", "", "", testWebhookReceivedAt)

	require.NoError(t, err)
	mockLinkedItemRepo.AssertExpectations(t)
	mockNotificationRepo.AssertExpectations(t)
	mockEmailService.AssertExpectations(t)
}

// TestProcessWebhook_Error tests routing to HandleError
func TestProcessWebhook_Error(t *testing.T) {
	ctx := context.Background()
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	// Issue #1623: every failure assert ALSO stamps last_failure_asserted_at, via a second,
	// unconditional statement (see MarkFailureAsserted). Declared Maybe() here so tests that
	// never reach a Handle* method need not opt in; the stamp itself is asserted exactly, with
	// its real arguments, by TestHandleLoginRequired_StampsFailureAssertedEvenOnADuplicateSignal.
	mockLinkedItemRepo.On("MarkFailureAsserted", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	mockNotificationRepo := new(MockNotificationRepositoryForItemError)
	mockPlaidService := new(MockPlaidService)

	mockEmailService := new(MockEmailService)
	mockUserRepo := new(MockUserRepository)

	mockRepairNonceRepo := new(MockLinkedItemRepairNonceRepository)
	service := NewItemErrorService(
		mockLinkedItemRepo,
		mockNotificationRepo,
		mockUserRepo,
		mockPlaidService,
		mockEmailService,
		mockRepairNonceRepo,
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
	err := service.ProcessWebhook(ctx, "plaid-item-789", "ERROR", "ITEM_ERROR", "Connection error", testWebhookReceivedAt)

	// Then: Routes to HandleError and succeeds
	require.NoError(t, err)
	mockLinkedItemRepo.AssertExpectations(t)
	mockNotificationRepo.AssertExpectations(t)
	mockEmailService.AssertExpectations(t)
	// HandleError does NOT call encryption or plaid service

	mockassert.NoCallTo(t, &mockPlaidService.Mock, "CreateUpdateModeLinkToken")
	mockassert.NoCallTo(t, &mockRepairNonceRepo.Mock, "Mint")
}

// TestProcessWebhook_PendingExpiration tests routing to HandlePendingExpiration
func TestProcessWebhook_PendingExpiration(t *testing.T) {
	ctx := context.Background()
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	// Issue #1623: every failure assert ALSO stamps last_failure_asserted_at, via a second,
	// unconditional statement (see MarkFailureAsserted). Declared Maybe() here so tests that
	// never reach a Handle* method need not opt in; the stamp itself is asserted exactly, with
	// its real arguments, by TestHandleLoginRequired_StampsFailureAssertedEvenOnADuplicateSignal.
	mockLinkedItemRepo.On("MarkFailureAsserted", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	mockNotificationRepo := new(MockNotificationRepositoryForItemError)
	mockPlaidService := new(MockPlaidService)

	mockEmailService := new(MockEmailService)
	mockUserRepo := new(MockUserRepository)

	mockRepairNonceRepo := new(MockLinkedItemRepairNonceRepository)
	service := NewItemErrorService(
		mockLinkedItemRepo,
		mockNotificationRepo,
		mockUserRepo,
		mockPlaidService,
		mockEmailService,
		mockRepairNonceRepo,
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
	err := service.ProcessWebhook(ctx, "plaid-item-789", "PENDING_EXPIRATION", "ITEM_PENDING_EXPIRATION", "Access expiring soon", testWebhookReceivedAt)

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
	// Issue #1623: every failure assert ALSO stamps last_failure_asserted_at, via a second,
	// unconditional statement (see MarkFailureAsserted). Declared Maybe() here so tests that
	// never reach a Handle* method need not opt in; the stamp itself is asserted exactly, with
	// its real arguments, by TestHandleLoginRequired_StampsFailureAssertedEvenOnADuplicateSignal.
	mockLinkedItemRepo.On("MarkFailureAsserted", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	mockNotificationRepo := new(MockNotificationRepositoryForItemError)
	mockPlaidService := new(MockPlaidService)

	mockEmailService := new(MockEmailService)
	mockUserRepo := new(MockUserRepository)

	mockRepairNonceRepo := new(MockLinkedItemRepairNonceRepository)
	service := NewItemErrorService(
		mockLinkedItemRepo,
		mockNotificationRepo,
		mockUserRepo,
		mockPlaidService,
		mockEmailService,
		mockRepairNonceRepo,
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
	err := service.ProcessWebhook(ctx, "plaid-item-789", "USER_PERMISSION_REVOKED", "ITEM_PERMISSION_REVOKED", "User revoked access", testWebhookReceivedAt)

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
	// Issue #1623: every failure assert ALSO stamps last_failure_asserted_at, via a second,
	// unconditional statement (see MarkFailureAsserted). Declared Maybe() here so tests that
	// never reach a Handle* method need not opt in; the stamp itself is asserted exactly, with
	// its real arguments, by TestHandleLoginRequired_StampsFailureAssertedEvenOnADuplicateSignal.
	mockLinkedItemRepo.On("MarkFailureAsserted", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	mockNotificationRepo := new(MockNotificationRepositoryForItemError)
	mockPlaidService := new(MockPlaidService)

	mockEmailService := new(MockEmailService)
	mockUserRepo := new(MockUserRepository)

	mockRepairNonceRepo := new(MockLinkedItemRepairNonceRepository)
	service := NewItemErrorService(
		mockLinkedItemRepo,
		mockNotificationRepo,
		mockUserRepo,
		mockPlaidService,
		mockEmailService,
		mockRepairNonceRepo,
		nil,
	)

	// Mock: LinkedItem not found
	mockLinkedItemRepo.On("GetByItemID", ctx, "unknown-item-id").
		Return(nil, repository.ErrLinkedItemNotFound)

	// When: ProcessWebhook is called with unknown itemID
	err := service.ProcessWebhook(ctx, "unknown-item-id", "ITEM_LOGIN_REQUIRED", "ITEM_LOGIN_REQUIRED", "Test error", testWebhookReceivedAt)

	// Then: Returns error (critical failure - cannot process webhook without LinkedItem)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "linked item not found")
	mockLinkedItemRepo.AssertExpectations(t)
	// No handlers should be called if LinkedItem not found

	mockassert.NoCallTo(t, &mockPlaidService.Mock, "CreateUpdateModeLinkToken")
	mockassert.NoCallTo(t, &mockNotificationRepo.Mock, "Create")
	mockassert.NoCallTo(t, &mockEmailService.Mock, "SendNotificationEmail")
}

// TestProcessWebhook_ItemError_UnknownErrorType tests handling of an unrecognized ITEM
// webhook_code. Issue #1621: this used to return an error, which sent the webhook to
// retry/DLQ forever. It now logs and returns nil — mirroring webhook_processing.go:215-220's
// handling of an unknown webhook TYPE one level up — and increments
// observability.UnknownItemWebhookCodeTotal so a future unhandled code stays visible instead of
// being silently swallowed by the safer default.
func TestProcessWebhook_ItemError_UnknownErrorType(t *testing.T) {
	ctx := context.Background()
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	// Issue #1623: every failure assert ALSO stamps last_failure_asserted_at, via a second,
	// unconditional statement (see MarkFailureAsserted). Declared Maybe() here so tests that
	// never reach a Handle* method need not opt in; the stamp itself is asserted exactly, with
	// its real arguments, by TestHandleLoginRequired_StampsFailureAssertedEvenOnADuplicateSignal.
	mockLinkedItemRepo.On("MarkFailureAsserted", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	mockNotificationRepo := new(MockNotificationRepositoryForItemError)
	mockPlaidService := new(MockPlaidService)

	mockEmailService := new(MockEmailService)
	mockUserRepo := new(MockUserRepository)

	mockRepairNonceRepo := new(MockLinkedItemRepairNonceRepository)
	service := NewItemErrorService(
		mockLinkedItemRepo,
		mockNotificationRepo,
		mockUserRepo,
		mockPlaidService,
		mockEmailService,
		mockRepairNonceRepo,
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

	const unknownCode = "SOME_FUTURE_WEBHOOK_CODE_1621_TEST"
	before := testutil.ToFloat64(observability.UnknownItemWebhookCodeTotal.WithLabelValues(unknownCode))

	// When: ProcessWebhook is called with an unrecognized webhook_code
	err := service.ProcessWebhook(ctx, "plaid-item-789", unknownCode, "UNKNOWN_CODE", "Test error", testWebhookReceivedAt)

	// Then: Logs and returns nil (does not error, does not retry/DLQ) but is counted
	require.NoError(t, err)
	mockLinkedItemRepo.AssertExpectations(t)
	assert.Equal(t, before+1, testutil.ToFloat64(observability.UnknownItemWebhookCodeTotal.WithLabelValues(unknownCode)))
	// No handlers should be called for an unrecognized webhook_code

	mockassert.NoCallTo(t, &mockLinkedItemRepo.Mock, "TransitionErrorStatus")
	mockassert.NoCallTo(t, &mockPlaidService.Mock, "CreateUpdateModeLinkToken")
	mockassert.NoCallTo(t, &mockNotificationRepo.Mock, "Create")
	mockassert.NoCallTo(t, &mockEmailService.Mock, "SendNotificationEmail")
}

// TestProcessWebhook_KnownUnhandledCodes_LogAndReturnNilWithoutErroring covers the three ITEM
// webhook_code values the vendored Plaid spec documents but this service does not yet act on
// (Issue #1621): USER_ACCOUNT_REVOKED, NEW_ACCOUNTS_AVAILABLE (no notification type/handler
// exists for either yet), and WEBHOOK_UPDATE_ACKNOWLEDGED (self-inflicted — plaid_service.go sets
// a webhook URL, which the spec says triggers this webhook). All three must fall into the safe
// default: logged, counted, and returned as nil rather than erroring into the DLQ.
func TestProcessWebhook_KnownUnhandledCodes_LogAndReturnNilWithoutErroring(t *testing.T) {
	for _, code := range []string{"USER_ACCOUNT_REVOKED", "NEW_ACCOUNTS_AVAILABLE", "WEBHOOK_UPDATE_ACKNOWLEDGED"} {
		t.Run(code, func(t *testing.T) {
			ctx := context.Background()
			mockLinkedItemRepo := new(MockLinkedItemRepository)
			// Issue #1623: every failure assert ALSO stamps last_failure_asserted_at, via a second,
			// unconditional statement (see MarkFailureAsserted). Declared Maybe() here so tests that
			// never reach a Handle* method need not opt in; the stamp itself is asserted exactly, with
			// its real arguments, by TestHandleLoginRequired_StampsFailureAssertedEvenOnADuplicateSignal.
			mockLinkedItemRepo.On("MarkFailureAsserted", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
			mockNotificationRepo := new(MockNotificationRepositoryForItemError)
			mockPlaidService := new(MockPlaidService)
			mockEmailService := new(MockEmailService)
			mockUserRepo := new(MockUserRepository)

			mockRepairNonceRepo := new(MockLinkedItemRepairNonceRepository)
			service := NewItemErrorService(
				mockLinkedItemRepo,
				mockNotificationRepo,
				mockUserRepo,
				mockPlaidService,
				mockEmailService,
				mockRepairNonceRepo,
				nil,
			)

			linkedItem := &domain.LinkedItem{
				ID:     "linked-item-123",
				UserID: "user-456",
				ItemID: "plaid-item-789",
				Status: "active",
			}
			mockLinkedItemRepo.On("GetByItemID", ctx, "plaid-item-789").Return(linkedItem, nil)

			before := testutil.ToFloat64(observability.UnknownItemWebhookCodeTotal.WithLabelValues(code))

			err := service.ProcessWebhook(ctx, "plaid-item-789", code, "", "", testWebhookReceivedAt)

			require.NoError(t, err)
			mockLinkedItemRepo.AssertExpectations(t)
			assert.Equal(t, before+1, testutil.ToFloat64(observability.UnknownItemWebhookCodeTotal.WithLabelValues(code)))
			mockassert.NoCallTo(t, &mockLinkedItemRepo.Mock, "TransitionErrorStatus")
			mockassert.NoCallTo(t, &mockNotificationRepo.Mock, "Create")
			mockassert.NoCallTo(t, &mockEmailService.Mock, "SendNotificationEmail")
		})
	}
}

// ============================================================================
// User Lookup Failure Tests (Graceful Degradation)
// ============================================================================

// TestHandleLoginRequired_UserLookupFailureNonBlocking tests graceful degradation when user lookup fails
func TestHandleLoginRequired_UserLookupFailureNonBlocking(t *testing.T) {
	ctx := context.Background()
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	// Issue #1623: every failure assert ALSO stamps last_failure_asserted_at, via a second,
	// unconditional statement (see MarkFailureAsserted). Declared Maybe() here so tests that
	// never reach a Handle* method need not opt in; the stamp itself is asserted exactly, with
	// its real arguments, by TestHandleLoginRequired_StampsFailureAssertedEvenOnADuplicateSignal.
	mockLinkedItemRepo.On("MarkFailureAsserted", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	mockNotificationRepo := new(MockNotificationRepositoryForItemError)
	mockPlaidService := new(MockPlaidService)

	mockEmailService := new(MockEmailService)
	mockUserRepo := new(MockUserRepository)

	mockRepairNonceRepo := new(MockLinkedItemRepairNonceRepository)
	service := NewItemErrorService(
		mockLinkedItemRepo,
		mockNotificationRepo,
		mockUserRepo,
		mockPlaidService,
		mockEmailService,
		mockRepairNonceRepo,
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
	// Issue #1623: every failure assert ALSO stamps last_failure_asserted_at, via a second,
	// unconditional statement (see MarkFailureAsserted). Declared Maybe() here so tests that
	// never reach a Handle* method need not opt in; the stamp itself is asserted exactly, with
	// its real arguments, by TestHandleLoginRequired_StampsFailureAssertedEvenOnADuplicateSignal.
	mockLinkedItemRepo.On("MarkFailureAsserted", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	mockNotificationRepo := new(MockNotificationRepositoryForItemError)
	mockPlaidService := new(MockPlaidService)

	mockEmailService := new(MockEmailService)
	mockUserRepo := new(MockUserRepository)

	mockRepairNonceRepo := new(MockLinkedItemRepairNonceRepository)
	service := NewItemErrorService(
		mockLinkedItemRepo,
		mockNotificationRepo,
		mockUserRepo,
		mockPlaidService,
		mockEmailService,
		mockRepairNonceRepo,
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
	// Issue #1623: every failure assert ALSO stamps last_failure_asserted_at, via a second,
	// unconditional statement (see MarkFailureAsserted). Declared Maybe() here so tests that
	// never reach a Handle* method need not opt in; the stamp itself is asserted exactly, with
	// its real arguments, by TestHandleLoginRequired_StampsFailureAssertedEvenOnADuplicateSignal.
	mockLinkedItemRepo.On("MarkFailureAsserted", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	mockNotificationRepo := new(MockNotificationRepositoryForItemError)
	mockPlaidService := new(MockPlaidService)

	mockEmailService := new(MockEmailService)
	mockUserRepo := new(MockUserRepository)

	mockRepairNonceRepo := new(MockLinkedItemRepairNonceRepository)
	service := NewItemErrorService(
		mockLinkedItemRepo,
		mockNotificationRepo,
		mockUserRepo,
		mockPlaidService,
		mockEmailService,
		mockRepairNonceRepo,
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
	// Issue #1623: every failure assert ALSO stamps last_failure_asserted_at, via a second,
	// unconditional statement (see MarkFailureAsserted). Declared Maybe() here so tests that
	// never reach a Handle* method need not opt in; the stamp itself is asserted exactly, with
	// its real arguments, by TestHandleLoginRequired_StampsFailureAssertedEvenOnADuplicateSignal.
	mockLinkedItemRepo.On("MarkFailureAsserted", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	mockNotificationRepo := new(MockNotificationRepositoryForItemError)
	mockPlaidService := new(MockPlaidService)

	mockEmailService := new(MockEmailService)
	mockUserRepo := new(MockUserRepository)

	mockRepairNonceRepo := new(MockLinkedItemRepairNonceRepository)
	service := NewItemErrorService(
		mockLinkedItemRepo,
		mockNotificationRepo,
		mockUserRepo,
		mockPlaidService,
		mockEmailService,
		mockRepairNonceRepo,
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

// testRepairNonce is a stand-in for the RAW nonce CreateReconnectLinkToken returns to the client
// (Issue #1620). Every RepairItem test presents it, and the mocked Burn is matched on its sha256
// digest — never on the raw value — so a service that "forgot" to hash before the burn would fail
// rather than quietly comparing the wrong thing.
const testRepairNonce = "raw-repair-nonce-for-tests"

// testWebhookReceivedAt stands in for webhook_events.created_at (Issue #1623) — the
// Postgres-stamped instant a webhook was received, which bounds how far back a LOGIN_REPAIRED may
// reach when clearing a failure. Deliberately a fixed value, not time.Now(): the assertions below
// check that this EXACT instant reaches RepairStatus, and a moving one would let a service that
// substituted its own clock pass.
var testWebhookReceivedAt = time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)

func newReconnectTestService() (ItemErrorService, *MockLinkedItemRepository, *MockPlaidService, *MockLinkedItemRepairNonceRepository) {
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	// Issue #1623: every failure assert ALSO stamps last_failure_asserted_at, via a second,
	// unconditional statement (see MarkFailureAsserted). Declared Maybe() here so tests that
	// never reach a Handle* method need not opt in; the stamp itself is asserted exactly, with
	// its real arguments, by TestHandleLoginRequired_StampsFailureAssertedEvenOnADuplicateSignal.
	mockLinkedItemRepo.On("MarkFailureAsserted", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	mockNotificationRepo := new(MockNotificationRepositoryForItemError)
	mockPlaidService := new(MockPlaidService)
	mockEmailService := new(MockEmailService)
	mockUserRepo := new(MockUserRepository)

	mockRepairNonceRepo := new(MockLinkedItemRepairNonceRepository)
	service := NewItemErrorService(
		mockLinkedItemRepo,
		mockNotificationRepo,
		mockUserRepo,
		mockPlaidService,
		mockEmailService,
		mockRepairNonceRepo,
		nil,
	)
	return service, mockLinkedItemRepo, mockPlaidService, mockRepairNonceRepo
}

func TestCreateReconnectLinkToken_Success(t *testing.T) {
	ctx := context.Background()
	service, mockLinkedItemRepo, mockPlaidService, mockRepairNonceRepo := newReconnectTestService()

	linkedItem := &domain.LinkedItem{
		ID:          "linked-item-123",
		UserID:      "user-456",
		ItemID:      "plaid-item-789",
		AccessToken: "decrypted-access-token",
		Status:      domain.LinkedItemStatusLoginRequired,
	}
	mockLinkedItemRepo.On("GetByItemID", ctx, "plaid-item-789").Return(linkedItem, nil)

	plaidExpiry := time.Now().Add(30 * time.Minute).UTC()
	linkTokenResponse := &domain.LinkTokenResponse{LinkToken: "link-token-fresh", Expiration: plaidExpiry}
	mockPlaidService.On("CreateUpdateModeLinkToken", ctx, "user-456", "decrypted-access-token").
		Return(linkTokenResponse, nil)

	var minted *domain.LinkedItemRepairNonce
	mockRepairNonceRepo.On("Mint", ctx, mock.AnythingOfType("*domain.LinkedItemRepairNonce")).
		Run(func(args mock.Arguments) {
			minted = args.Get(1).(*domain.LinkedItemRepairNonce)
		}).Return(nil)

	resp, err := service.CreateReconnectLinkToken(ctx, "user-456", "plaid-item-789")

	require.NoError(t, err)
	assert.Equal(t, "link-token-fresh", resp.LinkToken)
	mockPlaidService.AssertExpectations(t)
	mockRepairNonceRepo.AssertExpectations(t)

	// Issue #1620: the RAW nonce goes to the client and ONLY its sha256 digest is persisted.
	require.NotEmpty(t, resp.RepairNonce, "the mint must return a raw nonce; without one no repair is possible")
	require.NotNil(t, minted)
	assert.Equal(t, hashRepairNonce(resp.RepairNonce), minted.TokenHash,
		"the persisted token_hash must be the sha256 of the raw nonce handed to the client")
	assert.NotEqual(t, resp.RepairNonce, minted.TokenHash, "the RAW nonce must never be what is persisted")
	assert.Equal(t, "linked-item-123", minted.LinkedItemID)
	assert.Equal(t, "user-456", minted.UserID)
	// The nonce's TTL is Plaid's own, not an invented one — see LinkedItemRepairNonce.ExpiresAt.
	assert.Equal(t, plaidExpiry, minted.ExpiresAt)
}

// TestCreateReconnectLinkToken_TwoMintsProduceDifferentNonces guards the single most damaging way
// to get this wrong: a nonce derived from anything stable (the item id, the user id, a counter) is
// not a nonce at all — every user could replay their own, forever. crypto/rand is what makes the
// burn's single-use property mean anything.
func TestCreateReconnectLinkToken_TwoMintsProduceDifferentNonces(t *testing.T) {
	ctx := context.Background()
	service, mockLinkedItemRepo, mockPlaidService, mockRepairNonceRepo := newReconnectTestService()

	linkedItem := &domain.LinkedItem{
		ID:          "linked-item-123",
		UserID:      "user-456",
		ItemID:      "plaid-item-789",
		AccessToken: "decrypted-access-token",
		Status:      domain.LinkedItemStatusLoginRequired,
	}
	mockLinkedItemRepo.On("GetByItemID", ctx, "plaid-item-789").Return(linkedItem, nil)
	mockPlaidService.On("CreateUpdateModeLinkToken", ctx, "user-456", "decrypted-access-token").
		Return(&domain.LinkTokenResponse{LinkToken: "link-token-fresh"}, nil)
	mockRepairNonceRepo.On("Mint", ctx, mock.AnythingOfType("*domain.LinkedItemRepairNonce")).Return(nil)

	first, err := service.CreateReconnectLinkToken(ctx, "user-456", "plaid-item-789")
	require.NoError(t, err)
	second, err := service.CreateReconnectLinkToken(ctx, "user-456", "plaid-item-789")
	require.NoError(t, err)

	assert.NotEqual(t, first.RepairNonce, second.RepairNonce,
		"two mints for the SAME item and user must produce different nonces")
}

// TestCreateReconnectLinkToken_MintFailure_ReturnsNoToken pins the fail-closed choice: a link
// token the user cannot then redeem is worse than an error, because they would complete Plaid
// Link and only then be refused, with nothing to tell them why.
func TestCreateReconnectLinkToken_MintFailure_ReturnsNoToken(t *testing.T) {
	ctx := context.Background()
	service, mockLinkedItemRepo, mockPlaidService, mockRepairNonceRepo := newReconnectTestService()

	linkedItem := &domain.LinkedItem{
		ID:          "linked-item-123",
		UserID:      "user-456",
		ItemID:      "plaid-item-789",
		AccessToken: "decrypted-access-token",
		Status:      domain.LinkedItemStatusLoginRequired,
	}
	mockLinkedItemRepo.On("GetByItemID", ctx, "plaid-item-789").Return(linkedItem, nil)
	mockPlaidService.On("CreateUpdateModeLinkToken", ctx, "user-456", "decrypted-access-token").
		Return(&domain.LinkTokenResponse{LinkToken: "link-token-fresh"}, nil)
	mintErr := errors.New("nonce table unavailable")
	mockRepairNonceRepo.On("Mint", ctx, mock.AnythingOfType("*domain.LinkedItemRepairNonce")).Return(mintErr)

	resp, err := service.CreateReconnectLinkToken(ctx, "user-456", "plaid-item-789")

	require.Error(t, err)
	assert.ErrorIs(t, err, mintErr)
	assert.Nil(t, resp, "no link token may be handed out when its nonce could not be minted")
}

func TestCreateReconnectLinkToken_WrongUser_ReturnsNotFound(t *testing.T) {
	ctx := context.Background()
	service, mockLinkedItemRepo, mockPlaidService, mockRepairNonceRepo := newReconnectTestService()

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
	mockassert.NoCallTo(t, &mockRepairNonceRepo.Mock, "Mint")
}

func TestCreateReconnectLinkToken_ActiveItem_ReturnsNotRepairable(t *testing.T) {
	ctx := context.Background()
	service, mockLinkedItemRepo, mockPlaidService, mockRepairNonceRepo := newReconnectTestService()

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
	mockassert.NoCallTo(t, &mockRepairNonceRepo.Mock, "Mint")
}

func TestCreateReconnectLinkToken_DisconnectedItem_ReturnsNotRepairable(t *testing.T) {
	ctx := context.Background()
	service, mockLinkedItemRepo, mockPlaidService, mockRepairNonceRepo := newReconnectTestService()

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
	mockassert.NoCallTo(t, &mockRepairNonceRepo.Mock, "Mint")
}

func TestCreateReconnectLinkToken_ReplacedItem_ReturnsNotRepairable(t *testing.T) {
	ctx := context.Background()
	service, mockLinkedItemRepo, mockPlaidService, mockRepairNonceRepo := newReconnectTestService()

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
	mockassert.NoCallTo(t, &mockRepairNonceRepo.Mock, "Mint")
}

// TestCreateReconnectLinkToken_RevokedItem_ReturnsNotRepairable pins BLOCKING 1 (Issue #1609
// round 2): Plaid says a revoked Item cannot be restored, so the mint endpoint must refuse to
// hand out an update-mode token for one at all — never mind what RepairItem would later do.
func TestCreateReconnectLinkToken_RevokedItem_ReturnsNotRepairable(t *testing.T) {
	ctx := context.Background()
	service, mockLinkedItemRepo, mockPlaidService, mockRepairNonceRepo := newReconnectTestService()

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
	mockassert.NoCallTo(t, &mockRepairNonceRepo.Mock, "Mint")
}

func TestCreateReconnectLinkToken_ItemNotFound(t *testing.T) {
	ctx := context.Background()
	service, mockLinkedItemRepo, mockPlaidService, mockRepairNonceRepo := newReconnectTestService()

	mockLinkedItemRepo.On("GetByItemID", ctx, "unknown-item").Return(nil, repository.ErrLinkedItemNotFound)

	_, err := service.CreateReconnectLinkToken(ctx, "user-456", "unknown-item")

	require.Error(t, err)
	assert.ErrorIs(t, err, repository.ErrLinkedItemNotFound)
	mockassert.NoCallTo(t, &mockPlaidService.Mock, "CreateUpdateModeLinkToken")
	mockassert.NoCallTo(t, &mockRepairNonceRepo.Mock, "Mint")
}

// ============================================================================
// RepairItem Tests (Issue #1609)
// ============================================================================

func TestRepairItem_Success_ClearsErrorStateAndReactivates(t *testing.T) {
	ctx := context.Background()
	service, mockLinkedItemRepo, _, mockRepairNonceRepo := newReconnectTestService()

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
	mockRepairNonceRepo.On("Burn", ctx, hashRepairNonce(testRepairNonce), "user-456", "linked-item-123").
		Return(true, nil)
	mockLinkedItemRepo.On("RepairStatus", ctx, "linked-item-123", "user-456", repairableStatuses, (*time.Time)(nil)).
		Return(true, nil)

	err := service.RepairItem(ctx, "user-456", "plaid-item-789", testRepairNonce)

	require.NoError(t, err)
	mockLinkedItemRepo.AssertExpectations(t)
	mockRepairNonceRepo.AssertExpectations(t)
}

// TestRepairItem_NonceRefused_DoesNotRepair is the whole point of Issue #1620: a repair that
// cannot present a nonce this server minted must change nothing. A zero-row burn is a REFUSAL —
// if RepairStatus is reached at all here, the endpoint is still marking items healthy on nothing
// but an item_id, which is not a secret.
func TestRepairItem_NonceRefused_DoesNotRepair(t *testing.T) {
	ctx := context.Background()
	service, mockLinkedItemRepo, _, mockRepairNonceRepo := newReconnectTestService()

	linkedItem := &domain.LinkedItem{
		ID:     "linked-item-123",
		UserID: "user-456",
		ItemID: "plaid-item-789",
		Status: domain.LinkedItemStatusLoginRequired,
	}
	mockLinkedItemRepo.On("GetByItemID", ctx, "plaid-item-789").Return(linkedItem, nil)
	mockRepairNonceRepo.On("Burn", ctx, hashRepairNonce(testRepairNonce), "user-456", "linked-item-123").
		Return(false, nil)

	err := service.RepairItem(ctx, "user-456", "plaid-item-789", testRepairNonce)

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrRepairNonceInvalid)
	mockassert.NoCallTo(t, &mockLinkedItemRepo.Mock, "RepairStatus")
}

// TestRepairItem_EmptyNonce_IsRefusedWithoutTouchingTheDatabase pins that an ABSENT nonce is a
// refusal and not a bypass. The most likely way to reintroduce this hole is a caller that stops
// sending the field: if an empty nonce fell through to a burn that happened to match nothing, the
// behaviour would be right by accident; if it fell through to RepairStatus, the gate would be gone.
func TestRepairItem_EmptyNonce_IsRefusedWithoutTouchingTheDatabase(t *testing.T) {
	ctx := context.Background()
	service, mockLinkedItemRepo, _, mockRepairNonceRepo := newReconnectTestService()

	linkedItem := &domain.LinkedItem{
		ID:     "linked-item-123",
		UserID: "user-456",
		ItemID: "plaid-item-789",
		Status: domain.LinkedItemStatusLoginRequired,
	}
	mockLinkedItemRepo.On("GetByItemID", ctx, "plaid-item-789").Return(linkedItem, nil)

	err := service.RepairItem(ctx, "user-456", "plaid-item-789", "")

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrRepairNonceInvalid)
	mockassert.NoCallTo(t, &mockRepairNonceRepo.Mock, "Burn")
	mockassert.NoCallTo(t, &mockLinkedItemRepo.Mock, "RepairStatus")
}

// TestRepairItem_BurnError_IsWrappedAndDoesNotRepair: a database failure during the burn is NOT a
// refusal (it is an error the caller must see), but it must still not repair.
func TestRepairItem_BurnError_IsWrappedAndDoesNotRepair(t *testing.T) {
	ctx := context.Background()
	service, mockLinkedItemRepo, _, mockRepairNonceRepo := newReconnectTestService()

	linkedItem := &domain.LinkedItem{
		ID:     "linked-item-123",
		UserID: "user-456",
		ItemID: "plaid-item-789",
		Status: domain.LinkedItemStatusLoginRequired,
	}
	burnErr := errors.New("connection reset")
	mockLinkedItemRepo.On("GetByItemID", ctx, "plaid-item-789").Return(linkedItem, nil)
	mockRepairNonceRepo.On("Burn", ctx, hashRepairNonce(testRepairNonce), "user-456", "linked-item-123").
		Return(false, burnErr)

	err := service.RepairItem(ctx, "user-456", "plaid-item-789", testRepairNonce)

	require.Error(t, err)
	assert.ErrorIs(t, err, burnErr)
	assert.NotErrorIs(t, err, ErrRepairNonceInvalid,
		"a database failure must not masquerade as an authorization refusal")
	mockassert.NoCallTo(t, &mockLinkedItemRepo.Mock, "RepairStatus")
}

func TestRepairItem_WrongUser_ReturnsNotFound(t *testing.T) {
	ctx := context.Background()
	service, mockLinkedItemRepo, _, mockRepairNonceRepo := newReconnectTestService()

	linkedItem := &domain.LinkedItem{
		ID:     "linked-item-123",
		UserID: "user-456",
		ItemID: "plaid-item-789",
		Status: domain.LinkedItemStatusLoginRequired,
	}
	mockLinkedItemRepo.On("GetByItemID", ctx, "plaid-item-789").Return(linkedItem, nil)

	err := service.RepairItem(ctx, "attacker-999", "plaid-item-789", testRepairNonce)

	require.Error(t, err)
	assert.ErrorIs(t, err, repository.ErrLinkedItemNotFound)
	mockassert.NoCallTo(t, &mockLinkedItemRepo.Mock, "RepairStatus")
	mockassert.NoCallTo(t, &mockRepairNonceRepo.Mock, "Burn")
}

func TestRepairItem_ActiveItem_ReturnsNotRepairable(t *testing.T) {
	ctx := context.Background()
	service, mockLinkedItemRepo, _, mockRepairNonceRepo := newReconnectTestService()

	linkedItem := &domain.LinkedItem{
		ID:     "linked-item-123",
		UserID: "user-456",
		ItemID: "plaid-item-789",
		Status: domain.LinkedItemStatusActive,
	}
	mockLinkedItemRepo.On("GetByItemID", ctx, "plaid-item-789").Return(linkedItem, nil)

	err := service.RepairItem(ctx, "user-456", "plaid-item-789", testRepairNonce)

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrLinkedItemNotRepairable)
	mockassert.NoCallTo(t, &mockLinkedItemRepo.Mock, "RepairStatus")
	mockassert.NoCallTo(t, &mockRepairNonceRepo.Mock, "Burn")
}

// TestRepairItem_RevokedItem_ReturnsNotRepairable pins BLOCKING 1 (Issue #1609 round 2): Plaid
// says a revoked Item cannot be restored — update-mode repair must never mark it 'active'.
func TestRepairItem_RevokedItem_ReturnsNotRepairable(t *testing.T) {
	ctx := context.Background()
	service, mockLinkedItemRepo, _, mockRepairNonceRepo := newReconnectTestService()

	linkedItem := &domain.LinkedItem{
		ID:     "linked-item-123",
		UserID: "user-456",
		ItemID: "plaid-item-789",
		Status: domain.LinkedItemStatusRevoked,
	}
	mockLinkedItemRepo.On("GetByItemID", ctx, "plaid-item-789").Return(linkedItem, nil)

	err := service.RepairItem(ctx, "user-456", "plaid-item-789", testRepairNonce)

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrLinkedItemNotRepairable)
	mockassert.NoCallTo(t, &mockLinkedItemRepo.Mock, "RepairStatus")
	mockassert.NoCallTo(t, &mockRepairNonceRepo.Mock, "Burn")
}

// TestRepairItem_ConcurrentStateChange_ReturnsNotRepairable pins BLOCKING 2 (Issue #1609 round
// 2): the pre-check in Go read a status that was still repairable, but RepairStatus's atomic
// UPDATE — re-checking status fresh, in the same statement — found the row no longer matched
// (e.g. a concurrent disconnect landed between the read and the write) and correctly affected
// zero rows. RepairItem must treat that the same as "not repairable", not as success.
func TestRepairItem_ConcurrentStateChange_ReturnsNotRepairable(t *testing.T) {
	ctx := context.Background()
	service, mockLinkedItemRepo, _, mockRepairNonceRepo := newReconnectTestService()

	linkedItem := &domain.LinkedItem{
		ID:     "linked-item-123",
		UserID: "user-456",
		ItemID: "plaid-item-789",
		Status: domain.LinkedItemStatusLoginRequired, // stale read
	}
	mockLinkedItemRepo.On("GetByItemID", ctx, "plaid-item-789").Return(linkedItem, nil)
	mockRepairNonceRepo.On("Burn", ctx, hashRepairNonce(testRepairNonce), "user-456", "linked-item-123").
		Return(true, nil)
	mockLinkedItemRepo.On("RepairStatus", ctx, "linked-item-123", "user-456", repairableStatuses, (*time.Time)(nil)).
		Return(false, nil) // concurrent disconnect/re-link won the race

	err := service.RepairItem(ctx, "user-456", "plaid-item-789", testRepairNonce)

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrLinkedItemNotRepairable)
}

func TestRepairItem_RepairStatusError_IsWrapped(t *testing.T) {
	ctx := context.Background()
	service, mockLinkedItemRepo, _, mockRepairNonceRepo := newReconnectTestService()

	linkedItem := &domain.LinkedItem{
		ID:     "linked-item-123",
		UserID: "user-456",
		ItemID: "plaid-item-789",
		Status: domain.LinkedItemStatusLoginRequired,
	}
	dbErr := errors.New("connection reset")
	mockLinkedItemRepo.On("GetByItemID", ctx, "plaid-item-789").Return(linkedItem, nil)
	mockRepairNonceRepo.On("Burn", ctx, hashRepairNonce(testRepairNonce), "user-456", "linked-item-123").
		Return(true, nil)
	mockLinkedItemRepo.On("RepairStatus", ctx, "linked-item-123", "user-456", repairableStatuses, (*time.Time)(nil)).
		Return(false, dbErr)

	err := service.RepairItem(ctx, "user-456", "plaid-item-789", testRepairNonce)

	require.Error(t, err)
	assert.ErrorIs(t, err, dbErr)
}

// ============================================================================
// HandleLoginRepaired Tests (LOGIN_REPAIRED webhook, Issue #1609)
// ============================================================================

func TestHandleLoginRepaired_ClearsErrorState(t *testing.T) {
	ctx := context.Background()
	service, mockLinkedItemRepo, _, _ := newReconnectTestService()

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
		&testWebhookReceivedAt,
	).Return(true, nil)

	err := service.HandleLoginRepaired(ctx, linkedItem, testWebhookReceivedAt)

	require.NoError(t, err)
	mockLinkedItemRepo.AssertExpectations(t)
}

func TestHandleLoginRepaired_DisconnectedItem_IsNoOp(t *testing.T) {
	ctx := context.Background()
	service, mockLinkedItemRepo, _, _ := newReconnectTestService()

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
		&testWebhookReceivedAt,
	).Return(false, nil)

	// Then: no-op, NOT an error — a webhook the user did not act on must not resurrect a
	// deliberately disconnected item, and must not be retried/DLQ'd either.
	err := service.HandleLoginRepaired(ctx, linkedItem, testWebhookReceivedAt)

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
	service, mockLinkedItemRepo, _, _ := newReconnectTestService()

	linkedItem := &domain.LinkedItem{
		ID:     "linked-item-123",
		UserID: "user-456",
		ItemID: "plaid-item-789",
		Status: domain.LinkedItemStatusError, // a NEW, different error since LOGIN_REPAIRED fired
	}
	mockLinkedItemRepo.On(
		"RepairStatus", ctx, "linked-item-123", "", []domain.LinkedItemStatus{domain.LinkedItemStatusLoginRequired},
		&testWebhookReceivedAt,
	).Return(false, nil)

	err := service.HandleLoginRepaired(ctx, linkedItem, testWebhookReceivedAt)

	require.NoError(t, err)
	mockLinkedItemRepo.AssertExpectations(t)
}

func TestHandleLoginRepaired_RepairStatusError_IsWrapped(t *testing.T) {
	ctx := context.Background()
	service, mockLinkedItemRepo, _, _ := newReconnectTestService()

	linkedItem := &domain.LinkedItem{
		ID:     "linked-item-123",
		UserID: "user-456",
		ItemID: "plaid-item-789",
		Status: domain.LinkedItemStatusLoginRequired,
	}
	dbErr := errors.New("connection reset")
	mockLinkedItemRepo.On(
		"RepairStatus", ctx, "linked-item-123", "", []domain.LinkedItemStatus{domain.LinkedItemStatusLoginRequired},
		&testWebhookReceivedAt,
	).Return(false, dbErr)

	err := service.HandleLoginRepaired(ctx, linkedItem, testWebhookReceivedAt)

	require.Error(t, err)
	assert.ErrorIs(t, err, dbErr)
}

func TestProcessWebhook_LoginRepaired_RoutesToHandleLoginRepaired(t *testing.T) {
	ctx := context.Background()
	service, mockLinkedItemRepo, _, _ := newReconnectTestService()

	linkedItem := &domain.LinkedItem{
		ID:     "linked-item-123",
		UserID: "user-456",
		ItemID: "plaid-item-789",
		Status: domain.LinkedItemStatusLoginRequired,
	}
	mockLinkedItemRepo.On("GetByItemID", ctx, "plaid-item-789").Return(linkedItem, nil)
	mockLinkedItemRepo.On(
		"RepairStatus", ctx, "linked-item-123", "", []domain.LinkedItemStatus{domain.LinkedItemStatusLoginRequired},
		&testWebhookReceivedAt,
	).Return(true, nil)

	err := service.ProcessWebhook(ctx, "plaid-item-789", "LOGIN_REPAIRED", "", "", testWebhookReceivedAt)

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
	// Issue #1623: every failure assert ALSO stamps last_failure_asserted_at, via a second,
	// unconditional statement (see MarkFailureAsserted). Declared Maybe() here so tests that
	// never reach a Handle* method need not opt in; the stamp itself is asserted exactly, with
	// its real arguments, by TestHandleLoginRequired_StampsFailureAssertedEvenOnADuplicateSignal.
	mockLinkedItemRepo.On("MarkFailureAsserted", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	mockNotificationRepo := new(MockNotificationRepositoryForItemError)
	mockPlaidService := new(MockPlaidService)
	mockEmailService := new(MockEmailService)
	mockUserRepo := new(MockUserRepository)

	mockRepairNonceRepo := new(MockLinkedItemRepairNonceRepository)
	service := NewItemErrorService(
		mockLinkedItemRepo,
		mockNotificationRepo,
		mockUserRepo,
		mockPlaidService,
		mockEmailService,
		mockRepairNonceRepo,
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
	// Issue #1623: every failure assert ALSO stamps last_failure_asserted_at, via a second,
	// unconditional statement (see MarkFailureAsserted). Declared Maybe() here so tests that
	// never reach a Handle* method need not opt in; the stamp itself is asserted exactly, with
	// its real arguments, by TestHandleLoginRequired_StampsFailureAssertedEvenOnADuplicateSignal.
	mockLinkedItemRepo.On("MarkFailureAsserted", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	mockNotificationRepo := new(MockNotificationRepositoryForItemError)
	mockPlaidService := new(MockPlaidService)
	mockEmailService := new(MockEmailService)
	mockUserRepo := new(MockUserRepository)

	mockRepairNonceRepo := new(MockLinkedItemRepairNonceRepository)
	service := NewItemErrorService(
		mockLinkedItemRepo,
		mockNotificationRepo,
		mockUserRepo,
		mockPlaidService,
		mockEmailService,
		mockRepairNonceRepo,
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

// MockLinkedItemRepairNonceRepository mocks repository.LinkedItemRepairNonceRepository
// (Issue #1620). Deliberately a bare testify mock with no default behaviour: a test that reaches
// Mint or Burn without saying so fails loudly rather than silently minting or burning nothing,
// which is exactly the failure mode a nonce gate must never have.
type MockLinkedItemRepairNonceRepository struct {
	mock.Mock
}

func (m *MockLinkedItemRepairNonceRepository) Mint(ctx context.Context, nonce *domain.LinkedItemRepairNonce) error {
	args := m.Called(ctx, nonce)
	return args.Error(0)
}

func (m *MockLinkedItemRepairNonceRepository) Burn(ctx context.Context, tokenHash, userID, linkedItemID string) (bool, error) {
	args := m.Called(ctx, tokenHash, userID, linkedItemID)
	return args.Bool(0), args.Error(1)
}

var _ repository.LinkedItemRepairNonceRepository = (*MockLinkedItemRepairNonceRepository)(nil)

// ============================================================================
// Issue #1623 — a delayed clear must not erase a NEWER failure
// ============================================================================

// TestHandleLoginRequired_StampsFailureAssertedEvenOnADuplicateSignal is the whole reason
// MarkFailureAsserted exists as a SEPARATE statement.
//
// TransitionErrorStatus returns false here — the item is ALREADY login_required, so its
// `AND status <> $1` predicate matches zero rows and writes nothing, which is correct for its own
// dedup purpose. But that is exactly the case #1623 is about: the SECOND failure of the same kind.
// If the failure-assert clock were folded into that statement's SET list it would not advance, the
// second failure would look as old as the first, and the first failure's delayed LOGIN_REPAIRED
// would clear it. The stamp must land anyway.
func TestHandleLoginRequired_StampsFailureAssertedEvenOnADuplicateSignal(t *testing.T) {
	ctx := context.Background()
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	mockNotificationRepo := new(MockNotificationRepositoryForItemError)
	mockPlaidService := new(MockPlaidService)
	mockEmailService := new(MockEmailService)
	mockUserRepo := new(MockUserRepository)
	mockRepairNonceRepo := new(MockLinkedItemRepairNonceRepository)
	service := NewItemErrorService(
		mockLinkedItemRepo, mockNotificationRepo, mockUserRepo,
		mockPlaidService, mockEmailService, mockRepairNonceRepo, nil,
	)

	linkedItem := &domain.LinkedItem{
		ID:     "linked-item-123",
		UserID: "user-456",
		ItemID: "plaid-item-789",
		Status: domain.LinkedItemStatusLoginRequired, // already in this status
	}

	// Exact arguments, not mock.Anything: the terminal-status exclusion is part of the contract —
	// a stale signal must not advance the clock on an item the user deliberately disconnected.
	mockLinkedItemRepo.On("MarkFailureAsserted", ctx, "linked-item-123", terminalLinkedItemStatuses).Return(nil)
	mockLinkedItemRepo.On(
		"TransitionErrorStatus", ctx, "linked-item-123", domain.LinkedItemStatusLoginRequired,
		"ITEM_LOGIN_REQUIRED", "creds changed", terminalLinkedItemStatuses,
	).Return(false, nil) // duplicate signal: writes NOTHING

	err := service.HandleLoginRequired(ctx, linkedItem, "ITEM_LOGIN_REQUIRED", "creds changed")

	require.NoError(t, err)
	mockLinkedItemRepo.AssertExpectations(t)
	// Still a duplicate for notification purposes — the stamp must not resurrect the notification.
	mockassert.NoCallTo(t, &mockNotificationRepo.Mock, "Create")
}

// TestHandleFailure_StampsFailureAssertedOnEveryProducerPath: all four Handle* methods are failure
// asserts, and a clock that advances on only some of them orders nothing. Enumerated deliberately
// rather than testing one and assuming the rest.
func TestHandleFailure_StampsFailureAssertedOnEveryProducerPath(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status domain.LinkedItemStatus
		call   func(ItemErrorService, context.Context, *domain.LinkedItem) error
	}{
		{"login_required", domain.LinkedItemStatusLoginRequired, func(s ItemErrorService, ctx context.Context, li *domain.LinkedItem) error {
			return s.HandleLoginRequired(ctx, li, "ITEM_LOGIN_REQUIRED", "")
		}},
		{"error", domain.LinkedItemStatusError, func(s ItemErrorService, ctx context.Context, li *domain.LinkedItem) error {
			return s.HandleError(ctx, li, "ITEM_ERROR", "")
		}},
		{"pending_expiration", domain.LinkedItemStatusPendingExpiration, func(s ItemErrorService, ctx context.Context, li *domain.LinkedItem) error {
			return s.HandlePendingExpiration(ctx, li, "ITEM_PENDING_EXPIRATION", "")
		}},
		{"revoked", domain.LinkedItemStatusRevoked, func(s ItemErrorService, ctx context.Context, li *domain.LinkedItem) error {
			return s.HandlePermissionRevoked(ctx, li, "ITEM_PERMISSION_REVOKED", "")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			mockLinkedItemRepo := new(MockLinkedItemRepository)
			mockNotificationRepo := new(MockNotificationRepositoryForItemError)
			mockPlaidService := new(MockPlaidService)
			mockEmailService := new(MockEmailService)
			mockUserRepo := new(MockUserRepository)
			mockRepairNonceRepo := new(MockLinkedItemRepairNonceRepository)
			service := NewItemErrorService(
				mockLinkedItemRepo, mockNotificationRepo, mockUserRepo,
				mockPlaidService, mockEmailService, mockRepairNonceRepo, nil,
			)

			linkedItem := &domain.LinkedItem{
				ID: "linked-item-123", UserID: "user-456", ItemID: "plaid-item-789",
				Status: domain.LinkedItemStatusActive,
			}
			mockLinkedItemRepo.On("MarkFailureAsserted", ctx, "linked-item-123", terminalLinkedItemStatuses).Return(nil)
			mockLinkedItemRepo.On(
				"TransitionErrorStatus", ctx, "linked-item-123", tc.status,
				mock.Anything, mock.Anything, terminalLinkedItemStatuses,
			).Return(false, nil)

			require.NoError(t, tc.call(service, ctx, linkedItem))
			mockLinkedItemRepo.AssertExpectations(t)
		})
	}
}

// TestHandleLoginRequired_StampFailureIsLoggedNotFatal: the user must still be told their
// connection broke even if the ordering clock could not be written. Failing the whole webhook
// would send it to retry/DLQ and lose the notification entirely — a worse outcome than a
// theoretically-clearable failure.
func TestHandleLoginRequired_StampFailureIsLoggedNotFatal(t *testing.T) {
	ctx := context.Background()
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	mockNotificationRepo := new(MockNotificationRepositoryForItemError)
	mockPlaidService := new(MockPlaidService)
	mockEmailService := new(MockEmailService)
	mockUserRepo := new(MockUserRepository)
	mockRepairNonceRepo := new(MockLinkedItemRepairNonceRepository)
	service := NewItemErrorService(
		mockLinkedItemRepo, mockNotificationRepo, mockUserRepo,
		mockPlaidService, mockEmailService, mockRepairNonceRepo, nil,
	)

	linkedItem := &domain.LinkedItem{
		ID: "linked-item-123", UserID: "user-456", ItemID: "plaid-item-789",
		Status: domain.LinkedItemStatusActive,
	}
	mockLinkedItemRepo.On("MarkFailureAsserted", ctx, "linked-item-123", terminalLinkedItemStatuses).
		Return(errors.New("connection reset"))
	mockLinkedItemRepo.On(
		"TransitionErrorStatus", ctx, "linked-item-123", domain.LinkedItemStatusLoginRequired,
		"ITEM_LOGIN_REQUIRED", "", terminalLinkedItemStatuses,
	).Return(false, nil)

	err := service.HandleLoginRequired(ctx, linkedItem, "ITEM_LOGIN_REQUIRED", "")

	require.NoError(t, err, "a failed stamp must not fail webhook processing")
	mockLinkedItemRepo.AssertExpectations(t)
}

// TestHandleLoginRepaired_BoundsTheClearByTheWebhookReceiptInstant pins the plumbing #1623 needs:
// the receipt instant must reach RepairStatus's predicate, verbatim and by pointer. The mock
// records notNewerThan deliberately (see MockLinkedItemRepository.RepairStatus) — without that, a
// service that passed nil would pass this test while clearing failures asserted an hour later.
func TestHandleLoginRepaired_BoundsTheClearByTheWebhookReceiptInstant(t *testing.T) {
	ctx := context.Background()
	service, mockLinkedItemRepo, _, _ := newReconnectTestService()

	linkedItem := &domain.LinkedItem{
		ID: "linked-item-123", UserID: "user-456", ItemID: "plaid-item-789",
		Status: domain.LinkedItemStatusLoginRequired,
	}
	mockLinkedItemRepo.On(
		"RepairStatus", ctx, "linked-item-123", "",
		[]domain.LinkedItemStatus{domain.LinkedItemStatusLoginRequired},
		mock.MatchedBy(func(bound *time.Time) bool {
			return bound != nil && bound.Equal(testWebhookReceivedAt)
		}),
	).Return(true, nil)

	require.NoError(t, service.HandleLoginRepaired(ctx, linkedItem, testWebhookReceivedAt))
	mockLinkedItemRepo.AssertExpectations(t)
}

// TestHandleLoginRepaired_ZeroReceiptInstantAppliesNoBound: the zero time is not an instant, and
// using it as an upper bound would refuse EVERY clear (every stamp is after year 1). A caller with
// no receipt instant must get the pre-#1623 behaviour, not a silently dead handler.
func TestHandleLoginRepaired_ZeroReceiptInstantAppliesNoBound(t *testing.T) {
	ctx := context.Background()
	service, mockLinkedItemRepo, _, _ := newReconnectTestService()

	linkedItem := &domain.LinkedItem{
		ID: "linked-item-123", UserID: "user-456", ItemID: "plaid-item-789",
		Status: domain.LinkedItemStatusLoginRequired,
	}
	mockLinkedItemRepo.On(
		"RepairStatus", ctx, "linked-item-123", "",
		[]domain.LinkedItemStatus{domain.LinkedItemStatusLoginRequired},
		(*time.Time)(nil),
	).Return(true, nil)

	require.NoError(t, service.HandleLoginRepaired(ctx, linkedItem, time.Time{}))
	mockLinkedItemRepo.AssertExpectations(t)
}
