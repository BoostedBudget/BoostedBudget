package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// Mock NotificationRepository for Testing
// ============================================================================

type mockNotificationRepository struct {
	getByUserIDFunc      func(ctx context.Context, userID string, limit int) ([]domain.Notification, error)
	getUnreadCountFunc   func(ctx context.Context, userID string) (int, error)
	markAsReadFunc       func(ctx context.Context, id, userID string) error
	getUnsentEmailsFunc  func(ctx context.Context, limit int) ([]domain.Notification, error)
}

func (m *mockNotificationRepository) GetByUserID(ctx context.Context, userID string, limit int) ([]domain.Notification, error) {
	if m.getByUserIDFunc != nil {
		return m.getByUserIDFunc(ctx, userID, limit)
	}
	return nil, errors.New("not implemented")
}

func (m *mockNotificationRepository) GetUnreadCount(ctx context.Context, userID string) (int, error) {
	if m.getUnreadCountFunc != nil {
		return m.getUnreadCountFunc(ctx, userID)
	}
	return 0, errors.New("not implemented")
}

func (m *mockNotificationRepository) MarkAsRead(ctx context.Context, id, userID string) error {
	if m.markAsReadFunc != nil {
		return m.markAsReadFunc(ctx, id, userID)
	}
	return errors.New("not implemented")
}

func (m *mockNotificationRepository) GetUnsentEmails(ctx context.Context, limit int) ([]domain.Notification, error) {
	if m.getUnsentEmailsFunc != nil {
		return m.getUnsentEmailsFunc(ctx, limit)
	}
	return nil, errors.New("not implemented")
}

func (m *mockNotificationRepository) Create(ctx context.Context, notification *domain.Notification) error {
	return errors.New("not implemented")
}

func (m *mockNotificationRepository) Update(ctx context.Context, notification *domain.Notification) error {
	return errors.New("not implemented")
}

// ============================================================================
// PHASE 7 RED: NotificationHandler Tests
// Issue #257: Item Error Handler - Process ITEM Webhooks and User Notifications
// ============================================================================

// TestGetNotifications_Unauthorized tests that unauthenticated requests are rejected
func TestGetNotifications_Unauthorized(t *testing.T) {
	// Given: Handler with mock repository
	mockRepo := &mockNotificationRepository{}
	handler := NewNotificationHandler(mockRepo)

	// When: Making GET request WITHOUT user_id in context
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/notifications", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	// Note: NOT setting user_id in context

	err := handler.GetNotifications(c)

	// Then: Returns 401 Unauthorized
	require.Error(t, err)
	httpErr, ok := err.(*echo.HTTPError)
	require.True(t, ok)
	assert.Equal(t, http.StatusUnauthorized, httpErr.Code)
}

// TestGetNotifications_Success tests successful notification retrieval
func TestGetNotifications_Success(t *testing.T) {
	// Given: Mock repository that returns notifications
	actionURL := "/accounts/reconnect?link_token=token123&item_id=item1"
	actionLabel := "Reconnect Bank"
	expectedNotifications := []domain.Notification{
		{
			ID:          "notif-1",
			UserID:      "user-123",
			Type:        "login_required",
			Title:       "Reconnect Your Bank",
			Message:     "We need you to reconnect Chase Bank.",
			ActionURL:   &actionURL,
			ActionLabel: &actionLabel,
			Read:        false,
			SentEmail:   false,
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		},
	}

	mockRepo := &mockNotificationRepository{
		getByUserIDFunc: func(ctx context.Context, userID string, limit int) ([]domain.Notification, error) {
			assert.Equal(t, "user-123", userID)
			assert.Equal(t, 50, limit)  // Default limit
			return expectedNotifications, nil
		},
	}

	handler := NewNotificationHandler(mockRepo)

	// When: Making GET request with authenticated user
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/notifications", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", "user-123")

	err := handler.GetNotifications(c)

	// Then: Returns 200 with notifications
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "notif-1")
	assert.Contains(t, rec.Body.String(), "Reconnect Your Bank")
	assert.Contains(t, rec.Body.String(), "login_required")
}

// TestGetNotifications_ParsesLimitParam tests limit query parameter parsing
func TestGetNotifications_ParsesLimitParam(t *testing.T) {
	// Given: Mock repository that captures limit
	mockRepo := &mockNotificationRepository{
		getByUserIDFunc: func(ctx context.Context, userID string, limit int) ([]domain.Notification, error) {
			assert.Equal(t, 10, limit)
			return []domain.Notification{}, nil
		},
	}

	handler := NewNotificationHandler(mockRepo)

	// When: Making GET request with limit query param
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/notifications?limit=10", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", "user-123")

	err := handler.GetNotifications(c)

	// Then: Passes custom limit to repository
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
}

// TestGetNotifications_InvalidLimit tests invalid limit parameter handling
func TestGetNotifications_InvalidLimit(t *testing.T) {
	// Given: Handler with mock repository
	mockRepo := &mockNotificationRepository{}
	handler := NewNotificationHandler(mockRepo)

	// When: Making GET request with invalid limit
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/notifications?limit=invalid", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", "user-123")

	err := handler.GetNotifications(c)

	// Then: Returns 400 Bad Request
	require.Error(t, err)
	httpErr, ok := err.(*echo.HTTPError)
	require.True(t, ok)
	assert.Equal(t, http.StatusBadRequest, httpErr.Code)
}

// TestMarkNotificationAsRead_Unauthorized tests unauthenticated requests
func TestMarkNotificationAsRead_Unauthorized(t *testing.T) {
	// Given: Handler with mock repository
	mockRepo := &mockNotificationRepository{}
	handler := NewNotificationHandler(mockRepo)

	// When: Making PATCH request WITHOUT user_id in context
	e := echo.New()
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/notifications/notif-1/read", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("notif-1")

	err := handler.MarkNotificationAsRead(c)

	// Then: Returns 401 Unauthorized
	require.Error(t, err)
	httpErr, ok := err.(*echo.HTTPError)
	require.True(t, ok)
	assert.Equal(t, http.StatusUnauthorized, httpErr.Code)
}

// TestMarkNotificationAsRead_Success tests successful mark as read
func TestMarkNotificationAsRead_Success(t *testing.T) {
	// Given: Mock repository that marks notification as read
	mockRepo := &mockNotificationRepository{
		markAsReadFunc: func(ctx context.Context, id, userID string) error {
			assert.Equal(t, "notif-1", id)
			assert.Equal(t, "user-123", userID)
			return nil
		},
	}

	handler := NewNotificationHandler(mockRepo)

	// When: Making PATCH request with authenticated user
	e := echo.New()
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/notifications/notif-1/read", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", "user-123")
	c.SetParamNames("id")
	c.SetParamValues("notif-1")

	err := handler.MarkNotificationAsRead(c)

	// Then: Returns 200 OK
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
}

// TestMarkNotificationAsRead_NotFound tests notification not found scenario
func TestMarkNotificationAsRead_NotFound(t *testing.T) {
	// Given: Mock repository that returns NotFound error
	mockRepo := &mockNotificationRepository{
		markAsReadFunc: func(ctx context.Context, id, userID string) error {
			return repository.ErrNotificationNotFound
		},
	}

	handler := NewNotificationHandler(mockRepo)

	// When: Making PATCH request for non-existent notification
	e := echo.New()
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/notifications/invalid-id/read", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", "user-123")
	c.SetParamNames("id")
	c.SetParamValues("invalid-id")

	err := handler.MarkNotificationAsRead(c)

	// Then: Returns 404 Not Found
	require.Error(t, err)
	httpErr, ok := err.(*echo.HTTPError)
	require.True(t, ok)
	assert.Equal(t, http.StatusNotFound, httpErr.Code)
}

// TestGetUnreadCount_Unauthorized tests unauthenticated requests
func TestGetUnreadCount_Unauthorized(t *testing.T) {
	// Given: Handler with mock repository
	mockRepo := &mockNotificationRepository{}
	handler := NewNotificationHandler(mockRepo)

	// When: Making GET request WITHOUT user_id in context
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/notifications/unread/count", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := handler.GetUnreadCount(c)

	// Then: Returns 401 Unauthorized
	require.Error(t, err)
	httpErr, ok := err.(*echo.HTTPError)
	require.True(t, ok)
	assert.Equal(t, http.StatusUnauthorized, httpErr.Code)
}

// TestGetUnreadCount_Success tests successful unread count retrieval
func TestGetUnreadCount_Success(t *testing.T) {
	// Given: Mock repository that returns unread count
	mockRepo := &mockNotificationRepository{
		getUnreadCountFunc: func(ctx context.Context, userID string) (int, error) {
			assert.Equal(t, "user-123", userID)
			return 5, nil
		},
	}

	handler := NewNotificationHandler(mockRepo)

	// When: Making GET request with authenticated user
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/notifications/unread/count", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", "user-123")

	err := handler.GetUnreadCount(c)

	// Then: Returns 200 with count
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "\"count\":5")
}

// TestGetUnreadCount_RepositoryError tests database error handling
func TestGetUnreadCount_RepositoryError(t *testing.T) {
	// Given: Mock repository that returns error
	mockRepo := &mockNotificationRepository{
		getUnreadCountFunc: func(ctx context.Context, userID string) (int, error) {
			return 0, errors.New("database connection failed")
		},
	}

	handler := NewNotificationHandler(mockRepo)

	// When: Making GET request with authenticated user
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/notifications/unread/count", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", "user-123")

	err := handler.GetUnreadCount(c)

	// Then: Returns 500 Internal Server Error
	require.Error(t, err)
	httpErr, ok := err.(*echo.HTTPError)
	require.True(t, ok)
	assert.Equal(t, http.StatusInternalServerError, httpErr.Code)
}
