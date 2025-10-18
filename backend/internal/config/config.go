package config

import (
	"fmt"
	"os"
)

// Config holds application configuration
type Config struct {
	// Server
	Port         string
	Environment  string
	Version      string
	PrimaryRegion string

	// Database
	DatabaseURL string

	// Redis
	RedisURL string

	// RabbitMQ
	RabbitMQURL string

	// Auth0
	Auth0Domain   string
	Auth0Audience string
	Auth0ClientID string
	Auth0ClientSecret string

	// Plaid
	PlaidClientID string
	PlaidSecret   string
	PlaidEnv      string

	// Stripe
	StripeSecretKey      string
	StripePublishableKey string
	StripeWebhookSecret  string
}

// Load loads configuration from environment variables
func Load() (*Config, error) {
	cfg := &Config{
		// Server defaults
		Port:         getEnv("PORT", "8080"),
		Environment:  getEnv("ENVIRONMENT", "development"),
		Version:      getEnv("VERSION", "0.1.0"),
		PrimaryRegion: getEnv("PRIMARY_REGION", "us-east-1"),

		// Database
		DatabaseURL: getEnv("DATABASE_URL", ""),

		// Redis
		RedisURL: getEnv("REDIS_URL", "redis://localhost:6379"),

		// RabbitMQ
		RabbitMQURL: getEnv("RABBITMQ_URL", "amqp://localhost:5672/"),

		// Auth0
		Auth0Domain:   getEnv("AUTH0_DOMAIN", ""),
		Auth0Audience: getEnv("AUTH0_AUDIENCE", ""),
		Auth0ClientID: getEnv("AUTH0_CLIENT_ID", ""),
		Auth0ClientSecret: getEnv("AUTH0_CLIENT_SECRET", ""),

		// Plaid
		PlaidClientID: getEnv("PLAID_CLIENT_ID", ""),
		PlaidSecret:   getEnv("PLAID_SECRET", ""),
		PlaidEnv:      getEnv("PLAID_ENV", "sandbox"),

		// Stripe
		StripeSecretKey:      getEnv("STRIPE_SECRET_KEY", ""),
		StripePublishableKey: getEnv("STRIPE_PUBLISHABLE_KEY", ""),
		StripeWebhookSecret:  getEnv("STRIPE_WEBHOOK_SECRET", ""),
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
	if c.Auth0Domain == "" {
		return fmt.Errorf("AUTH0_DOMAIN is required")
	}
	if c.Auth0Audience == "" {
		return fmt.Errorf("AUTH0_AUDIENCE is required")
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
