// Package services provides business logic and external service integrations.
// This file implements PlaidService for secure Plaid API communication.
package services

import (
	"context"
	"errors"
	"fmt"

	"github.com/maxnussbaum/BoostedBudget/internal/config"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
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

	// CreateLinkToken generates a Plaid Link token for initiating bank account connections.
	// The link token is used by the frontend to initialize Plaid Link for the specified user.
	CreateLinkToken(ctx context.Context, userID string) (*domain.LinkTokenResponse, error)
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

// CreateLinkToken generates a Plaid Link token for the specified user.
//
// The link token is a short-lived (30 minute) credential that enables the frontend
// to initialize Plaid Link for connecting bank accounts. Currently configured with:
//   - User identification (client_user_id from Auth0)
//   - Products: AUTH (account/routing numbers), TRANSACTIONS (transaction history)
//   - Country codes (US) and language (English)
//
// Phase 1 of issue #201: TRANSACTIONS product added for transaction history access.
// Future phases will add BALANCE product and OAuth redirect_uri support.
//
// Returns an error if the Plaid API call fails.
func (s *plaidService) CreateLinkToken(ctx context.Context, userID string) (*domain.LinkTokenResponse, error) {
	// Build the link token request with required parameters
	request := plaid.NewLinkTokenCreateRequest(
		"BoostedBudget",                              // Client name displayed in Plaid Link
		"en",                                          // Language
		[]plaid.CountryCode{plaid.COUNTRYCODE_US},    // Country codes
		*plaid.NewLinkTokenCreateRequestUser(userID), // User identifier (dereferenced from pointer)
	)

	// Set products: AUTH + TRANSACTIONS
	// Phase 1 of issue #201 - adding TRANSACTIONS product for transaction history access
	// AUTH: Account and routing numbers for ACH transfers
	// TRANSACTIONS: Historical transaction data (up to 24 months), categories, merchant info
	request.SetProducts([]plaid.Products{
		plaid.PRODUCTS_AUTH,
		plaid.PRODUCTS_TRANSACTIONS,
	})

	// REMOVED: Webhook URL validation and SetWebhook()
	// REMOVED: Redirect URI validation and SetRedirectUri()
	// These will be added back incrementally once minimal version works

	// Call Plaid API to create link token
	resp, _, err := s.client.PlaidApi.LinkTokenCreate(ctx).LinkTokenCreateRequest(*request).Execute()
	if err != nil {
		return nil, fmt.Errorf("failed to create link token: %w", err)
	}

	// Build response
	return &domain.LinkTokenResponse{
		LinkToken:  resp.GetLinkToken(),
		Expiration: resp.GetExpiration(),
		RequestID:  resp.GetRequestId(),
	}, nil
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
