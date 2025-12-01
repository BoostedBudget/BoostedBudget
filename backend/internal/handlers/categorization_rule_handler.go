package handlers

import (
	"errors"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/middleware"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
)

// CategorizationRuleHandler handles categorization rule-related HTTP requests.
type CategorizationRuleHandler struct {
	service services.CategorizationRuleService
}

// NewCategorizationRuleHandler creates a new CategorizationRuleHandler instance.
func NewCategorizationRuleHandler(service services.CategorizationRuleService) *CategorizationRuleHandler {
	return &CategorizationRuleHandler{service: service}
}

// CreateRule handles POST /rules
// Creates a new categorization rule for the authenticated user.
func (h *CategorizationRuleHandler) CreateRule(c echo.Context) error {
	userID, err := middleware.GetUserID(c)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "User not authenticated")
	}

	var req domain.CreateCategorizationRuleRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid request body")
	}

	rule, err := h.service.CreateRule(c.Request().Context(), userID, &req)
	if err != nil {
		if errors.Is(err, domain.ErrRuleLimitExceeded) {
			return echo.NewHTTPError(http.StatusForbidden, "Rule limit exceeded for your subscription tier")
		}
		if errors.Is(err, domain.ErrRuleAlreadyExists) {
			return echo.NewHTTPError(http.StatusConflict, "A rule with this name already exists")
		}
		// Check for validation errors
		if err.Error() != "" && (
			err.Error() == "name is required" ||
			err.Error() == "at least one condition is required" ||
			err.Error() == "rule must have at least one action (category or tags)") {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to create rule")
	}

	return c.JSON(http.StatusCreated, rule)
}

// GetUserRules handles GET /rules
// Retrieves all categorization rules for the authenticated user.
func (h *CategorizationRuleHandler) GetUserRules(c echo.Context) error {
	userID, err := middleware.GetUserID(c)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "User not authenticated")
	}

	rules, err := h.service.GetUserRules(c.Request().Context(), userID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to retrieve rules")
	}

	// Return empty array instead of null if no rules
	if rules == nil {
		rules = []*domain.CategorizationRule{}
	}

	return c.JSON(http.StatusOK, rules)
}

// GetRuleByID handles GET /rules/:id
// Retrieves a specific rule by ID.
func (h *CategorizationRuleHandler) GetRuleByID(c echo.Context) error {
	userID, err := middleware.GetUserID(c)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "User not authenticated")
	}

	ruleID := c.Param("id")
	if ruleID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Rule ID is required")
	}

	rule, err := h.service.GetRuleByID(c.Request().Context(), userID, ruleID)
	if err != nil {
		if errors.Is(err, domain.ErrRuleNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Rule not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to retrieve rule")
	}

	return c.JSON(http.StatusOK, rule)
}

// UpdateRule handles PUT /rules/:id
// Updates a rule's configuration.
func (h *CategorizationRuleHandler) UpdateRule(c echo.Context) error {
	userID, err := middleware.GetUserID(c)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "User not authenticated")
	}

	ruleID := c.Param("id")
	if ruleID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Rule ID is required")
	}

	var req domain.UpdateCategorizationRuleRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid request body")
	}

	rule, err := h.service.UpdateRule(c.Request().Context(), userID, ruleID, &req)
	if err != nil {
		if errors.Is(err, domain.ErrRuleNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Rule not found")
		}
		if errors.Is(err, domain.ErrRuleAlreadyExists) {
			return echo.NewHTTPError(http.StatusConflict, "A rule with this name already exists")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to update rule")
	}

	return c.JSON(http.StatusOK, rule)
}

// DeleteRule handles DELETE /rules/:id
// Removes a rule.
func (h *CategorizationRuleHandler) DeleteRule(c echo.Context) error {
	userID, err := middleware.GetUserID(c)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "User not authenticated")
	}

	ruleID := c.Param("id")
	if ruleID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Rule ID is required")
	}

	err = h.service.DeleteRule(c.Request().Context(), userID, ruleID)
	if err != nil {
		if errors.Is(err, domain.ErrRuleNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Rule not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to delete rule")
	}

	return c.NoContent(http.StatusNoContent)
}

// ToggleRule handles POST /rules/:id/toggle
// Enables or disables a rule.
func (h *CategorizationRuleHandler) ToggleRule(c echo.Context) error {
	userID, err := middleware.GetUserID(c)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "User not authenticated")
	}

	ruleID := c.Param("id")
	if ruleID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Rule ID is required")
	}

	var req domain.ToggleCategorizationRuleRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid request body")
	}

	rule, err := h.service.ToggleRule(c.Request().Context(), userID, ruleID, req.Enabled)
	if err != nil {
		if errors.Is(err, domain.ErrRuleNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Rule not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to toggle rule")
	}

	return c.JSON(http.StatusOK, rule)
}
