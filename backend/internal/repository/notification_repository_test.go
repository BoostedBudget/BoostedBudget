package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNotificationRepository_Create tests creating notifications
func TestNotificationRepository_Create(t *testing.T) {
	// TODO: Implement once test database setup is in place
	t.Skip("TDD RED: Test will be implemented with test database")

	repo := setupTestNotificationRepository(t)
	ctx := context.Background()

	userID := uuid.New().String()

	tests := []struct {
		name          string
		notification  *domain.Notification
		wantErr       bool
		errorContains string
	}{
		{
			name: "valid notification",
			notification: &domain.Notification{
				UserID:      userID,
				Type:        "login_required",
				Title:       "Reconnect Bank",
				Message:     "Please reconnect your bank account",
				ActionURL:   stringPtr("/accounts/reconnect"),
				ActionLabel: stringPtr("Reconnect"),
			},
			wantErr: false,
		},
		{
			name: "notification without action URL",
			notification: &domain.Notification{
				UserID:  userID,
				Type:    "item_error",
				Title:   "Connection Error",
				Message: "Error connecting to your bank",
			},
			wantErr: false,
		},
		{
			name: "invalid user ID",
			notification: &domain.Notification{
				UserID:  "invalid-uuid",
				Type:    "login_required",
				Title:   "Test",
				Message: "Test message",
			},
			wantErr:       true,
			errorContains: "invalid user",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := repo.Create(ctx, tt.notification)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				require.NoError(t, err)
				assert.NotEmpty(t, tt.notification.ID)
				assert.False(t, tt.notification.CreatedAt.IsZero())
			}
		})
	}
}

// TestNotificationRepository_GetByUserID tests retrieving notifications for a user
func TestNotificationRepository_GetByUserID(t *testing.T) {
	t.Skip("TDD RED: Test will be implemented with test database")

	repo := setupTestNotificationRepository(t)
	ctx := context.Background()

	userID := uuid.New().String()

	// Create test notifications
	notifications := []domain.Notification{
		{
			UserID:  userID,
			Type:    "login_required",
			Title:   "Reconnect 1",
			Message: "Message 1",
		},
		{
			UserID:  userID,
			Type:    "item_error",
			Title:   "Error 1",
			Message: "Message 2",
		},
		{
			UserID:  userID,
			Type:    "pending_expiration",
			Title:   "Expiring",
			Message: "Message 3",
		},
	}

	for i := range notifications {
		err := repo.Create(ctx, &notifications[i])
		require.NoError(t, err)
		time.Sleep(10 * time.Millisecond) // Ensure different timestamps
	}

	tests := []struct {
		name      string
		userID    string
		limit     int
		wantCount int
	}{
		{
			name:      "get all notifications",
			userID:    userID,
			limit:     10,
			wantCount: 3,
		},
		{
			name:      "get with limit",
			userID:    userID,
			limit:     2,
			wantCount: 2,
		},
		{
			name:      "user with no notifications",
			userID:    uuid.New().String(),
			limit:     10,
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := repo.GetByUserID(ctx, tt.userID, tt.limit)
			require.NoError(t, err)
			assert.Len(t, result, tt.wantCount)

			// Verify ordering (most recent first)
			if len(result) > 1 {
				for i := 0; i < len(result)-1; i++ {
					assert.True(t, result[i].CreatedAt.After(result[i+1].CreatedAt) ||
						result[i].CreatedAt.Equal(result[i+1].CreatedAt))
				}
			}
		})
	}
}

// TestNotificationRepository_MarkAsRead tests marking notifications as read
func TestNotificationRepository_MarkAsRead(t *testing.T) {
	t.Skip("TDD RED: Test will be implemented with test database")

	repo := setupTestNotificationRepository(t)
	ctx := context.Background()

	userID := uuid.New().String()
	notification := &domain.Notification{
		UserID:  userID,
		Type:    "login_required",
		Title:   "Test",
		Message: "Test message",
	}

	err := repo.Create(ctx, notification)
	require.NoError(t, err)
	assert.False(t, notification.Read)

	tests := []struct {
		name           string
		notificationID string
		userID         string
		wantErr        bool
	}{
		{
			name:           "mark as read success",
			notificationID: notification.ID,
			userID:         userID,
			wantErr:        false,
		},
		{
			name:           "wrong user",
			notificationID: notification.ID,
			userID:         uuid.New().String(),
			wantErr:        true,
		},
		{
			name:           "non-existent notification",
			notificationID: uuid.New().String(),
			userID:         userID,
			wantErr:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := repo.MarkAsRead(ctx, tt.notificationID, tt.userID)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)

				// Verify notification is marked as read
				notifications, err := repo.GetByUserID(ctx, userID, 10)
				require.NoError(t, err)
				found := false
				for _, n := range notifications {
					if n.ID == tt.notificationID {
						assert.True(t, n.Read)
						found = true
						break
					}
				}
				assert.True(t, found, "notification should be found")
			}
		})
	}
}

// TestNotificationRepository_GetUnreadCount tests counting unread notifications
func TestNotificationRepository_GetUnreadCount(t *testing.T) {
	t.Skip("TDD RED: Test will be implemented with test database")

	repo := setupTestNotificationRepository(t)
	ctx := context.Background()

	userID := uuid.New().String()

	// Create 3 notifications
	for i := 0; i < 3; i++ {
		notification := &domain.Notification{
			UserID:  userID,
			Type:    "login_required",
			Title:   "Test",
			Message: "Test message",
		}
		err := repo.Create(ctx, notification)
		require.NoError(t, err)

		// Mark first one as read
		if i == 0 {
			err = repo.MarkAsRead(ctx, notification.ID, userID)
			require.NoError(t, err)
		}
	}

	count, err := repo.GetUnreadCount(ctx, userID)
	require.NoError(t, err)
	assert.Equal(t, 2, count)
}

// TestNotificationRepository_GetUnsentEmails tests retrieving unsent email notifications
func TestNotificationRepository_GetUnsentEmails(t *testing.T) {
	t.Skip("TDD RED: Test will be implemented with test database")

	repo := setupTestNotificationRepository(t)
	ctx := context.Background()

	// Create notifications with different email states
	for i := 0; i < 3; i++ {
		notification := &domain.Notification{
			UserID:    uuid.New().String(),
			Type:      "login_required",
			Title:     "Test",
			Message:   "Test message",
			SentEmail: i == 0, // First one already sent
		}
		if i == 0 {
			now := time.Now()
			notification.EmailSentAt = &now
		}
		err := repo.Create(ctx, notification)
		require.NoError(t, err)
		time.Sleep(10 * time.Millisecond)
	}

	// Should return only unsent emails (2 of them)
	unsent, err := repo.GetUnsentEmails(ctx, 10)
	require.NoError(t, err)
	assert.Len(t, unsent, 2)

	// Verify all are unsent
	for _, n := range unsent {
		assert.False(t, n.SentEmail)
		assert.Nil(t, n.EmailSentAt)
	}

	// Verify ordering (oldest first for FIFO processing)
	if len(unsent) > 1 {
		for i := 0; i < len(unsent)-1; i++ {
			assert.True(t, unsent[i].CreatedAt.Before(unsent[i+1].CreatedAt) ||
				unsent[i].CreatedAt.Equal(unsent[i+1].CreatedAt))
		}
	}
}

// Helper functions

func setupTestNotificationRepository(t *testing.T) NotificationRepository {
	// TODO: Setup test database connection
	// For now, return nil - tests are skipped
	return nil
}

func stringPtr(s string) *string {
	return &s
}
