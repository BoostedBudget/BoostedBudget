import { describe, expect, it } from 'bun:test'
import { ClerkTokenVerifier, AuthContext, TokenVerificationError, verifyClerkToken } from './clerk-verifier'
import type { JwtVerifyFn } from './clerk-verifier'

// We test verifyClerkToken by injecting a mock jwtVerifyFn (dependency injection, not module mock).
// This avoids the process-global side effects of bun:test mock.module() on the jose package.

const fakeEnv = {
  CLERK_JWKS_URL: 'https://clerk.example.com/.well-known/jwks.json',
  CLERK_ISSUER_URL: 'https://clerk.example.com',
  MCP_OAUTH_AUDIENCE: 'client_123',
}

// Use explicit `null` sentinel to omit a field from the payload (undefined falls back to default)
function makeJwtVerifyFn(overrides: Partial<{ sub: string | null; sid: string | null; scope: string; exp: number }>): JwtVerifyFn {
  return async () => {
    const payload: Record<string, unknown> = {
      iss: fakeEnv.CLERK_ISSUER_URL,
      aud: fakeEnv.MCP_OAUTH_AUDIENCE,
      scope: overrides.scope ?? 'openid email',
      exp: overrides.exp ?? Math.floor(Date.now() / 1000) + 3600,
    }
    // null means explicitly omit the field; undefined means use default
    if (overrides.sub !== null) payload.sub = overrides.sub ?? 'user_abc'
    if (overrides.sid !== null) payload.sid = overrides.sid ?? 'session_xyz'
    return { payload, protectedHeader: { alg: 'RS256' } } as ReturnType<JwtVerifyFn>
  }
}

function makeFailingJwtVerifyFn(errorMessage: string): JwtVerifyFn {
  return async () => { throw new Error(errorMessage) }
}

describe('verifyClerkToken', () => {
  it('R4-10: returns AuthContext for a valid token', async () => {
    const ctx = await verifyClerkToken('fake.jwt.token', fakeEnv, makeJwtVerifyFn({}))
    expect(ctx.clerkUserId).toBe('user_abc')
    expect(ctx.grantId).toBe('session_xyz')
    expect(ctx.scopes).toEqual(['openid', 'email'])
    expect(ctx.isExpired()).toBe(false)
  })

  it('R4-10: throws expired_token when jose reports "expired"', async () => {
    const fn = makeFailingJwtVerifyFn('"exp" claim timestamp check failed (it is expired)')
    await expect(verifyClerkToken('old.jwt', fakeEnv, fn)).rejects.toMatchObject({
      code: 'expired_token',
    })
  })

  it('R4-10: throws invalid_token for wrong-audience/issuer errors', async () => {
    const fn = makeFailingJwtVerifyFn('unexpected "aud" claim value')
    await expect(verifyClerkToken('bad.jwt', fakeEnv, fn)).rejects.toMatchObject({
      code: 'invalid_token',
    })
  })

  it('R4-10: throws invalid_token for malformed token', async () => {
    const fn = makeFailingJwtVerifyFn('invalid compact serialization')
    await expect(verifyClerkToken('garbage', fakeEnv, fn)).rejects.toMatchObject({
      code: 'invalid_token',
    })
  })

  it('R4-10: throws invalid_token when sub claim is missing', async () => {
    // Use null to explicitly omit sub from the payload
    const fn = makeJwtVerifyFn({ sub: null })
    await expect(verifyClerkToken('no-sub.jwt', fakeEnv, fn)).rejects.toMatchObject({
      code: 'invalid_token',
      message: 'Token missing sub claim',
    })
  })

  it('R4-10: uses sid as grantId, defaults to "unknown" if sid is omitted', async () => {
    const ctxWithSid = await verifyClerkToken('jwt', fakeEnv, makeJwtVerifyFn({ sid: 'grant_abc' }))
    expect(ctxWithSid.grantId).toBe('grant_abc')

    // null = omit sid from payload entirely → grantId defaults to 'unknown'
    const ctxNoSid = await verifyClerkToken('jwt', fakeEnv, makeJwtVerifyFn({ sid: null }))
    expect(ctxNoSid.grantId).toBe('unknown')
  })
})

describe('TokenVerificationError', () => {
  it('is an instance of Error', () => {
    const err = new TokenVerificationError('invalid_token', 'Token expired')
    expect(err).toBeInstanceOf(Error)
    expect(err.code).toBe('invalid_token')
    expect(err.message).toBe('Token expired')
  })

  it('has well-known error codes', () => {
    expect(new TokenVerificationError('invalid_token', 'x').code).toBe('invalid_token')
    expect(new TokenVerificationError('expired_token', 'x').code).toBe('expired_token')
    expect(new TokenVerificationError('missing_token', 'x').code).toBe('missing_token')
  })
})

describe('ClerkTokenVerifier.extractBearerToken', () => {
  it('extracts token from valid Authorization header', () => {
    const token = ClerkTokenVerifier.extractBearerToken('Bearer my-token-value')
    expect(token).toBe('my-token-value')
  })

  it('throws TokenVerificationError for missing header', () => {
    expect(() => ClerkTokenVerifier.extractBearerToken(undefined)).toThrow(TokenVerificationError)
  })

  it('throws for non-Bearer scheme', () => {
    expect(() => ClerkTokenVerifier.extractBearerToken('Basic abc123')).toThrow(
      TokenVerificationError,
    )
  })

  it('throws for empty Bearer value', () => {
    expect(() => ClerkTokenVerifier.extractBearerToken('Bearer ')).toThrow(TokenVerificationError)
  })

  it('throws for malformed header', () => {
    expect(() => ClerkTokenVerifier.extractBearerToken('notvalid')).toThrow(TokenVerificationError)
  })
})

describe('AuthContext', () => {
  it('exposes clerkUserId, grantId, scopes', () => {
    const ctx = new AuthContext({
      clerkUserId: 'user_abc',
      grantId: 'grant_xyz',
      scopes: ['email', 'profile'],
      tokenExp: 9999999999,
    })
    expect(ctx.clerkUserId).toBe('user_abc')
    expect(ctx.grantId).toBe('grant_xyz')
    expect(ctx.scopes).toEqual(['email', 'profile'])
    expect(ctx.tokenExp).toBe(9999999999)
  })

  it('isExpired returns false for future expiry', () => {
    const ctx = new AuthContext({
      clerkUserId: 'u',
      grantId: 'g',
      scopes: [],
      tokenExp: Math.floor(Date.now() / 1000) + 3600,
    })
    expect(ctx.isExpired()).toBe(false)
  })

  it('isExpired returns true for past expiry', () => {
    const ctx = new AuthContext({
      clerkUserId: 'u',
      grantId: 'g',
      scopes: [],
      tokenExp: Math.floor(Date.now() / 1000) - 60,
    })
    expect(ctx.isExpired()).toBe(true)
  })
})
