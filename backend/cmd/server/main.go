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
	sentryecho "github.com/getsentry/sentry-go/echo"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	echomiddleware "github.com/labstack/echo/v4/middleware"
	"go.opentelemetry.io/contrib/instrumentation/github.com/labstack/echo/otelecho"
	"github.com/maxnussbaum/BoostedBudget/internal/config"
	appcrypto "github.com/maxnussbaum/BoostedBudget/internal/crypto"
	"github.com/maxnussbaum/BoostedBudget/internal/database"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	humaconfig "github.com/maxnussbaum/BoostedBudget/internal/huma"
	humamiddleware "github.com/maxnussbaum/BoostedBudget/internal/huma/middleware"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/operations"
	phclient "github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"
	"github.com/maxnussbaum/BoostedBudget/internal/middleware"
	"github.com/maxnussbaum/BoostedBudget/internal/observability"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/maxnussbaum/BoostedBudget/internal/queue/jobs"
	riverqueue "github.com/maxnussbaum/BoostedBudget/internal/queue/river"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
	"github.com/maxnussbaum/BoostedBudget/internal/workers"
	"github.com/riverqueue/river"
)

func main() {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatal("Failed to load config:", err)
	}

	// Init Sentry (graceful no-op if DSN empty)
	if err := observability.InitSentry(observability.SentryConfig{
		DSN:         cfg.SentryDSN,
		Environment: cfg.Environment,
		Release:     cfg.Version,
		SampleRate:  cfg.SentrySampleRate,
	}); err != nil {
		log.Printf("⚠️  Sentry init failed: %v", err)
	}
	defer observability.FlushSentry(2 * time.Second)

	// Init OTEL tracer (graceful no-op if endpoint empty)
	shutdownTracer, err := observability.InitTracer(observability.OTELConfig{
		Endpoint:    cfg.OTELEndpoint,
		ServiceName: "boostedbudget-api",
		Version:     cfg.Version,
		Environment: cfg.Environment,
		SampleRate:  cfg.OTELSampleRate,
	})
	if err != nil {
		log.Printf("⚠️  OTEL init failed: %v", err)
		shutdownTracer = func(ctx context.Context) error { return nil }
	}
	defer shutdownTracer(context.Background())

	// Create unified logger
	appLogger := observability.NewLogger(observability.LogConfig{
		Level:        cfg.LogLevel,
		Environment:  cfg.Environment,
		ServiceName:  "boostedbudget-api",
		Version:      cfg.Version,
		SentryDSN:    cfg.SentryDSN,
		OTELEndpoint: cfg.OTELEndpoint,
	})
	slog.SetDefault(appLogger)

	// Init PostHog (graceful no-op if API key empty)
	phClient := phclient.NewClient(cfg.PostHogAPIKey, cfg.PostHogHost, appLogger)
	if phClient != nil {
		defer phClient.Close()
		appLogger.Info("PostHog analytics initialized")
	}

	// Log startup configuration summary
	appLogger.Info("Startup config summary",
		"env", cfg.Environment,
		"database", boolStatus(cfg.DatabaseURL != ""),
		"database_max_conns", cfg.DatabaseMaxConns,
		"clerk", boolStatus(cfg.ClerkSecretKey != ""),
		"encryption", boolStatus(cfg.EncryptionKey != ""),
		"plaid", boolStatus(cfg.PlaidClientID != "" && cfg.PlaidSecret != ""),
		"paddle", boolStatus(cfg.PaddleAPIKey != "" && cfg.PaddleWebhookSecret != ""),
		"sendgrid", boolStatus(cfg.SendGridAPIKey != ""),
		"deletion", boolStatus(cfg.DeletionTokenSecret != ""),
		"posthog", boolStatus(cfg.PostHogAPIKey != ""),
	)

	// Initialize database connection pool (optional for local dev)
	var db *pgxpool.Pool
	if cfg.DatabaseURL != "" {
		dbConfig := database.DefaultConfig(cfg.DatabaseURL)
		dbConfig.MaxConns = cfg.DatabaseMaxConns
		db, err = database.NewPool(context.Background(), dbConfig)
		if err != nil {
			appLogger.Error("Failed to connect to database", "error", err)
			os.Exit(1)
		}
		defer db.Close()

		appLogger.Info("Database connection pool established (pgx v5)")
		// Start DB pool metrics collection (runs until process exits)
		observability.StartDBMetricsCollector(context.Background(), db)
	} else {
		appLogger.Warn("Running without database (set DATABASE_URL to enable)")
	}

	// Validate CORS configuration before starting (fail-fast for production misconfiguration)
	if err := middleware.ValidateCORSConfig(cfg.CORSAllowedOrigins, cfg.Environment); err != nil {
		appLogger.Error("CORS configuration error", "error", err)
		os.Exit(1)
	}

	// Create Echo instance
	e := echo.New()

	// HTTP server hardening: timeouts and max header size
	e.Server.ReadTimeout = cfg.ReadTimeout
	e.Server.WriteTimeout = cfg.WriteTimeout
	e.Server.IdleTimeout = cfg.IdleTimeout
	e.Server.MaxHeaderBytes = 8 << 10 // 8KB

	// Register request validator (required for c.Validate() in handlers)
	e.Validator = middleware.NewValidator()

	// Middleware (order matters!)
	e.Use(middleware.RequestID())                                                          // 1. Generate request IDs first
	e.Use(middleware.SecurityHeaders(cfg.Environment))                                    // 2. Security hardening headers
	e.Use(middleware.IPRateLimiter(middleware.RateLimitConfig{                            // 3. Per-IP rate limiting
		IPLimit:           cfg.RateLimitIP,
		AuthEndpointLimit: cfg.RateLimitAuth,
		WindowDuration:    time.Minute,
	}))
	e.Use(middleware.PanicRecovery(appLogger, cfg.Environment))                            // 4. Panic recovery (formats RFC 9457 response)
	e.Use(sentryecho.New(sentryecho.Options{Repanic: true}))                               // 5. Sentry panic reporting
	e.Use(otelecho.Middleware("boostedbudget-api"))                                        // 6. OTEL tracing
	e.Use(middleware.StructuredLogging(appLogger))                                         // 7. slog-based HTTP logging
	e.Use(middleware.MetricsMiddleware())                                                  // 8. Prometheus metrics
	e.Use(echomiddleware.BodyLimit(cfg.MaxBodySize))                                       // 9. Request body size limit
	e.Use(echomiddleware.CORSWithConfig(middleware.GetCORSConfig(cfg.CORSAllowedOrigins))) // 10. CORS handling with configured origins

	// Initialize Huma API wrapping Echo for OpenAPI generation
	// Huma provides automatic OpenAPI 3.1 spec generation from Go code
	humaAPI := humaecho.New(e, humaconfig.NewAPIConfig(cfg.Version, cfg.Environment))
	appLogger.Info("Huma API initialized - OpenAPI at /openapi.json, Docs at /docs")

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
	appLogger.Info("Huma metrics route registered")

	// Load tier configuration (used by both public and authenticated endpoints)
	tierConfig, err := config.LoadTierConfig()
	if err != nil {
		appLogger.Error("Failed to load tier config", "error", err)
		appLogger.Error("Tier configuration is required")
		os.Exit(1)
	}
	appLogger.Info("Tier configuration loaded")

	// Subscription tiers endpoint (public - needed for pricing page) - Huma operation
	subscriptionTiersHandler := operations.NewSubscriptionTiersHandler(tierConfig)
	operations.RegisterSubscriptionTiersRoutes(humaAPI, subscriptionTiersHandler)

	// Load region configuration and initialize domain registry (#974)
	regionConfig, err := config.LoadRegionConfig()
	if err != nil {
		appLogger.Error("Failed to load region config", "error", err)
		os.Exit(1)
	}
	domain.InitRegionRegistry(convertRegionConfig(regionConfig), regionConfig.GetDefaultRegion(), regionConfig.GetServerRegion())
	appLogger.Info("Region configuration loaded", "regions", len(regionConfig.Regions))

	// Regions endpoint (public - needed for profile region selector)
	regionsHandler := operations.NewRegionsHandler(regionConfig)
	operations.RegisterRegionRoutes(humaAPI, regionsHandler)

	// Default dashboard layout endpoint migrated to Huma (see humaDashboardHandler) - Issue #559

	// Declare plaidService at package scope for use in both authenticated routes and webhook endpoint
	var plaidService services.PlaidService
	// Declare accountService at package scope for Huma handler registration
	var accountService services.AccountService

	// Authenticated routes (requires database and Clerk)

	// Issue #573/629: River queue for async job processing
	var jobQueue queue.JobQueue
	var riverRunner queue.JobRunner

	// Issue #629: Declare deletion workers for River registration
	var accountDeletionWorker *jobs.AccountDeletionWorker
	var scheduledDeletionCheckWorker *jobs.ScheduledDeletionCheckWorker

	// Issue #668: Declare cleanup workers for Plaid item lifecycle management
	var itemCleanupCheckWorker *jobs.ItemCleanupCheckWorker
	var itemCleanupWorker *jobs.ItemCleanupWorker
	var plaidCleanupService services.PlaidItemCleanupService
	var deletionService services.DeletionService // Declared here for late binding of plaidCleanupService

	// Issue #679, #680: Declare trial service and expiration check worker
	var trialService *services.TrialService
	var trialExpirationCheckWorker *jobs.TrialExpirationCheckWorker

	// Issue #762: Declare Paddle reconciliation worker
	var paddleReconciliationWorker *jobs.PaddleReconciliationWorker

	// Issue #930: Declare retention cleanup check worker
	var retentionCleanupCheckWorker *jobs.RetentionCleanupCheckWorker

	// Issue #37: Declare rule scheduler manager at package scope for graceful shutdown
	var ruleSchedulerManager *workers.RuleSchedulerManager

	// Issue #45, #46: Declare Paddle services and workers at package scope
	var paddleService services.PaddleService
	var paddleWebhookService services.PaddleWebhookService
	var paddleCheckoutHandler *operations.PaddleCheckoutHandler
	var subscriptionWebhookWorker *jobs.SubscriptionWebhookWorker
	var paddleWebhookRepo repository.PaddleWebhookRepository
	var paddleTransactionRepo repository.PaddleTransactionRepository // Issues #747 #748

	// Declare encryption service and field encryptor at outer scope so they are accessible
	// in both the authenticated block and later public webhook handlers.
	var encryptionService services.EncryptionService
	var fieldEncryptor *appcrypto.FieldEncryptor

	if db != nil && cfg.ClerkSecretKey != "" {
		// Create encryption service (required for repository-layer PII encryption)
		if cfg.EncryptionKey != "" {
			encryptionService, err = services.NewEncryptionService(cfg)
			if err != nil {
				appLogger.Error("Failed to initialize encryption service", "error", err)
				appLogger.Error("Encryption service is required for account linking")
				os.Exit(1)
			}
			appLogger.Info("Encryption service initialized")
		} else {
			appLogger.Error("ENCRYPTION_KEY environment variable is required")
			os.Exit(1)
		}

		// Create field encryptor for repository-layer PII encryption
		fieldEncryptor, err = appcrypto.NewFieldEncryptor(encryptionService, []byte(cfg.EncryptionKey))
		if err != nil {
			appLogger.Error("Failed to initialize field encryptor", "error", err)
			os.Exit(1)
		}

		// Create repositories
		profileRepo := repository.NewProfileRepository(db, fieldEncryptor)
		privacyRepo := repository.NewPrivacyRepository(db)
		linkedItemRepo := repository.NewLinkedItemRepository(db, fieldEncryptor)
		accountRepo := repository.NewAccountRepository(db, fieldEncryptor)
		transactionRepo := repository.NewTransactionRepository(db, fieldEncryptor)
		webhookRepo := repository.NewWebhookRepository(db, fieldEncryptor)
		paddleWebhookRepo = repository.NewPaddleWebhookRepository(db, fieldEncryptor)            // Issue #45, #46: Paddle webhooks
		paddleTransactionRepo = repository.NewPaddleTransactionRepository(db, fieldEncryptor)   // Issues #747 #748: Paddle transaction audit log
		identityRepo := repository.NewUserIdentityRepository(db, fieldEncryptor) // Maps Clerk IDs to user UUIDs
		notificationRepo := repository.NewNotificationRepository(db, fieldEncryptor)             // Issue #257: Notification storage
		balanceSnapshotRepo := repository.NewBalanceSnapshotRepository(db)       // Issue #353: Balance tracking
		userTagRepo := repository.NewUserTagRepository(db)                       // Issue #345: User tags
		customCategoryRepo := repository.NewCustomCategoryRepository(db)         // Issue #345: Custom categories
		categorizationRuleRepo := repository.NewCategorizationRuleRepository(db) // Issue #346: Categorization rules
		analyticsRepo := repository.NewAnalyticsRepository(db)                   // Issue #354: Cash flow analytics
		budgetRepo := repository.NewBudgetRepository(db)                         // Issue #28: Budget management
		savingsGoalRepo := repository.NewSavingsGoalRepository(db)               // Issue #28: Savings goals
		dashboardPrefsRepo := repository.NewDashboardPreferencesRepository(db)   // Issue #354: Dashboard preferences
		transferSuggestionRepo := repository.NewTransferSuggestionRepository(db) // Issue #36: Transfer suggestions
		automationRuleRepo := repository.NewAutomationRulesRepository(db)        // Issue #37: Automation rules
		adminUserRepo := repository.NewAdminUserRepository(db, fieldEncryptor)    // Issue #639: Admin user management
		systemHealthRepo := repository.NewSystemHealthRepository(db)             // Issue #640: System health monitoring
		deadLetterJobRepo := repository.NewDeadLetterJobRepository(db)           // Issue #640: Dead letter queue metrics
		securityRepo := repository.NewSecurityRepository(db)                           // Issue #659: Plaid Investments
		holdingRepo := repository.NewHoldingRepository(db)                             // Issue #659: Plaid Investments
		investmentTxnRepo := repository.NewInvestmentTransactionRepository(db)        // Issue #659: Plaid Investments
		creditLiabilityRepo := repository.NewCreditLiabilityRepository(db)            // Issue #910: Plaid Liabilities
		mortgageLiabilityRepo := repository.NewMortgageLiabilityRepository(db, fieldEncryptor)       // Issue #910: Plaid Liabilities
		studentLoanLiabilityRepo := repository.NewStudentLoanLiabilityRepository(db, fieldEncryptor) // Issue #910: Plaid Liabilities

		// Create mock audit logger (will be replaced with real implementation later)
		auditLogger := &mockAuditLogger{}

		// Create services
		profileService := services.NewProfileService(profileRepo)
		privacyService := services.NewPrivacyService(privacyRepo)
		transactionService := services.NewTransactionService(transactionRepo)
		registrationService := services.NewRegistrationService(profileRepo, identityRepo, privacyRepo, auditLogger, db)
		registrationService.SetAnalytics(phClient)
		subscriptionOverrideRepo := repository.NewSubscriptionOverrideRepository(db)
		subscriptionService := services.NewSubscriptionService(tierConfig, subscriptionOverrideRepo)
		// Issue #353: Dashboard service for balance tracking
		dashboardService := services.NewDashboardService(balanceSnapshotRepo, accountRepo)
		// Issue #345: Tag and category services
		userTagService := services.NewUserTagService(userTagRepo)
		customCategoryService := services.NewCustomCategoryService(customCategoryRepo)
		// Issue #346: Categorization rules services
		categorizationRuleService := services.NewCategorizationRuleService(categorizationRuleRepo)
		ruleApplicationService := services.NewRuleApplicationService(categorizationRuleRepo)
		ruleApplicationService.SetAnalytics(phClient)
		// Issue #354: Cash flow analytics and dashboard preferences services
		analyticsService := services.NewAnalyticsService(analyticsRepo)
		// Issue #639: adminUserService is created later after adminService is initialized
		// Issue #474: Load dashboard layout config for preferences service
		// This ensures reset-to-default uses YAML config version, not hardcoded version 2
		dashboardLayoutConfig, err := config.LoadDashboardLayoutConfig()
		if err != nil {
			appLogger.Error("Failed to load dashboard layout config", "error", err)
			os.Exit(1)
		}
		dashboardPrefsService := services.NewDashboardPreferencesService(dashboardPrefsRepo, dashboardLayoutConfig)
		// Issue #28: Budget and savings goal services
		budgetLogger := appLogger
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
		automationRuleService := services.NewAutomationRuleService(automationRuleRepo)
		appLogger.Info("Automation rule service initialized")

		// Issue #37: Rule Execution Service
		ruleExecutionRepo := repository.NewRuleExecutionRepository(db)
		ruleExecutionService := services.NewRuleExecutionService(ruleExecutionRepo, transferSuggestionRepo, automationRuleRepo, nil)
		ruleExecutionService.SetAnalytics(phClient)
		appLogger.Info("Rule execution service initialized")

		// Issue #37: Rule Scheduler Manager with providers
		ruleSchedulerManager = workers.NewRuleSchedulerManager(appLogger)

		// Create and register CronTriggerProvider
		cronProvider := workers.NewCronTriggerProvider(
			automationRuleRepo,
			ruleExecutionService,
			15*time.Minute, // Refresh interval for picking up new/updated rules
			appLogger,
		)
		ruleSchedulerManager.RegisterProvider(cronProvider)

		// Create and register ManualTriggerProvider
		manualProvider := workers.NewManualTriggerProvider(
			automationRuleRepo,
			ruleExecutionService,
			appLogger,
		)
		ruleSchedulerManager.RegisterProvider(manualProvider)
		appLogger.Info("Rule scheduler manager initialized with cron and manual providers")

		// Initialize Email service (needed by deletion service and item error service)
		// Optional - uses no-op mock if not configured
		var emailService services.EmailService
		if cfg.SendGridAPIKey != "" && cfg.SendGridFromEmail != "" {
			emailService = services.NewEmailService(cfg.SendGridAPIKey, cfg.SendGridFromEmail, cfg.FrontendURL)
			appLogger.Info("Email service (SendGrid) initialized")
		} else {
			emailService = services.NewEmailServiceMock("", "", cfg.FrontendURL)
			appLogger.Warn("Email service disabled - cancellation links shown in UI instead (see GitHub issue for SendGrid integration)")
		}

		// Create deletion services (if token secret configured)
		if cfg.DeletionTokenSecret != "" {
			// Initialize Clerk Backend API service
			clerkService := services.NewClerkService(cfg.ClerkSecretKey)
			appLogger.Info("Clerk Backend API service initialized")

			// Initialize Deletion service (reuses auditLogger and emailService created earlier)
			// Issue #668: plaidCleanupSvc is nil here - will be wired later after plaidService is created
			// Uses assignment (=) not declaration (:=) to assign to outer-scoped deletionService variable
			deletionService = services.NewDeletionService(
				profileRepo,
				emailService,
				clerkService,
				auditLogger,
				nil, // plaidCleanupSvc - wired up later via SetPlaidCleanupService() if Plaid is configured
				cfg.DeletionGracePeriodDays,
				cfg.DeletionTokenSecret,
			)
			deletionService.SetAnalytics(phClient)
			appLogger.Info("Deletion service initialized (30-day grace period)")

			// Register Huma deletion routes (Issue #559: Huma migration)
			humaDeletionHandler := operations.NewDeletionHandler(deletionService)
			operations.RegisterDeletionRoutes(humaAPI, humaDeletionHandler)
			appLogger.Info("Huma deletion routes registered")

			// Issue #629: Create River-based deletion workers
			// These will be registered with River after jobQueue is created
			accountDeletionWorker = jobs.NewAccountDeletionWorker(jobs.AccountDeletionWorkerDeps{
				DeletionService: deletionService,
				Logger:          appLogger,
			})
			appLogger.Info("Account deletion worker created (River)")

			// Issue #629: Create scheduled deletion check worker
			// Note: JobQueue is nil initially - set later via SetJobQueue() to handle circular dependency
			scheduledDeletionCheckWorker = jobs.NewScheduledDeletionCheckWorker(jobs.ScheduledDeletionCheckWorkerDeps{
				ProfileRepo: profileRepo,
				JobQueue:    nil, // Set later after River client is created
				Logger:      appLogger,
			})
			appLogger.Info("Scheduled deletion check worker created (River)")

			// Issue #930: Create retention cleanup check worker
			// Note: EmailService is nil in MVP — notice emails are skipped until Phase 3 email templates
			retentionCleanupCheckWorker = jobs.NewRetentionCleanupCheckWorker(jobs.RetentionCleanupCheckWorkerDeps{
				ProfileRepo:     profileRepo,
				DeletionService: deletionService,
				RetentionMonths: cfg.RetentionPeriodMonths,
				Logger:          appLogger,
			})
			appLogger.Info("Retention cleanup check worker created (River)")
		} else {
			appLogger.Warn("Deletion services disabled (requires DELETION_TOKEN_SECRET)")
		}

		// Create Paddle services (if configured) - Issue #45, #46: Paddle Payment Integration
		if cfg.PaddleAPIKey != "" {
			logger := appLogger
			var err error

			// Initialize PaddleService for API operations
			paddleService, err = services.NewPaddleService(cfg, logger)
			if err != nil {
				appLogger.Error("Failed to initialize Paddle service", "error", err)
			} else {
				appLogger.Info("Paddle service initialized")
			}

			// Initialize PaddleWebhookService for signature verification
			if cfg.PaddleWebhookSecret != "" {
				paddleWebhookService, err = services.NewPaddleWebhookService(cfg, logger)
				if err != nil {
					appLogger.Error("Failed to initialize Paddle webhook service", "error", err)
				} else {
					appLogger.Info("Paddle webhook service initialized (HMAC-SHA256)")
				}
			}

			// Create Paddle checkout handler (authenticated route)
			if paddleService != nil {
				paddleCheckoutHandler = operations.NewPaddleCheckoutHandler(operations.PaddleCheckoutHandlerDeps{
					PaddleService: paddleService,
					ProfileRepo:   profileRepo,
					Config:        cfg,
					TierConfig:    tierConfig,
					Logger:        logger,
				})
				appLogger.Info("Paddle checkout handler created")
			}

			// Create subscription webhook worker
			// Issues #747 #748: Wire ClerkService, NotificationRepo, and PaddleTransactionRepo
			if paddleWebhookService != nil {
				webhookClerkService := services.NewClerkService(cfg.ClerkSecretKey)
				subscriptionWebhookWorker = jobs.NewSubscriptionWebhookWorker(jobs.SubscriptionWebhookWorkerDeps{
					PaddleWebhookRepo:     paddleWebhookRepo,
					ProfileRepo:           profileRepo,
					ClerkService:          webhookClerkService,
					NotificationRepo:      notificationRepo,
					PaddleTransactionRepo: paddleTransactionRepo,
					TierConfig:            tierConfig,
					Logger:                logger,
				})
				subscriptionWebhookWorker.SetAnalytics(phClient)
				appLogger.Info("Subscription webhook worker created (River)")
			}
		} else {
			appLogger.Warn("Paddle services disabled (PADDLE_API_KEY not configured)")
		}

		// Create Plaid service and account linking (if configured)
		// plaidHandler migrated to Huma (see humaPlaidHandler) - Issue #559
		// var plaidHandler *handlers.PlaidHandler
		if cfg.PlaidClientID != "" && cfg.PlaidSecret != "" {
			var err error
			plaidService, err = services.NewPlaidService(cfg)
			if err != nil {
				appLogger.Error("Failed to initialize Plaid service", "error", err)
			} else {
				// Create account service with all dependencies
				accountService = services.NewAccountService(
					plaidService,
					subscriptionService,
					linkedItemRepo,
					accountRepo,
					profileRepo,
					db,
				)

				// plaidHandler migrated to Huma - Issue #559
				_ = plaidService   // Silence unused variable (used by Huma handler)
				_ = accountService // Silence unused variable (used by Huma handler)
				appLogger.Info("Plaid service initialized")
				appLogger.Info("Account linking service initialized")

				// Issue #668: Create PlaidItemCleanupService for Plaid item lifecycle management
				// This service handles calling Plaid /item/remove to stop billing
				plaidCleanupService = services.NewPlaidItemCleanupService(
					linkedItemRepo,
					plaidService,
				)
				appLogger.Info("Plaid item cleanup service initialized")

				// Issue #668: Wire cleanup service to deletion service (resolves nil dependency from earlier)
				// DeletionService was created before Plaid was available, now we can inject via setter
				if deletionService != nil {
					deletionService.SetPlaidCleanupService(plaidCleanupService)
					appLogger.Info("Plaid cleanup service wired to deletion service")
				}

				// Issue #896: Wire cleanup service to subscription webhook worker for disconnect on cancel/pause
				if subscriptionWebhookWorker != nil {
					subscriptionWebhookWorker.SetPlaidCleanupService(plaidCleanupService)
					appLogger.Info("Plaid cleanup service wired to subscription webhook worker")
				}

				// Issue #679, #680: Create TrialService for trial management
				trialService = services.NewTrialService(tierConfig, profileRepo, plaidCleanupService)
				trialService.SetRetentionMonths(cfg.RetentionPeriodMonths) // #930: Propagate retention period from config
				appLogger.Info("Trial service initialized")

				// Issue #679: Wire trial service to registration service for auto-grant on signup
				registrationService.SetTrialGranter(trialService)
				appLogger.Info("Trial service wired to registration service")

				// Issue #679, #680: Create trial expiration check worker
				trialExpirationCheckWorker = jobs.NewTrialExpirationCheckWorker(jobs.TrialExpirationCheckWorkerDeps{
					ProfileRepo:  profileRepo,
					TrialService: trialService,
					Logger:       appLogger,
				})
				appLogger.Info("Trial expiration check worker created (River)")

				// Issue #762: Create Paddle reconciliation worker
				if cfg.PaddleReconciliationEnabled && paddleService != nil {
					paddleReconciliationWorker = jobs.NewPaddleReconciliationWorker(jobs.PaddleReconciliationWorkerDeps{
						ProfileRepo: profileRepo,
						PaddleSvc:   paddleService,
						TierConfig:  tierConfig,
						Logger:      appLogger,
						AutoFix:     cfg.PaddleReconciliationAutoFix,
					})
					appLogger.Info("Paddle reconciliation worker created (River)")
				}

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
					subscriptionService,
					ruleApplicationService,
					budgetSyncService,
					transactionMonitorService,
					appLogger,
				)

				// Initialize Item Error Service for ITEM webhook processing
				// Issue #257: Item Error Handler - Process ITEM Webhooks and User Notifications
				itemErrorService := services.NewItemErrorService(
					linkedItemRepo,
					notificationRepo,
					profileRepo,
					plaidService,
					emailService,
					appLogger,
				)

				// Issue #659: Initialize InvestmentSyncService for holdings and investment transactions
				investmentSyncService := services.NewInvestmentSyncService(
					webhookRepo,
					linkedItemRepo,
					accountRepo,
					securityRepo,
					holdingRepo,
					investmentTxnRepo,
					plaidService,
					appLogger,
				)
				appLogger.Info("Investment sync service initialized")
				if phClient != nil {
					investmentSyncService.SetAnalytics(phClient)
				}

				// Issue #910: Initialize LiabilitySyncService for credit, mortgage, student loan liabilities
				liabilitySyncService := services.NewLiabilitySyncService(
					webhookRepo,
					linkedItemRepo,
					accountRepo,
					creditLiabilityRepo,
					mortgageLiabilityRepo,
					studentLoanLiabilityRepo,
					plaidService,
					appLogger,
				)
				appLogger.Info("Liability sync service initialized")
				if phClient != nil {
					liabilitySyncService.SetAnalytics(phClient)
				}

				// Issue #573/629: Initialize River queue for async job processing
				logger := slog.Default()
				riverConfig := riverqueue.DefaultConfig()

				// Issue #629: Configure dead letter handler for failed jobs
				deadLetterHandler := riverqueue.NewDeadLetterHandler(riverqueue.DeadLetterHandlerDeps{
					DB:     db,
					Logger: logger,
				})
				deadLetterHandler.SetAnalytics(phClient)
				riverConfig.ErrorHandler = deadLetterHandler.ErrorHandlerFunc()
				appLogger.Info("Dead letter handler configured for failed jobs")

				// Issue #629: Configure periodic jobs
				var periodicJobs []*river.PeriodicJob

				// Scheduled deletion check (hourly) - checks for users scheduled for deletion
				if scheduledDeletionCheckWorker != nil {
					periodicJobs = append(periodicJobs, river.NewPeriodicJob(
						river.PeriodicInterval(1*time.Hour),
						func() (river.JobArgs, *river.InsertOpts) {
							return jobs.ScheduledDeletionCheckArgs{
									TriggerSource: "periodic",
								}, &river.InsertOpts{
									Queue: "low", // Deletions run on low priority queue
								}
						},
						&river.PeriodicJobOpts{RunOnStart: true}, // Run immediately on startup to catch any pending deletions
					))
					appLogger.Info("Scheduled deletion check periodic job configured (hourly)")
				}

				// Issue #668: Item cleanup check (daily at 3 AM) - checks for orphaned Plaid items
				// Detects: items in error state > 30 days, revoked items, stale items > 60 days, zero-account items
				periodicJobs = append(periodicJobs, river.NewPeriodicJob(
					river.PeriodicInterval(24*time.Hour), // Run once per day
					func() (river.JobArgs, *river.InsertOpts) {
						return jobs.ItemCleanupCheckArgs{
								TriggerSource: "periodic",
							}, &river.InsertOpts{
								Queue: "low", // Cleanup runs on low priority queue
							}
					},
					&river.PeriodicJobOpts{RunOnStart: false}, // Don't run on startup - wait for daily schedule
				))
				appLogger.Info("Item cleanup check periodic job configured (daily)")

				// Issue #679, #680: Trial expiration check (daily) - finds expired trials and freezes accounts
				// Expires trial: sets subscription_status='inactive', disconnects all Plaid items
				if trialExpirationCheckWorker != nil {
					periodicJobs = append(periodicJobs, river.NewPeriodicJob(
						river.PeriodicInterval(24*time.Hour), // Run once per day
						func() (river.JobArgs, *river.InsertOpts) {
							return jobs.TrialExpirationCheckArgs{
									TriggerSource: "periodic",
								}, &river.InsertOpts{
									Queue: "low", // Trial checks run on low priority queue
								}
						},
						&river.PeriodicJobOpts{RunOnStart: true}, // Run on startup to catch any expired trials
					))
					appLogger.Info("Trial expiration check periodic job configured (daily)")
				}

				// Issue #762: Paddle reconciliation check (daily) - detects status/tier discrepancies
				if paddleReconciliationWorker != nil {
					periodicJobs = append(periodicJobs, river.NewPeriodicJob(
						river.PeriodicInterval(24*time.Hour),
						func() (river.JobArgs, *river.InsertOpts) {
							return jobs.PaddleReconciliationArgs{
								TriggerSource: "periodic",
							}, &river.InsertOpts{
								Queue: "low",
							}
						},
						&river.PeriodicJobOpts{RunOnStart: false},
					))
					appLogger.Info("Paddle reconciliation periodic job configured (daily)")
				}

				// Issue #930: Retention cleanup check (daily) — warns and auto-deletes inactive/cancelled accounts
				if retentionCleanupCheckWorker != nil {
					periodicJobs = append(periodicJobs, river.NewPeriodicJob(
						river.PeriodicInterval(24*time.Hour),
						func() (river.JobArgs, *river.InsertOpts) {
							return jobs.RetentionCleanupCheckArgs{
								TriggerSource: "periodic",
							}, &river.InsertOpts{
								Queue: "low",
							}
						},
						&river.PeriodicJobOpts{RunOnStart: false},
					))
					appLogger.Info("Retention cleanup check periodic job configured (daily)")
				}

				if len(periodicJobs) > 0 {
					riverConfig.PeriodicJobs = periodicJobs
				}

				// Create WebhookProcessingWorker with all dependencies
				// Issue #659: InvestmentSyncSvc added for HOLDINGS and INVESTMENTS_TRANSACTIONS routing
				// Issue #910: LiabilitySyncSvc added for LIABILITIES routing
				webhookProcessingWorker := jobs.NewWebhookProcessingWorker(jobs.WebhookProcessingWorkerDeps{
					WebhookRepo:        webhookRepo,
					TransactionSyncSvc: transactionSyncService,
					InvestmentSyncSvc:  investmentSyncService,
					LiabilitySyncSvc:   liabilitySyncService,
					ItemErrorSvc:       itemErrorService,
					Logger:             logger,
				})

				// Issue #666: Create PlaidSyncWorker for auto-sync after account linking
				// Issue #171: Add DB for row-level locking to prevent concurrent syncs
				plaidSyncWorker := jobs.NewPlaidSyncWorker(jobs.PlaidSyncWorkerDeps{
					SyncService:    transactionSyncService,
					LinkedItemRepo: linkedItemRepo,
					JobQueue:       nil, // Set after River client created (circular dependency)
					Logger:         logger,
					DB:             db, // For row-level locking via FOR UPDATE
				})
				appLogger.Info("Plaid sync worker created")

				// Issue #659: Create InvestmentHoldingsSyncWorker
				investmentHoldingsSyncWorker := jobs.NewInvestmentHoldingsSyncWorker(jobs.InvestmentHoldingsSyncWorkerDeps{
					InvestmentSyncSvc: investmentSyncService,
					LinkedItemRepo:    linkedItemRepo,
					Logger:            logger,
					DB:                db,
				})
				appLogger.Info("Investment holdings sync worker created")

				// Issue #659: Create InvestmentTransactionsSyncWorker
				investmentTransactionsSyncWorker := jobs.NewInvestmentTransactionsSyncWorker(jobs.InvestmentTransactionsSyncWorkerDeps{
					InvestmentSyncSvc: investmentSyncService,
					LinkedItemRepo:    linkedItemRepo,
					Logger:            logger,
					DB:                db,
				})
				appLogger.Info("Investment transactions sync worker created")

				// Issue #668: Create ItemCleanupCheckWorker for periodic orphaned item detection
				// Checks for items in error state, revoked, stale, or with zero accounts
				itemCleanupCheckWorker = jobs.NewItemCleanupCheckWorker(jobs.ItemCleanupCheckWorkerDeps{
					LinkedItemRepo:     linkedItemRepo,
					AccountRepo:        accountRepo, // For zero-account detection
					JobQueue:           nil,         // Set after River client created (circular dependency)
					Logger:             logger,
					GracePeriodDays:    7,  // Days to wait before cleanup (error/stale/revoked items)
					ErrorDaysThreshold: 30, // Days in error state before cleanup candidate
					StaleDaysThreshold: 60, // Days since last sync before cleanup candidate
				})
				appLogger.Info("Item cleanup check worker created")

				// Issue #668: Create ItemCleanupWorker for executing individual item cleanup
				// Calls Plaid /item/remove to stop billing for orphaned items
				itemCleanupWorker = jobs.NewItemCleanupWorker(jobs.ItemCleanupWorkerDeps{
					PlaidCleanupSvc: plaidCleanupService,
					LinkedItemRepo:  linkedItemRepo,
					Logger:          logger,
				})
				appLogger.Info("Item cleanup worker created")

				// Create River workers and register all workers
				riverWorkers := river.NewWorkers()
				river.AddWorker(riverWorkers, webhookProcessingWorker)

				// Issue #666: Register PlaidSyncWorker with River
				river.AddWorker(riverWorkers, plaidSyncWorker)
				appLogger.Info("Plaid sync worker registered with River")

				// Issue #629: Register deletion workers if available
				if accountDeletionWorker != nil {
					river.AddWorker(riverWorkers, accountDeletionWorker)
					appLogger.Info("Account deletion worker registered with River")
				}
				if scheduledDeletionCheckWorker != nil {
					river.AddWorker(riverWorkers, scheduledDeletionCheckWorker)
					appLogger.Info("Scheduled deletion check worker registered with River")
				}

				// Issue #668: Register cleanup workers
				if itemCleanupCheckWorker != nil {
					river.AddWorker(riverWorkers, itemCleanupCheckWorker)
					appLogger.Info("Item cleanup check worker registered with River")
				}
				if itemCleanupWorker != nil {
					river.AddWorker(riverWorkers, itemCleanupWorker)
					appLogger.Info("Item cleanup worker registered with River")
				}

				// Issue #679, #680: Register trial expiration check worker
				if trialExpirationCheckWorker != nil {
					river.AddWorker(riverWorkers, trialExpirationCheckWorker)
					appLogger.Info("Trial expiration check worker registered with River")
				}

				// Issue #762: Register Paddle reconciliation worker
				if paddleReconciliationWorker != nil {
					river.AddWorker(riverWorkers, paddleReconciliationWorker)
					appLogger.Info("Paddle reconciliation worker registered with River")
				}

				// Issue #930: Register retention cleanup check worker
				if retentionCleanupCheckWorker != nil {
					river.AddWorker(riverWorkers, retentionCleanupCheckWorker)
					appLogger.Info("Retention cleanup check worker registered with River")
				}

				// Issue #45, #46: Register Paddle subscription webhook worker
				if subscriptionWebhookWorker != nil {
					river.AddWorker(riverWorkers, subscriptionWebhookWorker)
					appLogger.Info("Paddle subscription webhook worker registered with River")
				}

				// Issue #659: Register investment sync workers
				river.AddWorker(riverWorkers, investmentHoldingsSyncWorker)
				appLogger.Info("Investment holdings sync worker registered with River")
				river.AddWorker(riverWorkers, investmentTransactionsSyncWorker)
				appLogger.Info("Investment transactions sync worker registered with River")

				// Create River client
				riverClient, err := riverqueue.NewClient(db, riverWorkers, riverConfig)
				if err != nil {
					appLogger.Warn("Failed to initialize River queue", "error", err)
				} else {
					jobQueue = riverClient
					riverRunner = riverClient
					appLogger.Info("River queue initialized")

					// Wire up job queue to workers (resolves circular dependency)
					if scheduledDeletionCheckWorker != nil {
						scheduledDeletionCheckWorker.SetJobQueue(jobQueue)
						appLogger.Info("Scheduled deletion check worker connected to job queue")
					}
					if accountService != nil {
						accountService.SetJobQueue(jobQueue)
						appLogger.Info("Account service connected to job queue for auto-sync")
					}
					plaidSyncWorker.SetJobQueue(jobQueue)
					appLogger.Info("Plaid sync worker connected to job queue for follow-up jobs")

					// Issue #668: Wire up job queue to cleanup check worker
					if itemCleanupCheckWorker != nil {
						itemCleanupCheckWorker.SetJobQueue(jobQueue)
						appLogger.Info("Item cleanup check worker connected to job queue")
					}
				}

				appLogger.Info("Transaction sync service initialized")
				appLogger.Info("Item error service initialized")
			}
		}

		// Create handlers
		// profileHandler migrated to Huma (see humaProfileHandler below) - Issue #559
		_ = profileService // Used by Huma handler below
		// privacyHandler migrated to Huma (see humaPrivacyHandler below) - Issue #559
		_ = privacyService // Used by Huma handler below
		// transactionHandler migrated to Huma (see humaTransactionHandler below)
		// notificationHandler migrated to Huma (see humaNotificationHandler below) - Issue #559
		// dashboardHandler migrated to Huma (see humaDashboardHandler below) - Issue #559
		// tagHandler migrated to Huma (see humaTagHandler below)
		// categoryHandler migrated to Huma (see humaCategoryHandler below)
		// ruleHandler migrated to Huma (see humaRuleHandler below) - Issue #559
		// analyticsHandler migrated to Huma (see humaAnalyticsHandler below) - Issue #559
		// dashboardPrefsHandler migrated to Huma (see humaDashboardPrefsHandler below) - Issue #559
		// budgetHandler migrated to Huma (see humaBudgetHandler below) - Issue #559
		_ = budgetService                                                             // Used by Huma handler below
		_ = budgetSyncService                                                         // Used by Huma handler below
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
			identityRepo,        // Resolves Clerk IDs to internal user UUIDs
			registrationService, // Auto-registers new users on first login
		)

		// Apply Huma Clerk middleware for authenticated Huma routes
		// #930: Pass profileRepo so the middleware stores subscription_status in context
		humaClerkMiddleware := humamiddleware.NewClerkMiddleware(
			cfg.ClerkSecretKey,
			identityRepo,
			registrationService,
			appLogger,
			profileRepo,
		)
		humaAPI.UseMiddleware(humaClerkMiddleware)
		appLogger.Info("Huma Clerk middleware applied")

		// #930: Apply read-only enforcement middleware — blocks mutations for inactive/cancelled subscriptions
		humaAPI.UseMiddleware(humamiddleware.NewReadOnlyMiddleware())
		appLogger.Info("Huma read-only enforcement middleware applied")

		// Load admin email config and create AdminService (Issue #309)
		adminEmailConfig, err := config.LoadAdminEmailConfig()
		if err != nil {
			appLogger.Warn("Failed to load admin email config (using empty list)", "error", err)
			adminEmailConfig = &domain.AdminEmailConfig{AdminEmails: []string{}}
		}
		adminService := services.NewAdminService(cfg.ClerkSecretKey, adminEmailConfig.AdminEmails)
		appLogger.Info("Admin service initialized", "hardcoded_admin_emails", len(adminEmailConfig.AdminEmails))

		// Issue #639: Admin user management - needs adminService for admin status checks
		adminUserService := services.NewAdminUserService(adminUserRepo, adminService)

		// Issue #640: System health monitoring service
		systemHealthService := services.NewSystemHealthService(
			systemHealthRepo,
			deadLetterJobRepo,
			&dbPoolAdapter{db},
			cfg.PlaidEnv,
		)

		// Apply Huma Admin middleware for admin routes (Issue #309)
		humaAdminMiddleware := humamiddleware.NewAdminMiddleware(cfg, adminService)
		humaAPI.UseMiddleware(humaAdminMiddleware)
		appLogger.Info("Huma Admin middleware applied")

		// Register Huma tag routes (Issue #345: User tag management)
		humaTagHandler := operations.NewTagHandler(userTagService)
		operations.RegisterTagRoutes(humaAPI, humaTagHandler)
		appLogger.Info("Huma tag routes registered")

		// Register Huma transaction routes (Issue #559: Huma migration)
		humaTransactionHandler := operations.NewTransactionHandler(transactionService)
		operations.RegisterTransactionRoutes(humaAPI, humaTransactionHandler)
		appLogger.Info("Huma transaction routes registered")

		// Register Huma category routes (Issue #559: Huma migration)
		humaCategoryHandler := operations.NewCategoryHandler(customCategoryService)
		operations.RegisterCategoryRoutes(humaAPI, humaCategoryHandler)
		appLogger.Info("Huma category routes registered")

		// Register Huma categorization rule routes (Issue #559: Huma migration)
		humaRuleHandler := operations.NewCategorizationRuleHandler(categorizationRuleService)
		operations.RegisterCategorizationRuleRoutes(humaAPI, humaRuleHandler)
		appLogger.Info("Huma categorization rule routes registered")

		// Register Huma notification routes (Issue #559: Huma migration)
		humaNotificationHandler := operations.NewNotificationHandler(notificationRepo)
		operations.RegisterNotificationRoutes(humaAPI, humaNotificationHandler)
		appLogger.Info("Huma notification routes registered")

		// Register Huma dashboard routes (Issue #559: Huma migration)
		humaDashboardHandler := operations.NewDashboardHandler(dashboardService)
		operations.RegisterDashboardRoutes(humaAPI, humaDashboardHandler)
		appLogger.Info("Huma dashboard routes registered")

		// Register Huma analytics routes (Issue #559: Huma migration)
		humaAnalyticsHandler := operations.NewAnalyticsHandler(analyticsService)
		operations.RegisterAnalyticsRoutes(humaAPI, humaAnalyticsHandler)
		appLogger.Info("Huma analytics routes registered")

		// Register Huma dashboard preferences routes (Issue #559: Huma migration)
		humaDashboardPrefsHandler := operations.NewDashboardPreferencesHandler(dashboardPrefsService)
		operations.RegisterDashboardPreferencesRoutes(humaAPI, humaDashboardPrefsHandler)
		appLogger.Info("Huma dashboard preferences routes registered")

		// Register Huma budget routes (Issue #559: Huma migration)
		humaBudgetHandler := operations.NewBudgetHandler(budgetService, budgetSyncService, transactionRepo)
		operations.RegisterBudgetRoutes(humaAPI, humaBudgetHandler)
		appLogger.Info("Huma budget routes registered")

		// Register Huma savings goal routes (Issue #559: Huma migration)
		humaSavingsGoalHandler := operations.NewSavingsGoalHandler(savingsGoalService)
		operations.RegisterSavingsGoalRoutes(humaAPI, humaSavingsGoalHandler)
		appLogger.Info("Huma savings goal routes registered")

		// Register Huma automation rule routes (Issue #37: Rule Scheduler)
		humaAutomationRuleHandler := operations.NewAutomationRuleHandler(automationRuleService, ruleSchedulerManager)
		humaAutomationRuleHandler.SetExecutionService(ruleExecutionService) // Issue #579: Wire execution service for test endpoint
		operations.RegisterAutomationRuleRoutes(humaAPI, humaAutomationRuleHandler)
		appLogger.Info("Huma automation rule routes registered")

		// Register Huma plaid routes (Issue #559: Huma migration)
		if plaidService != nil && accountService != nil {
			humaPlaidHandler := operations.NewPlaidHandler(plaidService, accountService)
			operations.RegisterPlaidRoutes(humaAPI, humaPlaidHandler)
			appLogger.Info("Huma plaid routes registered")
		}

		// Register Huma profile routes (Issue #559: Huma migration)
		humaProfileHandler := operations.NewProfileHandler(profileService)
		operations.RegisterProfileRoutes(humaAPI, humaProfileHandler)
		appLogger.Info("Huma profile routes registered")

		// Register Huma privacy routes (Issue #559: Huma migration)
		humaPrivacyHandler := operations.NewPrivacyHandler(privacyService)
		operations.RegisterPrivacyRoutes(humaAPI, humaPrivacyHandler)
		appLogger.Info("Huma privacy routes registered")

		// Register Huma category preview routes (Issue #559: Huma migration)
		humaCategoryPreviewHandler := operations.NewCategoryPreviewHandler(categoryPreviewService)
		operations.RegisterCategoryPreviewRoutes(humaAPI, humaCategoryPreviewHandler)
		appLogger.Info("Huma category preview routes registered")

		// Register Huma admin routes (Issue #559: Huma migration)
		// Security: AdminMiddleware handles environment check (404 in non-qa) and role validation (Issue #309)
		// Issue #698: Pass trialService for E2E test trial grant endpoint
		// Issue #638: Pass adminService for check-role endpoint
		// Issue #639: Pass adminUserService for user management endpoints
		// Issue #640: Pass systemHealthService for system health monitoring
		if plaidService != nil {
			humaAdminHandler := operations.NewAdminHandler(plaidService, linkedItemRepo, trialService, adminService, adminUserService, systemHealthService, subscriptionService, cfg).
				WithPaddleTransactionRepo(paddleTransactionRepo).
				WithPaddleWebhookRepo(paddleWebhookRepo).
				WithJobQueue(jobQueue)
			operations.RegisterAdminRoutes(humaAPI, humaAdminHandler)
			appLogger.Info("Huma admin routes registered (protected by AdminMiddleware)")
		}

		// Issue #821: QA auth bypass endpoint (QA/Staging only).
		// Guard 1: Environment check here. Guard 2: Handler rejects production in Bypass().
		if (cfg.Environment == "qa" || cfg.Environment == "staging") &&
			cfg.QABypassSecret != "" && cfg.QAAdminClerkUserID != "" && cfg.QAPremiumClerkUserID != "" {
			qaBypassService := services.NewQAAuthBypassService(cfg.QAAdminClerkUserID, cfg.QAPremiumClerkUserID, nil)
			qaBypassHandler := operations.NewQAAuthBypassHandler(qaBypassService, cfg.QABypassSecret, cfg.Environment)
			operations.RegisterQAAuthBypassRoutes(humaAPI, qaBypassHandler)
			appLogger.Info("QA auth bypass route registered")
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
		// 	appLogger.Info("Admin endpoints enabled (qa only)")
		// }

		// Deletion routes migrated to Huma (see humaDeletionHandler below) - Issue #559
		// Old Echo routes commented out:
		// if deletionHandler != nil {
		// 	api.POST("/user/delete", deletionHandler.RequestDeletion)
		// 	api.GET("/user/delete/cancel/:token", deletionHandler.CancelDeletion)
		// }

		appLogger.Info("Authenticated routes enabled (Clerk + Database)")
	}

	// Plaid webhook endpoint (public - no auth required) - Migrated to Huma (Issue #559)
	// Plaid servers need to POST to this endpoint without authentication
	// Issue #202: Enhanced with JWT signature verification and database storage
	// Issue #256: Webhook repository created in auth block above for webhook processor
	// Issue #573: Now enqueues River jobs for async processing
	if db != nil && plaidService != nil {
		// Webhook repository for audit trail
		webhookRepoForHandler := repository.NewWebhookRepository(db, fieldEncryptor)

		// Initialize webhook service for JWT signature verification
		webhookService := services.NewWebhookService(plaidService.GetClient())

		// Register Huma plaid webhook route (Issue #559: Huma migration)
		// Issue #573: Pass jobQueue for async webhook processing
		humaPlaidWebhookHandler := operations.NewPlaidWebhookHandler(operations.PlaidWebhookHandlerDeps{
			WebhookService: webhookService,
			WebhookRepo:    webhookRepoForHandler,
			JobQueue:       jobQueue,
			Logger:         slog.Default(),
		})
		operations.RegisterPlaidWebhookRoutes(humaAPI, humaPlaidWebhookHandler)

		appLogger.Info("Huma Plaid webhook route registered at POST /webhooks/plaid")
		appLogger.Info("Webhook signature verification enabled (ES256 JWT)")
		appLogger.Info("Webhook audit trail enabled (database storage)")
		if jobQueue != nil {
			appLogger.Info("Webhook async processing enabled (River queue)")
		}
	} else if db != nil {
		// Fallback: Basic webhook handler without signature verification
		// Used when Plaid credentials are not configured (e.g., local development)
		humaPlaidWebhookHandler := operations.NewPlaidWebhookHandler(operations.PlaidWebhookHandlerDeps{
			WebhookRepo: repository.NewWebhookRepository(db, fieldEncryptor),
			JobQueue:    jobQueue,
			Logger:      slog.Default(),
		})
		operations.RegisterPlaidWebhookRoutes(humaAPI, humaPlaidWebhookHandler)
		appLogger.Info("Huma Plaid webhook route registered at POST /webhooks/plaid (verification disabled)")
		appLogger.Warn("Webhook signature verification disabled (Plaid credentials not configured)")
	} else {
		appLogger.Warn("Webhook endpoint disabled (requires DATABASE_URL)")
	}

	// Paddle webhook endpoint (public - no auth required) - Issue #46: Paddle Webhook Handler
	// Paddle servers need to POST to this endpoint without authentication
	// Issue #45, #46: Paddle payment integration with HMAC-SHA256 signature verification
	if db != nil && paddleWebhookService != nil {
		// Register Huma paddle webhook route
		humaPaddleWebhookHandler := operations.NewPaddleWebhookHandler(operations.PaddleWebhookHandlerDeps{
			WebhookService: paddleWebhookService,
			WebhookRepo:    paddleWebhookRepo,
			JobQueue:       jobQueue,
			Logger:         slog.Default(),
		})
		operations.RegisterPaddleWebhookRoutes(humaAPI, humaPaddleWebhookHandler)

		appLogger.Info("Huma Paddle webhook route registered at POST /webhooks/paddle")
		appLogger.Info("Paddle webhook signature verification enabled (HMAC-SHA256)")
		appLogger.Info("Paddle webhook audit trail enabled (database storage)")
		if jobQueue != nil {
			appLogger.Info("Paddle webhook async processing enabled (River queue)")
		}
	} else if db != nil && paddleWebhookRepo != nil {
		// Fallback: Basic webhook handler without signature verification
		// Used when Paddle credentials are not configured (e.g., local development)
		humaPaddleWebhookHandler := operations.NewPaddleWebhookHandler(operations.PaddleWebhookHandlerDeps{
			WebhookRepo: paddleWebhookRepo,
			JobQueue:    jobQueue,
			Logger:      slog.Default(),
		})
		operations.RegisterPaddleWebhookRoutes(humaAPI, humaPaddleWebhookHandler)
		appLogger.Info("Huma Paddle webhook route registered at POST /webhooks/paddle (verification disabled)")
		appLogger.Warn("Paddle webhook signature verification disabled (webhook secret not configured)")
	} else {
		appLogger.Warn("Paddle webhook endpoint disabled (requires DATABASE_URL)")
	}

	// Paddle checkout routes (authenticated) - Issue #45: Paddle Payment Service
	// These routes require JWT authentication via Clerk middleware
	if paddleCheckoutHandler != nil {
		operations.RegisterPaddleCheckoutRoutes(humaAPI, paddleCheckoutHandler)
		appLogger.Info("Huma Paddle checkout routes registered")
		appLogger.Info("   - POST /api/v1/subscription/checkout")
		appLogger.Info("   - POST /api/v1/subscription/preview-update")
	}

	// Start background workers (if enabled)
	var workerCtx context.Context
	var workerCancel context.CancelFunc

	// Issue #573/629: Start River queue runner (handles webhook processing + account deletion)
	if riverRunner != nil {
		if workerCtx == nil {
			workerCtx, workerCancel = context.WithCancel(context.Background())
		}
		go func() {
			if err := riverRunner.Start(workerCtx); err != nil {
				appLogger.Warn("River queue runner error", "error", err)
			}
		}()
		appLogger.Info("River queue runner started")
	}

	// Issue #37: Start rule scheduler manager
	if ruleSchedulerManager != nil {
		if workerCtx == nil {
			workerCtx, workerCancel = context.WithCancel(context.Background())
		}
		go func() {
			if err := ruleSchedulerManager.Start(workerCtx); err != nil {
				appLogger.Warn("Failed to start rule scheduler", "error", err)
			}
		}()
		appLogger.Info("Rule scheduler manager started")
	}

	// Start server
	// Bind to 0.0.0.0 to be accessible from outside the container (required for Fly.io)
	address := fmt.Sprintf("0.0.0.0:%s", cfg.Port)
	appLogger.Info("Starting BoostedBudget API", "address", address, "region", regionConfig.GetServerRegion())

	// Graceful shutdown
	go func() {
		if err := e.Start(address); err != nil {
			appLogger.Error("Server stopped", "error", err)
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt)
	<-quit

	appLogger.Info("Shutting down server...")

	// Issue #37: Stop rule scheduler manager first (graceful shutdown)
	if ruleSchedulerManager != nil {
		appLogger.Info("Stopping rule scheduler manager...")
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := ruleSchedulerManager.Stop(stopCtx); err != nil {
			appLogger.Warn("Error stopping rule scheduler", "error", err)
		}
		stopCancel()
		appLogger.Info("Rule scheduler manager stopped")
	}

	// Issue #573: Stop River queue runner gracefully
	if riverRunner != nil {
		appLogger.Info("Stopping River queue runner...")
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 30*time.Second)
		if err := riverRunner.Stop(stopCtx); err != nil {
			appLogger.Warn("Error stopping River queue", "error", err)
			// Try force stop if graceful stop fails
			if err := riverRunner.StopAndCancel(stopCtx); err != nil {
				appLogger.Warn("Error force stopping River queue", "error", err)
			}
		}
		stopCancel()
		appLogger.Info("River queue runner stopped")
	}

	// Stop background workers (if running)
	if workerCancel != nil {
		appLogger.Info("Stopping background workers...")
		workerCancel()
	}

	// Shutdown HTTP server
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := e.Shutdown(ctx); err != nil {
		appLogger.Error("Server forced to shutdown", "error", err)
		os.Exit(1)
	}

	appLogger.Info("Server stopped gracefully")
}

// mockAuditLogger is a placeholder audit logger for deletion operations
// TODO: Replace with real audit logging implementation
type mockAuditLogger struct{}

func (m *mockAuditLogger) Log(ctx context.Context, action, resourceType, resourceID string) error {
	slog.Info("AUDIT", "action", action, "resource_type", resourceType, "resource_id", resourceID)
	return nil
}

// dbPoolAdapter adapts *pgxpool.Pool to services.DatabasePool interface (Issue #640)
// Lightweight wrapper that embeds pgxpool and delegates to its methods
type dbPoolAdapter struct {
	*pgxpool.Pool
}

func (a *dbPoolAdapter) GetStats() services.PoolStats {
	s := a.Stat()
	return services.PoolStats{
		AcquiredConns:     s.AcquiredConns(),
		IdleConns:         s.IdleConns(),
		MaxConns:          s.MaxConns(),
		ConstructingConns: s.ConstructingConns(),
		TotalConns:        s.TotalConns(),
	}
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

func boolStatus(configured bool) string {
	if configured {
		return "configured"
	}
	return "not configured"
}

// convertRegionConfig maps config.RegionConfig to domain.RegionInfo map for InitRegionRegistry.
func convertRegionConfig(rc *config.RegionConfig) map[string]domain.RegionInfo {
	result := make(map[string]domain.RegionInfo, len(rc.Regions))
	for code, def := range rc.Regions {
		result[code] = domain.RegionInfo{
			Code:        code,
			DisplayName: def.DisplayName,
			Country:     def.Country,
			City:        def.City,
			Flag:        def.Flag,
			FlyRegion:   def.Infrastructure.FlyRegion,
		}
	}
	return result
}
