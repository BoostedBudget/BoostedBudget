package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"net/netip"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humaecho"
	sentryecho "github.com/getsentry/sentry-go/echo"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	echomiddleware "github.com/labstack/echo/v4/middleware"
	"github.com/maxnussbaum/BoostedBudget/internal/cache"
	"github.com/maxnussbaum/BoostedBudget/internal/config"
	appcrypto "github.com/maxnussbaum/BoostedBudget/internal/crypto"
	"github.com/maxnussbaum/BoostedBudget/internal/database"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/health"
	humaconfig "github.com/maxnussbaum/BoostedBudget/internal/huma"
	humamiddleware "github.com/maxnussbaum/BoostedBudget/internal/huma/middleware"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/operations"
	phclient "github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"
	"github.com/maxnussbaum/BoostedBudget/internal/middleware"
	"github.com/maxnussbaum/BoostedBudget/internal/observability"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/maxnussbaum/BoostedBudget/internal/queue/jobs"
	riverqueue "github.com/maxnussbaum/BoostedBudget/internal/queue/river"
	"github.com/maxnussbaum/BoostedBudget/internal/ratelimitstore"
	"github.com/maxnussbaum/BoostedBudget/internal/redisclient"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
	"github.com/maxnussbaum/BoostedBudget/internal/workers"
	"github.com/riverqueue/river"
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
	defer func() {
		if err := shutdownTracer(context.Background()); err != nil {
			log.Printf("⚠️  OTEL tracer shutdown failed: %v", err)
		}
	}()

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

	// Issue #1531 gap round 2: an ERROR-level (not Warn) startup log when SENTRY_DSN is unset in
	// production/staging — see sentryDSNWarning's doc comment (sentry_dsn_warning.go) for why this
	// is neither a validate() hard failure nor an in-repo alert-rule file.
	if msg, attrs, warranted := sentryDSNWarning(cfg.Environment, cfg.SentryDSN); warranted {
		appLogger.Error(msg, attrs...)
	}

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

	// Issue #1531 Task 7c/7d: construct the two Redis clients — limiter (fleet-wide rate
	// limiting, wired below) and cache (Task 9, out of scope here).
	//
	// Task 8: cfg.RateLimitRedisURL (RATE_LIMIT_REDIS_URL) replaces the old generic cfg.RedisURL
	// (REDIS_URL) — the rename exists so there is no generic "the Redis URL" left for a future
	// change to point the cache at. The two clients below use TWO SEPARATE config fields
	// (RateLimitRedisURL / CacheRedisURL) precisely so they can never be pointed at the same
	// instance by accident: internal/redisclient's package doc comment explains why sharing one
	// URL between a noeviction-provisioned limiter instance and a cache is a real hazard (an
	// evicting cache write can OOM the limiter's SETs) — config.validate() also hard-errors in
	// production/staging if the two URLs are ever set to the same value.
	// Issue #1531 Blocker 4: cfg.RedisTimeoutMs/RedisPoolSize were validated and documented as
	// "read by redisclient.Options construction" but never actually passed to either client
	// below — both constructors sent only URL/Name, silently discarding the configured knobs and
	// running every deploy on redisclient's hardcoded defaults (200ms read/write timeout, pool
	// size 10) regardless of what an operator set. Wired through to both clients now, via
	// newRedisClientOptions, so the config actually does what it claims.
	limiterClient, err := redisclient.New(newRedisClientOptions(cfg, cfg.RateLimitRedisURL,
		// Issue #1531 Task 10a: was the literal "limiter" — now the shared constant, so a
		// future rename is a compile error instead of a silent boot-time failure in
		// cache.NewControl (which fails fast unless client.Name() == cache.LimiterClientName).
		cache.LimiterClientName))
	if err != nil {
		switch {
		case errors.Is(err, redisclient.ErrNotConfigured):
			appLogger.Warn("Redis limiter client not configured — rate limiting runs memory-only (per-machine limits)")
			limiterClient = nil
		case isProductionOrStagingEnvironment(cfg.Environment):
			appLogger.Error("Failed to construct Redis limiter client", "error", err)
			os.Exit(1)
		default:
			appLogger.Warn("Redis limiter client failed to construct — continuing memory-only", "error", err)
			limiterClient = nil
		}
	} else {
		// NOT deferred here: closed explicitly, LAST, by the shutdown sequence below (Issue
		// #1531 Task 7b) — a bare `defer` would close it at the same instant as everything else
		// in this function's defer stack, undoing the deliberate "Redis closes after HTTP has
		// drained" ordering.
		appLogger.Info("Redis limiter client constructed", "addr", limiterClient.RedactedAddr())
	}

	cacheClient, err := redisclient.New(newRedisClientOptions(cfg, cfg.CacheRedisURL, "cache"))
	if err != nil {
		// Cache-client failure is NEVER fatal (Task 9 brief) — log and continue without a cache.
		if !errors.Is(err, redisclient.ErrNotConfigured) {
			appLogger.Warn("Redis cache client failed to construct — continuing without a cache", "error", err)
		}
		cacheClient = nil
	}
	// Not deferred here either, for the same reason as limiterClient above — closed explicitly by
	// the shutdown sequence.

	// Issue #1531 Task 10a: wire Task 9's caching primitives to a real Cache instance.
	//
	// Control MUST poll against the LIMITER's no-eviction client, never the cache's own
	// (eviction-enabled) instance — see cache.NewControl's doc comment for why an evicted "off"
	// control key would otherwise silently re-enable caching. NewControl fails fast if handed
	// the wrong client (checked via client.Name() == cache.LimiterClientName, fixed just above).
	var cacheControl *cache.Control
	if limiterClient != nil {
		cacheControl, err = cache.NewControl(limiterClient, cache.AllNamespaces())
		if err != nil {
			// Construction only fails on a client-name mismatch — a wiring bug, not a runtime
			// condition — so this is a hard fail rather than falling back to no-Control.
			appLogger.Error("Failed to construct cache Control", "error", err)
			os.Exit(1)
		}
	}
	cacheControlCtx, cacheControlCancel := context.WithCancel(context.Background())
	if cacheControl != nil {
		go cacheControl.Run(cacheControlCtx, cache.DefaultControlPollInterval)
	}

	// cacheEnvDisabled is the parsed form of CACHE_NAMESPACES_DISABLED (comma-separated
	// namespace names) — resolution order is hardDisabled -> Redis control key -> this env
	// override -> compiled default (see cache.redisCache.Enabled's doc comment).
	cacheEnvDisabled := parseCacheNamespacesDisabled(cfg.CacheNamespacesDisabled)

	// Cache-client failure (including CACHE_ENABLED=false) is NEVER fatal — fall back to
	// cache.NewNoopCache(), which issues zero Redis commands, so every consumer wired below stays
	// unconditional and nil-free (Task 9/10a brief).
	var cacheInstance cache.Cache
	if cfg.CacheEnabled && cacheClient != nil {
		cacheInstance = cache.NewCache(cache.Options{
			Client:                cacheClient,
			Control:               cacheControl,
			EnvDisabledNamespaces: cacheEnvDisabled,
			// SingleflightLoadTimeout (Issue #1531 N1): cache.go's default (30s) was justified as
			// "matches this server's default WriteTimeout" but had no path to a NON-default
			// SERVER_WRITE_TIMEOUT — passing cfg.WriteTimeout here explicitly makes that
			// justification actually hold for any configured POSITIVE value, not just the default.
			// Round-6 finding P5: SERVER_WRITE_TIMEOUT=0 is the one value where it still does not
			// hold — net/http treats WriteTimeout=0 as "no write timeout" (unbounded), but
			// cache.Options.SingleflightLoadTimeout treats any value <= 0 as "use the 30s floor",
			// deliberately, not unbounded — see that field's doc comment for why an unbounded
			// singleflight wait was rejected (a single stuck loader would hang every concurrent
			// caller sharing its key forever, which is worse than a bounded timeout error).
			SingleflightLoadTimeout: cfg.WriteTimeout,
		})
		appLogger.Info("Cache wired to Redis", "control_enabled", cacheControl != nil)
	} else {
		cacheInstance = cache.NewNoopCache()
		appLogger.Info("Cache disabled — using no-op cache", "cache_enabled_config", cfg.CacheEnabled, "cache_client_available", cacheClient != nil)
	}

	// Redis pool metrics (Issue #1531 Task 2/7): report both clients' local pool state next to
	// the existing DB pool metrics collector. redisPoolStatsAdapter is the local mirror adapter —
	// see its doc comment for why internal/observability never imports internal/redisclient.
	//
	// Fix round 1: given its own cancellable context (rather than context.Background(),
	// uncancellable) so shutdownSequence below can stop this goroutine — ordered, per the brief,
	// after the HTTP/River drain and before the Redis clients close.
	redisMetricsCtx, redisMetricsCancel := context.WithCancel(context.Background())
	var redisMetricsSources []observability.RedisPoolStatsSource
	if limiterClient != nil {
		redisMetricsSources = append(redisMetricsSources, redisPoolStatsAdapter{limiterClient})
	}
	if cacheClient != nil {
		redisMetricsSources = append(redisMetricsSources, redisPoolStatsAdapter{cacheClient})
	}
	if len(redisMetricsSources) > 0 {
		observability.StartRedisMetricsCollector(redisMetricsCtx, redisMetricsSources...)
	}

	// Issue #1531 Task 7b: the rate-limit middleware's background cleanup goroutine (and the
	// fallback limiter's accumulateActiveSecondsWhileDegraded goroutine) stop when this channel
	// closes — see shutdown ordering below. Closed exactly once, well after HTTP has drained.
	rateLimitDone := make(chan struct{})

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
	e.Use(middleware.RequestID())                      // 1. Generate request IDs first
	e.Use(middleware.SecurityHeaders(cfg.Environment)) // 2. Security hardening headers
	// Issue #1531 Task 7: the fleet-wide Redis-backed primary (nil-safe: memory-only when
	// limiterClient is nil), wired via the same ordering-critical helper
	// (registerCloudflareAndRateLimitMiddleware — the byte-for-byte-pinned issue1514 ordering
	// test still calls that 3-arg function directly, which now just delegates here with a nil
	// backend/done/logger, reproducing its exact prior behaviour).
	rateLimitBackend, err := buildRateLimitBackend(context.Background(), limiterClient, cfg)
	if err != nil {
		// RATE_LIMIT_BACKEND=redis with no working limiter client — a STARTUP failure (we are
		// still in main()'s initialization, before Echo starts accepting traffic), never a
		// per-request one. See buildRateLimitBackend's doc comment.
		appLogger.Error("Failed to build rate-limit backend", "error", err)
		os.Exit(1)
	}
	// #1609: header-only CORS middleware, registered BEFORE the Cloudflare gate and rate
	// limiter so a 403/429 rejection still carries Access-Control-Allow-Origin (and
	// -Credentials, Vary, Expose-Headers) — those two paths short-circuit and never reach
	// the real CORS middleware at the end of this chain. It never short-circuits itself
	// (does not handle OPTIONS/preflight) — see CORSErrorHeaders' doc comment for why moving
	// the REAL CORS middleware here instead was rejected as an unmetered perimeter bypass.
	e.Use(middleware.CORSErrorHeaders(cfg.CORSAllowedOrigins))                                                         // 3. Error-path CORS headers (must run before the gate)
	registerCloudflareAndRateLimitMiddlewareWithBackend(e, cfCIDRRef, cfg, rateLimitBackend, rateLimitDone, appLogger) // 4-5. Cloudflare gate, then per-IP rate limiting (order load-bearing, see helper doc)
	e.Use(middleware.PanicRecovery(appLogger, cfg.Environment))                                                        // 6. Panic recovery (formats RFC 9457 response)
	e.Use(sentryecho.New(sentryecho.Options{Repanic: true}))                                                           // 7. Sentry panic reporting
	e.Use(otelecho.Middleware("boostedbudget-api"))                                                                    // 8. OTEL tracing
	e.Use(middleware.StructuredLogging(appLogger))                                                                     // 9. slog-based HTTP logging
	e.Use(middleware.MetricsMiddleware())                                                                              // 10. Prometheus metrics
	e.Use(echomiddleware.BodyLimit(cfg.MaxBodySize))                                                                   // 11. Request body size limit
	e.Use(echomiddleware.CORSWithConfig(middleware.GetCORSConfig(cfg.CORSAllowedOrigins)))                             // 12. CORS handling with configured origins

	// Initialize Huma API wrapping Echo for OpenAPI generation
	// Huma provides automatic OpenAPI 3.1 spec generation from Go code
	// NewV4, not New: huma v2.39.0 repointed humaecho.New at Echo v5. NewV4 is the same
	// adapter for Echo v4, which this app still runs on.
	humaAPI := humaecho.NewV4(e, humaconfig.NewAPIConfig(cfg.Version, cfg.Environment))
	appLogger.Info("Huma API initialized - OpenAPI at /openapi.json, Docs at /docs")

	// Health check handler (no auth required) - Huma operation.
	//
	// Issue #1531 Task 7c: the full health.Registry, built ONCE and shared with the admin
	// system-health service below (via sharedHealthChecks) so an admin page load reuses whatever
	// this liveness probe already paid for instead of re-probing Redis/Clerk. Task 6's structural
	// guarantee still holds unmodified: operations.HealthHandler.GetHealth only ever reads
	// registry.DB — registry.Redis/registry.Clerk are populated here but inert on this path (see
	// that handler's doc comment, and TestWiring_HealthProbeSucceedsAndIsFastWithRedisCompletelyDown).
	//
	// dbPingerArg guards against the classic typed-nil-interface footgun: db is a possibly-nil
	// *pgxpool.Pool, and boxing a nil *pgxpool.Pool directly into the dbPinger interface would
	// produce a NON-nil interface value (wrong type descriptor, nil underlying pointer) that
	// createDBCheck's `pool == nil` check cannot catch — it would panic on the first pool.Ping.
	var dbPingerArg dbPinger
	if db != nil {
		dbPingerArg = db
	}
	registry := health.Registry{
		DB:    createDBCheck(dbPingerArg),
		Redis: createRedisCheck(limiterClient),
		Clerk: createClerkCheck(cfg.ClerkSecretKey, http.DefaultClient),
	}
	healthHandler := operations.NewHealthHandler(appLogger, registry)
	operations.RegisterHealthRoutes(humaAPI, healthHandler)

	// Issue #1531 Task 7d: shared with services.NewSystemHealthService below (once db/Clerk are
	// available) so the admin dashboard's Redis/Clerk detail reuses these SAME Check instances
	// rather than re-probing. RedisPoolStats is left nil when there is no limiter client — see
	// services.SharedHealthChecks's doc comment for why that must skip PoolStats entirely rather
	// than report a zero-valued (falsely "idle but live") snapshot.
	sharedHealthChecks := services.SharedHealthChecks{
		Clerk: registry.Clerk,
		Redis: registry.Redis,
	}
	if limiterClient != nil {
		sharedHealthChecks.RedisPoolStats = limiterClient.PoolStats
	}

	// Metrics endpoint (Prometheus format) — internal-only listener, NOT the public Huma
	// API (humaAPI/e) above. Issue #1514: /metrics exposes River queue depth, Plaid error
	// rates, dead-letter state, and GDPR-deletion internals, and was reachable from the
	// public internet unauthenticated. See internal_metrics.go for the full rationale and
	// the listener construction. Binding happens synchronously so a failure to bind is a
	// startup error (os.Exit(1) below), not a silent no-op. resolveInternalMetricsPort
	// also fails loudly here — rather than downstream as a silently-dead public API — if
	// INTERNAL_METRICS_PORT collides with cfg.Port or is not a valid TCP port.
	//
	// Gated on cfg.EnableMetrics (ENABLE_METRICS, default true — see config.go) so the flag
	// actually controls something: previously it was read and never consulted. Fly's
	// [metrics] scrape config in fly.toml/fly.staging.toml points at this listener's port
	// (9091) in every environment that deploys this binary, so ENABLE_METRICS=false will
	// produce failing Fly scrapes wherever it's set — that is the intended, documented
	// tradeoff of an operator explicitly disabling metrics, not a bug.
	var internalMetricsEcho *echo.Echo
	if cfg.EnableMetrics {
		resolvedInternalMetricsPort, err := resolveInternalMetricsPort(internalMetricsPort(), cfg.Port)
		if err != nil {
			appLogger.Error("Invalid internal metrics port configuration", "error", err)
			os.Exit(1)
		}
		internalMetricsListener, err := bindInternalMetricsListener(resolvedInternalMetricsPort)
		if err != nil {
			appLogger.Error("Failed to bind internal metrics listener", "error", err)
			os.Exit(1)
		}
		var internalHumaAPI huma.API
		internalMetricsEcho, internalHumaAPI = newInternalMetricsAPI(cfg)
		internalMetricsEcho.Listener = internalMetricsListener
		humaMetricsHandler := operations.NewMetricsHandler()
		operations.RegisterMetricsRoutes(internalHumaAPI, humaMetricsHandler)
		go func() {
			if err := internalMetricsEcho.Start(""); err != nil && !errors.Is(err, http.ErrServerClosed) {
				appLogger.Error("Internal metrics listener stopped", "error", err)
			}
		}()
		appLogger.Info("Internal metrics listener started", "address", internalMetricsListener.Addr().String())
	} else {
		appLogger.Info("Internal metrics listener disabled via ENABLE_METRICS=false — any Fly [metrics] scrape config still pointed at this instance will fail")
	}

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
	// Declare itemErrorService at outer scope: it's constructed inside the "if cfg.PlaidClientID
	// != ..." block (Issue #257) but registered as a Huma handler dependency inside a later,
	// separate "if plaidService != nil && accountService != nil" block (Issue #1609). Both blocks
	// close before the other opens, so this can't be a := inside either one.
	var itemErrorService services.ItemErrorService
	// Issue #1569: userAccountService must be constructed BEFORE accountService, not alongside the
	// user-account routes further down. LinkAccounts now REFUSES to create an account without a
	// stable user_accounts identity (ErrStableAccountIdentityUnavailable), because an identity-less
	// account is a permanent re-link double-count seed once history is retained rather than
	// cascade-deleted. Until this wiring existed, accountService.userAccountSvc was nil in
	// production and EVERY linked account was written with user_account_id = NULL.
	var userAccountService services.UserAccountService

	// Authenticated routes (requires database and Clerk)

	// Issue #573/629: River queue for async job processing
	var jobQueue queue.JobQueue
	var riverRunner queue.JobRunner

	// Issue #629: Declare deletion workers for River registration
	var accountDeletionWorker *jobs.AccountDeletionWorker
	var scheduledDeletionCheckWorker *jobs.ScheduledDeletionCheckWorker

	// Issue #1626 Phase 3, Defect 3: declare the periodic balance-snapshot dispatcher and its
	// per-item capture worker. Both are Plaid-specific (the capture calls /accounts/get), so both
	// stay nil when Plaid is not configured — and BOTH must be nil or non-nil together: a
	// registered periodic dispatcher with no capture worker would enqueue jobs nothing can run.
	var balanceSnapshotDispatchWorker *jobs.BalanceSnapshotDispatchWorker
	var balanceSnapshotCaptureWorker *jobs.BalanceSnapshotCaptureWorker

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
	// Not gated on DELETION_TOKEN_SECRET — it depends on adminUserRepo/clerkService, not
	// deletionService. (Stale as of round-8: this used to be a contrast with retention's worker,
	// which WAS gated on DELETION_TOKEN_SECRET at the time. Issue #1413 round-8 Blocker 2 moved
	// retention's worker's own construction outside that gate too — see its construction site
	// below — so both are now unconditional on DELETION_TOKEN_SECRET specifically. Neither is
	// truly ungated, though: both still sit inside the OUTER `if db != nil && cfg.ClerkSecretKey
	// != ""` block this whole function body lives in — a real, separate gate, filed as its own
	// issue.)
	var manualTierOverrideExpiryCheckWorker *jobs.ManualTierOverrideExpiryCheckWorker

	// Issue #1371 — Phase 7 (Task 2): Declare household member cleanup check worker
	// (purges household_members rows removed 30+ days ago). Constructed once
	// householdService is available (below); not gated on DELETION_TOKEN_SECRET either — it
	// depends on householdService, not deletionService. See
	// manualTierOverrideExpiryCheckWorker's declaration comment above for why this is no longer a
	// useful contrast with retention's worker (round-8 made that unconditional too), and for the
	// real outer gate both still sit inside.
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

	// Issue #1497: Declare the two orphaned-Plaid-Item sweeps. They are declared TOGETHER and
	// gated DIFFERENTLY on purpose, and the difference is the compliance property:
	//
	//   - the token PURGE worker is ALWAYS ACTIVE. DeleteUserCascade retains a Plaid access token
	//     on every plaid_orphaned_items row it writes, whether or not Plaid is configured at the
	//     time, and migration 000135 promises that credential is purged at its 30-day deadline (at the
	//     first sweep at or after it — see domain.OrphanedItemTokenPurgeInterval for the bound)
	//     regardless of anything else. Gating it on Plaid config would make a GDPR retention limit
	//     conditional on a feature flag.
	//   - the RETRY worker needs a Plaid client, so it is nil when Plaid is not configured — which
	//     is exactly why the purge is not allowed to live inside it.
	var plaidOrphanTokenPurgeWorker *jobs.PlaidOrphanTokenPurgeWorker
	var plaidOrphanRetryWorker *jobs.PlaidOrphanRetryWorker

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

	// #1625 Task 18 fix round 1: declare rule quarantine census worker (daily full-table scan,
	// no Plaid dependency) — constructed unconditionally alongside its siblings below, since
	// categorizationRuleRepo has no configuration gate.
	var ruleQuarantineCensusWorker *jobs.RuleQuarantineCensusWorker

	// Issue #1123: Declare effective category refresh worker
	var effectiveCategoryRefreshWorker *jobs.EffectiveCategoryRefreshWorker

	// Issue #1630 Task B1: Declare budget hierarchy resync worker (custom-category reparent).
	var budgetHierarchyResyncWorker *jobs.BudgetHierarchyResyncWorker

	// Issue #1630 Task B7: Declare budget hierarchy audit worker (periodic sweep over
	// pre-existing budgets the write-time guards would refuse). No Plaid dependency.
	var budgetHierarchyAuditWorker *jobs.BudgetHierarchyAuditWorker

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
	// Issue #1514: bounded audit signal for rejected (unsigned/invalid-signature) webhook
	// deliveries. Requires only a DB connection (no encryption — see repository doc comment on
	// why raw payload is never stored), so it's constructed alongside paddleWebhookRepo below.
	var invalidWebhookAttemptRepo repository.InvalidWebhookAttemptRepository

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
		// Issue #1587: GDPR data export (Article 20). Needs the field encryptor: several exported
		// columns are stored as ENC$ ciphertext and the user must receive the plaintext.
		dataExportRepo := repository.NewDataExportRepository(db, fieldEncryptor)
		linkedItemRepo := repository.NewLinkedItemRepository(db, fieldEncryptor)
		accountRepo := repository.NewAccountRepository(db, fieldEncryptor)
		transactionRepo := repository.NewTransactionRepository(db, fieldEncryptor)
		webhookRepo := repository.NewWebhookRepository(db, fieldEncryptor)
		paddleWebhookRepo = repository.NewPaddleWebhookRepository(db, fieldEncryptor)                                         // Issue #45, #46: Paddle webhooks
		invalidWebhookAttemptRepo = repository.NewInvalidWebhookAttemptRepository(db, cfg.InvalidWebhookAttemptRetentionDays) // Issue #1514: bounded invalid-attempt audit
		paddleTransactionRepo = repository.NewPaddleTransactionRepository(db, fieldEncryptor)                                 // Issues #747 #748: Paddle transaction audit log
		identityRepo := repository.NewUserIdentityRepository(db, fieldEncryptor)                                              // Maps Clerk IDs to user UUIDs
		notificationRepo := repository.NewNotificationRepository(db, fieldEncryptor)                                          // Issue #257: Notification storage
		balanceSnapshotRepo := repository.NewBalanceSnapshotRepository(db)                                                    // Issue #353: Balance tracking
		userTagRepo := repository.NewUserTagRepository(db)                                                                    // Issue #345: User tags
		customCategoryRepo := repository.NewCustomCategoryRepository(db)                                                      // Issue #345: Custom categories
		categorizationRuleRepo := repository.NewCategorizationRuleRepository(db)                                              // Issue #346: Categorization rules
		analyticsRepo := repository.NewAnalyticsRepository(db)                                                                // Issue #354: Cash flow analytics
		budgetRepo := repository.NewBudgetRepository(db)                                                                      // Issue #28: Budget management
		savingsGoalRepo := repository.NewSavingsGoalRepository(db)                                                            // Issue #28: Savings goals
		dashboardPrefsRepo := repository.NewDashboardPreferencesRepository(db)                                                // Issue #354: Dashboard preferences
		transferSuggestionRepo := repository.NewTransferSuggestionRepositoryWithEncryptor(db, fieldEncryptor)                 // Issue #36 / #1267: Transfer suggestions (enc for account-name enrichment)
		boostRepo := repository.NewBoostsRepository(db)                                                                       // Issue #37: Boosts
		adminUserRepo := repository.NewAdminUserRepository(db, fieldEncryptor)                                                // Issue #639: Admin user management
		systemHealthRepo := repository.NewSystemHealthRepository(db)                                                          // Issue #640: System health monitoring
		deadLetterJobRepo := repository.NewDeadLetterJobRepository(db)                                                        // Issue #640: Dead letter queue metrics
		plaidOrphanedItemRepo := repository.NewPlaidOrphanedItemRepository(db, fieldEncryptor)                                // Issue #1497: orphaned Plaid Items surviving a GDPR erasure (enc: every identifying column is ciphertext)
		// Issue #1458: a second, bound JobHealthRepository instance for systemHealthService below —
		// distinct from the one constructed inline at startJobHealthMetricsCollector's call site
		// above, which is unbound (passed directly as an argument, never stored) and so can't be
		// reused here.
		jobHealthRepoForHealth := repository.NewJobHealthRepository(db)
		securityRepo := repository.NewSecurityRepository(db)                                         // Issue #659: Plaid Investments
		holdingRepo := repository.NewHoldingRepository(db)                                           // Issue #659: Plaid Investments
		investmentTxnRepo := repository.NewInvestmentTransactionRepository(db)                       // Issue #659: Plaid Investments
		portfolioSnapshotRepo := repository.NewPortfolioSnapshotRepository(db)                       // Issue #138: Portfolio snapshots
		investmentAnalyticsRepo := repository.NewInvestmentAnalyticsRepository(db)                   // Issue #138: Investment analytics
		creditLiabilityRepo := repository.NewCreditLiabilityRepository(db)                           // Issue #910: Plaid Liabilities
		mortgageLiabilityRepo := repository.NewMortgageLiabilityRepository(db, fieldEncryptor)       // Issue #910: Plaid Liabilities
		studentLoanLiabilityRepo := repository.NewStudentLoanLiabilityRepository(db, fieldEncryptor) // Issue #910: Plaid Liabilities
		merchantRepo := repository.NewMerchantRepository(db)                                         // Issue #1163: Merchant batch-upsert
		// NewWorkClaimsOutcomeRepository (not NewWorkClaimsRepository): the subscription-cancellation
		// webhook needs the wider WorkClaimsOutcomeRepository view (#1413 round-3 finding H3, see
		// SubscriptionWebhookWorker.SetWorkClaimsRepo). Every other consumer below still declares its
		// setter parameter as the narrower repository.WorkClaimsRepository, and a
		// WorkClaimsOutcomeRepository value satisfies that automatically (its method set is a
		// superset) — so this single swap requires no other changes at any other call site.
		workClaimsRepo := repository.NewWorkClaimsOutcomeRepository(db) // Issue #1413: durable leased claims for destructive actions

		// Issue #1477: the persisted cross-run resume position shared by the four capped periodic
		// sweeps (scheduled_deletion_check, retention_cleanup_check's deleteExpiredAccounts,
		// item_cleanup_check, plaid_orphan_retry). Each worker treats it as optional and nil-safe,
		// so leaving it unwired would not break anything visibly — it would silently restore the
		// pre-#1477 behaviour in which a permanently-failing head of the queue starves everything
		// behind it. It is therefore wired here unconditionally, alongside the other shared
		// repositories, and passed to all four workers below.
		sweepCursorRepo := repository.NewSweepCursorRepository(db)

		// Create GDPR-consent-gated analytics client.
		// User-behavior analytics events (e.g. transfer suggestion accept/reject) use
		// gdprClient.CaptureIfConsented, which checks privacy settings before firing.
		// Operational/system events use phClient.Capture directly.
		// New backend user-behavior events MUST use gdprClient; existing operational events are NOT retrofitted.
		consentChecker := phclient.NewPrivacyConsentAdapter(privacyRepo)
		gdprClient := phclient.NewGDPRClient(phClient, consentChecker, appLogger)

		// GDPR audit trail. Until Issue #1505 this was &mockAuditLogger{} — a stub that logged a
		// line and persisted nothing, while audit_logs existed and was already being queried and
		// redacted elsewhere. repository.AuditLogRepository satisfies services.AuditLogger
		// structurally; see its doc comment for the column semantics.
		auditLogger := repository.NewAuditLogRepository(db)

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
		// Consent-gated (Issue #1413 round 10): this worker's event carries the expired target user's
		// own UUID as distinct_id, so it is that user's personal data even though a system job fired
		// it — see SetGDPRAnalytics' doc comment for why the earlier raw-phClient clearance was reversed.
		manualTierOverrideExpiryCheckWorker.SetGDPRAnalytics(gdprClient)
		appLogger.Info("Manual tier override expiry check worker created (River)")

		// Create services
		profileService := services.NewProfileService(services.NewProfileServiceDeps{
			Repo:         profileRepo,
			IdentityRepo: identityRepo,
			DB:           db,
			JobQueue:     nil, // Set after jobQueue is created via SetJobQueue
		})
		privacyService := services.NewPrivacyService(privacyRepo)
		dataExportService := services.NewDataExportService(dataExportRepo)
		dataExportService.SetAnalytics(phClient)
		transactionService := services.NewTransactionService(transactionRepo, customCategoryRepo)
		transactionService.SetAnalytics(phClient)
		transactionService.SetAccountRepository(accountRepo) // #1329: embed account in investment rows
		registrationService := services.NewRegistrationService(profileRepo, identityRepo, privacyRepo, auditLogger, db)
		registrationService.SetAnalytics(phClient)
		subscriptionOverrideRepo := repository.NewSubscriptionOverrideRepository(db)
		subscriptionService := services.NewSubscriptionService(tierConfig, subscriptionOverrideRepo)
		// Issue #1413 (tier axis, this task): same validator as profileRepo/humaProfileHandler/
		// householdEntitlementService above, so a malformed manual_tier_override falls back to
		// the raw subscription tier here too — GetEffectiveSubscription backs the admin
		// subscription-detail endpoint, Plaid sync depth limits, and feature/limit resolution
		// for every user. See SubscriptionService.SetOverrideTierValidator's doc comment.
		subscriptionService.SetOverrideTierValidator(overrideTierValidator)
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
		// Issue #1569 Task 9: the category delete is a tombstone now, and writes a
		// custom_category.deleted audit entry inside its own transaction.
		customCategoryService.SetAuditLogRepository(repository.NewAuditLogRepository(db))
		// Issue #1630 Task B1: budgetRepo satisfies services.BudgetCategoryRepository
		// structurally (GetActiveByUserID + GetCategoriesByBudgetID). Required, not optional —
		// UpdateCategory refuses a reparent rather than proceeding unchecked when this is unset.
		customCategoryService.SetBudgetCategoryRepository(budgetRepo)
		// Issue #346: Categorization rules services
		categorizationRuleService := services.NewCategorizationRuleService(categorizationRuleRepo, customCategoryRepo)
		// #1625: backs POST /auto-categorization/preview. Setter injection so every other
		// NewCategorizationRuleService call site keeps compiling; preview reports itself
		// unavailable rather than panicking when this is not wired.
		if svc, ok := categorizationRuleService.(interface {
			SetTransactionReader(services.PreviewTransactionReader)
		}); ok {
			svc.SetTransactionReader(transactionRepo)
		}
		ruleApplicationService := services.NewRuleApplicationService(categorizationRuleRepo)
		ruleApplicationService.SetAnalytics(phClient)
		// #1625 Task 11: lets a rule match `effective_merchant` -- the name the user sees after
		// their own rename or merge -- during sync, not just in preview. Fail-open: unwired, such
		// conditions fall back to the raw provider name.
		ruleApplicationService.SetEffectiveMerchantResolver(transactionRepo)
		// Issue #1569 Task 5 (#1572): the automation audit trail's writer. Until this line
		// rule_execution_history had no production writer at all — it was backfilled by migration
		// 000164 and then never written to again, so the trail was permanently empty.
		ruleExecutionHistoryRepo := repository.NewRuleExecutionHistoryRepository(db)
		ruleApplicationService.SetRuleExecutionHistoryRepository(ruleExecutionHistoryRepo)
		// Issue #354: Cash flow analytics and dashboard preferences services
		analyticsServiceImpl := services.NewAnalyticsService(analyticsRepo)
		analyticsServiceImpl.SetBalanceSnapshotRepo(balanceSnapshotRepo) // wires snapshot repo for GetNetWorthTrend (Issue #1215 PR3)
		// Issue #1531 Task 10a: wrap AFTER SetBalanceSnapshotRepo above — GetNetWorthTrend fans
		// out via balanceSnapshotRepo INSIDE the service, so wrapping first would permanently
		// capture a service whose fan-out repo is nil. Decorates the SERVICE, not the repository:
		// 39+ non-test call sites construct repositories directly over a pgx.Tx, bypassing
		// anything wired here, and a repository-level decorator would silently miss all of them.
		analyticsService := services.NewCachedAnalyticsService(analyticsServiceImpl, cacheInstance)
		// Issue #639: adminUserService is created later after adminService is initialized
		// Issue #474: Load dashboard layout config for preferences service
		// This ensures reset-to-default uses YAML config version, not hardcoded version 2
		dashboardLayoutConfig, err := config.LoadDashboardLayoutConfig()
		if err != nil {
			appLogger.Error("Failed to load dashboard layout config", "error", err)
			os.Exit(1)
		}
		dashboardPrefsServiceImpl := services.NewDashboardPreferencesService(dashboardPrefsRepo, dashboardLayoutConfig)
		// Issue #1531 Task 10a: caches GetPreferences only (namespace dashprefs, 300s TTL);
		// SavePreferences/ResetToDefault pass through unchanged.
		dashboardPrefsService := services.NewCachedDashboardPreferencesService(dashboardPrefsServiceImpl, cacheInstance)
		// Issue #1531 Task 10a: narrow identity cache — resolves clerkUserID -> internal user
		// UUID ONLY (never the full identity record: email, provider, verification state).
		// Namespace identity, 600s TTL. Wired into the Clerk middleware below as a fast path.
		cachedIdentityResolver := services.NewCachedIdentityResolver(identityRepo, cacheInstance)
		// Issue #28: Budget and savings goal services
		budgetLogger := appLogger
		budgetService := services.NewBudgetService(budgetRepo, customCategoryRepo, transactionRepo, budgetLogger)
		savingsGoalService := services.NewSavingsGoalService(savingsGoalRepo, accountRepo, budgetLogger)

		// Issue #1300: Savings contribution repo + progress service.
		// Created here so budget sync and goal services can share a single instance.
		savingsContribRepo := repository.NewSavingsContributionRepository(db)
		savingsProgressService := services.NewSavingsProgressService(savingsGoalRepo, savingsContribRepo, accountRepo, services.NewAccountChainResolver(accountRepo), budgetLogger)

		// Issue #1300: Cross-wire budget ↔ savings-goal services for carry-forward / auto-create.
		budgetService.SetGoalRepo(savingsGoalRepo)
		// Issue #1300: Wire IDOR ownership checker so AddCategory validates savings_goal_id
		// belongs to the authenticated user before persisting (Codex C1).
		budgetService.SetGoalOwnershipChecker(savingsGoalRepo)
		// Issue #1569 Task 8: budget.deleted / budget_category.deleted audit entries, written
		// inside the tombstone's own transaction via LogInTx.
		budgetService.SetAuditLogRepository(repository.NewAuditLogRepository(db))
		savingsGoalService.SetBudgetRepo(budgetRepo)
		// Issue #1300 M13: Wire progress recomputer so CreateGoal initialises
		// current_amount per progress_basis immediately after goal creation.
		savingsGoalService.SetProgressService(savingsProgressService)
		// Issue #1300 M14: Wire contribution repo + analytics for basis-switch semantics.
		savingsGoalService.SetBasisSwitchContribRepo(savingsContribRepo)
		if phClient != nil {
			savingsGoalService.SetAnalytics(phClient)
		}
		// Issue #1569 Task 8: savings_goal.deleted / savings_goal.history_purged audit entries,
		// written inside the repository's own transaction via LogInTx. Declared later in this
		// function as `auditLogger`, so a fresh AuditLogRepository is constructed here rather than
		// reordering the wiring; both wrap the same pool and hold no state.
		savingsGoalService.SetAuditLogRepository(repository.NewAuditLogRepository(db))

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
		// #1630 fixwave 4: IncomeTransactionGetter added GetIncomeSumForSources, which is not part
		// of the broader repository.TransactionRepository interface transactionRepo is typed as
		// (that interface is implemented by many mocks across the codebase; widening it would
		// force every one of them to grow the method too). The concrete *transactionRepository
		// this variable holds already implements GetIncomeSumForSources, so this assertion always
		// succeeds — it exists purely to narrow the static interface type at this one call site.
		budgetIncomeService := services.NewBudgetIncomeService(transactionRepo.(services.IncomeTransactionGetter), budgetLogger)
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
		// Issue #1569 Task 8: boost.deleted audit entry, written inside the tombstone's own
		// transaction via LogInTx.
		boostService.SetAuditLogRepository(repository.NewAuditLogRepository(db))
		appLogger.Info("Boost service initialized")

		// Issue #37: Boost Execution Service
		boostExecutionRepo := repository.NewBoostExecutionRepository(db)
		boostExecutionService := services.NewBoostExecutionService(boostExecutionRepo, transferSuggestionRepo, boostRepo, nil)
		boostExecutionService.SetAnalytics(phClient)
		// Issue #1630 review round 2: without this, a category_rollover boost's sub-allocation
		// check has no live budget-category data to verify against and fails CLOSED (skips)
		// every execution rather than trusting a possibly-stale triggerData snapshot.
		boostExecutionService.SetBudgetCategoryRepo(budgetRepo)
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
		// Issue #1531 Task 10b-2b: bump cache generations for household writes that affect
		// members OTHER than the acting HTTP caller (a member removed by the owner, a joiner +
		// existing members on invite acceptance, every member on disband/auto-offboard, and
		// every member after a share-visibility change) — NewCacheInvalidationMiddleware only
		// bumps the acting caller. cacheInstance is never nil; Generations() is nil-safe when
		// the cache is disabled (NewNoopCache), and every bump helper nil-guards on it.
		householdService.SetCacheInvalidation(cacheInstance.Generations())

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
		// Issue #1531 Task 10b-2a-iii: wire cache invalidation so an auto-offboard (this
		// worker's ONLY caller of HouseholdService.OffboardExcessMembers — a job, never an
		// HTTP request, so NewCacheInvalidationMiddleware never runs for it) bumps the
		// household's cache generation once membership actually changes. cacheInstance is
		// never nil (see cachePurgeRetryWorker below); Generations() is nil-safe when the
		// cache is disabled (NewNoopCache), and bumpCacheForHousehold nil-guards on it.
		householdOverCapacityCheckWorker.SetCacheInvalidation(cacheInstance.Generations())
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

			// Issue #1560: deletion routes are NOT registered here. huma.Register snapshots the
			// middleware chain at registration time (huma.go:881 -> chain.go:22-32), and every
			// UseMiddleware call happens far below this point — so registering here gave
			// requestDeletion zero middlewares: no Clerk auth (its AuthenticatedWithEmailInput
			// resolver then 401s unconditionally, which is why the GDPR deletion endpoint was
			// dead), no read-only enforcement despite read_only.go listing that exact path, and no
			// cache-generation bump. Registration now happens after the middleware chain is
			// assembled; see the RegisterDeletionRoutes call further down. deletionService is the
			// outer-scoped var declared near the top, so it is reachable from there.

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
				// Issue #1477: resume a capped run where it stopped instead of restarting from the
				// oldest eligible user every run.
				SweepCursors: sweepCursorRepo,
			})
			appLogger.Info("Scheduled deletion check worker created (River)")

			// Issue #633: Create DLQ cleanup worker
			dlqCleanupCheckWorker = jobs.NewDLQCleanupCheckWorker(jobs.DLQCleanupCheckWorkerDeps{
				Repo:          deadLetterJobRepo,
				RetentionDays: cfg.DLQRetentionDays,
				Logger:        appLogger,
			})
			appLogger.Info("DLQ cleanup check worker created (River)")
		} else {
			appLogger.Warn("Hard-deletion services disabled (requires DELETION_TOKEN_SECRET) — account-deletion routes/workers unavailable; retention-clock repair and Plaid-disconnect repair are unaffected (constructed unconditionally below)")
		}

		// Issue #930: Create retention cleanup check worker. Issue #1413 round-8 Blocker 2
		// (Codex review): this MUST be constructed unconditionally, outside the
		// cfg.DeletionTokenSecret gate above — retentionCleanupCheckWorker.repairPlaidDisconnects
		// is the ONLY place a read-only user's Plaid items are ever disconnected (see that
		// function's doc comment), so gating its construction on an unrelated secret meant a
		// production deployment with perfectly good Plaid credentials but no
		// DELETION_TOKEN_SECRET would silently never disconnect anyone. deletionService may
		// still be nil here (DELETION_TOKEN_SECRET unset) — that only disables Step 3's
		// auto-deletion (deleteExpiredAccounts nil-guards it and logs a warning instead of
		// panicking); Steps 0/0b/1/2 (retention-clock repair, Plaid-disconnect repair, 30d/7d
		// notices) all run regardless, since none of them touch deletionService at all.
		// ⚠ EmailService IS DELIBERATELY LEFT NIL, AND THAT NOW WITHHOLDS DELETIONS. The general email
		// service constructed above (SendGrid or its mock) does not implement
		// jobs.RetentionEmailService.SendRetentionNoticeEmail at all, so there is nothing to pass here:
		// the 30d/7d pre-deletion notices apps/web/src/legal/PrivacyPolicy.tsx promises are not sent in
		// any environment, and canSendRetentionNotice returns false before the work-claim is taken, so
		// retention_notice_30d_sent_at / _7d_sent_at are never stamped. Email lands after go-live.
		//
		// ⚠ WHAT THIS COMMENT USED TO OMIT, and the omission is how Issue #1569 defect C1 survived: it
		// said only that notices were skipped, and never that Step 3 DELETED THE ACCOUNT ANYWAY. It did
		// — GetUsersWithExpiredRetentionPage had no predicate on the notice markers — so a cancelled
		// user's data was hard-deleted at the end of the retention window having been warned zero times.
		// Since #1569 T1 the sweep and the deletion claim both require BOTH markers
		// (retentionNoticesDeliveredPredicate), so with EmailService nil the consequence is inverted:
		// expired accounts are RETAINED INDEFINITELY rather than deleted. That is deliberate — retain is
		// the safe direction to fail — and it is made visible, not silent, by Step 2b
		// (monitorRetentionDeletionsWithheld / observability.RetentionDeletionsWithheld). The
		// privacy-policy text still promises deletion after the window and is knowingly out of step
		// until the notice email exists; it belongs with that work, not with the gate.
		retentionCleanupCheckWorker = jobs.NewRetentionCleanupCheckWorker(jobs.RetentionCleanupCheckWorkerDeps{
			ProfileRepo:     profileRepo,
			DeletionService: deletionService,
			RetentionMonths: cfg.RetentionPeriodMonths,
			Logger:          appLogger,
			// Issue #1497 item 4: Step 0e publishes the orphaned-Plaid-Item state gauges and the
			// worst unresolved retry_attempt_count. Wired unconditionally for the same reason as
			// the /admin/health derivation below: the manual-intervention population is largest
			// exactly when Plaid is unconfigured, so gating this on plaidService would silence the
			// signal in the deployment that needs it most.
			PlaidOrphanedItemRepo: plaidOrphanedItemRepo,
			// Issue #1477: resume a capped deleteExpiredAccounts run where it stopped instead of
			// restarting from the oldest expired user every run.
			SweepCursors: sweepCursorRepo,
		})
		// Issue #1413 Medium finding M9 (Codex review round 3): required for
		// send30dNotices/send7dNotices to gain EXCLUSIVITY on ActionRetentionNotice30d/7d
		// before a notice email is sent — replaces the old ClaimRetentionNotice30dSent/7dSent,
		// which stamped the *_sent_at marker BEFORE the send was attempted.
		retentionCleanupCheckWorker.SetWorkClaimsRepo(workClaimsRepo)
		appLogger.Info("Retention cleanup check worker created (River)")

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
		// Issue #1413 round-7: TrialService has no Plaid dependency at all — ExpireTrial never
		// calls Plaid. The disconnect lives entirely in RetentionCleanupCheckWorker's grace-window
		// repair pass (wired further below, once plaidCleanupService is available).
		trialService = services.NewTrialService(tierConfig, profileRepo)
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
			// #1569 T6: the daily run also records the row-level history behind each aggregate,
			// so a day with no Plaid sync is not a day task 11 has nothing to rebuild from.
			HoldingSnapshotRepo: repository.NewHoldingSnapshotRepository(db),
			Logger:              appLogger,
		})
		appLogger.Info("Portfolio snapshot worker created (River)")

		// #1625 Task 18 fix round 1: create rule quarantine census worker (daily full-table
		// scan feeding the distinct-rule population gauge). categorizationRuleRepo already exists
		// in this scope (constructed above for the categorization-rule service/API).
		ruleQuarantineCensusWorker = jobs.NewRuleQuarantineCensusWorker(jobs.RuleQuarantineCensusWorkerDeps{
			RuleRepo: categorizationRuleRepo,
			Logger:   appLogger,
		})
		appLogger.Info("Rule quarantine census worker created (River)")

		// Issue #1123: Create effective category refresh worker (no external dependencies)
		effectiveCategoryRefreshWorker = jobs.NewEffectiveCategoryRefreshWorker(jobs.EffectiveCategoryRefreshWorkerDeps{
			TransactionRepo: transactionRepo,
			Logger:          appLogger,
		})
		// Issue #1531 Task 10b-2a-iii: wire cache invalidation — this job rewrites
		// transactions.effective_category_name, the field several cached analytics queries
		// GROUP BY (spending by category/merchant, budget vs actual), with no prior bump.
		// cacheInstance is never nil (see cachePurgeRetryWorker below); Generations() is
		// nil-safe when the cache is disabled. Issue #1531 M7: also wires householdRepo so the
		// rename cascade bumps the household scope too, matching transactionSyncService's
		// SetCacheInvalidation call below — household-scoped spending-by-category/trends GROUP BY
		// this same effective_category_name column.
		effectiveCategoryRefreshWorker.SetCacheInvalidation(cacheInstance.Generations(), householdRepo)
		appLogger.Info("Effective category refresh worker created (River)")

		// Issue #1630 Task B1: Create budget hierarchy resync worker — recomputes spent_amount
		// for every active budget a custom-category reparent affects. customCategoryRepo,
		// budgetRepo and budgetCategoryMatchingService are all constructed above this point.
		budgetHierarchyResyncWorker = jobs.NewBudgetHierarchyResyncWorker(jobs.BudgetHierarchyResyncWorkerDeps{
			CustomCategoryRepo: customCategoryRepo,
			BudgetRepo:         budgetRepo,
			SpentCalc:          budgetCategoryMatchingService,
			Logger:             appLogger,
		})
		// spent_amount is summed by the cached GetBudgetVsActual analytics query (ScopeUser) —
		// see repository/cache_completeness_repo_test.go's
		// "BudgetRepository.UpdateCategorySpentAmount" entry. cacheInstance is never nil (see
		// cachePurgeRetryWorker below); Generations() is nil-safe when the cache is disabled.
		budgetHierarchyResyncWorker.SetCacheInvalidation(cacheInstance.Generations())
		appLogger.Info("Budget hierarchy resync worker created (River)")

		// Issue #1630 Task B7: Create budget hierarchy audit worker — periodic sweep over budgets
		// domain.DuplicateParentIdentity/domain.EscapedNestedChild would refuse if created today,
		// but which predate those write-time guards. budgetRepo and budgetCategoryMatchingService
		// are the same dependencies Task B1's resync worker uses, constructed above this point.
		budgetHierarchyAuditWorker = jobs.NewBudgetHierarchyAuditWorker(jobs.BudgetHierarchyAuditWorkerDeps{
			BudgetRepo: budgetRepo,
			SpentCalc:  budgetCategoryMatchingService,
			Logger:     appLogger,
		})
		// Same reasoning as budgetHierarchyResyncWorker directly above: spent_amount corrections
		// this audit makes are summed by the cached GetBudgetVsActual analytics query (ScopeUser).
		budgetHierarchyAuditWorker.SetCacheInvalidation(cacheInstance.Generations())
		appLogger.Info("Budget hierarchy audit worker created (River)")

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
				if userAccountService == nil {
					userAccountService = services.NewUserAccountService(
						repository.NewUserAccountRepository(db, fieldEncryptor))
				}
				accountService = services.NewAccountService(
					plaidService,
					linkedItemRepo,
					accountRepo,
					profileRepo,
					db,
					services.WithEncryptor(fieldEncryptor), // Issue #1010: Cross-user fingerprint checks
					// Issue #1569: required, not optional — see the declaration comment above.
					services.WithUserAccountService(userAccountService),
				)

				// Issue #1531 Blocker 1: wire cache invalidation for the cross-user fingerprint
				// release path — checkFingerprintOwnership's ExpireAccountsByFingerprint call
				// flips a DIFFERENT user's accounts to expired during THIS user's LinkAccounts
				// call, and the HTTP mutation middleware only ever bumps the acting user, never a
				// third party a write happens to touch. cacheInstance is never nil (see
				// cachePurgeRetryWorker's construction below); householdRepo structurally
				// satisfies services.SyncCacheHouseholdLookup, same as it already does for
				// transactionSyncService and humamiddleware.HouseholdLookup.
				accountService.SetCacheInvalidation(cacheInstance.Generations(), householdRepo)

				// Issue #1569, Task 5G: audit-log account.removed / institution.disconnected.
				// Nothing was audit-logged on either path before this.
				accountService.SetAuditLogger(auditLogger)

				// Issue #1569, Task 6: audit-log account.history_purged / linked_item.history_purged
				// INSIDE PurgeAccount/PurgeInstitution's own transaction, via LogInTx. auditLogger
				// (repository.NewAuditLogRepository) satisfies both AuditLogger and
				// AuditLogRepository, so the same instance backs both setters.
				accountService.SetAuditLogRepository(auditLogger)

				appLogger.Info("Plaid service initialized")
				appLogger.Info("Account linking service initialized")

				// Issue #668: Create PlaidItemCleanupService for Plaid item lifecycle management
				// This service handles calling Plaid /item/remove to stop billing
				plaidCleanupService = services.NewPlaidItemCleanupService(
					linkedItemRepo,
					plaidService,
				)
				// Issue #1413 H1: required for RemoveAllUserItems/DisconnectAllUserItemsIfEligible to gain
				// EXCLUSIVITY on ActionPlaidDisconnectUser before any Plaid /item/remove call —
				// closes the race where deletion, past-due dunning, the subscription-cancellation
				// webhook, and trial expiration (all sharing this one service instance) could each
				// hold their own, different flow claim and disconnect the same user's Plaid items
				// concurrently.
				plaidCleanupService.SetWorkClaimsRepo(workClaimsRepo)
				// Issue #1569 Task 20 (A2): required so that removing an item's Plaid Item also
				// transitions its accounts to 'removed'. Without it the accounts stayed 'active'
				// forever with no removed_at, and the frontend rendered their frozen balances as
				// current ones. The frontend helper is now getAccountClosedDate, which likewise
				// returns undefined for an active account; accountBalanceProjection nulls a
				// terminal account's balance server-side as well, but the status transition is
				// still what triggers both.
				// The service refuses to touch Plaid at all when this is unset, so a wiring
				// regression here fails loudly rather than silently reproducing the defect.
				plaidCleanupService.SetAccountRepo(accountRepo)
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

				// Issue #1531 Task 10b-2: wire cache invalidation so a Plaid sync (which never
				// passes through the HTTP mutation middleware — it runs from a River job)
				// still bumps the synced user's (and their household's) cache generation once,
				// at the end of SyncTransactions. cacheInstance is never nil (see the Task 10b
				// comment at cachePurgeRetryWorker's construction below); householdRepo
				// structurally satisfies services.SyncCacheHouseholdLookup, same as it already
				// does for humamiddleware.HouseholdLookup.
				transactionSyncService.SetCacheInvalidation(cacheInstance.Generations(), householdRepo)

				// Issue #1569, Task 5: audit-log account.removed_by_provider. When a bank closes an
				// account, Plaid stops returning it from /accounts/get and the sync soft-removes it
				// so it drops out of CURRENT net worth; without this wiring that removal would
				// happen with no trace a user could be shown. Same auditLogger instance as
				// accountService's user-initiated account.removed entries above.
				transactionSyncService.SetAuditLogger(auditLogger)

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
				// Issue #1620: repairNonceRepo backs the single-use nonce that binds
				// POST /api/v1/plaid/reconnect to a token this server minted. Deliberately the
				// DATABASE and not the cache: this process boots with a NoopCache when Redis is
				// absent or unreachable (see the cache wiring above), so a Redis-backed nonce
				// would fail open or fail closed depending on deploy configuration, silently.
				repairNonceRepo := repository.NewLinkedItemRepairNonceRepository(db)
				itemErrorService = services.NewItemErrorService(
					linkedItemRepo,
					notificationRepo,
					profileRepo,
					plaidService,
					emailService,
					repairNonceRepo,
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
					// #1631 M4: carries the investment backfill obligation recorded on
					// user_accounts.investment_backfill_requested_at when an owner re-includes a
					// previously excluded chain. A fresh repository rather than a shared handle
					// because this file constructs one per consumer already (see
					// userAccountService above); the repository is stateless.
					repository.NewUserAccountRepository(db, fieldEncryptor),
					plaidService,
					appLogger,
				)
				appLogger.Info("Investment sync service initialized")
				if phClient != nil {
					investmentSyncService.SetAnalytics(phClient)
				}
				// Issue #1531 Task 10b-2a-ii: wire cache invalidation so a HOLDINGS/
				// INVESTMENTS_TRANSACTIONS webhook (routed through ProcessWebhook, which is also
				// what InvestmentHoldingsSyncWorker/InvestmentTransactionsSyncWorker call) bumps
				// the synced user's cache generation — GetNetWorthTrend folds in holdings and
				// investment-transaction data. Scope is user-only (no household lookup), matching
				// the brief's table. cacheInstance is never nil (see cachePurgeRetryWorker below).
				investmentSyncService.SetCacheInvalidation(cacheInstance.Generations())

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
				// Issue #1531 Task 10b-2a-ii: wire cache invalidation so a LIABILITIES webhook
				// (routed through ProcessWebhook, which calls SyncLiabilities below) bumps the
				// synced user's cache generation — GetNetWorthTrend folds in liability data.
				liabilitySyncService.SetCacheInvalidation(cacheInstance.Generations())

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
				// Issue #1609 P3-T2: ItemErrorSvc routes permanent Plaid errors (INVALID_ACCESS_TOKEN,
				// ITEM_LOGIN_REQUIRED) discovered by a SYNC — not just a webhook — into the same
				// in-app notification. Wired for all three sync workers below (transaction and both
				// investment kinds): all three are enqueued together for a given item and a broken
				// access token fails all three identically.
				plaidSyncWorker = jobs.NewPlaidSyncWorker(jobs.PlaidSyncWorkerDeps{
					SyncService:    transactionSyncService,
					LinkedItemRepo: linkedItemRepo,
					JobQueue:       nil, // Set after River client created (circular dependency)
					Logger:         appLogger,
					DB:             db, // For row-level locking via FOR UPDATE
					ItemErrorSvc:   itemErrorService,
				})
				appLogger.Info("Plaid sync worker created")
				plaidSyncWorker.SetAnalytics(phClient)

				// Issue #659: Create InvestmentHoldingsSyncWorker
				// Issue #1609 P3-T2: ItemErrorSvc — see PlaidSyncWorker comment above.
				investmentHoldingsSyncWorker = jobs.NewInvestmentHoldingsSyncWorker(jobs.InvestmentHoldingsSyncWorkerDeps{
					InvestmentSyncSvc: investmentSyncService,
					LinkedItemRepo:    linkedItemRepo,
					Logger:            appLogger,
					DB:                db,
					ItemErrorSvc:      itemErrorService,
				})
				appLogger.Info("Investment holdings sync worker created")

				// Issue #659: Create InvestmentTransactionsSyncWorker
				// Issue #1609 P3-T2: ItemErrorSvc — see PlaidSyncWorker comment above.
				investmentTransactionsSyncWorker = jobs.NewInvestmentTransactionsSyncWorker(jobs.InvestmentTransactionsSyncWorkerDeps{
					InvestmentSyncSvc: investmentSyncService,
					LinkedItemRepo:    linkedItemRepo,
					Logger:            appLogger,
					DB:                db,
					ItemErrorSvc:      itemErrorService,
				})
				appLogger.Info("Investment transactions sync worker created")

				// Issue #1626 Phase 3, Defect 3: the periodic balance-snapshot gap-filler.
				//
				// balance_snapshots — what the net-worth and balance-history charts read — was
				// written ONLY as a side effect of a transaction sync, i.e. on initial link and on an
				// inbound Plaid TRANSACTIONS webhook. A day with no webhook was a day with no row,
				// permanently, because no backfill exists.
				//
				// The capture service is deliberately NOT transactionSyncService: it shares that
				// service's row-building code (writeBalanceCohort) but performs no account
				// reconciliation, which retires the whole partial-roster risk class rather than
				// guarding it.
				balanceCaptureService := services.NewBalanceCaptureService(services.BalanceCaptureServiceDeps{
					LinkedItemRepo:      linkedItemRepo,
					AccountRepo:         accountRepo,
					BalanceSnapshotRepo: balanceSnapshotRepo,
					PlaidService:        plaidService,
					Logger:              appLogger,
				})
				balanceSnapshotCaptureWorker = jobs.NewBalanceSnapshotCaptureWorker(jobs.BalanceSnapshotCaptureWorkerDeps{
					CaptureService: balanceCaptureService,
					Logger:         appLogger,
				})
				// The GENERATION bump, not NamespaceAnalytics.SchemaVersion — see the setter's own
				// comment. Without it a day's captured rows stay invisible to cached dashboard and
				// analytics reads until the namespace TTL, and a River job never passes through the
				// HTTP mutation middleware that would otherwise cover the write. householdRepo
				// structurally satisfies the lookup, same as it does for transactionSyncService.
				balanceSnapshotCaptureWorker.SetCacheInvalidation(cacheInstance.Generations(), householdRepo)
				balanceSnapshotDispatchWorker = jobs.NewBalanceSnapshotDispatchWorker(jobs.BalanceSnapshotDispatchWorkerDeps{
					ItemRepo: linkedItemRepo,
					JobQueue: nil, // Set after River client created (circular dependency)
					Logger:   appLogger,
					// MUST be wired. Without it every dispatch run re-enumerates from the oldest
					// active linked item, so above balanceCaptureItemsPerRun the same prefix is
					// captured every day and every newer item accrues no balance snapshot ever.
					SweepCursors: sweepCursorRepo,
				})
				appLogger.Info("Balance snapshot dispatch and capture workers created")

				// Issue #668: Create ItemCleanupCheckWorker for periodic orphaned item detection
				// Checks for items in error state, revoked, stale, or with zero accounts
				itemCleanupCheckWorker = jobs.NewItemCleanupCheckWorker(jobs.ItemCleanupCheckWorkerDeps{
					LinkedItemRepo:  linkedItemRepo,
					AccountRepo:     accountRepo, // For zero-account detection
					JobQueue:        nil,         // Set after River client created (circular dependency)
					Logger:          appLogger,
					GracePeriodDays: 7, // Days to wait before cleanup (error/stale/revoked items)
					// ErrorDaysThreshold / StaleDaysThreshold are deliberately LEFT UNSET here
					// (Issue #1486). They must equal ItemCleanupWorker's, which re-verifies
					// eligibility with them immediately before the irreversible Plaid /item/remove;
					// a stricter pair there would silently decline cleanups this sweep legitimately
					// scheduled, and each affected item would keep its cleanup_scheduled_at — hiding
					// it from every future sweep while Plaid kept billing. Passing literals to each
					// constructor separately made that parity comment-enforced and one-sidedly
					// editable. Unset means both constructors read the SAME
					// defaultCleanupErrorDays / defaultCleanupStaleDays (30 / 60, unchanged from the
					// literals this replaced), so the invariant is structural.

					// Issue #1477: resume a capped run where it stopped instead of restarting from
					// the oldest candidate item every run.
					SweepCursors: sweepCursorRepo,
				})
				appLogger.Info("Item cleanup check worker created")

				// Issue #668: Create ItemCleanupWorker for executing individual item cleanup
				// Calls Plaid /item/remove to stop billing for orphaned items
				// ErrorDaysThreshold/StaleDaysThreshold are left unset for the same reason as the
				// check worker's above (Issue #1486) — both then read the one shared pair of
				// constants, so the two cannot drift apart.
				itemCleanupWorker = jobs.NewItemCleanupWorker(jobs.ItemCleanupWorkerDeps{
					PlaidCleanupSvc: plaidCleanupService,
					LinkedItemRepo:  linkedItemRepo,
					Logger:          appLogger,
				})
				appLogger.Info("Item cleanup worker created")

				// Issue #1497: re-attempt /item/remove for Items stranded by a completed GDPR erasure,
				// using the credential retained on the orphan record until its 30-day deadline.
				//
				// It takes plaidService (not plaidCleanupService): the linked_items row those helpers
				// operate on no longer exists — that is what makes the Item an orphan — so the only
				// operation available is the raw /item/remove with the retained token.
				plaidOrphanRetrySvc := services.NewPlaidOrphanRetryService(plaidOrphanedItemRepo, plaidService)
				plaidOrphanRetrySvc.SetLogger(appLogger)
				plaidOrphanRetryWorker = jobs.NewPlaidOrphanRetryWorker(jobs.PlaidOrphanRetryWorkerDeps{
					Service: plaidOrphanRetrySvc,
					Logger:  appLogger,
					// Issue #1477: the fourth sweep of the same shape. Unwired, a record whose
					// /item/remove keeps failing at Plaid holds the head of the candidate set and
					// nothing past the per-run cap is ever retried.
					SweepCursors: sweepCursorRepo,
				})
				appLogger.Info("Plaid orphaned-item retry worker created")

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

			// Configure periodic jobs (all nil-guarded). Extracted to buildPeriodicJobs
			// (periodic_jobs.go) — Issue #1448 — so the "every registration has RunOnStart:
			// true, a non-empty PeriodicJobOpts.ID, and a real dedup guarantee" invariant is
			// unit-tested directly (periodic_jobs_test.go) instead of enforced only by a
			// main.go text scan.
			periodicJobs := buildPeriodicJobs(periodicJobsDeps{
				scheduledDeletionCheckWorker:        scheduledDeletionCheckWorker,
				itemCleanupCheckWorker:              itemCleanupCheckWorker,
				trialExpirationCheckWorker:          trialExpirationCheckWorker,
				pastDueCheckWorker:                  pastDueCheckWorker,
				paddleReconciliationWorker:          paddleReconciliationWorker,
				retentionCleanupCheckWorker:         retentionCleanupCheckWorker,
				manualTierOverrideExpiryCheckWorker: manualTierOverrideExpiryCheckWorker,
				householdMemberCleanupCheckWorker:   householdMemberCleanupCheckWorker,
				householdOverCapacityCheckWorker:    householdOverCapacityCheckWorker,
				dlqCleanupCheckWorker:               dlqCleanupCheckWorker,
				budgetPeriodCloseCheckWorker:        budgetPeriodCloseCheckWorker,
				savingsStatsWorker:                  savingsStatsWorker,
				portfolioSnapshotWorker:             portfolioSnapshotWorker,
				balanceSnapshotDispatchWorker:       balanceSnapshotDispatchWorker,
				ruleQuarantineCensusWorker:          ruleQuarantineCensusWorker,
				plaidOrphanRetryWorker:              plaidOrphanRetryWorker,
				budgetHierarchyAuditWorker:          budgetHierarchyAuditWorker,
			}, appLogger)

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

			// Issue #1497: constructed HERE, unconditionally, alongside idempotency cleanup rather than
			// inside the Plaid block above — see this worker's var declaration for why its activation must
			// not depend on Plaid being configured. Its only dependency is the database.
			plaidOrphanTokenPurgeWorker = jobs.NewPlaidOrphanTokenPurgeWorker(jobs.PlaidOrphanTokenPurgeWorkerDeps{
				Repo:   plaidOrphanedItemRepo,
				Logger: appLogger,
			})
			appLogger.Info("Plaid orphaned-item credential purge worker created (River)")

			// Register ALL workers with nil-guards.
			// Non-Plaid workers are always non-nil when their config gates are met.
			// Plaid-specific workers are nil when Plaid is not configured.
			riverWorkers := river.NewWorkers()

			// Non-Plaid workers (run unconditionally when configured)
			if emailChangePropagationWorker != nil {
				river.AddWorker(riverWorkers, emailChangePropagationWorker)
				appLogger.Info("Email change propagation worker registered with River")
			}

			// Issue #1531 Task 10b: durable retry for a failed GDPR-deletion cache purge (see
			// deletion_service.go's purgeCacheForDeletedUser / enqueueCachePurgeRetry). Always
			// registered — cacheInstance is never nil (it defaults to cache.NewNoopCache(), whose
			// Purger implementation is a no-op), so this worker is always constructible and simply
			// does nothing when the cache is disabled.
			cachePurgeRetryWorker := jobs.NewCachePurgeRetryWorker(cacheInstance.(cache.Purger), cacheInstance.Generations(), appLogger)
			river.AddWorker(riverWorkers, cachePurgeRetryWorker)
			appLogger.Info("Cache purge retry worker registered with River")
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
			// Issue #1497: the credential purge. Registered in the NON-Plaid block deliberately — its
			// periodic registration is unconditional (periodic_jobs.go), so a job would be enqueued with
			// no worker to run it if this registration ever moved inside the Plaid gate below.
			if plaidOrphanTokenPurgeWorker != nil {
				river.AddWorker(riverWorkers, plaidOrphanTokenPurgeWorker)
				appLogger.Info("Plaid orphaned-item credential purge worker registered with River")
			}
			if plaidOrphanRetryWorker != nil {
				river.AddWorker(riverWorkers, plaidOrphanRetryWorker)
				appLogger.Info("Plaid orphaned-item retry worker registered with River")
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
			if ruleQuarantineCensusWorker != nil {
				river.AddWorker(riverWorkers, ruleQuarantineCensusWorker)
				appLogger.Info("Rule quarantine census worker registered with River")
			}
			if effectiveCategoryRefreshWorker != nil {
				river.AddWorker(riverWorkers, effectiveCategoryRefreshWorker)
				appLogger.Info("Effective category refresh worker registered with River")
			}
			if budgetHierarchyResyncWorker != nil {
				river.AddWorker(riverWorkers, budgetHierarchyResyncWorker)
				appLogger.Info("Budget hierarchy resync worker registered with River")
			}
			if budgetHierarchyAuditWorker != nil {
				river.AddWorker(riverWorkers, budgetHierarchyAuditWorker)
				appLogger.Info("Budget hierarchy audit worker registered with River")
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
			// Issue #1626 Phase 3, Defect 3. Registered as a PAIR: the periodic dispatcher is only
			// registered when its worker is non-nil (periodic_jobs.go), and the capture worker must
			// be registered whenever the dispatcher is, or the dispatcher enqueues jobs River has
			// nothing to run.
			if balanceSnapshotDispatchWorker != nil {
				river.AddWorker(riverWorkers, balanceSnapshotDispatchWorker)
				appLogger.Info("Balance snapshot dispatch worker registered with River")
			}
			if balanceSnapshotCaptureWorker != nil {
				river.AddWorker(riverWorkers, balanceSnapshotCaptureWorker)
				appLogger.Info("Balance snapshot capture worker registered with River")
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
				// #1631 M4: nudge an investment-transactions sync when the owner re-includes a
				// previously excluded chain, so the recorded backfill obligation is honoured now
				// instead of at Plaid's next INVESTMENTS_TRANSACTIONS webhook — which for a quiet
				// brokerage can be months away.
				//
				// ⚠ Wired HERE, alongside the other post-River SetJobQueue calls, and NOT beside
				// NewUserAccountService: `jobQueue` is nil until the River client is built a few
				// lines above, so the earlier site would have silently wired a no-op nudge. The
				// obligation itself lives on the user_accounts row and is honoured by whichever
				// sync runs next, so even a missed wiring costs latency, not data.
				if userAccountService != nil {
					userAccountService.SetInvestmentBackfillEnqueuer(jobQueue, accountRepo, appLogger)
					appLogger.Info("User account service connected to job queue for investment backfill nudges")
				}
				if plaidSyncWorker != nil {
					plaidSyncWorker.SetJobQueue(jobQueue)
					appLogger.Info("Plaid sync worker connected to job queue for follow-up jobs")
				}
				if itemCleanupCheckWorker != nil {
					itemCleanupCheckWorker.SetJobQueue(jobQueue)
					appLogger.Info("Item cleanup check worker connected to job queue")
				}
				// Issue #1626 Phase 3, Defect 3: the dispatcher's ONLY action is enqueueing, so an
				// unwired queue makes it useless rather than degraded — its Work() returns an error
				// instead of a silent no-op, which on a daily schedule would otherwise mean no
				// balance history for anybody, indefinitely.
				if balanceSnapshotDispatchWorker != nil {
					balanceSnapshotDispatchWorker.SetJobQueue(jobQueue)
					appLogger.Info("Balance snapshot dispatch worker connected to job queue")
				}
				if budgetPeriodCloseCheckWorker != nil {
					budgetPeriodCloseCheckWorker.SetJobQueue(jobQueue)
					appLogger.Info("Budget period close check worker connected to job queue")
				}

				// Issue #1123: Wire job queue for async effective_category_name refresh
				customCategoryService.SetJobQueue(jobQueue)
				appLogger.Info("Custom category service connected to job queue for rename cascade")

				// Issue #1531 Task 10b: GDPR deletion cache purge. Wired here (not alongside
				// deletionService's other Set* calls above) because it needs jobQueue for the
				// durable-retry enqueue, and jobQueue isn't constructed until riverClient succeeds,
				// just above. cacheInstance.(cache.Purger) always succeeds: both NewCache's
				// production type and NewNoopCache's disabled-cache type implement Purger (the
				// latter as a no-op) — see cache.Purger's doc comment.
				//
				// #1531 Task 11e: deletionService itself CAN be nil here — it is only assigned when
				// cfg.DeletionTokenSecret != "" (see the `if cfg.DeletionTokenSecret != ""` gate
				// far above) — and this call was previously unconditional, so every deployment
				// without DELETION_TOKEN_SECRET configured (every local/E2E run before this fix,
				// confirmed via `bun run test:e2e` panicking at server startup with a nil-pointer
				// dereference on this exact line) crashed the process before it ever bound a port.
				// Every other caller of deletionService in this file (see e.g. the
				// `paddleService != nil && deletionService != nil` and
				// `if deletionService != nil { deletionService.SetPlaidCleanupService(...) }` guards
				// above) already nil-checks it; this call simply never got one.
				if deletionService != nil {
					deletionService.SetCache(cacheInstance.(cache.Purger), cacheInstance.Generations(), jobQueue)
					appLogger.Info("Deletion service connected to cache purge + job queue for GDPR retry")
				}
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
			// Issue #1531 Task 10a: cached scalar-UUID fast path — see cachedIdentityResolver's
			// construction above.
			cachedIdentityResolver,
			profileRepo,
		)
		humaAPI.UseMiddleware(humaClerkMiddleware)
		appLogger.Info("Huma Clerk middleware applied")

		// #930: Apply read-only enforcement middleware — blocks mutations for inactive/cancelled subscriptions
		// #1367 — Phase 3: household-aware — a covered member's own stale read-only status no longer locks them out.
		humaAPI.UseMiddleware(humamiddleware.NewReadOnlyMiddleware(householdEntitlementService))
		appLogger.Info("Huma read-only enforcement middleware applied")

		// Issue #1531 Task 10b: cache-invalidation middleware. MUST register after both the Clerk
		// middleware (so inputs.UserIDKey is populated) and NewReadOnlyMiddleware (so a request the
		// read-only gate blocked with 403/503 never reaches this middleware and never bumps —
		// nothing committed). cacheInstance.Generations() is nil for a disabled/noop cache (Task 9's
		// NewNoopCache), which the middleware treats as a pure pass-through — see its own doc
		// comment. householdRepo satisfies humamiddleware.HouseholdLookup structurally.
		humaAPI.UseMiddleware(humamiddleware.NewCacheInvalidationMiddleware(cacheInstance.Generations(), householdRepo, appLogger))
		appLogger.Info("Cache invalidation middleware applied")

		// Load admin email config and create AdminService (Issue #309)
		//
		// Issue #1498: resolve the two lists against the environment. The hardcoded list
		// bypasses the role check, so the CI/QA entries in non_production_admin_emails must
		// not load in production — nor under an unset or unrecognised ENVIRONMENT, both of
		// which EffectiveAdminEmails treats as production. Never pass AdminEmails directly.
		//
		// cfg.AdminAllowlistEnvironment(), NOT cfg.Environment: the latter defaults to
		// "development" when ENVIRONMENT is unset, and development is a recognised
		// non-production environment, so using it would load the test admins on a
		// production deployment that forgot the variable. The same value also decides whether
		// the ADMIN_EMAILS_CONFIG relocation override is honoured at all — one notion of
		// "is this production?" for the whole authorization path, not two.
		allowlistEnv := cfg.AdminAllowlistEnvironment()
		adminEmailConfig, err := config.LoadAdminEmailConfig(allowlistEnv)
		if err != nil {
			appLogger.Warn("Failed to load admin email config (using empty list)", "error", err)
			adminEmailConfig = &domain.AdminEmailConfig{AdminEmails: []string{}}
		}
		effectiveAdminEmails := adminEmailConfig.EffectiveAdminEmails(allowlistEnv)
		adminService := services.NewAdminService(cfg.ClerkSecretKey, effectiveAdminEmails)

		// Log the posture, never the addresses: an operator must be able to see from the
		// startup log which of the two postures is live, and why.
		adminEmailMode := "production-safe (non-production admin entries excluded)"
		if domain.IsNonProductionEnvironment(allowlistEnv) {
			adminEmailMode = "non-production (test admin entries loaded)"
		}
		appLogger.Info("Admin service initialized",
			"environment", cfg.Environment,
			"environment_explicitly_set", cfg.EnvironmentExplicitlySet,
			"admin_email_mode", adminEmailMode,
			"effective_hardcoded_admin_emails", len(effectiveAdminEmails),
			"non_production_admin_emails_in_file", len(adminEmailConfig.NonProductionAdminEmails))
		// Warn on BOTH ways of arriving at the production posture by accident, not just the
		// unset one: ENVIRONMENT=" " or ENVIRONMENT=prod counts as explicitly set, so an
		// `if !EnvironmentExplicitlySet` check alone would stay silent about a misconfigured
		// value while still (correctly) refusing to load the test admins.
		switch {
		case !cfg.EnvironmentExplicitlySet:
			appLogger.Warn("ENVIRONMENT is not set; the admin allowlist is resolving as production. "+
				"Set ENVIRONMENT explicitly (development/qa/staging/production).",
				"effective_environment", cfg.Environment)
		case !domain.IsNonProductionEnvironment(allowlistEnv) && allowlistEnv != "production":
			appLogger.Warn("ENVIRONMENT is set to a value this build does not recognise; the admin "+
				"allowlist is resolving as production. Expected one of "+
				"development/qa/staging/production.",
				"environment", cfg.Environment)
		}
		// The admin allowlist is an ACL, so ADMIN_EMAILS_CONFIG is an authorization input.
		// Issue #1498: outside a recognised non-production environment the loader REFUSES it and
		// reads the committed config/admin_emails.yaml instead — a warning after the fact would
		// be detective, not preventive. Say either outcome loudly; a silently-ignored override is
		// its own trap.
		if override, honoured := config.AdminEmailsConfigOverride(allowlistEnv); override != "" {
			if honoured {
				appLogger.Warn("Admin email allowlist loaded from an ADMIN_EMAILS_CONFIG override, "+
					"not the committed config/admin_emails.yaml",
					"path", override,
					"effective_hardcoded_admin_emails", len(effectiveAdminEmails))
			} else {
				appLogger.Error("ADMIN_EMAILS_CONFIG was set and REJECTED: the admin allowlist may "+
					"only be relocated in a recognised non-production environment. Loaded the "+
					"committed config/admin_emails.yaml instead.",
					"rejected_path", override,
					"admin_allowlist_environment", allowlistEnv,
					"environment_explicitly_set", cfg.EnvironmentExplicitlySet,
					"effective_hardcoded_admin_emails", len(effectiveAdminEmails))
			}
		}

		// Issue #639: Admin user management - needs adminService for admin status checks
		// Issue #1414: clerkService re-syncs Clerk metadata on tier-override removal;
		// RetentionPeriodMonths sizes the retention-clock re-arm (same value trialService.SetRetentionMonths gets).
		// Issue #1413: tierConfig validates manual tier overrides and sizes the household seat-capacity warning.
		adminUserService := services.NewAdminUserService(adminUserRepo, adminService, clerkService, cfg.RetentionPeriodMonths, tierConfig)
		// Issue #1413 Task NB (Codex High finding): these events land on the TARGET user's
		// PostHog profile (their UUID is distinct_id), so consent is gated the same way
		// transfer_suggestion_service's user-initiated events are — via gdprClient, not the raw
		// phClient other admin-triggered-but-operational events use.
		adminUserService.SetGDPRAnalytics(gdprClient)

		// Issue #640: System health monitoring service
		systemHealthService := services.NewSystemHealthService(
			systemHealthRepo,
			deadLetterJobRepo,
			&dbPoolAdapter{db},
			cfg.PlaidEnv,
			jobHealthRepoForHealth, // Issue #1458: backs the 3 backlog gauges on /admin/health
			// Issue #1497 item 5: backs the orphaned-Plaid-Item state counts on /admin/health.
			// The SAME repository the admin list view and both sweeps use, narrowed at the
			// interface to List alone (services.PlaidOrphanedItemLister) so a read surface cannot
			// purge a retained credential. NOT gated on plaidService != nil, for the reason #1499
			// ungated the admin routes themselves: these rows exist because a GDPR erasure
			// happened, whether or not this deployment can talk to Plaid, and a deployment without
			// Plaid credentials is precisely the one where nothing automated will ever clear them.
			plaidOrphanedItemRepo,
			// Issue #1514 requirement #5: backs the "Rejected webhook attempts (24h)" tile —
			// the operator-visibility signal restored after Phase 1 removed invalid-signature
			// webhooks from paddle_webhook_events/webhook_events. Narrowed at the interface
			// (services.InvalidWebhookAttemptStatsReader) to GetStats24h alone, same discipline
			// as plaidOrphanedItemRepo above.
			invalidWebhookAttemptRepo,
			// Issue #1531 Task 7d: the SAME shared, memoized Clerk/Redis Check instances the
			// public /health handler's registry holds, plus the limiter client's pool-stats
			// reader — so /admin/health's Redis detail costs zero extra PINGs beyond what the
			// liveness probe already paid for. See services.SharedHealthChecks's doc comment.
			sharedHealthChecks,
			// Issue #1531 Task 11e: cacheInstance satisfies services.CacheStatusReader structurally
			// (its Enabled(ns) method) — never nil (defaults to cache.NewNoopCache(), whose Enabled
			// always reports false), so /admin/health's cache_enabled field has a live signal to
			// report regardless of whether caching is actually turned on for this deployment.
			cacheInstance,
			// #1631 M8b: backs the "forked security identities" section on /admin/health. The SAME
			// repository investment sync uses, narrowed at the interface
			// (services.ForkedSecurityIdentityLister) to FindForkedIdentities alone, same
			// discipline as plaidOrphanedItemRepo and invalidWebhookAttemptRepo above.
			securityRepo,
			// #1631 M8b I1: backs the global position-reconciliation drift gauge on /admin/health.
			// The SAME repository the investment analytics endpoints use, narrowed at the interface
			// (services.PositionReconciliationSummarizer) to PositionReconciliationSummary alone.
			investmentAnalyticsRepo,
		)

		// Issue #633: Dead letter queue admin service.
		// Issue #1492: jobs.UniqueOptsForKind gives the replay path the same uniqueness options each
		// kind's real producers pass. It is injected here because internal/services cannot import
		// internal/queue/jobs (jobs imports services), and this file already imports both.
		deadLetterService := services.NewDeadLetterService(deadLetterJobRepo, jobQueue, jobs.UniqueOptsForKind)

		// Apply Huma Admin middleware for admin routes (Issue #309)
		humaAdminMiddleware := humamiddleware.NewAdminMiddleware(cfg, adminService)
		humaAPI.UseMiddleware(humaAdminMiddleware)
		appLogger.Info("Huma Admin middleware applied")

		// Register Huma deletion routes (Issue #559: Huma migration).
		// Issue #1560: MUST stay below every humaAPI.UseMiddleware call — huma snapshots the
		// middleware chain when Register runs, so registering earlier silently opts these routes
		// out of Clerk auth, read-only enforcement and cache invalidation. Guarded on
		// deletionService rather than cfg.DeletionTokenSecret so this cannot drift from the
		// construction block above, which is what decides whether the service exists at all.
		// TestMutatingRoutesRegisteredAfterMiddlewareChain enforces the ordering, that this
		// group stays registered, and that it keeps both of its paths.
		if deletionService != nil {
			humaDeletionHandler := operations.NewDeletionHandler(deletionService)
			operations.RegisterDeletionRoutes(humaAPI, humaDeletionHandler)
			appLogger.Info("Huma deletion routes registered")
		} else {
			appLogger.Warn("Deletion routes NOT registered — deletion service unavailable " +
				"(DELETION_TOKEN_SECRET unset); POST /api/v1/user/delete will 404")
		}

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
		// Issue #1569: reuse the instance wired into accountService above rather than constructing a
		// second one, so the linking path and the routes cannot diverge.
		if userAccountService == nil {
			userAccountService = services.NewUserAccountService(
				repository.NewUserAccountRepository(db, fieldEncryptor))
		}
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

		// Issue #1569 Task 5 (#1572): the automation audit trail's read path. The writer half is
		// wired above, onto ruleApplicationService. A sibling path, not a child of
		// /api/v1/auto-categorization — see RegisterRuleExecutionRoutes for why.
		operations.RegisterRuleExecutionRoutes(humaAPI,
			operations.NewRuleExecutionHandler(services.NewRuleExecutionHistoryService(ruleExecutionHistoryRepo)))
		appLogger.Info("Huma rule execution history routes registered")

		// Issue #1569 Task 6 (#1579): the annotation trail's read path. There is no writer to wire:
		// the capture is a CTE inside the three UPDATE statements in transaction_repository.go, so
		// it cannot be left unwired by a future call site the way a Go writer can.
		operations.RegisterTransactionAnnotationHistoryRoutes(humaAPI,
			operations.NewTransactionAnnotationHistoryHandler(
				services.NewTransactionAnnotationHistoryService(
					repository.NewTransactionAnnotationHistoryRepository(db))))
		appLogger.Info("Huma transaction annotation history routes registered")

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

		// Register retained holdings history route (Issue #1569).
		// Its own service and its own repository read: this is the HISTORICAL universe, and it is
		// wired separately from investmentService so no current-state caller can reach it.
		humaHoldingHistoryHandler := operations.NewHoldingHistoryHandler(
			services.NewHoldingHistoryService(repository.NewHoldingSnapshotRepository(db)),
		)
		operations.RegisterHoldingHistoryRoutes(humaAPI, humaHoldingHistoryHandler)
		appLogger.Info("Retained holdings history route registered")

		// Register retained per-account balance history route (Issue #1569).
		// Same shape and the same reason as the holdings history above: its own service over its
		// own repository read, wired away from every current-state balance caller so none can
		// reach it and none of its figures can be mistaken for a live balance.
		humaAccountBalanceHistoryHandler := operations.NewAccountBalanceHistoryHandler(
			services.NewAccountBalanceHistoryService(repository.NewBalanceSnapshotRepository(db)),
		)
		operations.RegisterAccountBalanceHistoryRoutes(humaAPI, humaAccountBalanceHistoryHandler)
		appLogger.Info("Retained account balance history route registered")

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
			PlanService:          services.NewBudgetPlanService(repository.NewBudgetPlanRepository(db)),
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
		// Issue #1569 round 17 task 6: DeleteContribution is the one deliberate user-facing hard
		// delete of financial history; its audit row must be written INSIDE the delete's own
		// transaction. Same construction as savingsGoalService's wiring above — a fresh
		// AuditLogRepository, because `auditLogger` is scoped to a different block.
		humaContributionHandler.SetAuditLogRepository(repository.NewAuditLogRepository(db))
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

		// Register the reconnect-flow routes (Issue #1609): mint an update-mode link token for a
		// broken linked item, and clear its error state once Plaid Link succeeds. Separate from
		// RegisterPlaidRoutes above because it depends on itemErrorService, not accountService.
		if itemErrorService != nil {
			humaPlaidReconnectHandler := operations.NewPlaidReconnectHandler(itemErrorService)
			operations.RegisterPlaidReconnectRoutes(humaAPI, humaPlaidReconnectHandler)
			appLogger.Info("Huma plaid reconnect routes registered")
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

		// Support identity is signed from the authenticated Clerk subject. The
		// PostHog secret token remains server-only and an absent token fails closed.
		humaPostHogSupportIdentityHandler := operations.NewPostHogSupportIdentityHandler(cfg.PostHogAPIKey, cfg.PostHogSecretAPIToken)
		operations.RegisterPostHogSupportIdentityRoutes(humaAPI, humaPostHogSupportIdentityHandler)
		appLogger.Info("Huma PostHog Support identity route registered")

		// Register the Huma data-export route (Issue #1587).
		// ⚠ Also registered in operations.RegisterAllRoutesForSpec — `make generate-openapi` reads
		// that function and never this file, so a route wired only here is invisible to the spec
		// and to the generated client.
		humaDataExportHandler := operations.NewDataExportHandler(dataExportService)
		operations.RegisterDataExportRoutes(humaAPI, humaDataExportHandler)
		appLogger.Info("Huma data export route registered")

		// Register Huma category preview routes (Issue #559: Huma migration)
		humaCategoryPreviewHandler := operations.NewCategoryPreviewHandler(categoryPreviewService)
		operations.RegisterCategoryPreviewRoutes(humaAPI, humaCategoryPreviewHandler)
		appLogger.Info("Huma category preview routes registered")

		// Register Huma admin routes (Issue #559: Huma migration)
		// Security: AdminMiddleware authorizes by identity and role — 401 unauthenticated,
		// 403 without domain.RoleAdmin, 500 (refused) if the role lookup fails. The role lookup
		// reads CLERK privateMetadata, not the database (#1432 B2-3), and its positive results
		// are cached, so a revocation takes effect within AdminService.positiveRoleCacheTTLCap
		// rather than instantly. Issue #1499
		// removed its environment check, so this surface is reachable in production BY ADMINS;
		// the role gate, not the deployment environment, is what keeps everyone else out.
		// Issue #698: Pass trialService for E2E test trial grant endpoint
		// Issue #638: Pass adminService for check-role endpoint
		// Issue #639: Pass adminUserService for user management endpoints
		// Issue #640: Pass systemHealthService for system health monitoring
		//
		// Issue #1499: registration is deliberately NOT gated on plaidService != nil. It used to
		// be, which meant a deployment without Plaid credentials silently lost all 24 admin
		// endpoints — system health, user management, subscription admin, the dead-letter queue
		// and the orphaned-Plaid-Item view — none of which is Plaid-specific. plaidService and
		// linkedItemRepo feed exactly one handler (FireWebhook, a QA webhook-firing helper),
		// which returns 503 when either is nil rather than dereferencing it.
		humaAdminHandler := operations.NewAdminHandler(plaidService, linkedItemRepo, trialService, adminService, adminUserService, systemHealthService, subscriptionService, cfg).
			WithPaddleTransactionRepo(paddleTransactionRepo).
			WithPaddleWebhookRepo(paddleWebhookRepo).
			WithJobQueue(jobQueue).
			WithDeadLetterService(deadLetterService).
			WithPlaidOrphanedItemRepo(plaidOrphanedItemRepo).
			// Issue #1584: the enumerating read over accounts the #1569 T1 pre-deletion-notice gate
			// is holding indefinitely. Without this the endpoint registers and answers 503 — the
			// same shape the other optional admin dependencies use.
			WithRetentionWithheldReader(profileRepo).
			// Issue #1630 Task B7: the enumerating read over budgets the hierarchy audit has
			// flagged for a human. budgetRepo already implements
			// operations.BudgetHierarchyAuditReader via ListBudgetHierarchyAuditFindings.
			WithBudgetHierarchyAuditReader(budgetRepo)
		operations.RegisterAdminRoutes(humaAPI, humaAdminHandler)
		appLogger.Info("Huma admin routes registered (protected by AdminMiddleware)", "plaid_configured", plaidService != nil)

		// Issue #821: QA auth bypass endpoint (QA/Staging only).
		// Guard 1: the registration gate below. Guard 2: the handler re-checks the environment
		// against domain.IsQAAuthBypassEnvironment in Bypass().
		//
		// Issue #1432 batch ED, finding B2-1: this condition used to be
		// `cfg.Environment == "qa" || cfg.Environment == "staging"` plus three non-empty QA
		// settings — a single mislabelled ENVIRONMENT string exposed the only unauthenticated
		// path to admin authority in this codebase. The decision now lives in
		// Config.QAAuthBypassRefusalReason (internal/config/qa_auth_bypass_gate.go), which
		// additionally requires ENVIRONMENT to have been set EXPLICITLY, requires the
		// QA_AUTH_BYPASS_ENABLED opt-in, and refuses outright when a production-only signal
		// (a Clerk sk_live_ key, PADDLE_ENVIRONMENT=production) is present. Keep the decision
		// there — it is unit-tested; an inline equality here is not.
		if refusal := cfg.QAAuthBypassRefusalReason(); refusal == "" {
			qaBypassService := services.NewQAAuthBypassService(cfg.QAAdminClerkUserID, cfg.QAPremiumClerkUserID, nil)
			qaBypassHandler := operations.NewQAAuthBypassHandler(qaBypassService, cfg.QABypassSecret, cfg.Environment)
			operations.RegisterQAAuthBypassRoutes(humaAPI, qaBypassHandler)
			appLogger.Warn("QA auth bypass route registered — this route mints admin sign-in tokens without authentication and must never exist in production",
				"environment", cfg.Environment)
		} else if cfg.QABypassSecret != "" || cfg.QAAdminClerkUserID != "" || cfg.QAPremiumClerkUserID != "" || cfg.QAAuthBypassEnabled {
			// Only log the refusal when SOMETHING QA-shaped was configured. A production
			// deployment with none of these set is the normal case and needs no noise; a
			// deployment that meant to enable the route needs to know which check refused.
			appLogger.Warn(refusal)
		}

		// All API v1 routes fully migrated to Huma (Issue #559)
		// The legacy Echo group and its Clerk middleware have been removed —
		// the Echo group's prefix-matching was shadowing Huma routes and
		// blocking requests with 401 (the Echo middleware lacks the Clerk API
		// email fallback that the Huma middleware provides).

		appLogger.Info("Authenticated routes enabled (Clerk + Database)")
	} else if msg, attrs, warranted := clerkGateWarning(db != nil, cfg.ClerkSecretKey != ""); warranted {
		// Issue #1450: this whole block (background job registration + Huma admin/authenticated
		// routes) is gated on cfg.ClerkSecretKey != "", not just db != nil. The gate is
		// intentional — Clerk is this app's identity source and most of the above touches user
		// records — but a closed gate used to leave NOTHING behind to say background processing
		// stopped. Concretely, every periodic job configured inside the gate above never runs,
		// including RetentionCleanupCheckWorker — the ONLY place that runs retention-clock
		// repair, the 30-day/7-day GDPR pre-deletion notices, deleteExpiredAccounts, and the
		// Plaid-disconnect repair pass.
		//
		// The decision and the message live in clerkGateWarning (clerk_gate_warning.go) so a test
		// can assert on their real values instead of text-scanning this file — see that file's
		// header for why the original inline version's test was unsound.
		appLogger.Warn(msg, attrs...)
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
			WebhookService:     webhookService,
			WebhookRepo:        webhookRepoForHandler,
			InvalidAttemptRepo: repository.NewInvalidWebhookAttemptRepository(db, cfg.InvalidWebhookAttemptRetentionDays), // Issue #1514
			JobQueue:           jobQueue,
			Logger:             appLogger,
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
			WebhookRepo:        repository.NewWebhookRepository(db, fieldEncryptor),
			InvalidAttemptRepo: repository.NewInvalidWebhookAttemptRepository(db, cfg.InvalidWebhookAttemptRetentionDays), // Issue #1514
			JobQueue:           jobQueue,
			Logger:             appLogger,
		})
		operations.RegisterPlaidWebhookRoutes(humaAPI, humaPlaidWebhookHandler)
		appLogger.Info("Huma Plaid webhook route registered at POST /webhooks/plaid (no verifier configured)")
		// Issue #1514: no webhookService means HandlePlaidWebhook's verifySignature always fails
		// closed — every delivery to this route now gets rejected with 401, it is NOT processed
		// audit-trail-style as before this fix. Log that plainly so local/QA debugging doesn't
		// mistake "every webhook 401s" for a bug.
		appLogger.Warn("Plaid webhook signature verifier not configured — ALL incoming webhooks will be rejected (401), not processed (Plaid credentials not configured)")
	} else {
		appLogger.Warn("Webhook endpoint disabled (requires DATABASE_URL)")
	}

	// Paddle webhook endpoint (public - no auth required) - Issue #46: Paddle Webhook Handler
	// Paddle servers need to POST to this endpoint without authentication
	// Issue #45, #46: Paddle payment integration with HMAC-SHA256 signature verification
	if db != nil && paddleWebhookService != nil {
		// Register Huma paddle webhook route
		humaPaddleWebhookHandler := operations.NewPaddleWebhookHandler(operations.PaddleWebhookHandlerDeps{
			WebhookService:     paddleWebhookService,
			WebhookRepo:        paddleWebhookRepo,
			InvalidAttemptRepo: invalidWebhookAttemptRepo, // Issue #1514
			JobQueue:           jobQueue,
			Logger:             appLogger,
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
			WebhookRepo:        paddleWebhookRepo,
			InvalidAttemptRepo: invalidWebhookAttemptRepo, // Issue #1514
			JobQueue:           jobQueue,
			Logger:             appLogger,
		})
		operations.RegisterPaddleWebhookRoutes(humaAPI, humaPaddleWebhookHandler)
		appLogger.Info("Huma Paddle webhook route registered at POST /webhooks/paddle (no verifier configured)")
		// Issue #1514: no webhookService means HandlePaddleWebhook's verifySignature always fails
		// closed — every delivery to this route now gets rejected with 401, it is NOT processed
		// audit-trail-style as before this fix.
		appLogger.Warn("Paddle webhook signature verifier not configured — ALL incoming webhooks will be rejected (401), not processed (webhook secret not configured)")
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

	// Issue #1531 Task 7a: waitForShutdownSignal registers BOTH SIGINT and SIGTERM — Fly's
	// kill_signal is SIGTERM (see fly.toml/fly.staging.toml), and before this fix the whole
	// graceful-shutdown sequence below had never once run in production; every deploy hard-killed
	// in-flight requests and River jobs. See waitForShutdownSignal's doc comment.
	waitForShutdownSignal()

	appLogger.Info("Shutting down server...")

	// Issue #1531 Task 7b: shutdownSequence runs these IN THIS EXACT ORDER and continues past any
	// failing step, aggregating errors — see shutdownSequence's doc comment for the full
	// rationale. Required order:
	//   1. Drain HTTP (public listener, then the internal metrics listener).
	//   2. Stop/drain River, the boost scheduler, and their launch context — Redis-touching
	//      background work must finish BEFORE the Redis clients close.
	//   3. Cancel the Cloudflare CIDR refresher and close the rate-limit Done channel (stops the
	//      rate limiter's cleanup goroutine and Task 5's accumulateActiveSecondsWhileDegraded).
	//   4. Close the cache Redis client, then the limiter Redis client, LAST.
	// ⚠ Closing Redis before HTTP has drained gives every in-flight request a pool-closed error →
	// limiter fallback → a fallback-activation Sentry event on every single deploy, training
	// everyone to ignore the single most important alert in this whole change.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	steps := []namedShutdownStep{
		{Name: "http", Fn: func() error {
			return e.Shutdown(ctx)
		}},
		{Name: "internal_metrics_listener", Fn: func() error {
			// Issue #1514: shut down the internal metrics listener alongside the public one so
			// neither leaks a bound port or an unclosed goroutine on exit. Nil when
			// ENABLE_METRICS=false (listener was never started above).
			if internalMetricsEcho == nil {
				return nil
			}
			return internalMetricsEcho.Shutdown(ctx)
		}},
		{Name: "boost_scheduler", Fn: func() error {
			// Issue #37: Stop rule scheduler manager (graceful shutdown).
			if boostSchedulerManager == nil {
				return nil
			}
			stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer stopCancel()
			return boostSchedulerManager.Stop(stopCtx)
		}},
		{Name: "river", Fn: func() error {
			// Issue #573: Stop River queue runner gracefully.
			if riverRunner == nil {
				return nil
			}
			stopCtx, stopCancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer stopCancel()
			if err := riverRunner.Stop(stopCtx); err != nil {
				// Try force stop if graceful stop fails.
				if cancelErr := riverRunner.StopAndCancel(stopCtx); cancelErr != nil {
					return fmt.Errorf("graceful stop: %w; force stop: %v", err, cancelErr)
				}
				return nil
			}
			return nil
		}},
		{Name: "worker_ctx_cancel", Fn: func() error {
			if workerCancel != nil {
				workerCancel()
			}
			return nil
		}},
		{Name: "cf_cidr_refresher", Fn: func() error {
			cfCancel()
			return nil
		}},
		{Name: "rate_limit_done", Fn: func() error {
			// Issue #1531 Task 5/7b: stops IPRateLimiter's cleanup goroutine and the fallback
			// limiter's accumulateActiveSecondsWhileDegraded goroutine.
			close(rateLimitDone)
			return nil
		}},
		{Name: "redis_metrics_collector", Fn: func() error {
			// Fix round 1 (Issue #1531 Task 7 review): stops
			// observability.StartRedisMetricsCollector's goroutine. Ordered here — after HTTP/River
			// have drained, before the Redis clients close below — per the brief's step 3.
			redisMetricsCancel()
			return nil
		}},
		{Name: "cache_control_poller", Fn: func() error {
			// Issue #1531 Task 10a: stops Control.Run's polling goroutine (if started). Same
			// ordering rationale as redis_metrics_collector above — it reads the limiter Redis
			// client, so it must stop before that client closes below.
			cacheControlCancel()
			return nil
		}},
		{Name: "cache_redis_client", Fn: func() error {
			if cacheClient == nil {
				return nil
			}
			return cacheClient.Close()
		}},
		{Name: "limiter_redis_client", Fn: func() error {
			if limiterClient == nil {
				return nil
			}
			return limiterClient.Close()
		}},
	}

	if err := shutdownSequence(steps); err != nil {
		appLogger.Error("Server forced to shutdown with errors", "error", err)
		os.Exit(1)
	}

	appLogger.Info("Server stopped gracefully")
}

// registerCloudflareAndRateLimitMiddleware registers, in this exact order,
// the two middleware whose relative order the rate limiter's identity model
// depends on: CloudflareOnly MUST run before IPRateLimiter.
//
// Why the order is load-bearing: IPRateLimiter's clientIdentity (see
// internal/middleware/rate_limit.go) trusts the Cf-Connecting-Ip header only
// when the current request's echo.Context already carries
// middleware.GateStatusPassed — the status CloudflareOnly stamps onto the
// context after verifying the immediate peer is a genuine Cloudflare edge IP
// and the X-Origin-Secret matched. That status is set on the SAME
// echo.Context that flows through the rest of the chain, so it is only
// visible to middleware that runs AFTER CloudflareOnly in registration order.
// If IPRateLimiter were registered first, it would always observe the
// zero-value GateStatusUnknown (CloudflareOnly hasn't run yet at that point
// in the chain) and fall back to the request's RemoteAddr for every request —
// which, in this app's Fly.io deployment, is the Fly proxy's fixed internal
// address for effectively every inbound request, not the real end client.
// That collapses per-client rate limiting into one shared bucket, letting one
// client exhaust the budget for everyone else.
//
// See issue1514_rate_limit_ordering_test.go for a behavioural test that
// builds this exact chain via httptest and demonstrates the failure mode
// above when the two calls are (deliberately, temporarily, for that test's
// RED demonstration only) reordered.
// newRedisClientOptions builds the redisclient.Options for a Redis client named `name` (either
// cache.LimiterClientName or "cache") from cfg. Extracted from main() (Issue #1531 Blocker 4) so
// the actual wiring of cfg.RedisTimeoutMs/RedisPoolSize into the constructed client is unit
// testable without a real Redis server for the URL/Name/timeout/pool-size fields (see
// issue1531_redis_config_wiring_test.go), and against a real client's rdb.Options() (see
// redisclient/client_test.go's TestNew_ExplicitOptionsOverrideDefaults) for the fields that matter
// once a real Redis instance is available. Before this helper existed, both call sites in main() passed only
// URL and Name — cfg.RedisTimeoutMs and cfg.RedisPoolSize were validated and documented ("read by
// redisclient.Options construction") but silently discarded, so every deploy ran on
// redisclient's hardcoded defaults regardless of what an operator configured.
func newRedisClientOptions(cfg *config.Config, url, name string) redisclient.Options {
	readWriteTimeout := time.Duration(cfg.RedisTimeoutMs) * time.Millisecond
	return redisclient.Options{
		URL:          url,
		Name:         name,
		ReadTimeout:  readWriteTimeout,
		WriteTimeout: readWriteTimeout,
		PoolSize:     cfg.RedisPoolSize,
	}
}

// redisStartupPingTimeout bounds buildRateLimitBackend's "redis" mode Ping — a STARTUP-only
// check, never on the per-request path.
const redisStartupPingTimeout = 5 * time.Second

// buildRateLimitBackend constructs the fleet-wide Redis-backed rate-limit primary from
// limiterClient/cfg, or returns nil (memory-only) when limiterClient is nil. Extracted from
// main() so its SentinelLimit choice — the subject of Issue #1531 fix round 1 — is unit-testable
// without a real Redis server.
//
// Issue #1531 Slice B / M2: cfg.RateLimitBackend (RATE_LIMIT_BACKEND) is a documented incident
// kill switch (docs/architecture/redis-topology.md, "Kill switches") that was validated at
// config.Load() time but never actually read here — this function used to branch solely on
// limiterClient == nil, so setting RATE_LIMIT_BACKEND=memory during an incident (e.g. a sick
// Upstash instance) had zero effect: every request still hit the Redis-backed primary. Now:
//   - "memory": force in-memory limiting even when limiterClient is non-nil (the incident lever).
//   - "redis": require a working limiterClient; if one isn't available, return an error so the
//     CALLER can fail fast at STARTUP — never per-request. This function itself must never fail a
//     request; it only runs once, during main()'s initialization, before any HTTP traffic is
//     accepted.
//   - "auto" (default) or any other/empty value (e.g. a *config.Config built directly by a test
//     without going through Load(), where the zero value is ""): preserves the original
//     nil-check-only behaviour.
//
// Issue #1531 GA-4: redisclient.New is lazy and never Pings, so a limiterClient being non-nil
// only proves the URL PARSED, not that Redis is reachable. "redis" mode exists so an operator can
// force Redis during an incident and KNOW it took effect — a syntactically valid but unreachable
// RATE_LIMIT_REDIS_URL booting cleanly would defeat that and contradict this function's own error
// text below ("missing/unreachable"). So "redis" mode additionally Pings, bounded by
// redisStartupPingTimeout, ONCE here at startup — never per-request; "auto" and "memory" do not
// Ping at all, since neither documents a reachability guarantee.
func buildRateLimitBackend(ctx context.Context, limiterClient redisclient.Client, cfg *config.Config) (middleware.RateLimitBackend, error) {
	switch cfg.RateLimitBackend {
	case "memory":
		return nil, nil
	case "redis":
		if limiterClient == nil {
			return nil, fmt.Errorf(
				"RATE_LIMIT_BACKEND=redis requires a working Redis limiter client, but none is available " +
					"(RATE_LIMIT_REDIS_URL missing/unreachable) — refusing to start rather than silently " +
					"falling back to memory-only rate limiting")
		}
		pingCtx, cancel := context.WithTimeout(ctx, redisStartupPingTimeout)
		defer cancel()
		if err := limiterClient.Ping(pingCtx); err != nil {
			return nil, fmt.Errorf(
				"RATE_LIMIT_BACKEND=redis requires a working Redis limiter client, but it failed to "+
					"respond to a startup Ping (RATE_LIMIT_REDIS_URL missing/unreachable): %w — refusing "+
					"to start rather than silently falling back to memory-only rate limiting", err)
		}
	}

	if limiterClient == nil {
		return nil, nil
	}
	return ratelimitstore.NewRedisBackend(limiterClient, ratelimitstore.Options{
		RegularLimit: cfg.RateLimitIP,
		AuthLimit:    cfg.RateLimitAuth,
		// SentinelLimit is now wired explicitly from cfg.RateLimitSentinelLimit
		// (RATE_LIMIT_SENTINEL_LIMIT, Task 8) so an operator can raise the sentinel bucket's
		// limit during a live incident without a deploy. Zero (the default) still means "use
		// RegularLimit, not AuthLimit" — Options.withDefaults treats any <=0 SentinelLimit as
		// "fall back to RegularLimit" (see TestRedisBackend_SentinelLimitDefaultsToRegularLimitWhenZero),
		// so passing an explicit zero here preserves that fix-round-1 behaviour exactly; it is NOT
		// a regression to "wire the field explicitly" as fix round 1's comment anticipated. The
		// sentinel bucket ("rl:v2:sentinel") is used whenever a Cloudflare-gated request's
		// Cf-Connecting-Ip is missing, duplicated, or malformed — and under Redis it is ONE
		// fleet-wide key, not per-machine. A Cloudflare header anomaly (Managed Transform change,
		// config drift, a CF incident) collapses ALL affected traffic into that single bucket:
		// capping it at the auth limit would mean the entire fleet falls over to ~10 req/min in
		// precisely the incident where the API most needs to keep serving. RegularLimit (the
		// zero-value default) is the correct floor; RATE_LIMIT_SENTINEL_LIMIT lets it be raised
		// higher still without a deploy.
		SentinelLimit: cfg.RateLimitSentinelLimit,
		Window:        time.Minute,
	}), nil
}

// precheckSentinelCeiling resolves the sentinel tier's AUTHORITATIVE limit the same way
// ratelimitstore.Options.withDefaults does, so the cost pre-check's sentinel ceiling can never sit
// below the budget Redis actually enforces.
//
// RATE_LIMIT_SENTINEL_LIMIT is an operator escape hatch for RAISING the sentinel bucket during a
// live Cloudflare header anomaly — the incident where every request collapses onto one shared
// identity. Deriving the pre-check ceiling from RateLimitIP instead silently capped that knob, at
// maximum blast radius, during the precise outage it was built for.
func precheckSentinelCeiling(cfg *config.Config) int {
	if cfg.RateLimitSentinelLimit > 0 {
		return cfg.RateLimitSentinelLimit
	}
	return cfg.RateLimitIP
}

func registerCloudflareAndRateLimitMiddleware(e *echo.Echo, cfCIDRRef *atomic.Pointer[[]netip.Prefix], cfg *config.Config) {
	// Delegates to the Task 7 variant with a nil backend/done/logger — RateLimitConfig treats all
	// three as OPTIONAL (nil Backend: memory-only; nil Done: cleanup goroutine never stops; nil
	// Logger: slog.Default()), so this reproduces this function's exact pre-#1531 behaviour byte
	// for byte. issue1514_rate_limit_ordering_test.go calls this 3-arg function directly and is
	// pinned unmodified — do not add parameters to IT; extend
	// registerCloudflareAndRateLimitMiddlewareWithBackend instead.
	registerCloudflareAndRateLimitMiddlewareWithBackend(e, cfCIDRRef, cfg, nil, nil, nil)
}

// registerCloudflareAndRateLimitMiddlewareWithBackend is registerCloudflareAndRateLimitMiddleware
// plus the Issue #1531 Task 5/7 wiring: an optional fleet-wide RateLimitBackend, an optional Done
// channel that stops the rate limiter's background goroutines on shutdown, and an optional
// *slog.Logger for fallback-activation/recovery logging. The Cloudflare-before-rate-limiter
// ordering — the entire reason this helper exists rather than two adjacent e.Use(...) calls at
// the call site — is identical to the 3-arg function above; see its doc comment for why that
// order is load-bearing.
func registerCloudflareAndRateLimitMiddlewareWithBackend(e *echo.Echo, cfCIDRRef *atomic.Pointer[[]netip.Prefix], cfg *config.Config, backend middleware.RateLimitBackend, done <-chan struct{}, logger *slog.Logger) {
	e.Use(middleware.CloudflareOnly(cfCIDRRef, cfg.CFOriginSecret, cfg.Environment)) // Cloudflare-only enforcement: blocks direct *.fly.dev access
	e.Use(middleware.IPRateLimiter(middleware.RateLimitConfig{                       // Per-IP rate limiting
		// IPLimit/AuthEndpointLimit here feed ONLY the in-memory fallback store (memoryBackend) —
		// the Redis-backed primary gets its own RegularLimit/AuthLimit via buildRateLimitBackend
		// above, unaffected by this division. cfg.RateLimitFallbackDivisor (Task 8) divides these
		// so N machines independently falling back to memory-only limiting do not multiply the
		// intended FLEET-WIDE budget by N — see rateLimitFallbackLimit's doc comment.
		IPLimit:           rateLimitFallbackLimit(cfg.RateLimitIP, cfg.RateLimitFallbackDivisor),
		AuthEndpointLimit: rateLimitFallbackLimit(cfg.RateLimitAuth, cfg.RateLimitFallbackDivisor),
		// ExemptPathLimit (Issue #1531 L3) is deliberately the RAW, UNDIVIDED cfg.RateLimitIP —
		// /health is the only exempt path, it never uses the Redis-backed primary at all (by
		// design), and its budget was never fleet-wide to begin with (it's Fly's own
		// single-machine liveness check), so the fleet-machine-count divisor above must not apply
		// to it. See RateLimitConfig.ExemptPathLimit's doc comment.
		ExemptPathLimit: cfg.RateLimitIP,
		// The COST circuit breaker (2026-09-08 review). Like ExemptPathLimit these are the RAW,
		// UNDIVIDED limits — deliberately NOT rateLimitFallbackLimit'd. The divisor above exists
		// so N machines falling back to memory do not multiply the fleet budget by N; that
		// reasoning is inverted here. These buckets must sit at or ABOVE the authoritative limit,
		// because they run BEFORE the Redis backend and must never reject traffic it would have
		// allowed. Dividing them would do exactly that.
		//
		// ⚠ CORRECTED. This block previously passed ONE ceiling and justified it with
		// "cfg.RateLimitIP (the regular tier) is >= cfg.RateLimitAuth in every shipped config, so
		// one ceiling safely covers both tiers." That sentence is wrong twice over, and two
		// independent adversarial reviewers each executed a reproduction:
		//
		//   - There are THREE tiers, not two. It never mentioned the sentinel tier — the one with
		//     an operator-settable limit (RATE_LIMIT_SENTINEL_LIMIT) that is DOCUMENTED as
		//     intended to exceed RateLimitIP during a live Cloudflare header anomaly. A single
		//     RateLimitIP ceiling silently neutered that escape hatch during the exact incident it
		//     exists for, with every customer sharing the sentinel key.
		//   - Per-tier ceilings are not sufficient either: one SHARED bucket charged an identity's
		//     regular traffic against its auth allowance, so 100 ordinary API calls followed by a
		//     single legitimate POST /webhooks/plaid returned 429 — on the traffic that must never
		//     be dropped.
		//
		// So each tier now carries its own ceiling, taken from that tier's own authoritative
		// limit. RateLimitConfig applies precheckHeadroomFactor on top; see that constant for why
		// parity would be fragile rather than tight.
		//
		// The "100 >= 10" claim was also unguarded: both are plain env vars, so
		// `fly secrets set RATE_LIMIT_AUTH=500` would have broken the invariant with no test
		// failing. Wiring each tier from its own limit removes the dependency on that ordering
		// entirely rather than asserting it.
		//
		// Why any of this exists: the Redis backend is billed per command and is consulted on
		// every request, INCLUDING rejected ones. /webhooks/ carries no Cloudflare rate-limit rule
		// (by design — throttling it would drop real Plaid/Paddle deliveries), so a POST flood
		// there cost one billable command per request without bound. This caps it per identity.
		PrecheckLimit:     cfg.RateLimitIP,
		PrecheckAuthLimit: cfg.RateLimitAuth,
		// Mirrors ratelimitstore.Options.withDefaults: a <=0 RATE_LIMIT_SENTINEL_LIMIT means "use
		// RegularLimit" there, so the pre-check ceiling has to resolve the same way or it would
		// under-cover the sentinel tier by exactly the amount of that default.
		PrecheckSentinelLimit: precheckSentinelCeiling(cfg),
		// PrimaryTimeout (Issue #1531 L7): cfg.RedisTimeoutMs already reaches the go-redis
		// CLIENT's own read/write timeouts (see newRedisClientOptions) but, before this fix, had
		// no path to the LIMITER's per-call budget — that was a hardcoded, uncustomizable 50ms
		// (middleware.defaultRedisLimiterTimeout). Raising REDIS_TIMEOUT_MS during a real latency
		// incident changed nothing about the limiter's own cutoff, which kept tripping the
		// circuit breaker regardless. Wiring the same config value through here makes
		// REDIS_TIMEOUT_MS the operative knob it was already documented (config.go) as being.
		PrimaryTimeout: time.Duration(cfg.RedisTimeoutMs) * time.Millisecond,
		WindowDuration: time.Minute,
		MaxEntries:     cfg.RateLimitMaxEntries,
		Backend:        backend,
		Done:           done,
		Logger:         logger,
	}))
}

// rateLimitFallbackLimit divides limit by divisor for the in-memory rate-limit fallback (see
// registerCloudflareAndRateLimitMiddlewareWithBackend). Issue #1531 Task 8: when the fleet-wide
// Redis-backed primary is unavailable, every machine falls back independently to its own
// in-memory limiter — N machines each enforcing the FULL fleet-wide limit multiplies the intended
// budget by N. Dividing the per-machine fallback limit by (an operator-tunable estimate of) fleet
// size keeps the aggregate closer to the intended budget during an outage.
//
// divisor <= 0 is treated as 1 (no division — today's behaviour, and the config default), rather
// than panicking or dividing by zero.
//
// limit <= 0 passes through UNCHANGED — deliberately NOT floored to 1 here (fix round 1).
// middleware.RateLimitConfig.withDefaults() is the ONE place that owns the "IPLimit/
// AuthEndpointLimit <= 0 means fall back to the built-in default (100/10)" contract. A
// misconfigured RATE_LIMIT_IP=0 or RATE_LIMIT_AUTH=0 must keep self-healing through that single
// defaulting path. Flooring a non-positive limit to 1 here — this function's first version did —
// pre-empts withDefaults() and turns a self-correcting misconfiguration (silently falls back to
// 100 req/min) into a near-total lockout (1 req/min): a real behaviour regression, caught in fix
// round 1, not a pre-existing gap. Only a genuinely positive limit is divided and floored at 1
// (so a divisor larger than the limit degrades to "1 request per window", never to "0 requests" —
// which would silently disable the fallback limiter, letting every request through unenforced).
func rateLimitFallbackLimit(limit, divisor int) int {
	if limit <= 0 {
		return limit
	}
	if divisor <= 0 {
		divisor = 1
	}
	result := limit / divisor
	if result < 1 {
		result = 1
	}
	return result
}

// redisPoolStatsAdapter adapts redisclient.Client to observability.RedisPoolStatsSource — a
// LOCAL mirror interface (see that type's doc comment) so internal/observability never imports
// internal/redisclient. Reads local pool state only; dispatches no Redis command.
type redisPoolStatsAdapter struct {
	client redisclient.Client
}

func (a redisPoolStatsAdapter) Name() string { return a.client.Name() }

func (a redisPoolStatsAdapter) RedisPoolStats() observability.RedisPoolStats {
	s := a.client.PoolStats()
	return observability.RedisPoolStats{
		Hits:       s.Hits,
		Misses:     s.Misses,
		Timeouts:   s.Timeouts,
		TotalConns: s.TotalConns,
		IdleConns:  s.IdleConns,
		StaleConns: s.StaleConns,
		PoolSize:   s.PoolSize,
	}
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

// startJobHealthMetricsCollector runs a goroutine that periodically refreshes
// observability.ManualTierOverrideOldestUnclearedExpiredSeconds and, since Issue #1413's
// Plaid-cleanup-backlog follow-up, observability.PlaidCleanupOutstandingUsers/
// PlaidCleanupOldestOutstandingSeconds and, since Issue #1504, observability.DeletionResidueRecords/
// DeletionResidueNewestSeconds from the database. Lives here — not in the observability
// package alongside observability.StartDBMetricsCollector, which it otherwise mirrors — because it
// needs repository.JobHealthRepository, and internal/repository already imports
// internal/observability; putting this collector there would create an import cycle. A failed
// query is logged and skipped for THAT metric only (not fatal, and not allowed to block the
// other): a transient DB hiccup here must not crash the whole process, and each gauge simply keeps
// its last-known value until its next tick succeeds. The goroutine stops when ctx is cancelled.
//
// ⚠ IT READS ONCE BEFORE THE TICKER STARTS, and that is not a latency optimisation. Every gauge
// here is now an observability.UnmeasuredGauge — absent from the scrape until this function
// publishes it — precisely so that "nobody has looked yet" and "measured, and it is zero" are
// different scrapes. Before both halves of that change, these were plain Gauges exporting 0 from
// process start and untouched for the first five minutes, so each process asserted "no backlog, no
// residue, nothing expired" with nothing having queried anything. For
// observability.DeletionResidueRecords that was worse than a stale number: its prescribed rule is
// `increase(deletion_residue_records[15m]) > 0`, and on any process with pre-existing residue the
// 0 → N step five minutes into the process LOOKED LIKE N NEW INCIDENTS. A restart paged somebody
// about a GDPR failure that had not happened.
//
// ⚠ AND A FAILED READ — startup or tick — STILL PUBLISHES NOTHING. There is no "publish 0 and log
// it" branch anywhere below, on purpose: a zero produced by a failed query is the same false claim
// as a zero produced by never having queried, just later. On the first pass that leaves the series
// absent (the absent()/staleness rules documented at each gauge's declaration are what report it);
// on a later pass it leaves the last real reading in place, which staleness likewise reports.
func startJobHealthMetricsCollector(ctx context.Context, repo repository.JobHealthRepository, logger *slog.Logger) {
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		// The initial measurement, before the loop: the first sample any scrape can see is therefore a
		// reading rather than a default, and the unmeasured window is one query long instead of one
		// tick long.
		refreshJobHealthMetrics(ctx, repo, logger)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				refreshJobHealthMetrics(ctx, repo, logger)
			}
		}
	}()
}

// refreshJobHealthMetrics performs one pass over every job-health gauge. Extracted from
// startJobHealthMetricsCollector's loop so the startup read and the periodic read are literally the
// same code — two copies of this would be two chances for the startup pass to drift into publishing
// something the tick does not, which is the class of bug this whole change is about.
//
// Per-metric error isolation throughout: one failing query must not suppress the others, and none of
// them is fatal.
func refreshJobHealthMetrics(ctx context.Context, repo repository.JobHealthRepository, logger *slog.Logger) {
	age, err := repo.GetOldestUnclearedExpiredManualTierOverrideAge(ctx)
	if err != nil {
		logger.Warn("failed to refresh manual tier override job-health gauge", "error", err)
	} else {
		observability.ManualTierOverrideOldestUnclearedExpiredSeconds.WithLabelValues().Set(age.Seconds())
	}

	plaidCount, plaidOldestAge, err := repo.GetOutstandingPlaidCleanupStats(ctx)
	if err != nil {
		logger.Warn("failed to refresh Plaid-cleanup job-health gauges", "error", err)
	} else {
		observability.PlaidCleanupOutstandingUsers.WithLabelValues().Set(float64(plaidCount))
		observability.PlaidCleanupOldestOutstandingSeconds.WithLabelValues().Set(plaidOldestAge.Seconds())
	}

	// Issue #1504: the deletion-residue pair. Same per-metric error isolation as the two above — a
	// failure here logs and leaves both gauges as they were (absent before the first success, at their
	// last-known value after it) rather than zeroing them, because a zeroed monotonic gauge would read
	// as "the residue went away", which is the one thing it can never mean.
	residueCount, residueNewestAge, err := repo.GetDeletionResidueStats(ctx)
	if err != nil {
		logger.Warn("failed to refresh deletion-residue gauges", "error", err)
	} else {
		observability.DeletionResidueRecords.WithLabelValues().Set(float64(residueCount))
		observability.DeletionResidueNewestSeconds.WithLabelValues().Set(residueNewestAge.Seconds())
	}
}

func boolStatus(configured bool) string {
	if configured {
		return "configured"
	}
	return "not configured"
}

// parseCacheNamespacesDisabled parses config.CacheNamespacesDisabled (comma-separated namespace
// names, e.g. "analytics,dashprefs") into the map[cache.Namespace]bool shape cache.Options.
// EnvDisabledNamespaces expects. Unknown/blank tokens are ignored — an unrecognised namespace has
// no registry entry and cache.Cache.Enabled already reports it disabled unconditionally, so a typo
// here fails safe (extra caching never turns on) rather than panicking at boot.
func parseCacheNamespacesDisabled(raw string) map[cache.Namespace]bool {
	out := make(map[cache.Namespace]bool)
	for _, tok := range strings.Split(raw, ",") {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		out[cache.Namespace(tok)] = true
	}
	return out
}

// isProductionOrStagingEnvironment reports whether env is one of the two environments where a
// Redis limiter-client construction FAILURE (as opposed to ErrNotConfigured) should be fatal
// (Issue #1531 Task 7c). Deliberately NOT an inline `cfg.Environment == "production" ||
// cfg.Environment == "staging"` comparison at the call site: qa_auth_bypass_wiring_test.go's
// TestQAAuthBypassRegistrationUsesTheTestedGate scans main.go's source for the raw string
// `cfg.Environment == "staging"` anywhere in the file (not just near QA-bypass gating) — see that
// test's doc comment on why an inline environment comparison is the exact anti-pattern #1432
// B2-1 closed.
func isProductionOrStagingEnvironment(env string) bool {
	return env == "production" || env == "staging"
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
