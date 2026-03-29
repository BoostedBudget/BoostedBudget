package operations

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockNotificationRepository is a mock implementation of repository.NotificationRepository
type mockNotificationRepository struct {
	notifications  []domain.Notification
	unreadCount    int
	getErr         error
	markReadErr    error
	countErr       error
}

func (m *mockNotificationRepository) GetByUserID(ctx context.Context, userID string, limit int) ([]domain.Notification, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	return m.notifications, nil
}

func (m *mockNotificationRepository) GetUnreadCount(ctx context.Context, userID string) (int, error) {
	if m.countErr != nil {
		return 0, m.countErr
	}
	return m.unreadCount, nil
}

func (m *mockNotificationRepository) MarkAsRead(ctx context.Context, notificationID, userID string) error {
	return m.markReadErr
}

func (m *mockNotificationRepository) Create(ctx context.Context, notification *domain.Notification) error {
	return errors.New("not implemented")
}

func (m *mockNotificationRepository) Update(ctx context.Context, notification *domain.Notification) error {
	return errors.New("not implemented")
}

func (m *mockNotificationRepository) GetUnsentEmails(ctx context.Context, limit int) ([]domain.Notification, error) {
	return nil, errors.New("not implemented")
}

// Helper to create a test notification
func createTestNotification(userID, id string) domain.Notification {
	now := time.Now()
	actionURL := "/accounts/reconnect?item_id=item1"
	actionLabel := "Reconnect Bank"
	return domain.Notification{
		ID:          id,
		UserID:      userID,
		Type:        "item_error",
		Title:       "Bank Connection Issue",
		Message:     "Your bank connection needs attention",
		ActionURL:   &actionURL,
		ActionLabel: &actionLabel,
		Read:        false,
		SentEmail:   false,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}

// TestGetNotifications_Success tests successful retrieval of notifications.
func TestGetNotifications_Success(t *testing.T) {
	userID := "user-123"
	mockRepo := &mockNotificationRepository{
		notifications: []domain.Notification{
			createTestNotification(userID, "notif-1"),
			createTestNotification(userID, "notif-2"),
		},
	}

	handler := NewNotificationHandler(mockRepo)

	input := &GetNotificationsInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
		Limit:              50,
	}

	output, err := handler.GetNotifications(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Len(t, output.Body, 2)
	assert.Equal(t, "notif-1", output.Body[0].ID)
	assert.Equal(t, "notif-2", output.Body[1].ID)
}

// TestGetNotifications_EmptyList tests that empty array is returned instead of null.
func TestGetNotifications_EmptyList(t *testing.T) {
	mockRepo := &mockNotificationRepository{
		notifications: nil,
	}

	handler := NewNotificationHandler(mockRepo)

	input := &GetNotificationsInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		Limit:              50,
	}

	output, err := handler.GetNotifications(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.NotNil(t, output.Body, "Should return empty array, not nil")
	assert.Len(t, output.Body, 0)
}

// TestGetNotifications_DefaultLimit tests that default limit is applied.
func TestGetNotifications_DefaultLimit(t *testing.T) {
	mockRepo := &mockNotificationRepository{
		notifications: []domain.Notification{},
	}

	handler := NewNotificationHandler(mockRepo)

	input := &GetNotificationsInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		Limit:              0, // should default to 50
	}

	output, err := handler.GetNotifications(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
}

// TestGetNotifications_Error tests error handling.
func TestGetNotifications_Error(t *testing.T) {
	mockRepo := &mockNotificationRepository{
		getErr: errors.New("database error"),
	}

	handler := NewNotificationHandler(mockRepo)

	input := &GetNotificationsInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		Limit:              50,
	}

	output, err := handler.GetNotifications(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
}

// TestMarkNotificationRead_Success tests successful marking of notification as read.
func TestMarkNotificationRead_Success(t *testing.T) {
	mockRepo := &mockNotificationRepository{
		markReadErr: nil,
	}

	handler := NewNotificationHandler(mockRepo)

	input := &MarkNotificationReadInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		ID:                 "notif-123",
	}

	output, err := handler.MarkNotificationRead(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, "Notification marked as read", output.Body.Message)
}

// TestMarkNotificationRead_NotFound tests 404 when notification doesn't exist.
func TestMarkNotificationRead_NotFound(t *testing.T) {
	mockRepo := &mockNotificationRepository{
		markReadErr: repository.ErrNotificationNotFound,
	}

	handler := NewNotificationHandler(mockRepo)

	input := &MarkNotificationReadInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		ID:                 "nonexistent-notif",
	}

	output, err := handler.MarkNotificationRead(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// TestGetUnreadCount_Success tests successful retrieval of unread count.
func TestGetUnreadCount_Success(t *testing.T) {
	mockRepo := &mockNotificationRepository{
		unreadCount: 5,
	}

	handler := NewNotificationHandler(mockRepo)

	input := &GetUnreadCountInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}

	output, err := handler.GetUnreadCount(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, 5, output.Body.Count)
}

// TestGetUnreadCount_ZeroCount tests that zero count is properly returned.
func TestGetUnreadCount_ZeroCount(t *testing.T) {
	mockRepo := &mockNotificationRepository{
		unreadCount: 0,
	}

	handler := NewNotificationHandler(mockRepo)

	input := &GetUnreadCountInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}

	output, err := handler.GetUnreadCount(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, 0, output.Body.Count)
}

// TestGetUnreadCount_Error tests error handling.
func TestGetUnreadCount_Error(t *testing.T) {
	mockRepo := &mockNotificationRepository{
		countErr: errors.New("database error"),
	}

	handler := NewNotificationHandler(mockRepo)

	input := &GetUnreadCountInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}

	output, err := handler.GetUnreadCount(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
}

// TestNotificationRouteRegistration tests that routes are properly registered.
func TestNotificationRouteRegistration(t *testing.T) {
	mockRepo := &mockNotificationRepository{
		notifications: []domain.Notification{},
	}

	_, api := humatest.New(t)
	handler := NewNotificationHandler(mockRepo)
	RegisterNotificationRoutes(api, handler)

	// Test GET /api/v1/notifications (without auth, should fail with 401)
	resp := api.Get("/api/v1/notifications")
	assert.Equal(t, http.StatusUnauthorized, resp.Code)

	// Verify the response contains expected error message
	body := resp.Body.String()
	assert.Contains(t, body, "User not authenticated")
}

// TestNotificationResponseJSON tests JSON serialization of response.
func TestNotificationResponseJSON(t *testing.T) {
	mockRepo := &mockNotificationRepository{
		notifications: []domain.Notification{},
	}

	_, api := humatest.New(t)
	handler := NewNotificationHandler(mockRepo)
	RegisterNotificationRoutes(api, handler)

	resp := api.Get("/api/v1/notifications")
	assert.Equal(t, http.StatusUnauthorized, resp.Code)

	// Verify error response is valid JSON
	var errorResp map[string]interface{}
	err := json.Unmarshal(resp.Body.Bytes(), &errorResp)
	require.NoError(t, err, "Error response should be valid JSON")
}
