// Package redisclient wraps go-redis/v9 with the timeout defaults, context-deadline
// enforcement, and circuit breaker that BoostedBudget's Redis-backed rate limiter (Task 4/5)
// and cache (Task 9) both depend on.
//
// # Timeout rationale
//
//   - ReadTimeout/WriteTimeout default to 200ms. The rate limiter runs at middleware position
//     4, ahead of everything else in the request path. Fly's http_checks timeout is 5s, so
//     200ms leaves a 25x margin on the liveness path.
//   - DialTimeout defaults to 1s, deliberately looser than read/write: production uses
//     rediss:// (TLS), so a cold dial pays a TLS handshake. A 150ms dial timeout would flap.
//     Cold dials stay rare because MinIdleConns defaults to 2, which keeps the pool warm.
//   - MaxRetries defaults to 0. go-redis itself defaults to 3 retries with backoff, which
//     multiplies the worst case by roughly 4x on the request path. We have a strictly better
//     retry: the in-memory limiter fallback (Task 5). Retrying inside a 200ms budget is worse
//     than degrading immediately.
//   - PoolTimeout defaults to 250ms so pool exhaustion surfaces as a fast error (which trips
//     the fallback) rather than queuing the caller behind a wait.
//
// # ContextTimeoutEnabled
//
// go-redis does NOT honour context deadlines by default. Without ContextTimeoutEnabled: true, a
// command uses ReadTimeout/WriteTimeout regardless of the context deadline, and the 200ms budget
// above becomes fiction: a detached call could blow far past the breaker's expectations. New
// always sets it explicitly to true; it is not configurable.
//
// # The circuit breaker
//
// Without a breaker, a hard-down Redis costs DialTimeout (1s) on every request at middleware
// position 4. At hard_limit = 250 connections per machine that is a latency cliff, and on the
// probe path a real risk of tripping Fly's 5s health check, cycling machines, and causing a
// self-inflicted outage. The breaker turns a down Redis into a ~0us local decision instead.
//
// # Two clients, never share one
//
// New is one constructor, called twice by Task 7 (limiter + cache), each with its own Options.URL.
// On an empty URL it returns (nil, ErrNotConfigured) and the caller proceeds with a nil client.
//
// Never fall back to the limiter's URL when the cache URL is empty. The limiter instance is
// provisioned noeviction; a cache filling it produces "OOM command not allowed when used memory >
// 'maxmemory'" on the limiter's SET, which becomes a permanent limiter fallback with an
// unrelated-looking cause. Do not "simplify" the two constructions into one shared instance.
package redisclient

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sync"
	"time"

	redis "github.com/redis/go-redis/v9"
)

// ErrNotConfigured is returned by New when Options.URL is empty. Callers should treat this as
// "Redis is not configured for this environment" and proceed with a nil Client, not a fatal error.
var ErrNotConfigured = errors.New("redis: not configured")

// Options configures a Client returned by New. Zero-valued fields are replaced with the defaults
// documented on the package doc comment and on each field below.
type Options struct {
	// URL is a redis:// or rediss:// connection string, parsed with redis.ParseURL. Empty means
	// "not configured": New returns (nil, ErrNotConfigured).
	URL string

	// Name identifies this client instance ("limiter" | "cache"). Used as a metric label and log
	// field by later tasks; never used to alter connection behavior.
	Name string

	// DialTimeout for establishing new connections. Default: 1s.
	DialTimeout time.Duration
	// ReadTimeout for socket reads. Default: 200ms.
	ReadTimeout time.Duration
	// WriteTimeout for socket writes. Default: 200ms.
	WriteTimeout time.Duration
	// PoolTimeout is how long a caller waits for a pooled connection before failing fast.
	// Default: 250ms.
	PoolTimeout time.Duration
	// PoolSize is the base number of socket connections. Default: 10.
	PoolSize int
	// MinIdleConns keeps this many connections warm so a cold TLS dial is rare. Default: 2.
	MinIdleConns int
	// MaxRetries is the maximum number of command retries. Default: 0 (see package doc comment
	// for why go-redis's own default of 3 is wrong for this codebase).
	MaxRetries int

	// Breaker configures the circuit breaker wrapping Ping and RunScript.
	Breaker BreakerOptions
}

func (o Options) withDefaults() Options {
	if o.DialTimeout <= 0 {
		o.DialTimeout = time.Second
	}
	if o.ReadTimeout <= 0 {
		o.ReadTimeout = 200 * time.Millisecond
	}
	if o.WriteTimeout <= 0 {
		o.WriteTimeout = 200 * time.Millisecond
	}
	if o.PoolTimeout <= 0 {
		o.PoolTimeout = 250 * time.Millisecond
	}
	if o.PoolSize <= 0 {
		o.PoolSize = 10
	}
	if o.MinIdleConns <= 0 {
		o.MinIdleConns = 2
	}
	// MaxRetries has no "unset" sentinel distinct from its legitimate default (0), so no
	// substitution happens here; the go-redis-specific -1 workaround lives in New.
	return o
}

// PoolStats mirrors the subset of go-redis's pool.Stats this package publishes. It exists so
// callers (and observability.RedisPoolStatsSource implementations built on top of Client) never
// need to import go-redis directly.
type PoolStats struct {
	Hits, Misses, Timeouts            uint32
	TotalConns, IdleConns, StaleConns uint32
	PoolSize                          int
}

// Client is the interface Tasks 4, 5, 7, and 9 depend on. It is an interface (rather than a
// concrete struct) so those tasks can inject fake.go's in-memory implementation in their own unit
// tests without a real Redis server.
type Client interface {
	// Name returns the client's configured Name ("limiter" | "cache").
	Name() string
	// RedactedAddr returns host:port only. It never returns a username or password, even if the
	// underlying URL contained credentials.
	RedactedAddr() string
	// Ping checks connectivity. It is gated by the circuit breaker: while open, it returns
	// ErrBreakerOpen immediately without contacting Redis.
	Ping(ctx context.Context) error
	// RunScript executes s against Redis (EVALSHA with EVAL fallback on NOSCRIPT), gated by the
	// circuit breaker.
	RunScript(ctx context.Context, s *redis.Script, keys []string, args ...any) (any, error)
	// PoolStats reports the local connection pool's state. It reads local pool state only and
	// dispatches no Redis command.
	PoolStats() PoolStats
	// Close releases the underlying connection pool. Safe to call more than once.
	Close() error
}

// client is the production Client implementation backed by a real *redis.Client.
type client struct {
	name         string
	redactedAddr string
	opts         Options // fully-resolved (post withDefaults), for introspection/tests
	rdb          *redis.Client
	breaker      *breaker

	closeOnce sync.Once
	closeErr  error
}

// New constructs a Client from opts. It returns (nil, ErrNotConfigured) when opts.URL is empty,
// so callers can construct a "not configured" client without treating it as fatal.
//
// Construction errors are always wrapped so the URL (and any credentials it contains) never
// reaches logs or Sentry: redis.ParseURL's own errors can echo the input verbatim, which for a
// URL means echoing the password.
func New(opts Options) (Client, error) {
	if opts.URL == "" {
		return nil, ErrNotConfigured
	}

	parsed, err := redis.ParseURL(opts.URL)
	if err != nil {
		return nil, fmt.Errorf("redis client %q: invalid URL (redacted): %w", opts.Name, sanitize(err))
	}

	resolved := opts.withDefaults()

	parsed.DialTimeout = resolved.DialTimeout
	parsed.ReadTimeout = resolved.ReadTimeout
	parsed.WriteTimeout = resolved.WriteTimeout
	parsed.PoolTimeout = resolved.PoolTimeout
	parsed.PoolSize = resolved.PoolSize
	parsed.MinIdleConns = resolved.MinIdleConns
	// go-redis treats its own Options.MaxRetries == 0 as "use the built-in default of 3", and -1
	// as "disable retries". Our public default of 0 means "no retries", so translate it here.
	if resolved.MaxRetries == 0 {
		parsed.MaxRetries = -1
	} else {
		parsed.MaxRetries = resolved.MaxRetries
	}
	// go-redis v9.22+ added a SEPARATE dial-level retry (default: 5 attempts, ~100ms backoff)
	// beneath the connection pool, independent of the command-level MaxRetries above. Left at its
	// default, a hard-down/blackholed address costs DialTimeout*5 (~5s) instead of the DialTimeout
	// (1s) documented above — the same "retrying inside a tight budget is worse than degrading"
	// problem MaxRetries: 0 exists to avoid, just one layer lower. Disable it for the same reason.
	parsed.DialerRetries = 1
	// MUST be true: see package doc comment. Not configurable — a caller who could disable this
	// would silently reintroduce the "detached call blows past the breaker budget" bug.
	parsed.ContextTimeoutEnabled = true

	rdb := redis.NewClient(parsed)

	return &client{
		name:         opts.Name,
		redactedAddr: parsed.Addr,
		opts:         resolved,
		rdb:          rdb,
		breaker:      newBreaker(resolved.Breaker),
	}, nil
}

func (c *client) Name() string { return c.name }

func (c *client) RedactedAddr() string { return c.redactedAddr }

func (c *client) Ping(ctx context.Context) error {
	return c.breaker.call(func() error {
		return c.rdb.Ping(ctx).Err()
	})
}

func (c *client) RunScript(ctx context.Context, s *redis.Script, keys []string, args ...any) (any, error) {
	var result any
	err := c.breaker.call(func() error {
		var innerErr error
		result, innerErr = s.Run(ctx, c.rdb, keys, args...).Result()
		return innerErr
	})
	return result, err
}

func (c *client) PoolStats() PoolStats {
	stats := mapPoolStats(c.rdb.PoolStats())
	stats.PoolSize = c.opts.PoolSize
	return stats
}

// Close releases the underlying connection pool. It is idempotent: go-redis's own Close is NOT
// safe to call twice (a second call returns "redis: client is closed"), so this wraps it in a
// sync.Once and caches the first call's result for every subsequent caller.
func (c *client) Close() error {
	c.closeOnce.Do(func() {
		c.closeErr = c.rdb.Close()
	})
	return c.closeErr
}

// mapPoolStats copies the fields this package cares about from go-redis's pool.Stats into our own
// PoolStats. Kept as a small pure function so a go-redis upgrade that silently drops or renames one
// of these fields fails a unit test instead of shipping a metrics regression.
func mapPoolStats(s *redis.PoolStats) PoolStats {
	return PoolStats{
		Hits:       s.Hits,
		Misses:     s.Misses,
		Timeouts:   s.Timeouts,
		TotalConns: s.TotalConns,
		IdleConns:  s.IdleConns,
		StaleConns: s.StaleConns,
	}
}

// credentialPattern matches the userinfo portion of a URL ("//user:pass@") so it can be redacted
// out of any error text that might otherwise echo it verbatim.
var credentialPattern = regexp.MustCompile(`://[^/@\s]*@`)

// sanitize strips any embedded URL credentials from err's message. redis.ParseURL delegates to
// net/url, whose ParseError includes the raw input string; for a rediss://user:pass@host URL that
// means the password would otherwise land in logs or Sentry via a wrapped error.
func sanitize(err error) error {
	return errors.New(credentialPattern.ReplaceAllString(err.Error(), "://[REDACTED]@"))
}
