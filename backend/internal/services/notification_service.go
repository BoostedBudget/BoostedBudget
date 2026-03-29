// Package services provides budget alert notification functionality.
// Issue #36: Rule Executors - Budget alert notifications.
package services

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// BudgetAlertNotificationService handles sending budget alert notifications.
// It creates in-app notifications (critical) and optionally sends emails (non-blocking).
type BudgetAlertNotificationService interface {
	// SendBudgetAlert sends a budget alert notification to a user.
	// Creates in-app notification (returns error on failure).
	// Sends email if "email" is in notification channels (non-blocking, logs errors).
	SendBudgetAlert(ctx context.Context, userID string, alert *domain.AlertAction, data map[string]interface{}) error
}

// UserRepository is a minimal interface for getting user info for notifications.
// Uses only the methods we need to avoid tight coupling.
type UserRepository interface {
	GetUserByID(ctx context.Context, id string) (*domain.User, error)
}

// budgetAlertNotificationService implements BudgetAlertNotificationService.
type budgetAlertNotificationService struct {
	logger           *slog.Logger
	notificationRepo repository.NotificationRepository
	userRepo         UserRepository
	emailService     EmailService
}

// NewBudgetAlertNotificationService creates a new BudgetAlertNotificationService.
func NewBudgetAlertNotificationService(
	notificationRepo repository.NotificationRepository,
	userRepo UserRepository,
	emailService EmailService,
	logger *slog.Logger,
) BudgetAlertNotificationService {
	if logger == nil {
		logger = slog.Default()
	}
	return &budgetAlertNotificationService{
		logger:           logger,
		notificationRepo: notificationRepo,
		userRepo:         userRepo,
		emailService:     emailService,
	}
}

// Default templates for budget alerts.
const (
	defaultBudgetAlertTitle   = "Budget Alert"
	defaultBudgetAlertMessage = "Your budget has reached the alert threshold."
)

// SendBudgetAlert sends a budget alert notification.
// In-app notification is critical (returns error on failure).
// Email is non-blocking (logs errors but doesn't fail the operation).
func (s *budgetAlertNotificationService) SendBudgetAlert(
	ctx context.Context,
	userID string,
	alert *domain.AlertAction,
	data map[string]interface{},
) error {
	// 1. Build notification content from templates
	title := alert.TitleTemplate
	if title == "" {
		title = defaultBudgetAlertTitle
	}
	title = interpolateTemplate(title, data)

	message := alert.MessageTemplate
	if message == "" {
		message = defaultBudgetAlertMessage
	}
	message = interpolateTemplate(message, data)

	// 2. Create in-app notification (CRITICAL - return error on failure)
	notification := &domain.Notification{
		UserID:    userID,
		Type:      "budget_alert",
		Title:     title,
		Message:   message,
		Read:      false,
		SentEmail: false,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if err := s.notificationRepo.Create(ctx, notification); err != nil {
		return fmt.Errorf("failed to create in-app notification: %w", err)
	}

	// 3. Check if email should be sent
	sendEmail := false
	for _, channel := range alert.NotificationChannels {
		if channel == "email" {
			sendEmail = true
			break
		}
	}

	if sendEmail {
		// Email is non-blocking - log errors but don't fail the operation
		s.sendEmailAsync(ctx, userID, notification)
	}

	return nil
}

// sendEmailAsync sends an email notification (non-blocking).
// Logs errors but doesn't return them - email failure shouldn't fail the operation.
func (s *budgetAlertNotificationService) sendEmailAsync(
	ctx context.Context,
	userID string,
	notification *domain.Notification,
) {
	// Look up user email
	user, err := s.userRepo.GetUserByID(ctx, userID)
	if err != nil {
		s.logger.Error("failed to get user email for budget alert", "user_id", userID, "error", err)
		return
	}

	if user.Email == "" {
		s.logger.Warn("user has no email address for budget alert", "user_id", userID)
		return
	}

	// Send the email
	if err := s.emailService.SendNotificationEmail(ctx, user.Email, notification); err != nil {
		s.logger.Error("failed to send budget alert email", "email", user.Email, "error", err)
		return
	}

	s.logger.Info("budget alert email sent", "email", user.Email, "notification_id", notification.ID)
}

// interpolateTemplate replaces {{key}} placeholders with values from data.
// Missing keys are left as-is (not replaced).
func interpolateTemplate(template string, data map[string]interface{}) string {
	if template == "" {
		return ""
	}

	// Match {{key}} pattern
	re := regexp.MustCompile(`\{\{(\w+)\}\}`)

	result := re.ReplaceAllStringFunc(template, func(match string) string {
		// Extract key name (remove {{ and }})
		key := strings.TrimPrefix(match, "{{")
		key = strings.TrimSuffix(key, "}}")

		// Look up value
		if value, ok := data[key]; ok {
			return fmt.Sprintf("%v", value)
		}
		// Key not found - return original placeholder
		return match
	})

	return result
}
