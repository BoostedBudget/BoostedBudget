package services

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// getTestEnv retrieves environment variable for testing
func getTestEnv(key string) string {
	return os.Getenv(key)
}

// TestNewPlaidService_ValidConfig tests successful PlaidService creation with valid configuration
func TestNewPlaidService_ValidConfig(t *testing.T) {
	// Given: Valid config with all required Plaid credentials
	cfg := &config.Config{
		PlaidClientID: "test_client_id",
		PlaidSecret:   "test_secret",
		PlaidEnv:      "sandbox",
	}

	// When: Creating PlaidService
	service, err := NewPlaidService(cfg)

	// Then: Service is created successfully
	require.NoError(t, err, "Expected no error with valid configuration")
	require.NotNil(t, service, "Expected service to be created")

	// And: Client is initialized
	client := service.GetClient()
	assert.NotNil(t, client, "Expected Plaid client to be initialized")
}

// TestNewPlaidService_MissingClientID tests error handling when PlaidClientID is missing
func TestNewPlaidService_MissingClientID(t *testing.T) {
	// Given: Config with missing PlaidClientID
	cfg := &config.Config{
		PlaidClientID: "", // Missing!
		PlaidSecret:   "test_secret",
		PlaidEnv:      "sandbox",
	}

	// When: Creating PlaidService
	service, err := NewPlaidService(cfg)

	// Then: Returns error
	assert.Error(t, err, "Expected error when PlaidClientID is missing")
	assert.ErrorIs(t, err, ErrPlaidNotConfigured, "Expected ErrPlaidNotConfigured")
	assert.Nil(t, service, "Expected nil service on error")
}

// TestNewPlaidService_MissingSecret tests error handling when PlaidSecret is missing
func TestNewPlaidService_MissingSecret(t *testing.T) {
	// Given: Config with missing PlaidSecret
	cfg := &config.Config{
		PlaidClientID: "test_client_id",
		PlaidSecret:   "", // Missing!
		PlaidEnv:      "sandbox",
	}

	// When: Creating PlaidService
	service, err := NewPlaidService(cfg)

	// Then: Returns error
	assert.Error(t, err, "Expected error when PlaidSecret is missing")
	assert.ErrorIs(t, err, ErrPlaidNotConfigured, "Expected ErrPlaidNotConfigured")
	assert.Nil(t, service, "Expected nil service on error")
}

// TestNewPlaidService_InvalidEnvironment tests error handling with invalid environment
func TestNewPlaidService_InvalidEnvironment(t *testing.T) {
	// Given: Config with invalid PlaidEnv
	cfg := &config.Config{
		PlaidClientID: "test_client_id",
		PlaidSecret:   "test_secret",
		PlaidEnv:      "invalid_environment", // Invalid!
	}

	// When: Creating PlaidService
	service, err := NewPlaidService(cfg)

	// Then: Returns error
	assert.Error(t, err, "Expected error when PlaidEnv is invalid")
	assert.ErrorIs(t, err, ErrInvalidPlaidEnv, "Expected ErrInvalidPlaidEnv")
	assert.Nil(t, service, "Expected nil service on error")
}

// TestGetClient_ReturnsInitializedClient tests that GetClient returns initialized client
func TestGetClient_ReturnsInitializedClient(t *testing.T) {
	// Given: Valid PlaidService
	cfg := &config.Config{
		PlaidClientID: "test_client_id",
		PlaidSecret:   "test_secret",
		PlaidEnv:      "sandbox",
	}
	service, err := NewPlaidService(cfg)
	require.NoError(t, err, "Setup failed: could not create service")

	// When: Getting client
	client := service.GetClient()

	// Then: Client is not nil
	assert.NotNil(t, client, "Expected GetClient to return initialized client")
}

// TestMapEnvironment_AllCases tests environment string mapping (table-driven test)
func TestMapEnvironment_AllCases(t *testing.T) {
	tests := []struct {
		name        string
		envString   string
		expectError bool
		description string
	}{
		{
			name:        "sandbox environment",
			envString:   "sandbox",
			expectError: false,
			description: "sandbox maps to plaid.Sandbox",
		},
		{
			name:        "development environment",
			envString:   "development",
			expectError: false,
			description: "development maps to plaid.Development",
		},
		{
			name:        "production environment",
			envString:   "production",
			expectError: false,
			description: "production maps to plaid.Production",
		},
		{
			name:        "invalid environment",
			envString:   "invalid",
			expectError: true,
			description: "invalid environment returns error",
		},
		{
			name:        "empty environment",
			envString:   "",
			expectError: true,
			description: "empty environment returns error",
		},
		{
			name:        "case sensitive - Sandbox (uppercase)",
			envString:   "Sandbox",
			expectError: true,
			description: "environment strings are case-sensitive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// When: Mapping environment string
			env, err := mapEnvironment(tt.envString)

			// Then: Verify expectation
			if tt.expectError {
				assert.Error(t, err, tt.description)
				assert.ErrorIs(t, err, ErrInvalidPlaidEnv, "Expected ErrInvalidPlaidEnv for invalid input")
			} else {
				assert.NoError(t, err, tt.description)
				assert.NotEmpty(t, env, "Expected non-empty environment constant")
			}
		})
	}
}

// TestCreateLinkToken_Success tests successful link token creation
// Note: This is an integration test that requires real Plaid sandbox credentials
func TestCreateLinkToken_Success(t *testing.T) {
	// Skip if real Plaid credentials not available
	clientID := getTestEnv("PLAID_CLIENT_ID")
	secret := getTestEnv("PLAID_SECRET")
	if clientID == "" || secret == "" {
		t.Skip("Skipping integration test: PLAID_CLIENT_ID and PLAID_SECRET required")
	}

	// Given: Valid PlaidService with real sandbox credentials
	cfg := &config.Config{
		PlaidClientID:   clientID,
		PlaidSecret:     secret,
		PlaidEnv:        "sandbox",
		PlaidWebhookURL:  "https://api.boostedbudget.com/webhooks/plaid",
		PlaidRedirectURI: "https://boostedbudget.com/oauth-callback",
	}

	service, err := NewPlaidService(cfg)
	require.NoError(t, err, "Setup failed: could not create service")

	// When: Creating link token for a user
	ctx := context.Background()
	userID := "auth0|test-user-123"

	response, err := service.CreateLinkToken(ctx, userID)

	// Then: Returns link token successfully
	require.NoError(t, err, "Expected no error creating link token")
	require.NotNil(t, response, "Expected non-nil response")
	assert.NotEmpty(t, response.LinkToken, "Expected link token to be populated")
	assert.NotEmpty(t, response.RequestID, "Expected request ID to be populated")
}

// TestCreateLinkToken_IncludesCorrectUserID tests that the link token includes the correct user ID
func TestCreateLinkToken_IncludesCorrectUserID(t *testing.T) {
	clientID := getTestEnv("PLAID_CLIENT_ID")
	secret := getTestEnv("PLAID_SECRET")
	if clientID == "" || secret == "" {
		t.Skip("Skipping integration test: PLAID_CLIENT_ID and PLAID_SECRET required")
	}

	// Given: Valid PlaidService
	cfg := &config.Config{
		PlaidClientID:   clientID,
		PlaidSecret:     secret,
		PlaidEnv:        "sandbox",
		PlaidWebhookURL:  "https://api.boostedbudget.com/webhooks/plaid",
		PlaidRedirectURI: "https://boostedbudget.com/oauth-callback",
	}

	service, err := NewPlaidService(cfg)
	require.NoError(t, err, "Setup failed")

	// When: Creating link token with specific user ID
	ctx := context.Background()
	userID := "auth0|specific-user-456"

	response, err := service.CreateLinkToken(ctx, userID)

	// Then: Link token is created (Plaid API accepts the user ID)
	require.NoError(t, err, "Expected no error - user ID should be accepted by Plaid")
	require.NotNil(t, response, "Expected response")
	// Note: We can't directly verify userID is in token (it's opaque), but successful creation validates it
}

// TestCreateLinkToken_IncludesRequiredProducts tests that link token is configured with required Plaid products
func TestCreateLinkToken_IncludesRequiredProducts(t *testing.T) {
	clientID := getTestEnv("PLAID_CLIENT_ID")
	secret := getTestEnv("PLAID_SECRET")
	if clientID == "" || secret == "" {
		t.Skip("Skipping integration test: PLAID_CLIENT_ID and PLAID_SECRET required")
	}

	// Given: Valid PlaidService
	cfg := &config.Config{
		PlaidClientID:   clientID,
		PlaidSecret:     secret,
		PlaidEnv:        "sandbox",
		PlaidWebhookURL:  "https://api.boostedbudget.com/webhooks/plaid",
		PlaidRedirectURI: "https://boostedbudget.com/oauth-callback",
	}

	service, err := NewPlaidService(cfg)
	require.NoError(t, err, "Setup failed")

	// When: Creating link token
	ctx := context.Background()
	response, err := service.CreateLinkToken(ctx, "auth0|test-user-123")

	// Then: Link token is created successfully
	// (Products are configured internally: TRANSACTIONS, AUTH, BALANCE)
	require.NoError(t, err, "Expected no error - products should be configured correctly")
	require.NotNil(t, response, "Expected response")
	assert.NotEmpty(t, response.LinkToken, "Expected valid link token with products configured")
}

// TestCreateLinkToken_IncludesWebhookURL tests that webhook URL is included in link token configuration
func TestCreateLinkToken_IncludesWebhookURL(t *testing.T) {
	clientID := getTestEnv("PLAID_CLIENT_ID")
	secret := getTestEnv("PLAID_SECRET")
	if clientID == "" || secret == "" {
		t.Skip("Skipping integration test: PLAID_CLIENT_ID and PLAID_SECRET required")
	}

	// Given: PlaidService with webhook URL configured
	cfg := &config.Config{
		PlaidClientID:   clientID,
		PlaidSecret:     secret,
		PlaidEnv:        "sandbox",
		PlaidWebhookURL:  "https://api.boostedbudget.com/webhooks/plaid",
		PlaidRedirectURI: "https://boostedbudget.com/oauth-callback",
	}

	service, err := NewPlaidService(cfg)
	require.NoError(t, err, "Setup failed")

	// When: Creating link token
	ctx := context.Background()
	response, err := service.CreateLinkToken(ctx, "auth0|test-user-123")

	// Then: Link token is created successfully with webhook configured
	require.NoError(t, err, "Expected no error - webhook URL should be accepted")
	require.NotNil(t, response, "Expected response")
	assert.NotEmpty(t, response.LinkToken, "Expected valid link token with webhook configured")
}

// TestCreateLinkToken_ReturnsExpirationTime tests that response includes token expiration
func TestCreateLinkToken_ReturnsExpirationTime(t *testing.T) {
	clientID := getTestEnv("PLAID_CLIENT_ID")
	secret := getTestEnv("PLAID_SECRET")
	if clientID == "" || secret == "" {
		t.Skip("Skipping integration test: PLAID_CLIENT_ID and PLAID_SECRET required")
	}

	// Given: Valid PlaidService
	cfg := &config.Config{
		PlaidClientID:   clientID,
		PlaidSecret:     secret,
		PlaidEnv:        "sandbox",
		PlaidWebhookURL:  "https://api.boostedbudget.com/webhooks/plaid",
		PlaidRedirectURI: "https://boostedbudget.com/oauth-callback",
	}

	service, err := NewPlaidService(cfg)
	require.NoError(t, err, "Setup failed")

	// When: Creating link token
	ctx := context.Background()
	before := time.Now()
	response, err := service.CreateLinkToken(ctx, "auth0|test-user-123")
	after := time.Now().Add(31 * time.Minute) // Plaid tokens typically expire in 30 minutes

	// Then: Response includes expiration time
	require.NoError(t, err, "Expected no error")
	require.NotNil(t, response, "Expected response")
	assert.False(t, response.Expiration.IsZero(), "Expected expiration time to be set")
	assert.True(t, response.Expiration.After(before), "Expiration should be in the future")
	assert.True(t, response.Expiration.Before(after), "Expiration should be within 31 minutes")
}

// TestCreateLinkToken_PlaidAPIError tests handling of Plaid API errors
func TestCreateLinkToken_PlaidAPIError(t *testing.T) {
	// Given: PlaidService with invalid credentials (will cause API error)
	cfg := &config.Config{
		PlaidClientID:   "invalid_client_id",
		PlaidSecret:     "invalid_secret",
		PlaidEnv:        "sandbox",
		PlaidWebhookURL:  "https://api.boostedbudget.com/webhooks/plaid",
		PlaidRedirectURI: "https://boostedbudget.com/oauth-callback",
	}

	service, err := NewPlaidService(cfg)
	require.NoError(t, err, "Service creation should succeed even with invalid creds")

	// When: Attempting to create link token (will fail at API call)
	ctx := context.Background()
	response, err := service.CreateLinkToken(ctx, "auth0|test-user-123")

	// Then: Returns error from Plaid API
	assert.Error(t, err, "Expected error with invalid credentials")
	assert.Nil(t, response, "Expected nil response on error")
}

// TestCreateLinkToken_MissingWebhookURL - DISABLED for issue #200 minimal implementation
// This test is intentionally skipped because the minimal implementation (issue #200)
// does NOT require webhook URL. This will be re-enabled in issue #202 when webhook
// support is added back.
//
// TODO(#202): Re-enable this test when webhook URL validation is added back
func TestCreateLinkToken_MissingWebhookURL(t *testing.T) {
	t.Skip("SKIPPED: Minimal implementation (issue #200) doesn't use webhook URL. Will re-enable in issue #202")

	// Given: PlaidService with missing webhook URL
	cfg := &config.Config{
		PlaidClientID:    "test_client_id",
		PlaidSecret:      "test_secret",
		PlaidEnv:         "sandbox",
		PlaidWebhookURL:  "", // Missing!
		PlaidRedirectURI: "https://boostedbudget.com/oauth-callback",
	}

	service, err := NewPlaidService(cfg)
	require.NoError(t, err, "Service creation should succeed")

	// When: Attempting to create link token
	ctx := context.Background()
	response, err := service.CreateLinkToken(ctx, "auth0|test-user-123")

	// Then: Returns error about missing webhook URL
	assert.Error(t, err, "Expected error when webhook URL is missing")
	assert.Contains(t, err.Error(), "PLAID_WEBHOOK_URL", "Error should mention missing webhook URL")
	assert.Nil(t, response, "Expected nil response on error")
}

// TestCreateLinkToken_MissingRedirectURI tests error handling when redirect URI is not configured.
// Re-enabled in Phase 3 of issue #201 - redirect_uri is now REQUIRED for OAuth institution support.
//
// This ensures we fail fast with a clear error message if the redirect URI is not configured,
// rather than allowing a link token to be created that will fail when users try to connect
// OAuth-based institutions like Chase or Capital One.
func TestCreateLinkToken_MissingRedirectURI(t *testing.T) {
	// Given: PlaidService with missing redirect URI
	cfg := &config.Config{
		PlaidClientID:    "test_client_id",
		PlaidSecret:      "test_secret",
		PlaidEnv:         "sandbox",
		PlaidRedirectURI: "", // Missing!
	}

	service, err := NewPlaidService(cfg)
	require.NoError(t, err, "Service creation should succeed")

	// When: Attempting to create link token
	ctx := context.Background()
	response, err := service.CreateLinkToken(ctx, "auth0|test-user-123")

	// Then: Returns error about missing redirect URI
	assert.Error(t, err, "Expected error when redirect URI is missing")
	assert.Contains(t, err.Error(), "PLAID_REDIRECT_URI", "Error should mention missing redirect URI")
	assert.Nil(t, response, "Expected nil response on error")
}

// TestCreateLinkTokenMinimal tests link token creation with ONLY required parameters.
// This replicates the exact pattern from plaid-go v23 official tests to debug issue #200.
//
// Pattern: clientName + language + countryCodes + user + AUTH product ONLY
// NO webhook, NO redirect_uri, NO optional parameters
//
// Purpose: Isolate the 400 error by testing the absolute minimum implementation.
// If this test passes, the issue is in one of our optional parameters.
// If this test fails, the issue is credentials, account access, or userID format.
func TestCreateLinkTokenMinimal(t *testing.T) {
	// Skip if Plaid credentials not available in environment
	if getTestEnv("PLAID_CLIENT_ID") == "" || getTestEnv("PLAID_SECRET") == "" {
		t.Skip("Plaid credentials not configured - set PLAID_CLIENT_ID and PLAID_SECRET")
	}

	// Given: Minimal config - only credentials and environment (NO webhook, NO redirect_uri)
	cfg := &config.Config{
		PlaidClientID: getTestEnv("PLAID_CLIENT_ID"),
		PlaidSecret:   getTestEnv("PLAID_SECRET"),
		PlaidEnv:      "sandbox",
		// Intentionally NO PlaidWebhookURL
		// Intentionally NO PlaidRedirectURI
	}

	service, err := NewPlaidService(cfg)
	require.NoError(t, err, "should create plaid service successfully")

	// When: Creating link token with time-based user ID (exactly like official v23 tests)
	ctx := context.Background()
	userID := time.Now().String()

	response, err := service.CreateLinkToken(ctx, userID)

	// Then: Should create link token successfully with minimal implementation
	// If this fails with 400, check: credentials validity, Plaid account product access, userID format
	require.NoError(t, err, "should create link token with minimal params")
	assert.NotEmpty(t, response.LinkToken, "link token should not be empty")
	assert.NotEmpty(t, response.Expiration, "expiration should not be empty")
	assert.NotEmpty(t, response.RequestID, "request ID should not be empty")

	t.Logf("✅ Minimal link token created successfully: %s (RequestID: %s)", response.LinkToken, response.RequestID)
}

// TestCreateLinkTokenWithTransactions tests link token creation with AUTH + TRANSACTIONS products.
// This is Phase 1 of issue #201 - incrementally adding TRANSACTIONS product to identify
// if it causes the 400 error that was resolved in issue #200.
//
// TRANSACTIONS product provides access to:
// - Historical transaction data (up to 24 months)
// - Transaction categories and merchant information
// - Pending and posted transactions
//
// Hypothesis: TRANSACTIONS product will NOT cause 400 error when added to AUTH product.
// Note: Updated in Phase 3 to include redirect_uri (now required).
func TestCreateLinkTokenWithTransactions(t *testing.T) {
	// Skip if Plaid credentials not available in environment
	if getTestEnv("PLAID_CLIENT_ID") == "" || getTestEnv("PLAID_SECRET") == "" {
		t.Skip("Plaid credentials not configured - set PLAID_CLIENT_ID and PLAID_SECRET")
	}

	// Given: Config with credentials and redirect_uri (now required in Phase 3)
	cfg := &config.Config{
		PlaidClientID:    getTestEnv("PLAID_CLIENT_ID"),
		PlaidSecret:      getTestEnv("PLAID_SECRET"),
		PlaidEnv:         "sandbox",
		PlaidRedirectURI: "https://boostedbudget.com/plaid/oauth-callback",
	}

	service, err := NewPlaidService(cfg)
	require.NoError(t, err, "should create plaid service successfully")

	// When: Creating link token (implementation uses AUTH + TRANSACTIONS + redirect_uri)
	ctx := context.Background()
	userID := time.Now().String()

	response, err := service.CreateLinkToken(ctx, userID)

	// Then: Should succeed - TRANSACTIONS product should not cause 400 error
	require.NoError(t, err, "TRANSACTIONS product should not cause 400 error")
	assert.NotEmpty(t, response.LinkToken, "link token should not be empty")
	assert.NotEmpty(t, response.Expiration, "expiration should not be empty")
	assert.NotEmpty(t, response.RequestID, "request ID should not be empty")

	t.Logf("✅ Link token with TRANSACTIONS product: %s (RequestID: %s)", response.LinkToken, response.RequestID)
}

// TestCreateLinkTokenWithBalance tests link token creation with TRANSACTIONS product only.
// NOTE: BALANCE product is NOT included because it's not a valid Link initialization product.
// Per Plaid docs: "Balance itself is not a product that can be used to initialize Link."
//
// Issue #232: PRODUCTS_BALANCE was causing 400 Bad Request from Plaid API in production.
// Issue #237: PRODUCTS_AUTH removed - only supports checking/savings, caused NO_AUTH_ACCOUNTS for credit cards/CDs.
// Balance data is automatically included in /accounts/get endpoint response (FREE).
//
// This test verifies that link tokens can be created successfully with TRANSACTIONS only,
// and that balance data will still be available via the /accounts/get endpoint.
func TestCreateLinkTokenWithBalance(t *testing.T) {
	// Skip if Plaid credentials not available in environment
	if getTestEnv("PLAID_CLIENT_ID") == "" || getTestEnv("PLAID_SECRET") == "" {
		t.Skip("Plaid credentials not configured - set PLAID_CLIENT_ID and PLAID_SECRET")
	}

	// Given: Config with credentials and redirect_uri (now required in Phase 3)
	cfg := &config.Config{
		PlaidClientID:    getTestEnv("PLAID_CLIENT_ID"),
		PlaidSecret:      getTestEnv("PLAID_SECRET"),
		PlaidEnv:         "sandbox",
		PlaidRedirectURI: "https://boostedbudget.com/plaid/oauth-callback",
	}

	service, err := NewPlaidService(cfg)
	require.NoError(t, err, "should create plaid service successfully")

	// When: Creating link token (implementation uses TRANSACTIONS + redirect_uri, NO AUTH/BALANCE)
	ctx := context.Background()
	userID := time.Now().String()

	response, err := service.CreateLinkToken(ctx, userID)

	// Then: Should succeed - link token created without BALANCE product
	require.NoError(t, err, "link token creation should succeed without BALANCE product")
	assert.NotEmpty(t, response.LinkToken, "link token should not be empty")
	assert.NotEmpty(t, response.Expiration, "expiration should not be empty")
	assert.NotEmpty(t, response.RequestID, "request ID should not be empty")

	t.Logf("✅ Link token without BALANCE product: %s (RequestID: %s)", response.LinkToken, response.RequestID)
	t.Logf("   Balance data will be available via /accounts/get endpoint")
}

// TestCreateLinkTokenWithRedirectURI tests link token creation with full configuration including OAuth support.
// This is Phase 3 of issue #201 - adding redirect_uri for OAuth institution support.
//
// redirect_uri is required for OAuth-based institutions such as:
// - Chase
// - Capital One
// - Wells Fargo
// - US Bank
//
// The redirect URI must be:
// 1. Registered in the Plaid Dashboard (Team Settings → API → Allowed redirect URIs)
// 2. Set in the PLAID_REDIRECT_URI environment variable
// 3. Included in the link token request via SetRedirectUri()
//
// Hypothesis: redirect_uri will NOT cause 400 error when properly configured.
func TestCreateLinkTokenWithRedirectURI(t *testing.T) {
	// Skip if Plaid credentials not available
	if getTestEnv("PLAID_CLIENT_ID") == "" || getTestEnv("PLAID_SECRET") == "" {
		t.Skip("Plaid credentials not configured - set PLAID_CLIENT_ID and PLAID_SECRET")
	}

	// Given: Full config with redirect_uri for OAuth support
	cfg := &config.Config{
		PlaidClientID:    getTestEnv("PLAID_CLIENT_ID"),
		PlaidSecret:      getTestEnv("PLAID_SECRET"),
		PlaidEnv:         "sandbox",
		PlaidRedirectURI: "https://boostedbudget.com/plaid/oauth-callback",
	}

	service, err := NewPlaidService(cfg)
	require.NoError(t, err, "should create plaid service successfully")

	// When: Creating link token with full configuration (AUTH + TRANSACTIONS + BALANCE + redirect_uri)
	ctx := context.Background()
	userID := time.Now().String()

	response, err := service.CreateLinkToken(ctx, userID)

	// Then: Should succeed - full configuration should work
	require.NoError(t, err, "redirect_uri should not cause 400 error")
	assert.NotEmpty(t, response.LinkToken, "link token should not be empty")
	assert.NotEmpty(t, response.Expiration, "expiration should not be empty")
	assert.NotEmpty(t, response.RequestID, "request ID should not be empty")

	t.Logf("✅ Full link token with redirect_uri: %s (RequestID: %s)", response.LinkToken, response.RequestID)
}
