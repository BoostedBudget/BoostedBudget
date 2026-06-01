/**
 * Revocation gate helper for McpApiHandler.
 *
 * Extracted into its own file (no Worker-runtime imports) so it can be
 * unit-tested with bun:test (see GAP-R3-20 for the broader Worker testing gap).
 *
 * McpApiHandler calls checkRevocationOrRespond at the top of every fetch():
 *   - Returns null   → token is active (or fail-open); continue handling the request.
 *   - Returns Response → token is revoked; return the 401 immediately.
 */

import { assertNotRevoked } from '../oauth/revocation'
import { TokenVerificationError } from '../oauth/clerk-verifier'
import { unauthorizedResponse } from '../oauth/resource-server'

export interface RevocationGateEnv {
  CLERK_SECRET_KEY: string
  MCP_RESOURCE_URL: string
}

/**
 * Check whether the provided Clerk access token has been revoked.
 *
 * Returns null if the token is active (or the check fails-open due to a Clerk
 * API error). Returns a 401 Response if the token is confirmed as revoked.
 *
 * The caller (McpApiHandler.fetch) should return the response immediately:
 *   const gate = await checkRevocationOrRespond(accessToken, env)
 *   if (gate) return gate
 */
export async function checkRevocationOrRespond(
  accessToken: string,
  env: RevocationGateEnv,
): Promise<Response | null> {
  try {
    await assertNotRevoked({ accessToken }, env)
  } catch (e) {
    if (e instanceof TokenVerificationError) {
      return unauthorizedResponse(env.MCP_RESOURCE_URL, 'invalid_token')
    }
    throw e
  }
  return null
}
