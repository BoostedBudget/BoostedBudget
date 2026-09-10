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

  // ── Confirmation token — concurrent consume race (R5-5) ─────────────────

  it('burns token before async work — concurrent second consume fails (R5-5)', async () => {
    const proxy = createHostProxy({ mutator: makeMutator(), userId: 'user-concurrent', grantId: 'grant-concurrent' })
    const token = await proxy.invoke(ALLOWED_WRITE_OP, {}, {}).then(
      () => { throw new Error('expected confirmation_required') },
      (err: { code: string; confirmationToken: string }) => err.confirmationToken,
    )
    const [result1, result2] = await Promise.allSettled([
      proxy.invoke(ALLOWED_WRITE_OP, {}, { confirmationToken: token }),
      proxy.invoke(ALLOWED_WRITE_OP, {}, { confirmationToken: token }),
    ])
    const successes = [result1, result2].filter((r) => r.status === 'fulfilled').length
    const failures = [result1, result2].filter((r) => r.status === 'rejected').length
    expect(successes).toBe(1)
    expect(failures).toBe(1)
  })

  // ── Idempotency key — challenge→confirm round-trip (R5-20) ───────────────

  it('R5-20: forwards same Idempotency-Key on confirmed write as was minted at challenge time', async () => {
    const capturedOpts: Array<{ headers?: Record<string, string> }> = []
    const capturingMutator: FetchMutator = mock((_config, opts) => {
      if (opts) capturedOpts.push(opts as { headers?: Record<string, string> })
      return Promise.resolve({})
    }) as unknown as FetchMutator

    const proxy = createHostProxy({ mutator: capturingMutator })
    const challenge = await proxy.invoke(ALLOWED_WRITE_OP, {}).catch((e) => e) as HostProxyError
    expect(challenge.code).toBe('confirmation_required')
    const token = challenge.confirmationToken!

    await proxy.invoke(ALLOWED_WRITE_OP, {}, { confirmationToken: token })

    expect(capturedOpts).toHaveLength(1)
    const idempotencyKey = capturedOpts[0]?.headers?.['Idempotency-Key']
    expect(idempotencyKey).toBeDefined()
    expect(typeof idempotencyKey).toBe('string')
    expect(idempotencyKey!.length).toBeGreaterThan(0)
  })

  // #1630: `previewCategoryMatches` is now admitted into readExecute as an acknowledged
  // safe-POST. readExecute never passes an `idempotencyKey` opt (mcp/tools/execute.ts calls
  // `proxy.invoke(operationId, args, { confirmationToken })` only), and
  // `confirmationRequired: false` (from the `read()` manifest helper) means the
  // `entry.confirmationRequired` branch above — the ONLY place that mints/consumes an
  // idempotency key — never runs for it. So `confirmedIdempotencyKey` stays `undefined` and
  // `effectiveIdempotencyKey` falls through to `opts.idempotencyKey`, also undefined: no
  // `Idempotency-Key` header is ever attached to a previewCategoryMatches call. Proven here
  // against the real host-proxy + real manifest, not asserted from reading the code alone.
  it('#1630: an admitted safe-POST (previewCategoryMatches) never carries an Idempotency-Key header', async () => {
    const capturedOpts: Array<{ headers?: Record<string, string> } | undefined> = []
    const capturingMutator: FetchMutator = mock((_config, opts) => {
      capturedOpts.push(opts as { headers?: Record<string, string> } | undefined)
      return Promise.resolve({ transaction_count: 0, total_amount: 0 })
    }) as unknown as FetchMutator

    const proxy = createHostProxy({ mutator: capturingMutator })
    await proxy.invoke('previewCategoryMatches', { pfc_primary: 'FOOD_AND_DRINK' })

    expect(capturedOpts).toHaveLength(1)
    expect(capturedOpts[0]?.headers?.['Idempotency-Key']).toBeUndefined()
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

// ── #1625: batch confirmation, tested at the PROXY boundary ──────────────────────────────
//
// Codex gap analysis P5: the multi-write scenario was only covered through execute.ts, so the
// proxy's own contract — the thing that actually enforces user/grant binding, one-time slot use
// and batch identity — had no direct test. These exercise confirmBatch/invoke directly.
describe('confirmBatch (#1625)', () => {
  const A = { operationId: ALLOWED_WRITE_OP, args: { url: '/a', method: 'POST', data: { n: 1 } } }
  const B = { operationId: ALLOWED_WRITE_OP, args: { url: '/b', method: 'POST', data: { n: 2 } } }

  it('issues ONE token covering every gated call in the batch', async () => {
    const proxy = createHostProxy({ mutator: makeMutator(), userId: 'u1', grantId: 'g1' })
    const decision = await proxy.confirmBatch([A, B])
    expect(decision.needsConfirmation).toBe(true)
    expect(decision.count).toBe(2)
    expect(decision.token).toBeDefined()
  })

  it('lets BOTH writes through on the confirmed replay — the bug that made a batch uncompletable', async () => {
    const mutator = makeMutator()
    const proxy = createHostProxy({ mutator, userId: 'u1', grantId: 'g1' })
    const { token } = await proxy.confirmBatch([A, B])

    await expect(proxy.confirmBatch([A, B], token)).resolves.toMatchObject({ needsConfirmation: false })
    await expect(proxy.invoke(A.operationId, A.args, { confirmationToken: token! })).resolves.toBeDefined()
    await expect(proxy.invoke(B.operationId, B.args, { confirmationToken: token! })).resolves.toBeDefined()
  })

  it('consumes each slot exactly once, so a call cannot be replayed within the batch', async () => {
    const proxy = createHostProxy({ mutator: makeMutator(), userId: 'u1', grantId: 'g1' })
    const { token } = await proxy.confirmBatch([A, B])
    await proxy.confirmBatch([A, B], token)

    await proxy.invoke(A.operationId, A.args, { confirmationToken: token! })
    const err = await proxy.invoke(A.operationId, A.args, { confirmationToken: token! }).catch((e) => e)
    expect((err as HostProxyError).code).toBe('invalid_confirmation')
  })

  // #1625 Task 4: the token must cover the WHOLE batch, not just its gated subset.
  //
  // hashBatch hashed only the gated calls, while the challenge message and the invalid-token
  // message both told the agent to replay "the IDENTICAL calls array". So a confirmed batch could
  // swap, add or drop any ungated read and the token still validated -- the enforced invariant was
  // narrower than the stated one. Low practical risk (reads are class `read` and non-destructive)
  // but it is the same defect shape as everything else in this ticket: a promise wider than the
  // guarantee.
  const READ = { operationId: ALLOWED_READ_OP, args: { url: '/txns', method: 'GET' } }
  const OTHER_READ = { operationId: ALLOWED_READ_OP, args: { url: '/other', method: 'GET' } }

  it('refuses a confirmed batch whose UNGATED read was swapped', async () => {
    const proxy = createHostProxy({ mutator: makeMutator(), userId: 'u1', grantId: 'g1' })
    const { token } = await proxy.confirmBatch([READ, A])

    const err = await proxy.confirmBatch([OTHER_READ, A], token).catch((e) => e)
    expect((err as HostProxyError).code).toBe('invalid_confirmation')
  })

  it('refuses a confirmed batch that gained an ungated read', async () => {
    const proxy = createHostProxy({ mutator: makeMutator(), userId: 'u1', grantId: 'g1' })
    const { token } = await proxy.confirmBatch([A])

    const err = await proxy.confirmBatch([READ, A], token).catch((e) => e)
    expect((err as HostProxyError).code).toBe('invalid_confirmation')
  })

  it('still admits the unchanged batch, reads included', async () => {
    const proxy = createHostProxy({ mutator: makeMutator(), userId: 'u1', grantId: 'g1' })
    const { token } = await proxy.confirmBatch([READ, A])

    await expect(proxy.confirmBatch([READ, A], token)).resolves.toMatchObject({ needsConfirmation: false })
  })

  it('counts only the GATED calls, so the agent is not told a read needs confirming', async () => {
    const proxy = createHostProxy({ mutator: makeMutator(), userId: 'u1', grantId: 'g1' })
    const decision = await proxy.confirmBatch([READ, A, B])
    expect(decision.count).toBe(2)
  })

  it('refuses a token issued for a DIFFERENT batch', async () => {
    const proxy = createHostProxy({ mutator: makeMutator(), userId: 'u1', grantId: 'g1' })
    const { token } = await proxy.confirmBatch([A, B])

    const err = await proxy.confirmBatch([A], token).catch((e) => e)
    expect((err as HostProxyError).code).toBe('invalid_confirmation')
  })

  it('refuses a batch token replayed by a different user, and by a different grant', async () => {
    const proxy = createHostProxy({ mutator: makeMutator(), userId: 'u1', grantId: 'g1' })
    const { token } = await proxy.confirmBatch([A, B])

    const otherUser = createHostProxy({ mutator: makeMutator(), userId: 'u2', grantId: 'g1' })
    const otherGrant = createHostProxy({ mutator: makeMutator(), userId: 'u1', grantId: 'g2' })

    expect((await otherUser.confirmBatch([A, B], token).catch((e) => e) as HostProxyError).code)
      .toBe('invalid_confirmation')
    expect((await otherGrant.confirmBatch([A, B], token).catch((e) => e) as HostProxyError).code)
      .toBe('invalid_confirmation')
  })

  it('names args-mismatch in the message, so an agent can self-correct instead of looping', async () => {
    const proxy = createHostProxy({ mutator: makeMutator(), userId: 'u1', grantId: 'g1' })
    const err = await proxy.confirmBatch([A], 'not-a-real-token').catch((e) => e)
    expect((err as HostProxyError).message.toLowerCase()).toContain('identical')
  })

  it('reports no confirmation needed for a batch with no gated calls', async () => {
    const proxy = createHostProxy({ mutator: makeMutator(), userId: 'u1', grantId: 'g1' })
    await expect(proxy.confirmBatch([])).resolves.toMatchObject({ needsConfirmation: false, count: 0 })
  })
})
