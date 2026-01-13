package config

import (
	"fmt"
	"os"
)

// Config holds application configuration
type Config struct {
	// Server
	Port              string
	Environment       string
	Version           string
	PrimaryRegion     string
	CORSAllowedOrigins string

	// Database
	DatabaseURL string

	// Redis
	RedisURL string

	// RabbitMQ
	RabbitMQURL string

	// Clerk
	ClerkSecretKey      string
	ClerkPublishableKey string

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
		Port:              getEnv("PORT", "8080"),
		Environment:       getEnv("ENVIRONMENT", "development"),
		Version:           getEnv("VERSION", "0.1.0"),
		PrimaryRegion:     getEnv("PRIMARY_REGION", "iad"),
		CORSAllowedOrigins: getEnv("CORS_ALLOWED_ORIGINS", "*"),

		// Database
		DatabaseURL: getEnv("DATABASE_URL", ""),

		// Redis
		RedisURL: getEnv("REDIS_URL", "redis://localhost:6379"),

		// RabbitMQ
		RabbitMQURL: getEnv("RABBITMQ_URL", "amqp://localhost:5672/"),

		// Clerk
		ClerkSecretKey:      getEnv("CLERK_SECRET_KEY", ""),
		ClerkPublishableKey: getEnv("CLERK_PUBLISHABLE_KEY", ""),

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
	if cfg.Environment == "production" {
		if err := cfg.validate(); err != nil {
			return nil, err
		}
	}

	return cfg, nil
}

// validate checks that required configuration is present
func (c *Config) validate() error {
	if c.DatabaseURL == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}
	if c.ClerkSecretKey == "" {
		return fmt.Errorf("CLERK_SECRET_KEY is required")
	}
	return nil
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
