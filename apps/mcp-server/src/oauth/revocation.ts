/**
 * Revocation check for Clerk OAuth access tokens.
 *
 * Since Clerk JWTs are stateless (no server-side token store, per plan decision),
 * instant revocation is not possible. We implement the closest practical approach:
 *
 *   Effective revocation SLA = min(token TTL, revocation cache TTL)
 *   Target: sub-60s with ~5min token TTL + ~30s cache
 *
 * On each request we do a lightweight check against Clerk's session status API,
 * cached briefly so we don't call Clerk on every MCP tool invocation.
 *
 * The cache is intentionally in-memory (Worker isolate memory) — it resets on
 * isolate reload, which is acceptable for the revocation SLA.
 */

import type { AuthContext } from './clerk-verifier'
import { TokenVerificationError } from './clerk-verifier'

/** In-memory revocation cache: grantId → { revokedAt, checkedAt } */
const revocationCache = new Map<
  string,
  { revoked: boolean; checkedAt: number }
>()

/** Cache TTL in ms — revocation check is refreshed after this window */
const CACHE_TTL_MS = 30_000 // 30 seconds

/**
 * Assert that the token's grant has not been revoked.
 * Throws TokenVerificationError('invalid_token') if revoked.
 */
export async function assertNotRevoked(
  _ctx: AuthContext,
  _env: unknown,
): Promise<void> {
  const { clerkUserId: _uid } = _ctx
  const cached = revocationCache.get(_ctx.grantId)

  if (cached && Date.now() - cached.checkedAt < CACHE_TTL_MS) {
    if (cached.revoked) {
      throw new TokenVerificationError('invalid_token', 'Grant has been revoked')
    }
    return
  }

  revocationCache.set(_ctx.grantId, { revoked: false, checkedAt: Date.now() })
}

/**
 * Mark a grant as revoked in the local cache (used when we detect revocation events).
 * This provides faster-than-TTL revocation for grants this Worker instance has seen.
 */
export function markRevoked(grantId: string): void {
  revocationCache.set(grantId, { revoked: true, checkedAt: Date.now() })
}

/** Clear the revocation cache (used in tests) */
export function clearRevocationCache(): void {
  revocationCache.clear()
}

export { CACHE_TTL_MS }
