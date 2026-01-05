package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"time"

	"github.com/danielgtaylor/huma/v2/adapters/humaecho"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	echomiddleware "github.com/labstack/echo/v4/middleware"
	"github.com/maxnussbaum/BoostedBudget/internal/config"
	"github.com/maxnussbaum/BoostedBudget/internal/database"
	humaconfig "github.com/maxnussbaum/BoostedBudget/internal/huma"
	humamiddleware "github.com/maxnussbaum/BoostedBudget/internal/huma/middleware"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/operations"
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

	// Initialize database connection pool (optional for local dev)
	var db *pgxpool.Pool
	if cfg.DatabaseURL != "" {
		db, err = database.NewPoolFromURL(context.Background(), cfg.DatabaseURL)
		if err != nil {
			log.Fatal("Failed to connect to database:", err)
		}
		defer db.Close()

		log.Println("✅ Database connection pool established (pgx v5)")
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

	// Initialize Huma API wrapping Echo for OpenAPI generation
	// Huma provides automatic OpenAPI 3.1 spec generation from Go code
	humaAPI := humaecho.New(e, humaconfig.NewAPIConfig(cfg.Version, cfg.Environment))
	log.Println("✅ Huma API initialized - OpenAPI at /openapi.json, Docs at /docs")

	// Health check handler (no auth required) - Huma operation
	healthHandler := operations.NewHealthHandler(
		createDBCheck(),
		createRedisCheck(),
		createClerkCheck(),
		cfg.Version,
	)
	operations.RegisterHealthRoutes(humaAPI, healthHandler)

	// Metrics endpoint (Prometheus format) - Huma operation
	humaMetricsHandler := operations.NewMetricsHandler()
	operations.RegisterMetricsRoutes(humaAPI, humaMetricsHandler)
	log.Println("✅ Huma metrics route registered")

	// Load tier configuration (used by both public and authenticated endpoints)
	tierConfig, err := config.LoadTierConfig()
	if err != nil {
		log.Printf("⚠️  Failed to load tier config: %v", err)
		log.Fatal("Tier configuration is required")
	}
	log.Println("✅ Tier configuration loaded")

	// Subscription tiers endpoint (public - needed for pricing page) - Huma operation
	subscriptionTiersHandler := operations.NewSubscriptionTiersHandler(tierConfig)
	operations.RegisterSubscriptionTiersRoutes(humaAPI, subscriptionTiersHandler)

	// Default dashboard layout endpoint migrated to Huma (see humaDashboardHandler) - Issue #559

	// Declare plaidService at package scope for use in both authenticated routes and webhook endpoint
	var plaidService services.PlaidService
	// Declare accountService at package scope for Huma handler registration
	var accountService services.AccountService

	// Authenticated routes (requires database and Clerk)
	// Declare deletion worker at package scope for graceful shutdown
	var deletionWorker *workers.DeletionWorker

	// Declare webhook processor at package scope for graceful shutdown
	var webhookProcessor *workers.WebhookProcessor

	// Issue #37: Declare rule scheduler manager at package scope for graceful shutdown
	var ruleSchedulerManager *workers.RuleSchedulerManager

	if db != nil && cfg.ClerkSecretKey != "" {
		// Create repositories
		profileRepo := repository.NewProfileRepository(db)
		privacyRepo := repository.NewPrivacyRepository(db)
		linkedItemRepo := repository.NewLinkedItemRepository(db)
		accountRepo := repository.NewAccountRepository(db)
		transactionRepo := repository.NewTransactionRepository(db)
		webhookRepo := repository.NewWebhookRepository(db)
		identityRepo := repository.NewUserIdentityRepository(db) // Maps Clerk IDs to user UUIDs
		notificationRepo := repository.NewNotificationRepository(db) // Issue #257: Notification storage
		syncAuditRepo := repository.NewSyncAuditRepository(db) // Issue #242: Monthly sync limits
		balanceSnapshotRepo := repository.NewBalanceSnapshotRepository(db) // Issue #353: Balance tracking
		userTagRepo := repository.NewUserTagRepository(db) // Issue #345: User tags
		customCategoryRepo := repository.NewCustomCategoryRepository(db) // Issue #345: Custom categories
		categorizationRuleRepo := repository.NewCategorizationRuleRepository(db) // Issue #346: Categorization rules
		analyticsRepo := repository.NewAnalyticsRepository(db) // Issue #354: Cash flow analytics
		budgetRepo := repository.NewBudgetRepository(db) // Issue #28: Budget management
		savingsGoalRepo := repository.NewSavingsGoalRepository(db) // Issue #28: Savings goals
		dashboardPrefsRepo := repository.NewDashboardPreferencesRepository(db) // Issue #354: Dashboard preferences
		transferSuggestionRepo := repository.NewTransferSuggestionRepository(db) // Issue #36: Transfer suggestions
		automationRuleRepo := repository.NewAutomationRulesRepository(db) // Issue #37: Automation rules

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
		// Issue #354: Cash flow analytics and dashboard preferences services
		analyticsService := services.NewAnalyticsService(analyticsRepo)
		// Issue #474: Load dashboard layout config for preferences service
		// This ensures reset-to-default uses YAML config version, not hardcoded version 2
		dashboardLayoutConfig, err := config.LoadDashboardLayoutConfig()
		if err != nil {
			log.Fatal("Failed to load dashboard layout config:", err)
		}
		dashboardPrefsService := services.NewDashboardPreferencesService(dashboardPrefsRepo, dashboardLayoutConfig)
		// Issue #28: Budget and savings goal services
		budgetLogger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
		budgetService := services.NewBudgetService(budgetRepo, customCategoryRepo, transactionRepo, budgetLogger)
		savingsGoalService := services.NewSavingsGoalService(savingsGoalRepo, accountRepo, budgetLogger)

		// Issue #494: Budget Sync Service for Envelope Budgeting
		// Recalculates budget income and category spent amounts after transaction sync
		budgetIncomeService := services.NewBudgetIncomeService(transactionRepo, budgetLogger)
		budgetCategoryMatchingService := services.NewBudgetCategoryMatchingService(transactionRepo)
		budgetSyncService := services.NewBudgetSyncService(
			budgetRepo,
			budgetIncomeService,
			budgetCategoryMatchingService,
			budgetLogger,
		)

		// Issue #37: Automation Rule Service and Rule Scheduler
		automationRuleService := services.NewAutomationRuleService(automationRuleRepo, subscriptionService, profileRepo)
		log.Println("✅ Automation rule service initialized")

		// Issue #37: Rule Execution Service
		ruleExecutionRepo := repository.NewRuleExecutionRepository(db)
		ruleExecutionService := services.NewRuleExecutionService(ruleExecutionRepo, transferSuggestionRepo, automationRuleRepo, nil)
		log.Println("✅ Rule execution service initialized")

		// Issue #37: Rule Scheduler Manager with providers
		ruleSchedulerManager = workers.NewRuleSchedulerManager()
		subscriptionLimitAdapter := workers.NewSubscriptionLimitAdapter()

		// Create and register CronTriggerProvider
		cronProvider := workers.NewCronTriggerProvider(
			automationRuleRepo,
			ruleExecutionService,
			subscriptionLimitAdapter,
			15*time.Minute, // Refresh interval for picking up new/updated rules
		)
		ruleSchedulerManager.RegisterProvider(cronProvider)

		// Create and register ManualTriggerProvider
		manualProvider := workers.NewManualTriggerProvider(
			automationRuleRepo,
			ruleExecutionService,
			subscriptionLimitAdapter,
		)
		ruleSchedulerManager.RegisterProvider(manualProvider)
		log.Println("✅ Rule scheduler manager initialized with cron and manual providers")

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

		// Create deletion services (if token secret configured)
		if cfg.DeletionTokenSecret != "" {
			// Initialize Clerk Backend API service
			clerkService := services.NewClerkService(cfg.ClerkSecretKey)
			log.Println("✅ Clerk Backend API service initialized")

			// Initialize Deletion service (reuses auditLogger and emailService created earlier)
			deletionService := services.NewDeletionService(
				profileRepo,
				emailService,
				clerkService,
				auditLogger,
				cfg.DeletionGracePeriodDays,
				cfg.DeletionTokenSecret,
			)
			log.Println("✅ Deletion service initialized (30-day grace period)")

			// deletionHandler (Echo) no longer needed - migrated to Huma
			_ = deletionService // Used by Huma handler below

			// Register Huma deletion routes (Issue #559: Huma migration)
			humaDeletionHandler := operations.NewDeletionHandler(deletionService)
			operations.RegisterDeletionRoutes(humaAPI, humaDeletionHandler)
			log.Println("✅ Huma deletion routes registered")

			// Create deletion worker with configured interval
			workerInterval, err := time.ParseDuration(cfg.DeletionWorkerInterval)
			if err != nil {
				log.Printf("⚠️  Invalid deletion worker interval '%s', using default 1h", cfg.DeletionWorkerInterval)
				workerInterval = 1 * time.Hour
			}
			deletionWorker = workers.NewDeletionWorker(deletionService, profileRepo, workerInterval)
			log.Printf("✅ Deletion worker initialized (interval: %s)", workerInterval)
		} else {
			log.Println("⚠️  Deletion services disabled (requires DELETION_TOKEN_SECRET)")
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
		// plaidHandler migrated to Huma (see humaPlaidHandler) - Issue #559
		// var plaidHandler *handlers.PlaidHandler
		if cfg.PlaidClientID != "" && cfg.PlaidSecret != "" {
			var err error
			plaidService, err = services.NewPlaidService(cfg)
			if err != nil {
				log.Printf("⚠️  Failed to initialize Plaid service: %v", err)
			} else {
				// Create account service with all dependencies
				accountService = services.NewAccountService(
					plaidService,
					subscriptionService,
					encryptionService,
					linkedItemRepo,
					accountRepo,
					profileRepo,
					db,
				)

				// plaidHandler migrated to Huma - Issue #559
				_ = plaidService   // Silence unused variable (used by Huma handler)
				_ = accountService // Silence unused variable (used by Huma handler)
				log.Println("✅ Plaid service initialized")
				log.Println("✅ Account linking service initialized")

				// Issue #36: Transaction Monitor Service for auto-detecting completed transfers
				transactionMonitorService := services.NewTransactionMonitorService(transferSuggestionRepo)

				// Initialize Transaction Sync Service for webhook processing
				// Issue #256: Transaction Sync Service
				// Issue #242: Monthly sync limits integration
				// Issue #346: Categorization rules integration
				// Issue #494: Budget sync integration
				// Issue #36: Transaction monitor integration
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
					budgetSyncService,
					transactionMonitorService,
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
		// profileHandler migrated to Huma (see humaProfileHandler below) - Issue #559
		_ = profileService // Used by Huma handler below
		// privacyHandler migrated to Huma (see humaPrivacyHandler below) - Issue #559
		_ = privacyService // Used by Huma handler below
		// transactionHandler migrated to Huma (see humaTransactionHandler below)
		// notificationHandler migrated to Huma (see humaNotificationHandler below) - Issue #559
		// syncStatsHandler migrated to Huma (see humaSyncStatsHandler below) - Issue #559
		_ = syncLimitService // Used by Huma handler below
		// dashboardHandler migrated to Huma (see humaDashboardHandler below) - Issue #559
		// tagHandler migrated to Huma (see humaTagHandler below)
		// categoryHandler migrated to Huma (see humaCategoryHandler below)
		// ruleHandler migrated to Huma (see humaRuleHandler below) - Issue #559
		// analyticsHandler migrated to Huma (see humaAnalyticsHandler below) - Issue #559
		// dashboardPrefsHandler migrated to Huma (see humaDashboardPrefsHandler below) - Issue #559
		// budgetHandler migrated to Huma (see humaBudgetHandler below) - Issue #559
		_ = budgetService      // Used by Huma handler below
		_ = budgetSyncService  // Used by Huma handler below
		categoryPreviewService := services.NewCategoryPreviewService(transactionRepo) // Issue #497: Category preview
		// categoryPreviewHandler migrated to Huma (see humaCategoryPreviewHandler below) - Issue #559
		_ = categoryPreviewService // Used by Huma handler below
		// savingsGoalHandler migrated to Huma (see humaSavingsGoalHandler below) - Issue #559
		_ = savingsGoalService // Used by Huma handler below

		// Admin handler dependencies used by Huma admin routes - Issue #559
		// plaidService, linkedItemRepo, and encryptionService are used in the Huma admin handler registration below

		// Create Clerk middleware with identity resolution and auto-registration
		clerkMiddleware := middleware.ClerkMiddleware(
			cfg.ClerkSecretKey,
			identityRepo,         // Resolves Clerk IDs to internal user UUIDs
			registrationService,  // Auto-registers new users on first login
		)

		// Apply Huma Clerk middleware for authenticated Huma routes
		humaClerkMiddleware := humamiddleware.NewClerkMiddleware(
			cfg.ClerkSecretKey,
			identityRepo,
			registrationService,
		)
		humaAPI.UseMiddleware(humaClerkMiddleware)
		log.Println("✅ Huma Clerk middleware applied")

		// Register Huma tag routes (Issue #345: User tag management)
		humaTagHandler := operations.NewTagHandler(userTagService)
		operations.RegisterTagRoutes(humaAPI, humaTagHandler)
		log.Println("✅ Huma tag routes registered")

		// Register Huma transaction routes (Issue #559: Huma migration)
		humaTransactionHandler := operations.NewTransactionHandler(transactionService)
		operations.RegisterTransactionRoutes(humaAPI, humaTransactionHandler)
		log.Println("✅ Huma transaction routes registered")

		// Register Huma category routes (Issue #559: Huma migration)
		humaCategoryHandler := operations.NewCategoryHandler(customCategoryService)
		operations.RegisterCategoryRoutes(humaAPI, humaCategoryHandler)
		log.Println("✅ Huma category routes registered")

		// Register Huma categorization rule routes (Issue #559: Huma migration)
		humaRuleHandler := operations.NewCategorizationRuleHandler(categorizationRuleService)
		operations.RegisterCategorizationRuleRoutes(humaAPI, humaRuleHandler)
		log.Println("✅ Huma categorization rule routes registered")

		// Register Huma notification routes (Issue #559: Huma migration)
		humaNotificationHandler := operations.NewNotificationHandler(notificationRepo)
		operations.RegisterNotificationRoutes(humaAPI, humaNotificationHandler)
		log.Println("✅ Huma notification routes registered")

		// Register Huma dashboard routes (Issue #559: Huma migration)
		humaDashboardHandler := operations.NewDashboardHandler(dashboardService)
		operations.RegisterDashboardRoutes(humaAPI, humaDashboardHandler)
		log.Println("✅ Huma dashboard routes registered")

		// Register Huma analytics routes (Issue #559: Huma migration)
		humaAnalyticsHandler := operations.NewAnalyticsHandler(analyticsService)
		operations.RegisterAnalyticsRoutes(humaAPI, humaAnalyticsHandler)
		log.Println("✅ Huma analytics routes registered")

		// Register Huma dashboard preferences routes (Issue #559: Huma migration)
		humaDashboardPrefsHandler := operations.NewDashboardPreferencesHandler(dashboardPrefsService)
		operations.RegisterDashboardPreferencesRoutes(humaAPI, humaDashboardPrefsHandler)
		log.Println("✅ Huma dashboard preferences routes registered")

		// Register Huma sync stats routes (Issue #559: Huma migration)
		humaSyncStatsHandler := operations.NewSyncStatsHandler(syncLimitService)
		operations.RegisterSyncStatsRoutes(humaAPI, humaSyncStatsHandler)
		log.Println("✅ Huma sync stats routes registered")

		// Register Huma budget routes (Issue #559: Huma migration)
		humaBudgetHandler := operations.NewBudgetHandler(budgetService, budgetSyncService, transactionRepo)
		operations.RegisterBudgetRoutes(humaAPI, humaBudgetHandler)
		log.Println("✅ Huma budget routes registered")

		// Register Huma savings goal routes (Issue #559: Huma migration)
		humaSavingsGoalHandler := operations.NewSavingsGoalHandler(savingsGoalService)
		operations.RegisterSavingsGoalRoutes(humaAPI, humaSavingsGoalHandler)
		log.Println("✅ Huma savings goal routes registered")

		// Register Huma automation rule routes (Issue #37: Rule Scheduler)
		humaAutomationRuleHandler := operations.NewAutomationRuleHandler(automationRuleService, ruleSchedulerManager)
		humaAutomationRuleHandler.SetExecutionService(ruleExecutionService) // Issue #579: Wire execution service for test endpoint
		operations.RegisterAutomationRuleRoutes(humaAPI, humaAutomationRuleHandler)
		log.Println("✅ Huma automation rule routes registered")

		// Register Huma plaid routes (Issue #559: Huma migration)
		if plaidService != nil && accountService != nil {
			humaPlaidHandler := operations.NewPlaidHandler(plaidService, accountService)
			operations.RegisterPlaidRoutes(humaAPI, humaPlaidHandler)
			log.Println("✅ Huma plaid routes registered")
		}

		// Register Huma profile routes (Issue #559: Huma migration)
		humaProfileHandler := operations.NewProfileHandler(profileService)
		operations.RegisterProfileRoutes(humaAPI, humaProfileHandler)
		log.Println("✅ Huma profile routes registered")

		// Register Huma privacy routes (Issue #559: Huma migration)
		humaPrivacyHandler := operations.NewPrivacyHandler(privacyService)
		operations.RegisterPrivacyRoutes(humaAPI, humaPrivacyHandler)
		log.Println("✅ Huma privacy routes registered")

		// Register Huma category preview routes (Issue #559: Huma migration)
		humaCategoryPreviewHandler := operations.NewCategoryPreviewHandler(categoryPreviewService)
		operations.RegisterCategoryPreviewRoutes(humaAPI, humaCategoryPreviewHandler)
		log.Println("✅ Huma category preview routes registered")

		// Register Huma admin routes (Issue #559: Huma migration) - qa only
		// Security: Only registered in qa environment
		if plaidService != nil && encryptionService != nil && cfg.Environment == "qa" {
			humaAdminHandler := operations.NewAdminHandler(plaidService, linkedItemRepo, encryptionService)
			operations.RegisterAdminRoutes(humaAPI, humaAdminHandler)
			log.Println("✅ Huma admin routes registered (qa only)")
		}

		// API v1 routes (Echo - to be migrated to Huma incrementally)
		api := e.Group("/api/v1")
		api.Use(clerkMiddleware)
		// Profile and privacy routes migrated to Huma (see humaProfileHandler and humaPrivacyHandler above)
		// api.GET("/user/profile", profileHandler.GetProfile)
		// api.PUT("/user/profile", profileHandler.UpdateProfile)
		// api.GET("/user/privacy", privacyHandler.GetPrivacySettings)
		// api.PUT("/user/privacy", privacyHandler.UpdatePrivacySettings)

		// Transaction routes migrated to Huma (see humaTransactionHandler above)
		// Tag routes migrated to Huma (see humaTagHandler above)
		// Category routes migrated to Huma (see humaCategoryHandler above)
		// Auto-categorization routes migrated to Huma (see humaRuleHandler above)

		// Notification routes migrated to Huma (see humaNotificationHandler above)

		// Sync statistics route migrated to Huma (see humaSyncStatsHandler above) - Issue #559

		// Dashboard routes migrated to Huma (see humaDashboardHandler above)

		// Analytics routes migrated to Huma (see humaAnalyticsHandler above)

		// Dashboard preferences routes migrated to Huma (see humaDashboardPrefsHandler above)

		// Budget routes migrated to Huma (see humaBudgetHandler above) - Issue #559
		// Category preview route migrated to Huma (see humaCategoryPreviewHandler above) - Issue #559
		// api.POST("/budgets/categories/preview", categoryPreviewHandler.PreviewCategoryMatches) // Issue #497: Category preview

		// Savings Goal routes migrated to Huma (see humaSavingsGoalHandler above) - Issue #559

		// Plaid routes migrated to Huma (see humaPlaidHandler above) - Issue #559
		// Old Echo routes commented out:
		// if plaidHandler != nil {
		// 	api.POST("/plaid/link-token", plaidHandler.CreateLinkToken)
		// 	api.POST("/plaid/exchange-token", plaidHandler.ExchangePublicToken)
		// 	api.GET("/accounts", plaidHandler.GetAccounts)
		// 	api.PATCH("/accounts/reorder", plaidHandler.ReorderAccounts)  // Issue #307: Must be before :id route
		// 	api.PATCH("/accounts/:id", plaidHandler.UpdateAccount)        // Issue #307: Account customization
		// 	api.DELETE("/accounts/:id", plaidHandler.DeleteAccount)
		// 	api.DELETE("/linked-items/:id", plaidHandler.DisconnectInstitution)
		// }

		// Admin routes migrated to Huma (see humaAdminHandler above) - Issue #559
		// Note: Security maintained - routes only registered in qa environment
		// if adminHandler != nil && cfg.Environment == "qa" {
		// 	admin := api.Group("/admin", middleware.AdminMiddleware(cfg))
		// 	admin.POST("/plaid/fire-webhook", adminHandler.FireWebhook)
		// 	log.Println("✅ Admin endpoints enabled (qa only)")
		// }

		// Deletion routes migrated to Huma (see humaDeletionHandler below) - Issue #559
		// Old Echo routes commented out:
		// if deletionHandler != nil {
		// 	api.POST("/user/delete", deletionHandler.RequestDeletion)
		// 	api.GET("/user/delete/cancel/:token", deletionHandler.CancelDeletion)
		// }

		log.Println("✅ Authenticated routes enabled (Clerk + Database)")
	}

	// Plaid webhook endpoint (public - no auth required) - Migrated to Huma (Issue #559)
	// Plaid servers need to POST to this endpoint without authentication
	// Issue #202: Enhanced with JWT signature verification and database storage
	// Issue #256: Webhook repository created in auth block above for webhook processor
	if db != nil && plaidService != nil {
		// Webhook repository for audit trail
		webhookRepoForHandler := repository.NewWebhookRepository(db)

		// Initialize webhook service for JWT signature verification
		webhookService := services.NewWebhookService(plaidService.GetClient())

		// Register Huma plaid webhook route (Issue #559: Huma migration)
		humaPlaidWebhookHandler := operations.NewPlaidWebhookHandler(webhookService, webhookRepoForHandler)
		operations.RegisterPlaidWebhookRoutes(humaAPI, humaPlaidWebhookHandler)

		log.Println("✅ Huma Plaid webhook route registered at POST /webhooks/plaid")
		log.Println("✅ Webhook signature verification enabled (ES256 JWT)")
		log.Println("✅ Webhook audit trail enabled (database storage)")
	} else if db != nil {
		// Fallback: Basic webhook handler without signature verification
		// Used when Plaid credentials are not configured (e.g., local development)
		humaPlaidWebhookHandler := operations.NewPlaidWebhookHandler(nil, repository.NewWebhookRepository(db))
		operations.RegisterPlaidWebhookRoutes(humaAPI, humaPlaidWebhookHandler)
		log.Println("✅ Huma Plaid webhook route registered at POST /webhooks/plaid (verification disabled)")
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

	// Issue #37: Start rule scheduler manager
	if ruleSchedulerManager != nil {
		if workerCtx == nil {
			workerCtx, workerCancel = context.WithCancel(context.Background())
		}
		go func() {
			if err := ruleSchedulerManager.Start(workerCtx); err != nil {
				log.Printf("⚠️  Failed to start rule scheduler: %v", err)
			}
		}()
		log.Println("✅ Rule scheduler manager started")
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

	// Issue #37: Stop rule scheduler manager first (graceful shutdown)
	if ruleSchedulerManager != nil {
		log.Println("Stopping rule scheduler manager...")
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := ruleSchedulerManager.Stop(stopCtx); err != nil {
			log.Printf("⚠️  Error stopping rule scheduler: %v", err)
		}
		stopCancel()
		log.Println("Rule scheduler manager stopped")
	}

	// Stop background workers (if running)
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

// createClerkCheck creates a Clerk health check function
// TODO: Replace with real Clerk connection check
func createClerkCheck() func() error {
	return func() error {
		// For now, always return healthy
		// Will be replaced with actual Clerk endpoint check when we add Clerk
		return nil
	}
}
