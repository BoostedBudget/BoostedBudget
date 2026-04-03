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

	// CreateUpdateModeLinkToken generates a Plaid Link token in update mode for re-authentication.
	// This is used when a user needs to re-authenticate their bank connection due to:
	//   - LOGIN_REQUIRED: Credentials changed, MFA required, or session expired
	//   - PENDING_EXPIRATION: Item access expiring soon
	//   - USER_PERMISSION_REVOKED: User revoked access at their bank
	//
	// The link token allows the user to re-authenticate through Plaid Link without creating
	// a new Item. The existing Item and accounts remain intact.
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeout
	//   - userID: The user's ID (Clerk ID)
	//   - accessToken: The Plaid access token for the Item to update (must be decrypted)
	//
	// Returns the link token response containing the link token for update mode.
	// Returns error if validation fails or the Plaid API call fails.
	//
	// Issue #257: Item Error Handler - Process ITEM Webhooks and User Notifications
	CreateUpdateModeLinkToken(ctx context.Context, userID string, accessToken string) (*domain.LinkTokenResponse, error)

	// RemoveItem invalidates a Plaid Item's access token and removes it from Plaid.
	// This stops monthly billing for the Item and prevents further API calls with the access token.
	// Should be called when a user disconnects an institution from BoostedBudget.
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeout
	//   - accessToken: The Plaid access token for the Item to remove
	//
	// Returns error if the Plaid API call fails, nil on success.
	// Note: If this fails, the Item should still be deleted from the database as the
	// access token is likely already invalid.
	RemoveItem(ctx context.Context, accessToken string) error

	// TransactionsSync calls Plaid /transactions/sync API for a single page of transactions.
	// This is the preferred method for fetching transaction data (cursor-based pagination).
	//
	// The caller is responsible for pagination loop (checking HasMore and calling again with cursor).
	// TransactionSyncService handles the pagination logic and cursor updates.
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeout
	//   - accessToken: The Plaid access token (decrypted) for the Item
	//   - cursor: Cursor for pagination (empty string for first sync)
	//   - daysRequested: Historical depth for initial sync (30, 365, 730 days) - only used when cursor is empty
	//
	// Returns the Plaid API response containing Added, Modified, Removed transactions,
	// NextCursor, and HasMore flag. Returns error if the Plaid API call fails.
	//
	// Issue #260: Plaid TransactionsSync API Integration
	// Issue #242: Monthly sync limits with tier-based historical depth
	TransactionsSync(
		ctx context.Context,
		accessToken string,
		cursor string,
		daysRequested int,
	) (*plaid.TransactionsSyncResponse, error)

	// FireWebhook fires a Plaid webhook for testing in sandbox environment.
	// Uses the /sandbox/item/fire_webhook API endpoint to manually trigger webhook events.
	//
	// IMPORTANT: This method is ONLY available in sandbox environment. Calling this method
	// when PlaidEnv is "production" or "development" will return an error.
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeout
	//   - accessToken: The Plaid access token (must be decrypted)
	//   - webhookCode: Webhook code to fire (e.g., "DEFAULT_UPDATE", "INITIAL_UPDATE")
	//
	// Valid webhook codes:
	//   - INITIAL_UPDATE: Triggers initial transaction sync
	//   - DEFAULT_UPDATE: Triggers incremental transaction sync
	//   - HISTORICAL_UPDATE: Triggers historical transaction sync
	//   - TRANSACTIONS_REMOVED: Simulates transaction removal
	//
	// Returns the Plaid API response with webhook_fired=true on success.
	// Returns error if PlaidEnv is not "sandbox" or if the Plaid API call fails.
	//
	// Issue #308: Admin API for Manual Webhook Firing in Staging
	FireWebhook(
		ctx context.Context,
		accessToken string,
		webhookCode string,
	) (*plaid.SandboxItemFireWebhookResponse, error)

	// AccountsGet fetches accounts and their balances for a linked item.
	// Uses the Plaid /accounts/get endpoint to retrieve current account data.
	//
	// This method is used after transaction sync to capture balance snapshots
	// and to verify account information. Balance data retrieval is FREE
	// (included with TRANSACTIONS product).
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeout
	//   - accessToken: The Plaid access token (must be decrypted)
	//
	// Returns account information including current and available balances.
	// Returns error if the Plaid API call fails or the access token is invalid.
	//
	// Plaid Staging Validation - AccountsGet abstraction for testability
	AccountsGet(ctx context.Context, accessToken string) (*plaid.AccountsGetResponse, error)

	// InvestmentsHoldingsGet fetches current investment holdings and securities.
	// Returns a full snapshot (no cursor/delta) — callers should replace, not merge.
	// Issue #659: Plaid Investments
	InvestmentsHoldingsGet(ctx context.Context, accessToken string) (*plaid.InvestmentsHoldingsGetResponse, error)

	// InvestmentsTransactionsGet fetches investment transactions within a date range.
	// Uses offset/count pagination (not cursor-based). Caller handles the pagination loop.
	// Issue #659: Plaid Investments
	InvestmentsTransactionsGet(ctx context.Context, accessToken, startDate, endDate string, offset, count int32) (*plaid.InvestmentsTransactionsGetResponse, error)

	// LiabilitiesGet fetches current liability data for all accounts in a linked item.
	// Returns a full snapshot (no cursor/delta) of credit, mortgage, and student loan liabilities.
	// Callers should replace existing data, not merge.
	// Issue #910: Plaid Liabilities
	LiabilitiesGet(ctx context.Context, accessToken string) (*plaid.LiabilitiesGetResponse, error)
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
// to initialize Plaid Link for connecting bank accounts. Configured with:
//   - User identification (client_user_id from Clerk)
//   - Products: TRANSACTIONS only (account data + transaction history)
//   - OAuth redirect URI for institutions like Chase, Capital One, Wells Fargo
//   - Country codes (US) and language (English)
//
// Note: Balance data is retrieved automatically via /accounts/get endpoint (FREE).
// BALANCE is not a valid Link initialization product per Plaid docs and should not be included.
//
// Issue #201 implementation: Initially AUTH + TRANSACTIONS products + OAuth redirect_uri support.
// Issue #232 fix: Removed PRODUCTS_BALANCE (causes 400 Bad Request from Plaid API).
// Issue #237 fix: Removed PRODUCTS_AUTH (causes NO_AUTH_ACCOUNTS error for credit cards/CDs).
//
// Why AUTH was removed:
// - AUTH only supports checking/savings/cash management (NOT credit cards or CDs)
// - BoostedBudget has "no money movement" - doesn't need account/routing numbers
// - Current code only uses /accounts/get (FREE, doesn't require AUTH)
// - TRANSACTIONS product supports: credit cards, all depository types (checking, savings, CDs,
//   money market), and student loans - sufficient for budgeting app needs
//
// The redirect URI must be configured via PLAID_REDIRECT_URI environment variable and
// registered in Plaid Dashboard (Team Settings → API → Allowed redirect URIs).
//
// Returns an error if the Plaid API call fails or if redirect URI is not configured.
func (s *plaidService) CreateLinkToken(ctx context.Context, userID string) (*domain.LinkTokenResponse, error) {
	// Validate redirect URI is configured (required for OAuth institutions)
	// Phase 3 of issue #201 - OAuth institutions like Chase, Capital One require redirect_uri
	if s.config.PlaidRedirectURI == "" {
		return nil, fmt.Errorf("plaid redirect URI not configured (PLAID_REDIRECT_URI required)")
	}

	// Build the link token request with required parameters
	request := plaid.NewLinkTokenCreateRequest(
		"BoostedBudget",                              // Client name displayed in Plaid Link
		"en",                                          // Language
		[]plaid.CountryCode{plaid.COUNTRYCODE_US},    // Country codes
		*plaid.NewLinkTokenCreateRequestUser(userID), // User identifier (dereferenced from pointer)
	)

	// Set products: Phase 2 (#241) - TRANSACTIONS + LIABILITIES + INVESTMENTS
	// Issue #237: AUTH removed (only supports checking/savings, caused NO_AUTH_ACCOUNTS for credit cards/CDs)
	// Issue #241: LIABILITIES and INVESTMENTS added (full processing deferred to follow-up issues)
	// Balance data is automatically included in /accounts/get response (FREE)
	request.SetProducts([]plaid.Products{
		plaid.PRODUCTS_TRANSACTIONS,
		plaid.PRODUCTS_LIABILITIES,
		plaid.PRODUCTS_INVESTMENTS,
	})

	// Set redirect URI for OAuth institution support (Chase, Capital One, Wells Fargo, etc.)
	// Phase 3 of issue #201 - OAuth flow requires redirect_uri
	// Must be registered in Plaid Dashboard: Team Settings → API → Allowed redirect URIs
	request.SetRedirectUri(s.config.PlaidRedirectURI)

	// Issue #202: Set webhook URL if configured
	// Webhook URL enables real-time notifications from Plaid for:
	//   - New transactions available (TRANSACTIONS webhook)
	//   - Item errors requiring user re-authentication (ITEM webhook)
	//   - Account updates and institution changes
	// Must be publicly accessible HTTPS endpoint registered in Plaid Dashboard
	// If not configured, webhooks are disabled (polling-based sync required)
	if s.config.PlaidWebhookURL != "" {
		request.SetWebhook(s.config.PlaidWebhookURL)
	}

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

// CreateUpdateModeLinkToken creates a Plaid Link token in update mode for re-authentication.
//
// Update mode is used when a user needs to re-authenticate their existing bank connection
// due to credential changes, MFA requirements, session expiration, or access revocation.
// Unlike CreateLinkToken (which creates a new Item), update mode maintains the existing
// Item and accounts while allowing the user to re-authorize.
//
// When to use:
//   - Plaid webhook returns ITEM_LOGIN_REQUIRED (credentials changed)
//   - Plaid webhook returns PENDING_EXPIRATION (access expiring soon)
//   - Plaid webhook returns USER_PERMISSION_REVOKED (user revoked access at bank)
//
// The frontend should initialize Plaid Link with the returned link token. After successful
// re-authentication, the existing Item's access token remains valid and can continue to be
// used for API calls.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control
//   - userID: The user's ID (Clerk ID) for identifying the user in Plaid
//   - accessToken: The Plaid access token for the Item (must be decrypted)
//
// Returns the link token response for update mode, or error if validation fails or Plaid API fails.
//
// Issue #257: Item Error Handler - Process ITEM Webhooks and User Notifications
func (s *plaidService) CreateUpdateModeLinkToken(ctx context.Context, userID string, accessToken string) (*domain.LinkTokenResponse, error) {
	// Validate required parameters
	if userID == "" {
		return nil, fmt.Errorf("user ID cannot be empty")
	}
	if accessToken == "" {
		return nil, fmt.Errorf("access token cannot be empty")
	}

	// Validate required config before building request
	if s.config.PlaidRedirectURI == "" {
		return nil, fmt.Errorf("PLAID_REDIRECT_URI is required but not configured")
	}

	// Create Plaid API request
	request := plaid.NewLinkTokenCreateRequest(
		"BoostedBudget",       // client_name: Displayed in Plaid Link
		"en",                  // language: English
		[]plaid.CountryCode{plaid.COUNTRYCODE_US}, // country_codes: USA only (MVP)
		*plaid.NewLinkTokenCreateRequestUser(userID), // user: Clerk user ID
	)

	// CRITICAL: Set access token to enable update mode
	// This tells Plaid to create a link token for re-authenticating the EXISTING Item
	// rather than creating a new Item. The accessToken must be decrypted.
	//
	// Common mistake: Using item_id instead of access_token (SetAccessToken needs access_token!)
	request.SetAccessToken(accessToken)

	// Set products: same 3 as CreateLinkToken (TRANSACTIONS + LIABILITIES + INVESTMENTS, Phase 2 #241)
	request.SetProducts([]plaid.Products{
		plaid.PRODUCTS_TRANSACTIONS,
		plaid.PRODUCTS_LIABILITIES,
		plaid.PRODUCTS_INVESTMENTS,
	})

	// Set redirect URI for OAuth institution support
	// Required for OAuth-based institutions (Chase, Capital One, etc.)
	request.SetRedirectUri(s.config.PlaidRedirectURI)

	// Set webhook URL if configured (same as CreateLinkToken)
	// Enables real-time notifications after re-authentication succeeds
	if s.config.PlaidWebhookURL != "" {
		request.SetWebhook(s.config.PlaidWebhookURL)
	}

	// Call Plaid API to create update mode link token
	resp, _, err := s.client.PlaidApi.LinkTokenCreate(ctx).LinkTokenCreateRequest(*request).Execute()
	if err != nil {
		return nil, fmt.Errorf("failed to create update mode link token: %w", err)
	}

	// Build response
	return &domain.LinkTokenResponse{
		LinkToken:  resp.GetLinkToken(),
		Expiration: resp.GetExpiration(),
		RequestID:  resp.GetRequestId(),
	}, nil
}

// RemoveItem removes a Plaid Item by invalidating its access token.
//
// This method calls the Plaid /item/remove API endpoint to permanently invalidate
// the access token for an Item. Once removed:
//   - The access token can no longer be used for any Plaid API calls
//   - Monthly billing for this Item stops (cost control - Issue #226)
//   - All accounts associated with this Item are disconnected
//
// This method should be called when a user disconnects an institution from BoostedBudget.
// Even if this call fails (e.g., token already invalid), the Item should still be deleted
// from the database since the access token cannot be used anyway.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control
//   - accessToken: The Plaid access token for the Item to remove
//
// Returns error if the Plaid API call fails, nil on success.
func (s *plaidService) RemoveItem(ctx context.Context, accessToken string) error {
	// Validate access token is not empty
	if accessToken == "" {
		return fmt.Errorf("access token cannot be empty")
	}

	// Build the item remove request
	request := plaid.NewItemRemoveRequest(accessToken)

	// Call Plaid API to remove the item
	_, _, err := s.client.PlaidApi.ItemRemove(ctx).ItemRemoveRequest(*request).Execute()
	if err != nil {
		return fmt.Errorf("failed to remove item: %w", err)
	}

	return nil
}

// TransactionsSync calls Plaid /transactions/sync API for a single page.
//
// This is a thin wrapper around the Plaid API client that handles request construction
// and response extraction. The caller (TransactionSyncService) is responsible for:
//   - Decrypting the access token
//   - Pagination loop (calling until has_more=false)
//   - Updating cursor after each successful page
//   - Error classification and retry strategy
//
// Parameters:
//   - ctx: Context for cancellation and timeout control
//   - accessToken: The Plaid access token (already decrypted)
//   - cursor: Cursor from previous sync (empty string for first sync)
//
// Returns the Plaid API response or error.
//
// Issue #260: Plaid TransactionsSync API Integration - thin PlaidService wrapper
func (s *plaidService) TransactionsSync(
	ctx context.Context,
	accessToken string,
	cursor string,
	daysRequested int,
) (*plaid.TransactionsSyncResponse, error) {
	// Validate access token
	if accessToken == "" {
		return nil, fmt.Errorf("access token cannot be empty")
	}

	// Build TransactionsSync request
	request := plaid.NewTransactionsSyncRequest(accessToken)

	// Set cursor if provided (empty for first sync)
	if cursor != "" {
		request.SetCursor(cursor)
	}

	// Set historical depth for initial sync (Issue #242: tier-based limits)
	// Only used when cursor is empty (first sync). Subsequent syncs use cursor.
	// Plaid supports 30-730 days historical depth.
	if cursor == "" && daysRequested > 0 {
		options := plaid.NewTransactionsSyncRequestOptions()
		days := int32(daysRequested)
		options.SetDaysRequested(days)
		request.SetOptions(*options)
	}

	// Call Plaid API
	resp, _, err := s.client.PlaidApi.TransactionsSync(ctx).
		TransactionsSyncRequest(*request).
		Execute()

	if err != nil {
		return nil, fmt.Errorf("plaid TransactionsSync failed: %w", err)
	}

	return &resp, nil
}

// FireWebhook fires a Plaid webhook for manual testing in sandbox environment.
//
// This method wraps the Plaid /sandbox/item/fire_webhook API endpoint, which allows
// triggering webhook events on-demand for testing purposes. This is critical for
// staging environments where webhooks cannot be delivered due to network restrictions
// (e.g., Tailscale private network).
//
// Security: Triple-layer protection ensures this only works in sandbox:
//   1. Route registration: Only registered when Environment=="staging" (main.go)
//   2. AdminMiddleware: Returns 404 in non-staging environments
//   3. This method: Returns error if PlaidEnv != "sandbox"
//
// Parameters:
//   - ctx: Context for cancellation and timeout
//   - accessToken: Plaid access token (decrypted)
//   - webhookCode: Webhook code to fire (INITIAL_UPDATE, DEFAULT_UPDATE, etc.)
//
// Returns the Plaid API response or error.
//
// Issue #308: Admin API for Manual Webhook Firing in Staging
func (s *plaidService) FireWebhook(
	ctx context.Context,
	accessToken string,
	webhookCode string,
) (*plaid.SandboxItemFireWebhookResponse, error) {
	// Validate access token
	if accessToken == "" {
		return nil, fmt.Errorf("access token cannot be empty")
	}

	// Validate webhook code
	if webhookCode == "" {
		return nil, fmt.Errorf("webhook code cannot be empty")
	}

	// Sandbox-only validation (defense in depth - third layer of security)
	// Layer 1: Route registration (main.go) only in staging environment
	// Layer 2: AdminMiddleware returns 404 in production
	// Layer 3: This check (prevents accidental misconfiguration)
	if s.config.PlaidEnv != "sandbox" {
		return nil, fmt.Errorf(
			"FireWebhook is only available in sandbox environment, current environment: %s",
			s.config.PlaidEnv,
		)
	}

	// Build the sandbox fire webhook request
	request := plaid.NewSandboxItemFireWebhookRequest(
		accessToken,
		webhookCode,
	)

	// Call Plaid API
	resp, _, err := s.client.PlaidApi.SandboxItemFireWebhook(ctx).
		SandboxItemFireWebhookRequest(*request).
		Execute()

	if err != nil {
		return nil, fmt.Errorf("plaid SandboxItemFireWebhook failed: %w", err)
	}

	return &resp, nil
}

// AccountsGet fetches accounts and their balances for a linked item.
//
// Uses the Plaid /accounts/get API endpoint to retrieve current account data
// including current and available balances. This is included FREE with the
// TRANSACTIONS product.
//
// Parameters:
//   - ctx: Context for cancellation and timeout
//   - accessToken: The Plaid access token (must be decrypted)
//
// Returns account information with current balances, or error if API call fails.
func (s *plaidService) AccountsGet(ctx context.Context, accessToken string) (*plaid.AccountsGetResponse, error) {
	// Build request
	request := plaid.NewAccountsGetRequest(accessToken)

	// Call Plaid API
	resp, _, err := s.client.PlaidApi.AccountsGet(ctx).
		AccountsGetRequest(*request).
		Execute()

	if err != nil {
		return nil, fmt.Errorf("plaid AccountsGet failed: %w", err)
	}

	return &resp, nil
}

// InvestmentsHoldingsGet calls Plaid /investments/holdings/get.
// Returns all current holdings and associated securities for the item.
// Issue #659: Plaid Investments
func (s *plaidService) InvestmentsHoldingsGet(ctx context.Context, accessToken string) (*plaid.InvestmentsHoldingsGetResponse, error) {
	if accessToken == "" {
		return nil, fmt.Errorf("access token cannot be empty")
	}

	request := plaid.NewInvestmentsHoldingsGetRequest(accessToken)
	resp, _, err := s.client.PlaidApi.InvestmentsHoldingsGet(ctx).
		InvestmentsHoldingsGetRequest(*request).
		Execute()
	if err != nil {
		return nil, fmt.Errorf("plaid InvestmentsHoldingsGet failed: %w", err)
	}
	return &resp, nil
}

// InvestmentsTransactionsGet calls Plaid /investments/transactions/get.
// Fetches investment transactions within a date range using offset/count pagination.
// Issue #659: Plaid Investments
func (s *plaidService) InvestmentsTransactionsGet(ctx context.Context, accessToken, startDate, endDate string, offset, count int32) (*plaid.InvestmentsTransactionsGetResponse, error) {
	if accessToken == "" {
		return nil, fmt.Errorf("access token cannot be empty")
	}
	if startDate == "" {
		return nil, fmt.Errorf("start_date is required")
	}
	if endDate == "" {
		return nil, fmt.Errorf("end_date is required")
	}

	request := plaid.NewInvestmentsTransactionsGetRequest(accessToken, startDate, endDate)

	if offset > 0 || count > 0 {
		options := plaid.NewInvestmentsTransactionsGetRequestOptions()
		if offset > 0 {
			options.SetOffset(offset)
		}
		if count > 0 {
			options.SetCount(count)
		}
		request.SetOptions(*options)
	}

	resp, _, err := s.client.PlaidApi.InvestmentsTransactionsGet(ctx).
		InvestmentsTransactionsGetRequest(*request).
		Execute()
	if err != nil {
		return nil, fmt.Errorf("plaid InvestmentsTransactionsGet failed: %w", err)
	}
	return &resp, nil
}

// LiabilitiesGet calls Plaid /liabilities/get.
// Returns full liability snapshot: credit card, mortgage, and student loan data.
// Issue #910: Plaid Liabilities
func (s *plaidService) LiabilitiesGet(ctx context.Context, accessToken string) (*plaid.LiabilitiesGetResponse, error) {
	if accessToken == "" {
		return nil, fmt.Errorf("access token cannot be empty")
	}
	request := plaid.NewLiabilitiesGetRequest(accessToken)
	resp, _, err := s.client.PlaidApi.LiabilitiesGet(ctx).
		LiabilitiesGetRequest(*request).
		Execute()
	if err != nil {
		return nil, fmt.Errorf("plaid LiabilitiesGet failed: %w", err)
	}
	return &resp, nil
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
