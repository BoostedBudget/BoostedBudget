package handlers

import (
	"errors"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/middleware"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
)

// TagHandler handles user tag-related HTTP requests
type TagHandler struct {
	service services.UserTagService
}

// NewTagHandler creates a new TagHandler instance
func NewTagHandler(service services.UserTagService) *TagHandler {
	return &TagHandler{service: service}
}

// CreateTag handles POST /tags
// Creates a new tag for the authenticated user
func (h *TagHandler) CreateTag(c echo.Context) error {
	userID, err := middleware.GetUserID(c)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "User not authenticated")
	}

	var req domain.CreateUserTagRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid request body")
	}

	tag, err := h.service.CreateTag(c.Request().Context(), userID, &req)
	if err != nil {
		if errors.Is(err, domain.ErrTagLimitExceeded) {
			return echo.NewHTTPError(http.StatusForbidden, "Tag limit exceeded for your subscription tier")
		}
		if errors.Is(err, domain.ErrTagAlreadyExists) {
			return echo.NewHTTPError(http.StatusConflict, "A tag with this name already exists")
		}
		if errors.Is(err, domain.ErrInvalidTagName) {
			return echo.NewHTTPError(http.StatusBadRequest, "Invalid tag name")
		}
		if errors.Is(err, domain.ErrInvalidTagColor) {
			return echo.NewHTTPError(http.StatusBadRequest, "Invalid tag color")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to create tag")
	}

	return c.JSON(http.StatusCreated, tag)
}

// GetUserTags handles GET /tags
// Retrieves all tags for the authenticated user
func (h *TagHandler) GetUserTags(c echo.Context) error {
	userID, err := middleware.GetUserID(c)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "User not authenticated")
	}

	tags, err := h.service.GetUserTags(c.Request().Context(), userID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to retrieve tags")
	}

	// Return empty array instead of null if no tags
	if tags == nil {
		tags = []*domain.UserTag{}
	}

	return c.JSON(http.StatusOK, tags)
}

// GetTagByID handles GET /tags/:id
// Retrieves a specific tag by ID
func (h *TagHandler) GetTagByID(c echo.Context) error {
	userID, err := middleware.GetUserID(c)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "User not authenticated")
	}

	tagID := c.Param("id")
	if tagID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Tag ID is required")
	}

	tag, err := h.service.GetTagByID(c.Request().Context(), tagID)
	if err != nil {
		if errors.Is(err, domain.ErrTagNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Tag not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to retrieve tag")
	}

	// Verify ownership
	if tag.UserID != userID {
		return echo.NewHTTPError(http.StatusNotFound, "Tag not found")
	}

	return c.JSON(http.StatusOK, tag)
}

// UpdateTag handles PUT /tags/:id
// Updates a tag's name or color
func (h *TagHandler) UpdateTag(c echo.Context) error {
	userID, err := middleware.GetUserID(c)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "User not authenticated")
	}

	tagID := c.Param("id")
	if tagID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Tag ID is required")
	}

	var req domain.UpdateUserTagRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid request body")
	}

	tag, err := h.service.UpdateTag(c.Request().Context(), userID, tagID, &req)
	if err != nil {
		if errors.Is(err, domain.ErrTagNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Tag not found")
		}
		if errors.Is(err, domain.ErrTagAlreadyExists) {
			return echo.NewHTTPError(http.StatusConflict, "A tag with this name already exists")
		}
		if errors.Is(err, domain.ErrInvalidTagName) {
			return echo.NewHTTPError(http.StatusBadRequest, "Invalid tag name")
		}
		if errors.Is(err, domain.ErrInvalidTagColor) {
			return echo.NewHTTPError(http.StatusBadRequest, "Invalid tag color")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to update tag")
	}

	return c.JSON(http.StatusOK, tag)
}

// DeleteTag handles DELETE /tags/:id
// Removes a tag from the user's collection
func (h *TagHandler) DeleteTag(c echo.Context) error {
	userID, err := middleware.GetUserID(c)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "User not authenticated")
	}

	tagID := c.Param("id")
	if tagID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Tag ID is required")
	}

	err = h.service.DeleteTag(c.Request().Context(), userID, tagID)
	if err != nil {
		if errors.Is(err, domain.ErrTagNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Tag not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to delete tag")
	}

	return c.NoContent(http.StatusNoContent)
}
