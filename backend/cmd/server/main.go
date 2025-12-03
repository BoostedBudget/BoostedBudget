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
	"github.com/maxnussbaum/BoostedBudget/internal/workers"
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
	e.Use(echomiddleware.CORSWithConfig(middleware.GetCORSConfig(cfg.CORSAllowedOrigins))) // 5. CORS handling with configured origins

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

	// Declare plaidService at package scope for use in both authenticated routes and webhook endpoint
	var plaidService services.PlaidService

	// Authenticated routes (requires database and Auth0)
	// Declare deletion components at package scope for graceful shutdown
	var deletionHandler *handlers.DeletionHandler
	var deletionWorker *workers.DeletionWorker

	// Declare webhook processor at package scope for graceful shutdown
	var webhookProcessor *workers.WebhookProcessor

	if db != nil && cfg.Auth0Domain != "" && cfg.Auth0Audience != "" {
		// Create repositories
		profileRepo := repository.NewProfileRepository(db)
		privacyRepo := repository.NewPrivacyRepository(db)
		linkedItemRepo := repository.NewLinkedItemRepository(db)
		accountRepo := repository.NewAccountRepository(db)
		transactionRepo := repository.NewTransactionRepository(db)
		webhookRepo := repository.NewWebhookRepository(db)
		identityRepo := repository.NewUserIdentityRepository(db) // Maps Auth0 IDs to user UUIDs
		notificationRepo := repository.NewNotificationRepository(db) // Issue #257: Notification storage
		syncAuditRepo := repository.NewSyncAuditRepository(db) // Issue #242: Monthly sync limits
		balanceSnapshotRepo := repository.NewBalanceSnapshotRepository(db) // Issue #353: Balance tracking
		userTagRepo := repository.NewUserTagRepository(db) // Issue #345: User tags
		customCategoryRepo := repository.NewCustomCategoryRepository(db) // Issue #345: Custom categories
		categorizationRuleRepo := repository.NewCategorizationRuleRepository(db) // Issue #346: Categorization rules

		// Create mock audit logger (will be replaced with real implementation later)
		auditLogger := &mockAuditLogger{}

		// Create services
		profileService := services.NewProfileService(profileRepo)
		privacyService := services.NewPrivacyService(privacyRepo)
		transactionService := services.NewTransactionService(transactionRepo)
		registrationService := services.NewRegistrationService(profileRepo, identityRepo, privacyRepo, auditLogger, db)
		// Note: Passing nil for override repo - not yet implemented
		subscriptionService := services.NewSubscriptionService(tierConfig, nil)
		// Issue #242: Monthly sync limits service
		syncLimitService := services.NewSyncLimitService(syncAuditRepo, profileRepo, subscriptionService)
		// Issue #353: Dashboard service for balance tracking
		dashboardService := services.NewDashboardService(balanceSnapshotRepo, accountRepo)
		// Issue #345: Tag and category services
		userTagService := services.NewUserTagService(userTagRepo, subscriptionService, profileRepo)
		customCategoryService := services.NewCustomCategoryService(customCategoryRepo, subscriptionService, profileRepo)
		// Issue #346: Categorization rules services
		categorizationRuleService := services.NewCategorizationRuleService(categorizationRuleRepo, subscriptionService, profileRepo)
		ruleApplicationService := services.NewRuleApplicationService(categorizationRuleRepo)

		// Initialize Email service (needed by deletion service and item error service)
		// Optional - uses no-op mock if not configured
		var emailService services.EmailService
		if cfg.SendGridAPIKey != "" && cfg.SendGridFromEmail != "" {
			emailService = services.NewEmailService(cfg.SendGridAPIKey, cfg.SendGridFromEmail, cfg.FrontendURL)
			log.Println("✅ Email service (SendGrid) initialized")
		} else {
			emailService = services.NewEmailServiceMock("", "", cfg.FrontendURL)
			log.Println("⚠️  Email service disabled - cancellation links shown in UI instead (see GitHub issue for SendGrid integration)")
		}

		// Create deletion services (if Auth0 Management API configured)
		if cfg.Auth0ManagementClientID != "" && cfg.Auth0ManagementClientSecret != "" && cfg.DeletionTokenSecret != "" {
			// Initialize Auth0 Management API service
			auth0Service := services.NewAuth0Service(
				cfg.Auth0Domain,
				cfg.Auth0ManagementClientID,
				cfg.Auth0ManagementClientSecret,
			)
			log.Println("✅ Auth0 Management API service initialized")

			// Initialize Deletion service (reuses auditLogger and emailService created earlier)
			deletionService := services.NewDeletionService(
				profileRepo,
				emailService,
				auth0Service,
				auditLogger,
				cfg.DeletionGracePeriodDays,
				cfg.DeletionTokenSecret,
			)
			log.Println("✅ Deletion service initialized (30-day grace period)")

			// Create deletion handler
			deletionHandler = handlers.NewDeletionHandler(deletionService)

			// Create deletion worker with configured interval
			workerInterval, err := time.ParseDuration(cfg.DeletionWorkerInterval)
			if err != nil {
				log.Printf("⚠️  Invalid deletion worker interval '%s', using default 1h", cfg.DeletionWorkerInterval)
				workerInterval = 1 * time.Hour
			}
			deletionWorker = workers.NewDeletionWorker(deletionService, profileRepo, workerInterval)
			log.Printf("✅ Deletion worker initialized (interval: %s)", workerInterval)
		} else {
			log.Println("⚠️  Deletion services disabled (requires AUTH0_MANAGEMENT_CLIENT_ID, AUTH0_MANAGEMENT_CLIENT_SECRET, DELETION_TOKEN_SECRET)")
		}

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
			var err error
			plaidService, err = services.NewPlaidService(cfg)
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

				// Initialize Transaction Sync Service for webhook processing
				// Issue #256: Transaction Sync Service
				// Issue #242: Monthly sync limits integration
				// Issue #346: Categorization rules integration
				transactionSyncService := services.NewTransactionSyncService(
					webhookRepo,
					linkedItemRepo,
					transactionRepo,
					accountRepo,
					profileRepo,
					balanceSnapshotRepo,
					plaidService,
					encryptionService,
					syncLimitService,
					ruleApplicationService,
				)

				// Initialize Item Error Service for ITEM webhook processing
				// Issue #257: Item Error Handler - Process ITEM Webhooks and User Notifications
				itemErrorService := services.NewItemErrorService(
					linkedItemRepo,
					notificationRepo,
					plaidService,
					encryptionService,
					emailService,
				)

				// Initialize Webhook Processor background worker
				// Polls for unprocessed TRANSACTIONS and ITEM webhooks every 30 seconds
				// Processes up to 10 webhooks per batch (per type)
				webhookProcessor = workers.NewWebhookProcessor(
					transactionSyncService,
					itemErrorService,
					webhookRepo,
					30*time.Second, // Poll interval
					10,             // Batch size
				)
				log.Println("✅ Transaction sync service initialized")
				log.Println("✅ Item error service initialized")
				log.Println("✅ Webhook processor initialized (interval: 30s, batch: 10, types: TRANSACTIONS+ITEM)")
			}
		}

		// Create handlers
		profileHandler := handlers.NewProfileHandler(profileService)
		privacyHandler := handlers.NewPrivacyHandler(privacyService, profileService)
		transactionHandler := handlers.NewTransactionHandler(transactionService)
		notificationHandler := handlers.NewNotificationHandler(notificationRepo) // Issue #257: Notification API
		syncStatsHandler := handlers.NewSyncStatsHandler(syncLimitService)       // Issue #242: Monthly sync limits API
		dashboardHandler := handlers.NewDashboardHandler(dashboardService)       // Issue #353: Balance tracking dashboard
		tagHandler := handlers.NewTagHandler(userTagService)                      // Issue #345: User tag management
		categoryHandler := handlers.NewCategoryHandler(customCategoryService)     // Issue #345: Custom category management
		ruleHandler := handlers.NewCategorizationRuleHandler(categorizationRuleService) // Issue #346: Categorization rules

		// Create admin handler (Issue #308: Admin API for Manual Webhook Firing)
		// Requires plaidService and encryptionService, so declared here
		var adminHandler *handlers.AdminHandler
		if plaidService != nil && encryptionService != nil {
			adminHandler = handlers.NewAdminHandler(plaidService, linkedItemRepo, encryptionService)
		}

		// Create Auth0 middleware with identity resolution and auto-registration
		auth0Middleware := middleware.Auth0Middleware(
			"https://"+cfg.Auth0Domain+"/",
			cfg.Auth0Audience,
			identityRepo,         // Resolves Auth0 IDs to internal user UUIDs
			registrationService,  // Auto-registers new users on first login
		)

		// API v1 routes
		api := e.Group("/api/v1")
		api.Use(auth0Middleware)
		api.GET("/user/profile", profileHandler.GetProfile)
		api.PUT("/user/profile", profileHandler.UpdateProfile)
		api.GET("/user/privacy", privacyHandler.GetPrivacySettings)
		api.PUT("/user/privacy", privacyHandler.UpdatePrivacySettings)
		api.GET("/transactions", transactionHandler.GetTransactions)
		api.GET("/transactions/:id", transactionHandler.GetTransactionByID)
		api.PUT("/transactions/:id/tags", transactionHandler.UpdateTransactionTags)
		api.PUT("/transactions/:id/category", transactionHandler.UpdateTransactionCategory)

		// Tag routes (Issue #345: User tag management)
		api.POST("/tags", tagHandler.CreateTag)
		api.GET("/tags", tagHandler.GetUserTags)
		api.GET("/tags/:id", tagHandler.GetTagByID)
		api.PUT("/tags/:id", tagHandler.UpdateTag)
		api.DELETE("/tags/:id", tagHandler.DeleteTag)

		// Category routes (Issue #345: Custom category management)
		api.POST("/categories", categoryHandler.CreateCategory)
		api.GET("/categories", categoryHandler.GetUserCategories)
		api.GET("/categories/plaid", transactionHandler.GetPlaidCategories) // Issue #397: Plaid categories for rule editor
		api.GET("/categories/:id", categoryHandler.GetCategoryByID)
		api.PUT("/categories/:id", categoryHandler.UpdateCategory)
		api.DELETE("/categories/:id", categoryHandler.DeleteCategory)

		// Auto-categorization rule routes (Issue #346: Categorization rules)
		api.POST("/auto-categorization", ruleHandler.CreateRule)
		api.GET("/auto-categorization", ruleHandler.GetUserRules)
		api.GET("/auto-categorization/:id", ruleHandler.GetRuleByID)
		api.PUT("/auto-categorization/:id", ruleHandler.UpdateRule)
		api.DELETE("/auto-categorization/:id", ruleHandler.DeleteRule)
		api.POST("/auto-categorization/:id/toggle", ruleHandler.ToggleRule)

		// Notification routes (Issue #257: User Notifications)
		api.GET("/notifications", notificationHandler.GetNotifications)
		api.PATCH("/notifications/:id/read", notificationHandler.MarkNotificationAsRead)
		api.GET("/notifications/unread/count", notificationHandler.GetUnreadCount)

		// Sync statistics route (Issue #242: Monthly sync limits)
		api.GET("/sync-stats", syncStatsHandler.GetSyncStats)

		// Dashboard routes (Issue #353: Balance tracking dashboard)
		api.GET("/dashboard/balance-summary", dashboardHandler.GetBalanceSummary)
		api.GET("/dashboard/balance-history", dashboardHandler.GetBalanceHistory)
		api.GET("/dashboard/account-types", dashboardHandler.GetAccountTypes)

		// Plaid routes (if enabled)
		if plaidHandler != nil {
			api.POST("/plaid/link-token", plaidHandler.CreateLinkToken)
			api.POST("/plaid/exchange-token", plaidHandler.ExchangePublicToken)
			api.GET("/accounts", plaidHandler.GetAccounts)
			api.DELETE("/accounts/:id", plaidHandler.DeleteAccount)
			api.DELETE("/linked-items/:id", plaidHandler.DisconnectInstitution)
		}

		// Admin routes (Issue #308: staging-only admin endpoints)
		// Triple-layer security:
		//   1. Route registration only in staging (here)
		//   2. AdminMiddleware returns 404 in production
		//   3. Service-level validations (e.g., sandbox-only webhook firing)
		// Future (Issue #309): Add role-based access control requiring "admin" role
		if adminHandler != nil && cfg.Environment == "staging" {
			admin := api.Group("/admin", middleware.AdminMiddleware(cfg))
			admin.POST("/plaid/fire-webhook", adminHandler.FireWebhook)
			log.Println("✅ Admin endpoints enabled (staging only)")
		}

		// Deletion routes (if enabled)
		if deletionHandler != nil {
			api.POST("/user/delete", deletionHandler.RequestDeletion)
			api.GET("/user/delete/cancel/:token", deletionHandler.CancelDeletion)
		}

		log.Println("✅ Authenticated routes enabled (Auth0 + Database)")
	}

	// Plaid webhook endpoint (public - no auth required)
	// Plaid servers need to POST to this endpoint without authentication
	// Issue #202: Enhanced with JWT signature verification and database storage
	// Issue #256: Webhook repository created in auth block above for webhook processor
	if db != nil && plaidService != nil {
		// Webhook repository already created in auth block (used by webhook processor)
		// Re-create if not available (shouldn't happen since plaidService requires auth block)
		var webhookRepoForHandler repository.WebhookRepository
		if webhookProcessor != nil {
			// Webhook repo already created in auth block - reuse from there
			// Create a new instance to avoid coupling
			webhookRepoForHandler = repository.NewWebhookRepository(db)
		} else {
			// Fallback: create webhook repo if processor not initialized
			webhookRepoForHandler = repository.NewWebhookRepository(db)
		}

		// Initialize webhook service for JWT signature verification
		webhookService := services.NewWebhookService(plaidService.GetClient())

		// Create webhook handler with full dependencies
		webhookHandler := handlers.NewPlaidWebhookHandlerWithDeps(webhookService, webhookRepoForHandler)
		e.POST("/webhooks/plaid", webhookHandler.HandlePlaidWebhook)

		log.Println("✅ Plaid webhook endpoint registered at POST /webhooks/plaid")
		log.Println("✅ Webhook signature verification enabled (ES256 JWT)")
		log.Println("✅ Webhook audit trail enabled (database storage)")
	} else if db != nil {
		// Fallback: Basic webhook handler without signature verification
		// Used when Plaid credentials are not configured (e.g., local development)
		webhookHandler := handlers.NewPlaidWebhookHandler()
		e.POST("/webhooks/plaid", webhookHandler.HandlePlaidWebhook)
		log.Println("✅ Plaid webhook endpoint registered at POST /webhooks/plaid (verification disabled)")
		log.Println("⚠️  Webhook signature verification disabled (Plaid credentials not configured)")
	} else {
		log.Println("⚠️  Webhook endpoint disabled (requires DATABASE_URL)")
	}

	// Start background workers (if enabled)
	var workerCtx context.Context
	var workerCancel context.CancelFunc

	// Start deletion worker
	if deletionWorker != nil {
		if workerCtx == nil {
			workerCtx, workerCancel = context.WithCancel(context.Background())
		}
		go deletionWorker.Start(workerCtx)
		log.Println("✅ Deletion worker started")
	}

	// Start webhook processor
	if webhookProcessor != nil {
		if workerCtx == nil {
			workerCtx, workerCancel = context.WithCancel(context.Background())
		}
		go webhookProcessor.Start(workerCtx)
		log.Println("✅ Webhook processor started")
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

	// Stop background workers first (if running)
	if workerCancel != nil {
		log.Println("Stopping background workers...")
		workerCancel()
	}

	// Shutdown HTTP server
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := e.Shutdown(ctx); err != nil {
		log.Fatal("Server forced to shutdown:", err)
	}

	log.Println("Server stopped gracefully")
}

// mockAuditLogger is a placeholder audit logger for deletion operations
// TODO: Replace with real audit logging implementation
type mockAuditLogger struct{}

func (m *mockAuditLogger) Log(ctx context.Context, action, resourceType, resourceID string) error {
	log.Printf("AUDIT: %s - %s - %s", action, resourceType, resourceID)
	return nil
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
