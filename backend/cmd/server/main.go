package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"net/netip"
	"os"
	"os/signal"
	"sync/atomic"
	"time"

	"github.com/danielgtaylor/huma/v2/adapters/humaecho"
	sentryecho "github.com/getsentry/sentry-go/echo"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	echomiddleware "github.com/labstack/echo/v4/middleware"
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
	"github.com/riverqueue/river/rivertype"
	"go.opentelemetry.io/contrib/instrumentation/github.com/labstack/echo/otelecho"
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
		// Issue #1413 Task P4: start the manual-tier-override job-health gauge collector (runs
		// until process exits) — the direct, always-on measure of the C1 hazard, independent of
		// whether manual_tier_override_expiry_check's own job metrics are being scraped.
		startJobHealthMetricsCollector(context.Background(), repository.NewJobHealthRepository(db), appLogger)
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

	// Load Cloudflare IP ranges (falls back to hardcoded list on error)
	cfCIDRs := middleware.LoadCloudflareCIDRs(context.Background())
	cfCIDRRef := new(atomic.Pointer[[]netip.Prefix])
	cfCIDRRef.Store(&cfCIDRs)
	cfCtx, cfCancel := context.WithCancel(context.Background())
	go middleware.StartCIDRRefresher(cfCtx, 24*time.Hour, cfCIDRRef)

	// Middleware (order matters!)
	e.Use(middleware.RequestID())                                                    // 1. Generate request IDs first
	e.Use(middleware.SecurityHeaders(cfg.Environment))                               // 2. Security hardening headers
	e.Use(middleware.CloudflareOnly(cfCIDRRef, cfg.CFOriginSecret, cfg.Environment)) // 3. Cloudflare-only enforcement: blocks direct *.fly.dev access
	e.Use(middleware.IPRateLimiter(middleware.RateLimitConfig{                       // 4. Per-IP rate limiting
		IPLimit:           cfg.RateLimitIP,
		AuthEndpointLimit: cfg.RateLimitAuth,
		WindowDuration:    time.Minute,
	}))
	e.Use(middleware.PanicRecovery(appLogger, cfg.Environment))                            // 5. Panic recovery (formats RFC 9457 response)
	e.Use(sentryecho.New(sentryecho.Options{Repanic: true}))                               // 6. Sentry panic reporting
	e.Use(otelecho.Middleware("boostedbudget-api"))                                        // 7. OTEL tracing
	e.Use(middleware.StructuredLogging(appLogger))                                         // 8. slog-based HTTP logging
	e.Use(middleware.MetricsMiddleware())                                                  // 9. Prometheus metrics
	e.Use(echomiddleware.BodyLimit(cfg.MaxBodySize))                                       // 10. Request body size limit
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

	// Issue #898: Declare past_due check worker
	var pastDueCheckWorker *jobs.PastDueCheckWorker

	// Issue #930: Declare retention cleanup check worker
	var retentionCleanupCheckWorker *jobs.RetentionCleanupCheckWorker

	// Issue #1413 (Task 9): Declare manual tier override expiry check worker (the
	// time-triggered twin of the admin "remove override" endpoint — Issue #1414 / Task 7).
	// Not gated on DELETION_TOKEN_SECRET like retention's worker, since it depends on
	// adminUserRepo/clerkService, not deletionService.
	var manualTierOverrideExpiryCheckWorker *jobs.ManualTierOverrideExpiryCheckWorker

	// Issue #1371 — Phase 7 (Task 2): Declare household member cleanup check worker
	// (purges household_members rows removed 30+ days ago). Constructed once
	// householdService is available (below); not gated on DELETION_TOKEN_SECRET like
	// retention's worker, since it depends on householdService, not deletionService.
	var householdMemberCleanupCheckWorker *jobs.HouseholdMemberCleanupCheckWorker

	// Issue #1371 — Phase 7 (Task 6): Declare household over-capacity check worker
	// (authoritative daily backstop for the over-capacity grace lifecycle — flags/
	// clears grace, sends owner notices, auto-offboards at the 14-day deadline).
	// Constructed once householdService is available (below), same ordering
	// constraint as Task 2's worker since it also needs householdService.OffboardExcessMembers.
	var householdOverCapacityCheckWorker *jobs.HouseholdOverCapacityCheckWorker

	// Issue #633: Declare DLQ cleanup check worker
	var dlqCleanupCheckWorker *jobs.DLQCleanupCheckWorker

	// Issue #1087 R4-7: Declare idempotency cleanup worker (always active)
	var idempotencyCleanupWorker *jobs.IdempotencyCleanupWorker

	// Issue #1007: Declare email change propagation worker.
	// Issue #1015: Created unconditionally (before Plaid block); registered with River after.
	var emailChangePropagationWorker *jobs.EmailChangePropagationWorker

	// Issue #1015: Declare boost evaluation worker (non-Plaid)
	var boostEvaluationWorker *jobs.BoostEvaluationWorker

	// Issue #972: Declare budget period close check worker (daily scanner, no Plaid dependency)
	var budgetPeriodCloseCheckWorker *jobs.BudgetPeriodCloseCheckWorker

	// Issue #972: Declare savings stats snapshot worker (daily aggregate, no Plaid dependency)
	var savingsStatsWorker *jobs.SavingsStatsWorker

	// Issue #138: Declare portfolio snapshot worker (daily per-user snapshot, no Plaid dependency)
	var portfolioSnapshotWorker *jobs.PortfolioSnapshotWorker

	// Issue #1123: Declare effective category refresh worker
	var effectiveCategoryRefreshWorker *jobs.EffectiveCategoryRefreshWorker

	// Issue #138: Declare investment read services at outer scope (initialized inside Plaid block;
	// nil-checked in route registration since repos are available regardless of Plaid config).
	var investmentService services.InvestmentService
	var investmentAnalyticsService services.InvestmentAnalyticsService

	// Issue #1015: Declare Plaid-dependent workers at outer scope so they can be nil-checked
	// during River registration (which now occurs after the Plaid block).
	var webhookProcessingWorker *jobs.WebhookProcessingWorker
	var plaidSyncWorker *jobs.PlaidSyncWorker
	var investmentHoldingsSyncWorker *jobs.InvestmentHoldingsSyncWorker
	var investmentTransactionsSyncWorker *jobs.InvestmentTransactionsSyncWorker

	// Issue #37: Declare boost scheduler manager at package scope for graceful shutdown
	var boostSchedulerManager *workers.BoostSchedulerManager

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

		// #1001: Safety-net backfill — encrypt any rows that were created before encryption
		// was deployed (they have NULL blind indexes and are invisible to blind-index lookups).
		// Runs synchronously before serving traffic. Idempotent — skips already-encrypted rows.
		if err := database.BackfillEncryption(context.Background(), db, fieldEncryptor); err != nil {
			appLogger.Error("Encryption backfill failed — exiting to prevent duplicate accounts", "error", err)
			os.Exit(1)
		}

		// #1178: Populate prefix blind indices for existing users at startup.
		// Idempotent — skips users that already have rows. Fast no-op on restart.
		if err := database.BackfillEmailPrefixIndices(context.Background(), db, fieldEncryptor); err != nil {
			appLogger.Error("Email prefix index backfill failed — exiting", "error", err)
			os.Exit(1)
		}

		// #1306 P2: Repair accounts.user_account_id (silently dropped by CreateBatch INSERT bug).
		// Cascades to transactions, investment_transactions, and holdings. Idempotent backfill.
		if err := database.BackfillUserAccountIDs(context.Background(), db, fieldEncryptor); err != nil {
			appLogger.Error("BackfillUserAccountIDs failed — exiting to prevent orphaned records", "error", err)
			os.Exit(1)
		}

		// Create repositories
		profileRepo := repository.NewProfileRepository(db, fieldEncryptor)
		// Issue #1413 G2 / round-5: reject a manual_tier_override that names no configured tier
		// — both on the access-granting path (GetEffectiveSubscriptionStatusByUserID) and on the
		// profile response's representation path (humaProfileHandler.SetOverrideTierValidator,
		// wired below once it exists) — tierConfig was already loaded above (or the process
		// would have os.Exit(1)'d), so this validator is wired for the entire lifetime of the
		// process; see SetOverrideTierValidator's doc comment (profile_repository.go) for the
		// nil-validator fallback this deliberately never exercises in production. Shared as one
		// closure so both call sites can never drift apart on what counts as "configured".
		overrideTierValidator := func(tier string) bool {
			_, tierErr := tierConfig.GetTier(domain.TierName(tier))
			return tierErr == nil
		}
		profileRepo.SetOverrideTierValidator(overrideTierValidator)
		privacyRepo := repository.NewPrivacyRepository(db)
		linkedItemRepo := repository.NewLinkedItemRepository(db, fieldEncryptor)
		accountRepo := repository.NewAccountRepository(db, fieldEncryptor)
		transactionRepo := repository.NewTransactionRepository(db, fieldEncryptor)
		webhookRepo := repository.NewWebhookRepository(db, fieldEncryptor)
		paddleWebhookRepo = repository.NewPaddleWebhookRepository(db, fieldEncryptor)                         // Issue #45, #46: Paddle webhooks
		paddleTransactionRepo = repository.NewPaddleTransactionRepository(db, fieldEncryptor)                 // Issues #747 #748: Paddle transaction audit log
		identityRepo := repository.NewUserIdentityRepository(db, fieldEncryptor)                              // Maps Clerk IDs to user UUIDs
		notificationRepo := repository.NewNotificationRepository(db, fieldEncryptor)                          // Issue #257: Notification storage
		balanceSnapshotRepo := repository.NewBalanceSnapshotRepository(db)                                    // Issue #353: Balance tracking
		userTagRepo := repository.NewUserTagRepository(db)                                                    // Issue #345: User tags
		customCategoryRepo := repository.NewCustomCategoryRepository(db)                                      // Issue #345: Custom categories
		categorizationRuleRepo := repository.NewCategorizationRuleRepository(db)                              // Issue #346: Categorization rules
		analyticsRepo := repository.NewAnalyticsRepository(db)                                                // Issue #354: Cash flow analytics
		budgetRepo := repository.NewBudgetRepository(db)                                                      // Issue #28: Budget management
		savingsGoalRepo := repository.NewSavingsGoalRepository(db)                                            // Issue #28: Savings goals
		dashboardPrefsRepo := repository.NewDashboardPreferencesRepository(db)                                // Issue #354: Dashboard preferences
		transferSuggestionRepo := repository.NewTransferSuggestionRepositoryWithEncryptor(db, fieldEncryptor) // Issue #36 / #1267: Transfer suggestions (enc for account-name enrichment)
		boostRepo := repository.NewBoostsRepository(db)                                                       // Issue #37: Boosts
		adminUserRepo := repository.NewAdminUserRepository(db, fieldEncryptor)                                // Issue #639: Admin user management
		systemHealthRepo := repository.NewSystemHealthRepository(db)                                          // Issue #640: System health monitoring
		deadLetterJobRepo := repository.NewDeadLetterJobRepository(db)                                        // Issue #640: Dead letter queue metrics
		securityRepo := repository.NewSecurityRepository(db)                                                  // Issue #659: Plaid Investments
		holdingRepo := repository.NewHoldingRepository(db)                                                    // Issue #659: Plaid Investments
		investmentTxnRepo := repository.NewInvestmentTransactionRepository(db)                                // Issue #659: Plaid Investments
		portfolioSnapshotRepo := repository.NewPortfolioSnapshotRepository(db)                                // Issue #138: Portfolio snapshots
		investmentAnalyticsRepo := repository.NewInvestmentAnalyticsRepository(db)                            // Issue #138: Investment analytics
		creditLiabilityRepo := repository.NewCreditLiabilityRepository(db)                                    // Issue #910: Plaid Liabilities
		mortgageLiabilityRepo := repository.NewMortgageLiabilityRepository(db, fieldEncryptor)                // Issue #910: Plaid Liabilities
		studentLoanLiabilityRepo := repository.NewStudentLoanLiabilityRepository(db, fieldEncryptor)          // Issue #910: Plaid Liabilities
		merchantRepo := repository.NewMerchantRepository(db)                                                  // Issue #1163: Merchant batch-upsert
		// NewWorkClaimsOutcomeRepository (not NewWorkClaimsRepository): the subscription-cancellation
		// webhook needs the wider WorkClaimsOutcomeRepository view (#1413 round-3 finding H3, see
		// SubscriptionWebhookWorker.SetWorkClaimsRepo). Every other consumer below still declares its
		// setter parameter as the narrower repository.WorkClaimsRepository, and a
		// WorkClaimsOutcomeRepository value satisfies that automatically (its method set is a
		// superset) — so this single swap requires no other changes at any other call site.
		workClaimsRepo := repository.NewWorkClaimsOutcomeRepository(db) // Issue #1413: durable leased claims for destructive actions

		// Create GDPR-consent-gated analytics client.
		// User-behavior analytics events (e.g. transfer suggestion accept/reject) use
		// gdprClient.CaptureIfConsented, which checks privacy settings before firing.
		// Operational/system events use phClient.Capture directly.
		// New backend user-behavior events MUST use gdprClient; existing operational events are NOT retrofitted.
		consentChecker := phclient.NewPrivacyConsentAdapter(privacyRepo)
		gdprClient := phclient.NewGDPRClient(phClient, consentChecker, appLogger)

		// Create mock audit logger (will be replaced with real implementation later)
		auditLogger := &mockAuditLogger{}

		// Clerk Backend API service — used by deletion service and email propagation worker
		clerkService := services.NewClerkService(cfg.ClerkSecretKey)
		appLogger.Info("Clerk Backend API service initialized")

		// Issue #1413 (Task 9): Manual tier override expiry check worker — reuses
		// AdminUserRepository.RemoveUserTierOverride (adminUserRepo, Issue #1414 / Task 7) as
		// the time-triggered twin of the admin "remove override" endpoint. RetentionMonths
		// mirrors trialService.SetRetentionMonths / NewAdminUserService's retentionMonths param
		// below — same cfg.RetentionPeriodMonths value threaded everywhere retention windows
		// are sized.
		manualTierOverrideExpiryCheckWorker = jobs.NewManualTierOverrideExpiryCheckWorker(jobs.ManualTierOverrideExpiryCheckWorkerDeps{
			ProfileRepo:     profileRepo,
			Remover:         adminUserRepo,
			ClerkService:    clerkService,
			RetentionMonths: cfg.RetentionPeriodMonths,
			Logger:          appLogger,
		})
		appLogger.Info("Manual tier override expiry check worker created (River)")

		// Create services
		profileService := services.NewProfileService(services.NewProfileServiceDeps{
			Repo:         profileRepo,
			IdentityRepo: identityRepo,
			DB:           db,
			JobQueue:     nil, // Set after jobQueue is created via SetJobQueue
		})
		privacyService := services.NewPrivacyService(privacyRepo)
		transactionService := services.NewTransactionService(transactionRepo, customCategoryRepo)
		transactionService.SetAnalytics(phClient)
		transactionService.SetAccountRepository(accountRepo) // #1329: embed account in investment rows
		registrationService := services.NewRegistrationService(profileRepo, identityRepo, privacyRepo, auditLogger, db)
		registrationService.SetAnalytics(phClient)
		subscriptionOverrideRepo := repository.NewSubscriptionOverrideRepository(db)
		subscriptionService := services.NewSubscriptionService(tierConfig, subscriptionOverrideRepo)
		entitlementRepo := repository.NewEntitlementRepository(db) // Issue #1366: household entitlement resolution
		householdRepo := repository.NewHouseholdRepository(db)     // Issue #1366: minimal household reads
		// Issue #1367 — Phase 3: declared here (not near route registration below) so the
		// deletion-service owner guard, wired a few lines down, can use them too.
		householdInviteRepo := repository.NewHouseholdInviteRepository(db, fieldEncryptor)
		householdShareRepo := repository.NewHouseholdShareRepository(db)
		householdEntitlementService := services.NewHouseholdEntitlementService(entitlementRepo, profileRepo, householdRepo, tierConfig)
		// Issue #1413 — closes the household-owner mutation bypass: same validator as
		// profileRepo/humaProfileHandler above, so a malformed owner override can no longer
		// make ResolveEffectiveEntitlement report the owner (and therefore every member) as
		// covered. See HouseholdEntitlementService.SetOverrideTierValidator's doc comment.
		householdEntitlementService.SetOverrideTierValidator(overrideTierValidator)
		subscriptionService.SetEntitlementResolver(householdEntitlementService) // nil-safe injection; dormant until Phase 3 seeds rows
		// Issue #353: Dashboard service for balance tracking
		dashboardService := services.NewDashboardService(balanceSnapshotRepo, accountRepo)
		// Issue #345: Tag and category services
		userTagService := services.NewUserTagService(userTagRepo)
		customCategoryService := services.NewCustomCategoryService(customCategoryRepo)
		// Issue #346: Categorization rules services
		categorizationRuleService := services.NewCategorizationRuleService(categorizationRuleRepo, customCategoryRepo)
		ruleApplicationService := services.NewRuleApplicationService(categorizationRuleRepo)
		ruleApplicationService.SetAnalytics(phClient)
		// Issue #354: Cash flow analytics and dashboard preferences services
		analyticsService := services.NewAnalyticsService(analyticsRepo)
		analyticsService.SetBalanceSnapshotRepo(balanceSnapshotRepo) // wires snapshot repo for GetNetWorthTrend (Issue #1215 PR3)
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

		// Issue #1300: Savings contribution repo + progress service.
		// Created here so budget sync and goal services can share a single instance.
		savingsContribRepo := repository.NewSavingsContributionRepository(db)
		savingsProgressService := services.NewSavingsProgressService(savingsGoalRepo, savingsContribRepo, accountRepo, budgetLogger)

		// Issue #1300: Cross-wire budget ↔ savings-goal services for carry-forward / auto-create.
		budgetService.SetGoalRepo(savingsGoalRepo)
		// Issue #1300: Wire IDOR ownership checker so AddCategory validates savings_goal_id
		// belongs to the authenticated user before persisting (Codex C1).
		budgetService.SetGoalOwnershipChecker(savingsGoalRepo)
		savingsGoalService.SetBudgetRepo(budgetRepo)
		// Issue #1300 M13: Wire progress recomputer so CreateGoal initialises
		// current_amount per progress_basis immediately after goal creation.
		savingsGoalService.SetProgressService(savingsProgressService)
		// Issue #1300 M14: Wire contribution repo + analytics for basis-switch semantics.
		savingsGoalService.SetBasisSwitchContribRepo(savingsContribRepo)
		if phClient != nil {
			savingsGoalService.SetAnalytics(phClient)
		}

		// Issue #138: Investment read services — DB-only, no Plaid dependency.
		// Initialized here (outside the Plaid block) so investment route handlers
		// always have a non-nil service even when Plaid credentials are absent.
		investmentService = services.NewInvestmentService(holdingRepo, portfolioSnapshotRepo, investmentTxnRepo)
		investmentAnalyticsService = services.NewInvestmentAnalyticsService(investmentAnalyticsRepo)
		if phClient != nil {
			investmentService.SetAnalytics(phClient)
		}

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

		// Issue #37: Boost Service and Boost Scheduler
		boostService := services.NewBoostService(boostRepo)
		// Issue #972 GAP AE: inject repos for create/update IDOR ownership validation.
		boostService.SetSavingsGoalRepo(savingsGoalRepo)
		boostService.SetAccountRepo(accountRepo)
		boostService.SetBudgetRepo(budgetRepo) // #1270: category ownership validation
		appLogger.Info("Boost service initialized")

		// Issue #37: Boost Execution Service
		boostExecutionRepo := repository.NewBoostExecutionRepository(db)
		boostExecutionService := services.NewBoostExecutionService(boostExecutionRepo, transferSuggestionRepo, boostRepo, nil)
		boostExecutionService.SetAnalytics(phClient)
		appLogger.Info("Boost execution service initialized")

		// Issue #972: Wire budget sync → rule engine so budget_updated events are not silently dropped.
		// SetEventProcessor must be called after boostExecutionService is fully initialized.
		budgetSyncService.SetEventProcessor(boostExecutionService)
		// Issue #1300: Wire savings contribution ledger for savings-kind category spent amounts.
		budgetSyncService.SetSavingsSpentCalc(savingsContribRepo)

		// Issue #37: Boost Scheduler Manager with providers
		boostSchedulerManager = workers.NewBoostSchedulerManager(appLogger)

		// Create and register CronTriggerProvider
		cronProvider := workers.NewCronTriggerProvider(
			boostRepo,
			boostExecutionService,
			15*time.Minute, // Refresh interval for picking up new/updated rules
			appLogger,
		)
		boostSchedulerManager.RegisterProvider(cronProvider)

		// Create and register ManualTriggerProvider
		manualProvider := workers.NewManualTriggerProvider(
			boostRepo,
			boostExecutionService,
			appLogger,
		)
		boostSchedulerManager.RegisterProvider(manualProvider)
		appLogger.Info("Boost scheduler manager initialized with cron and manual providers")

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

		// Construct the household service here (rather than near route registration
		// further down) so it is available to the Task 2 household member cleanup
		// worker below, as well as the deletion-service owner guard a few lines down —
		// both need it well before the dashboard/analytics handlers that also resolve
		// `scope=household` requests (Issue #1369 — Phase 5). Route registration for
		// the household/household-share endpoints themselves still happens further
		// down, reusing this same householdService instance. #1371 — Phase 7 (Task 2).
		householdService := services.NewHouseholdService(
			db, householdRepo, householdInviteRepo, householdShareRepo, entitlementRepo,
			householdEntitlementService, profileRepo, tierConfig, emailService, fieldEncryptor,
		)
		householdService.SetAnalytics(phClient)
		// #1367 — Phase 3 Round 2 (GT-3): enrich GET /households/me/members with a Clerk
		// display name + avatar. identityRepo maps internal user_id -> Clerk ID;
		// NewDefaultClerkUserGetter reuses the same Clerk SDK plumbing as AdminService
		// (clerk.SetKey is already called there, well before the server accepts requests).
		householdService.SetMemberProfileResolver(services.NewClerkMemberProfileResolver(identityRepo, services.NewDefaultClerkUserGetter()))

		// Issue #1371 — Phase 7 (Task 2): Create household member cleanup check worker.
		// Unlike retention's worker (gated on DELETION_TOKEN_SECRET, since it calls
		// deletionService), this worker delegates to householdService.PurgeRemovedMembership,
		// which is always constructed above — so this worker is always non-nil too.
		householdMemberCleanupCheckWorker = jobs.NewHouseholdMemberCleanupCheckWorker(jobs.HouseholdMemberCleanupCheckWorkerDeps{
			Repo:     householdRepo,
			PurgeSvc: householdService,
			Logger:   appLogger,
		})
		appLogger.Info("Household member cleanup check worker created (River)")

		// Issue #1371 — Phase 7 (Task 6): Create household over-capacity check worker
		// (authoritative daily backstop). Same non-nil-by-construction reasoning as
		// Task 2's worker above: it delegates auto-offboard to householdService.
		// OffboardExcessMembers, which is always constructed above.
		householdOverCapacityCheckWorker = jobs.NewHouseholdOverCapacityCheckWorker(jobs.HouseholdOverCapacityCheckWorkerDeps{
			Repo:         householdRepo,
			UserRepo:     profileRepo,
			OffboardSvc:  householdService,
			EmailService: emailService,
			TierConfig:   tierConfig,
			Logger:       appLogger,
		})
		appLogger.Info("Household over-capacity check worker created (River)")

		// Create deletion services (if token secret configured)
		if cfg.DeletionTokenSecret != "" {
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
			// Issue #1367 — Phase 3: block owner deletion while active non-owner members
			// exist, and revoke pending household invites when deletion proceeds. SetDB
			// runs the whole guard atomically under one household row lock (closes a race
			// with an in-flight AcceptInvite — Codex review, High finding 4); it also
			// takes over the (non-transactional) work SetHouseholdReader/
			// SetHouseholdInviteRevoker would otherwise do, but they're still wired for
			// clarity/fallback if db construction ever changes.
			deletionService.SetHouseholdReader(householdRepo)
			deletionService.SetHouseholdInviteRevoker(householdInviteRepo)
			deletionService.SetDB(db)
			// Issue #1413 Critical finding C2: required for ExecuteDeletion to gain
			// EXCLUSIVITY on ActionAccountDeletion before any irreversible Paddle/Plaid
			// cleanup or cascade delete — replaces the old self-assigning ClaimForDeletion
			// guard, which reserved nothing. See work_claims_repository.go's package doc.
			deletionService.SetWorkClaimsRepo(workClaimsRepo)
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
			// Issue #1413 Medium finding M9 (Codex review round 3): required for
			// send30dNotices/send7dNotices to gain EXCLUSIVITY on ActionRetentionNotice30d/7d
			// before a notice email is sent — replaces the old ClaimRetentionNotice30dSent/7dSent,
			// which stamped the *_sent_at marker BEFORE the send was attempted.
			retentionCleanupCheckWorker.SetWorkClaimsRepo(workClaimsRepo)
			appLogger.Info("Retention cleanup check worker created (River)")

			// Issue #633: Create DLQ cleanup worker
			dlqCleanupCheckWorker = jobs.NewDLQCleanupCheckWorker(jobs.DLQCleanupCheckWorkerDeps{
				Repo:          deadLetterJobRepo,
				RetentionDays: cfg.DLQRetentionDays,
				Logger:        appLogger,
			})
			appLogger.Info("DLQ cleanup check worker created (River)")
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
					// Issue #1367 — Phase 3: block an EFFECTIVELY covered household
					// member from starting their own Paddle checkout (decision #9).
					// Uses the resolver (not the raw entitlement repo) so a member is
					// freed to self-subscribe once their owner stops actually covering
					// them, even if the raw entitlement row hasn't been cleaned up yet.
					EntitlementRepo: householdEntitlementService,
					// Issue #1371 Task 5 — in-app downgrade seat guard: blocks a
					// household owner from downgrading to a tier without enough seats
					// for their current active+pending household.
					HouseholdRepo: householdRepo,
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
					// Issue #1371 Task 6 (Part B): best-effort fast-path household
					// over-capacity reconcile after tier-change/cancellation/past-due
					// webhooks. householdRepo/db are already constructed above.
					HouseholdRepo: householdRepo,
					DB:            db,
					Logger:        logger,
				})
				subscriptionWebhookWorker.SetAnalytics(phClient)
				// Issue #1413 High finding H7 (Codex review round 4): required for
				// processSubscriptionCanceled to gain EXCLUSIVITY on
				// ActionSubscriptionCancelledCleanup before its Plaid disconnect / retention-clock
				// side effects — replaces the old plain-read comp check, which reserved nothing.
				subscriptionWebhookWorker.SetWorkClaimsRepo(workClaimsRepo)
				appLogger.Info("Subscription webhook worker created (River)")
			}
		} else {
			appLogger.Warn("Paddle services disabled (PADDLE_API_KEY not configured)")
		}

		// Issue #1006: Wire PaddleService into DeletionService for billing cleanup on deletion.
		// Must run after Paddle init so paddleService is available.
		if paddleService != nil && deletionService != nil {
			deletionService.SetPaddleCleanupService(paddleService)
			appLogger.Info("Paddle cleanup service wired to deletion service")
		}

		// Issue #1015: Create non-Plaid workers unconditionally (decoupled from Plaid config).
		// These workers previously lived inside the Plaid block but have no Plaid dependency.
		// Placement: after Paddle init (paddleService available), before Plaid init.

		// Issue #679, #680: Create TrialService unconditionally.
		// PlaidCleanup is nil initially — wired via SetPlaidCleanup() if Plaid is configured.
		// ExpireTrial already nil-guards plaidCleanup before calling it.
		trialService = services.NewTrialService(tierConfig, profileRepo, nil)
		trialService.SetRetentionMonths(cfg.RetentionPeriodMonths)
		// #1367 — Phase 3: exempt actively covered household members from trial expiry.
		trialService.SetHouseholdChecker(householdEntitlementService)
		// Issue #1413 round-4 finding H2: required for ExpireTrial to gain EXCLUSIVITY on
		// ActionTrialExpiration before its Plaid disconnect — trial_service was the one
		// destructive flow in this branch with no work_claims flow claim at all.
		trialService.SetWorkClaimsRepo(workClaimsRepo)
		appLogger.Info("Trial service initialized")

		// Issue #898: Create PastDueService unconditionally; plaidCleanup wired later if Plaid is configured.
		pastDueCfg := tierConfig.GetPastDueConfig()
		pastDueService := services.NewPastDueService(
			profileRepo,
			nil, // plaidCleanup — wired via SetPlaidCleanup() if Plaid is configured
			paddleService,
			clerkService,
			notificationRepo,
			pastDueCfg.GraceDays,
			pastDueCfg.PlaidDisconnectDays,
		)
		pastDueService.SetAnalytics(phClient)
		// #1413: exempt actively covered household members from past_due dunning, closing an
		// asymmetry with TrialService's #1367 household exemption (wired identically above).
		pastDueService.SetHouseholdChecker(householdEntitlementService)
		// Issue #1413 High findings H6a/H6b (Codex review round 4): required for
		// processPlaidDisconnectWindow/processGraceExpiry to gain EXCLUSIVITY on
		// ActionPlaidDisconnectPastDue/ActionPastDueGraceCancel before any irreversible Plaid/Paddle
		// call — replaces the old self-assigning-update guards, which reserved nothing.
		pastDueService.SetWorkClaimsRepo(workClaimsRepo)
		appLogger.Info("Past-due service initialized",
			"grace_days", pastDueCfg.GraceDays,
			"plaid_disconnect_days", pastDueCfg.PlaidDisconnectDays,
		)

		// Issue #679: Wire trial service to registration service for auto-grant on signup.
		// Auto-grant now runs regardless of Plaid config (confirmed intended behavior #1015).
		registrationService.SetTrialGranter(trialService)
		appLogger.Info("Trial service wired to registration service")

		// Issue #679, #680: Create trial expiration check worker (depends only on trialService)
		trialExpirationCheckWorker = jobs.NewTrialExpirationCheckWorker(jobs.TrialExpirationCheckWorkerDeps{
			ProfileRepo:  profileRepo,
			TrialService: trialService,
			Logger:       appLogger,
		})
		appLogger.Info("Trial expiration check worker created (River)")

		// Issue #898: Create past-due check worker (depends on pastDueService, created unconditionally above)
		pastDueCheckWorker = jobs.NewPastDueCheckWorker(jobs.PastDueCheckWorkerDeps{
			ProfileRepo:    profileRepo,
			PastDueService: pastDueService,
			Logger:         appLogger,
		})
		appLogger.Info("Past-due check worker created (River)")

		// Issue #762: Create Paddle reconciliation worker (depends only on paddleService, not Plaid)
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

		// Issue #1007: Create EmailChangePropagationWorker (no Plaid dependency).
		// Issue #1015: Moved before Plaid block so it runs unconditionally.
		emailChangePropagationWorker = jobs.NewEmailChangePropagationWorker(jobs.EmailChangePropagationWorkerDeps{
			ProfileRepo:   profileRepo,
			ClerkService:  clerkService,
			PaddleService: paddleService, // nil if Paddle not configured — worker nil-checks before calling
			Logger:        appLogger,
		})
		appLogger.Info("Email change propagation worker created (River)")
		emailChangePropagationWorker.SetAnalytics(phClient)

		// Issue #1015: Create rule evaluation worker (depends only on boostExecutionService, no Plaid dependency)
		boostEvaluationWorker = jobs.NewBoostEvaluationWorker(jobs.BoostEvaluationWorkerDeps{
			BoostExecutionService: boostExecutionService,
			Logger:                appLogger,
		})
		appLogger.Info("Boost evaluation worker created (River)")

		// Issue #972: Create budget period close check worker (daily scanner, no Plaid dependency)
		budgetPeriodCloseCheckWorker = jobs.NewBudgetPeriodCloseCheckWorker(jobs.BudgetPeriodCloseCheckWorkerDeps{
			BudgetRepo:                budgetRepo,
			PeriodEventsRepo:          repository.NewBudgetPeriodEventsRepository(db),
			JobQueue:                  nil,                    // Set after River client created (circular dependency)
			TransferSuggestionExpirer: transferSuggestionRepo, // Issue #972 GAP E: expire stale pending suggestions
			UserTimezones:             budgetRepo,             // Issue #972 Task 2e: per-user timezone for period-boundary detection
			Logger:                    appLogger,
		})
		appLogger.Info("Budget period close check worker created (River)")

		// Issue #972: Create savings stats snapshot worker (daily aggregate, no Plaid dependency)
		// Share one repo instance — both fields satisfy different narrow interfaces from the same concrete type.
		// Pass fieldEncryptor (may be nil if ClerkSecretKey is unset) — the stats worker only calls
		// WriteSnapshot/GetAggregateSavingsStats, which do not use the encryptor; it is included
		// here for interface consistency and future calls that may resolve account names.
		savingsImpactStatsRepo := repository.NewSavingsImpactRepositoryWithEncryptor(db, fieldEncryptor)
		savingsStatsWorker = jobs.NewSavingsStatsWorker(jobs.SavingsStatsWorkerDeps{
			AggregateRepo:     savingsImpactStatsRepo,
			SavingsImpactRepo: savingsImpactStatsRepo,
			Logger:            appLogger,
		})
		appLogger.Info("Savings stats snapshot worker created (River)")

		// Issue #138: Create portfolio snapshot worker (daily per-user snapshot).
		portfolioSnapshotWorker = jobs.NewPortfolioSnapshotWorker(jobs.PortfolioSnapshotWorkerDeps{
			HoldingRepo:  holdingRepo,
			SnapshotRepo: portfolioSnapshotRepo,
			Logger:       appLogger,
		})
		appLogger.Info("Portfolio snapshot worker created (River)")

		// Issue #1123: Create effective category refresh worker (no external dependencies)
		effectiveCategoryRefreshWorker = jobs.NewEffectiveCategoryRefreshWorker(jobs.EffectiveCategoryRefreshWorkerDeps{
			TransactionRepo: transactionRepo,
			Logger:          appLogger,
		})
		appLogger.Info("Effective category refresh worker created (River)")

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
					linkedItemRepo,
					accountRepo,
					profileRepo,
					db,
					services.WithEncryptor(fieldEncryptor), // Issue #1010: Cross-user fingerprint checks
				)

				appLogger.Info("Plaid service initialized")
				appLogger.Info("Account linking service initialized")

				// Issue #668: Create PlaidItemCleanupService for Plaid item lifecycle management
				// This service handles calling Plaid /item/remove to stop billing
				plaidCleanupService = services.NewPlaidItemCleanupService(
					linkedItemRepo,
					plaidService,
				)
				// Issue #1413 H1: required for RemoveAllUserItems/DisconnectAllUserItems to gain
				// EXCLUSIVITY on ActionPlaidDisconnectUser before any Plaid /item/remove call —
				// closes the race where deletion, past-due dunning, the subscription-cancellation
				// webhook, and trial expiration (all sharing this one service instance) could each
				// hold their own, different flow claim and disconnect the same user's Plaid items
				// concurrently.
				plaidCleanupService.SetWorkClaimsRepo(workClaimsRepo)
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

				// Issue #1015: Wire Plaid cleanup to trial service (created before Plaid block with nil cleanup)
				trialService.SetPlaidCleanup(plaidCleanupService)
				appLogger.Info("Plaid cleanup service wired to trial service")

				// Issue #898: Wire Plaid cleanup to past-due service (created before Plaid block with nil cleanup)
				pastDueService.SetPlaidCleanup(plaidCleanupService)
				appLogger.Info("Plaid cleanup service wired to past-due service")

				// Issue #1413 round-6: wire Plaid cleanup to the retention cleanup check worker's
				// Plaid-disconnect repair pass (created before this Plaid block, with nil cleanup —
				// same late-binding pattern as trial/past-due service above).
				if retentionCleanupCheckWorker != nil {
					retentionCleanupCheckWorker.SetPlaidCleanup(plaidCleanupService)
					appLogger.Info("Plaid cleanup service wired to retention cleanup check worker")
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
					merchantRepo, // Issue #1163: Merchant batch-upsert
					appLogger,
				)

				// Issue #972: Initialize TransferCorrelationService and wire into sync pipeline (B4)
				transferCorrelationService := services.NewTransferCorrelationService(
					transferSuggestionRepo,
					transactionRepo,
					notificationRepo,
				)
				if phClient != nil {
					transferCorrelationService.SetAnalytics(phClient)
				}
				// Issue #1300 M19: Wire D6 source-c boost-match → confirmed contribution
				// savingsGoalRepo.GetActiveByUserID satisfies CorrelationGoalResolver
				// savingsContribRepo.UpsertFromTransaction satisfies CorrelationConfirmedContributionUpserter
				transferCorrelationService.SetBoostMatchContributionServices(
					savingsGoalRepo,    // CorrelationGoalResolver: GetActiveByUserID
					savingsContribRepo, // CorrelationConfirmedContributionUpserter: UpsertFromTransaction
				)
				appLogger.Info("Boost-match contribution services wired into transfer correlation (M19 D6 source-c)")
				transactionSyncService.SetTransferCorrelationService(transferCorrelationService)
				appLogger.Info("Transfer correlation service wired into transaction sync")

				// Issue #1300 M12: Wire savings progress reconciliation into transaction sync
				// ordering: DeleteManaged (for removed txns) → RecomputeUser → BackfillManagedCategories
				// → RecalculateBudgetAmounts (which reads savings "spent" from the ledger)
				transactionSyncService.SetSavingsProgressServices(
					savingsContribRepo,     // SyncContributionCleaner: DeleteManagedForTransaction
					savingsProgressService, // SyncGoalProgressUpdater: RecomputeUser
					budgetService,          // SyncManagedCategoryBackfiller: BackfillManagedCategories
				)
				appLogger.Info("Savings progress services wired into transaction sync")

				// Issue #1300 M18: Wire D6 source-b proposed-contribution detection into sync
				// ordering: detectAndUpsertProposedContributions → RecomputeUser
				// savingsGoalRepo implements SyncContributionsBasisGoalReader (GetActiveByUserID)
				// savingsContribRepo implements SyncProposedContributionUpserter (UpsertProposedFromTransaction)
				// transactionRepo implements GetInflowTransactionsForAccount (37-day window)
				transactionSyncService.SetSavingsDetectionServices(
					savingsGoalRepo,    // SyncContributionsBasisGoalReader: GetActiveByUserID
					savingsContribRepo, // SyncProposedContributionUpserter: UpsertProposedFromTransaction
					transactionRepo,    // TransactionRepository: GetInflowTransactionsForAccount
				)
				appLogger.Info("Savings detection services wired into transaction sync (M18 D6 source-b)")

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
					portfolioSnapshotRepo,
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

				// Create WebhookProcessingWorker with all dependencies
				// Issue #659: InvestmentSyncSvc added for HOLDINGS and INVESTMENTS_TRANSACTIONS routing
				// Issue #910: LiabilitySyncSvc added for LIABILITIES routing
				// Issue #1015: Use = (not :=) — these vars are declared at outer scope for post-Plaid River registration
				webhookProcessingWorker = jobs.NewWebhookProcessingWorker(jobs.WebhookProcessingWorkerDeps{
					WebhookRepo:        webhookRepo,
					TransactionSyncSvc: transactionSyncService,
					InvestmentSyncSvc:  investmentSyncService,
					LiabilitySyncSvc:   liabilitySyncService,
					ItemErrorSvc:       itemErrorService,
					Logger:             appLogger,
				})

				// Issue #666: Create PlaidSyncWorker for auto-sync after account linking
				// Issue #171: Add DB for row-level locking to prevent concurrent syncs
				plaidSyncWorker = jobs.NewPlaidSyncWorker(jobs.PlaidSyncWorkerDeps{
					SyncService:    transactionSyncService,
					LinkedItemRepo: linkedItemRepo,
					JobQueue:       nil, // Set after River client created (circular dependency)
					Logger:         appLogger,
					DB:             db, // For row-level locking via FOR UPDATE
				})
				appLogger.Info("Plaid sync worker created")

				// Issue #659: Create InvestmentHoldingsSyncWorker
				investmentHoldingsSyncWorker = jobs.NewInvestmentHoldingsSyncWorker(jobs.InvestmentHoldingsSyncWorkerDeps{
					InvestmentSyncSvc: investmentSyncService,
					LinkedItemRepo:    linkedItemRepo,
					Logger:            appLogger,
					DB:                db,
				})
				appLogger.Info("Investment holdings sync worker created")

				// Issue #659: Create InvestmentTransactionsSyncWorker
				investmentTransactionsSyncWorker = jobs.NewInvestmentTransactionsSyncWorker(jobs.InvestmentTransactionsSyncWorkerDeps{
					InvestmentSyncSvc: investmentSyncService,
					LinkedItemRepo:    linkedItemRepo,
					Logger:            appLogger,
					DB:                db,
				})
				appLogger.Info("Investment transactions sync worker created")

				// Issue #668: Create ItemCleanupCheckWorker for periodic orphaned item detection
				// Checks for items in error state, revoked, stale, or with zero accounts
				itemCleanupCheckWorker = jobs.NewItemCleanupCheckWorker(jobs.ItemCleanupCheckWorkerDeps{
					LinkedItemRepo:     linkedItemRepo,
					AccountRepo:        accountRepo, // For zero-account detection
					JobQueue:           nil,         // Set after River client created (circular dependency)
					Logger:             appLogger,
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
					Logger:          appLogger,
				})
				appLogger.Info("Item cleanup worker created")

				appLogger.Info("Transaction sync service initialized")
				appLogger.Info("Item error service initialized")
			}
		}

		// ═══════════════════════════════════════════════════════════════════════
		// Issue #1015: River queue initialization — decoupled from Plaid config.
		// River runs whenever DB + Clerk are available. Plaid-specific workers are
		// nil when Plaid is not configured and are silently skipped via nil-checks.
		// ═══════════════════════════════════════════════════════════════════════
		{
			riverConfig := riverqueue.DefaultConfig()

			// Configure dead letter handler for failed jobs
			deadLetterHandler := riverqueue.NewDeadLetterHandler(riverqueue.DeadLetterHandlerDeps{
				DB:     db,
				Logger: appLogger,
			})
			deadLetterHandler.SetAnalytics(phClient)
			riverConfig.ErrorHandler = deadLetterHandler.ErrorHandlerFunc()
			appLogger.Info("Dead letter handler configured for failed jobs")

			// Configure periodic jobs (all nil-guarded)
			var periodicJobs []*river.PeriodicJob

			// Scheduled deletion check (hourly)
			if scheduledDeletionCheckWorker != nil {
				periodicJobs = append(periodicJobs, river.NewPeriodicJob(
					river.PeriodicInterval(1*time.Hour),
					func() (river.JobArgs, *river.InsertOpts) {
						return jobs.ScheduledDeletionCheckArgs{
								TriggerSource: "periodic",
							}, &river.InsertOpts{
								Queue: "low",
							}
					},
					&river.PeriodicJobOpts{RunOnStart: true},
				))
				appLogger.Info("Scheduled deletion check periodic job configured (hourly)")
			}

			// Issue #668: Item cleanup check (daily) — Plaid-specific, nil-guarded
			if itemCleanupCheckWorker != nil {
				periodicJobs = append(periodicJobs, river.NewPeriodicJob(
					river.PeriodicInterval(24*time.Hour),
					func() (river.JobArgs, *river.InsertOpts) {
						return jobs.ItemCleanupCheckArgs{
								TriggerSource: "periodic",
							}, &river.InsertOpts{
								Queue: "low",
							}
					},
					&river.PeriodicJobOpts{RunOnStart: false},
				))
				appLogger.Info("Item cleanup check periodic job configured (daily)")
			}

			// Issue #679, #680: Trial expiration check (daily)
			if trialExpirationCheckWorker != nil {
				periodicJobs = append(periodicJobs, river.NewPeriodicJob(
					river.PeriodicInterval(24*time.Hour),
					func() (river.JobArgs, *river.InsertOpts) {
						return jobs.TrialExpirationCheckArgs{
								TriggerSource: "periodic",
							}, &river.InsertOpts{
								Queue: "low",
							}
					},
					&river.PeriodicJobOpts{RunOnStart: true},
				))
				appLogger.Info("Trial expiration check periodic job configured (daily)")
			}

			// Issue #898: Past-due check (daily) — disconnects Plaid at 14d and auto-cancels at 21d
			if pastDueCheckWorker != nil {
				periodicJobs = append(periodicJobs, river.NewPeriodicJob(
					river.PeriodicInterval(24*time.Hour),
					func() (river.JobArgs, *river.InsertOpts) {
						return jobs.PastDueCheckArgs{
								TriggerSource: "periodic",
							}, &river.InsertOpts{
								Queue: "low",
							}
					},
					&river.PeriodicJobOpts{RunOnStart: true},
				))
				appLogger.Info("Past-due check periodic job configured (daily)")
			}

			// Issue #762: Paddle reconciliation check (daily)
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

			// Issue #930: Retention cleanup check (daily)
			// Issue #1413: RetentionCleanupCheckUniqueOpts (retention_cleanup_check.go)
			// documents the same shape below as the source of truth this must stay in sync
			// with. UniqueOpts constrains to one check job in any non-terminal state AT
			// ALL within a 23-hour window (job-level defense in depth only — the real
			// correctness mechanism is ExecuteDeletion's work_claims claim, not this) so
			// concurrent River clients/schedulers can't double-run the daily check.
			if retentionCleanupCheckWorker != nil {
				periodicJobs = append(periodicJobs, river.NewPeriodicJob(
					river.PeriodicInterval(24*time.Hour),
					func() (river.JobArgs, *river.InsertOpts) {
						return jobs.RetentionCleanupCheckArgs{
								TriggerSource: "periodic",
							}, &river.InsertOpts{
								Queue: "low",
								UniqueOpts: river.UniqueOpts{
									ByArgs:   false,
									ByPeriod: 23 * time.Hour,
									ByState: []rivertype.JobState{
										rivertype.JobStateAvailable,
										rivertype.JobStateScheduled,
										rivertype.JobStateRunning,
										rivertype.JobStateRetryable,
										rivertype.JobStatePending,
									},
								},
							}
					},
					&river.PeriodicJobOpts{RunOnStart: false},
				))
				appLogger.Info("Retention cleanup check periodic job configured (daily)")
			}

			// Issue #1413 (Task 9): Manual tier override expiry check (daily) — the
			// time-triggered twin of the admin "remove override" endpoint (Issue #1414 /
			// Task 7). HasActiveManualOverride() derives comp state at read time only, so
			// nothing else re-arms retention / re-syncs Clerk / audits a comp that lapses on
			// its own without an admin manually clearing it.
			//
			// Issue #1413 G3: InsertOpts (Queue + UniqueOpts) is built by
			// jobs.ManualTierOverrideExpiryCheckInsertOpts() rather than hand-written here —
			// a previous version of this literal omitted UniqueOpts entirely, silently
			// leaving ManualTierOverrideExpiryCheckUniqueOpts()'s documented dedup
			// unenforced. That job is the ONLY thing bounding how long
			// noManualOverrideAtAllPredicate keeps a user out of the retention pipeline
			// (profile_repository.go), so duplicate or missed runs directly affect whether
			// accounts are ever deleted.
			if manualTierOverrideExpiryCheckWorker != nil {
				periodicJobs = append(periodicJobs, river.NewPeriodicJob(
					river.PeriodicInterval(24*time.Hour),
					func() (river.JobArgs, *river.InsertOpts) {
						return jobs.ManualTierOverrideExpiryCheckArgs{
							TriggerSource: "periodic",
						}, jobs.ManualTierOverrideExpiryCheckInsertOpts()
					},
					&river.PeriodicJobOpts{RunOnStart: false},
				))
				appLogger.Info("Manual tier override expiry check periodic job configured (daily)")
			}

			// Issue #1371 — Phase 7 (Task 2): Household member cleanup check (daily)
			// HouseholdMemberCleanupCheckUniqueOpts (household_member_cleanup_check.go)
			// documents the same shape below as the source of truth this must stay in
			// sync with. UniqueOpts constrains to one check job in any non-terminal
			// state AT ALL (no ByPeriod time window — see that function's doc comment
			// for the Round 8 rationale) so that concurrent River clients/schedulers
			// can't double-run the check and race to purge the same removed-membership
			// rows twice (this worker only purges rows — no email is sent here, unlike
			// its over-capacity sibling below).
			if householdMemberCleanupCheckWorker != nil {
				periodicJobs = append(periodicJobs, river.NewPeriodicJob(
					river.PeriodicInterval(24*time.Hour),
					func() (river.JobArgs, *river.InsertOpts) {
						return jobs.HouseholdMemberCleanupCheckArgs{
								TriggerSource: "periodic",
							}, &river.InsertOpts{
								Queue: "low",
								UniqueOpts: river.UniqueOpts{
									ByArgs: false,
									ByState: []rivertype.JobState{
										rivertype.JobStateAvailable,
										rivertype.JobStateScheduled,
										rivertype.JobStateRunning,
										rivertype.JobStateRetryable,
										rivertype.JobStatePending,
									},
								},
							}
					},
					&river.PeriodicJobOpts{RunOnStart: false},
				))
				appLogger.Info("Household member cleanup check periodic job configured (daily)")
			}

			// Issue #1371 — Phase 7 (Task 6): Household over-capacity check (daily)
			// HouseholdOverCapacityCheckUniqueOpts (household_over_capacity_check.go)
			// documents the same shape below as the source of truth this must stay in
			// sync with. UniqueOpts constrains to one check job in any non-terminal
			// state AT ALL (no ByPeriod time window — see that function's doc comment
			// for the Round 8 rationale) — this worker sends a notice email BEFORE
			// stamping *_sent_at (so a failed send retries next run), and two
			// overlapping runs would both see "not yet sent" and both send a
			// duplicate email.
			if householdOverCapacityCheckWorker != nil {
				periodicJobs = append(periodicJobs, river.NewPeriodicJob(
					river.PeriodicInterval(24*time.Hour),
					func() (river.JobArgs, *river.InsertOpts) {
						return jobs.HouseholdOverCapacityCheckArgs{
								TriggerSource: "periodic",
							}, &river.InsertOpts{
								Queue: "low",
								UniqueOpts: river.UniqueOpts{
									ByArgs: false,
									ByState: []rivertype.JobState{
										rivertype.JobStateAvailable,
										rivertype.JobStateScheduled,
										rivertype.JobStateRunning,
										rivertype.JobStateRetryable,
										rivertype.JobStatePending,
									},
								},
							}
					},
					&river.PeriodicJobOpts{RunOnStart: false},
				))
				appLogger.Info("Household over-capacity check periodic job configured (daily)")
			}

			// Issue #633: DLQ cleanup check (daily)
			if dlqCleanupCheckWorker != nil {
				periodicJobs = append(periodicJobs, river.NewPeriodicJob(
					river.PeriodicInterval(24*time.Hour),
					func() (river.JobArgs, *river.InsertOpts) {
						return jobs.DLQCleanupCheckArgs{
								TriggerSource: "periodic",
							}, &river.InsertOpts{
								Queue: "low",
							}
					},
					&river.PeriodicJobOpts{RunOnStart: false},
				))
				appLogger.Info("DLQ cleanup check periodic job configured (daily)")
			}

			// Issue #972: Budget period close check (daily) — detects closed budget periods and fires events
			if budgetPeriodCloseCheckWorker != nil {
				periodicJobs = append(periodicJobs, river.NewPeriodicJob(
					river.PeriodicInterval(24*time.Hour),
					func() (river.JobArgs, *river.InsertOpts) {
						return jobs.BudgetPeriodCloseCheckArgs{
								TriggerSource: "periodic",
							}, &river.InsertOpts{
								Queue: queue.QueueLow,
							}
					},
					&river.PeriodicJobOpts{RunOnStart: false},
				))
				appLogger.Info("Budget period close check periodic job configured (daily)")
			}

			// Issue #972: Savings stats snapshot (daily) — aggregate completed transfer amounts.
			// SavingsStatsUniqueOpts constrains to one snapshot job per 24h period (queue-level
			// dedup on top of the ON CONFLICT upsert in WriteSnapshot). We use river.UniqueOpts
			// directly because the periodic job constructor takes *river.InsertOpts, not queue opts.
			if savingsStatsWorker != nil {
				periodicJobs = append(periodicJobs, river.NewPeriodicJob(
					river.PeriodicInterval(24*time.Hour),
					func() (river.JobArgs, *river.InsertOpts) {
						return jobs.SavingsStatsArgs{
								TriggerSource: "periodic",
							}, &river.InsertOpts{
								Queue: queue.QueueLow,
								UniqueOpts: river.UniqueOpts{
									ByPeriod: 24 * time.Hour,
								},
							}
					},
					&river.PeriodicJobOpts{RunOnStart: false},
				))
				appLogger.Info("Savings stats snapshot periodic job configured (daily)")
			}

			// Issue #138: Portfolio snapshot (daily) — per-user holdings aggregate upsert.
			// UniqueOpts constrains to one snapshot job per 24h period (on top of ON CONFLICT upsert).
			if portfolioSnapshotWorker != nil {
				periodicJobs = append(periodicJobs, river.NewPeriodicJob(
					river.PeriodicInterval(24*time.Hour),
					func() (river.JobArgs, *river.InsertOpts) {
						return jobs.PortfolioSnapshotArgs{
								TriggerSource: "periodic",
							}, &river.InsertOpts{
								Queue: queue.QueueLow,
								UniqueOpts: river.UniqueOpts{
									ByPeriod: 24 * time.Hour,
								},
							}
					},
					&river.PeriodicJobOpts{RunOnStart: false},
				))
				appLogger.Info("Portfolio snapshot periodic job configured (daily)")
			}

			// Issue #1087 R4-7: Idempotency keys cleanup (daily — deletes expired rows)
			periodicJobs = append(periodicJobs, river.NewPeriodicJob(
				river.PeriodicInterval(24*time.Hour),
				func() (river.JobArgs, *river.InsertOpts) {
					return jobs.IdempotencyCleanupArgs{
							TriggerSource: "periodic",
						}, &river.InsertOpts{
							Queue: "low",
						}
				},
				&river.PeriodicJobOpts{RunOnStart: false},
			))
			appLogger.Info("Idempotency cleanup periodic job configured (daily)")

			if len(periodicJobs) > 0 {
				riverConfig.PeriodicJobs = periodicJobs
			}

			// Issue #1087 R5-2: Construct idempotency cleanup worker here (before river.NewWorkers)
			// so it is non-nil at river.AddWorker time. The var is declared at the top of main().
			// db is available from this point; the repo is a cheap wrapper over the same pool.
			idempotencyCleanupWorker = jobs.NewIdempotencyCleanupWorker(jobs.IdempotencyCleanupWorkerDeps{
				Repo:   repository.NewIdempotencyRepository(db),
				Logger: appLogger,
			})
			appLogger.Info("Idempotency cleanup worker created (River)")

			// Register ALL workers with nil-guards.
			// Non-Plaid workers are always non-nil when their config gates are met.
			// Plaid-specific workers are nil when Plaid is not configured.
			riverWorkers := river.NewWorkers()

			// Non-Plaid workers (run unconditionally when configured)
			if emailChangePropagationWorker != nil {
				river.AddWorker(riverWorkers, emailChangePropagationWorker)
				appLogger.Info("Email change propagation worker registered with River")
			}
			if accountDeletionWorker != nil {
				river.AddWorker(riverWorkers, accountDeletionWorker)
				appLogger.Info("Account deletion worker registered with River")
			}
			if scheduledDeletionCheckWorker != nil {
				river.AddWorker(riverWorkers, scheduledDeletionCheckWorker)
				appLogger.Info("Scheduled deletion check worker registered with River")
			}
			if trialExpirationCheckWorker != nil {
				river.AddWorker(riverWorkers, trialExpirationCheckWorker)
				appLogger.Info("Trial expiration check worker registered with River")
			}
			if pastDueCheckWorker != nil {
				river.AddWorker(riverWorkers, pastDueCheckWorker)
				appLogger.Info("Past-due check worker registered with River")
			}
			if paddleReconciliationWorker != nil {
				river.AddWorker(riverWorkers, paddleReconciliationWorker)
				appLogger.Info("Paddle reconciliation worker registered with River")
			}
			if retentionCleanupCheckWorker != nil {
				river.AddWorker(riverWorkers, retentionCleanupCheckWorker)
				appLogger.Info("Retention cleanup check worker registered with River")
			}
			if manualTierOverrideExpiryCheckWorker != nil {
				river.AddWorker(riverWorkers, manualTierOverrideExpiryCheckWorker)
				appLogger.Info("Manual tier override expiry check worker registered with River")
			}
			// Issue #1371 — Phase 7 (Task 2): Household member cleanup check worker
			// (constructed above, always non-nil here)
			if householdMemberCleanupCheckWorker != nil {
				river.AddWorker(riverWorkers, householdMemberCleanupCheckWorker)
				appLogger.Info("Household member cleanup check worker registered with River")
			}
			// Issue #1371 — Phase 7 (Task 6): Household over-capacity check worker
			// (constructed above, always non-nil here)
			if householdOverCapacityCheckWorker != nil {
				river.AddWorker(riverWorkers, householdOverCapacityCheckWorker)
				appLogger.Info("Household over-capacity check worker registered with River")
			}
			if dlqCleanupCheckWorker != nil {
				river.AddWorker(riverWorkers, dlqCleanupCheckWorker)
				appLogger.Info("DLQ cleanup check worker registered with River")
			}
			// Issue #1087 R4-7/R5-2: Idempotency cleanup worker (constructed above, always non-nil here)
			if idempotencyCleanupWorker != nil {
				river.AddWorker(riverWorkers, idempotencyCleanupWorker)
				appLogger.Info("Idempotency cleanup worker registered with River")
			}
			if subscriptionWebhookWorker != nil {
				river.AddWorker(riverWorkers, subscriptionWebhookWorker)
				appLogger.Info("Paddle subscription webhook worker registered with River")
			}
			if boostEvaluationWorker != nil {
				river.AddWorker(riverWorkers, boostEvaluationWorker)
				appLogger.Info("Rule evaluation worker registered with River")
			}
			if budgetPeriodCloseCheckWorker != nil {
				river.AddWorker(riverWorkers, budgetPeriodCloseCheckWorker)
				appLogger.Info("Budget period close check worker registered with River")
			}
			if savingsStatsWorker != nil {
				river.AddWorker(riverWorkers, savingsStatsWorker)
				appLogger.Info("Savings stats snapshot worker registered with River")
			}
			if portfolioSnapshotWorker != nil {
				river.AddWorker(riverWorkers, portfolioSnapshotWorker)
				appLogger.Info("Portfolio snapshot worker registered with River")
			}
			if effectiveCategoryRefreshWorker != nil {
				river.AddWorker(riverWorkers, effectiveCategoryRefreshWorker)
				appLogger.Info("Effective category refresh worker registered with River")
			}

			// Plaid-specific workers (nil when Plaid not configured)
			if webhookProcessingWorker != nil {
				river.AddWorker(riverWorkers, webhookProcessingWorker)
				appLogger.Info("Webhook processing worker registered with River")
			}
			if plaidSyncWorker != nil {
				river.AddWorker(riverWorkers, plaidSyncWorker)
				appLogger.Info("Plaid sync worker registered with River")
			}
			if investmentHoldingsSyncWorker != nil {
				river.AddWorker(riverWorkers, investmentHoldingsSyncWorker)
				appLogger.Info("Investment holdings sync worker registered with River")
			}
			if investmentTransactionsSyncWorker != nil {
				river.AddWorker(riverWorkers, investmentTransactionsSyncWorker)
				appLogger.Info("Investment transactions sync worker registered with River")
			}
			if itemCleanupCheckWorker != nil {
				river.AddWorker(riverWorkers, itemCleanupCheckWorker)
				appLogger.Info("Item cleanup check worker registered with River")
			}
			if itemCleanupWorker != nil {
				river.AddWorker(riverWorkers, itemCleanupWorker)
				appLogger.Info("Item cleanup worker registered with River")
			}

			// Create River client and wire all SetJobQueue dependencies
			riverClient, err := riverqueue.NewClient(db, riverWorkers, riverConfig)
			if err != nil {
				appLogger.Warn("Failed to initialize River queue", "error", err)
			} else {
				jobQueue = riverClient
				riverRunner = riverClient
				appLogger.Info("River queue initialized")

				// Unconditional — the core fix of Issue #1015
				profileService.SetJobQueue(jobQueue)
				appLogger.Info("Profile service connected to job queue for email propagation")

				// Nil-guarded wiring for workers with circular dependencies
				if scheduledDeletionCheckWorker != nil {
					scheduledDeletionCheckWorker.SetJobQueue(jobQueue)
					appLogger.Info("Scheduled deletion check worker connected to job queue")
				}
				if accountService != nil {
					accountService.SetJobQueue(jobQueue)
					appLogger.Info("Account service connected to job queue for auto-sync")
				}
				if plaidSyncWorker != nil {
					plaidSyncWorker.SetJobQueue(jobQueue)
					appLogger.Info("Plaid sync worker connected to job queue for follow-up jobs")
				}
				if itemCleanupCheckWorker != nil {
					itemCleanupCheckWorker.SetJobQueue(jobQueue)
					appLogger.Info("Item cleanup check worker connected to job queue")
				}
				if budgetPeriodCloseCheckWorker != nil {
					budgetPeriodCloseCheckWorker.SetJobQueue(jobQueue)
					appLogger.Info("Budget period close check worker connected to job queue")
				}

				// Issue #1123: Wire job queue for async effective_category_name refresh
				customCategoryService.SetJobQueue(jobQueue)
				appLogger.Info("Custom category service connected to job queue for rename cascade")
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

		// Apply Huma Clerk middleware for authenticated Huma routes
		// #930: Pass profileRepo so the middleware stores subscription_status in context
		humaClerkMiddleware := humamiddleware.NewClerkMiddleware(
			cfg.ClerkSecretKey,
			identityRepo,
			registrationService,
			appLogger,
			// #1247: MCP-origin service auth — trusted Worker (X-MCP-Worker-Secret)
			// + Clerk-verified OAuth access token. Disabled if MCPWorkerSecret is empty.
			&humamiddleware.MCPAuthConfig{
				WorkerSecret: cfg.MCPWorkerSecret,
				Verifier:     humamiddleware.NewClerkOAuthVerifier(cfg.ClerkSecretKey),
			},
			profileRepo,
		)
		humaAPI.UseMiddleware(humaClerkMiddleware)
		appLogger.Info("Huma Clerk middleware applied")

		// #930: Apply read-only enforcement middleware — blocks mutations for inactive/cancelled subscriptions
		// #1367 — Phase 3: household-aware — a covered member's own stale read-only status no longer locks them out.
		humaAPI.UseMiddleware(humamiddleware.NewReadOnlyMiddleware(householdEntitlementService))
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
		// Issue #1414: clerkService re-syncs Clerk metadata on tier-override removal;
		// RetentionPeriodMonths sizes the retention-clock re-arm (same value trialService.SetRetentionMonths gets).
		// Issue #1413: tierConfig validates manual tier overrides and sizes the household seat-capacity warning.
		adminUserService := services.NewAdminUserService(adminUserRepo, adminService, clerkService, cfg.RetentionPeriodMonths, tierConfig)

		// Issue #640: System health monitoring service
		systemHealthService := services.NewSystemHealthService(
			systemHealthRepo,
			deadLetterJobRepo,
			&dbPoolAdapter{db},
			cfg.PlaidEnv,
		)

		// Issue #633: Dead letter queue admin service
		deadLetterService := services.NewDeadLetterService(deadLetterJobRepo, jobQueue)

		// Apply Huma Admin middleware for admin routes (Issue #309)
		humaAdminMiddleware := humamiddleware.NewAdminMiddleware(cfg, adminService)
		humaAPI.UseMiddleware(humaAdminMiddleware)
		appLogger.Info("Huma Admin middleware applied")

		// Shared idempotency repository (Issue #1087: MCP server — R4-5 wiring)
		humaIdempotencyRepo := repository.NewIdempotencyRepository(db)
		humaMCPAuditRepo := repository.NewMCPAuditRepository(db)

		// Register Huma tag routes (Issue #345: User tag management)
		humaTagHandler := operations.NewTagHandler(userTagService, operations.TagHandlerDeps{
			IdempotencyRepo: humaIdempotencyRepo,
			DB:              db,
			MCPAuditRepo:    humaMCPAuditRepo,
			MCPWorkerSecret: cfg.MCPWorkerSecret,
		})
		operations.RegisterTagRoutes(humaAPI, humaTagHandler)
		appLogger.Info("Huma tag routes registered")

		// Register Huma transaction routes (Issue #559: Huma migration)
		humaTransactionHandler := operations.NewTransactionHandler(transactionService, operations.TransactionHandlerDeps{
			DB:              db,
			MCPAuditRepo:    humaMCPAuditRepo,
			MCPWorkerSecret: cfg.MCPWorkerSecret,
		})
		operations.RegisterTransactionRoutes(humaAPI, humaTransactionHandler)
		appLogger.Info("Huma transaction routes registered")

		// Register Huma user account routes (Issue #171; fixes #1247).
		// These were registered for OpenAPI spec generation (RegisterAllRoutesForSpec)
		// but never wired into the runtime server, so /api/v1/user-accounts 404'd even
		// though the generated spec + MCP Worker advertised it.
		userAccountRepo := repository.NewUserAccountRepository(db, fieldEncryptor)
		userAccountService := services.NewUserAccountService(userAccountRepo)
		operations.RegisterUserAccountRoutes(humaAPI, operations.NewUserAccountHandler(userAccountService))
		appLogger.Info("Huma user account routes registered")

		// Register Huma category routes (Issue #559: Huma migration)
		humaCategoryHandler := operations.NewCategoryHandler(customCategoryService, operations.CategoryHandlerDeps{
			IdempotencyRepo: humaIdempotencyRepo,
			DB:              db,
			MCPAuditRepo:    humaMCPAuditRepo,
			MCPWorkerSecret: cfg.MCPWorkerSecret,
		})
		operations.RegisterCategoryRoutes(humaAPI, humaCategoryHandler)
		appLogger.Info("Huma category routes registered")

		// Register Huma categorization rule routes (Issue #559: Huma migration)
		humaRuleHandler := operations.NewCategorizationRuleHandler(categorizationRuleService)
		operations.RegisterCategorizationRuleRoutes(humaAPI, humaRuleHandler)
		appLogger.Info("Huma categorization rule routes registered")

		// Register Huma merchant routes (Issue #957: Merchant name handling)
		merchantOverrideRepo := repository.NewMerchantOverrideRepository(db)
		merchantService := services.NewMerchantService(merchantRepo, merchantOverrideRepo)
		if phClient != nil {
			merchantService.SetAnalytics(phClient)
		}
		humaMerchantHandler := operations.NewMerchantHandler(merchantService)
		operations.RegisterMerchantRoutes(humaAPI, humaMerchantHandler)
		appLogger.Info("Huma merchant routes registered")

		// Register Huma notification routes (Issue #559: Huma migration)
		humaNotificationHandler := operations.NewNotificationHandler(notificationRepo)
		operations.RegisterNotificationRoutes(humaAPI, humaNotificationHandler)
		appLogger.Info("Huma notification routes registered")

		// Register Huma transfer suggestion routes (Issue #952: Notification Center, #972: C3 confirm-match)
		transferSuggestionSvc := services.NewTransferSuggestionService(transferSuggestionRepo)
		transferSuggestionSvc.SetGDPRAnalytics(gdprClient)
		humaTransferSuggestionHandler := operations.NewTransferSuggestionHandler(transferSuggestionSvc,
			operations.TransferSuggestionHandlerDeps{
				IdempotencyRepo: humaIdempotencyRepo,
				DB:              db,
				MCPAuditRepo:    humaMCPAuditRepo,
				MCPWorkerSecret: cfg.MCPWorkerSecret,
			})
		if phClient != nil {
			humaTransferSuggestionHandler.SetAnalytics(phClient)
		}
		operations.RegisterTransferSuggestionRoutes(humaAPI, humaTransferSuggestionHandler)
		appLogger.Info("Huma transfer suggestion routes registered")

		// Wire all optional setters for boostExecutionService.
		// IMPORTANT: ALL SetXxx calls must be listed here. Omitting any setter means
		// the corresponding feature silently fails in production (nil guard returns error,
		// no suggestion created). See Issue #972 (GAP H) for the savings_goal example.
		boostExecutionService.SetNotificationRepo(notificationRepo)
		boostExecutionService.SetSavingsGoalRepo(savingsGoalRepo)
		// Issue #972 GAP AE: execution-time account ownership guard for transfer rules.
		boostExecutionService.SetAccountRepo(accountRepo)
		// Issue #1109: Chain resolver maps stable user_account_id → current active account_id.
		boostExecutionService.SetChainResolver(services.NewAccountChainResolver(accountRepo))

		// householdService was already constructed earlier (right after emailService,
		// before the deletion-service block) so the Task 2 household member cleanup
		// worker and the deletion-service owner guard could both use it. Route
		// registration for the household/household-share endpoints themselves still
		// happens here, reusing that same instance. #1371 — Phase 7 (Task 2).

		// Register Huma dashboard routes (Issue #559: Huma migration)
		humaDashboardHandler := operations.NewDashboardHandler(dashboardService, householdService)
		operations.RegisterDashboardRoutes(humaAPI, humaDashboardHandler)
		appLogger.Info("Huma dashboard routes registered")

		// Register Huma analytics routes (Issue #559: Huma migration)
		humaAnalyticsHandler := operations.NewAnalyticsHandler(analyticsService, householdService)
		operations.RegisterAnalyticsRoutes(humaAPI, humaAnalyticsHandler)
		appLogger.Info("Huma analytics routes registered")

		// Register investment portfolio routes (Issue #138)
		humaInvestmentHandler := operations.NewInvestmentHandler(investmentService, investmentAnalyticsService)
		operations.RegisterInvestmentRoutes(humaAPI, humaInvestmentHandler)
		appLogger.Info("Investment routes registered")

		// Register MCP recipe routes (Issue #1087: MCP server)
		humaRecipeRepo := repository.NewRecipeRepository(db)
		humaRecipeService := services.NewRecipeService(humaRecipeRepo)
		humaRecipeHandler := operations.NewRecipeHandler(humaRecipeService, humaIdempotencyRepo)
		operations.RegisterRecipeRoutes(humaAPI, humaRecipeHandler)
		appLogger.Info("MCP recipe routes registered")

		// Register MCP audit log route (Issue #1087: MCP server, GAP-2)
		// Service-auth via CloudflareOnly middleware (X-Origin-Secret, not end-user Clerk JWT)
		humaMCPAuditHandler := operations.NewMCPAuditHandler(humaMCPAuditRepo, cfg.MCPWorkerSecret, identityRepo)
		operations.RegisterMCPAuditRoutes(humaAPI, humaMCPAuditHandler)
		appLogger.Info("MCP audit log route registered")

		// Register Huma dashboard preferences routes (Issue #559: Huma migration)
		humaDashboardPrefsHandler := operations.NewDashboardPreferencesHandler(dashboardPrefsService)
		operations.RegisterDashboardPreferencesRoutes(humaAPI, humaDashboardPrefsHandler)
		appLogger.Info("Huma dashboard preferences routes registered")

		// Register Huma budget routes (Issue #559: Huma migration)
		humaBudgetHandler := operations.NewBudgetHandler(budgetService, budgetSyncService, transactionService, operations.BudgetHandlerDeps{
			IdempotencyRepo:      humaIdempotencyRepo,
			DB:                   db,
			MCPAuditRepo:         humaMCPAuditRepo,
			MCPWorkerSecret:      cfg.MCPWorkerSecret,
			GoalOwnershipChecker: savingsGoalRepo, // R5-1: ensures MCP CreateBudget validates savings_goal_id ownership
		})
		operations.RegisterBudgetRoutes(humaAPI, humaBudgetHandler)
		appLogger.Info("Huma budget routes registered")

		// Register Huma savings goal routes (Issue #559: Huma migration)
		humaSavingsGoalHandler := operations.NewSavingsGoalHandler(savingsGoalService, operations.SavingsGoalHandlerDeps{
			ProgressService: savingsProgressService,
			IdempotencyRepo: humaIdempotencyRepo,
			DB:              db,
			MCPAuditRepo:    humaMCPAuditRepo,
			AccountGetter:   accountRepo,
			MCPWorkerSecret: cfg.MCPWorkerSecret,
		})
		operations.RegisterSavingsGoalRoutes(humaAPI, humaSavingsGoalHandler)
		appLogger.Info("Huma savings goal routes registered")

		// Issue #1300 M20: Register contribution API routes (D6 source-a manual log + confirm + delete)
		humaContributionHandler := operations.NewContributionHandler(
			savingsContribRepo,     // SavingsContributionRepository
			savingsProgressService, // ContributionProgressRecomputer: RecomputeGoalByID (R3-2)
			savingsGoalRepo,        // ContributionGoalReader: GetByIDForUser (R3-3 C8 basis gate)
		)
		operations.RegisterContributionRoutes(humaAPI, humaContributionHandler)
		appLogger.Info("Huma savings goal contribution routes registered (M20 D6 source-a)")

		// Register Huma boost routes (Issue #37: Boost Scheduler)
		humaBoostHandler := operations.NewBoostHandler(boostService, boostSchedulerManager)
		humaBoostHandler.SetExecutionService(boostExecutionService) // Issue #579: Wire execution service for test endpoint
		operations.RegisterBoostRoutes(humaAPI, humaBoostHandler)
		appLogger.Info("Huma boost routes registered")

		// Register Huma plaid routes (Issue #559: Huma migration)
		if plaidService != nil && accountService != nil {
			humaPlaidHandler := operations.NewPlaidHandler(plaidService, accountService)
			operations.RegisterPlaidRoutes(humaAPI, humaPlaidHandler)
			appLogger.Info("Huma plaid routes registered")
		}

		// Register Huma profile routes (Issue #559: Huma migration)
		humaProfileHandler := operations.NewProfileHandler(profileService)
		// Issue #1413 round-5: same validator as profileRepo above, so the profile response's
		// derived comp fields (effective_status/is_comped/comp_tier) can never report a comp the
		// access-granting path would reject.
		humaProfileHandler.SetOverrideTierValidator(overrideTierValidator)
		operations.RegisterProfileRoutes(humaAPI, humaProfileHandler)
		appLogger.Info("Huma profile routes registered")

		// Register Huma household entitlement routes (Issue #1366)
		humaHouseholdEntitlementHandler := operations.NewHouseholdEntitlementHandler(householdEntitlementService, profileRepo)
		operations.RegisterHouseholdEntitlementRoutes(humaAPI, humaHouseholdEntitlementHandler)
		appLogger.Info("Huma household entitlement routes registered")

		// Register Huma household routes (Issue #1367 — Phase 3: membership, invites & consent gate)
		// householdService was already constructed above (before the dashboard/analytics
		// handlers, which also need it to resolve `scope=household` — Issue #1369 Phase 5).
		humaHouseholdHandler := operations.NewHouseholdHandler(householdService, cfg.FrontendURL)
		operations.RegisterHouseholdRoutes(humaAPI, humaHouseholdHandler)
		appLogger.Info("Huma household routes registered (Phase 3)")

		// Register Huma household account-share routes (Issue #1368 — Phase 4: account
		// sharing grants). *householdService implements the narrow
		// HouseholdAccountShareService interface alongside the broad HouseholdService one —
		// the type assertion is safe because NewHouseholdService always returns that
		// concrete type.
		humaHouseholdShareHandler := operations.NewHouseholdShareHandler(householdService.(services.HouseholdAccountShareService))
		operations.RegisterHouseholdShareRoutes(humaAPI, humaHouseholdShareHandler)
		appLogger.Info("Huma household account-share routes registered (Phase 4)")

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
				WithJobQueue(jobQueue).
				WithDeadLetterService(deadLetterService)
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

		// All API v1 routes fully migrated to Huma (Issue #559)
		// The legacy Echo group and its Clerk middleware have been removed —
		// the Echo group's prefix-matching was shadowing Huma routes and
		// blocking requests with 401 (the Echo middleware lacks the Clerk API
		// email fallback that the Huma middleware provides).

		appLogger.Info("Authenticated routes enabled (Clerk + Database)")
	}

	// Issue #972: Savings impact routes (public aggregate + per-user) require DB but NOT Clerk,
	// so they are registered in their own if db != nil block rather than the Clerk-gated block.
	if db != nil {
		// GAP CA (#972): pass fieldEncryptor so GetUserImpact can decrypt user_accounts.account_name_hint.
		// fieldEncryptor is nil when ClerkSecretKey is unset; resolveDestinationName guards for nil enc.
		humaSavingsImpactHandler := operations.NewSavingsImpactHandler(repository.NewSavingsImpactRepositoryWithEncryptor(db, fieldEncryptor))
		operations.RegisterSavingsImpactRoutes(humaAPI, humaSavingsImpactHandler)
		appLogger.Info("Savings impact routes registered")
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
			Logger:         appLogger,
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
			Logger:      appLogger,
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
			Logger:         appLogger,
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
			Logger:      appLogger,
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

	// Initialize worker context once for all background workers
	if riverRunner != nil || boostSchedulerManager != nil {
		workerCtx, workerCancel = context.WithCancel(context.Background())
	}

	// Issue #573/629: Start River queue runner (handles webhook processing + account deletion)
	if riverRunner != nil {
		go func() {
			if err := riverRunner.Start(workerCtx); err != nil {
				appLogger.Warn("River queue runner error", "error", err)
			}
		}()
		appLogger.Info("River queue runner started")
	}

	// Issue #37: Start rule scheduler manager
	if boostSchedulerManager != nil {
		go func() {
			if err := boostSchedulerManager.Start(workerCtx); err != nil {
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
	if boostSchedulerManager != nil {
		appLogger.Info("Stopping rule scheduler manager...")
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := boostSchedulerManager.Stop(stopCtx); err != nil {
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

	// Stop Cloudflare CIDR refresher goroutine
	cfCancel()

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

// startJobHealthMetricsCollector runs a goroutine that periodically refreshes
// observability.ManualTierOverrideOldestUnclearedExpiredSeconds from the database (Issue #1413
// Task P4). Lives here — not in the observability package alongside
// observability.StartDBMetricsCollector, which it otherwise mirrors — because it needs
// repository.JobHealthRepository, and internal/repository already imports internal/observability;
// putting this collector there would create an import cycle. A failed query is logged and
// skipped (not fatal): a transient DB hiccup here must not crash the whole process, and the gauge
// simply keeps its last-known value until the next tick succeeds. The goroutine stops when ctx is
// cancelled.
func startJobHealthMetricsCollector(ctx context.Context, repo repository.JobHealthRepository, logger *slog.Logger) {
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				age, err := repo.GetOldestUnclearedExpiredManualTierOverrideAge(ctx)
				if err != nil {
					logger.Warn("failed to refresh manual tier override job-health gauge", "error", err)
					continue
				}
				observability.ManualTierOverrideOldestUnclearedExpiredSeconds.Set(age.Seconds())
			}
		}
	}()
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
