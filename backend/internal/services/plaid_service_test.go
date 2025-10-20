package services

import (
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
