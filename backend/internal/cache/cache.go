package cache

import (
	"context"
	"errors"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/maxnussbaum/BoostedBudget/internal/redisclient"
)

// Cache is the ONLY read entry point for this package: there is deliberately no bare Get/Set — a
// bare Get invites a caller that forgets to consult the generation token, which is exactly how
// stale financial data would leak back in. The returned error is ALWAYS the loader's; every Redis
// or codec error along the way is swallowed and counted, never surfaced to the caller.
type Cache interface {
	// GetOrLoad returns k's cached value if a valid, current, fresh entry exists; otherwise it
	// calls loader, caches the result (best-effort), and returns it. The bool reports whether the
	// value came from the cache (true) or the loader (false) — purely informational, callers
	// should not branch application logic on it.
	GetOrLoad(ctx context.Context, k Key, loader func(context.Context) ([]byte, error)) ([]byte, bool, error)
	// Generations exposes the per-scope generation-token manager, e.g. for Task 10's invalidation
	// hooks to call Bump. Returns nil for a disabled/noop cache.
	Generations() *Generations
	// Enabled reports whether ns is currently enabled, per the resolution order documented on
	// Control: hardDisabled constant -> Redis control key -> env var -> compiled default.
	Enabled(ns Namespace) bool
}

// Options configures a redis-backed Cache.
type Options struct {
	// Client is the cache's OWN eviction-enabled Redis client (produced by
	// redisclient.New(Options{Name: "cache", ...})) — never the limiter's no-eviction instance.
	Client redisclient.Client
	// Control, if non-nil, supplies Redis-hosted per-namespace kill-switch state. It MUST be
	// constructed against the LIMITER's no-eviction Redis instance (see control.go's doc comment).
	// Nil is valid — every namespace's Control.Enabled then reports "never observed" and
	// resolution falls through to EnvDisabledNamespaces / the compiled default.
	Control *Control
	// EnvDisabledNamespaces is the parsed form of config.CacheNamespacesDisabled (Task 8):
	// namespaces present here (and true) are disabled unless a Redis control key overrides them.
	EnvDisabledNamespaces map[Namespace]bool
	// Now is a clock seam for tests. Nil means time.Now.
	Now func() time.Time
}

// redisCache is the production Cache implementation over redisclient.Client.
type redisCache struct {
	client      redisclient.Client
	control     *Control
	envDisabled map[Namespace]bool
	now         func() time.Time
	gens        *Generations
	sf          singleflight.Group
}

// sfResult is what one de-duplicated singleflight call returns: the payload bytes and whether they
// came from the cache (hit) or the loader (miss/bypass).
type sfResult struct {
	data []byte
	hit  bool
}

// singleflightLoadTimeout bounds the detached, shared singleflight execution in GetOrLoad —
// see the Issue #1531 M4 comment at that call site.
const singleflightLoadTimeout = 30 * time.Second

// NewCache returns a production Cache over opts.Client.
func NewCache(opts Options) Cache {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	return &redisCache{
		client:      opts.Client,
		control:     opts.Control,
		envDisabled: opts.EnvDisabledNamespaces,
		now:         now,
		gens:        NewGenerations(opts.Client),
	}
}

func (c *redisCache) Generations() *Generations { return c.gens }

// Enabled resolves ns's enablement in order: hardDisabled constant (always wins) -> Redis control
// key (if ever observed) -> env var -> compiled default. An unregistered namespace is always
// disabled — there is no config for it to read.
func (c *redisCache) Enabled(ns Namespace) bool {
	cfg, ok := lookupNamespace(ns)
	if !ok {
		return false
	}
	if cfg.HardDisabled {
		return false
	}
	if c.control != nil {
		if enabled, observed := c.control.Enabled(ns); observed {
			return enabled
		}
	}
	if c.envDisabled != nil && c.envDisabled[ns] {
		return false
	}
	return cfg.DefaultEnabled
}

func (c *redisCache) GetOrLoad(
	ctx context.Context, k Key, loader func(context.Context) ([]byte, error),
) ([]byte, bool, error) {
	ns := k.Namespace
	cfg, ok := lookupNamespace(ns)
	if !ok || !c.Enabled(ns) {
		cacheRequestsTotal.WithLabelValues(string(ns), resultBypassDisabled).Inc()
		v, err := loader(ctx)
		return v, false, err
	}

	gen, err := c.gens.Get(ctx, k.ScopeKind, k.ScopeID)
	if err != nil {
		// Generation READ error: bypass the cache entirely. Do not read, do not write — serving
		// data whose generation could not be confirmed is exactly the stale-money failure this
		// package exists to prevent.
		cacheRequestsTotal.WithLabelValues(string(ns), resultBypassGenError).Inc()
		v, lerr := loader(ctx)
		return v, false, lerr
	}

	dataKey := k.DataKey(cfg.SchemaVersion, gen)
	scopeTag := k.ScopeTag()

	// Issue #1531 M4: the shared singleflight execution runs on a DETACHED context
	// (context.WithoutCancel(ctx), bounded by singleflightLoadTimeout), never the raw ctx of
	// whichever caller happened to be first through sf.Do. Only one goroutine's closure actually
	// runs per in-flight dataKey — every other concurrent caller for the same key just blocks on
	// its result — so without detaching, that ONE caller's context governs the outcome for ALL of
	// them: if member A's request is the one that wins the race and A then navigates away
	// (cancelling A's ctx), the shared loader returns context.Canceled and singleflight hands that
	// SAME error to member B, a concurrent but otherwise healthy request that never had a
	// cancelled context of its own — a spurious 500 on a request that would have succeeded before
	// this branch. Detaching also protects against inheriting a shorter caller's deadline: sfCtx
	// is bounded independently, not by whichever caller's remaining deadline happened to be
	// shortest. singleflightLoadTimeout (30s) matches this server's default WriteTimeout
	// (internal/config/config.go) — a load can never legitimately need longer than the server
	// would allow a response to take anyway.
	sfCtx, sfCancel := context.WithTimeout(context.WithoutCancel(ctx), singleflightLoadTimeout)
	defer sfCancel()

	raw, err, shared := c.sf.Do(dataKey, func() (any, error) {
		res, lerr := c.getOrLoadOnce(sfCtx, ns, cfg, dataKey, scopeTag, loader)
		return res, lerr
	})
	if shared {
		cacheSingleflightSharedTotal.WithLabelValues(string(ns)).Inc()
	}
	if err != nil {
		return nil, false, err
	}
	res := raw.(sfResult)
	return res.data, res.hit, nil
}

// getOrLoadOnce runs the actual GET -> decode -> (miss ? loader+SET : return) sequence for one
// fully-rendered data key. Only ever invoked once per in-flight key, via singleflight.
func (c *redisCache) getOrLoadOnce(
	ctx context.Context, ns Namespace, cfg namespaceConfig, dataKey, scopeTag string,
	loader func(context.Context) ([]byte, error),
) (sfResult, error) {
	getStart := c.now()
	raw, err := c.client.Get(ctx, dataKey)
	cacheOperationDuration.WithLabelValues(string(ns), opGet).Observe(c.now().Sub(getStart).Seconds())

	switch {
	case err == nil:
		body, decErr := decodeEnvelope([]byte(raw), cfg.SchemaVersion, cfg.TTL, c.now())
		if decErr == nil {
			cacheRequestsTotal.WithLabelValues(string(ns), resultHit).Inc()
			return sfResult{data: body, hit: true}, nil
		}
		// Envelope present but unusable (garbage, schema mismatch, or stale). Treat as a miss.
		// Do NOT delete the key here: a concurrent writer may have replaced it since this GET
		// started, and deleting now would erase that fresher write (racy).
		switch {
		case errors.Is(decErr, ErrSchemaMismatch):
			cacheErrorsTotal.WithLabelValues(string(ns), opGet, errKindSchemaMismatch).Inc()
		case errors.Is(decErr, ErrStaleEnvelope):
			cacheStaleEnvelopeTotal.WithLabelValues(string(ns)).Inc()
		default:
			cacheErrorsTotal.WithLabelValues(string(ns), opGet, errKindCodec).Inc()
		}
		cacheRequestsTotal.WithLabelValues(string(ns), resultError).Inc()
		return c.loadAndSet(ctx, ns, cfg, dataKey, scopeTag, loader)

	case errors.Is(err, redisclient.ErrNotFound):
		cacheRequestsTotal.WithLabelValues(string(ns), resultMiss).Inc()
		return c.loadAndSet(ctx, ns, cfg, dataKey, scopeTag, loader)

	case errors.Is(err, redisclient.ErrBreakerOpen):
		// Circuit open: loader only. Skip even attempting a SET — the breaker would reject that
		// too, so there is nothing to gain from trying.
		cacheRequestsTotal.WithLabelValues(string(ns), resultBypassCircuit).Inc()
		val, lerr := loader(ctx)
		if lerr != nil {
			return sfResult{}, lerr
		}
		return sfResult{data: val, hit: false}, nil

	default:
		// Any other Redis GET error: treat as miss, still attempt loader -> SET.
		cacheErrorsTotal.WithLabelValues(string(ns), opGet, errKindRedis).Inc()
		cacheRequestsTotal.WithLabelValues(string(ns), resultError).Inc()
		return c.loadAndSet(ctx, ns, cfg, dataKey, scopeTag, loader)
	}
}

// loadAndSet calls loader and, only on success, best-effort caches the result. A loader error is
// propagated as-is and NOTHING is cached.
func (c *redisCache) loadAndSet(
	ctx context.Context, ns Namespace, cfg namespaceConfig, dataKey, scopeTag string,
	loader func(context.Context) ([]byte, error),
) (sfResult, error) {
	val, err := loader(ctx)
	if err != nil {
		return sfResult{}, err
	}
	c.set(ctx, ns, cfg, dataKey, scopeTag, val)
	return sfResult{data: val, hit: false}, nil
}

// set writes val into dataKey, UNLESS scopeTag currently carries an active deletion tombstone
// (Issue #1531 Task 10b's GDPR purge — see purge.go's Purger doc comment): the tombstone check
// runs before every write in this package, closing the race where a fill that started before a
// user's deletion finishes and tries to write stale data back into their scope after the purge
// has already scanned and deleted it.
//
// Issue #1531 M5: the pre-Set check above is NOT atomic with the Set that follows it — a purge's
// tombstone+scan+delete can land in the gap between them. If the tombstone is written after this
// method's first check passes but the purge's SCAN has already run by the time the Set below
// lands, the purge finds nothing at dataKey to delete, and this write survives: a deleted user's
// cached data resurrected for up to the namespace TTL. A SECOND tombstone check immediately after
// the Set closes this: any tombstone visible at that point can only have been set no earlier than
// the purge that owns it, and because tombstoneTTL (900s) safely exceeds every registered
// namespace TTL, deleting the key this method just wrote is always safe and always sufficient,
// regardless of exactly when the purge's SCAN ran relative to the Set. This does not require a
// third check ad infinitum: once the write has been rolled back, there is nothing left for a
// LATER purge to have missed.
func (c *redisCache) set(ctx context.Context, ns Namespace, cfg namespaceConfig, dataKey, scopeTag string, val []byte) {
	if c.isTombstoned(ctx, scopeTag) {
		cacheRequestsTotal.WithLabelValues(string(ns), resultBypassTombstoned).Inc()
		return
	}

	env, err := encodeEnvelope(cfg.SchemaVersion, val, c.now())
	if err != nil {
		cacheErrorsTotal.WithLabelValues(string(ns), opSet, errKindCodec).Inc()
		return
	}

	setStart := c.now()
	setErr := c.client.Set(ctx, dataKey, string(env), cfg.TTL)
	cacheOperationDuration.WithLabelValues(string(ns), opSet).Observe(c.now().Sub(setStart).Seconds())
	if setErr != nil {
		cacheErrorsTotal.WithLabelValues(string(ns), opSet, errKindRedis).Inc()
		return
	}

	if c.isTombstoned(ctx, scopeTag) {
		_ = c.client.Del(ctx, dataKey)
		cacheRequestsTotal.WithLabelValues(string(ns), resultBypassTombstoned).Inc()
		return
	}

	cachePayloadBytes.WithLabelValues(string(ns)).Observe(float64(len(val)))
}
