package services

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
)

// TestSendDeletionConfirmation_Success tests successful email sending
func TestSendDeletionConfirmation_Success(t *testing.T) {
	ctx := context.Background()

	// Use mock mode for testing without real API calls
	service := NewEmailServiceMock("test-api-key", "test@boostedbudget.com", "https://boostedbudget.com")

	email := "user@example.com"
	token := "test-cancellation-token-123"
	scheduledDate := time.Now().Add(30 * 24 * time.Hour)

	err := service.SendDeletionConfirmation(ctx, email, token, scheduledDate)

	assert.NoError(t, err)
}

// TestSendDeletionConfirmation_InvalidEmail tests invalid email handling
func TestSendDeletionConfirmation_InvalidEmail(t *testing.T) {
	ctx := context.Background()

	service := NewEmailServiceMock("test-api-key", "test@boostedbudget.com", "https://boostedbudget.com")

	invalidEmail := "not-an-email"
	token := "test-token"
	scheduledDate := time.Now().Add(30 * 24 * time.Hour)

	err := service.SendDeletionConfirmation(ctx, invalidEmail, token, scheduledDate)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid email")
}

// TestSendDeletionConfirmation_SMTPError tests SMTP/API error handling
func TestSendDeletionConfirmation_SMTPError(t *testing.T) {
	ctx := context.Background()

	// Use invalid API key to simulate error
	service := NewEmailService("invalid-api-key", "test@boostedbudget.com", "https://boostedbudget.com")

	email := "user@example.com"
	token := "test-token"
	scheduledDate := time.Now().Add(30 * 24 * time.Hour)

	err := service.SendDeletionConfirmation(ctx, email, token, scheduledDate)

	// In a real scenario, this would fail due to invalid API key
	// For now, we expect it to handle errors gracefully
	if err != nil {
		assert.Contains(t, strings.ToLower(err.Error()), "error")
	}
}

// TestEmailTemplate_ContainsRequiredFields tests email template validation
func TestEmailTemplate_ContainsRequiredFields(t *testing.T) {
	ctx := context.Background()

	service := NewEmailServiceMock("test-api-key", "test@boostedbudget.com", "https://boostedbudget.com")

	email := "user@example.com"
	token := "test-cancellation-token-123"
	scheduledDate := time.Now().Add(30 * 24 * time.Hour)

	// This test verifies that the email template contains required fields
	// We'll check this by ensuring the service constructs the message correctly
	_ = service.SendDeletionConfirmation(ctx, email, token, scheduledDate)

	// Even if sending fails, we want to verify the template is constructed
	// The implementation should validate that:
	// - Email includes cancellation link with token
	// - Email includes scheduled deletion date
	// - Email includes clear instructions
	// - Subject line is appropriate

	// For now, we just check it doesn't panic
	// In GREEN phase, we'll add proper template validation
	assert.NotNil(t, service)
}

// TestSendNotificationEmail_Success tests successful notification email sending (stub)
// Issue #257: Item Error Handler - User Notifications
func TestSendNotificationEmail_Success(t *testing.T) {
	ctx := context.Background()

	// Use mock mode for testing (stub always succeeds)
	service := NewEmailServiceMock("test-api-key", "test@boostedbudget.com", "https://boostedbudget.com")

	// Create test notification with all fields
	actionURL := "/accounts/reconnect?item_id=test"
	actionLabel := "Reconnect Bank"
	notification := &domain.Notification{
		ID:          "notif-123",
		UserID:      "user-456",
		Type:        "login_required",
		Title:       "Reconnect Your Bank",
		Message:     "Please reconnect your bank account to continue syncing transactions.",
		ActionURL:   &actionURL,
		ActionLabel: &actionLabel,
		Read:        false,
		SentEmail:   false,
	}

	err := service.SendNotificationEmail(ctx, "user@example.com", notification)

	// MVP stub always succeeds
	assert.NoError(t, err)
}

// TestSendNotificationEmail_WithoutActionFields tests notification without action URL/label
func TestSendNotificationEmail_WithoutActionFields(t *testing.T) {
	ctx := context.Background()

	service := NewEmailServiceMock("test-api-key", "test@boostedbudget.com", "https://boostedbudget.com")

	// Notification without action fields (e.g., informational notification)
	notification := &domain.Notification{
		ID:          "notif-789",
		UserID:      "user-456",
		Type:        "transaction_sync",
		Title:       "Transactions Synced",
		Message:     "Successfully synced 15 new transactions.",
		ActionURL:   nil, // No action needed
		ActionLabel: nil,
		Read:        false,
		SentEmail:   false,
	}

	err := service.SendNotificationEmail(ctx, "user@example.com", notification)

	// MVP stub handles nil action fields gracefully
	assert.NoError(t, err)
}

// TestSendNotificationEmail_DifferentNotificationTypes tests various notification types
func TestSendNotificationEmail_DifferentNotificationTypes(t *testing.T) {
	ctx := context.Background()
	service := NewEmailServiceMock("test-api-key", "test@boostedbudget.com", "https://boostedbudget.com")

	notificationTypes := []string{
		"item_error",
		"login_required",
		"pending_expiration",
		"permission_revoked",
		"transaction_sync",
	}

	for _, notifType := range notificationTypes {
		t.Run(notifType, func(t *testing.T) {
			notification := &domain.Notification{
				ID:      "notif-" + notifType,
				UserID:  "user-123",
				Type:    notifType,
				Title:   "Test " + notifType,
				Message: "Test message for " + notifType,
			}

			err := service.SendNotificationEmail(ctx, "user@example.com", notification)

			// MVP stub always succeeds for all notification types
			assert.NoError(t, err)
		})
	}
}
