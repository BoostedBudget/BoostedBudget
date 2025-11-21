package handlers

import (
	"errors"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/middleware"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
)

// CategoryHandler handles custom category-related HTTP requests
type CategoryHandler struct {
	service services.CustomCategoryService
}

// NewCategoryHandler creates a new CategoryHandler instance
func NewCategoryHandler(service services.CustomCategoryService) *CategoryHandler {
	return &CategoryHandler{service: service}
}

// CreateCategory handles POST /categories
// Creates a new custom category for the authenticated user
func (h *CategoryHandler) CreateCategory(c echo.Context) error {
	userID, err := middleware.GetUserID(c)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "User not authenticated")
	}

	var req domain.CreateCustomCategoryRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid request body")
	}

	category, err := h.service.CreateCategory(c.Request().Context(), userID, &req)
	if err != nil {
		if errors.Is(err, domain.ErrCategoryLimitExceeded) {
			return echo.NewHTTPError(http.StatusForbidden, "Category limit exceeded for your subscription tier")
		}
		if errors.Is(err, domain.ErrCategoryAlreadyExists) {
			return echo.NewHTTPError(http.StatusConflict, "A category with this name already exists")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to create category")
	}

	return c.JSON(http.StatusCreated, category)
}

// GetUserCategories handles GET /categories
// Retrieves all custom categories for the authenticated user
func (h *CategoryHandler) GetUserCategories(c echo.Context) error {
	userID, err := middleware.GetUserID(c)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "User not authenticated")
	}

	categories, err := h.service.GetUserCategories(c.Request().Context(), userID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to retrieve categories")
	}

	// Return empty array instead of null if no categories
	if categories == nil {
		categories = []*domain.CustomCategory{}
	}

	return c.JSON(http.StatusOK, categories)
}

// GetCategoryByID handles GET /categories/:id
// Retrieves a specific category by ID
func (h *CategoryHandler) GetCategoryByID(c echo.Context) error {
	userID, err := middleware.GetUserID(c)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "User not authenticated")
	}

	categoryID := c.Param("id")
	if categoryID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Category ID is required")
	}

	category, err := h.service.GetCategoryByID(c.Request().Context(), categoryID)
	if err != nil {
		if errors.Is(err, domain.ErrCategoryNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Category not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to retrieve category")
	}

	// Verify ownership
	if category.UserID != userID {
		return echo.NewHTTPError(http.StatusNotFound, "Category not found")
	}

	return c.JSON(http.StatusOK, category)
}

// UpdateCategory handles PUT /categories/:id
// Updates a category's name, icon, or color
func (h *CategoryHandler) UpdateCategory(c echo.Context) error {
	userID, err := middleware.GetUserID(c)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "User not authenticated")
	}

	categoryID := c.Param("id")
	if categoryID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Category ID is required")
	}

	var req domain.UpdateCustomCategoryRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid request body")
	}

	category, err := h.service.UpdateCategory(c.Request().Context(), userID, categoryID, &req)
	if err != nil {
		if errors.Is(err, domain.ErrCategoryNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Category not found")
		}
		if errors.Is(err, domain.ErrCategoryAlreadyExists) {
			return echo.NewHTTPError(http.StatusConflict, "A category with this name already exists")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to update category")
	}

	return c.JSON(http.StatusOK, category)
}

// DeleteCategory handles DELETE /categories/:id
// Removes a category from the user's collection
func (h *CategoryHandler) DeleteCategory(c echo.Context) error {
	userID, err := middleware.GetUserID(c)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "User not authenticated")
	}

	categoryID := c.Param("id")
	if categoryID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Category ID is required")
	}

	err = h.service.DeleteCategory(c.Request().Context(), userID, categoryID)
	if err != nil {
		if errors.Is(err, domain.ErrCategoryNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Category not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to delete category")
	}

	return c.NoContent(http.StatusNoContent)
}
