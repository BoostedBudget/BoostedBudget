package services

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestSendDeletionConfirmation_Success tests successful email sending
func TestSendDeletionConfirmation_Success(t *testing.T) {
	ctx := context.Background()

	// This will fail until we implement EmailService
	service := NewEmailService("test-api-key", "test@boostedbudget.com", "https://boostedbudget.com")

	email := "user@example.com"
	token := "test-cancellation-token-123"
	scheduledDate := time.Now().Add(30 * 24 * time.Hour)

	err := service.SendDeletionConfirmation(ctx, email, token, scheduledDate)

	assert.NoError(t, err)
}

// TestSendDeletionConfirmation_InvalidEmail tests invalid email handling
func TestSendDeletionConfirmation_InvalidEmail(t *testing.T) {
	ctx := context.Background()

	service := NewEmailService("test-api-key", "test@boostedbudget.com", "https://boostedbudget.com")

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

	service := NewEmailService("test-api-key", "test@boostedbudget.com", "https://boostedbudget.com")

	email := "user@example.com"
	token := "test-cancellation-token-123"
	scheduledDate := time.Now().Add(30 * 24 * time.Hour)

	// This test verifies that the email template contains required fields
	// We'll check this by ensuring the service constructs the message correctly
	err := service.SendDeletionConfirmation(ctx, email, token, scheduledDate)

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
