package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/maxnussbaum/BoostedBudget/internal/config"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TDD RED: Test that GetSubscriptionTiers handler exists and returns 200
func TestSubscriptionTiersHandler_GetSubscriptionTiers(t *testing.T) {
	// Load real tier config
	tierConfig, err := config.LoadTierConfig()
	require.NoError(t, err, "Failed to load tier config")

	// Create handler
	handler := NewSubscriptionTiersHandler(tierConfig)

	// Create Echo context
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/subscription/tiers", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Call handler
	err = handler.GetSubscriptionTiers(c)
	require.NoError(t, err)

	// Assert response
	assert.Equal(t, http.StatusOK, rec.Code)
}

// TDD RED: Test that response has correct JSON structure
func TestSubscriptionTiersHandler_GetSubscriptionTiers_ResponseStructure(t *testing.T) {
	// Load real tier config
	tierConfig, err := config.LoadTierConfig()
	require.NoError(t, err)

	// Create handler
	handler := NewSubscriptionTiersHandler(tierConfig)

	// Create Echo context
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/subscription/tiers", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Call handler
	err = handler.GetSubscriptionTiers(c)
	require.NoError(t, err)

	// Parse response
	var response SubscriptionTiersResponse
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	require.NoError(t, err, "Response should be valid JSON")

	// Assert structure
	assert.NotNil(t, response.Tiers)
	assert.NotEmpty(t, response.DefaultTier)
	assert.Len(t, response.Tiers, 4, "Should have 4 tiers")

	// Verify all expected tiers exist
	_, hasFreeTier := response.Tiers[domain.TierFree]
	assert.True(t, hasFreeTier, "Should have free tier")

	_, hasPremiumLite := response.Tiers[domain.TierPremiumLite]
	assert.True(t, hasPremiumLite, "Should have premium_lite tier")

	_, hasPremium := response.Tiers[domain.TierPremium]
	assert.True(t, hasPremium, "Should have premium tier")

	_, hasPremiumPlus := response.Tiers[domain.TierPremiumPlus]
	assert.True(t, hasPremiumPlus, "Should have premium_plus tier")

	// Verify default tier is "free"
	assert.Equal(t, domain.TierFree, response.DefaultTier)
}

// TDD RED: Test that each tier has all required fields
func TestSubscriptionTiersHandler_GetSubscriptionTiers_TierFields(t *testing.T) {
	// Load real tier config
	tierConfig, err := config.LoadTierConfig()
	require.NoError(t, err)

	// Create handler
	handler := NewSubscriptionTiersHandler(tierConfig)

	// Create Echo context
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/subscription/tiers", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Call handler
	err = handler.GetSubscriptionTiers(c)
	require.NoError(t, err)

	// Parse response
	var response SubscriptionTiersResponse
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	require.NoError(t, err)

	// Verify free tier fields
	freeTier := response.Tiers[domain.TierFree]
	assert.Equal(t, domain.TierFree, freeTier.Name)
	assert.Equal(t, "Free", freeTier.DisplayName)
	assert.NotEmpty(t, freeTier.Description)
	assert.Equal(t, 3, freeTier.Limits.Accounts)
	assert.Equal(t, 1, freeTier.Limits.Rules)
	assert.Equal(t, 1, freeTier.Limits.ExecutionsPerDay)
	assert.Equal(t, 1440, freeTier.Limits.MinExecutionIntervalMinutes)
	assert.True(t, freeTier.Features.AdsEnabled)
	assert.False(t, freeTier.Features.AIInsights)
	assert.Equal(t, 0.0, freeTier.Pricing.PriceMonthly)
}

// TDD RED: Test that cache headers are set correctly
func TestSubscriptionTiersHandler_GetSubscriptionTiers_CacheHeaders(t *testing.T) {
	// Load real tier config
	tierConfig, err := config.LoadTierConfig()
	require.NoError(t, err)

	// Create handler
	handler := NewSubscriptionTiersHandler(tierConfig)

	// Create Echo context
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/subscription/tiers", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Call handler
	err = handler.GetSubscriptionTiers(c)
	require.NoError(t, err)

	// Assert cache headers
	cacheControl := rec.Header().Get("Cache-Control")
	assert.Equal(t, "public, max-age=86400", cacheControl, "Should set 24-hour cache")
}

// TDD RED: Test that Content-Type is application/json
func TestSubscriptionTiersHandler_GetSubscriptionTiers_ContentType(t *testing.T) {
	// Load real tier config
	tierConfig, err := config.LoadTierConfig()
	require.NoError(t, err)

	// Create handler
	handler := NewSubscriptionTiersHandler(tierConfig)

	// Create Echo context
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/subscription/tiers", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Call handler
	err = handler.GetSubscriptionTiers(c)
	require.NoError(t, err)

	// Assert content type
	contentType := rec.Header().Get("Content-Type")
	assert.Contains(t, contentType, "application/json")
}

// TDD RED: Test handler with nil config (error case)
func TestSubscriptionTiersHandler_NilConfig(t *testing.T) {
	// This should panic or fail gracefully
	// We expect the handler to require a valid config
	assert.Panics(t, func() {
		NewSubscriptionTiersHandler(nil)
	}, "Should panic with nil config")
}
