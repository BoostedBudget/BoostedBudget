package services

import (
	"context"
	"fmt"
	"log/slog"
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

	// SendHouseholdInvite sends a household invitation email containing inviteURL.
	// Callers should treat delivery as best-effort (#1367 decision #4) — the invite
	// create response also returns the raw-token link so an owner can share it
	// manually if transactional email isn't fully configured (see #1026).
	SendHouseholdInvite(ctx context.Context, toEmail, inviterName, householdName, inviteURL string, expiresAt time.Time) error

	// SendHouseholdMemberRemoved sends a "you've been removed from a household" email:
	// the recipient's own data is fully preserved, and the historical membership record
	// will be purged automatically on purgeDate if no action is taken. Callers should
	// treat delivery as best-effort — the removal itself is already committed by the
	// time this is called, so a failed send must never be surfaced as a removal error.
	// #1371 — Phase 7 (Task 3).
	SendHouseholdMemberRemoved(ctx context.Context, toEmail, householdName string, purgeDate time.Time) error
}

// emailService implements EmailService using SendGrid
type emailService struct {
	logger      *slog.Logger
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
		logger:      slog.Default(),
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
		logger:      slog.Default(),
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
	// MVP STUB: Log email details (no actual sending)
	args := []any{
		"to", toEmail,
		"user_id", notification.UserID,
		"type", notification.Type,
		"title", notification.Title,
	}
	if notification.ActionURL != nil {
		args = append(args, "action_url", *notification.ActionURL)
	}
	s.logger.Info("email stub: would send notification email", args...)

	// MVP: Always succeeds (no real sending)
	// Phase 3+: Will return actual sending errors
	return nil
}

// SendHouseholdInvite sends a household invitation email with a link the invitee
// uses to preview and accept. Best-effort — see interface doc.
func (s *emailService) SendHouseholdInvite(ctx context.Context, toEmail, inviterName, householdName, inviteURL string, expiresAt time.Time) error {
	if !isValidEmail(toEmail) {
		return fmt.Errorf("invalid email address: %s", toEmail)
	}

	formattedExpiry := expiresAt.Format("January 2, 2006")
	subject := fmt.Sprintf("%s invited you to join %s on BoostedBudget", inviterName, householdName)
	htmlContent := s.buildHouseholdInviteEmailHTML(inviterName, householdName, inviteURL, formattedExpiry)
	plainTextContent := s.buildHouseholdInviteEmailPlainText(inviterName, householdName, inviteURL, formattedExpiry)

	from := mail.NewEmail(s.fromName, s.fromEmail)
	to := mail.NewEmail("", toEmail)
	message := mail.NewSingleEmail(from, subject, to, plainTextContent, htmlContent)

	if s.mockMode {
		return nil
	}

	response, err := s.client.SendWithContext(ctx, message)
	if err != nil {
		return fmt.Errorf("failed to send email via SendGrid: %w", err)
	}
	if response.StatusCode >= 400 {
		return fmt.Errorf("SendGrid API error (status %d): %s", response.StatusCode, response.Body)
	}
	return nil
}

// SendHouseholdMemberRemoved sends a "you've been removed from a household" email.
// Best-effort — see interface doc.
func (s *emailService) SendHouseholdMemberRemoved(ctx context.Context, toEmail, householdName string, purgeDate time.Time) error {
	if !isValidEmail(toEmail) {
		return fmt.Errorf("invalid email address: %s", toEmail)
	}

	subject := fmt.Sprintf("You've been removed from %s on BoostedBudget", householdName)
	htmlContent := s.buildMemberRemovedEmailHTML(householdName, purgeDate)
	plainTextContent := s.buildMemberRemovedEmailPlainText(householdName, purgeDate)

	from := mail.NewEmail(s.fromName, s.fromEmail)
	to := mail.NewEmail("", toEmail)
	message := mail.NewSingleEmail(from, subject, to, plainTextContent, htmlContent)

	if s.mockMode {
		return nil
	}

	response, err := s.client.SendWithContext(ctx, message)
	if err != nil {
		return fmt.Errorf("failed to send email via SendGrid: %w", err)
	}
	if response.StatusCode >= 400 {
		return fmt.Errorf("SendGrid API error (status %d): %s", response.StatusCode, response.Body)
	}
	return nil
}

// buildHouseholdInviteEmailHTML creates the HTML email template for a household invite.
func (s *emailService) buildHouseholdInviteEmailHTML(inviterName, householdName, inviteURL, formattedExpiry string) string {
	return fmt.Sprintf(`
<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <title>Household Invitation</title>
</head>
<body style="font-family: Arial, sans-serif; line-height: 1.6; color: #333; max-width: 600px; margin: 0 auto; padding: 20px;">
    <div style="background-color: #f8f9fa; border-radius: 8px; padding: 30px; border: 1px solid #dee2e6;">
        <h1 style="color: #2563eb; margin-top: 0;">You're invited to join %s</h1>

        <p><strong>%s</strong> has invited you to join their household, %s, on BoostedBudget.</p>

        <p>Joining a household shares your accounts with the household by default — you'll be able to
        review exactly what will be shared and choose to keep any account private before you join.</p>

        <div style="text-align: center; margin: 30px 0;">
            <a href="%s"
               style="background-color: #2563eb; color: white; padding: 12px 30px; text-decoration: none; border-radius: 5px; display: inline-block; font-weight: bold;">
                View Invitation
            </a>
        </div>

        <p style="color: #6c757d; font-size: 14px; margin-top: 30px;">
            This invitation expires on %s. If you weren't expecting it, you can safely ignore this email.
        </p>

        <hr style="border: none; border-top: 1px solid #dee2e6; margin: 30px 0;">

        <p style="color: #6c757d; font-size: 12px; text-align: center;">
            If you're unable to click the button, copy and paste this URL into your browser:<br>
            <a href="%s" style="color: #007bff;">%s</a>
        </p>
    </div>
</body>
</html>
`, householdName, inviterName, householdName, inviteURL, formattedExpiry, inviteURL, inviteURL)
}

// buildHouseholdInviteEmailPlainText creates the plain text email template for a household invite.
func (s *emailService) buildHouseholdInviteEmailPlainText(inviterName, householdName, inviteURL, formattedExpiry string) string {
	return fmt.Sprintf(`
YOU'RE INVITED TO JOIN %s

%s has invited you to join their household, %s, on BoostedBudget.

Joining a household shares your accounts with the household by default — you'll be able to
review exactly what will be shared and choose to keep any account private before you join.

View the invitation:
%s

This invitation expires on %s. If you weren't expecting it, you can safely ignore this email.
`, householdName, inviterName, householdName, inviteURL, formattedExpiry)
}

// buildMemberRemovedEmailHTML creates the HTML email template for a household
// member-removed notification.
func (s *emailService) buildMemberRemovedEmailHTML(householdName string, purgeDate time.Time) string {
	formattedPurgeDate := purgeDate.Format("January 2, 2006")
	return fmt.Sprintf(`
<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <title>Removed from Household</title>
</head>
<body style="font-family: Arial, sans-serif; line-height: 1.6; color: #333; max-width: 600px; margin: 0 auto; padding: 20px;">
    <div style="background-color: #f8f9fa; border-radius: 8px; padding: 30px; border: 1px solid #dee2e6;">
        <h1 style="color: #2563eb; margin-top: 0;">You've been removed from %s</h1>

        <p>You are no longer a member of the household <strong>%s</strong> on BoostedBudget.</p>

        <div style="background-color: #d1ecf1; border-left: 4px solid #0c5460; padding: 15px; margin: 20px 0;">
            <strong>Your data is safe.</strong> Your own accounts, transactions, budgets, and automation
            rules are fully preserved and untouched — only your household membership link is affected.
            You can continue using BoostedBudget with your own independent account.
        </div>

        <p style="color: #6c757d; font-size: 14px; margin-top: 30px;">
            If you take no action, the historical membership record will be cleaned up automatically
            after the preservation window, on %s.
        </p>
    </div>
</body>
</html>
`, householdName, householdName, formattedPurgeDate)
}

// buildMemberRemovedEmailPlainText creates the plain text email template for a
// household member-removed notification.
func (s *emailService) buildMemberRemovedEmailPlainText(householdName string, purgeDate time.Time) string {
	formattedPurgeDate := purgeDate.Format("January 2, 2006")
	return fmt.Sprintf(`
YOU'VE BEEN REMOVED FROM %s

You are no longer a member of the household %s on BoostedBudget.

YOUR DATA IS SAFE

Your own accounts, transactions, budgets, and automation rules are fully preserved and
untouched — only your household membership link is affected. You can continue using
BoostedBudget with your own independent account.

If you take no action, the historical membership record will be cleaned up automatically
after the preservation window, on %s.
`, householdName, householdName, formattedPurgeDate)
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
