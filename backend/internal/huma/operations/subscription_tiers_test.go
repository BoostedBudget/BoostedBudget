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
