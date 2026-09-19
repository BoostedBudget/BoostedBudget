package config

import (
	"errors"
	"fmt"
	"strings"
)

func (c *Config) validateRevenueCat() error {
	settings := []struct{ name, value string }{
		{"REVENUECAT_PROJECT_ID", c.RevenueCatProjectID},
		{"REVENUECAT_SECRET_API_KEY", c.RevenueCatSecretAPIKey},
		{"REVENUECAT_GRANT_API_KEY", c.RevenueCatGrantAPIKey},
		{"REVENUECAT_ENVIRONMENT", c.RevenueCatEnvironment},
		{"REVENUECAT_WEBHOOK_AUTHORIZATION", c.RevenueCatWebhookAuthorization},
		{"REVENUECAT_WEBHOOK_SIGNING_SECRET", c.RevenueCatWebhookSigningSecret},
	}
	required := c.Environment == "production" || c.Environment == "staging"
	for _, setting := range settings {
		required = required || setting.value != ""
	}
	if !required {
		return nil
	}
	var errs []error
	for _, setting := range settings {
		if strings.TrimSpace(setting.value) == "" {
			errs = append(errs, fmt.Errorf("%s is required for RevenueCat connectivity", setting.name))
		}
	}
	wantEnvironment := "sandbox"
	if c.Environment == "production" {
		wantEnvironment = "production"
	}
	if c.RevenueCatEnvironment != wantEnvironment {
		errs = append(errs, fmt.Errorf("REVENUECAT_ENVIRONMENT must be %s for this deployment", wantEnvironment))
	}
	return errors.Join(errs...)
}
