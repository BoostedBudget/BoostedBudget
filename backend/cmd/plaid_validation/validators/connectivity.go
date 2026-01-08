package validators

import (
	"context"
	"time"
)

// ConnectivityValidator verifies Plaid API authentication and configuration.
type ConnectivityValidator struct{}

func init() {
	Register(&ConnectivityValidator{})
}

// Name returns the validator identifier.
func (v *ConnectivityValidator) Name() string {
	return "connectivity"
}

// Description returns a human-readable description.
func (v *ConnectivityValidator) Description() string {
	return "Verify Plaid API authentication and configuration"
}

// Validate checks Plaid API connectivity by creating a test link token.
func (v *ConnectivityValidator) Validate(ctx context.Context, opts *ValidationOptions) *ValidationResult {
	start := time.Now()
	result := &ValidationResult{
		Name:    v.Name(),
		Details: make(map[string]interface{}),
	}

	// Check configuration
	if opts.Config == nil {
		result.Status = StatusFail
		result.Message = "Configuration not provided"
		result.Duration = time.Since(start)
		return result
	}

	if opts.Config.PlaidClientID == "" {
		result.Status = StatusFail
		result.Message = "PLAID_CLIENT_ID not configured"
		result.Duration = time.Since(start)
		return result
	}

	if opts.Config.PlaidSecret == "" {
		result.Status = StatusFail
		result.Message = "PLAID_SECRET not configured"
		result.Duration = time.Since(start)
		return result
	}

	// Record environment details
	result.Details["plaid_env"] = opts.Config.PlaidEnv
	result.Details["webhook_url_configured"] = opts.Config.PlaidWebhookURL != ""
	result.Details["redirect_uri_configured"] = opts.Config.PlaidRedirectURI != ""

	// Check PlaidService
	if opts.PlaidService == nil {
		result.Status = StatusFail
		result.Message = "PlaidService not initialized"
		result.Duration = time.Since(start)
		return result
	}

	// Try creating a link token to verify API authentication
	linkToken, err := opts.PlaidService.CreateLinkToken(ctx, "validation-test-user")
	if err != nil {
		result.Status = StatusFail
		result.Message = "Plaid API authentication failed: " + err.Error()
		result.Error = err
		result.Duration = time.Since(start)
		return result
	}

	// Success
	result.Status = StatusPass
	result.Message = "Plaid API connection verified"
	result.Details["link_token_created"] = linkToken.LinkToken != ""
	result.Details["request_id"] = linkToken.RequestID
	result.Duration = time.Since(start)

	return result
}
