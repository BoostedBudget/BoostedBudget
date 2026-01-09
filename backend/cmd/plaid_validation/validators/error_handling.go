package validators

import (
	"context"
	"strings"
	"time"
)

// ErrorClassificationValidator tests error handling and classification.
type ErrorClassificationValidator struct{}

func init() {
	Register(&ErrorClassificationValidator{})
}

// Name returns the validator identifier.
func (v *ErrorClassificationValidator) Name() string {
	return "error-classification"
}

// Description returns a human-readable description.
func (v *ErrorClassificationValidator) Description() string {
	return "Verify error handling and classification"
}

// Validate tests error classification by triggering known error conditions.
func (v *ErrorClassificationValidator) Validate(ctx context.Context, opts *ValidationOptions) *ValidationResult {
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

	// Test 1: Invalid access token should return appropriate error
	_, err := opts.PlaidService.TransactionsSync(ctx, "invalid-access-token", "", 0)
	if err == nil {
		result.Status = StatusFail
		result.Message = "Expected error for invalid access token, got success"
		result.Duration = time.Since(start)
		return result
	}

	// Check that the error contains meaningful information
	errStr := err.Error()
	result.Details["invalid_token_error"] = errStr

	// Check for expected Plaid error patterns
	isPlaidError := strings.Contains(errStr, "INVALID_ACCESS_TOKEN") ||
		strings.Contains(errStr, "invalid_access_token") ||
		strings.Contains(errStr, "access_token") ||
		strings.Contains(errStr, "401") ||
		strings.Contains(errStr, "unauthorized")

	if !isPlaidError {
		result.Status = StatusWarn
		result.Message = "Error returned but may not be properly classified as Plaid error"
		result.Details["expected_patterns"] = "INVALID_ACCESS_TOKEN, 401, unauthorized"
		result.Duration = time.Since(start)
		return result
	}

	result.Details["error_properly_classified"] = true

	// Test 2: Invalid cursor should return appropriate error
	// Note: This requires a valid access token, so we skip if not available
	// The sync validators cover cursor handling

	result.Status = StatusPass
	result.Message = "Error classification verified for invalid access token"
	result.Duration = time.Since(start)
	return result
}
