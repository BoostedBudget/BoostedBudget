package config

import (
	"errors"
	"fmt"
	"os"
	"time"
)

// Config holds application configuration
type Config struct {
	// Server
	Port               string
	Environment        string
	Version            string
	PrimaryRegion      string
	CORSAllowedOrigins string

	// HTTP server timeouts and limits
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	IdleTimeout  time.Duration
	MaxBodySize  string

	// Rate limiting
	RateLimitIP   int // per-IP requests per minute
	RateLimitAuth int // per-IP requests per minute for sensitive endpoints

	// Database
	DatabaseURL string

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

	// Frontend URL (for cancellation links)
	FrontendURL string
}

// Load loads configuration from environment variables
func Load() (*Config, error) {
	cfg := &Config{
		// Server defaults
		Port:               getEnv("PORT", "8080"),
		Environment:        getEnv("ENVIRONMENT", "development"),
		Version:            getEnv("VERSION", "0.1.0"),
		PrimaryRegion:      getEnv("PRIMARY_REGION", "iad"),
		CORSAllowedOrigins: getEnv("CORS_ALLOWED_ORIGINS", "*"),

		// HTTP server timeouts and limits
		ReadTimeout:  getEnvAsDuration("SERVER_READ_TIMEOUT", 30*time.Second),
		WriteTimeout: getEnvAsDuration("SERVER_WRITE_TIMEOUT", 30*time.Second),
		IdleTimeout:  getEnvAsDuration("SERVER_IDLE_TIMEOUT", 120*time.Second),
		MaxBodySize:  getEnv("MAX_BODY_SIZE", "1M"),

		// Rate limiting
		RateLimitIP:   getEnvAsInt("RATE_LIMIT_IP", 100),
		RateLimitAuth: getEnvAsInt("RATE_LIMIT_AUTH", 10),

		// Database
		DatabaseURL: getEnv("DATABASE_URL", ""),

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

		// Frontend URL
		FrontendURL: getEnv("FRONTEND_URL", "http://localhost:5173"),
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

	return errors.Join(errs...)
}

// getEnv retrieves an environment variable or returns a default value
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
