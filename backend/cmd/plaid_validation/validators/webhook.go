package validators

import (
	"context"
	"os"
	"strings"
	"time"
)

// WebhookFireValidator tests webhook firing in sandbox mode.
type WebhookFireValidator struct{}

func init() {
	Register(&WebhookFireValidator{})
}

// Name returns the validator identifier.
func (v *WebhookFireValidator) Name() string {
	return "webhook-fire"
}

// Description returns a human-readable description.
func (v *WebhookFireValidator) Description() string {
	return "Fire test webhook (sandbox only)"
}

// Validate fires a test webhook in sandbox mode.
func (v *WebhookFireValidator) Validate(ctx context.Context, opts *ValidationOptions) *ValidationResult {
	start := time.Now()
	result := &ValidationResult{
		Name:    v.Name(),
		Details: make(map[string]interface{}),
	}

	// Check if we're in sandbox mode
	if opts.Config == nil {
		result.Status = StatusFail
		result.Message = "Configuration not provided"
		result.Duration = time.Since(start)
		return result
	}

	env := strings.ToLower(opts.Config.PlaidEnv)
	if env != "sandbox" {
		result.Status = StatusSkip
		result.Message = "Webhook fire only available in sandbox mode"
		result.Details["current_env"] = opts.Config.PlaidEnv
		result.Duration = time.Since(start)
		return result
	}

	result.Details["plaid_env"] = env

	// Get test access token
	accessToken := os.Getenv("PLAID_TEST_ACCESS_TOKEN")
	if accessToken == "" {
		result.Status = StatusSkip
		result.Message = "PLAID_TEST_ACCESS_TOKEN not set - cannot fire webhook"
		result.Duration = time.Since(start)
		return result
	}

	if opts.PlaidService == nil {
		result.Status = StatusFail
		result.Message = "PlaidService not initialized"
		result.Duration = time.Since(start)
		return result
	}

	// Fire a SYNC_UPDATES_AVAILABLE webhook
	webhookCode := "SYNC_UPDATES_AVAILABLE"
	resp, err := opts.PlaidService.FireWebhook(ctx, accessToken, webhookCode)
	if err != nil {
		result.Status = StatusFail
		result.Message = "Failed to fire webhook: " + err.Error()
		result.Error = err
		result.Duration = time.Since(start)
		return result
	}

	result.Details["webhook_code"] = webhookCode
	result.Details["webhook_fired"] = resp.GetWebhookFired()

	if !resp.GetWebhookFired() {
		result.Status = StatusWarn
		result.Message = "Webhook fire returned false"
		result.Duration = time.Since(start)
		return result
	}

	result.Status = StatusPass
	result.Message = "Webhook fired successfully"
	result.Duration = time.Since(start)
	return result
}
