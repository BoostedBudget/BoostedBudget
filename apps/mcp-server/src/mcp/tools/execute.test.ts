import { describe, expect, it, mock } from 'bun:test'
import { buildExecuteHandler, type ExecuteCall } from './execute'
import { HostProxyError } from '../../sandbox/host-proxy'

// Minimal proxy stub for testing
function makeProxy(invokeResult: unknown = {}) {
  return {
    invoke: mock(() => Promise.resolve(invokeResult)),
  }
}

function makeFailingProxy(code: string, message: string, confirmationToken?: string) {
  return {
    invoke: mock(() =>
      Promise.reject(new HostProxyError(code as 'denied', message, confirmationToken)),
    ),
  }
}

describe('buildExecuteHandler', () => {
  it('returns results for allowed read operations', async () => {
    const proxy = makeProxy({ transactions: [] })
    const handler = buildExecuteHandler(proxy)

    const result = await handler({
      calls: [{ operationId: 'getTransactions', params: { limit: 5 } }],
    })

    expect(result.content[0]?.type).toBe('text')
    expect(proxy.invoke).toHaveBeenCalledTimes(1)
    expect(proxy.invoke).toHaveBeenCalledWith(
      'getTransactions',
      expect.any(Object),
      expect.any(Object),
    )
  })

  it('returns confirmation challenge for write operations without token', async () => {
    const proxy = makeFailingProxy('confirmation_required', 'Requires confirmation', 'conf-token-xyz')
    const handler = buildExecuteHandler(proxy)

    const result = await handler({
      calls: [{ operationId: 'updateTransactionCategory', pathParams: { id: 'txn-1' }, body: { categoryId: 'cat-1' } }],
    })

    expect(result.content[0]?.type).toBe('text')
    expect(result.content[0]?.text).toContain('confirmation')
    expect(result.content[0]?.text).toContain('conf-token-xyz')
  })

  it('proceeds with write when confirmation token is provided', async () => {
    const proxy = makeProxy({ success: true })
    const handler = buildExecuteHandler(proxy)

    const result = await handler({
      calls: [{ operationId: 'updateTransactionCategory', pathParams: { id: 'txn-1' }, body: { categoryId: 'cat-1' } }],
      confirmationToken: 'valid-token',
    })

    expect(result.content[0]?.type).toBe('text')
    expect(proxy.invoke).toHaveBeenCalledWith(
      'updateTransactionCategory',
      expect.any(Object),
      expect.objectContaining({ confirmationToken: 'valid-token' }),
    )
  })

  it('returns denied error for excluded operations', async () => {
    const proxy = makeFailingProxy('excluded', "Operation 'checkRole' is not available via MCP")
    const handler = buildExecuteHandler(proxy)

    const result = await handler({
      calls: [{ operationId: 'checkRole' }],
    })

    expect(result.content[0]?.text).toContain('not available')
  })

  it('executes multiple operations in sequence', async () => {
    const proxy = makeProxy({ ok: true })
    const handler = buildExecuteHandler(proxy)

    const calls: ExecuteCall[] = [
      { operationId: 'getTransactions' },
      { operationId: 'getAccounts' },
      { operationId: 'getCashFlowSummary', params: { start_date: '2024-01-01', end_date: '2024-01-31' } },
    ]

    const result = await handler({ calls })

    expect(proxy.invoke).toHaveBeenCalledTimes(3)
    expect(result.content).toHaveLength(1) // results combined into one text block
  })

  it('stops on first error and reports it', async () => {
    let callCount = 0
    const proxy = {
      invoke: mock(() => {
        callCount++
        if (callCount === 2) {
          return Promise.reject(new HostProxyError('backend_error', 'Something went wrong'))
        }
        return Promise.resolve({})
      }),
    }
    const handler = buildExecuteHandler(proxy)

    const result = await handler({
      calls: [
        { operationId: 'getTransactions' },
        { operationId: 'getAccounts' },
        { operationId: 'getCashFlowSummary', params: { start_date: '2024-01-01', end_date: '2024-01-31' } },
      ],
    })

    expect(callCount).toBe(2) // stops after the second call fails
    expect(result.content[0]?.text).toContain('error')
  })

  it('returns error for empty calls array', async () => {
    const proxy = makeProxy()
    const handler = buildExecuteHandler(proxy)

    const result = await handler({ calls: [] })
    expect(result.content[0]?.text).toContain('No operations')
  })
})
