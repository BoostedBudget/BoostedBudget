package repository

import (
	"context"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// NotificationRepository defines the interface for notification data access.
// Issue #257: Item Error Handler - User Notifications
//
// Supports in-app notifications for:
// - Item errors (connection issues)
// - Login required (re-authentication needed)
// - Pending expiration (access expiring soon)
// - Permission revoked (user revoked access)
// - Transaction sync updates
type NotificationRepository interface {
	// Create creates a new notification in the database
	Create(ctx context.Context, notification *domain.Notification) error

	// Update updates an existing notification (e.g., mark as read, email sent)
	Update(ctx context.Context, notification *domain.Notification) error

	// GetByUserID retrieves notifications for a specific user with pagination
	// Returns notifications ordered by created_at DESC (most recent first)
	GetByUserID(ctx context.Context, userID string, limit int) ([]domain.Notification, error)

	// MarkAsRead marks a notification as read
	// Returns error if notification not found or doesn't belong to user
	MarkAsRead(ctx context.Context, notificationID string, userID string) error

	// GetUnreadCount returns the count of unread notifications for a user
	GetUnreadCount(ctx context.Context, userID string) (int, error)

	// GetUnsentEmails retrieves notifications where sent_email=false
	// Used by email worker to send pending email notifications
	// Returns notifications ordered by created_at ASC (oldest first, FIFO)
	GetUnsentEmails(ctx context.Context, limit int) ([]domain.Notification, error)

	// MarkAllAsRead marks all unread notifications as read for a user.
	// Returns the number of notifications marked as read.
	MarkAllAsRead(ctx context.Context, userID string) (int64, error)
}
