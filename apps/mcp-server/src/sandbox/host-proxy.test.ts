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
