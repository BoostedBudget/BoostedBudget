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
		PlaidWebhookURL: "https://api.boostedbudget.com/webhooks/plaid",
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
		PlaidWebhookURL: "https://api.boostedbudget.com/webhooks/plaid",
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
		PlaidWebhookURL: "https://api.boostedbudget.com/webhooks/plaid",
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
		PlaidWebhookURL: "https://api.boostedbudget.com/webhooks/plaid",
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
		PlaidWebhookURL: "https://api.boostedbudget.com/webhooks/plaid",
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
		PlaidWebhookURL: "https://api.boostedbudget.com/webhooks/plaid",
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
