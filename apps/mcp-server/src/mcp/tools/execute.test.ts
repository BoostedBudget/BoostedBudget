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

    const { toolResult, auditEvents } = await handler({
      calls: [{ operationId: 'getTransactions', params: { limit: 5 } }],
    })

    expect(toolResult.content[0]?.type).toBe('text')
    expect(proxy.invoke).toHaveBeenCalledTimes(1)
    expect(proxy.invoke).toHaveBeenCalledWith(
      'getTransactions',
      expect.any(Object),
      expect.any(Object),
    )
    // Audit: read op logged correctly
    expect(auditEvents).toHaveLength(1)
    expect(auditEvents[0]?.operationId).toBe('getTransactions')
    expect(auditEvents[0]?.operationClass).toBe('read')
    expect(auditEvents[0]?.result).toBe('success')
  })

  it('returns confirmation challenge for write operations without token', async () => {
    const proxy = makeFailingProxy('confirmation_required', 'Requires confirmation', 'conf-token-xyz')
    const handler = buildExecuteHandler(proxy)

    const { toolResult, auditEvents } = await handler({
      calls: [{ operationId: 'updateTransactionCategory', pathParams: { id: 'txn-1' }, body: { categoryId: 'cat-1' } }],
    })

    expect(toolResult.content[0]?.type).toBe('text')
    expect(toolResult.content[0]?.text).toContain('confirmation')
    expect(toolResult.content[0]?.text).toContain('conf-token-xyz')
    // Audit: confirmation challenge logged correctly (NOT as 'read' + 'success')
    expect(auditEvents).toHaveLength(1)
    expect(auditEvents[0]?.operationId).toBe('updateTransactionCategory')
    expect(auditEvents[0]?.operationClass).toBe('write')
    expect(auditEvents[0]?.result).toBe('confirmation_required')
  })

  it('proceeds with write when confirmation token is provided — audits as write/success', async () => {
    const proxy = makeProxy({ success: true })
    const handler = buildExecuteHandler(proxy)

    const { toolResult, auditEvents } = await handler({
      calls: [{ operationId: 'updateTransactionCategory', pathParams: { id: 'txn-1' }, body: { categoryId: 'cat-1' } }],
      confirmationToken: 'valid-token',
    })

    expect(toolResult.content[0]?.type).toBe('text')
    expect(proxy.invoke).toHaveBeenCalledWith(
      'updateTransactionCategory',
      expect.any(Object),
      expect.objectContaining({ confirmationToken: 'valid-token' }),
    )
    // Audit: confirmed write logged as write/success (was previously logged as read/success — the bug)
    expect(auditEvents).toHaveLength(1)
    expect(auditEvents[0]?.operationId).toBe('updateTransactionCategory')
    expect(auditEvents[0]?.operationClass).toBe('write')
    expect(auditEvents[0]?.result).toBe('success')
  })

  it('returns denied error for excluded operations', async () => {
    const proxy = makeFailingProxy('excluded', "Operation 'checkRole' is not available via MCP")
    const handler = buildExecuteHandler(proxy)

    const { toolResult } = await handler({
      calls: [{ operationId: 'checkRole' }],
    })

    expect(toolResult.content[0]?.text).toContain('not available')
  })

  it('executes multiple operations in sequence — emits one audit event per call', async () => {
    const proxy = makeProxy({ ok: true })
    const handler = buildExecuteHandler(proxy)

    const calls: ExecuteCall[] = [
      { operationId: 'getTransactions' },
      { operationId: 'getAccounts' },
      { operationId: 'getCashFlowSummary', params: { start_date: '2024-01-01', end_date: '2024-01-31' } },
    ]

    const { toolResult, auditEvents } = await handler({ calls })

    expect(proxy.invoke).toHaveBeenCalledTimes(3)
    expect(toolResult.content).toHaveLength(1) // results combined into one text block
    expect(auditEvents).toHaveLength(3)
    expect(auditEvents.every((e) => e.result === 'success')).toBe(true)
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

    const { toolResult, auditEvents } = await handler({
      calls: [
        { operationId: 'getTransactions' },
        { operationId: 'getAccounts' },
        { operationId: 'getCashFlowSummary', params: { start_date: '2024-01-01', end_date: '2024-01-31' } },
      ],
    })

    expect(callCount).toBe(2) // stops after the second call fails
    expect(toolResult.content[0]?.text).toContain('error')
    // Audit: first call success, second call error
    expect(auditEvents).toHaveLength(2)
    expect(auditEvents[0]?.result).toBe('success')
    expect(auditEvents[1]?.result).toBe('error')
  })

  it('returns error for empty calls array', async () => {
    const proxy = makeProxy()
    const handler = buildExecuteHandler(proxy)

    const { toolResult, auditEvents } = await handler({ calls: [] })
    expect(toolResult.content[0]?.text).toContain('No operations')
    expect(auditEvents).toHaveLength(0)
  })
})
