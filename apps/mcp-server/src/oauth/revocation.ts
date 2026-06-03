/**
 * Layer-2 revocation check for MCP OAuth sessions.
 *
 * On every /mcp and /sse request, assertNotRevoked() calls Clerk's token
 * introspection endpoint to verify the stored Clerk OAuth access token is still
 * active. Results are cached per-isolate for ~30 seconds to bound latency.
 *
 * Design:
 *   Layer 1 — stateless: workers-oauth-provider validates its own token (fast).
 *   Layer 2 — this module: Clerk introspection confirms the OAuth grant has not
 *             been revoked (adds ~50–100ms on cache miss, free on cache hit).
 *
 * Cache key: SHA-256 hex of the access token (never stored as a raw string).
 * Per-state TTL (see CACHE_TTLS):
 *   verified_active  — 30 s
 *   revoked          — 60 s (longer to prevent re-admission race)
 *   unknown_fail_open — 0 (never cached; re-checked every request)
 *
 * Failure mode: FAIL-OPEN. Clerk 5xx / network errors allow the request.
 * Layer-1 validation still applies; only confirmed revocations are rejected.
 * This is a best-effort SLA — not a hard security boundary.
 *
 * Caveats:
 *   - Cache is per-isolate in-memory; markRevoked() only affects the current
 *     isolate. KV/DO-backed shared cache is a future upgrade if needed.
 *   - /sse connections are checked at connect time, not per message; long-lived
 *     streams may survive up to the remaining session lifetime after revocation.
 *   - Effective revocation SLA ≈ max(30 s, request-arrival skew across isolates).
 *
 * Spike results (2026-06-02, Step-0 of issue #1224 plan):
 *   Active token  → HTTP 200, { id, revoked: false, expired: false, ... }
 *   Revoked token → HTTP 404, { errors: [{ code: "oauth_access_token_not_found" }] }
 * Revocation is reflected immediately (not at token expiry). Approach A is confirmed.
 */

import { TokenVerificationError } from './clerk-verifier'
import { MCP_WORKER_USER_AGENT } from '../client/user-agent'

/** Tri-state introspection result (never a plain boolean). */
type IntrospectionResult = 'verified_active' | 'revoked' | 'unknown_fail_open'

interface CacheEntry {
  state: IntrospectionResult
  checkedAt: number
}

/** Minimal env shape required by this module. */
interface RevocationEnv {
  CLERK_SECRET_KEY: string
}

// ─── In-memory cache ──────────────────────────────────────────────────────────

/** Per-isolate revocation cache: SHA-256(accessToken) → { state, checkedAt } */
const revocationCache = new Map<string, CacheEntry>()

/** TTL for each cache state (verified_active, revoked, unknown_fail_open). */
const CACHE_TTLS: Record<IntrospectionResult, number> = {
  verified_active: 30_000,
  revoked: 60_000, // longer TTL to block re-admission races
  unknown_fail_open: 0, // never cached; re-check every request
}

/** Clerk Backend API — token introspection endpoint. */
const CLERK_VERIFY_URL = 'https://api.clerk.com/oauth_applications/access_tokens/verify'

// ─── SHA-256 cache key ────────────────────────────────────────────────────────

/**
 * Compute a SHA-256 hex digest of an access token for use as a cache key.
 * Exported so callers of markRevoked can compute the same key without
 * holding the raw token.
 */
export async function computeCacheKey(accessToken: string): Promise<string> {
  const buf = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(accessToken))
  return Array.from(new Uint8Array(buf), b => b.toString(16).padStart(2, '0')).join('')
}

// ─── Clerk introspection ──────────────────────────────────────────────────────

/**
 * Call Clerk's token introspection endpoint and return a tri-state result.
 *
 * Verified response contract (from Step-0 staging spike, 2026-06-02):
 *
 *   Active token  → HTTP 200, body: { id: "oat_...", revoked: false, expired: false, ... }
 *   Revoked token → HTTP 404, body: { errors: [{ code: "oauth_access_token_not_found" }] }
 *
 * Mapping:
 *   HTTP 200 + id present + revoked:false + expired:false → 'verified_active'
 *   HTTP 200 + revoked:true OR expired:true              → 'revoked'
 *   HTTP 404 + oauth_access_token_not_found              → 'revoked' (deleted after revocation)
 *   5xx / network error / timeout / missing secret       → 'unknown_fail_open'
 *   Other 4xx or unexpected shape                        → 'unknown_fail_open'
 */
async function introspectClerkToken(
  accessToken: string,
  env: RevocationEnv,
): Promise<IntrospectionResult> {
  if (!env.CLERK_SECRET_KEY) {
    // Warn on every call so a production misconfiguration surfaces in Worker logs.
    // Does not throw — revocation is a best-effort layer-2 check (layer-1 still applies).
    console.warn('[revocation] CLERK_SECRET_KEY not configured — revocation check skipped (fail-open)')
    return 'unknown_fail_open'
  }

  // 3 s timeout — a slow Clerk response should not stall MCP requests; we fail-open instead.
  const controller = new AbortController()
  const timeout = setTimeout(() => controller.abort(), 3_000)

  try {
    const res = await fetch(CLERK_VERIFY_URL, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'Authorization': `Bearer ${env.CLERK_SECRET_KEY}`,
        'User-Agent': MCP_WORKER_USER_AGENT,
      },
      body: JSON.stringify({ access_token: accessToken }),
      signal: controller.signal,
    })

    clearTimeout(timeout)

    if (res.ok) {
      const body = (await res.json()) as {
        id?: string
        revoked?: boolean
        expired?: boolean
      }
      if (body.id && body.revoked === false && body.expired === false) return 'verified_active'
      if (body.revoked === true || body.expired === true) return 'revoked'
      // Unexpected shape (schema drift, partial response) → fail-open; no caching.
      console.warn('[revocation] Clerk verify: unexpected response body shape — fail-open')
      return 'unknown_fail_open'
    }

    // HTTP 404 "oauth_access_token_not_found" = token was revoked and deleted by Clerk.
    // By the time we reach assertNotRevoked, layer-1 (workers-oauth-provider) has validated
    // the MCP token, so ctx.props.accessToken is a genuine Clerk OAuth token. A 404 means
    // Clerk no longer recognises it — treat as revoked, not as a transient error.
    if (res.status === 404) {
      const body = (await res.json().catch(() => ({}))) as {
        errors?: Array<{ code?: string }>
      }
      if (body.errors?.some(e => e.code === 'oauth_access_token_not_found')) {
        return 'revoked'
      }
    }

    // Other 4xx (rate-limits, auth errors on our secret, etc.) or 5xx → fail-open.
    console.warn('[revocation] Clerk verify returned non-2xx status', res.status, '— fail-open')
    return 'unknown_fail_open'
  } catch {
    // Network error or abort (timeout) → fail-open.
    clearTimeout(timeout)
    return 'unknown_fail_open'
  }
}

// ─── Public API ───────────────────────────────────────────────────────────────

/**
 * Assert that the given Clerk OAuth access token has not been revoked.
 *
 * On cache miss, calls Clerk's introspection endpoint (fail-open on errors).
 * Throws TokenVerificationError('invalid_token') for confirmed revocations.
 *
 * @param params - { accessToken } — the Clerk OAuth access token from ctx.props
 * @param env    - bindings (requires CLERK_SECRET_KEY)
 */
export async function assertNotRevoked(
  params: { accessToken: string },
  env: RevocationEnv,
): Promise<void> {
  const key = await computeCacheKey(params.accessToken)
  const cached = revocationCache.get(key)

  if (cached) {
    const age = Date.now() - cached.checkedAt
    const ttl = CACHE_TTLS[cached.state]

    if (age < ttl) {
      if (cached.state === 'revoked') {
        throw new TokenVerificationError('invalid_token', 'Grant has been revoked')
      }
      return
    }
  }

  const result = await introspectClerkToken(params.accessToken, env)

  // Cache verified_active and revoked; do NOT cache unknown_fail_open.
  if (result !== 'unknown_fail_open') {
    revocationCache.set(key, { state: result, checkedAt: Date.now() })
  }

  if (result === 'revoked') {
    throw new TokenVerificationError('invalid_token', 'Grant has been revoked')
  }
}

/**
 * Mark a token as revoked in the local cache using its pre-computed cache key.
 *
 * ⚠️ `cacheKey` must be a SHA-256 hex digest — call `computeCacheKey(accessToken)`
 * to produce the correct value. Passing the raw token string will create a cache
 * entry that never matches. This API is intentionally low-level to keep the raw
 * token out of module scope.
 *
 * Used when a revocation event is detected externally (e.g., a webhook).
 * Only affects the current isolate's cache — not durable across instances.
 */
export function markRevoked(cacheKey: string): void {
  revocationCache.set(cacheKey, { state: 'revoked', checkedAt: Date.now() })
}

/** Clear the revocation cache (used in tests). */
export function clearRevocationCache(): void {
  revocationCache.clear()
}

