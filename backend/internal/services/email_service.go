package services

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/sendgrid/sendgrid-go"
	"github.com/sendgrid/sendgrid-go/helpers/mail"
)

// EmailService defines the interface for email operations
type EmailService interface {
	// SendDeletionConfirmation sends account deletion confirmation email
	SendDeletionConfirmation(ctx context.Context, toEmail, cancellationToken string, scheduledDate time.Time) error

	// SendNotificationEmail sends an email notification to a user.
	// Issue #257: Item Error Handler - User Notifications
	//
	// MVP Implementation: Stub that logs to console (no real email sending).
	// Phase 3+: Implement real email sending via SendGrid.
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeout control
	//   - toEmail: User's email address
	//   - notification: The notification containing email content
	//
	// Returns error if email sending fails.
	// Note: Callers should handle errors gracefully (non-blocking).
	SendNotificationEmail(ctx context.Context, toEmail string, notification *domain.Notification) error
}

// emailService implements EmailService using SendGrid
type emailService struct {
	apiKey      string
	fromEmail   string
	fromName    string
	frontendURL string
	client      *sendgrid.Client
	mockMode    bool // For testing without real API calls
}

// NewEmailService creates a new EmailService instance
func NewEmailService(apiKey, fromEmail, frontendURL string) EmailService {
	return &emailService{
		apiKey:      apiKey,
		fromEmail:   fromEmail,
		fromName:    "BoostedBudget",
		frontendURL: frontendURL,
		client:      sendgrid.NewSendClient(apiKey),
		mockMode:    false,
	}
}

// NewEmailServiceMock creates a new EmailService instance in mock mode for testing
func NewEmailServiceMock(apiKey, fromEmail, frontendURL string) EmailService {
	return &emailService{
		apiKey:      apiKey,
		fromEmail:   fromEmail,
		fromName:    "BoostedBudget",
		frontendURL: frontendURL,
		client:      nil, // No real client in mock mode
		mockMode:    true,
	}
}

// SendDeletionConfirmation sends account deletion confirmation email with cancellation link
func (s *emailService) SendDeletionConfirmation(ctx context.Context, toEmail, cancellationToken string, scheduledDate time.Time) error {
	// Validate email format
	if !isValidEmail(toEmail) {
		return fmt.Errorf("invalid email address: %s", toEmail)
	}

	// Construct cancellation URL
	cancellationURL := fmt.Sprintf("%s/cancel-deletion/%s", s.frontendURL, cancellationToken)

	// Format scheduled date
	formattedDate := scheduledDate.Format("January 2, 2006 at 3:04 PM MST")

	// Build email content
	subject := "Account Deletion Scheduled - Action Required"
	htmlContent := s.buildDeletionEmailHTML(cancellationURL, formattedDate)
	plainTextContent := s.buildDeletionEmailPlainText(cancellationURL, formattedDate)

	// Create SendGrid message
	from := mail.NewEmail(s.fromName, s.fromEmail)
	to := mail.NewEmail("", toEmail)
	message := mail.NewSingleEmail(from, subject, to, plainTextContent, htmlContent)

	// In mock mode, skip actual API call
	if s.mockMode {
		// Just validate the message was constructed successfully
		return nil
	}

	// Send email via SendGrid
	response, err := s.client.SendWithContext(ctx, message)
	if err != nil {
		return fmt.Errorf("failed to send email via SendGrid: %w", err)
	}

	// Check response status
	if response.StatusCode >= 400 {
		return fmt.Errorf("SendGrid API error (status %d): %s", response.StatusCode, response.Body)
	}

	return nil
}

// SendNotificationEmail sends a notification email (MVP: stub implementation).
// Issue #257: Item Error Handler - User Notifications
//
// MVP Implementation: Logs email details to console without actually sending.
// This allows the notification system to work without requiring email infrastructure.
//
// Future (Phase 3+): Replace with real SendGrid email sending:
//   - Create HTML/plain text templates for each notification type
//   - Send via SendGrid API
//   - Track delivery status
//   - Handle bounce/failure notifications
//
// Parameters:
//   - ctx: Context for cancellation and timeout
//   - toEmail: User's email address
//   - notification: Notification containing email content
//
// Returns: Always nil in MVP (stub always succeeds). Real implementation will return errors.
func (s *emailService) SendNotificationEmail(ctx context.Context, toEmail string, notification *domain.Notification) error {
	// MVP STUB: Log email details to console (no actual sending)
	log.Printf("[EMAIL STUB] Would send notification email:")
	log.Printf("  To: %s", toEmail)
	log.Printf("  User ID: %s", notification.UserID)
	log.Printf("  Type: %s", notification.Type)
	log.Printf("  Title: %s", notification.Title)
	log.Printf("  Message: %s", notification.Message)
	if notification.ActionURL != nil {
		log.Printf("  Action URL: %s", *notification.ActionURL)
	}
	if notification.ActionLabel != nil {
		log.Printf("  Action Label: %s", *notification.ActionLabel)
	}

	// MVP: Always succeeds (no real sending)
	// Phase 3+: Will return actual sending errors
	return nil
}

// buildDeletionEmailHTML creates HTML email template for deletion confirmation
func (s *emailService) buildDeletionEmailHTML(cancellationURL, scheduledDate string) string {
	return fmt.Sprintf(`
<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <title>Account Deletion Scheduled</title>
</head>
<body style="font-family: Arial, sans-serif; line-height: 1.6; color: #333; max-width: 600px; margin: 0 auto; padding: 20px;">
    <div style="background-color: #f8f9fa; border-radius: 8px; padding: 30px; border: 1px solid #dee2e6;">
        <h1 style="color: #dc3545; margin-top: 0;">Account Deletion Scheduled</h1>

        <p>We've received your request to delete your BoostedBudget account.</p>

        <div style="background-color: #fff3cd; border-left: 4px solid #ffc107; padding: 15px; margin: 20px 0;">
            <strong>Scheduled Deletion Date:</strong> %s
        </div>

        <p>Your account and all associated data will be permanently deleted on this date. This includes:</p>
        <ul style="padding-left: 20px;">
            <li>All transaction data</li>
            <li>Connected bank accounts</li>
            <li>Budget rules and automation</li>
            <li>User profile and preferences</li>
        </ul>

        <div style="background-color: #d1ecf1; border-left: 4px solid #0c5460; padding: 15px; margin: 20px 0;">
            <strong>Changed your mind?</strong><br>
            You can cancel the deletion at any time before the scheduled date by clicking the button below:
        </div>

        <div style="text-align: center; margin: 30px 0;">
            <a href="%s"
               style="background-color: #28a745; color: white; padding: 12px 30px; text-decoration: none; border-radius: 5px; display: inline-block; font-weight: bold;">
                Cancel Account Deletion
            </a>
        </div>

        <p style="color: #6c757d; font-size: 14px; margin-top: 30px;">
            If you did not request account deletion, please click the cancellation link immediately and contact our support team.
        </p>

        <hr style="border: none; border-top: 1px solid #dee2e6; margin: 30px 0;">

        <p style="color: #6c757d; font-size: 12px; text-align: center;">
            This email was sent to you because an account deletion was requested for your BoostedBudget account.<br>
            If you're unable to click the button, copy and paste this URL into your browser:<br>
            <a href="%s" style="color: #007bff;">%s</a>
        </p>
    </div>
</body>
</html>
`, scheduledDate, cancellationURL, cancellationURL, cancellationURL)
}

// buildDeletionEmailPlainText creates plain text email template for deletion confirmation
func (s *emailService) buildDeletionEmailPlainText(cancellationURL, scheduledDate string) string {
	return fmt.Sprintf(`
ACCOUNT DELETION SCHEDULED

We've received your request to delete your BoostedBudget account.

Scheduled Deletion Date: %s

Your account and all associated data will be permanently deleted on this date, including:
- All transaction data
- Connected bank accounts
- Budget rules and automation
- User profile and preferences

CHANGED YOUR MIND?

You can cancel the deletion at any time before the scheduled date by visiting:
%s

If you did not request account deletion, please use the cancellation link immediately and contact our support team.

---

This email was sent to you because an account deletion was requested for your BoostedBudget account.
If the link doesn't work, copy and paste this URL into your browser: %s
`, scheduledDate, cancellationURL, cancellationURL)
}

// isValidEmail validates email format using regex
func isValidEmail(email string) bool {
	// Basic email validation regex
	emailRegex := regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)
	return emailRegex.MatchString(email)
}
