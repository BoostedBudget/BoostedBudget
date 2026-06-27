package repository

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/pashagolub/pgxmock/v4"
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

// =============================================================================
// pgxmock-based unit tests (no DATABASE_URL required)
// =============================================================================

func setupNotificationMockDB(t *testing.T) (pgxmock.PgxPoolIface, NotificationRepository) {
	t.Helper()
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	enc := newTestFieldEncryptor(t)
	repo := NewNotificationRepository(mock, enc)
	return mock, repo
}

// notificationColumns matches the SELECT order in GetByUserID/GetUnsentEmails/GetOpenByTransferSuggestionID.
var notificationColumns = []string{
	"id", "user_id", "type", "title", "message", "action_url", "action_label",
	"read", "sent_email", "email_sent_at", "interaction_pattern", "transfer_suggestion_id",
	"created_at", "updated_at",
}

func makeNotificationRow(id, userID string) []interface{} {
	return []interface{}{
		id,
		userID,
		"item_error",
		"Test Title",
		"Test message body",
		(*string)(nil), // action_url
		(*string)(nil), // action_label
		false,          // read
		false,          // sent_email
		(*time.Time)(nil), // email_sent_at
		"informational", // interaction_pattern
		(*string)(nil), // transfer_suggestion_id
		time.Now(),
		time.Now(),
	}
}

// --- Constructor ---

func TestNewNotificationRepository_ReturnsNonNil(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()
	enc := newTestFieldEncryptor(t)
	repo := NewNotificationRepository(mock, enc)
	assert.NotNil(t, repo)
}

// --- Create ---

func TestNotificationRepository_Create_Pgxmock_Success(t *testing.T) {
	mock, repo := setupNotificationMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	n := &domain.Notification{
		UserID:             uuid.New().String(),
		Type:               "item_error",
		Title:              "Test",
		Message:            "Test message",
		InteractionPattern: "informational",
	}

	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO notifications`)).
		WithArgs(
			pgxmock.AnyArg(), // id (generated)
			n.UserID,
			n.Type,
			n.Title,
			n.Message,
			pgxmock.AnyArg(), // action_url (encrypted nil)
			pgxmock.AnyArg(), // action_label
			pgxmock.AnyArg(), // read
			pgxmock.AnyArg(), // sent_email
			pgxmock.AnyArg(), // email_sent_at
			pgxmock.AnyArg(), // interaction_pattern
			pgxmock.AnyArg(), // transfer_suggestion_id
			pgxmock.AnyArg(), // created_at
			pgxmock.AnyArg(), // updated_at
		).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	err := repo.Create(ctx, n)

	assert.NoError(t, err)
	assert.NotEmpty(t, n.ID)
	assert.False(t, n.CreatedAt.IsZero())
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestNotificationRepository_Create_Pgxmock_Error(t *testing.T) {
	mock, repo := setupNotificationMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	n := &domain.Notification{
		UserID:             uuid.New().String(),
		Type:               "item_error",
		Title:              "Test",
		Message:            "Test message",
		InteractionPattern: "informational",
	}

	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO notifications`)).
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnError(errors.New("db error"))

	err := repo.Create(ctx, n)

	assert.Error(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// --- Update ---

func TestNotificationRepository_Update_Pgxmock_Success(t *testing.T) {
	mock, repo := setupNotificationMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	n := &domain.Notification{
		ID:                 uuid.New().String(),
		UserID:             uuid.New().String(),
		Type:               "item_error",
		Title:              "Updated",
		Message:            "Updated message",
		Read:               true,
		InteractionPattern: "informational",
	}

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE notifications`)).
		WithArgs(
			n.ID,
			n.Type,
			n.Title,
			n.Message,
			pgxmock.AnyArg(), // action_url (encrypted)
			pgxmock.AnyArg(), // action_label
			n.Read,
			n.SentEmail,
			pgxmock.AnyArg(), // email_sent_at
			pgxmock.AnyArg(), // interaction_pattern
			pgxmock.AnyArg(), // transfer_suggestion_id
			pgxmock.AnyArg(), // updated_at
		).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	err := repo.Update(ctx, n)

	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestNotificationRepository_Update_Pgxmock_NotFound(t *testing.T) {
	mock, repo := setupNotificationMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	n := &domain.Notification{
		ID:                 uuid.New().String(),
		Type:               "item_error",
		Title:              "Test",
		Message:            "Test",
		InteractionPattern: "informational",
	}

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE notifications`)).
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))

	err := repo.Update(ctx, n)

	assert.ErrorIs(t, err, ErrNotificationNotFound)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// --- GetByUserID (pgxmock) ---

func TestNotificationRepository_GetByUserID_Pgxmock_Success(t *testing.T) {
	mock, repo := setupNotificationMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	notifID := uuid.New().String()

	rows := pgxmock.NewRows(notificationColumns)
	rows.AddRow(makeNotificationRow(notifID, userID)...)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, user_id, type`)).
		WithArgs(userID, 20).
		WillReturnRows(rows)

	results, err := repo.GetByUserID(ctx, userID, 20)

	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, notifID, results[0].ID)
	assert.Equal(t, userID, results[0].UserID)
	assert.Equal(t, "item_error", results[0].Type)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestNotificationRepository_GetByUserID_Pgxmock_Empty(t *testing.T) {
	mock, repo := setupNotificationMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()

	rows := pgxmock.NewRows(notificationColumns)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, user_id, type`)).
		WithArgs(userID, 20).
		WillReturnRows(rows)

	results, err := repo.GetByUserID(ctx, userID, 20)

	require.NoError(t, err)
	assert.Empty(t, results)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestNotificationRepository_GetByUserID_Pgxmock_QueryError(t *testing.T) {
	mock, repo := setupNotificationMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, user_id, type`)).
		WithArgs(userID, 20).
		WillReturnError(errors.New("db error"))

	results, err := repo.GetByUserID(ctx, userID, 20)

	assert.Nil(t, results)
	assert.Error(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// --- MarkAsRead (pgxmock) ---

func TestNotificationRepository_MarkAsRead_Pgxmock_Success(t *testing.T) {
	mock, repo := setupNotificationMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	notifID := uuid.New().String()
	userID := uuid.New().String()

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE notifications`)).
		WithArgs(notifID, userID, pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	err := repo.MarkAsRead(ctx, notifID, userID)

	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestNotificationRepository_MarkAsRead_Pgxmock_NotFound(t *testing.T) {
	mock, repo := setupNotificationMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	notifID := uuid.New().String()
	userID := uuid.New().String()

	// UPDATE returns 0 rows affected
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE notifications`)).
		WithArgs(notifID, userID, pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))

	// Existence check query - not found
	existsRows := pgxmock.NewRows([]string{"exists"}).AddRow(false)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT EXISTS`)).
		WithArgs(notifID).
		WillReturnRows(existsRows)

	err := repo.MarkAsRead(ctx, notifID, userID)

	assert.ErrorIs(t, err, ErrNotificationNotFound)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestNotificationRepository_MarkAsRead_Pgxmock_AccessDenied(t *testing.T) {
	mock, repo := setupNotificationMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	notifID := uuid.New().String()
	userID := uuid.New().String()

	// UPDATE returns 0 rows (notification exists but belongs to someone else)
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE notifications`)).
		WithArgs(notifID, userID, pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))

	// Existence check returns true
	existsRows := pgxmock.NewRows([]string{"exists"}).AddRow(true)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT EXISTS`)).
		WithArgs(notifID).
		WillReturnRows(existsRows)

	err := repo.MarkAsRead(ctx, notifID, userID)

	assert.ErrorIs(t, err, ErrNotificationAccessDenied)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// --- GetUnreadCount (pgxmock) ---

func TestNotificationRepository_GetUnreadCount_Pgxmock_Success(t *testing.T) {
	mock, repo := setupNotificationMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()

	countRows := pgxmock.NewRows([]string{"count"}).AddRow(7)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT COUNT(*)`)).
		WithArgs(userID).
		WillReturnRows(countRows)

	count, err := repo.GetUnreadCount(ctx, userID)

	require.NoError(t, err)
	assert.Equal(t, 7, count)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestNotificationRepository_GetUnreadCount_Pgxmock_Zero(t *testing.T) {
	mock, repo := setupNotificationMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()

	countRows := pgxmock.NewRows([]string{"count"}).AddRow(0)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT COUNT(*)`)).
		WithArgs(userID).
		WillReturnRows(countRows)

	count, err := repo.GetUnreadCount(ctx, userID)

	require.NoError(t, err)
	assert.Equal(t, 0, count)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// --- GetUnsentEmails (pgxmock) ---

func TestNotificationRepository_GetUnsentEmails_Pgxmock_Success(t *testing.T) {
	mock, repo := setupNotificationMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	notifID := uuid.New().String()

	rows := pgxmock.NewRows(notificationColumns)
	rows.AddRow(makeNotificationRow(notifID, userID)...)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, user_id, type`)).
		WithArgs(50).
		WillReturnRows(rows)

	results, err := repo.GetUnsentEmails(ctx, 50)

	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, notifID, results[0].ID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestNotificationRepository_GetUnsentEmails_Pgxmock_Empty(t *testing.T) {
	mock, repo := setupNotificationMockDB(t)
	defer mock.Close()

	ctx := context.Background()

	rows := pgxmock.NewRows(notificationColumns)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, user_id, type`)).
		WithArgs(50).
		WillReturnRows(rows)

	results, err := repo.GetUnsentEmails(ctx, 50)

	require.NoError(t, err)
	assert.Empty(t, results)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// --- MarkAllAsRead (pgxmock) ---

func TestNotificationRepository_MarkAllAsRead_Pgxmock_Success(t *testing.T) {
	mock, repo := setupNotificationMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE notifications`)).
		WithArgs(userID, pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("UPDATE", 3))

	count, err := repo.MarkAllAsRead(ctx, userID)

	require.NoError(t, err)
	assert.Equal(t, int64(3), count)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestNotificationRepository_MarkAllAsRead_Pgxmock_None(t *testing.T) {
	mock, repo := setupNotificationMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE notifications`)).
		WithArgs(userID, pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))

	count, err := repo.MarkAllAsRead(ctx, userID)

	require.NoError(t, err)
	assert.Equal(t, int64(0), count)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// --- GetOpenByTransferSuggestionID (pgxmock) ---

func TestNotificationRepository_GetOpenByTransferSuggestionID_Pgxmock_Found(t *testing.T) {
	mock, repo := setupNotificationMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	transferSuggestionID := uuid.New().String()
	notifID := uuid.New().String()
	userID := uuid.New().String()
	notifType := "transfer_suggestion"

	rows := pgxmock.NewRows(notificationColumns)
	rows.AddRow(makeNotificationRow(notifID, userID)...)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, user_id, type`)).
		WithArgs(transferSuggestionID, notifType).
		WillReturnRows(rows)

	result, err := repo.GetOpenByTransferSuggestionID(ctx, transferSuggestionID, notifType)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, notifID, result.ID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestNotificationRepository_GetOpenByTransferSuggestionID_Pgxmock_NotFound(t *testing.T) {
	mock, repo := setupNotificationMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	transferSuggestionID := uuid.New().String()
	notifType := "transfer_suggestion"

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, user_id, type`)).
		WithArgs(transferSuggestionID, notifType).
		WillReturnError(pgx.ErrNoRows)

	result, err := repo.GetOpenByTransferSuggestionID(ctx, transferSuggestionID, notifType)

	assert.NoError(t, err)
	assert.Nil(t, result)
	assert.NoError(t, mock.ExpectationsWereMet())
}
