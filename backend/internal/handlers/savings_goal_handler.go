package handlers

import (
	"errors"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/middleware"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
)

// SavingsGoalHandler handles savings goal-related HTTP requests
type SavingsGoalHandler struct {
	service services.SavingsGoalService
}

// NewSavingsGoalHandler creates a new SavingsGoalHandler instance
func NewSavingsGoalHandler(service services.SavingsGoalService) *SavingsGoalHandler {
	return &SavingsGoalHandler{service: service}
}

// CreateGoal handles POST /savings-goals
// Creates a new savings goal for the authenticated user
func (h *SavingsGoalHandler) CreateGoal(c echo.Context) error {
	userID, err := middleware.GetUserID(c)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "User not authenticated")
	}

	var req domain.CreateSavingsGoalRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid request body")
	}

	goal, err := h.service.CreateGoal(c.Request().Context(), userID, &req)
	if err != nil {
		if errors.Is(err, domain.ErrSavingsGoalAlreadyExists) {
			return echo.NewHTTPError(http.StatusConflict, "A savings goal with this name already exists")
		}
		// Check for validation errors or account errors
		if err.Error() != "" && !errors.Is(err, domain.ErrSavingsGoalAlreadyExists) {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to create savings goal")
	}

	return c.JSON(http.StatusCreated, goal)
}

// GetGoals handles GET /savings-goals
// Retrieves all savings goals for the authenticated user
func (h *SavingsGoalHandler) GetGoals(c echo.Context) error {
	userID, err := middleware.GetUserID(c)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "User not authenticated")
	}

	goals, err := h.service.GetGoals(c.Request().Context(), userID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to retrieve savings goals")
	}

	// Return empty array instead of null if no goals
	if goals == nil {
		goals = []*domain.SavingsGoal{}
	}

	return c.JSON(http.StatusOK, goals)
}

// GetActiveGoals handles GET /savings-goals/active
// Retrieves all active (not completed) savings goals for the authenticated user
func (h *SavingsGoalHandler) GetActiveGoals(c echo.Context) error {
	userID, err := middleware.GetUserID(c)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "User not authenticated")
	}

	goals, err := h.service.GetActiveGoals(c.Request().Context(), userID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to retrieve active savings goals")
	}

	// Return empty array instead of null if no goals
	if goals == nil {
		goals = []*domain.SavingsGoal{}
	}

	return c.JSON(http.StatusOK, goals)
}

// GetGoal handles GET /savings-goals/:id
// Retrieves a specific savings goal by ID
func (h *SavingsGoalHandler) GetGoal(c echo.Context) error {
	userID, err := middleware.GetUserID(c)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "User not authenticated")
	}

	goalID := c.Param("id")
	if goalID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Goal ID is required")
	}

	goal, err := h.service.GetGoal(c.Request().Context(), userID, goalID)
	if err != nil {
		if errors.Is(err, domain.ErrSavingsGoalNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Savings goal not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to retrieve savings goal")
	}

	return c.JSON(http.StatusOK, goal)
}

// UpdateGoal handles PUT /savings-goals/:id
// Updates a savings goal's properties
func (h *SavingsGoalHandler) UpdateGoal(c echo.Context) error {
	userID, err := middleware.GetUserID(c)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "User not authenticated")
	}

	goalID := c.Param("id")
	if goalID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Goal ID is required")
	}

	var req domain.UpdateSavingsGoalRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid request body")
	}

	goal, err := h.service.UpdateGoal(c.Request().Context(), userID, goalID, &req)
	if err != nil {
		if errors.Is(err, domain.ErrSavingsGoalNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Savings goal not found")
		}
		if errors.Is(err, domain.ErrSavingsGoalAlreadyExists) {
			return echo.NewHTTPError(http.StatusConflict, "A savings goal with this name already exists")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to update savings goal")
	}

	return c.JSON(http.StatusOK, goal)
}

// DeleteGoal handles DELETE /savings-goals/:id
// Removes a savings goal from the user's collection
func (h *SavingsGoalHandler) DeleteGoal(c echo.Context) error {
	userID, err := middleware.GetUserID(c)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "User not authenticated")
	}

	goalID := c.Param("id")
	if goalID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Goal ID is required")
	}

	err = h.service.DeleteGoal(c.Request().Context(), userID, goalID)
	if err != nil {
		if errors.Is(err, domain.ErrSavingsGoalNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Savings goal not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to delete savings goal")
	}

	return c.NoContent(http.StatusNoContent)
}
