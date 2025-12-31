package operations

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	humaerrors "github.com/maxnussbaum/BoostedBudget/internal/huma/errors"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// NotificationHandler handles notification-related HTTP requests.
// Issue #257: Item Error Handler - Process ITEM Webhooks and User Notifications
type NotificationHandler struct {
	repo repository.NotificationRepository
}

// NewNotificationHandler creates a new NotificationHandler instance.
func NewNotificationHandler(repo repository.NotificationRepository) *NotificationHandler {
	return &NotificationHandler{repo: repo}
}

// --- Input/Output Types ---

// GetNotificationsInput represents the input for getting user notifications.
type GetNotificationsInput struct {
	inputs.AuthenticatedInput
	Limit int `query:"limit" minimum:"1" maximum:"100" default:"50" doc:"Number of notifications to return (1-100, default 50)"`
}

// GetNotificationsOutput represents the response for getting user notifications.
type GetNotificationsOutput struct {
	Body []domain.Notification
}

// MarkNotificationReadInput represents the input for marking a notification as read.
type MarkNotificationReadInput struct {
	inputs.AuthenticatedInput
	ID string `path:"id" doc:"Notification UUID"`
}

// MarkNotificationReadOutput represents the response for marking a notification as read.
type MarkNotificationReadOutput struct {
	Body struct {
		Message string `json:"message"`
	}
}

// GetUnreadCountInput represents the input for getting unread notification count.
type GetUnreadCountInput struct {
	inputs.AuthenticatedInput
}

// GetUnreadCountOutput represents the response for getting unread notification count.
type GetUnreadCountOutput struct {
	Body struct {
		Count int `json:"count"`
	}
}

// --- Handler Methods ---

// GetNotifications handles GET /api/v1/notifications requests.
// Retrieves notifications for the authenticated user.
func (h *NotificationHandler) GetNotifications(ctx context.Context, input *GetNotificationsInput) (*GetNotificationsOutput, error) {
	limit := input.Limit
	if limit == 0 {
		limit = 50 // default
	}

	notifications, err := h.repo.GetByUserID(ctx, input.UserID, limit)
	if err != nil {
		return nil, humaerrors.MapError(err)
	}

	// Return empty array instead of null if no notifications
	if notifications == nil {
		notifications = []domain.Notification{}
	}

	return &GetNotificationsOutput{Body: notifications}, nil
}

// MarkNotificationRead handles PATCH /api/v1/notifications/:id/read requests.
// Marks a specific notification as read for the authenticated user.
func (h *NotificationHandler) MarkNotificationRead(ctx context.Context, input *MarkNotificationReadInput) (*MarkNotificationReadOutput, error) {
	err := h.repo.MarkAsRead(ctx, input.ID, input.UserID)
	if err != nil {
		return nil, humaerrors.MapError(err)
	}

	output := &MarkNotificationReadOutput{}
	output.Body.Message = "Notification marked as read"
	return output, nil
}

// GetUnreadCount handles GET /api/v1/notifications/unread/count requests.
// Returns the count of unread notifications for the authenticated user.
func (h *NotificationHandler) GetUnreadCount(ctx context.Context, input *GetUnreadCountInput) (*GetUnreadCountOutput, error) {
	count, err := h.repo.GetUnreadCount(ctx, input.UserID)
	if err != nil {
		return nil, humaerrors.MapError(err)
	}

	output := &GetUnreadCountOutput{}
	output.Body.Count = count
	return output, nil
}

// RegisterNotificationRoutes registers notification routes with the Huma API.
// Note: These routes require the Clerk middleware to be applied.
func RegisterNotificationRoutes(api huma.API, handler *NotificationHandler) {
	// GET /api/v1/notifications - Get user notifications
	huma.Register(api, huma.Operation{
		OperationID: "getNotifications",
		Method:      http.MethodGet,
		Path:        "/api/v1/notifications",
		Summary:     "Get user notifications",
		Description: "Retrieves notifications for the authenticated user, ordered by created_at DESC.",
		Tags:        []string{"Notifications"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.GetNotifications)

	// GET /api/v1/notifications/unread/count - Get unread notification count
	// Note: This route must be registered BEFORE the /:id routes to avoid path conflict
	huma.Register(api, huma.Operation{
		OperationID: "getUnreadNotificationCount",
		Method:      http.MethodGet,
		Path:        "/api/v1/notifications/unread/count",
		Summary:     "Get unread notification count",
		Description: "Returns the count of unread notifications for the authenticated user.",
		Tags:        []string{"Notifications"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.GetUnreadCount)

	// PATCH /api/v1/notifications/{id}/read - Mark notification as read
	huma.Register(api, huma.Operation{
		OperationID: "markNotificationAsRead",
		Method:      http.MethodPatch,
		Path:        "/api/v1/notifications/{id}/read",
		Summary:     "Mark notification as read",
		Description: "Marks a specific notification as read for the authenticated user.",
		Tags:        []string{"Notifications"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.MarkNotificationRead)
}
