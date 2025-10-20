// Package services provides business logic and external service integrations.
// This file implements PlaidService for secure Plaid API communication.
package services

import (
	"errors"
	"fmt"

	"github.com/maxnussbaum/BoostedBudget/internal/config"
	"github.com/plaid/plaid-go/v23/plaid"
)

var (
	// ErrPlaidNotConfigured is returned when required Plaid credentials (client ID or secret) are missing.
	// Both PLAID_CLIENT_ID and PLAID_SECRET environment variables must be set.
	ErrPlaidNotConfigured = errors.New("plaid credentials not configured")

	// ErrInvalidPlaidEnv is returned when the PLAID_ENV value is not recognized.
	// Valid values: "sandbox" (testing), "development" (development), "production" (live).
	ErrInvalidPlaidEnv = errors.New("invalid plaid environment")
)

// PlaidService defines the interface for Plaid API interactions.
// It provides access to the configured Plaid API client for making requests to Plaid's banking APIs.
//
// Example usage:
//
//	service, err := NewPlaidService(cfg)
//	if err != nil {
//	    return err
//	}
//	client := service.GetClient()
//	// Use client for Plaid API calls (link tokens, accounts, transactions, etc.)
type PlaidService interface {
	// GetClient returns the initialized Plaid API client for making authenticated requests.
	// The client is pre-configured with credentials and environment settings.
	GetClient() *plaid.APIClient
}

// plaidService implements PlaidService with a configured Plaid API client
type plaidService struct {
	client *plaid.APIClient
	config *config.Config
}

// NewPlaidService creates a new PlaidService with validated configuration.
//
// Validates that required credentials are present and maps the environment string
// to the appropriate Plaid environment constant. Returns an error if:
//   - PlaidClientID is empty (ErrPlaidNotConfigured)
//   - PlaidSecret is empty (ErrPlaidNotConfigured)
//   - PlaidEnv is not "sandbox", "development", or "production" (ErrInvalidPlaidEnv)
//
// On success, returns a fully initialized PlaidService ready for API calls.
func NewPlaidService(cfg *config.Config) (PlaidService, error) {
	// Validate required credentials
	if cfg.PlaidClientID == "" {
		return nil, fmt.Errorf("%w: PLAID_CLIENT_ID is required", ErrPlaidNotConfigured)
	}
	if cfg.PlaidSecret == "" {
		return nil, fmt.Errorf("%w: PLAID_SECRET is required", ErrPlaidNotConfigured)
	}

	// Map environment string to Plaid environment constant
	plaidEnv, err := mapEnvironment(cfg.PlaidEnv)
	if err != nil {
		return nil, err
	}

	// Create Plaid configuration
	configuration := plaid.NewConfiguration()
	configuration.AddDefaultHeader("PLAID-CLIENT-ID", cfg.PlaidClientID)
	configuration.AddDefaultHeader("PLAID-SECRET", cfg.PlaidSecret)
	configuration.UseEnvironment(plaidEnv)

	// Initialize Plaid API client
	client := plaid.NewAPIClient(configuration)

	return &plaidService{
		client: client,
		config: cfg,
	}, nil
}

// GetClient returns the initialized Plaid API client.
// The returned client is ready to make authenticated API calls to Plaid.
func (s *plaidService) GetClient() *plaid.APIClient {
	return s.client
}

// mapEnvironment maps environment string to Plaid environment constant.
//
// Supported environments (case-sensitive):
//   - "sandbox": Plaid Sandbox environment for testing without real bank data
//   - "development": Plaid Development environment for integration testing with limited real data
//   - "production": Plaid Production environment for live banking data
//
// Returns ErrInvalidPlaidEnv if the environment string is not recognized.
func mapEnvironment(env string) (plaid.Environment, error) {
	switch env {
	case "sandbox":
		return plaid.Sandbox, nil
	case "development":
		return plaid.Development, nil
	case "production":
		return plaid.Production, nil
	default:
		return "", fmt.Errorf("%w: must be 'sandbox', 'development', or 'production', got '%s'", ErrInvalidPlaidEnv, env)
	}
}
