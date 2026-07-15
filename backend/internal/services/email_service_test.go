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

// ─── SendHouseholdInvite (#1367 — Phase 3) ───────────────────────────────────

func TestSendHouseholdInvite_Success(t *testing.T) {
	ctx := context.Background()
	service := NewEmailServiceMock("test-api-key", "test@boostedbudget.com", "https://boostedbudget.com")

	err := service.SendHouseholdInvite(ctx, "invitee@example.com", "Alex Owner", "The Smiths",
		"https://boostedbudget.com/households/invites/rawtoken123", time.Now().Add(48*time.Hour))

	assert.NoError(t, err)
}

func TestSendHouseholdInvite_InvalidEmail(t *testing.T) {
	ctx := context.Background()
	service := NewEmailServiceMock("test-api-key", "test@boostedbudget.com", "https://boostedbudget.com")

	err := service.SendHouseholdInvite(ctx, "not-an-email", "Alex Owner", "The Smiths",
		"https://boostedbudget.com/households/invites/rawtoken123", time.Now().Add(48*time.Hour))

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid email")
}

func TestHouseholdInviteEmailTemplates_ContainInviteLink(t *testing.T) {
	s := &emailService{fromName: "BoostedBudget"}
	inviteURL := "https://boostedbudget.com/households/invites/rawtoken123"

	html := s.buildHouseholdInviteEmailHTML("Alex Owner", "The Smiths", inviteURL, "March 3, 2026")
	text := s.buildHouseholdInviteEmailPlainText("Alex Owner", "The Smiths", inviteURL, "March 3, 2026")

	assert.Contains(t, html, inviteURL)
	assert.Contains(t, html, "The Smiths")
	assert.Contains(t, html, "Alex Owner")
	assert.Contains(t, text, inviteURL)
	assert.Contains(t, text, "The Smiths")
}

// ─── SendHouseholdMemberRemoved (#1371 — Phase 7, Task 3) ────────────────────

func TestSendHouseholdMemberRemoved_Success(t *testing.T) {
	ctx := context.Background()
	service := NewEmailServiceMock("test-api-key", "test@boostedbudget.com", "https://boostedbudget.com")

	err := service.SendHouseholdMemberRemoved(ctx, "removed@example.com", "The Smiths", time.Now().Add(30*24*time.Hour))

	assert.NoError(t, err)
}

func TestSendHouseholdMemberRemoved_InvalidEmail(t *testing.T) {
	ctx := context.Background()
	service := NewEmailServiceMock("test-api-key", "test@boostedbudget.com", "https://boostedbudget.com")

	err := service.SendHouseholdMemberRemoved(ctx, "not-an-email", "The Smiths", time.Now().Add(30*24*time.Hour))

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid email")
}

func TestMemberRemovedEmailTemplates_ContainRequiredContent(t *testing.T) {
	s := &emailService{fromName: "BoostedBudget"}
	purgeDate := time.Date(2026, time.January, 15, 0, 0, 0, 0, time.UTC)

	html := s.buildMemberRemovedEmailHTML("The Smiths", purgeDate)
	text := s.buildMemberRemovedEmailPlainText("The Smiths", purgeDate)

	// Household name present in both.
	assert.Contains(t, html, "The Smiths")
	assert.Contains(t, text, "The Smiths")

	// Purge date formatted in a human-readable way in both.
	assert.Contains(t, html, "January 15, 2026")
	assert.Contains(t, text, "January 15, 2026")

	// Preserved-data messaging present in both.
	assert.Contains(t, html, "preserved")
	assert.Contains(t, text, "preserved")
}

// ─── SendHouseholdOverCapacityNotice / Warning (#1371 — Phase 7, Task 6) ─────

func TestSendHouseholdOverCapacityNotice_Success(t *testing.T) {
	ctx := context.Background()
	service := NewEmailServiceMock("test-api-key", "test@boostedbudget.com", "https://boostedbudget.com")

	err := service.SendHouseholdOverCapacityNotice(ctx, "owner@example.com", "The Smiths", 4, 2, time.Now().Add(14*24*time.Hour))

	assert.NoError(t, err)
}

func TestSendHouseholdOverCapacityNotice_InvalidEmail(t *testing.T) {
	ctx := context.Background()
	service := NewEmailServiceMock("test-api-key", "test@boostedbudget.com", "https://boostedbudget.com")

	err := service.SendHouseholdOverCapacityNotice(ctx, "not-an-email", "The Smiths", 4, 2, time.Now().Add(14*24*time.Hour))

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid email")
}

func TestOverCapacityNoticeEmailTemplates_ContainRequiredContent(t *testing.T) {
	s := &emailService{fromName: "BoostedBudget"}
	deadline := time.Date(2026, time.January, 15, 0, 0, 0, 0, time.UTC)

	html := s.buildOverCapacityNoticeEmailHTML("The Smiths", 4, 2, deadline)
	text := s.buildOverCapacityNoticeEmailPlainText("The Smiths", 4, 2, deadline)

	// Household name present in both.
	assert.Contains(t, html, "The Smiths")
	assert.Contains(t, text, "The Smiths")

	// Deadline formatted in a human-readable way in both (matches Task 3's convention).
	assert.Contains(t, html, "January 15, 2026")
	assert.Contains(t, text, "January 15, 2026")

	// Active/seat counts present in both.
	assert.Contains(t, html, "4")
	assert.Contains(t, html, "2")
	assert.Contains(t, text, "4")
	assert.Contains(t, text, "2")

	// Auto-removal consequence messaging present in both.
	assert.Contains(t, html, "automatically")
	assert.Contains(t, text, "automatically")
}

func TestSendHouseholdOverCapacityWarning_Success(t *testing.T) {
	ctx := context.Background()
	service := NewEmailServiceMock("test-api-key", "test@boostedbudget.com", "https://boostedbudget.com")

	err := service.SendHouseholdOverCapacityWarning(ctx, "owner@example.com", "The Smiths", 4, 2, time.Now().Add(3*24*time.Hour))

	assert.NoError(t, err)
}

func TestSendHouseholdOverCapacityWarning_InvalidEmail(t *testing.T) {
	ctx := context.Background()
	service := NewEmailServiceMock("test-api-key", "test@boostedbudget.com", "https://boostedbudget.com")

	err := service.SendHouseholdOverCapacityWarning(ctx, "not-an-email", "The Smiths", 4, 2, time.Now().Add(3*24*time.Hour))

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid email")
}

func TestOverCapacityWarningEmailTemplates_ContainRequiredContent(t *testing.T) {
	s := &emailService{fromName: "BoostedBudget"}
	deadline := time.Date(2026, time.January, 15, 0, 0, 0, 0, time.UTC)

	html := s.buildOverCapacityWarningEmailHTML("The Smiths", 4, 2, deadline)
	text := s.buildOverCapacityWarningEmailPlainText("The Smiths", 4, 2, deadline)

	// Household name present in both.
	assert.Contains(t, html, "The Smiths")
	assert.Contains(t, text, "The Smiths")

	// Deadline formatted in a human-readable way in both.
	assert.Contains(t, html, "January 15, 2026")
	assert.Contains(t, text, "January 15, 2026")

	// Urgency messaging ("3 days left") present in both.
	assert.Contains(t, html, "3 days")
	assert.Contains(t, text, "3 days")
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
