# Redis Topology (Issue #1531)

BoostedBudget uses **two separate Redis instances**, never one shared instance:

| Instance | Env var | Local port | Eviction policy | Purpose |
|---|---|---|---|---|
| Rate limiter | `RATE_LIMIT_REDIS_URL` | `6379` (dev), `6380` (QA) | `noeviction` | Fleet-wide request rate limiting |
| Cache | `CACHE_REDIS_URL` | `6380` (dev), `6381` (QA) | `allkeys-lru` (local) / `optimistic-volatile` (Upstash prod) | Distributed read-through cache |

## Why two instances, not one

The rate limiter's correctness depends on every `SET` it issues succeeding — a denied
`SET` means a request that should have been throttled goes through untracked. The
limiter's Redis instance is therefore provisioned `--maxmemory-policy noeviction`: it
would rather run out of memory and start refusing commands than silently drop a
limiter key.

If the cache shared that same instance, its write volume would eventually fill it. Once
that happens, the limiter's own `SET`s start failing with:

```
OOM command not allowed when used memory > 'maxmemory'
```

The circuit breaker in `backend/internal/redisclient/breaker.go` sees this as a
failure, trips, and the limiter permanently falls back to its per-machine in-memory
mode. The resulting fallback alert is *correct* (the limiter really did fall back) but
the *cause* looks like "Redis is down" when the real cause is "the cache next door
filled up the wrong instance." This is exactly the failure this topology is designed to
make structurally impossible: `redisclient.New` is one constructor, called twice
(rate limiter + cache), each with its own `Options.URL`, and it deliberately never
falls back to the limiter's URL when the cache URL is empty. See
`backend/internal/redisclient/client.go`'s package doc ("Two clients, never share
one") for the full mechanical explanation, and `backend/internal/config/config.go`'s
`validate()` rejects `CACHE_REDIS_URL == RATE_LIMIT_REDIS_URL` outright in
production/staging.

## Local approximation vs. Upstash production

Locally (`docker-compose.yml`, `docker-compose.qa.yml`) the cache Redis container runs
`--maxmemory-policy allkeys-lru`. Production/staging use Upstash, which does **not**
let you choose an eviction policy — Upstash always runs its own `optimistic-volatile`
policy, which is not equivalent to any open-source Redis `--maxmemory-policy` value.
`allkeys-lru` is the closest available local shape (it evicts under memory pressure
without requiring every key to carry a TTL) — this is a deliberate, acknowledged
approximation, not a claim that local mirrors Upstash's behavior exactly.

**Upstash's `optimistic-volatile` policy can evict keys that have no TTL at all.** This
is why cache generation values (used to invalidate whole namespaces at once) are minted
as random tokens rather than incrementing counters: an evicted generation key must never
be able to silently roll back to a value the cache has already used, which an
incrementing counter restarted from zero could do. A random token can only ever produce
a fresh generation, never repeat one that was already invalidated.

## Kill switches

- `CACHE_ENABLED` (default `true`) — top-level kill switch for the cache. `false`
  disables all cache reads/writes; the app falls through to hitting the database
  directly, cache absence is always non-fatal.
- `CACHE_NAMESPACES_DISABLED` (default `""`, comma-separated) — disables specific cache
  namespaces individually (e.g. `analytics_rows,dashprefs`) while leaving the rest of the
  cache enabled. Use this to isolate a single misbehaving namespace without giving up the
  whole cache.
- `RATE_LIMIT_BACKEND` (default `auto`) — `auto` resolves to Redis when
  `RATE_LIMIT_REDIS_URL` is configured, memory otherwise; `memory` forces in-memory
  per-machine limiting; `redis` forces Redis-backed limiting (fails fast if
  misconfigured). Rejected at `Load()` time if set to anything else, in every
  environment.
- `RATE_LIMIT_SENTINEL_LIMIT` (default `0`, meaning "use `RATE_LIMIT_IP`") — **escape
  hatch**: lets an operator raise the fleet-wide sentinel bucket's limit at runtime
  (a deploy with an updated secret/env var, no code change) during a live incident,
  without waiting on a full release.
- `RATE_LIMIT_FALLBACK_DIVISOR` (default `1`) — divides the per-machine in-memory
  fallback limiter's own limits, so that N machines each independently falling back to
  memory-only limiting do not multiply the intended fleet-wide budget by N.

## Machine count and the rate limiter

`backend/fly.toml` / `backend/fly.staging.toml`'s `min_machines_running` is a **floor**,
not a ceiling, and is inert on both apps today since `auto_stop_machines = false` means
machines are never stopped/started based on load in the first place. Whatever the
eventual machine count is, rate-limiter correctness across multiple machines depends on
`RATE_LIMIT_REDIS_URL` being configured and reachable — not on the machine count itself.
Each machine's in-memory fallback limiter only ever sees that machine's own traffic, so
without Redis, N machines effectively multiply the real fleet-wide limit by N (mitigated,
but not eliminated, by `RATE_LIMIT_FALLBACK_DIVISOR`).
