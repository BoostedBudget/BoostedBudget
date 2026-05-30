/**
 * Clerk OAuth access token verifier.
 *
 * Validates Clerk-issued OAuth access tokens via JWKS.
 * Returns an AuthContext with the claims needed by the MCP host-proxy.
 *
 * Spike results:
 *   - Clerk OAuth only supports built-in scopes; no custom mcp:read/write
 *   - `aud` claim = the registered OAuth client_id; validate accordingly
 *   - Authorization granularity is enforced by the capability manifest,
 *     not by token scopes
 */

import { createRemoteJWKSet, jwtVerify } from 'jose'

export type TokenErrorCode = 'missing_token' | 'invalid_token' | 'expired_token'

/** Thrown when token validation fails — maps to 401 with error= code */
export class TokenVerificationError extends Error {
  constructor(
    public readonly code: TokenErrorCode,
    message: string,
  ) {
    super(message)
    this.name = 'TokenVerificationError'
  }
}

export interface AuthContextParams {
  clerkUserId: string
  grantId: string
  scopes: string[]
  tokenExp: number
}

/** Verified token context passed to the host-proxy on every request */
export class AuthContext {
  readonly clerkUserId: string
  readonly grantId: string
  readonly scopes: string[]
  readonly tokenExp: number

  constructor(params: AuthContextParams) {
    this.clerkUserId = params.clerkUserId
    this.grantId = params.grantId
    this.scopes = params.scopes
    this.tokenExp = params.tokenExp
  }

  /** Returns true if the token has already expired (clock comparison) */
  isExpired(): boolean {
    return Math.floor(Date.now() / 1000) >= this.tokenExp
  }
}

interface ClerkVerifierEnv {
  CLERK_JWKS_URL: string
  /** Clerk Frontend API URL (issuer), e.g. https://clerk.boostedbudget.com */
  CLERK_ISSUER_URL: string
  /** OAuth client_id registered for this MCP app in Clerk — used as audience */
  MCP_OAUTH_AUDIENCE: string
}

const jwksCache = new Map<string, ReturnType<typeof createRemoteJWKSet>>()

function getJwks(jwksUrl: string): ReturnType<typeof createRemoteJWKSet> {
  if (!jwksCache.has(jwksUrl)) {
    jwksCache.set(jwksUrl, createRemoteJWKSet(new URL(jwksUrl)))
  }
  return jwksCache.get(jwksUrl)!
}

/**
 * Verify a Clerk OAuth access token and return an AuthContext.
 * Throws TokenVerificationError on failure.
 */
export async function verifyClerkToken(token: string, env: ClerkVerifierEnv): Promise<AuthContext> {
  const jwks = getJwks(env.CLERK_JWKS_URL)

  try {
    const { payload } = await jwtVerify(token, jwks, {
      issuer: env.CLERK_ISSUER_URL,
      audience: env.MCP_OAUTH_AUDIENCE,
    })

    const clerkUserId = payload.sub
    if (!clerkUserId) {
      throw new TokenVerificationError('invalid_token', 'Token missing sub claim')
    }

    const grantId = (payload.sid as string | undefined) ?? 'unknown'
    const scopes = ((payload.scope as string | undefined) ?? '').split(' ').filter(Boolean)
    const tokenExp = payload.exp ?? 0

    return new AuthContext({ clerkUserId, grantId, scopes, tokenExp })
  } catch (err) {
    if (err instanceof TokenVerificationError) throw err

    const message = err instanceof Error ? err.message : 'Token verification failed'
    throw new TokenVerificationError(
      message.includes('expired') ? 'expired_token' : 'invalid_token',
      message.includes('expired') ? 'Token has expired' : message,
    )
  }
}

/** Static helpers on ClerkTokenVerifier namespace (used in tests + middleware) */
export const ClerkTokenVerifier = {
  /**
   * Extract the raw JWT from an Authorization header.
   * Throws TokenVerificationError if missing, wrong scheme, or empty.
   */
  extractBearerToken(authHeader: string | undefined): string {
    if (!authHeader) {
      throw new TokenVerificationError('missing_token', 'Authorization header required')
    }
    const parts = authHeader.split(' ')
    if (parts.length !== 2 || parts[0] !== 'Bearer' || !parts[1]) {
      throw new TokenVerificationError(
        'invalid_token',
        'Authorization header must be: Bearer <token>',
      )
    }
    return parts[1]
  },
}
