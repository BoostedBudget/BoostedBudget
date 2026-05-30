/**
 * Revocation check for Clerk OAuth access tokens.
 *
 * ⚠️ KNOWN LIMITATION: The current implementation does NOT call Clerk's session
 * status API. assertNotRevoked() unconditionally marks grants as valid after a
 * cache miss. Effective revocation SLA = Clerk access token TTL (~5 minutes),
 * NOT the 30-second cache described below.
 *
 * Revoking an OAuth grant in BoostedBudget settings will stop new authentications
 * immediately (the JWKS validation in clerk-verifier.ts will reject new tokens),
 * but existing sessions continue until token expiry (~5 minutes by default).
 *
 * To achieve true sub-60s revocation, implement a Clerk session status API call
 * in assertNotRevoked() — see CLERK_SECRET_KEY in wrangler.toml secrets.
 * This is tracked as a follow-up improvement.
 *
 * markRevoked() is used when this Worker instance is explicitly notified of a
 * revocation event (e.g., via a webhook) — it provides faster local revocation
 * for the current isolate's in-memory cache.
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
