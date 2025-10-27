package handlers

import (
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
)

// DeletionHandler handles account deletion HTTP endpoints
type DeletionHandler struct {
	deletionService services.DeletionService
}

// NewDeletionHandler creates a new DeletionHandler instance
func NewDeletionHandler(deletionService services.DeletionService) *DeletionHandler {
	return &DeletionHandler{
		deletionService: deletionService,
	}
}

// RequestDeletionResponse is the response for deletion requests
type RequestDeletionResponse struct {
	Message           string `json:"message"`
	ScheduledAt       string `json:"scheduled_at"`
	CancellationToken string `json:"cancellation_token"`
}

// CancelDeletionResponse is the response for cancellation requests
type CancelDeletionResponse struct {
	Message string `json:"message"`
}

// RequestDeletion handles POST /api/v1/user/delete
// Requires authentication (user_id and email in context)
func (h *DeletionHandler) RequestDeletion(c echo.Context) error {
	// Extract user_id and email from context (set by auth middleware)
	userID, ok := c.Get("user_id").(string)
	if !ok || userID == "" {
		return echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
	}

	email, ok := c.Get("email").(string)
	if !ok || email == "" {
		return echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
	}

	// Request deletion from service
	cancellationToken, scheduledAt, err := h.deletionService.RequestDeletion(c.Request().Context(), userID, email)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to request account deletion")
	}

	// Return success response with token and scheduled date
	response := RequestDeletionResponse{
		Message:           "Account deletion scheduled",
		ScheduledAt:       scheduledAt.Format("2006-01-02T15:04:05Z07:00"),
		CancellationToken: cancellationToken,
	}

	return c.JSON(http.StatusOK, response)
}

// CancelDeletion handles GET /api/v1/user/delete/cancel/:token
// Public endpoint (no auth required - token validates the request)
func (h *DeletionHandler) CancelDeletion(c echo.Context) error {
	// Extract token from path parameter
	token := c.Param("token")
	if token == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "cancellation token required")
	}

	// Cancel deletion using service
	err := h.deletionService.CancelDeletion(c.Request().Context(), token)
	if err != nil {
		// Assume any error from service is invalid token
		return echo.NewHTTPError(http.StatusBadRequest, "invalid or expired cancellation token")
	}

	// Return success response
	response := CancelDeletionResponse{
		Message: "Account deletion cancelled",
	}

	return c.JSON(http.StatusOK, response)
}
