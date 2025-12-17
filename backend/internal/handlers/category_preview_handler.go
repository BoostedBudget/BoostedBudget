package handlers

import (
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/middleware"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
)

// =============================================================================
// Issue #497: Category Preview Handler
// =============================================================================

// CategoryPreviewHandler handles category preview HTTP requests
type CategoryPreviewHandler struct {
	service services.CategoryPreviewService
}

// NewCategoryPreviewHandler creates a new CategoryPreviewHandler instance
func NewCategoryPreviewHandler(service services.CategoryPreviewService) *CategoryPreviewHandler {
	return &CategoryPreviewHandler{service: service}
}

// PreviewCategoryMatches handles POST /budgets/categories/preview
// Returns the count and total amount of transactions matching the given category config
func (h *CategoryPreviewHandler) PreviewCategoryMatches(c echo.Context) error {
	userID, err := middleware.GetUserID(c)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "User not authenticated")
	}

	var req domain.CategoryPreviewRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid request body")
	}

	resp, err := h.service.PreviewCategoryMatches(c.Request().Context(), userID, &req)
	if err != nil {
		// Return validation errors as 400 Bad Request
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	return c.JSON(http.StatusOK, resp)
}
