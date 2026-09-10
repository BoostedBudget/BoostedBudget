import { describe, expect, it, mock } from 'bun:test'
import { buildWriteExecuteHandler, buildReadExecuteHandler, type ExecuteCall, type ExecuteSpecEntry } from './execute'
import { HostProxyError } from '../../sandbox/host-proxy'
import { ALL_OPERATION_IDS, getCapability } from '../../manifest/capability-manifest'
import {
  ACKNOWLEDGED_LAZY_INIT_OPERATIONS,
  findUnacknowledgedLazyInitOperations,
  readLazyInitOperationIds,
} from '../../manifest/lazy-init-extension'
import {
  ACKNOWLEDGED_SAFE_POST_OPERATIONS,
  findUnacknowledgedSafePostOperations,
  readSafePostOperationIds,
} from '../../manifest/safe-post-extension'
import { readSpecIndex } from '../../../scripts/generate-mcp-capability-summary'

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
// Codex adversarial review of d8567c2e found the module doc's old claim false: `getPrivacySettings`
// and `getUserProfile` pass all three admission legs yet lazily `repo.Create` the caller's own
// default row on first access (privacy_service.go / user profile equivalent — see each operation's
// OpenAPI description). The fix keeps both operations admitted (the user's explicit choice) and
// replaces the false claim with the properties that ARE true of every admitted call. These tests
// pin those properties against the REAL manifest + spec index, not a fabricated one, so a future
// widening of the gate (e.g. dropping the method check, or admitting confirmationRequired: true)
// is caught here even if no other test happens to exercise the specific operation that regresses.
describe("readExecute's REAL guarantee, checked against the real manifest + spec index (#1610)", () => {
  const specIndex = readSpecIndex()
  const specEntries: ExecuteSpecEntry[] = specIndex.entries.map((e) => ({
    operationId: e.operationId,
    method: e.method,
    path: e.path,
  }))
  const READ_ONLY_METHODS = new Set(['GET', 'HEAD', 'OPTIONS'])

  it('every operation readExecute actually admits is non-destructive, non-confirmation-gated, and uses a conventionally read-only verb', async () => {
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

    const acknowledgedSafePostSet = new Set(ACKNOWLEDGED_SAFE_POST_OPERATIONS)

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

    expect(nonReadOnlyVerbAdmitted.sort()).toEqual([...ACKNOWLEDGED_SAFE_POST_OPERATIONS].sort())
    expect(nonReadOnlyVerbAdmitted).not.toContain('runMCPRecipe')
  })

  // The properties above are true of EVERY admitted operation, including these two — but they do
  // NOT prove "readExecute cannot write". These two are known, named exceptions: both lazily
  // create the caller's own default row on first access.
  //
  // #1610: this used to be a hardcoded array here — a tripwire that pinned the two known
  // exceptions but couldn't see a THIRD one added later, since nothing in this package can see
  // the Go implementation to detect a write on its own. It is now derived from
  // ACKNOWLEDGED_LAZY_INIT_OPERATIONS (manifest/lazy-init-extension.ts), which is itself checked
  // against api/openapi.yaml's `x-mcp-lazy-init` extension by
  // scripts/check-manifest-completeness.ts (a real CI gate, not just this vitest suite) — see
  // that module's doc comment for the full mechanism.
  it.each(ACKNOWLEDGED_LAZY_INIT_OPERATIONS)(
    'acknowledges %s as an admitted GET that lazily creates a row on first access',
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

  it('the acknowledged list is exactly these two — a new admitted read-with-a-write needs a human to update this list', () => {
    // This does not (and cannot) detect a new lazy-init write on its own; it only pins the
    // count/names of the ones already acknowledged, so a PR adding a third stands out in review.
    expect(ACKNOWLEDGED_LAZY_INIT_OPERATIONS).toEqual(['getPrivacySettings', 'getUserProfile'])
  })

  it('api/openapi.yaml tags exactly the acknowledged operations with x-mcp-lazy-init — no unreviewed drift', () => {
    // The actual scanner: derive what the SPEC currently tags and diff it against what has been
    // reviewed. A non-empty result here means a third lazy-init-tagged operation was added to the
    // Go layer without anyone updating ACKNOWLEDGED_LAZY_INIT_OPERATIONS — the exact gap the
    // by-name-only tripwire above could not close.
    const specTagged = readLazyInitOperationIds()
    expect(findUnacknowledgedLazyInitOperations(specTagged, ACKNOWLEDGED_LAZY_INIT_OPERATIONS)).toEqual([])
  })

  // #1630: the mirror-image mechanism — a POST that unexpectedly does NOT write, rather than a
  // GET that unexpectedly does. Same shape as the lazy-init tests above, copied part for part.
  it.each(ACKNOWLEDGED_SAFE_POST_OPERATIONS)(
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
    expect(ACKNOWLEDGED_SAFE_POST_OPERATIONS).toEqual(['previewCategoryMatches'])
  })

  it('api/openapi.yaml tags exactly the acknowledged operations with x-mcp-safe-post — no unreviewed drift', () => {
    const specTagged = readSafePostOperationIds()
    expect(findUnacknowledgedSafePostOperations(specTagged, ACKNOWLEDGED_SAFE_POST_OPERATIONS)).toEqual([])
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

  it('bounds the returned text at the budget plus one call plus the marker', async () => {
    // The honest bound, pinned. The budget check is a PRE-check, so the text may exceed
    // MAX_BATCH_RESULT_CHARS by at most one call's rendered output (itself capped at
    // MAX_RESULT_CHARS) plus the exhaustion marker. This asserts that bound rather than the
    // tighter "never exceeds 300_000", which is not what the implementation provides.
    const MAX_BATCH = 300_000
    const MAX_RESULT = 100_000
    const MARKER_SLACK = 1_000

    // Deliberately the many-small-results shape, NOT a few large ones: with large payloads the
    // payload-only accounting trips the cap at nearly the right place anyway, so the assertion
    // would pass against the old code and pin nothing. Here the uncharged prefixes dominate, so
    // the old accounting overshoots this bound by ~240_000 characters.
    const small = { id: 'x' }
    const proxy = makeProxy(small)
    const handler = buildWriteExecuteHandler(proxy)

    const calls: ExecuteCall[] = Array.from({ length: 20_000 }, () => ({ operationId: 'getTransactions' }))

    const { toolResult } = await handler({ calls })

    // Length first, deliberately: this test exists to pin the BOUND, so the bound must be the
    // assertion that fires when the accounting regresses -- not the marker check, which would
    // otherwise mask it and duplicate the test above.
    const text = toolResult.content[0]?.text ?? ''
    expect(text.length).toBeLessThanOrEqual(MAX_BATCH + MAX_RESULT + MARKER_SLACK)
    expect(text).toContain('Batch result budget exhausted')
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
})
