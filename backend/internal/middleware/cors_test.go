package middleware

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetCORSConfig(t *testing.T) {
	tests := []struct {
		name           string
		allowedOrigins string
		expectedOrigins []string
		expectDefaults  bool
	}{
		{
			name:            "Empty string allows all origins",
			allowedOrigins:  "",
			expectedOrigins: []string{"*"},
			expectDefaults:  true,
		},
		{
			name:            "Asterisk allows all origins",
			allowedOrigins:  "*",
			expectedOrigins: []string{"*"},
			expectDefaults:  true,
		},
		{
			name:            "Single origin",
			allowedOrigins:  "https://boostedbudget.com",
			expectedOrigins: []string{"https://boostedbudget.com"},
			expectDefaults:  true,
		},
		{
			name:            "Multiple origins comma-separated",
			allowedOrigins:  "https://boostedbudget.com,https://www.boostedbudget.com",
			expectedOrigins: []string{"https://boostedbudget.com", "https://www.boostedbudget.com"},
			expectDefaults:  true,
		},
		{
			name:            "Multiple origins with whitespace",
			allowedOrigins:  "https://boostedbudget.com, https://www.boostedbudget.com , https://api.boostedbudget.com",
			expectedOrigins: []string{"https://boostedbudget.com", "https://www.boostedbudget.com", "https://api.boostedbudget.com"},
			expectDefaults:  true,
		},
		{
			name:            "Tailscale URL for staging",
			allowedOrigins:  "https://qa.example.ts.net:3443,https://localhost:3443",
			expectedOrigins: []string{"https://qa.example.ts.net:3443", "https://localhost:3443"},
			expectDefaults:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := GetCORSConfig(tt.allowedOrigins)

			// Verify allowed origins
			assert.Equal(t, tt.expectedOrigins, config.AllowOrigins, "AllowOrigins should match")

			if tt.expectDefaults {
				// Verify default allowed methods
				expectedMethods := []string{
					http.MethodGet,
					http.MethodPost,
					http.MethodPut,
					http.MethodPatch,
					http.MethodDelete,
					http.MethodOptions,
				}
				assert.Equal(t, expectedMethods, config.AllowMethods, "AllowMethods should include all standard methods")

				// Verify default allowed headers
				expectedHeaders := []string{"Authorization", "Content-Type", "X-Requested-With"}
				assert.Equal(t, expectedHeaders, config.AllowHeaders, "AllowHeaders should include auth and content headers")

				// Verify credentials and max age
				assert.True(t, config.AllowCredentials, "AllowCredentials should be true")
				assert.Equal(t, 86400, config.MaxAge, "MaxAge should be 24 hours")
			}
		})
	}
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
