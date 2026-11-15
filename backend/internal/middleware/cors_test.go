package middleware

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
			allowedOrigins:  "https://qa.example.ts.net:3443,https://localhost:3443",
			expectedOrigins: []string{"https://qa.example.ts.net:3443", "https://localhost:3443"},
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
			// #1609: without ExposeHeaders, Retry-After (set by IPRateLimiter on a 429) is not
			// readable cross-origin by a browser client.
			assert.Equal(t, []string{"Retry-After"}, config.ExposeHeaders)
			assert.True(t, config.AllowCredentials)
			assert.Equal(t, 86400, config.MaxAge)
		})
	}
}

func TestValidateCORSConfig_ProductionBlocksWildcard(t *testing.T) {
	err := ValidateCORSConfig("*", "production")
	require.Error(t, err, "wildcard origins must be rejected in production")
	assert.Contains(t, err.Error(), "production")
}

func TestValidateCORSConfig_ProductionBlocksEmpty(t *testing.T) {
	err := ValidateCORSConfig("", "production")
	assert.Error(t, err, "empty origins must be rejected in production")
}

// TestValidateCORSConfig_StagingBlocksWildcard is the RED test for issue #1514 (F10): staging
// used to only warn on a wildcard CORS_ALLOWED_ORIGINS, the same permissive treatment as
// development. That is an inconsistent guardrail — see cors.go's corsPermissiveEnvironments
// allowlist — and a bad value copy-pasted from a warned-but-running staging box is exactly how
// a wildcard reaches production. Staging must now fail closed exactly like production.
func TestValidateCORSConfig_StagingBlocksWildcard(t *testing.T) {
	err := ValidateCORSConfig("*", "staging")
	require.Error(t, err, "wildcard origins must be rejected in staging")
	assert.Contains(t, err.Error(), "staging")
}

// TestValidateCORSConfig_StagingBlocksEmpty covers the other half of the wildcard check: an
// empty CORS_ALLOWED_ORIGINS is treated identically to "*" (see isWildcard in cors.go).
func TestValidateCORSConfig_StagingBlocksEmpty(t *testing.T) {
	err := ValidateCORSConfig("", "staging")
	require.Error(t, err, "empty origins must be rejected in staging")
	assert.Contains(t, err.Error(), "staging")
}

// TestValidateCORSConfig_StagingAllowsExplicitOrigins pins that the staging fix only closes
// the wildcard/empty gap — a real, explicit origin list still passes staging validation.
func TestValidateCORSConfig_StagingAllowsExplicitOrigins(t *testing.T) {
	err := ValidateCORSConfig("https://staging.boostedbudget.com", "staging")
	assert.NoError(t, err, "an explicit staging origin must not be rejected")
}

func TestValidateCORSConfig_DevelopmentAllowsWildcard(t *testing.T) {
	err := ValidateCORSConfig("*", "development")
	assert.NoError(t, err, "wildcard in development is fine")
}

// TestValidateCORSConfig_QAAllowsWildcard pins that the deployed QA environment (ENVIRONMENT=qa
// in docker-compose.qa.yml, .github/workflows/deploy-qa.yml, and the Playwright E2E backend in
// apps/web/playwright.config.ts) keeps its permissive local/test CORS — this fix must not touch
// QA, only staging and unrecognised environment names.
func TestValidateCORSConfig_QAAllowsWildcard(t *testing.T) {
	err := ValidateCORSConfig("*", "qa")
	assert.NoError(t, err, "wildcard in qa is fine — QA/E2E rely on permissive CORS")
}

// TestValidateCORSConfig_UnrecognisedEnvironmentBlocksWildcard: an unset/misspelled/unknown
// ENVIRONMENT value must fail SAFE (no wildcard CORS) rather than silently falling through to
// the permissive branch, matching the allowlist pattern in
// backend/internal/config/qa_auth_bypass_gate.go and domain.IsNonProductionEnvironment. Before
// this fix, ValidateCORSConfig's switch only special-cased "production" and "staging"; any other
// string — including a typo like "stagng" or an empty environment argument — fell through and
// returned nil regardless of the wildcard, which is the same fail-open shape this issue exists
// to close.
func TestValidateCORSConfig_UnrecognisedEnvironmentBlocksWildcard(t *testing.T) {
	err := ValidateCORSConfig("*", "stagng")
	assert.Error(t, err, "an unrecognised environment must fail closed on wildcard CORS")
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
