package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"time"
)

// PaddleWebhookTimestampToleranceMaxSeconds is the upper bound accepted for
// PADDLE_WEBHOOK_TIMESTAMP_TOLERANCE_SECONDS (issue #1514 fix-round-1 item 1). Chosen as "a few
// minutes" per the reviewer's guidance, well above Paddle's documented 5-second SDK default (so
// legitimate deployment/retry jitter has generous headroom) but far short of the hours/days an
// operator typo (e.g. mistaking the field for a TTL and entering "999999999") would otherwise
// produce — a value that large would leave the signature's ts= check accepting almost anything,
// which is indistinguishable in practice from having no replay window at all on a payments path.
const PaddleWebhookTimestampToleranceMaxSeconds = 300

// InvalidWebhookAttemptRetentionMaxDays is the upper bound accepted for
// INVALID_WEBHOOK_ATTEMPT_RETENTION_DAYS (Issue #1514 G3). 365 days is a generous sanity ceiling
// against an operator typo (e.g. entering a number of hours or minutes by mistake) blowing the
// retention window out to years for what is meant to be a short-lived operational audit signal.
const InvalidWebhookAttemptRetentionMaxDays = 365

// RedisTimeoutMsMax is the upper bound accepted for REDIS_TIMEOUT_MS.
//
// #1531 slice H5 (reverses G3's choice, made two commits earlier in this branch): G3 set this to
// 10000 purely to stop time.Duration(cfg.RedisTimeoutMs) * time.Millisecond (main.go, both the
// Redis client and the rate limiter's PrimaryTimeout) from overflowing int64 and wrapping
// negative — which would silently reproduce the P4 divergence (each consumer's own <=0 check
// falling back to a DIFFERENT default) through an absurd-but-reachable input rather than a
// non-positive one. That closes the overflow, but 10000 (10s) is not a sane ceiling for what this
// value actually feeds: cfg.RedisTimeoutMs is ALSO wired into the rate limiter's PrimaryTimeout
// (main.go), which BLOCKS the request path at middleware position 4
// (rate_limit_fallback.go's Allow) before falling back. redisclient/client.go's own package doc
// says a 1s DialTimeout delay at that exact position is already "a latency cliff" — a 10s ceiling
// permitted ten times that, on every request, not just a cold dial. (This is a request-path
// latency argument only: /health itself never waits on Redis — it is served from its own
// dedicated memory limiter store, entirely bypassing the Redis-backed primary; see
// middleware/rate_limit.go's ExemptPathLimit doc comment. Fly's 5s health-check probe is
// therefore not at risk from this value either way.) 2000ms is far beyond any sane limiter or
// cache-client budget on the request path, so the overflow protection G3 added is unaffected.
const RedisTimeoutMsMax = 2000

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
	// RateLimitMaxEntries caps the number of distinct identities each rate-limit
	// store tracks (see middleware.RateLimitConfig.MaxEntries). Validated
	// positive at load time; a zero/invalid RATE_LIMIT_MAX_ENTRIES falls back to
	// the default rather than producing an uncapped store.
	RateLimitMaxEntries int

	// Database
	DatabaseURL      string
	DatabaseMaxConns int32

	// Redis
	//
	// Issue #1531 Task 8: RATE_LIMIT_REDIS_URL replaces the old generic REDIS_URL. The rename
	// exists so there is no generic "the Redis URL" left for a future change to point the cache
	// at — internal/redisclient's package doc comment explains why sharing one URL between the
	// noeviction-provisioned limiter instance and a cache is a real hazard (an evicting cache
	// write can OOM the limiter's SETs), producing a permanent limiter fallback with an
	// unrelated-looking cause. validate() below enforces RateLimitRedisURL != CacheRedisURL.
	//
	// The default is "" (was "redis://localhost:6379"). A production deploy that forgot the
	// secret used to silently dial localhost, trip the circuit breaker, run permanently on the
	// in-memory limiter, and fire the fallback alert on every boot — a correct alert with a
	// misleading cause (looks like a Redis outage, is actually a missing secret). An empty
	// default lets RATE_LIMIT_BACKEND=auto resolve honestly to memory, and validate() refuses to
	// boot production/staging without it.
	RateLimitRedisURL string // env: RATE_LIMIT_REDIS_URL, default: ""

	// CacheRedisURL (Task 9) is a SEPARATE Redis instance/URL from RateLimitRedisURL — never the
	// same value; see the warning above. Optional: an empty value means "no cache" (Task 9 must
	// treat cache-client construction failure as non-fatal).
	CacheRedisURL string // env: CACHE_REDIS_URL, default: ""

	// RedisTimeoutMs and RedisPoolSize are read by Task 9's redisclient.Options construction for
	// both the limiter and cache clients. RedisTimeoutMs is ALSO wired (Issue #1531 L7) into
	// middleware.RateLimitConfig.PrimaryTimeout — the fleet-wide rate limiter's own per-call
	// budget for the Redis-backed primary, previously a hardcoded, unconfigurable 50ms with no
	// path to this field at all. Both consumers must read the SAME value so raising
	// REDIS_TIMEOUT_MS during a latency incident actually changes the limiter's breaker behaviour,
	// not just the underlying client's read/write timeouts.
	//
	// Round-6 finding P4: loaded via getEnvAsPositiveInt, not getEnvAsInt — a non-positive value
	// (0 or negative) falls back to the 200ms default at Load() time, rather than reaching the two
	// consumers above as a literal 0/negative that each independently reinterprets:
	// redisclient.Options's own <=0 check upgrades it to ITS 200ms default, while
	// middleware.RateLimitConfig's own <=0 check falls back to defaultRedisLimiterTimeout (50ms) —
	// two different effective timeouts from the one setting this field's doc comment says both
	// consumers "must read the SAME value" from. Clamping here, once, keeps that promise for every
	// reachable value instead of relying on the two fallbacks to agree by coincidence.
	//
	// #1531 G3: getEnvAsPositiveInt alone still let an absurdly large value reach the same
	// divergence through a different door — time.Duration(cfg.RedisTimeoutMs) * time.Millisecond
	// (both consumers, main.go) overflows int64 and wraps negative for a value like
	// 9223372036855, and each consumer's own <=0 check then falls back to ITS OWN default exactly
	// as in the P4 case. Loaded via getEnvAsBoundedPositiveInt with an upper bound
	// (RedisTimeoutMsMax) that closes the overflow class; see that constant's own doc comment
	// (#1531 slice H5) for why the bound is also small enough to matter for the request path, not
	// just large enough to avoid overflow.
	RedisTimeoutMs int // env: REDIS_TIMEOUT_MS, default: 200
	RedisPoolSize  int // env: REDIS_POOL_SIZE, default: 10

	// RateLimitBackend selects the fleet-wide rate-limit primary: "auto" (Redis when configured,
	// memory otherwise), "memory" (force in-memory, per-machine), or "redis" (force Redis-backed).
	// Rejected at Load() time — unconditionally, not just in production/staging — if it is
	// anything other than these three values.
	RateLimitBackend string // env: RATE_LIMIT_BACKEND, default: "auto"

	// RateLimitSentinelLimit overrides the fleet-wide sentinel bucket's limit (see
	// buildRateLimitBackend in cmd/server/main.go). Zero means "use RateLimitIP" —
	// ratelimitstore.Options.withDefaults already treats <=0 as "fall back to RegularLimit"
	// (TestRedisBackend_SentinelLimitDefaultsToRegularLimitWhenZero); this field just lets an
	// operator raise the sentinel limit during a live incident without a deploy.
	RateLimitSentinelLimit int // env: RATE_LIMIT_SENTINEL_LIMIT, default: 0

	// RateLimitFallbackDivisor divides the per-machine in-memory fallback limiter's limits so
	// N machines independently falling back to memory-only limiting do not multiply the
	// intended FLEET-WIDE budget by N. Divisor 1 (default) is today's behaviour (no division).
	// Callers must guard against a zero/negative divisor and against a divisor that would floor
	// a limit to 0 (see rateLimitFallbackLimit in cmd/server/main.go).
	RateLimitFallbackDivisor int // env: RATE_LIMIT_FALLBACK_DIVISOR, default: 1

	// CacheEnabled/CacheNamespacesDisabled (Task 9): CacheEnabled is the top-level kill switch;
	// CacheNamespacesDisabled is a comma-separated list of cache namespaces to bypass
	// individually while leaving the rest of the cache enabled.
	CacheEnabled            bool   // env: CACHE_ENABLED, default: true
	CacheNamespacesDisabled string // env: CACHE_NAMESPACES_DISABLED, default: "" (comma-separated)

	// Clerk
	ClerkSecretKey      string
	ClerkPublishableKey string

	// QA Auth Bypass (QA/Staging only - never set in production)
	//
	// QAAuthBypassEnabled (QA_AUTH_BYPASS_ENABLED) is the explicit opt-in added by #1432
	// batch ED, finding B2-1: ENVIRONMENT equality alone used to decide whether the
	// unauthenticated, admin-token-minting bypass route existed. It is consulted only through
	// Config.QAAuthBypassRefusalReason — see qa_auth_bypass_gate.go for the full gate.
	QABypassSecret       string
	QAAdminClerkUserID   string
	QAPremiumClerkUserID string
	QAAuthBypassEnabled  bool

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
	// PaddleWebhookTimestampToleranceSeconds bounds how far a Paddle webhook's `ts=` signature
	// component may drift from this server's clock (in EITHER direction — too old or
	// unreasonably future-dated) before PaddleWebhookService.VerifyWebhook rejects it as a
	// replay/clock-skew risk (issue #1514, F3). Paddle's documented SDK default tolerance is 5
	// seconds — we mirror that as our default, but treat it as the vendor's stated behavior
	// rather than something this codebase has independently verified against live traffic.
	// Validated at load time to be in (0, PaddleWebhookTimestampToleranceMaxSeconds]; a value
	// outside that range (zero/negative, OR an operator typo like 999999999) falls back to the
	// default and logs a warning, rather than either disabling the replay window entirely or
	// silently accepting a value that would do so in practice (issue #1514 fix-round-1 item 1:
	// an unbounded-above tunable "looks configured" while completely defeating the control it
	// exists to enforce).
	PaddleWebhookTimestampToleranceSeconds int

	// Encryption
	EncryptionKey string

	// Email (SendGrid)
	SendGridAPIKey    string
	SendGridFromEmail string
	SendGridFromName  string

	// Deletion Configuration
	DeletionGracePeriodDays int
	DeletionTokenSecret     string

	// Retention Configuration (#930: 6-month read-only period before auto-deletion)
	RetentionPeriodMonths int // env: RETENTION_PERIOD_MONTHS, default: 6

	// DLQ Configuration (#633: Dead Letter Queue admin UI + cleanup)
	DLQRetentionDays int // env: DLQ_RETENTION_DAYS, default: 30

	// InvalidWebhookAttemptRetentionDays bounds how long a rejected (unsigned/invalid-signature)
	// webhook audit row (see repository.InvalidWebhookAttemptRepository) is eligible to be deleted
	// on an age basis — a bound orthogonal to the row-count cap in what it deletes on (elapsed
	// time, not row count), but NOT wall-clock-scheduled: the age-based DELETE runs only inside
	// trim(), and trim() only ever runs from Record(), which only ever runs in response to an
	// incoming rejected webhook (every invalidWebhookAttemptTrimInterval-th call per provider, or
	// that provider's first call after process startup). So reclamation is traffic-triggered, not
	// independent of call volume or restart timing: a provider that goes quiet — stops receiving
	// invalid attempts, with or without a restart — has no future Record call to trigger the
	// sweep, and its already-written rows persist past this window until (if ever) that provider
	// is rejected again. See invalidWebhookAttemptTrimInterval's doc comment for the full
	// mechanism. Default 30 days mirrors this project's existing DeletionGracePeriodDays
	// convention rather than inventing an unrelated number. Validated at load time via
	// getEnvAsBoundedPositiveInt; an out-of-range value (<=0 or >
	// InvalidWebhookAttemptRetentionMaxDays) falls back to the default and logs a warning.
	InvalidWebhookAttemptRetentionDays int // env: INVALID_WEBHOOK_ATTEMPT_RETENTION_DAYS, default: 30

	// Frontend URL (for cancellation links)
	FrontendURL string

	// Observability
	LogLevel                       string     // from LOG_LEVEL — "debug"/"info"/"warn"/"error", default "info"
	EnableMetrics                  bool       // from ENABLE_METRICS, default true
	SentryDSN                      string     // from SENTRY_DSN — empty = disabled
	OTELEndpoint                   string     // from OTEL_EXPORTER_OTLP_ENDPOINT — empty = disabled
	SentrySampleRate               float64    // from SENTRY_SAMPLE_RATE — default 1.0
	OTELSampleRate                 float64    // from OTEL_SAMPLE_RATE — default 1.0
	PostHogAPIKey                  string     // from POSTHOG_API_KEY — empty = disabled
	PostHogHost                    string     // from POSTHOG_HOST — default us.i.posthog.com
	PostHogSecretAPIToken          string     // from POSTHOG_SECRET_API_TOKEN — server-only Support identity HMAC key
	PostHogSyncCompletionCutoverAt *time.Time // from POSTHOG_SYNC_COMPLETION_CUTOVER_AT — unset suppresses first-sync telemetry

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
		RateLimitIP:         getEnvAsInt("RATE_LIMIT_IP", 100),
		RateLimitAuth:       getEnvAsInt("RATE_LIMIT_AUTH", 10),
		RateLimitMaxEntries: getEnvAsPositiveInt("RATE_LIMIT_MAX_ENTRIES", 20000),

		// Database
		DatabaseURL:      getEnv("DATABASE_URL", ""),
		DatabaseMaxConns: int32(getEnvAsInt("DATABASE_MAX_CONNS", 25)),

		// Redis (issue #1531 Task 8: RATE_LIMIT_REDIS_URL replaces REDIS_URL — see the field's
		// doc comment above for why the default is now "" rather than localhost)
		RateLimitRedisURL: getEnv("RATE_LIMIT_REDIS_URL", ""),
		CacheRedisURL:     getEnv("CACHE_REDIS_URL", ""),
		RedisTimeoutMs:    getEnvAsBoundedPositiveInt("REDIS_TIMEOUT_MS", 200, RedisTimeoutMsMax),
		RedisPoolSize:     getEnvAsInt("REDIS_POOL_SIZE", 10),

		// Rate-limit backend selection and tuning (issue #1531 Task 8)
		RateLimitBackend:         getEnv("RATE_LIMIT_BACKEND", "auto"),
		RateLimitSentinelLimit:   getEnvAsInt("RATE_LIMIT_SENTINEL_LIMIT", 0),
		RateLimitFallbackDivisor: getEnvAsInt("RATE_LIMIT_FALLBACK_DIVISOR", 1),

		// Cache (issue #1531 Task 8 fields; Task 9 wires these into behaviour)
		CacheEnabled:            getEnvBool("CACHE_ENABLED", true),
		CacheNamespacesDisabled: getEnv("CACHE_NAMESPACES_DISABLED", ""),

		// Clerk
		ClerkSecretKey:      getEnv("CLERK_SECRET_KEY", ""),
		ClerkPublishableKey: getEnv("CLERK_PUBLISHABLE_KEY", ""),

		// QA Auth Bypass
		QABypassSecret:       getEnv("QA_BYPASS_SECRET", ""),
		QAAdminClerkUserID:   getEnv("QA_ADMIN_CLERK_USER_ID", ""),
		QAPremiumClerkUserID: getEnv("QA_PREMIUM_CLERK_USER_ID", ""),
		QAAuthBypassEnabled:  getEnvBool("QA_AUTH_BYPASS_ENABLED", false),

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
		PaddleWebhookTimestampToleranceSeconds: getEnvAsBoundedPositiveInt(
			"PADDLE_WEBHOOK_TIMESTAMP_TOLERANCE_SECONDS", 5, PaddleWebhookTimestampToleranceMaxSeconds,
		),

		// Encryption
		EncryptionKey: getEnv("ENCRYPTION_KEY", ""),

		// Email (SendGrid)
		SendGridAPIKey:    getEnv("SENDGRID_API_KEY", ""),
		SendGridFromEmail: getEnv("SENDGRID_FROM_EMAIL", "noreply@boostedbudget.com"),
		SendGridFromName:  getEnv("SENDGRID_FROM_NAME", "BoostedBudget"),

		// Deletion Configuration
		DeletionGracePeriodDays: getEnvAsInt("DELETION_GRACE_PERIOD_DAYS", 30),
		DeletionTokenSecret:     getEnv("DELETION_TOKEN_SECRET", ""),

		// Retention Configuration (#930)
		RetentionPeriodMonths: getEnvAsInt("RETENTION_PERIOD_MONTHS", 6),

		// DLQ Configuration (#633)
		DLQRetentionDays: getEnvAsInt("DLQ_RETENTION_DAYS", 30),

		InvalidWebhookAttemptRetentionDays: getEnvAsBoundedPositiveInt(
			"INVALID_WEBHOOK_ATTEMPT_RETENTION_DAYS", 30, InvalidWebhookAttemptRetentionMaxDays,
		),

		// Frontend URL
		FrontendURL: getEnv("FRONTEND_URL", "http://localhost:5173"),

		// Observability
		LogLevel:              getEnv("LOG_LEVEL", "info"),
		EnableMetrics:         getEnvBool("ENABLE_METRICS", true),
		SentryDSN:             getEnv("SENTRY_DSN", ""),
		OTELEndpoint:          getEnv("OTEL_EXPORTER_OTLP_ENDPOINT", ""),
		SentrySampleRate:      getEnvFloat64("SENTRY_SAMPLE_RATE", 1.0),
		OTELSampleRate:        getEnvFloat64("OTEL_SAMPLE_RATE", 1.0),
		PostHogAPIKey:         getEnv("POSTHOG_API_KEY", ""),
		PostHogHost:           getEnv("POSTHOG_HOST", "https://us.i.posthog.com"),
		PostHogSecretAPIToken: getEnv("POSTHOG_SECRET_API_TOKEN", ""),

		// Paddle Reconciliation
		PaddleReconciliationEnabled: getEnvBool("PADDLE_RECONCILIATION_ENABLED", true),
		PaddleReconciliationAutoFix: getEnvBool("PADDLE_RECONCILIATION_AUTO_FIX", false),

		// Cloudflare Origin Secret
		CFOriginSecret: getEnv("CF_ORIGIN_SECRET", ""),

		// MCP Worker Secret (fail-closed when empty — IsMCPOrigin returns false)
		MCPWorkerSecret: getEnv("MCP_WORKER_SECRET", ""),
	}

	if rawCutover := os.Getenv("POSTHOG_SYNC_COMPLETION_CUTOVER_AT"); rawCutover != "" {
		cutover, err := time.Parse(time.RFC3339, rawCutover)
		if err != nil {
			return nil, fmt.Errorf("POSTHOG_SYNC_COMPLETION_CUTOVER_AT must be RFC3339: %w", err)
		}
		cfg.PostHogSyncCompletionCutoverAt = &cutover
	}

	// RATE_LIMIT_BACKEND is validated unconditionally — in every environment, not just
	// production/staging — because an unrecognised value is a config typo the process should
	// never silently misinterpret as "auto", regardless of where it's running.
	switch cfg.RateLimitBackend {
	case "auto", "memory", "redis":
	default:
		return nil, fmt.Errorf(
			"RATE_LIMIT_BACKEND must be one of \"auto\", \"memory\", or \"redis\" (got %q)",
			cfg.RateLimitBackend,
		)
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

	// Rate-limit Redis URL (issue #1531 Task 8): required non-empty in production/staging — see
	// RateLimitRedisURL's doc comment for why the old localhost default was a hazard. Error
	// messages below deliberately never interpolate the URL value itself (it carries a
	// password) — refer to the env var by name only.
	if c.RateLimitRedisURL == "" {
		errs = append(errs, fmt.Errorf("RATE_LIMIT_REDIS_URL is required"))
	} else if err := validateRedisURLScheme("RATE_LIMIT_REDIS_URL", c.RateLimitRedisURL); err != nil {
		errs = append(errs, err)
	}

	// Cache Redis URL is optional, but if set it must (a) parse as a redis/rediss URL and (b) be
	// a DIFFERENT instance than RateLimitRedisURL. Sharing one URL between the two would point
	// the cache at a noeviction-provisioned limiter instance — see internal/redisclient's package
	// doc comment: an evicting cache write there produces "OOM command not allowed" on the
	// limiter's SET, a permanent limiter fallback with an unrelated-looking cause.
	if c.CacheRedisURL != "" {
		if err := validateRedisURLScheme("CACHE_REDIS_URL", c.CacheRedisURL); err != nil {
			errs = append(errs, err)
		}
		if c.CacheRedisURL == c.RateLimitRedisURL {
			errs = append(errs, fmt.Errorf(
				"CACHE_REDIS_URL must not be the same value as RATE_LIMIT_REDIS_URL — they must "+
					"point at two separate Redis instances (a shared noeviction-provisioned limiter "+
					"instance will OOM under cache writes; see internal/redisclient's package doc)",
			))
		}
	}

	return errors.Join(errs...)
}

// validateRedisURLScheme checks that value parses as a URL with scheme "redis" or "rediss".
// It never includes value (the URL, which may carry a password) in its returned error — only
// fieldName, the env var name — so a validation failure can be logged/surfaced safely.
//
// This uses only net/url, deliberately: internal/config is a leaf package and must not import
// go-redis. The real redis.ParseURL runs in internal/redisclient.New, and cmd/server/main.go
// already fails fast on that error one line after constructing the client.
func validateRedisURLScheme(fieldName, value string) error {
	parsed, err := url.Parse(value)
	if err != nil {
		return fmt.Errorf("%s must be a valid URL", fieldName)
	}
	if parsed.Scheme != "redis" && parsed.Scheme != "rediss" {
		return fmt.Errorf("%s must use the redis:// or rediss:// scheme", fieldName)
	}
	return nil
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

// getEnvAsPositiveInt is like getEnvAsInt but rejects zero/negative values,
// falling back to defaultValue instead. Used for config knobs (like
// RateLimitMaxEntries) where a zero or negative value would silently disable a
// safety cap rather than express "unset".
func getEnvAsPositiveInt(key string, defaultValue int) int {
	v := getEnvAsInt(key, defaultValue)
	if v <= 0 {
		return defaultValue
	}
	return v
}

// getEnvAsBoundedPositiveInt is like getEnvAsPositiveInt but also rejects values above max,
// falling back to defaultValue (and logging a warning) in EITHER out-of-range direction. Added
// additively for issue #1514 fix-round-1 item 1 — it does NOT replace or get wired into
// getEnvAsPositiveInt or its existing callers (e.g. RateLimitMaxEntries), which are owned by
// other in-flight work on this branch and are left untouched.
//
// defaultValue is expected to itself be in (0, max] — getEnvAsInt already falls back to
// defaultValue for an unset or non-numeric env var, so that path never logs a spurious warning
// here; only an explicitly out-of-range NUMERIC value (<=0 or >max) does.
func getEnvAsBoundedPositiveInt(key string, defaultValue, max int) int {
	v := getEnvAsInt(key, defaultValue)
	if v <= 0 || v > max {
		slog.Default().Warn("config value out of range, falling back to default",
			"key", key,
			"value", v,
			"default", defaultValue,
			"max", max,
		)
		return defaultValue
	}
	return v
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
