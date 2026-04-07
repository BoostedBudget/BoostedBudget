package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	appcrypto "github.com/maxnussbaum/BoostedBudget/internal/crypto"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

var (
	// ErrNotificationNotFound is returned when a notification is not found in the database
	ErrNotificationNotFound = errors.New("notification not found")
	// ErrNotificationAccessDenied is returned when user tries to access notification they don't own
	ErrNotificationAccessDenied = errors.New("access denied: notification does not belong to user")
)

// notificationRepository implements NotificationRepository using PostgreSQL
type notificationRepository struct {
	db  DB
	enc *appcrypto.FieldEncryptor
}

// NewNotificationRepository creates a new NotificationRepository instance
func NewNotificationRepository(db DB, enc *appcrypto.FieldEncryptor) NotificationRepository {
	return &notificationRepository{db: db, enc: enc}
}

// Create creates a new notification in the database
func (r *notificationRepository) Create(ctx context.Context, notification *domain.Notification) error {
	// Generate UUID if not already set
	if notification.ID == "" {
		notification.ID = uuid.New().String()
	}

	// Set timestamps
	now := time.Now()
	notification.CreatedAt = now
	notification.UpdatedAt = now

	// Validate before insert
	if err := notification.Validate(); err != nil {
		return fmt.Errorf("invalid notification: %w", err)
	}

	encActionURL, err := r.enc.EncryptNullableString(notification.ActionURL)
	if err != nil {
		return fmt.Errorf("encrypt action_url: %w", err)
	}

	query := `
		INSERT INTO notifications
		(id, user_id, type, title, message, action_url, action_label,
		 read, sent_email, email_sent_at, interaction_pattern, transfer_suggestion_id,
		 created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
	`

	_, err = r.db.Exec(ctx, query,
		notification.ID,
		notification.UserID,
		notification.Type,
		notification.Title,
		notification.Message,
		encActionURL,
		notification.ActionLabel,
		notification.Read,
		notification.SentEmail,
		notification.EmailSentAt,
		notification.InteractionPattern,
		notification.TransferSuggestionID,
		notification.CreatedAt,
		notification.UpdatedAt,
	)

	if err != nil {
		// Check for foreign key violations (invalid user_id)
		if strings.Contains(err.Error(), "foreign key constraint") ||
			strings.Contains(err.Error(), "violates foreign key") {
			return fmt.Errorf("invalid user_id: user does not exist: %w", err)
		}
		return fmt.Errorf("failed to create notification: %w", err)
	}

	return nil
}

// Update updates an existing notification (e.g., mark as read, email sent)
func (r *notificationRepository) Update(ctx context.Context, notification *domain.Notification) error {
	// Update timestamp
	notification.UpdatedAt = time.Now()

	encActionURL, err := r.enc.EncryptNullableString(notification.ActionURL)
	if err != nil {
		return fmt.Errorf("encrypt action_url: %w", err)
	}

	query := `
		UPDATE notifications
		SET type = $2,
		    title = $3,
		    message = $4,
		    action_url = $5,
		    action_label = $6,
		    read = $7,
		    sent_email = $8,
		    email_sent_at = $9,
		    interaction_pattern = $10,
		    transfer_suggestion_id = $11,
		    updated_at = $12
		WHERE id = $1
	`

	ct, err := r.db.Exec(ctx, query,
		notification.ID,
		notification.Type,
		notification.Title,
		notification.Message,
		encActionURL,
		notification.ActionLabel,
		notification.Read,
		notification.SentEmail,
		notification.EmailSentAt,
		notification.InteractionPattern,
		notification.TransferSuggestionID,
		notification.UpdatedAt,
	)

	if err != nil {
		return fmt.Errorf("failed to update notification: %w", err)
	}

	if ct.RowsAffected() == 0 {
		return ErrNotificationNotFound
	}

	return nil
}

// decryptNotification decrypts the encrypted fields on a Notification in place.
func (r *notificationRepository) decryptNotification(n *domain.Notification) error {
	var err error
	n.ActionURL, err = r.enc.DecryptNullableString(n.ActionURL)
	if err != nil {
		return fmt.Errorf("decrypt action_url: %w", err)
	}
	return nil
}

// GetByUserID retrieves notifications for a specific user with pagination
// Returns notifications ordered by created_at DESC (most recent first)
func (r *notificationRepository) GetByUserID(ctx context.Context, userID string, limit int) ([]domain.Notification, error) {
	query := `
		SELECT id, user_id, type, title, message, action_url, action_label,
		       read, sent_email, email_sent_at, interaction_pattern, transfer_suggestion_id,
		       created_at, updated_at
		FROM notifications
		WHERE user_id = $1
		ORDER BY created_at DESC
		LIMIT $2
	`

	rows, err := r.db.Query(ctx, query, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query notifications: %w", err)
	}
	defer rows.Close()

	var notifications []domain.Notification
	for rows.Next() {
		var n domain.Notification
		err := rows.Scan(
			&n.ID,
			&n.UserID,
			&n.Type,
			&n.Title,
			&n.Message,
			&n.ActionURL,
			&n.ActionLabel,
			&n.Read,
			&n.SentEmail,
			&n.EmailSentAt,
			&n.InteractionPattern,
			&n.TransferSuggestionID,
			&n.CreatedAt,
			&n.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan notification: %w", err)
		}
		if err := r.decryptNotification(&n); err != nil {
			return nil, fmt.Errorf("failed to decrypt notification: %w", err)
		}
		notifications = append(notifications, n)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating notifications: %w", err)
	}

	// Return empty slice instead of nil if no results
	if notifications == nil {
		notifications = []domain.Notification{}
	}

	return notifications, nil
}

// MarkAsRead marks a notification as read
// Returns error if notification not found or doesn't belong to user
func (r *notificationRepository) MarkAsRead(ctx context.Context, notificationID string, userID string) error {
	query := `
		UPDATE notifications
		SET read = true,
		    updated_at = $3
		WHERE id = $1 AND user_id = $2
	`

	ct, err := r.db.Exec(ctx, query, notificationID, userID, time.Now())
	if err != nil {
		return fmt.Errorf("failed to mark notification as read: %w", err)
	}

	if ct.RowsAffected() == 0 {
		// Check if notification exists at all
		var exists bool
		checkQuery := `SELECT EXISTS(SELECT 1 FROM notifications WHERE id = $1)`
		err := r.db.QueryRow(ctx, checkQuery, notificationID).Scan(&exists)
		if err != nil {
			return fmt.Errorf("failed to check notification existence: %w", err)
		}

		if !exists {
			return ErrNotificationNotFound
		}

		// Notification exists but doesn't belong to this user
		return ErrNotificationAccessDenied
	}

	return nil
}

// GetUnreadCount returns the count of unread notifications for a user
func (r *notificationRepository) GetUnreadCount(ctx context.Context, userID string) (int, error) {
	query := `
		SELECT COUNT(*)
		FROM notifications
		WHERE user_id = $1 AND read = false
	`

	var count int
	err := r.db.QueryRow(ctx, query, userID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count unread notifications: %w", err)
	}

	return count, nil
}

// GetUnsentEmails retrieves notifications where sent_email=false
// Used by email worker to send pending email notifications
// Returns notifications ordered by created_at ASC (oldest first, FIFO)
func (r *notificationRepository) GetUnsentEmails(ctx context.Context, limit int) ([]domain.Notification, error) {
	query := `
		SELECT id, user_id, type, title, message, action_url, action_label,
		       read, sent_email, email_sent_at, interaction_pattern, transfer_suggestion_id,
		       created_at, updated_at
		FROM notifications
		WHERE sent_email = false
		ORDER BY created_at ASC
		LIMIT $1
	`

	rows, err := r.db.Query(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query unsent email notifications: %w", err)
	}
	defer rows.Close()

	var notifications []domain.Notification
	for rows.Next() {
		var n domain.Notification
		err := rows.Scan(
			&n.ID,
			&n.UserID,
			&n.Type,
			&n.Title,
			&n.Message,
			&n.ActionURL,
			&n.ActionLabel,
			&n.Read,
			&n.SentEmail,
			&n.EmailSentAt,
			&n.InteractionPattern,
			&n.TransferSuggestionID,
			&n.CreatedAt,
			&n.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan notification: %w", err)
		}
		if err := r.decryptNotification(&n); err != nil {
			return nil, fmt.Errorf("failed to decrypt notification: %w", err)
		}
		notifications = append(notifications, n)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating unsent notifications: %w", err)
	}

	// Return empty slice instead of nil if no results
	if notifications == nil {
		notifications = []domain.Notification{}
	}

	return notifications, nil
}

// Compile-time check to ensure notificationRepository implements NotificationRepository
var _ NotificationRepository = (*notificationRepository)(nil)
