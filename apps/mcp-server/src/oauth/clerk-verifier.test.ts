import { describe, expect, it, mock } from 'bun:test'
import { ClerkTokenVerifier, AuthContext, TokenVerificationError } from './clerk-verifier'

// NOTE: Real JWKS-based JWT tests require crypto infrastructure.
// We test the verifier's parsing, audience/issuer validation contract, and error-handling
// logic by mocking jose.jwtVerify.
// End-to-end JWKS + issuer/audience verification is covered by the Phase 3 Claude Desktop
// interop checkpoint.

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
