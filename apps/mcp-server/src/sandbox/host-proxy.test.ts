import { describe, expect, it, mock, beforeEach } from 'bun:test'
import { createHostProxy, HostProxyError } from './host-proxy'
import type { FetchMutator } from '../client/fetch-mutator'
import { ApiError } from '../client/fetch-mutator'

const ALLOWED_READ_OP = 'getTransactions'
const ALLOWED_WRITE_OP = 'updateTransactionCategory'
const EXCLUDED_OP = 'checkRole'
const UNKNOWN_OP = 'totallyFakeOperation'

function makeMutator(resolveWith: unknown = { transactions: [] }): FetchMutator {
  return mock(() => Promise.resolve(resolveWith)) as unknown as FetchMutator
}

describe('createHostProxy', () => {
  let mutator: FetchMutator

  beforeEach(() => {
    mutator = makeMutator()
  })

  // ── Manifest enforcement ────────────────────────────────────────────────

  it('denies unknown operationId (deny-by-default)', async () => {
    const proxy = createHostProxy({ mutator })
    await expect(proxy.invoke(UNKNOWN_OP, {})).rejects.toBeInstanceOf(HostProxyError)
  })

  it('denies excluded operationId', async () => {
    const proxy = createHostProxy({ mutator })
    await expect(proxy.invoke(EXCLUDED_OP, {})).rejects.toBeInstanceOf(HostProxyError)
  })

  it('allows allowed read operationId', async () => {
    const proxy = createHostProxy({ mutator })
    await expect(proxy.invoke(ALLOWED_READ_OP, {})).resolves.toBeDefined()
  })

  // ── Write gating ─────────────────────────────────────────────────────────

  it('throws ConfirmationRequired for write ops without confirmation token', async () => {
    const proxy = createHostProxy({ mutator })
    const err = await proxy.invoke(ALLOWED_WRITE_OP, {}).catch((e) => e)
    expect(err).toBeInstanceOf(HostProxyError)
    expect((err as HostProxyError).code).toBe('confirmation_required')
  })

  it('allows write op when valid confirmation token is provided', async () => {
    const proxy = createHostProxy({ mutator })
    // First call: get the confirmation challenge
    const challenge = await proxy.invoke(ALLOWED_WRITE_OP, {}).catch((e) => e) as HostProxyError
    expect(challenge.code).toBe('confirmation_required')
    expect(challenge.confirmationToken).toBeDefined()

    // Second call: provide the confirmation token
    await expect(
      proxy.invoke(ALLOWED_WRITE_OP, {}, { confirmationToken: challenge.confirmationToken! }),
    ).resolves.toBeDefined()
  })

  it('rejects reused confirmation token (one-time use)', async () => {
    const proxy = createHostProxy({ mutator })
    const challenge = await proxy.invoke(ALLOWED_WRITE_OP, {}).catch((e) => e) as HostProxyError
    const token = challenge.confirmationToken!

    // First use succeeds
    await proxy.invoke(ALLOWED_WRITE_OP, {}, { confirmationToken: token })

    // Second use with same (consumed) token is an invalid/replay attempt
    const err = await proxy.invoke(ALLOWED_WRITE_OP, {}, { confirmationToken: token }).catch((e) => e)
    expect((err as HostProxyError).code).toBe('invalid_confirmation')
  })

  // ── Confirmation token — user/grant binding ──────────────────────────────

  it('rejects confirmation token replayed by a different user (cross-user replay)', async () => {
    // userA gets a confirmation token for their write
    const proxyA = createHostProxy({ mutator: makeMutator(), userId: 'userA', grantId: 'grantA' })
    const challengeA = await proxyA.invoke(ALLOWED_WRITE_OP, {}).catch((e) => e) as HostProxyError
    expect(challengeA.code).toBe('confirmation_required')
    const token = challengeA.confirmationToken!

    // userB tries to replay userA's token — must be rejected
    const proxyB = createHostProxy({ mutator: makeMutator(), userId: 'userB', grantId: 'grantB' })
    const err = await proxyB.invoke(ALLOWED_WRITE_OP, {}, { confirmationToken: token }).catch((e) => e)
    expect((err as HostProxyError).code).toBe('invalid_confirmation')
  })

  it('rejects confirmation token replayed with a different grant (same user)', async () => {
    // Same user, different grantId (e.g. after re-authorizing)
    const proxy = createHostProxy({ mutator: makeMutator(), userId: 'userA', grantId: 'grant-old' })
    const challenge = await proxy.invoke(ALLOWED_WRITE_OP, {}).catch((e) => e) as HostProxyError
    const token = challenge.confirmationToken!

    // replay with new grantId — must be rejected
    const proxy2 = createHostProxy({ mutator: makeMutator(), userId: 'userA', grantId: 'grant-new' })
    const err = await proxy2.invoke(ALLOWED_WRITE_OP, {}, { confirmationToken: token }).catch((e) => e)
    expect((err as HostProxyError).code).toBe('invalid_confirmation')
  })

  it('accepts confirmation token for the same user+grant that issued it', async () => {
    const proxy = createHostProxy({ mutator: makeMutator(), userId: 'userA', grantId: 'grantA' })
    const challenge = await proxy.invoke(ALLOWED_WRITE_OP, {}).catch((e) => e) as HostProxyError
    const token = challenge.confirmationToken!
    await expect(
      proxy.invoke(ALLOWED_WRITE_OP, {}, { confirmationToken: token }),
    ).resolves.toBeDefined()
  })

  // ── Confirmation token — TOCTOU + TTL ────────────────────────────────────

  it('R4-11: rejects confirmation token when args differ (TOCTOU protection)', async () => {
    const argsA = { url: '/api/v1/budgets', method: 'POST', data: { name: 'Budget A' } }
    const argsB = { url: '/api/v1/budgets', method: 'POST', data: { name: 'Budget B (different)' } }
    const proxy = createHostProxy({ mutator: makeMutator(), userId: 'userA', grantId: 'grantA' })

    // Get a challenge for argsA
    const challengeA = await proxy.invoke(ALLOWED_WRITE_OP, argsA).catch((e) => e) as HostProxyError
    const token = challengeA.confirmationToken!

    // Replay the token with DIFFERENT args (budget name changed)
    const err = await proxy.invoke(ALLOWED_WRITE_OP, argsB, { confirmationToken: token }).catch((e) => e)
    expect((err as HostProxyError).code).toBe('invalid_confirmation')
  })

  it('R4-11: rejects confirmation token after TTL expiry (injectable clock)', async () => {
    let fakeNow = Date.now()
    const proxy = createHostProxy({
      mutator: makeMutator(),
      userId: 'userA',
      grantId: 'grantA',
      getNow: () => fakeNow,
    })

    const challenge = await proxy.invoke(ALLOWED_WRITE_OP, {}).catch((e) => e) as HostProxyError
    const token = challenge.confirmationToken!

    // Advance time past the 5-minute TTL
    fakeNow += 5 * 60 * 1000 + 1 // 5 min + 1ms

    const err = await proxy.invoke(ALLOWED_WRITE_OP, {}, { confirmationToken: token }).catch((e) => e)
    expect((err as HostProxyError).code).toBe('invalid_confirmation')
  })

  it('R4-11: accepts confirmation token exactly at TTL boundary (not expired yet)', async () => {
    let fakeNow = Date.now()
    const proxy = createHostProxy({
      mutator: makeMutator(),
      userId: 'userA',
      grantId: 'grantA',
      getNow: () => fakeNow,
    })

    const challenge = await proxy.invoke(ALLOWED_WRITE_OP, {}).catch((e) => e) as HostProxyError
    const token = challenge.confirmationToken!

    // Advance to exactly TTL ms (not past it)
    fakeNow += 5 * 60 * 1000 // exactly 5 min — still valid

    await expect(
      proxy.invoke(ALLOWED_WRITE_OP, {}, { confirmationToken: token }),
    ).resolves.toBeDefined()
  })

  // ── Rate limiting — per-user isolation ──────────────────────────────────

  it('rate-limits per userId independently (no cross-user bleed)', async () => {
    const { createRateLimiter } = await import('../limits/rate-limit')
    // Shared limiter with maxPerMinute=1 per user
    const rateLimiter = createRateLimiter({ maxPerMinute: 1, maxConcurrent: 10 })

    // user-1 exhausts their personal limit
    const proxy1 = createHostProxy({ mutator: makeMutator(), userId: 'user-1', rateLimiter })
    await proxy1.invoke(ALLOWED_READ_OP, {})
    await expect(proxy1.invoke(ALLOWED_READ_OP, {})).rejects.toMatchObject({ code: 'denied' })

    // user-2 uses a DIFFERENT userId with the SAME shared rateLimiter instance.
    // Their budget must be independent — user-1 exhausting MUST NOT block user-2.
    const proxy2 = createHostProxy({ mutator: makeMutator(), userId: 'user-2', rateLimiter })
    await expect(proxy2.invoke(ALLOWED_READ_OP, {})).resolves.toBeDefined()
  })

  it('defaults userId to anonymous — same bucket for all anonymous callers', async () => {
    // This test documents the bug: if no userId is passed, all callers share 'anonymous'
    // Fix: always pass userId (props.clerkUserId) when constructing the proxy in agent.ts
    const { createRateLimiter } = await import('../limits/rate-limit')
    const rateLimiter = createRateLimiter({ maxPerMinute: 1, maxConcurrent: 10 })

    // Two "different users" but neither passes userId → both default to 'anonymous'
    const proxy1 = createHostProxy({ mutator: makeMutator(), rateLimiter }) // no userId
    const proxy2 = createHostProxy({ mutator: makeMutator(), rateLimiter }) // no userId

    await proxy1.invoke(ALLOWED_READ_OP, {})                           // uses 'anonymous' slot
    await expect(proxy2.invoke(ALLOWED_READ_OP, {}))                   // also 'anonymous' → BLOCKED
      .rejects.toMatchObject({ code: 'denied' })
  })

  // ── Credential injection ─────────────────────────────────────────────────

  it('injects credentials from credential provider', async () => {
    const getCredentials = mock(() =>
      Promise.resolve({ authorization: 'Bearer user-token', originSecret: 'secret' }),
    )
    const proxy = createHostProxy({ mutator, getCredentials })
    await proxy.invoke(ALLOWED_READ_OP, {})
    // The mutator should have been called — credential injection is tested via fetch-mutator
    expect(mutator).toHaveBeenCalled()
  })

  // ── Error redaction ──────────────────────────────────────────────────────

  it('redacts errors from the backend before returning to caller', async () => {
    const failingMutator = mock(() =>
      Promise.reject(new ApiError(500, { message: 'Balance $99,999 exceeded' })),
    ) as unknown as FetchMutator
    const proxy = createHostProxy({ mutator: failingMutator })

    const err = await proxy.invoke(ALLOWED_READ_OP, {}).catch((e) => e)
    expect(err).toBeInstanceOf(HostProxyError)
    const serialized = JSON.stringify(err)
    expect(serialized).not.toContain('99,999')
    expect(serialized).not.toContain('Balance')
  })
})

describe('HostProxyError', () => {
  it('has code and message', () => {
    const err = new HostProxyError('denied', 'Operation denied')
    expect(err.code).toBe('denied')
    expect(err.message).toBe('Operation denied')
    expect(err).toBeInstanceOf(Error)
  })
})
