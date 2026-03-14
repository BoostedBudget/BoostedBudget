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
	t.Setenv("ENCRYPTION_KEY", "some-32-byte-key-for-aes-256-gcm!")
	t.Setenv("CORS_ALLOWED_ORIGINS", "*")

	_, err := Load()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "CORS_ALLOWED_ORIGINS")
}

func TestLoad_ProductionValidation_PassesWithAllRequiredFields(t *testing.T) {
	t.Setenv("ENVIRONMENT", "production")
	t.Setenv("DATABASE_URL", "postgres://user:pass@localhost/db")
	t.Setenv("CLERK_SECRET_KEY", "sk_live_test")
	t.Setenv("ENCRYPTION_KEY", "some-32-byte-key-for-aes-256-gcm!")
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://boostedbudget.com")

	_, err := Load()
	assert.NoError(t, err)
}

func TestLoad_DevelopmentPassesWithMinimalConfig(t *testing.T) {
	// Development should work with minimal env vars (or defaults)
	cfg, err := Load()
	require.NoError(t, err)
	assert.Equal(t, "development", cfg.Environment)
}
