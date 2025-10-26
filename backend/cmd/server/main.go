package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"os/signal"
	"time"

	_ "github.com/lib/pq"
	"github.com/labstack/echo/v4"
	echomiddleware "github.com/labstack/echo/v4/middleware"
	"github.com/maxnussbaum/BoostedBudget/internal/config"
	"github.com/maxnussbaum/BoostedBudget/internal/handlers"
	"github.com/maxnussbaum/BoostedBudget/internal/middleware"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
)

func main() {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatal("Failed to load config:", err)
	}

	// Initialize database connection (optional for local dev)
	var db *sql.DB
	if cfg.DatabaseURL != "" {
		db, err = sql.Open("postgres", cfg.DatabaseURL)
		if err != nil {
			log.Fatal("Failed to connect to database:", err)
		}
		defer db.Close()

		// Ping database to verify connection
		if err := db.Ping(); err != nil {
			log.Fatal("Failed to ping database:", err)
		}
		log.Println("✅ Database connection established")
	} else {
		log.Println("⚠️  Running without database (set DATABASE_URL to enable)")
	}

	// Create Echo instance
	e := echo.New()

	// Register request validator (required for c.Validate() in handlers)
	e.Validator = middleware.NewValidator()

	// Middleware (order matters!)
	e.Use(middleware.RequestID())                      // 1. Generate request IDs first
	e.Use(middleware.StructuredLogging(os.Stdout))    // 2. Structured JSON logging
	e.Use(middleware.MetricsMiddleware())             // 3. Prometheus metrics
	e.Use(echomiddleware.Recover())                    // 4. Panic recovery
	e.Use(echomiddleware.CORS())                       // 5. CORS handling

	// Health check handler (no auth required)
	healthHandler := &handlers.HealthHandler{
		DBCheck:    createDBCheck(),
		RedisCheck: createRedisCheck(),
		Auth0Check: createAuth0Check(),
		Version:    cfg.Version,
	}
	e.GET("/health", healthHandler.GetHealth)

	// Metrics endpoint (Prometheus format)
	metricsHandler := &handlers.MetricsHandler{}
	e.GET("/metrics", metricsHandler.GetMetrics)

	// Load tier configuration (used by both public and authenticated endpoints)
	tierConfig, err := config.LoadTierConfig()
	if err != nil {
		log.Printf("⚠️  Failed to load tier config: %v", err)
		log.Fatal("Tier configuration is required")
	}
	log.Println("✅ Tier configuration loaded")

	// Subscription tiers endpoint (public - needed for pricing page)
	subscriptionTiersHandler := handlers.NewSubscriptionTiersHandler(tierConfig)
	e.GET("/api/v1/subscription/tiers", subscriptionTiersHandler.GetSubscriptionTiers)

	// Authenticated routes (requires database and Auth0)
	if db != nil && cfg.Auth0Domain != "" && cfg.Auth0Audience != "" {
		// Create repositories
		profileRepo := repository.NewProfileRepository(db)
		privacyRepo := repository.NewPrivacyRepository(db)
		linkedItemRepo := repository.NewLinkedItemRepository(db)
		accountRepo := repository.NewAccountRepository(db)
		transactionRepo := repository.NewTransactionRepository(db)
		identityRepo := repository.NewUserIdentityRepository(db) // Maps Auth0 IDs to user UUIDs

		// Create services
		profileService := services.NewProfileService(profileRepo)
		privacyService := services.NewPrivacyService(privacyRepo)
		transactionService := services.NewTransactionService(transactionRepo)
		// Note: Passing nil for override repo - not yet implemented
		subscriptionService := services.NewSubscriptionService(tierConfig, nil)

		// Create encryption service (if configured)
		var encryptionService services.EncryptionService
		if cfg.EncryptionKey != "" {
			encryptionService, err = services.NewEncryptionService(cfg)
			if err != nil {
				log.Printf("⚠️  Failed to initialize encryption service: %v", err)
				log.Fatal("Encryption service is required for account linking")
			}
			log.Println("✅ Encryption service initialized")
		} else {
			log.Fatal("ENCRYPTION_KEY environment variable is required")
		}

		// Create Plaid service and account linking (if configured)
		var plaidHandler *handlers.PlaidHandler
		if cfg.PlaidClientID != "" && cfg.PlaidSecret != "" {
			plaidService, err := services.NewPlaidService(cfg)
			if err != nil {
				log.Printf("⚠️  Failed to initialize Plaid service: %v", err)
			} else {
				// Create account service with all dependencies
				accountService := services.NewAccountService(
					plaidService,
					subscriptionService,
					encryptionService,
					linkedItemRepo,
					accountRepo,
					profileRepo,
					db,
				)

				plaidHandler = handlers.NewPlaidHandler(plaidService, accountService)
				log.Println("✅ Plaid service initialized")
				log.Println("✅ Account linking service initialized")
			}
		}

		// Create handlers
		profileHandler := handlers.NewProfileHandler(profileService)
		privacyHandler := handlers.NewPrivacyHandler(privacyService, profileService)
		transactionHandler := handlers.NewTransactionHandler(transactionService)

		// Create Auth0 middleware with identity resolution
		auth0Middleware := middleware.Auth0Middleware(
			"https://"+cfg.Auth0Domain+"/",
			cfg.Auth0Audience,
			identityRepo, // Resolves Auth0 IDs to internal user UUIDs
		)

		// API v1 routes
		api := e.Group("/api/v1")
		api.Use(auth0Middleware)
		api.GET("/user/profile", profileHandler.GetProfile)
		api.PUT("/user/profile", profileHandler.UpdateProfile)
		api.GET("/user/privacy", privacyHandler.GetPrivacySettings)
		api.PUT("/user/privacy", privacyHandler.UpdatePrivacySettings)
		api.GET("/transactions", transactionHandler.GetTransactions)

		// Plaid routes (if enabled)
		if plaidHandler != nil {
			api.POST("/plaid/link-token", plaidHandler.CreateLinkToken)
			api.POST("/plaid/exchange-token", plaidHandler.ExchangePublicToken)
			api.GET("/accounts", plaidHandler.GetAccounts)
		}

		log.Println("✅ Authenticated routes enabled (Auth0 + Database)")
	}

	// Plaid webhook endpoint (public - no auth required)
	// Plaid servers need to POST to this endpoint without authentication
	// Note: In production, should verify webhook signatures for security
	// See: https://plaid.com/docs/api/webhooks/#webhook-verification
	if db != nil {
		webhookHandler := handlers.NewPlaidWebhookHandler()
		e.POST("/webhooks/plaid", webhookHandler.HandlePlaidWebhook)
		log.Println("✅ Plaid webhook endpoint registered at POST /webhooks/plaid")
	} else {
		log.Println("⚠️  Authenticated routes disabled (requires DATABASE_URL, AUTH0_DOMAIN, AUTH0_AUDIENCE)")
	}

	// Start server
	// Bind to 0.0.0.0 to be accessible from outside the container (required for Fly.io)
	address := fmt.Sprintf("0.0.0.0:%s", cfg.Port)
	log.Printf("🚀 Starting BoostedBudget API on %s (region: %s)", address, cfg.PrimaryRegion)

	// Graceful shutdown
	go func() {
		if err := e.Start(address); err != nil {
			log.Printf("Server stopped: %v", err)
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt)
	<-quit

	log.Println("Shutting down server...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := e.Shutdown(ctx); err != nil {
		log.Fatal("Server forced to shutdown:", err)
	}

	log.Println("Server stopped gracefully")
}

// createDBCheck creates a database health check function
// TODO: Replace with real database connection check
func createDBCheck() func() error {
	return func() error {
		// For now, always return healthy
		// Will be replaced with actual DB ping when we add database
		return nil
	}
}

// createRedisCheck creates a Redis health check function
// TODO: Replace with real Redis connection check
func createRedisCheck() func() error {
	return func() error {
		// For now, always return healthy
		// Will be replaced with actual Redis ping when we add Redis
		return nil
	}
}

// createAuth0Check creates an Auth0 health check function
// TODO: Replace with real Auth0 connection check
func createAuth0Check() func() error {
	return func() error {
		// For now, always return healthy
		// Will be replaced with actual Auth0 JWKS endpoint check when we add Auth0
		return nil
	}
}
