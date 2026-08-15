package cache

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"

	"github.com/maxnussbaum/BoostedBudget/internal/redisclient"
)

// Generations manages per-scope generation tokens.
//
// A generation token is a FRESH RANDOM 128-bit value, minted with crypto/rand and stored via
// SET/SETNX — never an incrementing counter. This is load-bearing, not a style choice: Upstash's
// eviction policy is `optimistic-volatile`, is NOT configurable, and explicitly evicts non-TTL
// keys (which is what a generation key is) when volatile keys run short. So a generation key CAN
// be evicted in production.
//
//   - With a COUNTER, losing the key resets it to 1, and every previously-written data key at
//     generations 1..N — persisted, since data keys still carry their own TTL and may not have
//     expired yet — becomes LIVE again. That serves arbitrarily old financial data.
//   - With a MINTED TOKEN, losing the key just orphans every existing data key (their rendered
//     key string embeds the old, now-unreachable token): cold cache, correct-but-slow. Never
//     "optimise" this back to a counter.
//
// PTTL(gen) == -1 (no expiry) is a hygiene assertion this package can test for, not a claim of
// eviction immunity — that property does not exist on Upstash.
type Generations struct {
	client redisclient.Client
}

// NewGenerations returns a Generations backed by client, which MUST be the cache's own
// eviction-enabled Redis client (never the limiter's no-eviction instance — that split matters
// for control.go's kill switches, not here, but the same client is reused for both data and
// generation keys within the cache instance).
func NewGenerations(client redisclient.Client) *Generations {
	return &Generations{client: client}
}

// Get returns the current generation token for the given scope, minting a fresh one if absent.
//
// A Redis READ error is returned as-is (never masked into a minted token): the caller MUST bypass
// the cache entirely on this path — call the loader, do not read data, do not write data — because
// serving data under a generation this method could not confirm is exactly the stale-money failure
// this whole package exists to prevent.
func (g *Generations) Get(ctx context.Context, kind ScopeKind, scopeID string) (string, error) {
	key := GenerationKey(ScopeTag(kind, scopeID))

	val, err := g.client.Get(ctx, key)
	if err == nil {
		return val, nil
	}
	if !errors.Is(err, redisclient.ErrNotFound) {
		// A genuine read error (timeout, breaker open, etc.) — propagate untouched.
		return "", err
	}

	// Absent: mint a brand-new random token and try to claim it.
	token := mintGenerationToken()
	won, setErr := g.client.SetNX(ctx, key, token, 0) // no TTL: see the type doc comment
	if setErr != nil {
		return "", setErr
	}
	if won {
		cacheGenerationMintsTotal.WithLabelValues(string(kind)).Inc()
		return token, nil
	}

	// Lost the race to a concurrent minter: read back whichever token actually won.
	return g.client.Get(ctx, key)
}

// Bump invalidates every data key for the given scope by minting and writing a fresh generation
// token unconditionally. source is a short static label (e.g. "invalidation") for the
// cache_generation_bumps_total metric — never a raw identifier.
//
// A write failure here does NOT fail the caller's request: the underlying data write that
// triggered this bump already committed, so returning an error here would be misleading (the
// write succeeded; only the cache invalidation of it did not). It is instead logged at ERROR and
// counted on cache_bump_failures_total, which the brief flags as an alerting metric — a
// (temporarily) inconsistent cache is a real risk, but failing the request that already succeeded
// would be worse.
func (g *Generations) Bump(ctx context.Context, kind ScopeKind, scopeID string, source string) {
	key := GenerationKey(ScopeTag(kind, scopeID))
	token := mintGenerationToken()

	if err := g.client.Set(ctx, key, token, 0); err != nil {
		cacheBumpFailuresTotal.WithLabelValues(string(kind)).Inc()
		slog.Default().Error("cache: generation bump failed; stale reads may be served until the "+
			"generation key is next lost or bumped",
			"scope_kind", kind, "source", source, "error", err)
		return
	}
	cacheGenerationBumpsTotal.WithLabelValues(string(kind), source).Inc()
}

// mintGenerationToken returns a fresh random 128-bit token, hex-encoded. crypto/rand.Read only
// fails if the OS entropy source is unavailable, which is treated as unrecoverable — a process
// that cannot mint random tokens cannot safely run this cache.
func mintGenerationToken() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("cache: crypto/rand unavailable: " + err.Error())
	}
	return hex.EncodeToString(b[:])
}
