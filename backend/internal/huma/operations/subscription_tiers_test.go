package operations

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/maxnussbaum/BoostedBudget/internal/config"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSubscriptionTiersHandler_GetSubscriptionTiers tests that the endpoint returns 200.
func TestSubscriptionTiersHandler_GetSubscriptionTiers(t *testing.T) {
	// Load real tier config
	tierConfig, err := config.LoadTierConfig()
	require.NoError(t, err, "Failed to load tier config")

	// Create test API
	_, api := humatest.New(t)

	// Create handler and register routes
	handler := NewSubscriptionTiersHandler(tierConfig)
	RegisterSubscriptionTiersRoutes(api, handler)

	// Execute request
	resp := api.Get("/api/v1/subscription/tiers")

	// Assert response
	assert.Equal(t, http.StatusOK, resp.Code)
}

// TestSubscriptionTiersHandler_GetSubscriptionTiers_ResponseStructure tests JSON structure.
func TestSubscriptionTiersHandler_GetSubscriptionTiers_ResponseStructure(t *testing.T) {
	// Load real tier config
	tierConfig, err := config.LoadTierConfig()
	require.NoError(t, err)

	// Create test API
	_, api := humatest.New(t)

	// Create handler and register routes
	handler := NewSubscriptionTiersHandler(tierConfig)
	RegisterSubscriptionTiersRoutes(api, handler)

	// Execute request
	resp := api.Get("/api/v1/subscription/tiers")
	require.Equal(t, http.StatusOK, resp.Code)

	// Parse response
	var response SubscriptionTiersResponse
	err = json.Unmarshal(resp.Body.Bytes(), &response)
	require.NoError(t, err, "Response should be valid JSON")

	// Assert structure
	assert.NotNil(t, response.Tiers)
	assert.NotEmpty(t, response.DefaultTier)
	assert.Len(t, response.Tiers, 5, "Should have 5 tiers")

	// Verify all expected tiers exist
	_, hasPremium := response.Tiers[domain.TierPremium]
	assert.True(t, hasPremium, "Should have premium tier")

	_, hasHousehold2 := response.Tiers[domain.TierHousehold2]
	assert.True(t, hasHousehold2, "Should have household_2 tier")

	_, hasHousehold3 := response.Tiers[domain.TierHousehold3]
	assert.True(t, hasHousehold3, "Should have household_3 tier")

	_, hasHousehold4 := response.Tiers[domain.TierHousehold4]
	assert.True(t, hasHousehold4, "Should have household_4 tier")

	_, hasHousehold5 := response.Tiers[domain.TierHousehold5]
	assert.True(t, hasHousehold5, "Should have household_5 tier")

	// Verify default tier is "premium"
	assert.Equal(t, domain.TierPremium, response.DefaultTier)
}

// TestSubscriptionTiersHandler_GetSubscriptionTiers_TierFields tests tier fields.
func TestSubscriptionTiersHandler_GetSubscriptionTiers_TierFields(t *testing.T) {
	// Load real tier config
	tierConfig, err := config.LoadTierConfig()
	require.NoError(t, err)

	// Create test API
	_, api := humatest.New(t)

	// Create handler and register routes
	handler := NewSubscriptionTiersHandler(tierConfig)
	RegisterSubscriptionTiersRoutes(api, handler)

	// Execute request
	resp := api.Get("/api/v1/subscription/tiers")
	require.Equal(t, http.StatusOK, resp.Code)

	// Parse response
	var response SubscriptionTiersResponse
	err = json.Unmarshal(resp.Body.Bytes(), &response)
	require.NoError(t, err)

	// Verify premium tier fields
	premiumTier := response.Tiers[domain.TierPremium]
	assert.Equal(t, domain.TierPremium, premiumTier.Name)
	assert.Equal(t, "Premium", premiumTier.DisplayName)
	assert.NotEmpty(t, premiumTier.Description)
	assert.Equal(t, 30, premiumTier.Limits.Accounts)
	assert.Equal(t, 730, premiumTier.Limits.TransactionHistoryDays)
	assert.Equal(t, 6, premiumTier.Limits.ExecutionsPerDay)
	assert.False(t, premiumTier.Features.AdsEnabled)
	assert.True(t, premiumTier.Features.AIInsights)
	assert.Equal(t, 8.99, premiumTier.Pricing.PriceMonthly)
}

// TestSubscriptionTiersHandler_GetSubscriptionTiers_CacheHeaders tests cache headers.
func TestSubscriptionTiersHandler_GetSubscriptionTiers_CacheHeaders(t *testing.T) {
	// Load real tier config
	tierConfig, err := config.LoadTierConfig()
	require.NoError(t, err)

	// Create test API
	_, api := humatest.New(t)

	// Create handler and register routes
	handler := NewSubscriptionTiersHandler(tierConfig)
	RegisterSubscriptionTiersRoutes(api, handler)

	// Execute request
	resp := api.Get("/api/v1/subscription/tiers")
	require.Equal(t, http.StatusOK, resp.Code)

	// Assert cache headers
	cacheControl := resp.Header().Get("Cache-Control")
	assert.Equal(t, "public, max-age=86400", cacheControl, "Should set 24-hour cache")
}

// TestSubscriptionTiersHandler_GetSubscriptionTiers_ContentType tests content type.
func TestSubscriptionTiersHandler_GetSubscriptionTiers_ContentType(t *testing.T) {
	// Load real tier config
	tierConfig, err := config.LoadTierConfig()
	require.NoError(t, err)

	// Create test API
	_, api := humatest.New(t)

	// Create handler and register routes
	handler := NewSubscriptionTiersHandler(tierConfig)
	RegisterSubscriptionTiersRoutes(api, handler)

	// Execute request
	resp := api.Get("/api/v1/subscription/tiers")
	require.Equal(t, http.StatusOK, resp.Code)

	// Assert content type
	contentType := resp.Header().Get("Content-Type")
	assert.Contains(t, contentType, "application/json")
}

// TestSubscriptionTiersHandler_NilConfig tests handler with nil config.
func TestSubscriptionTiersHandler_NilConfig(t *testing.T) {
	// This should panic
	assert.Panics(t, func() {
		NewSubscriptionTiersHandler(nil)
	}, "Should panic with nil config")
}
