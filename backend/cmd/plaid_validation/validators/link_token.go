package validators

import (
	"context"
	"time"
)

// LinkTokenValidator tests link token creation.
type LinkTokenValidator struct{}

func init() {
	Register(&LinkTokenValidator{})
}

// Name returns the validator identifier.
func (v *LinkTokenValidator) Name() string {
	return "link-token"
}

// Description returns a human-readable description.
func (v *LinkTokenValidator) Description() string {
	return "Test link token creation flow"
}

// Validate tests link token creation.
func (v *LinkTokenValidator) Validate(ctx context.Context, opts *ValidationOptions) *ValidationResult {
	start := time.Now()
	result := &ValidationResult{
		Name:    v.Name(),
		Details: make(map[string]interface{}),
	}

	if opts.PlaidService == nil {
		result.Status = StatusFail
		result.Message = "PlaidService not initialized"
		result.Duration = time.Since(start)
		return result
	}

	// Create a link token for a test user
	testUserID := "validation-link-token-test"
	linkToken, err := opts.PlaidService.CreateLinkToken(ctx, testUserID)
	if err != nil {
		result.Status = StatusFail
		result.Message = "Link token creation failed: " + err.Error()
		result.Error = err
		result.Duration = time.Since(start)
		return result
	}

	// Verify response
	if linkToken.LinkToken == "" {
		result.Status = StatusFail
		result.Message = "Empty link token returned"
		result.Duration = time.Since(start)
		return result
	}

	if linkToken.Expiration.IsZero() {
		result.Status = StatusWarn
		result.Message = "Link token created but expiration not set"
		result.Details["link_token_length"] = len(linkToken.LinkToken)
		result.Details["request_id"] = linkToken.RequestID
		result.Duration = time.Since(start)
		return result
	}

	// Check expiration is in the future
	if linkToken.Expiration.Before(time.Now()) {
		result.Status = StatusFail
		result.Message = "Link token already expired"
		result.Details["expiration"] = linkToken.Expiration.Format(time.RFC3339)
		result.Duration = time.Since(start)
		return result
	}

	// Calculate time until expiration
	expiresIn := time.Until(linkToken.Expiration)

	result.Status = StatusPass
	result.Message = "Link token created successfully"
	result.Details["link_token_length"] = len(linkToken.LinkToken)
	result.Details["expires_in_minutes"] = int(expiresIn.Minutes())
	result.Details["request_id"] = linkToken.RequestID
	result.Duration = time.Since(start)

	return result
}
