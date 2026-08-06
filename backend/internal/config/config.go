package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config holds application configuration
type Config struct {
	// Server
	Port        string
	Environment string
	// EnvironmentExplicitlySet reports whether ENVIRONMENT was set to a NON-EMPTY value in
	// the process environment, as opposed to Environment holding the "development" default.
	// It is `os.Getenv("ENVIRONMENT") != ""`, deliberately not `os.LookupEnv` — an
	// explicitly blank ENVIRONMENT= (a blanked Fly secret, an empty compose interpolation)
	// gets the same defaulted treatment from getEnv, so it must not count as explicit here
	// either.
	//
	// Issue #1498: getEnv maps an unset AND an empty ENVIRONMENT to "development", so
	// Environment alone cannot distinguish "this is a dev box" from "somebody deployed
	// production and forgot to set ENVIRONMENT". Any security decision that must fail safe
	// on a missing variable has to consult this flag — see AdminAllowlistEnvironment.
	EnvironmentExplicitlySet bool
	Version                  string
	CORSAllowedOrigins       string

	// HTTP server timeouts and limits
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	IdleTimeout  time.Duration
	MaxBodySize  string

	// Rate limiting
	RateLimitIP   int // per-IP requests per minute
	RateLimitAuth int // per-IP requests per minute for sensitive endpoints

	// Database
	DatabaseURL      string
	DatabaseMaxConns int32

	// Redis
	RedisURL string

	// RabbitMQ
	RabbitMQURL string

	// Clerk
	ClerkSecretKey      string
	ClerkPublishableKey string

	// QA Auth Bypass (QA/Staging only - never set in production)
	QABypassSecret       string
	QAAdminClerkUserID   string
	QAPremiumClerkUserID string

	// Plaid
	PlaidClientID    string
	PlaidSecret      string
	PlaidEnv         string
	PlaidWebhookURL  string
	PlaidRedirectURI string

	// Paddle
	PaddleAPIKey        string
	PaddleWebhookSecret string
	PaddleEnvironment   string

	// Encryption
	EncryptionKey string

	// Email (SendGrid)
	SendGridAPIKey    string
	SendGridFromEmail string
	SendGridFromName  string

	// Deletion Configuration
	DeletionGracePeriodDays int
	DeletionWorkerInterval  string
	DeletionTokenSecret     string

	// Retention Configuration (#930: 6-month read-only period before auto-deletion)
	RetentionPeriodMonths int // env: RETENTION_PERIOD_MONTHS, default: 6

	// DLQ Configuration (#633: Dead Letter Queue admin UI + cleanup)
	DLQRetentionDays int // env: DLQ_RETENTION_DAYS, default: 30

	// Frontend URL (for cancellation links)
	FrontendURL string

	// Observability
	LogLevel         string  // from LOG_LEVEL — "debug"/"info"/"warn"/"error", default "info"
	EnableMetrics    bool    // from ENABLE_METRICS, default true
	SentryDSN        string  // from SENTRY_DSN — empty = disabled
	OTELEndpoint     string  // from OTEL_EXPORTER_OTLP_ENDPOINT — empty = disabled
	SentrySampleRate float64 // from SENTRY_SAMPLE_RATE — default 1.0
	OTELSampleRate   float64 // from OTEL_SAMPLE_RATE — default 1.0
	PostHogAPIKey    string  // from POSTHOG_API_KEY — empty = disabled
	PostHogHost      string  // from POSTHOG_HOST — default us.i.posthog.com

	// Paddle Reconciliation
	PaddleReconciliationEnabled bool // from PADDLE_RECONCILIATION_ENABLED, default true
	PaddleReconciliationAutoFix bool // from PADDLE_RECONCILIATION_AUTO_FIX, default false

	// Cloudflare Origin Secret (CF_ORIGIN_SECRET) — required in prod/staging
	CFOriginSecret string

	// MCP Worker Secret (MCP_WORKER_SECRET) — shared secret between MCP Cloudflare Worker
	// and backend; used to verify X-MCP-Worker-Secret header; fail-closed when empty.
	MCPWorkerSecret string // env: MCP_WORKER_SECRET, default: ""
}

// Load loads configuration from environment variables
func Load() (*Config, error) {
	cfg := &Config{
		// Server defaults
		Port:                     getEnv("PORT", "8080"),
		Environment:              getEnv("ENVIRONMENT", "development"),
		EnvironmentExplicitlySet: os.Getenv("ENVIRONMENT") != "",
		Version:                  getEnv("VERSION", "0.1.0"),
		CORSAllowedOrigins:       getEnv("CORS_ALLOWED_ORIGINS", "*"),

		// HTTP server timeouts and limits
		ReadTimeout:  getEnvAsDuration("SERVER_READ_TIMEOUT", 30*time.Second),
		WriteTimeout: getEnvAsDuration("SERVER_WRITE_TIMEOUT", 30*time.Second),
		IdleTimeout:  getEnvAsDuration("SERVER_IDLE_TIMEOUT", 120*time.Second),
		MaxBodySize:  getEnv("MAX_BODY_SIZE", "1M"),

		// Rate limiting
		RateLimitIP:   getEnvAsInt("RATE_LIMIT_IP", 100),
		RateLimitAuth: getEnvAsInt("RATE_LIMIT_AUTH", 10),

		// Database
		DatabaseURL:      getEnv("DATABASE_URL", ""),
		DatabaseMaxConns: int32(getEnvAsInt("DATABASE_MAX_CONNS", 25)),

		// Redis
		RedisURL: getEnv("REDIS_URL", "redis://localhost:6379"),

		// RabbitMQ
		RabbitMQURL: getEnv("RABBITMQ_URL", "amqp://localhost:5672/"),

		// Clerk
		ClerkSecretKey:      getEnv("CLERK_SECRET_KEY", ""),
		ClerkPublishableKey: getEnv("CLERK_PUBLISHABLE_KEY", ""),

		// QA Auth Bypass
		QABypassSecret:       getEnv("QA_BYPASS_SECRET", ""),
		QAAdminClerkUserID:   getEnv("QA_ADMIN_CLERK_USER_ID", ""),
		QAPremiumClerkUserID: getEnv("QA_PREMIUM_CLERK_USER_ID", ""),

		// Plaid
		PlaidClientID:    getEnv("PLAID_CLIENT_ID", ""),
		PlaidSecret:      getEnv("PLAID_SECRET", ""),
		PlaidEnv:         getEnv("PLAID_ENV", "sandbox"),
		PlaidWebhookURL:  getEnv("PLAID_WEBHOOK_URL", ""),
		PlaidRedirectURI: getEnv("PLAID_REDIRECT_URI", ""),

		// Paddle
		PaddleAPIKey:        getEnv("PADDLE_API_KEY", ""),
		PaddleWebhookSecret: getEnv("PADDLE_WEBHOOK_SECRET", ""),
		PaddleEnvironment:   getEnv("PADDLE_ENVIRONMENT", "sandbox"),

		// Encryption
		EncryptionKey: getEnv("ENCRYPTION_KEY", ""),

		// Email (SendGrid)
		SendGridAPIKey:    getEnv("SENDGRID_API_KEY", ""),
		SendGridFromEmail: getEnv("SENDGRID_FROM_EMAIL", "noreply@boostedbudget.com"),
		SendGridFromName:  getEnv("SENDGRID_FROM_NAME", "BoostedBudget"),

		// Deletion Configuration
		DeletionGracePeriodDays: getEnvAsInt("DELETION_GRACE_PERIOD_DAYS", 30),
		DeletionWorkerInterval:  getEnv("DELETION_WORKER_INTERVAL", "1h"),
		DeletionTokenSecret:     getEnv("DELETION_TOKEN_SECRET", ""),

		// Retention Configuration (#930)
		RetentionPeriodMonths: getEnvAsInt("RETENTION_PERIOD_MONTHS", 6),

		// DLQ Configuration (#633)
		DLQRetentionDays: getEnvAsInt("DLQ_RETENTION_DAYS", 30),

		// Frontend URL
		FrontendURL: getEnv("FRONTEND_URL", "http://localhost:5173"),

		// Observability
		LogLevel:         getEnv("LOG_LEVEL", "info"),
		EnableMetrics:    getEnvBool("ENABLE_METRICS", true),
		SentryDSN:        getEnv("SENTRY_DSN", ""),
		OTELEndpoint:     getEnv("OTEL_EXPORTER_OTLP_ENDPOINT", ""),
		SentrySampleRate: getEnvFloat64("SENTRY_SAMPLE_RATE", 1.0),
		OTELSampleRate:   getEnvFloat64("OTEL_SAMPLE_RATE", 1.0),
		PostHogAPIKey:    getEnv("POSTHOG_API_KEY", ""),
		PostHogHost:      getEnv("POSTHOG_HOST", "https://us.i.posthog.com"),

		// Paddle Reconciliation
		PaddleReconciliationEnabled: getEnvBool("PADDLE_RECONCILIATION_ENABLED", true),
		PaddleReconciliationAutoFix: getEnvBool("PADDLE_RECONCILIATION_AUTO_FIX", false),

		// Cloudflare Origin Secret
		CFOriginSecret: getEnv("CF_ORIGIN_SECRET", ""),

		// MCP Worker Secret (fail-closed when empty — IsMCPOrigin returns false)
		MCPWorkerSecret: getEnv("MCP_WORKER_SECRET", ""),
	}

	// Validate required fields for production
	if cfg.Environment == "production" || cfg.Environment == "staging" {
		if err := cfg.validate(); err != nil {
			return nil, err
		}
	}

	return cfg, nil
}

// validate checks that required configuration is present for the production and staging environments
func (c *Config) validate() error {
	var errs []error

	// Core infrastructure (always required)
	if c.DatabaseURL == "" {
		errs = append(errs, fmt.Errorf("DATABASE_URL is required"))
	}
	if c.ClerkSecretKey == "" {
		errs = append(errs, fmt.Errorf("CLERK_SECRET_KEY is required"))
	}

	// Encryption (required + length check for AES-256)
	if c.EncryptionKey == "" {
		errs = append(errs, fmt.Errorf("ENCRYPTION_KEY is required"))
	} else if len([]byte(c.EncryptionKey)) != 32 {
		errs = append(errs, fmt.Errorf("ENCRYPTION_KEY must be exactly 32 bytes for AES-256 (got %d)", len([]byte(c.EncryptionKey))))
	}

	// CORS (no wildcard in prod/staging)
	if c.CORSAllowedOrigins == "" || c.CORSAllowedOrigins == "*" {
		errs = append(errs, fmt.Errorf("CORS_ALLOWED_ORIGINS must be explicitly set (not '*' or empty)"))
	}

	// Plaid (both required together)
	if c.PlaidClientID == "" || c.PlaidSecret == "" {
		errs = append(errs, fmt.Errorf("PLAID_CLIENT_ID and PLAID_SECRET are both required"))
	}

	// Paddle (both required together)
	if c.PaddleAPIKey == "" || c.PaddleWebhookSecret == "" {
		errs = append(errs, fmt.Errorf("PADDLE_API_KEY and PADDLE_WEBHOOK_SECRET are both required"))
	}

	// Cloudflare Origin Secret (required so direct *.fly.dev access is blocked)
	if c.CFOriginSecret == "" {
		errs = append(errs, fmt.Errorf("CF_ORIGIN_SECRET is required — set it to the value in the Cloudflare Transform Rule (see docs/FLYIO_SETUP.md)"))
	}

	// Issue #1413 round-8 Blocker 2 (Codex review): DELETION_TOKEN_SECRET gates deletion-service
	// construction in main.go (cancellation-link JWT signing), and RetentionCleanupCheckWorker's
	// Step 3 (deleteExpiredAccounts) needs a real DeletionService to actually hard-delete expired
	// accounts. The repair pass that disconnects a lapsed user's Plaid items no longer depends on
	// this secret (it is constructed unconditionally now), but a production/staging deployment
	// missing this secret is still misconfigured — hard-deletion silently never runs and account
	// deletion requests fail — so validation catches it as belt-and-braces, matching the same
	// "required together" treatment already given to Plaid/Paddle/CF_ORIGIN_SECRET above.
	if c.DeletionTokenSecret == "" {
		errs = append(errs, fmt.Errorf("DELETION_TOKEN_SECRET is required"))
	}

	return errors.Join(errs...)
}

// getEnv retrieves an environment variable or returns a default value
// AdminAllowlistEnvironment returns the environment name that the hardcoded admin
// allowlist (config/admin_emails.yaml) must be resolved against — see
// domain.AdminEmailConfig.EffectiveAdminEmails.
//
// Issue #1498: it returns the configured Environment ONLY when ENVIRONMENT was explicitly
// set, and "" otherwise. That empty string is not a recognised non-production name, so the
// allowlist's non_production_admin_emails entries are excluded.
//
// This exists because Environment defaults to "development", which IS a recognised
// non-production environment. Resolving the allowlist against Environment directly would
// mean a production deployment that forgot to set ENVIRONMENT silently loaded the CI and QA
// test accounts as production admins — a role-check-free bypass. Failing safe on a missing
// variable is the whole point; do not "simplify" this back to cfg.Environment.
//
// Every real deployment sets ENVIRONMENT explicitly (backend/fly.toml,
// backend/fly.staging.toml, docker-compose.yml, docker-compose.qa.yml,
// .github/workflows/deploy-qa.yml, apps/web/playwright.config.ts), so this costs nothing in
// practice. A bare `go run ./cmd/server` with no ENVIRONMENT loses the two test admins and
// keeps the owner accounts, which is the correct direction to fail.
func (c *Config) AdminAllowlistEnvironment() string {
	if c == nil || !c.EnvironmentExplicitlySet {
		return ""
	}
	return c.Environment
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getEnvAsInt retrieves an environment variable as an integer or returns a default value
func getEnvAsInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		var intValue int
		_, err := fmt.Sscanf(value, "%d", &intValue)
		if err == nil {
			return intValue
		}
	}
	return defaultValue
}

// getEnvAsDuration retrieves an environment variable as a time.Duration or returns a default value.
// Accepts standard Go duration strings (e.g. "30s", "1m", "2h").
func getEnvAsDuration(key string, defaultValue time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		d, err := time.ParseDuration(value)
		if err == nil {
			return d
		}
	}
	return defaultValue
}

// getEnvBool retrieves an environment variable as a bool or returns a default value.
// Accepts "true"/"1"/"yes" (case-insensitive) as truthy, anything else as false.
func getEnvBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		b, err := strconv.ParseBool(value)
		if err == nil {
			return b
		}
	}
	return defaultValue
}

// getEnvFloat64 retrieves an environment variable as a float64 or returns a default value.
func getEnvFloat64(key string, defaultValue float64) float64 {
	if value := os.Getenv(key); value != "" {
		f, err := strconv.ParseFloat(value, 64)
		if err == nil {
			return f
		}
	}
	return defaultValue
}
