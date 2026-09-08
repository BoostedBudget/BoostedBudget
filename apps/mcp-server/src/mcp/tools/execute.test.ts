import { describe, expect, it, mock } from 'bun:test'
import { buildWriteExecuteHandler, buildReadExecuteHandler, type ExecuteCall } from './execute'
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

describe('buildWriteExecuteHandler', () => {
  it('returns results for allowed read operations', async () => {
    const proxy = makeProxy({ transactions: [] })
    const handler = buildWriteExecuteHandler(proxy)

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
    const handler = buildWriteExecuteHandler(proxy)

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
    const handler = buildWriteExecuteHandler(proxy)

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
    const handler = buildWriteExecuteHandler(proxy)

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
  // `confirmationRequired: false` — so a denied `purgeAccount` or `grantTrial`
  // persisted an `mcp_audit_log` row saying `operation_class = 'read'`. The call
  // never runs (host-proxy.ts throws on `entry.excluded` before any gate), so the
  // harm is confined to the audit trail — but the audit trail is the whole point
  // of recording a denial, and it was wrong about the one field an operator would
  // filter on when asking "did anyone try to reach a mutating admin operation?".
  //
  // ⚠ UPDATED by #1569 Task 16 (F1). The sibling defect this comment used to say was "deliberately
  // left alone" — the spec index's `manifestClass` being 'read' on 45 mutating excluded entries —
  // has been fixed: excluded entries now carry their true class. deniedOperationClass therefore
  // reads the manifest first, and purgeAccount audits as 'destructive' rather than merely 'write'.
  // The original claim these cases exist to defend is unchanged and strengthened: a denied
  // mutating operation must NEVER be recorded as a read.
  const DENIAL_SPEC_ENTRIES = [
    { operationId: 'purgeAccount', method: 'DELETE', path: '/api/v1/accounts/{id}' },
    { operationId: 'grantTrial', method: 'POST', path: '/api/v1/admin/subscription/grant-trial' },
    { operationId: 'checkRole', method: 'GET', path: '/api/v1/admin/check-role' },
  ]

  it.each([
    ['purgeAccount', 'DELETE'],
    ['grantTrial', 'POST'],
  ])('audits a denied mutating operation (%s, %s) with its true class, never read', async (operationId) => {
    const proxy = makeFailingProxy('excluded', `Operation '${operationId}' is not available via MCP`)
    const handler = buildWriteExecuteHandler(proxy, DENIAL_SPEC_ENTRIES)

    const { auditEvents } = await handler({ calls: [{ operationId }] })

    expect(auditEvents).toHaveLength(1)
    expect(auditEvents[0]?.result).toBe('denied')
    expect(auditEvents[0]?.operationClass).not.toBe('read')
    // purgeAccount hard-deletes accounts + balance_snapshots, grantTrial does not destroy: the
    // audit row now distinguishes them instead of flattening both to 'write'.
    expect(auditEvents[0]?.operationClass).toBe(operationId === 'purgeAccount' ? 'destructive' : 'write')
    // The deny path itself must be untouched: nothing may be invoked.
    expect(proxy.invoke).not.toHaveBeenCalled()
  })

  // Paired control for THIS claim, not for the tool: the fix must classify the
  // denial from the operation's real HTTP method, so an excluded READ still
  // records 'read'. Without this, "always write" would pass the assertion above
  // while being just as dishonest in the other direction.
  it('audits a denied read-only operation as read', async () => {
    const proxy = makeFailingProxy('excluded', "Operation 'checkRole' is not available via MCP")
    const handler = buildWriteExecuteHandler(proxy, DENIAL_SPEC_ENTRIES)

    const { auditEvents } = await handler({ calls: [{ operationId: 'checkRole' }] })

    expect(auditEvents).toHaveLength(1)
    expect(auditEvents[0]).toMatchObject({ operationId: 'checkRole', operationClass: 'read', result: 'denied' })
  })

  // An operationId that is in NO manifest and NO spec index — a hallucinated or
  // misspelled name. There is no evidence either way, so the denial records the
  // class that gets an operator's attention rather than the one that hides.
  it('audits a denial for an unknown operationId as write', async () => {
    const proxy = makeProxy()
    const handler = buildWriteExecuteHandler(proxy, DENIAL_SPEC_ENTRIES)

    const { auditEvents } = await handler({ calls: [{ operationId: 'notAnOperation' }] })

    expect(auditEvents).toHaveLength(1)
    expect(auditEvents[0]).toMatchObject({ operationId: 'notAnOperation', operationClass: 'write', result: 'denied' })
  })

  // The mandatory sibling: fixing the denied branch must not disturb the ordinary
  // permitted-read branch, which still takes its class from the manifest.
  it('still audits an ordinary permitted read as read', async () => {
    const proxy = makeProxy({ transactions: [] })
    const handler = buildWriteExecuteHandler(proxy, DENIAL_SPEC_ENTRIES)

    const { auditEvents } = await handler({ calls: [{ operationId: 'getTransactions' }] })

    expect(auditEvents).toHaveLength(1)
    expect(auditEvents[0]).toMatchObject({ operationId: 'getTransactions', operationClass: 'read', result: 'success' })
  })

  it('executes multiple operations in sequence — emits one audit event per call', async () => {
    const proxy = makeProxy({ ok: true })
    const handler = buildWriteExecuteHandler(proxy)

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
    const handler = buildWriteExecuteHandler(proxy)

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
    const handler = buildWriteExecuteHandler(proxy)

    const { toolResult, auditEvents } = await handler({ calls: [] })
    expect(toolResult.content[0]?.text).toContain('No operations')
    expect(auditEvents).toHaveLength(0)
  })

  // ── Result truncation must be explicit, not silent (issue #1609, item 8) ──────
  //
  // Production impact (2026-09-07): a user asked for recent transactions; the
  // client received a silently-truncated result, couldn't see merchant names or
  // the reversal field (both sat in the discarded portion), and fell back to
  // reconstructing the data from analytics endpoints — reporting a confident
  // answer built on a fragment. A visible truncation marker turns that into an
  // explicit "I received partial data" instead of a silent falsehood.
  describe('result truncation', () => {
    it('truncates an oversized result and marks it explicitly with shown/full sizes', async () => {
      // Build an object whose JSON is comfortably larger than the cap.
      const bigArray = Array.from({ length: 5000 }, (_, i) => ({
        id: `txn-${i}`,
        merchant: 'Some Merchant Name',
        amount: -12.34,
        reversal: false,
      }))
      const proxy = makeProxy({ transactions: bigArray })
      const handler = buildWriteExecuteHandler(proxy)

      const { toolResult } = await handler({
        calls: [{ operationId: 'getTransactions' }],
      })

      const text = toolResult.content[0]?.text ?? ''
      const fullLength = JSON.stringify({ transactions: bigArray }).length

      expect(text).toContain('truncated')
      expect(text).toContain(String(fullLength))
    })

    it('reports the ACTUAL full size, not a hardcoded or cap-derived number', async () => {
      const bigArray = Array.from({ length: 3000 }, (_, i) => ({ id: `txn-${i}`, note: 'x'.repeat(50) }))
      const proxy = makeProxy({ items: bigArray })
      const handler = buildWriteExecuteHandler(proxy)

      const { toolResult } = await handler({
        calls: [{ operationId: 'getTransactions' }],
      })

      const text = toolResult.content[0]?.text ?? ''
      const fullLength = JSON.stringify({ items: bigArray }).length

      // The marker must cite the real length of THIS payload, not a fixed number
      // a lazy implementation might hardcode into the message.
      expect(text).toContain(String(fullLength))
      expect(fullLength).toBeGreaterThan(100_000)
    })

    it('leaves a small result byte-identical to today\'s output — no marker', async () => {
      const small = { transactions: [{ id: 'txn-1', merchant: 'Coffee Shop', amount: -4.5 }] }
      const proxy = makeProxy(small)
      const handler = buildWriteExecuteHandler(proxy)

      const { toolResult } = await handler({
        calls: [{ operationId: 'getTransactions' }],
      })

      const text = toolResult.content[0]?.text ?? ''
      expect(text).toBe(`✅ getTransactions: ${JSON.stringify(small)}`)
      expect(text).not.toContain('truncated')
    })

    it('does not mark a result exactly at the cap as truncated', async () => {
      // Construct a value whose JSON.stringify length is EXACTLY MAX_RESULT_CHARS,
      // by measuring and then adjusting one character at a time.
      const target = 100_000
      let payload = { s: '' }
      let str = JSON.stringify(payload)
      const padLength = target - str.length
      payload = { s: 'a'.repeat(Math.max(0, padLength)) }
      str = JSON.stringify(payload)
      // Adjust by +/-1 in case of off-by-one from the padding math above.
      while (str.length < target) {
        payload.s += 'a'
        str = JSON.stringify(payload)
      }
      while (str.length > target) {
        payload.s = payload.s.slice(0, -1)
        str = JSON.stringify(payload)
      }
      expect(str.length).toBe(target)

      const proxy = makeProxy(payload)
      const handler = buildWriteExecuteHandler(proxy)

      const { toolResult } = await handler({
        calls: [{ operationId: 'getTransactions' }],
      })

      const text = toolResult.content[0]?.text ?? ''
      expect(text).toBe(`✅ getTransactions: ${str}`)
      expect(text).not.toContain('truncated')
    })
  })
})

// ── Issue #1610b: readExecute's three-way admission gate ────────────────────
//
// THE SECURITY POINT: `capability.class === 'read'` alone is not a safe gate. Two REAL
// counter-examples in this manifest prove each single-field check is bypassable on its own:
//   - `cancelDeletion` is `excluded()`, which hard-codes `class: 'read'` regardless of the
//     operation's real behaviour — GET /api/v1/user/delete/cancel/{token} is genuinely
//     state-changing (deletion_service.go:373-375). Blocked today only by `excluded`, not by
//     `class`.
//   - `runMCPRecipe` is REAL, non-excluded, and invocable: `read('runMCPRecipe')` gives it
//     class 'read' AND confirmationRequired: false (honestly — it's read('...')-built), yet its
//     HTTP method is POST. class+confirmationRequired alone would wrongly admit it; only the
//     method check catches it.
// readExecute must therefore require ALL THREE: class === 'read', method is read-only, AND
// confirmationRequired === false.
describe('buildReadExecuteHandler — the three-way admission gate (#1610b)', () => {
  const READ_EXECUTE_SPEC_ENTRIES = [
    { operationId: 'getTransactions', method: 'GET', path: '/api/v1/transactions' },
    { operationId: 'updateTransactionCategory', method: 'PATCH', path: '/api/v1/transactions/{id}/category' },
    { operationId: 'runMCPRecipe', method: 'POST', path: '/api/v1/mcp/recipes/{id}/run' },
  ]

  it('admits a real read-only, non-confirmation-gated GET operation', async () => {
    const proxy = makeProxy({ transactions: [] })
    const handler = buildReadExecuteHandler(proxy, READ_EXECUTE_SPEC_ENTRIES)

    const { toolResult, auditEvents } = await handler({
      calls: [{ operationId: 'getTransactions', params: { limit: 5 } }],
    })

    expect(toolResult.content[0]?.text).toContain('getTransactions')
    expect(proxy.invoke).toHaveBeenCalledTimes(1)
    expect(auditEvents[0]).toMatchObject({ operationId: 'getTransactions', operationClass: 'read', result: 'success' })
  })

  it('REFUSES runMCPRecipe — class="read" and confirmationRequired=false are BOTH honestly true, but its method is POST', async () => {
    // The proof this gate exists: a check on class+confirmationRequired alone would admit this
    // call. It must be refused anyway, and the proxy must never be invoked.
    const proxy = makeProxy({ calls: [] })
    const handler = buildReadExecuteHandler(proxy, READ_EXECUTE_SPEC_ENTRIES)

    const { toolResult, auditEvents } = await handler({
      calls: [{ operationId: 'runMCPRecipe', pathParams: { id: 'recipe-1' } }],
    })

    expect(toolResult.content[0]?.text).toContain('runMCPRecipe')
    expect(toolResult.content[0]?.text).toContain('writeExecute')
    expect(proxy.invoke).not.toHaveBeenCalled()
    expect(auditEvents).toHaveLength(1)
    expect(auditEvents[0]).toMatchObject({ operationId: 'runMCPRecipe', result: 'denied' })
  })

  it('refuses a genuine write operation, naming writeExecute as the alternative', async () => {
    const proxy = makeProxy()
    const handler = buildReadExecuteHandler(proxy, READ_EXECUTE_SPEC_ENTRIES)

    const { toolResult, auditEvents } = await handler({
      calls: [{ operationId: 'updateTransactionCategory', pathParams: { id: 'txn-1' }, body: { categoryId: 'cat-1' } }],
    })

    expect(toolResult.content[0]?.text).toContain('writeExecute')
    expect(proxy.invoke).not.toHaveBeenCalled()
    expect(auditEvents[0]).toMatchObject({ operationId: 'updateTransactionCategory', result: 'denied' })
  })

  it('still refuses an excluded operation via the ordinary manifest gate (before the admission gate even runs)', async () => {
    const proxy = makeProxy()
    const handler = buildReadExecuteHandler(proxy, READ_EXECUTE_SPEC_ENTRIES)

    const { toolResult, auditEvents } = await handler({ calls: [{ operationId: 'checkRole' }] })

    expect(toolResult.content[0]?.text).toContain('not available via MCP')
    expect(auditEvents[0]).toMatchObject({ operationId: 'checkRole', result: 'denied' })
  })

  it('has no confirmationToken input field — passing one is simply ignored, never reaches the proxy', async () => {
    const proxy = makeProxy({ transactions: [] })
    const handler = buildReadExecuteHandler(proxy, READ_EXECUTE_SPEC_ENTRIES)

    await handler({ calls: [{ operationId: 'getTransactions' }] } as never)

    expect(proxy.invoke).toHaveBeenCalledWith(
      'getTransactions',
      expect.any(Object),
      expect.objectContaining({ confirmationToken: undefined }),
    )
  })
})

// ── Issue #1612: the batch aggregate is unbounded ────────────────────────────
//
// MAX_RESULT_CHARS (100_000) bounds a SINGLE call's result, but `calls` is capped at 25 →
// 25 * 100_000 = a 2.5 MB aggregate with no bound at all across the whole batch. These tests
// pin a running total tracked across the batch that stops rendering (and stops invoking further
// calls) once exhausted — using the same self-describing marker style as the per-result
// truncation marker (shown/full-equivalent sizes, an actionable hint).
describe('batch aggregate result bound (#1612)', () => {
  // A single result whose rendered length sits comfortably under MAX_RESULT_CHARS (so no
  // per-call truncation marker fires) but where several of them together blow the aggregate cap.
  function makeBigResult(sizeChars: number): { id: string; note: string } {
    return { id: 'x', note: 'a'.repeat(Math.max(0, sizeChars - 20)) }
  }

  it('stops invoking further calls once the running total exceeds the aggregate budget', async () => {
    const bigResult = makeBigResult(90_000) // under the 100k per-call cap
    const proxy = { invoke: mock(() => Promise.resolve(bigResult)) }
    const handler = buildWriteExecuteHandler(proxy)

    // 300_000 / ~90_000-ish rendered chars each → budget exhausted partway through 5 calls.
    const calls: ExecuteCall[] = Array.from({ length: 5 }, () => ({ operationId: 'getTransactions' }))

    const { toolResult, auditEvents } = await handler({ calls })

    // Fewer than all 5 calls were actually invoked.
    expect(proxy.invoke.mock.calls.length).toBeLessThan(5)
    expect(auditEvents.length).toBeLessThan(5)
    expect(auditEvents.every((e) => e.result === 'success')).toBe(true)

    const text = toolResult.content[0]?.text ?? ''
    expect(text).toContain('Batch result budget exhausted')
    expect(text).toContain('300000')
  })

  it('does not fire the batch marker when the aggregate stays under budget', async () => {
    const small = { id: 'x' }
    const proxy = makeProxy(small)
    const handler = buildWriteExecuteHandler(proxy)

    const calls: ExecuteCall[] = [
      { operationId: 'getTransactions' },
      { operationId: 'getAccounts' },
      { operationId: 'getCashFlowSummary' },
    ]

    const { toolResult, auditEvents } = await handler({ calls })

    expect(proxy.invoke).toHaveBeenCalledTimes(3)
    expect(auditEvents).toHaveLength(3)
    expect(toolResult.content[0]?.text ?? '').not.toContain('Batch result budget exhausted')
  })

  it('the manifest-denial break behaviour is untouched by the new budget check (denial still aborts the rest of the batch)', async () => {
    const proxy = makeFailingProxy('excluded', "Operation 'checkRole' is not available via MCP")
    const handler = buildWriteExecuteHandler(proxy)

    const { auditEvents } = await handler({
      calls: [{ operationId: 'checkRole' }, { operationId: 'getTransactions' }],
    })

    // Only the first (denied) call produces an audit event — the second was never reached.
    expect(auditEvents).toHaveLength(1)
    expect(auditEvents[0]?.result).toBe('denied')
  })
})
