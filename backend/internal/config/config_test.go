package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad_ServerTimeoutDefaults(t *testing.T) {
	// Stubbed rather than relying on ambient state: an exported ENVIRONMENT of
	// "production"/"staging" would make Load() enter validate() and return an error
	// instead, failing require.NoError for an unrelated reason; and an exported
	// SERVER_*_TIMEOUT / MAX_BODY_SIZE would break the default assertions below.
	t.Setenv("ENVIRONMENT", "development")
	t.Setenv("SERVER_READ_TIMEOUT", "")
	t.Setenv("SERVER_WRITE_TIMEOUT", "")
	t.Setenv("SERVER_IDLE_TIMEOUT", "")
	t.Setenv("MAX_BODY_SIZE", "")

	cfg, err := Load()
	require.NoError(t, err)

	assert.Equal(t, 30*time.Second, cfg.ReadTimeout, "ReadTimeout should default to 30s")
	assert.Equal(t, 30*time.Second, cfg.WriteTimeout, "WriteTimeout should default to 30s")
	assert.Equal(t, 120*time.Second, cfg.IdleTimeout, "IdleTimeout should default to 120s")
	assert.Equal(t, "1M", cfg.MaxBodySize, "MaxBodySize should default to 1M")
}

func TestLoad_ServerTimeoutsConfigurable(t *testing.T) {
	// See TestLoad_ServerTimeoutDefaults: an ambient production/staging ENVIRONMENT
	// would fail require.NoError below for an unrelated reason.
	t.Setenv("ENVIRONMENT", "development")
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

func TestLoad_RateLimitMaxEntriesDefault(t *testing.T) {
	t.Setenv("ENVIRONMENT", "development")
	t.Setenv("RATE_LIMIT_MAX_ENTRIES", "")

	cfg, err := Load()
	require.NoError(t, err)

	assert.Equal(t, 20000, cfg.RateLimitMaxEntries, "RateLimitMaxEntries should default to 20000")
}

func TestLoad_RateLimitMaxEntriesConfigurable(t *testing.T) {
	t.Setenv("ENVIRONMENT", "development")
	t.Setenv("RATE_LIMIT_MAX_ENTRIES", "5000")

	cfg, err := Load()
	require.NoError(t, err)

	assert.Equal(t, 5000, cfg.RateLimitMaxEntries)
}

func TestLoad_RateLimitMaxEntriesInvalidValueFallsBackToDefault(t *testing.T) {
	t.Setenv("ENVIRONMENT", "development")

	for _, invalid := range []string{"0", "-1", "not-a-number"} {
		t.Run(invalid, func(t *testing.T) {
			t.Setenv("RATE_LIMIT_MAX_ENTRIES", invalid)

			cfg, err := Load()
			require.NoError(t, err)

			assert.Equal(t, 20000, cfg.RateLimitMaxEntries,
				"a zero/negative/invalid RATE_LIMIT_MAX_ENTRIES must fall back to the default, never produce an uncapped store")
		})
	}
}

func TestLoad_ProductionValidation_RequiresEncryptionKey(t *testing.T) {
	t.Setenv("ENVIRONMENT", "production")
	t.Setenv("DATABASE_URL", "postgres://user:pass@localhost/db")
	t.Setenv("CLERK_SECRET_KEY", "sk_live_test")
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://boostedbudget.com")
	// Intentionally clearing (not just omitting) ENCRYPTION_KEY: an ambient
	// export (e.g. to run integration suites locally) would otherwise leak in
	// and make this required-field check pass, masking a real regression.
	t.Setenv("ENCRYPTION_KEY", "")

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
	t.Setenv("CF_ORIGIN_SECRET", "a3f9e2c1b0d84726f5e3a1c0b9d87264a3f9e2c1b0d84726f5e3a1c0b9d87264")
	t.Setenv("DELETION_TOKEN_SECRET", "test-deletion-token-secret")

	_, err := Load()
	assert.NoError(t, err)
}

// TestLoad_ProductionValidation_RequiresDeletionTokenSecret is the RED test for Issue #1413
// round-8 Blocker 2 (Codex review): RetentionCleanupCheckWorker.repairPlaidDisconnects is the ONLY
// place a read-only user's Plaid items are ever disconnected, and (before this fix) the worker was
// constructed only when DELETION_TOKEN_SECRET was set — but production/staging validation never
// checked that secret was present at all. A deployment with valid Plaid/Paddle credentials but no
// DELETION_TOKEN_SECRET would pass validation and silently never disconnect anyone. This test
// proves validate() now catches that misconfiguration as belt-and-braces, independent of the
// main.go construction fix (which decouples the worker's construction from this secret so the
// repair pass runs even when this validation is somehow bypassed).
func TestLoad_ProductionValidation_RequiresDeletionTokenSecret(t *testing.T) {
	t.Setenv("ENVIRONMENT", "production")
	t.Setenv("DATABASE_URL", "postgres://user:pass@localhost/db")
	t.Setenv("CLERK_SECRET_KEY", "sk_live_test")
	t.Setenv("ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://boostedbudget.com")
	t.Setenv("PLAID_CLIENT_ID", "test-client-id")
	t.Setenv("PLAID_SECRET", "test-secret")
	t.Setenv("PADDLE_API_KEY", "test-api-key")
	t.Setenv("PADDLE_WEBHOOK_SECRET", "test-webhook-secret")
	t.Setenv("CF_ORIGIN_SECRET", "a3f9e2c1b0d84726f5e3a1c0b9d87264a3f9e2c1b0d84726f5e3a1c0b9d87264")
	// Cleared (not just omitted): an ambient DELETION_TOKEN_SECRET export would
	// otherwise leak in and mask a real regression in this required-field check.
	t.Setenv("DELETION_TOKEN_SECRET", "")

	_, err := Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "DELETION_TOKEN_SECRET")
}

func TestLoad_ProductionValidation_RequiresCFOriginSecret(t *testing.T) {
	t.Setenv("ENVIRONMENT", "production")
	t.Setenv("DATABASE_URL", "postgres://user:pass@localhost/db")
	t.Setenv("CLERK_SECRET_KEY", "sk_live_test")
	t.Setenv("ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://boostedbudget.com")
	t.Setenv("PLAID_CLIENT_ID", "test-client-id")
	t.Setenv("PLAID_SECRET", "test-secret")
	t.Setenv("PADDLE_API_KEY", "test-api-key")
	t.Setenv("PADDLE_WEBHOOK_SECRET", "test-webhook-secret")
	// Cleared (not just omitted): an ambient CF_ORIGIN_SECRET export would
	// otherwise leak in and mask a real regression in this required-field check.
	t.Setenv("CF_ORIGIN_SECRET", "")

	_, err := Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "CF_ORIGIN_SECRET")
}

func TestLoad_DevelopmentPassesWithMinimalConfig(t *testing.T) {
	// Development should work with minimal env vars (or defaults).
	// Cleared to "" rather than set to "development": getEnv returns its default for an
	// empty value exactly as it does for an unset one (config.go:262-267), so this pins
	// the unset-variable default path this test exists to cover, instead of bypassing it.
	// Without the stub an ambient ENVIRONMENT export fails the assertion below.
	t.Setenv("ENVIRONMENT", "")

	cfg, err := Load()
	require.NoError(t, err)
	assert.Equal(t, "development", cfg.Environment)
}

func TestLoad_StagingValidation_EnforcesValidation(t *testing.T) {
	t.Setenv("ENVIRONMENT", "staging")
	// Cleared (not just omitted): ambient exports of any of these (e.g. a
	// developer's shell configured to run the integration suites) would
	// otherwise satisfy validate() entirely and mask a real regression.
	t.Setenv("DATABASE_URL", "")
	t.Setenv("CLERK_SECRET_KEY", "")
	t.Setenv("ENCRYPTION_KEY", "")
	t.Setenv("CORS_ALLOWED_ORIGINS", "")
	t.Setenv("PLAID_CLIENT_ID", "")
	t.Setenv("PLAID_SECRET", "")
	t.Setenv("PADDLE_API_KEY", "")
	t.Setenv("PADDLE_WEBHOOK_SECRET", "")
	t.Setenv("CF_ORIGIN_SECRET", "")
	t.Setenv("DELETION_TOKEN_SECRET", "")

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
	// Cleared (not just omitted): ambient PLAID_CLIENT_ID/PLAID_SECRET exports
	// would otherwise leak in and mask a real regression in this check.
	t.Setenv("PLAID_CLIENT_ID", "")
	t.Setenv("PLAID_SECRET", "")

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
	// Cleared (not just omitted): an ambient PLAID_SECRET export would
	// otherwise leak in and mask a real regression in this check.
	t.Setenv("PLAID_SECRET", "")
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
	// Cleared (not just omitted): ambient PADDLE_API_KEY/PADDLE_WEBHOOK_SECRET
	// exports would otherwise leak in and mask a real regression in this check.
	t.Setenv("PADDLE_API_KEY", "")
	t.Setenv("PADDLE_WEBHOOK_SECRET", "")

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
	// Cleared (not just omitted): an ambient PADDLE_WEBHOOK_SECRET export
	// would otherwise leak in and mask a real regression in this check.
	t.Setenv("PADDLE_WEBHOOK_SECRET", "")

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
	// Stub every field validate() checks to empty, rather than relying on the
	// ambient shell having none of them set. Without this, exporting e.g.
	// DATABASE_URL/CLERK_SECRET_KEY (as one does to run the integration suites
	// locally) makes validate() stop reporting that field as missing, and the
	// assert.Contains below for it fails — a false failure, not a real bug.
	t.Setenv("DATABASE_URL", "")
	t.Setenv("CLERK_SECRET_KEY", "")
	t.Setenv("ENCRYPTION_KEY", "")
	t.Setenv("CORS_ALLOWED_ORIGINS", "")
	t.Setenv("PLAID_CLIENT_ID", "")
	t.Setenv("PLAID_SECRET", "")
	t.Setenv("PADDLE_API_KEY", "")
	t.Setenv("PADDLE_WEBHOOK_SECRET", "")
	t.Setenv("CF_ORIGIN_SECRET", "")
	t.Setenv("DELETION_TOKEN_SECRET", "")

	_, err := Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "DATABASE_URL")
	assert.Contains(t, err.Error(), "CLERK_SECRET_KEY")
	assert.Contains(t, err.Error(), "ENCRYPTION_KEY")
	assert.Contains(t, err.Error(), "CORS_ALLOWED_ORIGINS")
	assert.Contains(t, err.Error(), "PLAID")
	assert.Contains(t, err.Error(), "PADDLE")
	assert.Contains(t, err.Error(), "CF_ORIGIN_SECRET")
	assert.Contains(t, err.Error(), "DELETION_TOKEN_SECRET")
}

func TestLoad_DevelopmentSkipsValidation(t *testing.T) {
	t.Setenv("ENVIRONMENT", "development")
	// No other env vars set

	_, err := Load()
	assert.NoError(t, err)
}

func TestLoad_DatabaseMaxConnsDefault(t *testing.T) {
	// Stubbed rather than relying on ambient state: an exported DATABASE_MAX_CONNS
	// would break the default-value assertion below, and an exported ENVIRONMENT
	// of "production"/"staging" would make Load() enter validate() and return an
	// error instead, failing require.NoError for an unrelated reason.
	t.Setenv("ENVIRONMENT", "development")
	t.Setenv("DATABASE_MAX_CONNS", "")

	cfg, err := Load()
	require.NoError(t, err)

	assert.Equal(t, int32(25), cfg.DatabaseMaxConns, "DatabaseMaxConns should default to 25")
}

func TestLoad_DatabaseMaxConnsConfigurable(t *testing.T) {
	// See TestLoad_DatabaseMaxConnsDefault: an ambient production/staging ENVIRONMENT
	// would fail require.NoError below for an unrelated reason.
	t.Setenv("ENVIRONMENT", "development")
	t.Setenv("DATABASE_MAX_CONNS", "50")

	cfg, err := Load()
	require.NoError(t, err)

	assert.Equal(t, int32(50), cfg.DatabaseMaxConns)
}

func TestLoad_MCPWorkerSecretDefaultsEmpty(t *testing.T) {
	// Stubbed rather than relying on ambient state: an exported MCP_WORKER_SECRET
	// would break the default-value assertion below, and an exported ENVIRONMENT
	// of "production"/"staging" would make Load() enter validate() and return an
	// error instead, failing require.NoError for an unrelated reason.
	t.Setenv("ENVIRONMENT", "development")
	t.Setenv("MCP_WORKER_SECRET", "")

	cfg, err := Load()
	require.NoError(t, err)
	// Default is empty — IsMCPOrigin() returns false (fail-closed) in dev.
	assert.Equal(t, "", cfg.MCPWorkerSecret)
}

func TestLoad_MCPWorkerSecretConfigurable(t *testing.T) {
	// See TestLoad_MCPWorkerSecretDefaultsEmpty: an ambient production/staging
	// ENVIRONMENT would fail require.NoError below for an unrelated reason.
	t.Setenv("ENVIRONMENT", "development")
	t.Setenv("MCP_WORKER_SECRET", "test-mcp-worker-secret")

	cfg, err := Load()
	require.NoError(t, err)

	assert.Equal(t, "test-mcp-worker-secret", cfg.MCPWorkerSecret)
}
