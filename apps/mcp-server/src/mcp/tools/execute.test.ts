import { describe, expect, it, mock } from 'bun:test'
import { readFileSync } from 'node:fs'
import {
  buildWriteExecuteHandler,
  buildReadExecuteHandler,
  MAX_BATCH_RESULT_CHARS,
  MAX_EMITTED_RESULT_CHARS,
  type ExecuteCall,
  type ExecuteSpecEntry,
} from './execute'
import { HostProxyError } from '../../sandbox/host-proxy'
import { ALL_OPERATION_IDS, getCapability } from '../../manifest/capability-manifest'
import {
  ACKNOWLEDGED_READ_SIDE_EFFECT_OPERATIONS,
  findUnacknowledgedReadSideEffectOperations,
  readReadSideEffectOperationIds,
} from '../../manifest/read-side-effect-extension'
import {
  ACKNOWLEDGED_SAFE_POST_OPERATIONS,
  ACKNOWLEDGED_SAFE_POST_OPERATION_IDS,
  findSafePostDrift,
  readSafePostOperations,
} from '../../manifest/safe-post-extension'
import { readSpecIndex } from '../../../scripts/generate-mcp-capability-summary'
import { sizeFixtures } from '../../responses/size-fixtures'

describe('readExecute module contract wording', () => {
  it('describes the profile last-login write as best-effort and the response as pre-update', () => {
    const source = readFileSync(new URL('./execute.ts', import.meta.url), 'utf8')

    expect(source).toContain(
      "`getUserProfile` attempts a best-effort update of the existing caller's last-login timestamp",
    )
    expect(source).toContain('response retains the pre-update timestamp')
    expect(source).not.toContain(
      "`getUserProfile` updates the existing caller's last-login timestamp on every successful read",
    )
  })
})

// Minimal proxy stub for testing
function makeProxy(invokeResult: unknown = {}) {
  return {
    invoke: mock(() => Promise.resolve(invokeResult)),
  }
}

describe('boost history recovery guidance', () => {
  const specs = readSpecIndex().entries
  const fixture = sizeFixtures.find((entry) => entry.operationId === 'getBoostExecutions')!.payload as {
    executions: Array<Record<string, unknown>>; limit: number; offset: number; total: number
  }
  const payload = { ...fixture, executions: [fixture.executions[0]!], limit: 2, offset: 7 }

  function recoveryParams(text: string): Record<string, unknown> {
    const match = text.match(/params: (\{[^\n]*?\})/)
    expect(match).not.toBeNull()
    return JSON.parse(match![1]!)
  }

  it('starts global history at zero with the response limit and bounded original status', async () => {
    const proxy = makeProxy(payload)
    const result = await buildReadExecuteHandler(proxy, specs)({ calls: [{
      operationId: 'getBoostExecutions', pathParams: { id: 'original-parent' },
      params: { status: 'completed', limit: 99, offset: 7, unrelated: 'omit-me' },
    }] })
    const text = result.toolResult.content[0]!.text!
    expect(recoveryParams(text)).toEqual({ limit: 2, offset: 0, status: 'completed' })
    const hint = text.slice(text.indexOf('Full response available'))
    expect(hint).toContain('Locate the execution IDs retained in this compact payload')
    expect(hint).toContain('advance offset by the returned limit')
    expect(hint).toContain('remove status and restart offset at 0')
    expect(hint).toContain('Stop when all IDs are found or unfiltered history is exhausted')
    expect(hint).toContain('report any missing IDs')
    expect(hint).toContain('Reads are not a consistent snapshot')
    expect(hint).not.toContain('original-parent')
    expect(hint).not.toContain('boost_id')
    expect(hint).not.toContain(String(payload.executions[0]!.id))
    expect(hint).not.toContain('omit-me')
    expect(proxy.invoke).toHaveBeenCalledTimes(1)
  })

  for (const limit of [undefined, null, '10', 0, -1, 101, 1.5, NaN, Infinity, 1, 100]) {
    it(`uses only an integer response limit in 1..100: ${String(limit)}`, async () => {
      const result = await buildReadExecuteHandler(makeProxy({ ...payload, limit }), specs)({ calls: [{
        operationId: 'getBoostExecutions', params: { limit: 88, offset: 777 },
      }] })
      expect(recoveryParams(result.toolResult.content[0]!.text!)).toEqual({
        limit: typeof limit === 'number' && Number.isInteger(limit) && limit >= 1 && limit <= 100 ? limit : 20,
        offset: 0,
      })
    })
  }

  for (const status of [undefined, null, 1, {}, '', 'completed', 'x'.repeat(100), 'x'.repeat(101), '"\\\n']) {
    it(`bounds and safely serializes status ${JSON.stringify(status)}`, async () => {
      const result = await buildReadExecuteHandler(makeProxy(payload), specs)({ calls: [{
        operationId: 'getBoostExecutions', params: { status },
      }] })
      const params = recoveryParams(result.toolResult.content[0]!.text!)
      expect(params).toEqual({ limit: 2, offset: 0,
        ...(typeof status === 'string' && status.length > 0 && status.length <= 100 ? { status } : {}),
      })
    })
  }

  it('keeps same-operation global history queries unchanged', async () => {
    const result = await buildReadExecuteHandler(makeProxy(payload), specs)({ calls: [{
      operationId: 'getUserBoostExecutions', params: { status: 'failed', limit: 2, offset: 7 },
    }] })
    const text = result.toolResult.content[0]!.text!
    expect(text).toContain('Keep the same query and body parameters.')
    expect(text).not.toContain('restart offset')
    expect(text).not.toContain('params:')
  })

  for (const missing of [false, true]) it(`recovers history after parent deletion and status changes (missing ID: ${missing})`, async () => {
    const targets = [
      { ...fixture.executions[0]!, id: 'target-completed' },
      { ...fixture.executions[0]!, id: 'target-changed' },
    ]
    const initial = { ...payload, executions: missing ? [...targets, { ...targets[0]!, id: 'missing' }] : targets }
    // Deleted parents lose their FK; the changed target now precedes the completed
    // stream's last offset, so removing status without resetting would miss it.
    const history = [
      { ...targets[1]!, boost_id: null, status: 'failed' },
      { ...fixture.executions[0]!, id: 'other-1', boost_id: 'other' },
      { ...fixture.executions[0]!, id: 'other-2', boost_id: 'other' },
      { ...targets[0]!, boost_id: null },
    ]
    const proxy = { invoke: mock(async (operationId: string, args: Record<string, unknown>) => {
      if (operationId === 'getBoostExecutions') return initial
      const params = args.params as { status?: string; limit: number; offset: number }
      const rows = history.filter((row) => !params.status || row.status === params.status)
      return { executions: rows.slice(params.offset, params.offset + params.limit), total: rows.length, limit: params.limit, offset: params.offset }
    }) }
    const execute = buildReadExecuteHandler(proxy, specs)
    const compact = (await execute({ calls: [{ operationId: 'getBoostExecutions', pathParams: { id: 'boost-0' }, params: { status: 'completed', offset: 7 } }] })).toolResult.content[0]!.text!
    const params = recoveryParams(compact)
    expect(params).toEqual({ limit: 2, offset: 0, status: 'completed' })
    expect(compact).toContain('remove status and restart offset at 0')
    const found = new Map<string, Record<string, unknown>>()
    const compactPayload = JSON.parse(compact.slice(compact.indexOf(': ') + 2, compact.indexOf('\n\nFull response'))) as typeof payload
    const wanted = new Set(compactPayload.executions.map(row => String(row.id)))
    // Execute the caller-directed two-pass protocol; production only emits guidance.
    for (const status of [params.status, undefined]) {
      for (let offset = 0; ; offset += Number(params.limit)) {
        const text = (await execute({ calls: [{ operationId: 'getUserBoostExecutions', detail: 'full', params: { ...params, status, offset } }] })).toolResult.content[0]!.text!
        const page = JSON.parse(text.slice(text.indexOf(': ') + 2)) as typeof payload
        for (const row of page.executions) if (wanted.has(String(row.id))) found.set(String(row.id), row)
        if (found.size === wanted.size || offset + page.limit >= page.total) break
      }
      if (found.size === wanted.size) break
    }
    expect([...found.keys()].sort()).toEqual(targets.map(row => row.id).sort())
    expect([...wanted].filter(id => !found.has(id))).toEqual(missing ? ['missing'] : [])
    expect(compact).toContain('report any missing IDs')
    for (const row of found.values()) {
      expect(row.boost_id).toBeNull()
      expect(row.definition_snapshot).toEqual(fixture.executions[0]!.definition_snapshot)
    }
    expect(proxy.invoke).toHaveBeenCalledTimes(missing ? 5 : 4)
  })

  it('charges bounded history guidance to result and batch allowances', async () => {
    const large = { ...payload, executions: [{ ...payload.executions[0]!, output_data: { value: 'x'.repeat(100_000) } }] }
    const proxy = makeProxy(large)
    const text = (await buildReadExecuteHandler(proxy, specs)({ calls: Array.from({ length: 4 }, () => ({ operationId: 'getBoostExecutions', params: { status: 'x'.repeat(100) } })) })).toolResult.content[0]!.text!
    expect(proxy.invoke).toHaveBeenCalledTimes(3)
    expect(text).toContain('Batch result budget exhausted after 3 of 4')
    const first = text.split('\n\n✅')[0]!
    const marker = first.match(/\n… \[truncated:[^\]]+\]/)![0]
    expect(first.replace(marker, '').length - '✅ getBoostExecutions: '.length).toBeLessThanOrEqual(100_000)
    expect(first).toContain('report any missing IDs')
  })
})

describe('response detail', () => {
  for (const [operationId, status] of [['rejectTransferSuggestion', 'rejected'], ['acceptTransferSuggestion', 'accepted']]) {
    it(`recovery includes the resulting ${status} transfer status`, async () => {
      const result = await buildWriteExecuteHandler(makeProxy({ suggestion: { id: 'suggestion', status, user_id: 'omit' } }), [
        { operationId: 'getTransferSuggestions', method: 'GET', path: '/suggestions' },
      ])({ calls: [{ operationId: operationId! }] })
      expect(result.toolResult.content[0]!.text).toContain('operationId: "getTransferSuggestions"')
      expect(result.toolResult.content[0]!.text).toContain(`params: {"status":"${status}"}`)
    })
  }

  it('recovers hidden and terminal accounts with explicit list filters', async () => {
    const result = await buildWriteExecuteHandler(makeProxy({ account: { id: 'account', is_hidden: true, status: 'removed', user_id: 'omit' } }))({
      calls: [{ operationId: 'updateAccount' }],
    })
    expect(result.toolResult.content[0]!.text).toContain('params: {"include_hidden":"true","status":"all_statuses"}')
  })

  it('keeps a successful full write byte-identical and presentation out of request arguments', async () => {
    const payload = { id: 'budget', name: 'Trip', user_id: 'owner', created_at: 'yesterday' }
    const proxy = { invoke: mock(async (_operation: string, _args: Record<string, unknown>) => payload) }
    const result = await buildWriteExecuteHandler(proxy, [{ operationId: 'createBudget', method: 'POST', path: '/budgets' }])({
      calls: [{ operationId: 'createBudget', body: { name: 'Trip' }, detail: 'full' }], confirmationToken: 'confirmed',
    })
    expect(result.toolResult.content[0]!.text).toBe(`✅ createBudget: ${JSON.stringify(payload)}`)
    expect(proxy.invoke.mock.calls[0]![1]).toEqual({ url: '/budgets', method: 'POST', data: { name: 'Trip' } })
    expect(result.auditEvents[0]!.result).toBe('success')
  })

  it('charges recovery hints to the whole batch budget', async () => {
    const payload = { accounts: [{ id: 'a', name: '', user_id: 'omit' }] }
    payload.accounts[0]!.name = 'x'.repeat(99_870)
    const proxy = makeProxy(payload)
    const result = await buildReadExecuteHandler(proxy, [{ operationId: 'getAccounts', method: 'GET', path: '/accounts' }])({
      calls: Array.from({ length: 4 }, () => ({ operationId: 'getAccounts' })),
    })
    expect(proxy.invoke).toHaveBeenCalledTimes(3)
    expect(result.toolResult.content[0]!.text).toContain('Batch result budget exhausted after 3 of 4')
  })

  it('uses the catalog history recovery read and does not hint on an unchanged empty collection', async () => {
    const specs = [{ operationId: 'getBoostExecutions', method: 'GET', path: '/boosts/{id}/executions' }]
    const result = await buildReadExecuteHandler(makeProxy({ executions: [{ id: 'execution', definition_snapshot: {} }] }), specs)({ calls: [{ operationId: 'getBoostExecutions', pathParams: { id: 'deleted-boost' } }] })
    expect(result.toolResult.content[0]!.text).toContain('operationId: "getUserBoostExecutions", detail: "full"')
    const empty = await buildReadExecuteHandler(makeProxy({ executions: [] }), specs)({ calls: [{ operationId: 'getBoostExecutions' }] })
    expect(empty.toolResult.content[0]!.text).toBe('✅ getBoostExecutions: {"executions":[]}')
    const schema = await buildReadExecuteHandler(makeProxy({ $schema: 'schema', executions: [] }), specs)({ calls: [{ operationId: 'getBoostExecutions' }] })
    expect(schema.toolResult.content[0]!.text).toContain('detail: "full"')
  })

  it('recovers category writes through their parent budget and continues after fallback', async () => {
    const proxy = { invoke: mock(async (operationId: string) => operationId === 'addBudgetCategory'
      ? { id: 'category-id', budget_id: 'budget-id', created_at: 'omit' }
      : operationId === 'createBudget' ? { id: 'new-budget', future_field: 'kept-on-fallback' } : { total: 1 }) }
    const specs = [
      { operationId: 'getBudget', method: 'GET', path: '/budgets/{id}' },
      { operationId: 'addBudgetCategory', method: 'POST', path: '/budgets/{id}/categories' },
    ]
    const result = await buildWriteExecuteHandler(proxy, specs)({ calls: [
      { operationId: 'addBudgetCategory', pathParams: { id: 'budget-id' } },
      { operationId: 'createBudget' }, { operationId: 'getCashFlowSummary' },
    ] })
    const text = result.toolResult.content[0]!.text
    expect(text).toContain('operationId: "getBudget", detail: "full"')
    expect(text).toContain('pathParams: {"id":"budget-id"}')
    expect(text).not.toContain('pathParams: {"id":"category-id"}')
    expect(text).toContain('Compact response unavailable; returned full response.')
    expect(text).toContain('kept-on-fallback')
    expect(text).toContain('✅ getCashFlowSummary')
    expect(result.auditEvents.map((event) => event.result)).toEqual(['success', 'success', 'success'])
  })

  it('reserves the full-response hint within the result allowance', async () => {
    const proxy = makeProxy({ accounts: [{ id: 'a', name: 'x'.repeat(100_000), user_id: 'omit' }] })
    const result = await buildReadExecuteHandler(proxy, [{ operationId: 'getAccounts', method: 'GET', path: '/accounts' }])({ calls: [{ operationId: 'getAccounts' }] })
    const text = result.toolResult.content[0]!.text
    const marker = text.match(/\n… \[truncated:[^\]]+\]/)![0]
    expect(text.replace(marker, '').length - '✅ getAccounts: '.length).toBeLessThanOrEqual(100_000)
    expect(text).toContain('detail: "full"')
  })

  it('explains the parent goal ID for contribution recovery', async () => {
    const result = await buildWriteExecuteHandler(makeProxy({ id: 'contribution', savings_goal_id: 'goal', updated_at: 'omit' }), [
      { operationId: 'listSavingsGoalContributions', method: 'GET', path: '/goals/{id}/contributions' },
    ])({ calls: [{ operationId: 'logSavingsGoalContribution' }] })
    expect(result.toolResult.content[0]!.text).toContain('pathParams: {"id":"goal"}')
  })

  for (const build of [buildReadExecuteHandler, buildWriteExecuteHandler]) {
    it(`${build.name} defaults to compact and preserves full payloads on request`, async () => {
      const payload = { accounts: [{ id: 'a', name: 'Checking', user_id: 'private' }], total_count: 1 }
      const proxy = makeProxy(payload)
      const handler = build(proxy, [{ operationId: 'getAccounts', method: 'GET', path: '/accounts' }])
      const compact = await handler({ calls: [{ operationId: 'getAccounts' }] })
      expect(compact.toolResult.content[0]!.text).not.toContain('private')
      expect(compact.toolResult.content[0]!.text).toContain('detail: "full"')
      const full = await handler({ calls: [{ operationId: 'getAccounts', detail: 'full' }] })
      expect(full.toolResult.content[0]!.text).toBe(`✅ getAccounts: ${JSON.stringify(payload)}`)
      expect(proxy.invoke.mock.calls[1]![1]).toEqual({ url: '/accounts', method: 'GET' })
    })

    it(`${build.name} validates the whole batch before any proxy activity`, async () => {
      const proxy = { invoke: mock(async () => ({})), confirmBatch: mock(async () => ({ needsConfirmation: true, count: 1 })) }
      const result = await build(proxy)({ calls: [{ operationId: 'createBudget' }, { operationId: 'getAccounts', detail: 'invalid' as 'full' }] })
      expect(result.toolResult.isError).toBe(true)
      expect(proxy.invoke).not.toHaveBeenCalled()
      expect(proxy.confirmBatch).not.toHaveBeenCalled()
    })
  }
})

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
        calls: [{ operationId: 'getTransactions', detail: 'full' }],
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
        calls: [{ operationId: 'getTransactions', detail: 'full' }],
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
// readExecute must therefore require class === 'read', confirmationRequired === false, AND a
// conventionally read-only method or the exact explicitly reviewed safe-POST tuple.
describe('buildReadExecuteHandler — the three-way admission gate (#1610b)', () => {
  const READ_EXECUTE_SPEC_ENTRIES = [
    { operationId: 'getTransactions', method: 'GET', path: '/api/v1/transactions' },
    { operationId: 'updateTransactionCategory', method: 'PATCH', path: '/api/v1/transactions/{id}/category' },
    { operationId: 'runMCPRecipe', method: 'POST', path: '/api/v1/mcp/recipes/{id}/run' },
    { operationId: 'previewCategoryMatches', method: 'POST', path: '/api/v1/budgets/categories/preview' },
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

  it('admits previewCategoryMatches — a POST reviewed and proven non-mutating (#1630 safe-POST)', async () => {
    // Same shape as runMCPRecipe below (class 'read', confirmationRequired false, method POST) —
    // the ONLY thing that distinguishes the two is ACKNOWLEDGED_SAFE_POST_OPERATIONS
    // (manifest/safe-post-extension.ts), a reviewed, human-maintained, explicitly enumerated
    // list. This must never become inferred from the method or the manifest alone.
    const proxy = makeProxy({ transaction_count: 3, total_amount: 42.5 })
    const handler = buildReadExecuteHandler(proxy, READ_EXECUTE_SPEC_ENTRIES)

    const { toolResult, auditEvents } = await handler({
      calls: [{ operationId: 'previewCategoryMatches', body: { pfc_primary: 'FOOD_AND_DRINK', tags: [] } }],
    })

    expect(toolResult.content[0]?.text).toContain('previewCategoryMatches')
    expect(proxy.invoke).toHaveBeenCalledTimes(1)
    expect(auditEvents[0]).toMatchObject({ operationId: 'previewCategoryMatches', operationClass: 'read', result: 'success' })
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

// ── Issue #1610: readExecute's REAL guarantee (not "read-only by construction") ─────────────
//
// `getPrivacySettings` and `getUserProfile` pass all three admission legs but are not side-effect
// free: privacy may create caller-owned defaults; profile attempts a best-effort last-login update
// after fetching the returned timestamp. Both remain admitted because their effects are
// caller-scoped and non-destructive.
// These tests pin the properties that are true of every admitted call against the real manifest
// and spec index, rather than assuming that an HTTP GET cannot write.
describe("readExecute's REAL guarantee, checked against the real manifest + spec index (#1610)", () => {
  const specIndex = readSpecIndex()
  const specEntries: ExecuteSpecEntry[] = specIndex.entries.map((e) => ({
    operationId: e.operationId,
    method: e.method,
    path: e.path,
  }))
  const READ_ONLY_METHODS = new Set(['GET', 'HEAD', 'OPTIONS'])

  it('every admitted operation is non-destructive, non-confirmation-gated, and uses a read-only verb or reviewed safe POST', async () => {
    const admittedOperationIds: string[] = []

    for (const operationId of ALL_OPERATION_IDS) {
      const proxy = makeProxy({})
      const handler = buildReadExecuteHandler(proxy, specEntries)
      await handler({ calls: [{ operationId }] })
      if ((proxy.invoke as unknown as { mock: { calls: unknown[] } }).mock.calls.length > 0) {
        admittedOperationIds.push(operationId)
      }
    }

    // Sanity: the probe actually admitted a non-trivial slice of the manifest (if this drops to
    // 0, the loop above is broken, not the manifest).
    expect(admittedOperationIds.length).toBeGreaterThan(10)

    const acknowledgedSafePostSet = new Set(ACKNOWLEDGED_SAFE_POST_OPERATION_IDS)

    for (const operationId of admittedOperationIds) {
      const capability = getCapability(operationId)
      const spec = specEntries.find((e) => e.operationId === operationId)
      expect(capability, `admitted operation ${operationId} has no manifest entry`).toBeTruthy()
      expect(capability!.destructive, `${operationId} is admitted but destructive`).toBe(false)
      expect(capability!.confirmationRequired, `${operationId} is admitted but confirmation-gated`).toBe(false)
      const methodIsReadOnly = !!spec && READ_ONLY_METHODS.has(spec.method.toUpperCase())
      // #1630: the one disjunction the "conventionally read-only verb" invariant now tolerates —
      // a POST reviewed and enumerated in ACKNOWLEDGED_SAFE_POST_OPERATIONS. Any OTHER
      // non-read-only-verb admission is still a hole in this invariant, same as before.
      const methodIsAcknowledgedSafePost = !!spec && acknowledgedSafePostSet.has(operationId)
      expect(
        methodIsReadOnly || methodIsAcknowledgedSafePost,
        `${operationId} is admitted but its method (${spec?.method}) is neither conventionally ` +
          `read-only nor an acknowledged safe-POST`,
      ).toBe(true)
    }
  })

  // #1630: the exact admitted set is pinned, not just "some non-trivial slice" (the sanity check
  // above). Isolates the interesting case — a class-'read' operation whose method is NOT
  // GET/HEAD/OPTIONS — from the ordinary bulk of admitted GETs, so a second safe-POST added
  // without deliberately updating this test stands out immediately. `runMCPRecipe` has the exact
  // same shape (class 'read', confirmationRequired false, method POST) and must NOT appear here.
  it('the only class-"read" operations admitted by readExecute on a non-read-only verb are the acknowledged safe-POSTs', async () => {
    const nonReadOnlyVerbAdmitted: string[] = []

    for (const operationId of ALL_OPERATION_IDS) {
      const capability = getCapability(operationId)
      const spec = specEntries.find((e) => e.operationId === operationId)
      if (!capability || capability.class !== 'read' || capability.confirmationRequired !== false) continue
      if (!spec || READ_ONLY_METHODS.has(spec.method.toUpperCase())) continue

      const proxy = makeProxy({})
      const handler = buildReadExecuteHandler(proxy, specEntries)
      await handler({ calls: [{ operationId }] })
      if ((proxy.invoke as unknown as { mock: { calls: unknown[] } }).mock.calls.length > 0) {
        nonReadOnlyVerbAdmitted.push(operationId)
      }
    }

    expect(nonReadOnlyVerbAdmitted.sort()).toEqual([...ACKNOWLEDGED_SAFE_POST_OPERATION_IDS].sort())
    expect(nonReadOnlyVerbAdmitted).not.toContain('runMCPRecipe')
  })

  // The properties above are true of EVERY admitted operation, including these two, but do not
  // prove "readExecute cannot write". These are reviewed caller-scoped side-effecting GETs.
  //
  // #1610: this used to be a hardcoded array here — a tripwire that pinned the two known
  // operations but couldn't see a THIRD one added later, since nothing in this package can see
  // the Go implementation to detect a write on its own. It is now derived from
  // ACKNOWLEDGED_READ_SIDE_EFFECT_OPERATIONS (manifest/read-side-effect-extension.ts), which is
  // checked against api/openapi.yaml's `x-mcp-read-side-effect` extension by
  // scripts/check-manifest-completeness.ts (a real CI gate, not just this vitest suite) — see
  // that module's doc comment for the full mechanism.
  it.each(ACKNOWLEDGED_READ_SIDE_EFFECT_OPERATIONS)(
    'acknowledges %s as an admitted GET with a reviewed caller-scoped side effect',
    async (operationId) => {
      const proxy = makeProxy({})
      const handler = buildReadExecuteHandler(proxy, specEntries)

      await handler({ calls: [{ operationId }] })

      // Admitted: the proxy was actually invoked, not refused.
      expect(proxy.invoke).toHaveBeenCalledTimes(1)
      const capability = getCapability(operationId)
      expect(capability?.class).toBe('read')
      expect(capability?.confirmationRequired).toBe(false)
    },
  )

  it('the acknowledged read-side-effect list is exactly these two operations', () => {
    expect(ACKNOWLEDGED_READ_SIDE_EFFECT_OPERATIONS).toEqual([
      'getPrivacySettings',
      'getUserProfile',
    ])
  })

  it('api/openapi.yaml tags exactly the acknowledged read-side-effect operations', () => {
    // The actual scanner: derive what the SPEC currently tags and diff it against what has been
    // reviewed. A non-empty result means a third side-effecting read was tagged without review.
    const specTagged = readReadSideEffectOperationIds()
    expect(findUnacknowledgedReadSideEffectOperations(
      specTagged,
      ACKNOWLEDGED_READ_SIDE_EFFECT_OPERATIONS,
    )).toEqual([])
  })

  // #1630: the mirror-image mechanism — a POST that unexpectedly does NOT write, rather than a
  // GET that unexpectedly does. Same shape as the read-side-effect tests above.
  it.each(ACKNOWLEDGED_SAFE_POST_OPERATION_IDS)(
    'acknowledges %s as an admitted POST proven to write nothing',
    async (operationId) => {
      const proxy = makeProxy({})
      const handler = buildReadExecuteHandler(proxy, specEntries)

      await handler({ calls: [{ operationId }] })

      expect(proxy.invoke).toHaveBeenCalledTimes(1)
      const capability = getCapability(operationId)
      expect(capability?.class).toBe('read')
      expect(capability?.confirmationRequired).toBe(false)
      const spec = specEntries.find((e) => e.operationId === operationId)
      expect(spec && !READ_ONLY_METHODS.has(spec.method.toUpperCase())).toBe(true)
    },
  )

  it('the acknowledged safe-POST list is exactly this one — a new admitted non-read-verb operation needs a human to update this list', () => {
    expect(ACKNOWLEDGED_SAFE_POST_OPERATIONS).toEqual([
      { operationId: 'previewCategoryMatches', method: 'POST', path: '/api/v1/budgets/categories/preview' },
    ])
  })

  it('api/openapi.yaml tags exactly the acknowledged operations with x-mcp-safe-post — no unreviewed drift in either direction', () => {
    const specTagged = readSafePostOperations()
    expect(findSafePostDrift(specTagged, ACKNOWLEDGED_SAFE_POST_OPERATIONS)).toEqual({ unacknowledged: [], stale: [] })
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
  function expectBoundedOutput(text: string, fullLength: number): void {
    const marker = text.match(
      /\n… \[output truncated: showing (\d+) of (\d+) UTF-16 code units\. Split the batch or narrow the request to receive the complete output\.\]$/,
    )
    expect(marker).not.toBeNull()
    expect(Number(marker![1])).toBe(text.indexOf('\n… [output truncated:'))
    expect(Number(marker![2])).toBe(fullLength)
    expect(text).toHaveLength(MAX_EMITTED_RESULT_CHARS)
  }

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
    expect(text).toContain(String(MAX_BATCH_RESULT_CHARS))
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

  it('preserves the exact bytes of an under-budget multi-call batch', async () => {
    const proxy = { invoke: mock((operationId: string) => Promise.resolve({ operationId })) }
    const handler = buildWriteExecuteHandler(proxy)

    const { toolResult } = await handler({ calls: [
      { operationId: 'listMCPRecipes', detail: 'full' },
      { operationId: 'listMCPRecipes', detail: 'full' },
    ] })

    expect(toolResult.content[0]?.text).toBe(
      '✅ listMCPRecipes: {"operationId":"listMCPRecipes"}\n\n' +
      '✅ listMCPRecipes: {"operationId":"listMCPRecipes"}',
    )
  })

  it('charges the per-call prefix and separator, not just the rendered payload', async () => {
    // #1612 round 2 regression. Each success line is `✅ <operationId>: <rendered>` joined by
    // '\n\n', but only `rendered.length` used to be charged against the budget. With many small
    // results the UNCHARGED text (prefix + separator, ~22 chars) dominates the charged text
    // (~10 chars), so the returned string ran roughly 3x past the cap while the counter still
    // read as under it. Against the old accounting all 20_000 calls execute and the marker never
    // fires; against the corrected accounting the batch stops partway.
    const small = { id: 'x' }
    const proxy = makeProxy(small)
    const handler = buildWriteExecuteHandler(proxy)

    const calls: ExecuteCall[] = Array.from({ length: 20_000 }, () => ({ operationId: 'getTransactions' }))

    const { toolResult } = await handler({ calls })

    const text = toolResult.content[0]?.text ?? ''
    expect(text).toContain('Batch result budget exhausted')
    expect(proxy.invoke.mock.calls.length).toBeLessThan(20_000)
  })

  it('bounds writeExecute output, including separators and the exhaustion marker, at exactly the aggregate budget', async () => {
    // Deliberately the many-small-results shape, NOT a few large ones: with large payloads the
    // payload-only accounting trips the cap at nearly the right place anyway, so the assertion
    // would pass against the old code and pin nothing. Here the uncharged prefixes dominate, so
    // the old accounting overshoots this bound by ~240_000 characters.
    const small = { id: 'x' }
    const proxy = makeProxy(small)
    const handler = buildWriteExecuteHandler(proxy)

    const calls: ExecuteCall[] = Array.from({ length: 20_000 }, () => ({ operationId: 'getTransactions' }))

    const { toolResult } = await handler({ calls })

    const text = toolResult.content[0]?.text ?? ''
    expect(text.length).toBeLessThanOrEqual(MAX_BATCH_RESULT_CHARS)
    expect(text).toContain('Batch result budget exhausted')
  })

  it('bounds readExecute output, including separators and the exhaustion marker, at exactly the aggregate budget', async () => {
    const proxy = makeProxy(makeBigResult(99_000))
    const handler = buildReadExecuteHandler(proxy, [
      { operationId: 'getTransactions', method: 'GET', path: '/api/v1/transactions' },
    ])

    const { toolResult } = await handler({
      calls: Array.from({ length: 5 }, () => ({ operationId: 'getTransactions' })),
    })

    const text = toolResult.content[0]?.text ?? ''
    expect(text.length).toBeLessThanOrEqual(MAX_BATCH_RESULT_CHARS)
    expect(text).toContain('Batch result budget exhausted')
    expect(proxy.invoke.mock.calls.length).toBeLessThan(5)
  })

  it('reports rendered and omitted call counts, the budget, and recovery guidance', async () => {
    const proxy = makeProxy(makeBigResult(99_000))
    const handler = buildWriteExecuteHandler(proxy)

    const { toolResult } = await handler({
      calls: Array.from({ length: 5 }, () => ({ operationId: 'getTransactions' })),
    })

    const text = toolResult.content[0]?.text ?? ''
    expect(text).toContain('after 4 of 5 call(s)')
    expect(text).toContain('1 call(s) were not executed')
    expect(text).toContain(`${MAX_BATCH_RESULT_CHARS} character budget`)
    expect(text).toContain('Split this batch into smaller invocations')
    expect(text).toContain('narrow filters/limits')
  })

  function payloadForSuccessLineLength(lineLength: number): { note: string } {
    const prefix = '✅ listMCPRecipes: '
    const emptyJson = JSON.stringify({ note: '' })
    return { note: 'x'.repeat(lineLength - prefix.length - emptyJson.length) }
  }

  const exactBudgetCalls: ExecuteCall[] = Array.from(
    { length: 3 },
    () => ({ operationId: 'listMCPRecipes', detail: 'full' }),
  )

  function makeExactBudgetProxy() {
    const desiredLineLengths = [99_999, 99_999, 99_998]
    let index = 0
    return {
      invoke: mock(() => Promise.resolve(payloadForSuccessLineLength(desiredLineLengths[index++]!))),
    }
  }

  it('returns an exactly 300,000-character completed batch without a marker', async () => {
    const proxy = makeExactBudgetProxy()
    const handler = buildWriteExecuteHandler(proxy)

    const { toolResult, auditEvents } = await handler({ calls: exactBudgetCalls })

    const text = toolResult.content[0]?.text ?? ''
    expect(text).toHaveLength(MAX_BATCH_RESULT_CHARS)
    expect(text).not.toContain('Batch result budget exhausted')
    expect(proxy.invoke).toHaveBeenCalledTimes(3)
    expect(auditEvents).toHaveLength(3)
  })

  it('does not invoke another call after an exact 300,000-character batch', async () => {
    const proxy = makeExactBudgetProxy()
    const handler = buildWriteExecuteHandler(proxy)

    const { toolResult, auditEvents } = await handler({
      calls: [...exactBudgetCalls, { operationId: 'getAccounts' }],
    })

    const text = toolResult.content[0]?.text ?? ''
    expect(proxy.invoke).toHaveBeenCalledTimes(3)
    expect(auditEvents).toHaveLength(3)
    expect(text.length).toBeLessThanOrEqual(MAX_BATCH_RESULT_CHARS)
    expect(text).toContain('Batch result budget exhausted after 3 of 4 call(s)')
    expect(text).toContain('1 call(s) were not executed')
  })

  it('bounds an oversized unknown operationId and retains its denied audit event', async () => {
    const tailSentinel = 'UNKNOWN_OPERATION_TAIL_MUST_NOT_LEAK'
    const operationId = `unknown-${'x'.repeat(400_000)}-${tailSentinel}`
    const proxy = makeProxy()

    const { toolResult, auditEvents } = await buildWriteExecuteHandler(proxy)({ calls: [
      { operationId },
      { operationId: 'getTransactions' },
    ] })

    const text = toolResult.content[0]?.text ?? ''
    expect(text.length).toBeLessThanOrEqual(MAX_EMITTED_RESULT_CHARS)
    expectBoundedOutput(text, `❌ ${operationId}: not available via MCP`.length)
    expect(text).not.toContain(tailSentinel)
    expect(proxy.invoke).not.toHaveBeenCalled()
    expect(auditEvents).toEqual([{ operationId, operationClass: 'write', result: 'denied' }])
  })

  it('bounds an oversized HostProxyError in readExecute and still stops on first error', async () => {
    const tailSentinel = 'BACKEND_ERROR_TAIL_MUST_NOT_LEAK'
    const message = `backend-${'y'.repeat(400_000)}-${tailSentinel}`
    const proxy = { invoke: mock(() => Promise.reject(new HostProxyError('backend_error', message))) }

    const { toolResult, auditEvents } = await buildReadExecuteHandler(proxy, [
      { operationId: 'getTransactions', method: 'GET', path: '/api/v1/transactions' },
      { operationId: 'getAccounts', method: 'GET', path: '/api/v1/accounts' },
    ])({ calls: [
      { operationId: 'getTransactions' },
      { operationId: 'getAccounts' },
    ] })

    const text = toolResult.content[0]?.text ?? ''
    expect(text.length).toBeLessThanOrEqual(MAX_EMITTED_RESULT_CHARS)
    expectBoundedOutput(text, `❌ getTransactions: ${message} (backend_error)`.length)
    expect(text).not.toContain(tailSentinel)
    expect(proxy.invoke).toHaveBeenCalledTimes(1)
    expect(auditEvents).toEqual([
      { operationId: 'getTransactions', operationClass: 'read', result: 'error' },
    ])
  })

  it('bounds an oversized early batch-confirmation HostProxyError', async () => {
    const tailSentinel = 'CONFIRMATION_ERROR_TAIL_MUST_NOT_LEAK'
    const message = `confirmation-${'z'.repeat(400_000)}-${tailSentinel}`
    const proxy = {
      invoke: mock(() => Promise.resolve({})),
      confirmBatch: mock(() => Promise.reject(new HostProxyError('invalid_confirmation', message))),
    }

    const { toolResult, auditEvents } = await buildWriteExecuteHandler(proxy)({ calls: [
      { operationId: 'createMCPRecipe' },
    ] })

    const text = toolResult.content[0]?.text ?? ''
    expect(text.length).toBeLessThanOrEqual(MAX_EMITTED_RESULT_CHARS)
    expectBoundedOutput(text, `❌ batch confirmation failed: ${message} (invalid_confirmation)`.length)
    expect(text).not.toContain(tailSentinel)
    expect(proxy.invoke).not.toHaveBeenCalled()
    expect(auditEvents).toEqual([
      { operationId: 'createMCPRecipe', operationClass: 'write', result: 'error' },
    ])
  })

  it('bounds an oversized token in an early batch-confirmation challenge', async () => {
    const tailSentinel = 'CONFIRMATION_TOKEN_TAIL_MUST_NOT_LEAK'
    const token = `token-${'t'.repeat(400_000)}-${tailSentinel}`
    const proxy = {
      invoke: mock(() => Promise.resolve({})),
      confirmBatch: mock(() => Promise.resolve({ needsConfirmation: true, token, count: 1 })),
    }

    const { toolResult, auditEvents } = await buildWriteExecuteHandler(proxy)({ calls: [
      { operationId: 'createMCPRecipe' },
    ] })

    const text = toolResult.content[0]?.text ?? ''
    expect(text.length).toBeLessThanOrEqual(MAX_EMITTED_RESULT_CHARS)
    expectBoundedOutput(
      text,
      (`⚠️ 1 operation requires confirmation. Nothing has been executed.\n` +
        `Re-invoke writeExecute() with the IDENTICAL calls array and confirmationToken: "${token}"`).length,
    )
    expect(text).not.toContain(tailSentinel)
    expect(proxy.invoke).not.toHaveBeenCalled()
    expect(auditEvents).toEqual([
      { operationId: 'createMCPRecipe', operationClass: 'write', result: 'confirmation_required' },
    ])
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


// ── Issue #1625 B + E: 204 rendering, and multi-write batch confirmation ──────────────────
import { createHostProxy } from '../../sandbox/host-proxy'
import type { FetchMutator } from '../../client/fetch-mutator'

describe('#1625 B: a 204 No Content response is a SUCCESS, not "unexpected error"', () => {
  // fetch-mutator maps 204 -> undefined. renderResult() then did
  // `JSON.stringify(result).length`, and JSON.stringify(undefined) returns the VALUE undefined,
  // not a string — so .length threw a TypeError that is not a HostProxyError, landing in the
  // generic "unexpected error" branch which ALSO break()s the rest of the batch. The delete had
  // already committed server-side, so the agent was told a successful write had failed.
  it('reports success when the proxy resolves undefined (HTTP 204)', async () => {
    const proxy = { invoke: mock(() => Promise.resolve(undefined)) }
    const handler = buildWriteExecuteHandler(proxy, [
      { operationId: 'deleteRule', path: '/api/v1/auto-categorization/{id}', method: 'DELETE' },
    ])

    const { toolResult, auditEvents } = await handler({
      calls: [{ operationId: 'deleteRule', pathParams: { id: 'r-1' } }],
      confirmationToken: 'tok',
    })

    const text = toolResult.content[0]!.text
    expect(text).not.toContain('unexpected error')
    expect(text).toContain('✅ deleteRule')
    expect(auditEvents[0]).toMatchObject({ operationId: 'deleteRule', result: 'success' })
  })

  it('does not abort the rest of the batch after a 204', async () => {
    let n = 0
    const proxy = { invoke: mock(() => Promise.resolve(n++ === 0 ? undefined : { ok: true })) }
    const handler = buildWriteExecuteHandler(proxy, [
      { operationId: 'deleteRule', path: '/api/v1/auto-categorization/{id}', method: 'DELETE' },
      { operationId: 'getTransactions', path: '/api/v1/transactions', method: 'GET' },
    ])

    const { auditEvents } = await handler({
      calls: [
        { operationId: 'deleteRule', pathParams: { id: 'r-1' } },
        { operationId: 'getTransactions' },
      ],
      confirmationToken: 'tok',
    })

    expect(auditEvents).toHaveLength(2)
    expect(auditEvents[1]).toMatchObject({ operationId: 'getTransactions', result: 'success' })
  })
})

describe('#1625 E: a batch of multiple confirmation-gated writes must be completable', () => {
  const SPEC: ExecuteSpecEntry[] = [
    { operationId: 'createBudget', path: '/api/v1/budgets', method: 'POST' },
    { operationId: 'addBudgetCategory', path: '/api/v1/budgets/{id}/categories', method: 'POST' },
  ]
  const CALLS: ExecuteCall[] = [
    { operationId: 'createBudget', body: { name: 'Monthly' } },
    { operationId: 'addBudgetCategory', pathParams: { id: 'b-1' }, body: { name: 'Mortgage' } },
  ]

  function realProxyHandler() {
    const mutator = mock(() => Promise.resolve({ id: 'b-1' })) as unknown as FetchMutator
    const proxy = createHostProxy({ mutator, userId: 'u1', grantId: 'g1' })
    return { handler: buildWriteExecuteHandler(proxy, SPEC), mutator }
  }
  const tokenFrom = (t: string) => t.match(/confirmationToken: "([^"]+)"/)?.[1]
  const callCount = (m: FetchMutator) => (m as unknown as { mock: { calls: unknown[] } }).mock.calls.length

  it('executes NOTHING on the challenge round', async () => {
    const { handler, mutator } = realProxyHandler()
    const r1 = await handler({ calls: CALLS })
    expect(tokenFrom(r1.toolResult.content[0]!.text)).toBeDefined()
    expect(callCount(mutator)).toBe(0)
  })

  it('completes BOTH writes when the identical batch is replayed with the token', async () => {
    const { handler, mutator } = realProxyHandler()
    const r1 = await handler({ calls: CALLS })
    const token = tokenFrom(r1.toolResult.content[0]!.text)!

    const r2 = await handler({ calls: CALLS, confirmationToken: token })
    const text = r2.toolResult.content[0]!.text

    expect(text).not.toContain('invalid_confirmation')
    expect(text).toContain('✅ createBudget')
    expect(text).toContain('✅ addBudgetCategory')
    expect(callCount(mutator)).toBe(2)
    expect(r2.auditEvents.every((e) => e.result === 'success')).toBe(true)
  })

  it('refuses a replay whose calls differ from the confirmed batch', async () => {
    const { handler, mutator } = realProxyHandler()
    const r1 = await handler({ calls: CALLS })
    const token = tokenFrom(r1.toolResult.content[0]!.text)!

    const tampered: ExecuteCall[] = [
      CALLS[0]!,
      { operationId: 'addBudgetCategory', pathParams: { id: 'b-1' }, body: { name: 'SOMETHING ELSE' } },
    ]
    const r2 = await handler({ calls: tampered, confirmationToken: token })

    expect(r2.toolResult.content[0]!.text).toContain('invalid_confirmation')
    expect(callCount(mutator)).toBe(0)
  })

  it('cannot replay the same batch token twice', async () => {
    const { handler, mutator } = realProxyHandler()
    const r1 = await handler({ calls: CALLS })
    const token = tokenFrom(r1.toolResult.content[0]!.text)!

    await handler({ calls: CALLS, confirmationToken: token })
    const again = await handler({ calls: CALLS, confirmationToken: token })

    expect(again.toolResult.content[0]!.text).toContain('invalid_confirmation')
    expect(callCount(mutator)).toBe(2) // still only the one successful round
  })

  it('names args-mismatch as a cause so the agent can self-correct', async () => {
    const { handler } = realProxyHandler()
    const r = await handler({ calls: CALLS, confirmationToken: 'not-a-real-token' })
    expect(r.toolResult.content[0]!.text.toLowerCase()).toContain('identical')
  })

  it('tells the agent the identical calls payload must be resent', async () => {
    const { handler } = realProxyHandler()
    const r1 = await handler({ calls: CALLS })
    expect(r1.toolResult.content[0]!.text.toLowerCase()).toContain('identical')
  })
})

// #1625 Task 4: writeExecute must hand confirmBatch the WHOLE batch, not its gated subset.
//
// The challenge text and the invalid-token message both instruct the agent to replay "the
// IDENTICAL calls array", but execute.ts filtered to gated calls before handing them over and
// hashBatch hashed only those. An ungated read could therefore be swapped, added or dropped
// between challenge and confirmation while the token still validated -- the enforced invariant
// was narrower than the promised one. Reads are non-destructive so the practical risk is low;
// the mismatch between promise and guarantee is the defect.
describe('batch confirmation covers the whole batch (#1625)', () => {
  const GATED = 'createMCPRecipe'
  const READ = 'listMCPRecipes'

  function proxyCapturingConfirmBatch() {
    const seen: Array<Array<{ operationId: string }>> = []
    return {
      seen,
      invoke: mock(() => Promise.resolve({})),
      confirmBatch: mock((calls: Array<{ operationId: string }>) => {
        seen.push(calls)
        return Promise.resolve({ needsConfirmation: true, token: 'tok', count: 1 })
      }),
    }
  }

  it('passes ungated reads to confirmBatch, so they are inside the hashed batch', async () => {
    const proxy = proxyCapturingConfirmBatch()
    const handler = buildWriteExecuteHandler(proxy)

    await handler({
      calls: [
        { operationId: READ, params: {} },
        { operationId: GATED, params: {} },
      ] as ExecuteCall[],
    })

    expect(proxy.seen).toHaveLength(1)
    expect(proxy.seen[0]!.map((c) => c.operationId)).toEqual([READ, GATED])
  })

  it('rejects a replay that removes defined call data discarded by request normalization', async () => {
    const mutator = mock(() => Promise.resolve({ id: 'recipe-1' })) as unknown as FetchMutator
    const proxy = createHostProxy({ mutator, userId: 'exact-user', grantId: 'exact-grant' })
    const handler = buildWriteExecuteHandler(proxy)
    const challengedCalls: ExecuteCall[] = [
      { operationId: GATED, pathParams: { ignored: 'A' } },
    ]

    const challenge = await handler({ calls: challengedCalls })
    const token = challenge.toolResult.content[0]!.text.match(/confirmationToken: "([^"]+)"/)?.[1]
    expect(token).toBeDefined()
    expect(mutator).not.toHaveBeenCalled()

    const replay = await handler({
      calls: [{ operationId: GATED }],
      confirmationToken: token,
    })

    expect(replay.toolResult.content[0]!.text).toContain('invalid_confirmation')
    expect(mutator).not.toHaveBeenCalled()
    expect(replay.auditEvents).toEqual([
      { operationId: GATED, operationClass: 'write', result: 'error' },
    ])
  })
})
