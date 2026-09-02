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
		(*string)(nil),    // action_url
		(*string)(nil),    // action_label
		false,             // read
		false,             // sent_email
		(*time.Time)(nil), // email_sent_at
		"informational",   // interaction_pattern
		(*string)(nil),    // transfer_suggestion_id
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
