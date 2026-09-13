package config

import (
	"fmt"
	"strconv"
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

func TestLoad_PostHogSupportIdentitySecretIsServerOnlyConfiguration(t *testing.T) {
	t.Setenv("ENVIRONMENT", "development")
	t.Setenv("POSTHOG_SECRET_API_TOKEN", "phs_server_only")

	cfg, err := Load()
	require.NoError(t, err)
	assert.Equal(t, "phs_server_only", cfg.PostHogSecretAPIToken)
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

func TestLoad_PaddleWebhookTimestampToleranceSecondsDefault(t *testing.T) {
	t.Setenv("ENVIRONMENT", "development")
	t.Setenv("PADDLE_WEBHOOK_TIMESTAMP_TOLERANCE_SECONDS", "")

	cfg, err := Load()
	require.NoError(t, err)

	assert.Equal(t, 5, cfg.PaddleWebhookTimestampToleranceSeconds,
		"PaddleWebhookTimestampToleranceSeconds should default to 5 (Paddle's documented SDK default)")
}

func TestLoad_PaddleWebhookTimestampToleranceSecondsConfigurable(t *testing.T) {
	t.Setenv("ENVIRONMENT", "development")
	t.Setenv("PADDLE_WEBHOOK_TIMESTAMP_TOLERANCE_SECONDS", "30")

	cfg, err := Load()
	require.NoError(t, err)

	assert.Equal(t, 30, cfg.PaddleWebhookTimestampToleranceSeconds)
}

func TestLoad_PaddleWebhookTimestampToleranceSecondsInvalidValueFallsBackToDefault(t *testing.T) {
	t.Setenv("ENVIRONMENT", "development")

	for _, invalid := range []string{"0", "-1", "not-a-number"} {
		t.Run(invalid, func(t *testing.T) {
			t.Setenv("PADDLE_WEBHOOK_TIMESTAMP_TOLERANCE_SECONDS", invalid)

			cfg, err := Load()
			require.NoError(t, err)

			assert.Equal(t, 5, cfg.PaddleWebhookTimestampToleranceSeconds,
				"a zero/negative/invalid PADDLE_WEBHOOK_TIMESTAMP_TOLERANCE_SECONDS must fall back to the default, never disable the replay window")
		})
	}
}

// TestLoad_PaddleWebhookTimestampToleranceSecondsAboveMaxFallsBackToDefault is issue #1514
// fix-round-1 item 1's regression test: an operator typo like 999999999 must NOT be accepted as
// "validly configured" — it would silently and completely disable the replay window on a
// payments path. It must fall back to the default, exactly like the zero/negative case.
func TestLoad_PaddleWebhookTimestampToleranceSecondsAboveMaxFallsBackToDefault(t *testing.T) {
	t.Setenv("ENVIRONMENT", "development")

	for _, tooLarge := range []string{"301", "999999999"} {
		t.Run(tooLarge, func(t *testing.T) {
			t.Setenv("PADDLE_WEBHOOK_TIMESTAMP_TOLERANCE_SECONDS", tooLarge)

			cfg, err := Load()
			require.NoError(t, err)

			assert.Equal(t, 5, cfg.PaddleWebhookTimestampToleranceSeconds,
				"a PADDLE_WEBHOOK_TIMESTAMP_TOLERANCE_SECONDS above the max must fall back to the default, never accept a value that effectively disables the replay window")
		})
	}
}

// TestLoad_PaddleWebhookTimestampToleranceSecondsBoundaries checks the inclusive edges: 1 (just
// above zero) and PaddleWebhookTimestampToleranceMaxSeconds (the max itself) must both be
// ACCEPTED, since the validation is "reject <=0 or >max", not a stricter open interval.
func TestLoad_PaddleWebhookTimestampToleranceSecondsBoundaries(t *testing.T) {
	t.Setenv("ENVIRONMENT", "development")

	testCases := []struct {
		name string
		env  string
		want int
	}{
		{"lower boundary (1)", "1", 1},
		{"upper boundary (max)", fmt.Sprintf("%d", PaddleWebhookTimestampToleranceMaxSeconds), PaddleWebhookTimestampToleranceMaxSeconds},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("PADDLE_WEBHOOK_TIMESTAMP_TOLERANCE_SECONDS", tc.env)

			cfg, err := Load()
			require.NoError(t, err)

			assert.Equal(t, tc.want, cfg.PaddleWebhookTimestampToleranceSeconds)
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
	// Issue #1531 Task 8: RATE_LIMIT_REDIS_URL is now required in production/staging.
	t.Setenv("RATE_LIMIT_REDIS_URL", "rediss://:pw@limiter.internal:6379/0")

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
	// Cleared (not just omitted): an ambient RATE_LIMIT_REDIS_URL export would
	// otherwise leak in and mask a real regression in this check.
	t.Setenv("RATE_LIMIT_REDIS_URL", "")

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
	assert.Contains(t, err.Error(), "RATE_LIMIT_REDIS_URL")
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

// ─── Issue #1531 Task 8: RATE_LIMIT_REDIS_URL rename, cache/tuning fields, RabbitMQURL removal ──

// setProdRequiredEnv stubs every field validate() requires in production/staging EXCEPT the
// Redis fields under test here, so each test below only needs to set/clear the Redis-specific
// env vars it cares about. Mirrors the existing pattern in this file (e.g.
// TestLoad_ProductionValidation_PassesWithAllRequiredFields) of explicitly clearing/setting every
// checked field rather than relying on ambient shell state.
func setProdRequiredEnv(t *testing.T) {
	t.Helper()
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
}

// TestConfig_RateLimitRedisURLDefaultsToEmptyNotLocalhost is the RED test for the hazard
// documented on RateLimitRedisURL's doc comment: a production deploy that forgot the secret used
// to silently default to redis://localhost:6379, trip the breaker, and run permanently on the
// in-memory limiter fallback — a correct alert with a misleading cause.
func TestConfig_RateLimitRedisURLDefaultsToEmptyNotLocalhost(t *testing.T) {
	t.Setenv("ENVIRONMENT", "development")
	t.Setenv("RATE_LIMIT_REDIS_URL", "")

	cfg, err := Load()
	require.NoError(t, err)

	assert.Equal(t, "", cfg.RateLimitRedisURL,
		"RateLimitRedisURL must default to empty, never redis://localhost:6379")
}

func TestConfig_RateLimitRedisURLConfigurable(t *testing.T) {
	t.Setenv("ENVIRONMENT", "development")
	t.Setenv("RATE_LIMIT_REDIS_URL", "rediss://:pw@limiter.internal:6379/0")

	cfg, err := Load()
	require.NoError(t, err)

	assert.Equal(t, "rediss://:pw@limiter.internal:6379/0", cfg.RateLimitRedisURL)
}

func TestConfig_ValidateRequiresRateLimitRedisURLInProduction(t *testing.T) {
	t.Setenv("ENVIRONMENT", "production")
	setProdRequiredEnv(t)
	// Cleared (not just omitted): an ambient RATE_LIMIT_REDIS_URL export would
	// otherwise leak in and mask a real regression in this check.
	t.Setenv("RATE_LIMIT_REDIS_URL", "")

	_, err := Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "RATE_LIMIT_REDIS_URL")
}

func TestConfig_ValidateRejectsNonRedisSchemeForRateLimitRedisURL(t *testing.T) {
	t.Setenv("ENVIRONMENT", "production")
	setProdRequiredEnv(t)
	t.Setenv("RATE_LIMIT_REDIS_URL", "http://not-redis.example.com:6379")

	_, err := Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "RATE_LIMIT_REDIS_URL")
	assert.Contains(t, err.Error(), "scheme")
}

func TestConfig_ValidateDoesNotRequireCacheRedisURL(t *testing.T) {
	t.Setenv("ENVIRONMENT", "production")
	setProdRequiredEnv(t)
	t.Setenv("RATE_LIMIT_REDIS_URL", "rediss://:sup3rs3cr3t@limiter.internal:6379/0")
	t.Setenv("CACHE_REDIS_URL", "")

	_, err := Load()
	assert.NoError(t, err)
}

// TestConfig_ValidateRejectsCacheAndLimiterSharingOneURL is the discrimination-proof test for the
// hazard documented on RateLimitRedisURL's doc comment: pointing the cache at the same Redis
// instance as the rate limiter (provisioned noeviction) makes an evicting cache write OOM the
// limiter's SET — a permanent limiter fallback with an unrelated-looking cause.
func TestConfig_ValidateRejectsCacheAndLimiterSharingOneURL(t *testing.T) {
	t.Setenv("ENVIRONMENT", "production")
	setProdRequiredEnv(t)
	sharedURL := "rediss://:sup3rs3cr3t@shared.internal:6379/0"
	t.Setenv("RATE_LIMIT_REDIS_URL", sharedURL)
	t.Setenv("CACHE_REDIS_URL", sharedURL)

	_, err := Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "CACHE_REDIS_URL")
	assert.Contains(t, err.Error(), "RATE_LIMIT_REDIS_URL")
}

// TestConfig_ValidationErrorsNeverContainTheURLPassword is the discrimination-proof test for the
// "never leak the password" rule: uses a distinctive password (sup3rs3cr3t) and asserts it
// appears nowhere in the joined validation error output, even though every other field is ALSO
// cleared so validate() emits its full set of joined errors.
func TestConfig_ValidationErrorsNeverContainTheURLPassword(t *testing.T) {
	t.Setenv("ENVIRONMENT", "production")
	// Deliberately clear everything else so validate() reports every possible error alongside
	// the redis ones — proving the password never leaks through ANY joined error message, not
	// just the redis-specific ones.
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
	// Same URL for both fields: also exercises the cache/limiter-sharing rule's error message.
	sharedURL := "rediss://user:sup3rs3cr3t@limiter.internal:6379/0"
	t.Setenv("RATE_LIMIT_REDIS_URL", sharedURL)
	t.Setenv("CACHE_REDIS_URL", sharedURL)

	_, err := Load()
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "sup3rs3cr3t",
		"validation error messages must never contain the Redis URL — it carries a password; refer to the env var by name only")
}

func TestConfig_RateLimitBackendRejectsUnknownValues(t *testing.T) {
	// Must be rejected in EVERY environment, not just production/staging — an unrecognised
	// RATE_LIMIT_BACKEND value is a config typo the process should never silently misinterpret.
	t.Setenv("ENVIRONMENT", "development")
	t.Setenv("RATE_LIMIT_BACKEND", "bogus")

	_, err := Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "RATE_LIMIT_BACKEND")
}

func TestConfig_RateLimitBackendAcceptsKnownValues(t *testing.T) {
	for _, v := range []string{"auto", "memory", "redis"} {
		t.Run(v, func(t *testing.T) {
			t.Setenv("ENVIRONMENT", "development")
			t.Setenv("RATE_LIMIT_BACKEND", v)

			cfg, err := Load()
			require.NoError(t, err)
			assert.Equal(t, v, cfg.RateLimitBackend)
		})
	}
}

func TestConfig_RateLimitBackendDefaultsToAuto(t *testing.T) {
	t.Setenv("ENVIRONMENT", "development")
	t.Setenv("RATE_LIMIT_BACKEND", "")

	cfg, err := Load()
	require.NoError(t, err)
	assert.Equal(t, "auto", cfg.RateLimitBackend)
}

// TestConfig_RateLimitSentinelLimitZeroMeansUseIPLimit pins the config-layer half of the
// contract with ratelimitstore.Options.withDefaults (TestRedisBackend_SentinelLimitDefaultsToRegularLimitWhenZero
// on that side): RateLimitSentinelLimit's zero-value default must remain 0, which
// ratelimitstore interprets as "use RegularLimit (RateLimitIP)".
func TestConfig_RateLimitSentinelLimitZeroMeansUseIPLimit(t *testing.T) {
	t.Setenv("ENVIRONMENT", "development")
	t.Setenv("RATE_LIMIT_SENTINEL_LIMIT", "")

	cfg, err := Load()
	require.NoError(t, err)
	assert.Equal(t, 0, cfg.RateLimitSentinelLimit)
}

func TestConfig_RateLimitSentinelLimitConfigurable(t *testing.T) {
	t.Setenv("ENVIRONMENT", "development")
	t.Setenv("RATE_LIMIT_SENTINEL_LIMIT", "500")

	cfg, err := Load()
	require.NoError(t, err)
	assert.Equal(t, 500, cfg.RateLimitSentinelLimit)
}

func TestConfig_RateLimitFallbackDivisorDefaultsToOne(t *testing.T) {
	t.Setenv("ENVIRONMENT", "development")
	t.Setenv("RATE_LIMIT_FALLBACK_DIVISOR", "")

	cfg, err := Load()
	require.NoError(t, err)
	assert.Equal(t, 1, cfg.RateLimitFallbackDivisor)
}

func TestConfig_RateLimitFallbackDivisorConfigurable(t *testing.T) {
	t.Setenv("ENVIRONMENT", "development")
	t.Setenv("RATE_LIMIT_FALLBACK_DIVISOR", "4")

	cfg, err := Load()
	require.NoError(t, err)
	assert.Equal(t, 4, cfg.RateLimitFallbackDivisor)
}

func TestConfig_CacheEnabledDefaultsToTrue(t *testing.T) {
	t.Setenv("ENVIRONMENT", "development")
	t.Setenv("CACHE_ENABLED", "")

	cfg, err := Load()
	require.NoError(t, err)
	assert.True(t, cfg.CacheEnabled)
}

func TestConfig_CacheEnabledConfigurable(t *testing.T) {
	t.Setenv("ENVIRONMENT", "development")
	t.Setenv("CACHE_ENABLED", "false")

	cfg, err := Load()
	require.NoError(t, err)
	assert.False(t, cfg.CacheEnabled)
}

func TestConfig_CacheNamespacesDisabledDefaultsToEmpty(t *testing.T) {
	t.Setenv("ENVIRONMENT", "development")
	t.Setenv("CACHE_NAMESPACES_DISABLED", "")

	cfg, err := Load()
	require.NoError(t, err)
	assert.Equal(t, "", cfg.CacheNamespacesDisabled)
}

func TestConfig_CacheNamespacesDisabledConfigurable(t *testing.T) {
	t.Setenv("ENVIRONMENT", "development")
	t.Setenv("CACHE_NAMESPACES_DISABLED", "budget-summary,net-worth")

	cfg, err := Load()
	require.NoError(t, err)
	assert.Equal(t, "budget-summary,net-worth", cfg.CacheNamespacesDisabled)
}

func TestConfig_RedisTimeoutMsAndPoolSizeDefaults(t *testing.T) {
	t.Setenv("ENVIRONMENT", "development")
	t.Setenv("REDIS_TIMEOUT_MS", "")
	t.Setenv("REDIS_POOL_SIZE", "")

	cfg, err := Load()
	require.NoError(t, err)
	assert.Equal(t, 200, cfg.RedisTimeoutMs)
	assert.Equal(t, 10, cfg.RedisPoolSize)
}

// TestConfig_RedisTimeoutMsNonPositive_FallsBackToDefault is GDPR/rate-limit round-6 finding P4:
// REDIS_TIMEOUT_MS=0 (or negative) used to reach newRedisClientOptions as a literal 0/negative
// time.Duration, which redisclient.Options's own <= 0 check silently upgrades to ITS 200ms
// default — while middleware.RateLimitConfig.PrimaryTimeout (fed the SAME cfg.RedisTimeoutMs
// value) falls back to defaultRedisLimiterTimeout, 50ms, on its own separate <= 0 check. One env
// var, set to 0, therefore produced two DIFFERENT effective timeouts (200ms vs 50ms) instead of
// the single, coherent value this field's own doc comment promises ("Both consumers must read the
// SAME value"). Fixed by rejecting non-positive REDIS_TIMEOUT_MS at Load() itself — the same
// getEnvAsPositiveInt convention RATE_LIMIT_MAX_ENTRIES already uses — so 0/negative now falls
// back to the documented default (200) before either consumer ever sees it, and the two consumers
// stay in agreement for every reachable value.
func TestConfig_RedisTimeoutMsNonPositive_FallsBackToDefault(t *testing.T) {
	t.Setenv("ENVIRONMENT", "development")
	t.Setenv("REDIS_POOL_SIZE", "")

	t.Run("zero", func(t *testing.T) {
		t.Setenv("REDIS_TIMEOUT_MS", "0")
		cfg, err := Load()
		require.NoError(t, err)
		assert.Equal(t, 200, cfg.RedisTimeoutMs,
			"REDIS_TIMEOUT_MS=0 must fall back to the documented default, not reach consumers as a "+
				"literal 0 that each independently reinterprets")
	})

	t.Run("negative", func(t *testing.T) {
		t.Setenv("REDIS_TIMEOUT_MS", "-50")
		cfg, err := Load()
		require.NoError(t, err)
		assert.Equal(t, 200, cfg.RedisTimeoutMs,
			"a negative REDIS_TIMEOUT_MS must fall back to the documented default the same way 0 does")
	})
}

// TestConfig_RedisTimeoutMsOverflow_FallsBackToDefault is #1531 G3 (Codex-verified): P4 rejected
// non-positive REDIS_TIMEOUT_MS, but getEnvAsPositiveInt accepts ANY positive int — including one
// large enough that time.Duration(ms) * time.Millisecond overflows int64 and wraps negative.
// REDIS_TIMEOUT_MS=9223372036855 does exactly that: main.go's two consumers
// (time.Duration(cfg.RedisTimeoutMs) * time.Millisecond) each see a negative Duration and fall
// back to THEIR OWN separate default (redisclient.Options's 200ms vs
// middleware.RateLimitConfig's 50ms) — the identical P4 divergence, reached through a different
// door. Bounding RedisTimeoutMs at Load() (a few seconds is already far beyond any sane limiter
// budget) closes the class rather than this one instance.
func TestConfig_RedisTimeoutMsOverflow_FallsBackToDefault(t *testing.T) {
	t.Setenv("ENVIRONMENT", "development")
	t.Setenv("REDIS_POOL_SIZE", "")
	t.Setenv("REDIS_TIMEOUT_MS", "9223372036855")

	cfg, err := Load()
	require.NoError(t, err)
	assert.Equal(t, 200, cfg.RedisTimeoutMs,
		"an absurdly large REDIS_TIMEOUT_MS that would overflow time.Duration(ms)*time.Millisecond "+
			"must fall back to the documented default, not reach consumers as a wrapped-negative "+
			"duration that each independently reinterprets")

	// Confirm the overflow claim itself, so this test doesn't just pin an arbitrary cap: the raw
	// env value really does wrap negative once converted the same way main.go converts it (a
	// runtime int64 multiply, via a variable so the compiler doesn't reject the constant
	// expression as an overflow at compile time — main.go's cfg.RedisTimeoutMs is a runtime int
	// too).
	rawMs := int64(9223372036855)
	overflowed := time.Duration(rawMs) * time.Millisecond
	require.Negative(t, overflowed, "test setup: REDIS_TIMEOUT_MS fixture must actually overflow")
}

// TestConfig_RedisTimeoutMsMax_IsLoweredForTheRequestPath is Issue #1531 slice H5. G3 set
// RedisTimeoutMsMax to 10000 purely to stop the int64 overflow above; it never asked whether 10s
// is a SANE ceiling for what this value actually feeds. It is not: cfg.RedisTimeoutMs is the rate
// limiter's PRIMARY_TIMEOUT (main.go:2741, middleware.RateLimitConfig.PrimaryTimeout), which
// BLOCKS the request path at middleware position 4 (rate_limit_fallback.go's Allow) before falling
// back. redisclient/client.go's own package doc says a 1s DialTimeout delay at that position is "a
// latency cliff" with "a real risk of tripping Fly's 5s health check, cycling machines, and causing
// a self-inflicted outage" — a 10s ceiling permits double that, on every single request, not just a
// cold dial. This reverses G3's magnitude choice (made two commits earlier in this branch) while
// keeping G3's actual purpose (no int64 overflow) intact — the fix is a smaller sane bound, not
// removing the bound.
//
// The two cases below pin the boundary VALUE, not just "is bounded at all": G3's own
// TestConfig_RedisTimeoutMsOverflow_FallsBackToDefault only exercises 9223372036855, an input so
// extreme it would fall back to the default at ANY ceiling from 1 to 2^63-1 — it cannot
// distinguish a 10000ms cap from a 2000ms one. Asserting the exact-max value is accepted and
// max+1 is rejected makes a future regression that quietly raises the ceiling back toward 10s fail
// this test, not just the overflow case.
func TestConfig_RedisTimeoutMsMax_IsLoweredForTheRequestPath(t *testing.T) {
	require.LessOrEqual(t, RedisTimeoutMsMax, 2000, "H5: the ceiling must stay well under Fly's 5s health-check timeout at middleware position 4")
	require.Greater(t, RedisTimeoutMsMax, 0)

	t.Run("exactly the ceiling is accepted", func(t *testing.T) {
		t.Setenv("ENVIRONMENT", "development")
		t.Setenv("REDIS_POOL_SIZE", "")
		t.Setenv("REDIS_TIMEOUT_MS", strconv.Itoa(RedisTimeoutMsMax))

		cfg, err := Load()
		require.NoError(t, err)
		assert.Equal(t, RedisTimeoutMsMax, cfg.RedisTimeoutMs, "a value exactly at the ceiling must be accepted, not treated as out of range")
	})

	t.Run("one above the ceiling falls back to default", func(t *testing.T) {
		t.Setenv("ENVIRONMENT", "development")
		t.Setenv("REDIS_POOL_SIZE", "")
		t.Setenv("REDIS_TIMEOUT_MS", strconv.Itoa(RedisTimeoutMsMax+1))

		cfg, err := Load()
		require.NoError(t, err)
		assert.Equal(t, 200, cfg.RedisTimeoutMs, "one millisecond over the ceiling must fall back to the documented default, not be silently accepted")
	})
}

// TestConfig_RabbitMQURLIsGone is a compile-time-only assertion for issue #1531 Task 8:
// Config.RabbitMQURL no longer exists — zero consumers, no `amqp` import anywhere in the Go tree
// (River, Postgres-backed, is the queue). There is nothing to assert about its absence at runtime
// beyond "this package still compiles" without the field; this test is a named, discoverable
// marker of that fact rather than relying solely on reading config.go.
func TestConfig_RabbitMQURLIsGone(t *testing.T) {
	t.Setenv("ENVIRONMENT", "development")

	cfg, err := Load()
	require.NoError(t, err)
	_ = cfg // no RabbitMQURL field exists to reference — that absence IS the assertion
}
