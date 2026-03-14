package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad_ServerTimeoutDefaults(t *testing.T) {
	cfg, err := Load()
	require.NoError(t, err)

	assert.Equal(t, 30*time.Second, cfg.ReadTimeout, "ReadTimeout should default to 30s")
	assert.Equal(t, 30*time.Second, cfg.WriteTimeout, "WriteTimeout should default to 30s")
	assert.Equal(t, 120*time.Second, cfg.IdleTimeout, "IdleTimeout should default to 120s")
	assert.Equal(t, "1M", cfg.MaxBodySize, "MaxBodySize should default to 1M")
}

func TestLoad_ServerTimeoutsConfigurable(t *testing.T) {
	t.Setenv("SERVER_READ_TIMEOUT", "60s")
	t.Setenv("SERVER_WRITE_TIMEOUT", "45s")
	t.Setenv("SERVER_IDLE_TIMEOUT", "180s")
	t.Setenv("MAX_BODY_SIZE", "2M")

	cfg, err := Load()
	require.NoError(t, err)

	assert.Equal(t, 60*time.Second, cfg.ReadTimeout)
	assert.Equal(t, 45*time.Second, cfg.WriteTimeout)
	assert.Equal(t, 180*time.Second, cfg.IdleTimeout)
	assert.Equal(t, "2M", cfg.MaxBodySize)
}

func TestLoad_ProductionValidation_RequiresEncryptionKey(t *testing.T) {
	t.Setenv("ENVIRONMENT", "production")
	t.Setenv("DATABASE_URL", "postgres://user:pass@localhost/db")
	t.Setenv("CLERK_SECRET_KEY", "sk_live_test")
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://boostedbudget.com")
	// Intentionally NOT setting ENCRYPTION_KEY

	_, err := Load()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "ENCRYPTION_KEY")
}

func TestLoad_ProductionValidation_RejectsWildcardCORS(t *testing.T) {
	t.Setenv("ENVIRONMENT", "production")
	t.Setenv("DATABASE_URL", "postgres://user:pass@localhost/db")
	t.Setenv("CLERK_SECRET_KEY", "sk_live_test")
	t.Setenv("ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
	t.Setenv("CORS_ALLOWED_ORIGINS", "*")

	_, err := Load()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "CORS_ALLOWED_ORIGINS")
}

func TestLoad_ProductionValidation_PassesWithAllRequiredFields(t *testing.T) {
	t.Setenv("ENVIRONMENT", "production")
	t.Setenv("DATABASE_URL", "postgres://user:pass@localhost/db")
	t.Setenv("CLERK_SECRET_KEY", "sk_live_test")
	t.Setenv("ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://boostedbudget.com")
	t.Setenv("PLAID_CLIENT_ID", "test-client-id")
	t.Setenv("PLAID_SECRET", "test-secret")
	t.Setenv("PADDLE_API_KEY", "test-api-key")
	t.Setenv("PADDLE_WEBHOOK_SECRET", "test-webhook-secret")

	_, err := Load()
	assert.NoError(t, err)
}

func TestLoad_DevelopmentPassesWithMinimalConfig(t *testing.T) {
	// Development should work with minimal env vars (or defaults)
	cfg, err := Load()
	require.NoError(t, err)
	assert.Equal(t, "development", cfg.Environment)
}

func TestLoad_StagingValidation_EnforcesValidation(t *testing.T) {
	t.Setenv("ENVIRONMENT", "staging")
	// Intentionally NOT setting any other required fields

	_, err := Load()
	assert.Error(t, err)
}

func TestLoad_ProductionValidation_RequiresPlaidCredentials(t *testing.T) {
	t.Setenv("ENVIRONMENT", "production")
	t.Setenv("DATABASE_URL", "postgres://user:pass@localhost/db")
	t.Setenv("CLERK_SECRET_KEY", "sk_live_test")
	t.Setenv("ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://boostedbudget.com")
	t.Setenv("PADDLE_API_KEY", "test-api-key")
	t.Setenv("PADDLE_WEBHOOK_SECRET", "test-webhook-secret")
	// Intentionally NOT setting PLAID_CLIENT_ID or PLAID_SECRET

	_, err := Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "PLAID")
}

func TestLoad_ProductionValidation_RequiresPlaidCredentialsTogether(t *testing.T) {
	t.Setenv("ENVIRONMENT", "production")
	t.Setenv("DATABASE_URL", "postgres://user:pass@localhost/db")
	t.Setenv("CLERK_SECRET_KEY", "sk_live_test")
	t.Setenv("ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://boostedbudget.com")
	t.Setenv("PLAID_CLIENT_ID", "test-client-id")
	// Intentionally NOT setting PLAID_SECRET
	t.Setenv("PADDLE_API_KEY", "test-api-key")
	t.Setenv("PADDLE_WEBHOOK_SECRET", "test-webhook-secret")

	_, err := Load()
	assert.Error(t, err)
}

func TestLoad_ProductionValidation_RequiresPaddleCredentials(t *testing.T) {
	t.Setenv("ENVIRONMENT", "production")
	t.Setenv("DATABASE_URL", "postgres://user:pass@localhost/db")
	t.Setenv("CLERK_SECRET_KEY", "sk_live_test")
	t.Setenv("ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://boostedbudget.com")
	t.Setenv("PLAID_CLIENT_ID", "test-client-id")
	t.Setenv("PLAID_SECRET", "test-secret")
	// Intentionally NOT setting PADDLE_API_KEY or PADDLE_WEBHOOK_SECRET

	_, err := Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "PADDLE")
}

func TestLoad_ProductionValidation_RequiresPaddleCredentialsTogether(t *testing.T) {
	t.Setenv("ENVIRONMENT", "production")
	t.Setenv("DATABASE_URL", "postgres://user:pass@localhost/db")
	t.Setenv("CLERK_SECRET_KEY", "sk_live_test")
	t.Setenv("ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://boostedbudget.com")
	t.Setenv("PLAID_CLIENT_ID", "test-client-id")
	t.Setenv("PLAID_SECRET", "test-secret")
	t.Setenv("PADDLE_API_KEY", "test-api-key")
	// Intentionally NOT setting PADDLE_WEBHOOK_SECRET

	_, err := Load()
	assert.Error(t, err)
}

func TestLoad_ProductionValidation_RequiresEncryptionKeyLength(t *testing.T) {
	t.Setenv("ENVIRONMENT", "production")
	t.Setenv("DATABASE_URL", "postgres://user:pass@localhost/db")
	t.Setenv("CLERK_SECRET_KEY", "sk_live_test")
	t.Setenv("ENCRYPTION_KEY", "tooshort")
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://boostedbudget.com")
	t.Setenv("PLAID_CLIENT_ID", "test-client-id")
	t.Setenv("PLAID_SECRET", "test-secret")
	t.Setenv("PADDLE_API_KEY", "test-api-key")
	t.Setenv("PADDLE_WEBHOOK_SECRET", "test-webhook-secret")

	_, err := Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "32 bytes")
}

func TestLoad_ProductionValidation_AggregatesErrors(t *testing.T) {
	t.Setenv("ENVIRONMENT", "production")
	// Intentionally NOT setting any other required fields

	_, err := Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "DATABASE_URL")
	assert.Contains(t, err.Error(), "CLERK_SECRET_KEY")
	assert.Contains(t, err.Error(), "ENCRYPTION_KEY")
	assert.Contains(t, err.Error(), "CORS_ALLOWED_ORIGINS")
	assert.Contains(t, err.Error(), "PLAID")
	assert.Contains(t, err.Error(), "PADDLE")
}

func TestLoad_DevelopmentSkipsValidation(t *testing.T) {
	t.Setenv("ENVIRONMENT", "development")
	// No other env vars set

	_, err := Load()
	assert.NoError(t, err)
}

func TestLoad_DatabaseMaxConnsDefault(t *testing.T) {
	cfg, err := Load()
	require.NoError(t, err)

	assert.Equal(t, int32(25), cfg.DatabaseMaxConns, "DatabaseMaxConns should default to 25")
}

func TestLoad_DatabaseMaxConnsConfigurable(t *testing.T) {
	t.Setenv("DATABASE_MAX_CONNS", "50")

	cfg, err := Load()
	require.NoError(t, err)

	assert.Equal(t, int32(50), cfg.DatabaseMaxConns)
}
