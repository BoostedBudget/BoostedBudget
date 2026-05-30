import { describe, expect, it, beforeEach } from 'bun:test'
import { assertNotRevoked, markRevoked, clearRevocationCache } from './revocation'
import { AuthContext, TokenVerificationError } from './clerk-verifier'

function makeCtx(overrides: Partial<ConstructorParameters<typeof AuthContext>[0]> = {}): AuthContext {
  return new AuthContext({
    clerkUserId: 'user_test',
    grantId: 'grant_test',
    scopes: ['email'],
    tokenExp: Math.floor(Date.now() / 1000) + 300,
    ...overrides,
  })
}

describe('assertNotRevoked', () => {
  beforeEach(() => clearRevocationCache())

  it('does not throw for a fresh, non-revoked grant', async () => {
    const ctx = makeCtx()
    await expect(assertNotRevoked(ctx, {})).resolves.toBeUndefined()
  })

  it('throws TokenVerificationError for a manually-revoked grant', async () => {
    const ctx = makeCtx({ grantId: 'revoked_grant' })
    markRevoked('revoked_grant')
    await expect(assertNotRevoked(ctx, {})).rejects.toBeInstanceOf(TokenVerificationError)
  })

  it('error code is invalid_token for revoked grant', async () => {
    const ctx = makeCtx({ grantId: 'revoked_grant_2' })
    markRevoked('revoked_grant_2')

    let caught: TokenVerificationError | undefined
    try {
      await assertNotRevoked(ctx, {})
    } catch (e) {
      caught = e as TokenVerificationError
    }
    expect(caught?.code).toBe('invalid_token')
  })

  it('uses cached result within TTL', async () => {
    const ctx = makeCtx({ grantId: 'cached_grant' })
    // First call populates cache
    await assertNotRevoked(ctx, {})
    // Mark as revoked AFTER initial check
    markRevoked('cached_grant')
    // Should still use cached valid result (within TTL)
    await expect(assertNotRevoked(ctx, {})).rejects.toBeInstanceOf(TokenVerificationError)
  })
})

describe('markRevoked', () => {
  beforeEach(() => clearRevocationCache())

  it('marks a grant as revoked immediately', async () => {
    markRevoked('some_grant')
    const ctx = makeCtx({ grantId: 'some_grant' })
    await expect(assertNotRevoked(ctx, {})).rejects.toBeInstanceOf(TokenVerificationError)
  })
})
