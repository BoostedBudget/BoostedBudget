package middleware

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetCORSConfig(t *testing.T) {
	tests := []struct {
		name            string
		allowedOrigins  string
		expectedOrigins []string
	}{
		{
			name:            "Empty string allows all origins",
			allowedOrigins:  "",
			expectedOrigins: []string{"*"},
		},
		{
			name:            "Asterisk allows all origins",
			allowedOrigins:  "*",
			expectedOrigins: []string{"*"},
		},
		{
			name:            "Single origin",
			allowedOrigins:  "https://boostedbudget.com",
			expectedOrigins: []string{"https://boostedbudget.com"},
		},
		{
			name:            "Multiple origins comma-separated",
			allowedOrigins:  "https://boostedbudget.com,https://www.boostedbudget.com",
			expectedOrigins: []string{"https://boostedbudget.com", "https://www.boostedbudget.com"},
		},
		{
			name:            "Multiple origins with whitespace",
			allowedOrigins:  "https://boostedbudget.com, https://www.boostedbudget.com , https://api.boostedbudget.com",
			expectedOrigins: []string{"https://boostedbudget.com", "https://www.boostedbudget.com", "https://api.boostedbudget.com"},
		},
		{
			name:            "Tailscale URL for staging",
			allowedOrigins:  "https://yingyues-mac-mini.tailce219d.ts.net:3443,https://localhost:3443",
			expectedOrigins: []string{"https://yingyues-mac-mini.tailce219d.ts.net:3443", "https://localhost:3443"},
		},
	}

	expectedMethods := []string{
		http.MethodGet,
		http.MethodPost,
		http.MethodPut,
		http.MethodPatch,
		http.MethodDelete,
		http.MethodOptions,
	}
	expectedHeaders := []string{"Authorization", "Content-Type", "X-Requested-With"}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := GetCORSConfig(tt.allowedOrigins)

			assert.Equal(t, tt.expectedOrigins, config.AllowOrigins)
			assert.Equal(t, expectedMethods, config.AllowMethods)
			assert.Equal(t, expectedHeaders, config.AllowHeaders)
			assert.True(t, config.AllowCredentials)
			assert.Equal(t, 86400, config.MaxAge)
		})
	}
}

func TestValidateCORSConfig_ProductionBlocksWildcard(t *testing.T) {
	err := ValidateCORSConfig("*", "production")
	assert.Error(t, err, "wildcard origins must be rejected in production")
	assert.Contains(t, err.Error(), "production")
}

func TestValidateCORSConfig_ProductionBlocksEmpty(t *testing.T) {
	err := ValidateCORSConfig("", "production")
	assert.Error(t, err, "empty origins must be rejected in production")
}

func TestValidateCORSConfig_StagingAllowsWildcardWithoutError(t *testing.T) {
	err := ValidateCORSConfig("*", "staging")
	assert.NoError(t, err, "wildcard in staging should return nil (just log warning)")
}

func TestValidateCORSConfig_DevelopmentAllowsWildcard(t *testing.T) {
	err := ValidateCORSConfig("*", "development")
	assert.NoError(t, err, "wildcard in development is fine")
}

func TestValidateCORSConfig_ValidURLsPass(t *testing.T) {
	err := ValidateCORSConfig("https://boostedbudget.com,https://www.boostedbudget.com", "production")
	assert.NoError(t, err)
}

func TestValidateCORSConfig_MalformedURLRejected(t *testing.T) {
	err := ValidateCORSConfig("not-a-url,https://boostedbudget.com", "production")
	assert.Error(t, err, "origin not starting with http:// or https:// must be rejected")
}

func TestGetCORSConfig_EdgeCases(t *testing.T) {
	t.Run("Handles trailing commas", func(t *testing.T) {
		config := GetCORSConfig("https://boostedbudget.com,")
		assert.Equal(t, []string{"https://boostedbudget.com"}, config.AllowOrigins)
	})

	t.Run("Handles only commas and whitespace", func(t *testing.T) {
		config := GetCORSConfig(",  ,  ,")
		// Should result in empty slice, not ["*"]
		assert.Empty(t, config.AllowOrigins)
	})

	t.Run("Preserves protocol and port in origins", func(t *testing.T) {
		config := GetCORSConfig("https://example.com:8443,http://localhost:3000")
		expected := []string{"https://example.com:8443", "http://localhost:3000"}
		assert.Equal(t, expected, config.AllowOrigins)
	})
}
