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

  it('returns denied error for excluded operations and emits a denied audit event (R5-9)', async () => {
    const proxy = makeFailingProxy('excluded', "Operation 'checkRole' is not available via MCP")
    const handler = buildExecuteHandler(proxy)

    const { toolResult, auditEvents } = await handler({
      calls: [{ operationId: 'checkRole' }],
    })

    expect(toolResult.content[0]?.text).toContain('not available')
    // A denied/excluded op must produce an audit event so the trail is complete.
    expect(auditEvents).toHaveLength(1)
    expect(auditEvents[0]).toMatchObject({ operationId: 'checkRole', result: 'denied' })
  })

  // ── The denied-call audit class (round 14, finding CC-1) ────────────────────
  //
  // `operationClass` used to be computed from `capability?.confirmationRequired`
  // BEFORE the excluded check and then reused INSIDE it. Every excluded entry is
  // built by capability-manifest.ts's `excluded()` helper, which hard-codes
  // `confirmationRequired: false` — so a denied `deleteAccount` or `grantTrial`
  // persisted an `mcp_audit_log` row saying `operation_class = 'read'`. The call
  // never runs (host-proxy.ts throws on `entry.excluded` before any gate), so the
  // harm is confined to the audit trail — but the audit trail is the whole point
  // of recording a denial, and it was wrong about the one field an operator would
  // filter on when asking "did anyone try to reach a mutating admin operation?".
  //
  // ⚠ This is NOT the sibling defect. The spec index's `manifestClass` field is
  // ALSO 'read' on 45 mutating excluded entries, from the same default block. That
  // is a different field with no consumer today and is deliberately left alone.
  const DENIAL_SPEC_ENTRIES = [
    { operationId: 'deleteAccount', method: 'DELETE', path: '/api/v1/accounts/{id}' },
    { operationId: 'grantTrial', method: 'POST', path: '/api/v1/admin/subscription/grant-trial' },
    { operationId: 'checkRole', method: 'GET', path: '/api/v1/admin/check-role' },
  ]

  it.each([
    ['deleteAccount', 'DELETE'],
    ['grantTrial', 'POST'],
  ])('audits a denied mutating operation (%s, %s) as write, not read', async (operationId) => {
    const proxy = makeFailingProxy('excluded', `Operation '${operationId}' is not available via MCP`)
    const handler = buildExecuteHandler(proxy, DENIAL_SPEC_ENTRIES)

    const { auditEvents } = await handler({ calls: [{ operationId }] })

    expect(auditEvents).toHaveLength(1)
    expect(auditEvents[0]?.result).toBe('denied')
    expect(auditEvents[0]?.operationClass).not.toBe('read')
    expect(auditEvents[0]?.operationClass).toBe('write')
    // The deny path itself must be untouched: nothing may be invoked.
    expect(proxy.invoke).not.toHaveBeenCalled()
  })

  // Paired control for THIS claim, not for the tool: the fix must classify the
  // denial from the operation's real HTTP method, so an excluded READ still
  // records 'read'. Without this, "always write" would pass the assertion above
  // while being just as dishonest in the other direction.
  it('audits a denied read-only operation as read', async () => {
    const proxy = makeFailingProxy('excluded', "Operation 'checkRole' is not available via MCP")
    const handler = buildExecuteHandler(proxy, DENIAL_SPEC_ENTRIES)

    const { auditEvents } = await handler({ calls: [{ operationId: 'checkRole' }] })

    expect(auditEvents).toHaveLength(1)
    expect(auditEvents[0]).toMatchObject({ operationId: 'checkRole', operationClass: 'read', result: 'denied' })
  })

  // An operationId that is in NO manifest and NO spec index — a hallucinated or
  // misspelled name. There is no evidence either way, so the denial records the
  // class that gets an operator's attention rather than the one that hides.
  it('audits a denial for an unknown operationId as write', async () => {
    const proxy = makeProxy()
    const handler = buildExecuteHandler(proxy, DENIAL_SPEC_ENTRIES)

    const { auditEvents } = await handler({ calls: [{ operationId: 'notAnOperation' }] })

    expect(auditEvents).toHaveLength(1)
    expect(auditEvents[0]).toMatchObject({ operationId: 'notAnOperation', operationClass: 'write', result: 'denied' })
  })

  // The mandatory sibling: fixing the denied branch must not disturb the ordinary
  // permitted-read branch, which still takes its class from the manifest.
  it('still audits an ordinary permitted read as read', async () => {
    const proxy = makeProxy({ transactions: [] })
    const handler = buildExecuteHandler(proxy, DENIAL_SPEC_ENTRIES)

    const { auditEvents } = await handler({ calls: [{ operationId: 'getTransactions' }] })

    expect(auditEvents).toHaveLength(1)
    expect(auditEvents[0]).toMatchObject({ operationId: 'getTransactions', operationClass: 'read', result: 'success' })
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
