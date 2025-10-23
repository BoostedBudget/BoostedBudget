package handlers

import (
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/maxnussbaum/BoostedBudget/internal/config"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// SubscriptionTiersHandler handles subscription tier configuration requests
type SubscriptionTiersHandler struct {
	tierConfig *config.TierConfig
}

// SubscriptionTiersResponse represents the API response for subscription tiers
type SubscriptionTiersResponse struct {
	Tiers       map[domain.TierName]domain.SubscriptionTier `json:"tiers"`
	DefaultTier domain.TierName                              `json:"default_tier"`
}

// NewSubscriptionTiersHandler creates a new subscription tiers handler
// Panics if tierConfig is nil (required dependency)
func NewSubscriptionTiersHandler(tierConfig *config.TierConfig) *SubscriptionTiersHandler {
	if tierConfig == nil {
		panic("tierConfig is required for SubscriptionTiersHandler")
	}

	return &SubscriptionTiersHandler{
		tierConfig: tierConfig,
	}
}

// GetSubscriptionTiers handles GET /api/v1/subscription/tiers
// Returns all available subscription tiers with their limits, features, and pricing
// This is a public endpoint (no authentication required) for displaying pricing information
func (h *SubscriptionTiersHandler) GetSubscriptionTiers(c echo.Context) error {
	// Set cache headers for CDN caching (24 hours)
	c.Response().Header().Set("Cache-Control", "public, max-age=86400")

	// Build response from tier config
	response := SubscriptionTiersResponse{
		Tiers:       h.tierConfig.Tiers,
		DefaultTier: h.tierConfig.DefaultTier,
	}

	// Return JSON response
	return c.JSON(http.StatusOK, response)
}
