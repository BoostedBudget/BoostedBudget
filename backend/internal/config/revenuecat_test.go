package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

var revenueCatEnvKeys = []string{
	"REVENUECAT_PROJECT_ID", "REVENUECAT_SECRET_API_KEY", "REVENUECAT_GRANT_API_KEY",
	"REVENUECAT_ENVIRONMENT", "REVENUECAT_WEBHOOK_AUTHORIZATION", "REVENUECAT_WEBHOOK_SIGNING_SECRET",
}

func setRevenueCatEnv(t *testing.T, environment string) {
	t.Helper()
	for _, key := range revenueCatEnvKeys {
		t.Setenv(key, "test-private-value")
	}
	t.Setenv("REVENUECAT_ENVIRONMENT", environment)
}

func TestRevenueCatConfigurationRequiredForDeployments(t *testing.T) {
	for _, environment := range []string{"staging", "production"} {
		for _, missing := range revenueCatEnvKeys {
			t.Run(environment+"/"+missing, func(t *testing.T) {
				setProdRequiredEnv(t)
				t.Setenv("ENVIRONMENT", environment)
				t.Setenv("RATE_LIMIT_REDIS_URL", "redis://localhost:6379")
				rcEnvironment := "sandbox"
				if environment == "production" {
					rcEnvironment = "production"
				}
				setRevenueCatEnv(t, rcEnvironment)
				t.Setenv(missing, "")
				_, err := Load()
				require.ErrorContains(t, err, missing)
				require.NotContains(t, err.Error(), "test-private-value")
			})
		}
	}
}

func TestRevenueCatConfigurationRejectsPartialLocalSetup(t *testing.T) {
	t.Setenv("ENVIRONMENT", "development")
	for _, key := range revenueCatEnvKeys {
		t.Setenv(key, "")
	}
	_, err := Load()
	require.NoError(t, err)
	t.Setenv("REVENUECAT_PROJECT_ID", "test-project")
	_, err = Load()
	require.ErrorContains(t, err, "REVENUECAT_SECRET_API_KEY")
}

func TestRevenueCatConfigurationRejectsEnvironmentMismatch(t *testing.T) {
	for _, tc := range []struct{ deployment, provider string }{
		{"production", "sandbox"}, {"staging", "production"},
		{"development", "production"}, {"qa", "production"}, {"development", "invalid"},
	} {
		t.Run(tc.deployment+"/"+tc.provider, func(t *testing.T) {
			setProdRequiredEnv(t)
			t.Setenv("ENVIRONMENT", tc.deployment)
			t.Setenv("RATE_LIMIT_REDIS_URL", "redis://localhost:6379")
			setRevenueCatEnv(t, tc.provider)
			_, err := Load()
			require.ErrorContains(t, err, "REVENUECAT_ENVIRONMENT")
		})
	}
}

func TestRevenueCatConfigurationLoadsCompleteSetup(t *testing.T) {
	t.Setenv("ENVIRONMENT", "development")
	setRevenueCatEnv(t, "sandbox")
	cfg, err := Load()
	require.NoError(t, err)
	require.Equal(t, "test-private-value", cfg.RevenueCatProjectID)
	require.Equal(t, "test-private-value", cfg.RevenueCatSecretAPIKey)
	require.Equal(t, "test-private-value", cfg.RevenueCatGrantAPIKey)
	require.Equal(t, "sandbox", cfg.RevenueCatEnvironment)
	require.Equal(t, "test-private-value", cfg.RevenueCatWebhookAuthorization)
	require.Equal(t, "test-private-value", cfg.RevenueCatWebhookSigningSecret)
}
