/**
 * Revocation gate helper for McpApiHandler.
 *
 * Extracted into its own file (no Worker-runtime imports) so it can be
 * unit-tested with bun:test (see GAP-R3-20 for the broader Worker testing gap).
 *
 * McpApiHandler calls checkRevocationOrRespond at the top of every fetch():
 *   - Returns null     → token is active (or fail-open); continue handling the request.
 *   - Returns Response → token is revoked; return the 401 immediately.
 *
 * On a confirmed revocation, a best-effort audit entry is fired (fire-and-forget,
 * same pattern as tool-level audit calls). Audit failures never block the 401 response.
 */

import { assertNotRevoked } from '../oauth/revocation'
import { TokenVerificationError } from '../oauth/clerk-verifier'
import { unauthorizedResponse } from '../oauth/resource-server'
import { createAuditClient } from '../audit/audit-client'

export interface RevocationGateEnv {
  CLERK_SECRET_KEY: string
  MCP_RESOURCE_URL: string
  // Audit client fields — createAuditClient returns a no-op if any are absent.
  BOOSTEDBUDGET_API_URL: string
  BOOSTEDBUDGET_ORIGIN_SECRET: string
  MCP_WORKER_SECRET: string
}

/**
 * Check whether the provided Clerk access token has been revoked.
 *
 * Returns null if the token is active (or the check fails-open due to a Clerk
 * API error). Returns a 401 Response if the token is confirmed as revoked.
 * Fires a best-effort revocation-denial audit entry on revoked 401s.
 *
 * The caller (McpApiHandler.fetch) should return the response immediately:
 *   const gate = await checkRevocationOrRespond({ accessToken, clerkUserId }, env)
 *   if (gate) return gate
 */
export async function checkRevocationOrRespond(
  params: { accessToken: string; clerkUserId: string },
  env: RevocationGateEnv,
): Promise<Response | null> {
  try {
    await assertNotRevoked({ accessToken: params.accessToken }, env)
    return null
  } catch (e) {
    if (e instanceof TokenVerificationError) {
      // Fire-and-forget audit entry. grantId is always 'unknown' in live path
      // (McpApiHandler never sets X-MCP-Grant-Id).
      void createAuditClient(env).record({
        actorUserId: params.clerkUserId,
        grantId: 'unknown',
        operationClass: 'read',
        operationId: 'mcp.revocation_denied',
        result: 'denied',
        revocationState: 'revoked',
      })
      return unauthorizedResponse(env.MCP_RESOURCE_URL, 'invalid_token')
    }
    throw e
  }
}
