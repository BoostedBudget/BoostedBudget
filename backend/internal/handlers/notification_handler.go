package handlers

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"
	"github.com/maxnussbaum/BoostedBudget/internal/middleware"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// NotificationHandler handles notification-related HTTP requests
// Issue #257: Item Error Handler - Process ITEM Webhooks and User Notifications
type NotificationHandler struct {
	notificationRepo repository.NotificationRepository
}

// NewNotificationHandler creates a new NotificationHandler instance
func NewNotificationHandler(notificationRepo repository.NotificationRepository) *NotificationHandler {
	return &NotificationHandler{
		notificationRepo: notificationRepo,
	}
}

// GetNotifications handles GET /api/v1/notifications
// Retrieves notifications for the authenticated user
//
// Query Parameters:
//   - limit (optional): Number of notifications to return (1-100, default 50)
//
// Response: JSON array of notifications (ordered by created_at DESC)
func (h *NotificationHandler) GetNotifications(c echo.Context) error {
	// Extract user_id from context (set by Auth0 middleware)
	userID, err := middleware.GetUserID(c)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "User not authenticated")
	}

	// Parse limit parameter
	limit := 50 // default
	if limitStr := c.QueryParam("limit"); limitStr != "" {
		parsedLimit, err := strconv.Atoi(limitStr)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "Invalid limit parameter")
		}
		if parsedLimit < 1 || parsedLimit > 100 {
			return echo.NewHTTPError(http.StatusBadRequest, "Limit must be between 1 and 100")
		}
		limit = parsedLimit
	}

	// Fetch notifications from repository
	notifications, err := h.notificationRepo.GetByUserID(c.Request().Context(), userID, limit)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to fetch notifications")
	}

	return c.JSON(http.StatusOK, notifications)
}

// MarkNotificationAsRead handles PATCH /api/v1/notifications/:id/read
// Marks a specific notification as read for the authenticated user
//
// Path Parameters:
//   - id: Notification UUID
//
// Response: 200 OK on success, 404 if notification not found or doesn't belong to user
func (h *NotificationHandler) MarkNotificationAsRead(c echo.Context) error {
	// Extract user_id from context
	userID, err := middleware.GetUserID(c)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "User not authenticated")
	}

	// Extract notification ID from path parameter
	notificationID := c.Param("id")
	if notificationID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Notification ID is required")
	}

	// Mark notification as read (validates ownership via user_id)
	err = h.notificationRepo.MarkAsRead(c.Request().Context(), notificationID, userID)
	if err != nil {
		if errors.Is(err, repository.ErrNotificationNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Notification not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to mark notification as read")
	}

	return c.JSON(http.StatusOK, map[string]string{"message": "Notification marked as read"})
}

// GetUnreadCount handles GET /api/v1/notifications/unread/count
// Returns the count of unread notifications for the authenticated user
//
// Response: JSON with unread count
func (h *NotificationHandler) GetUnreadCount(c echo.Context) error {
	// Extract user_id from context
	userID, err := middleware.GetUserID(c)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "User not authenticated")
	}

	// Get unread count from repository
	count, err := h.notificationRepo.GetUnreadCount(c.Request().Context(), userID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to get unread count")
	}

	return c.JSON(http.StatusOK, map[string]int{"count": count})
}
