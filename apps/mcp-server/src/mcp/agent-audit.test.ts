/**
 * R4-9: agent.ts targeted unit tests — audit wiring and token refresh behavior.
 *
 * agent.ts itself is a Durable Object (McpAgent subclass) which cannot be instantiated
 * in the bun:test unit runner — it requires the Workers runtime. These tests cover the
 * key correctness properties by testing the collaborators and documented behaviors:
 *
 * 1. writeExecute() returns per-call audit events with correct operationClass/result (R4-1)
 * 2. host-proxy tokens are user+grant bound — agent passes correct props (R4-3)
 *
 * NOTE: The DO lifecycle (fetch() updateProps on every request, agent.ts:80-86) is
 * verified by inspection and the agent-auth.test.ts helper tests. The full Workers
 * runtime integration test (index.ts → DO → tool → proxy) is GAP-R4-8, deferred to
 * a follow-up (@cloudflare/vitest-pool-workers).
 */

import { describe, expect, it, mock } from 'bun:test'
import { buildWriteExecuteHandler } from './tools/execute'
import { createHostProxy } from '../sandbox/host-proxy'
import { toAuditEntry } from './audit-mapping'

const WRITE_OP = 'updateTransactionCategory'

describe('agent.ts writeExecute audit wiring (R4-1 + R4-9)', () => {
  it('audit events carry the real operationId and write class for a confirmed write', async () => {
    // Simulate what agent.ts does: build writeExecute handler, run it, capture auditEvents
    const proxy = createHostProxy({
      mutator: mock(() => Promise.resolve({ success: true })) as never,
      userId: 'user-abc',
      grantId: 'grant-xyz',
    })

    const handler = buildWriteExecuteHandler(proxy)

    // Step 1: get confirmation challenge
    const { auditEvents: challengeEvents } = await handler({
      calls: [{ operationId: WRITE_OP, pathParams: { id: 'txn-1' }, body: { categoryId: 'cat-1' } }],
    })
    // Challenge should be audited as write/confirmation_required (not read/success)
    expect(challengeEvents[0]?.operationClass).toBe('write')
    expect(challengeEvents[0]?.result).toBe('confirmation_required')
    expect(challengeEvents[0]?.operationId).toBe(WRITE_OP)
  })

  it('audit events carry write/success after a confirmed write (NOT read/success — the old bug)', async () => {
    const proxy = createHostProxy({
      mutator: mock(() => Promise.resolve({ success: true })) as never,
      userId: 'user-abc',
      grantId: 'grant-xyz',
    })

    const writeCall = { operationId: WRITE_OP, pathParams: { id: 'txn-1' }, body: { categoryId: 'c1' } }
    const handler = buildWriteExecuteHandler(proxy)

    // Step 1: invoke writeExecute without token → get the challenge audit event
    const { auditEvents: challengeEvents, toolResult: challengeResult } = await handler({
      calls: [writeCall],
    })
    expect(challengeEvents[0]?.result).toBe('confirmation_required')
    // Extract the confirmation token from the tool result text
    const tokenMatch = (challengeResult.content[0] as { text: string }).text.match(/"([^"]+)"$/)
    const token = tokenMatch?.[1]
    expect(token).toBeTruthy()

    // Step 2: invoke writeExecute with the token → should succeed and audit as write/success
    const { auditEvents: successEvents } = await handler({
      calls: [writeCall],
      confirmationToken: token!,
    })

    expect(successEvents).toHaveLength(1)
    expect(successEvents[0]?.operationClass).toBe('write')
    expect(successEvents[0]?.operationId).toBe(WRITE_OP)
    expect(successEvents[0]?.result).toBe('success')
    // Regression: must NOT be 'read' (the pre-R4-1 bug)
    expect(successEvents[0]?.operationClass).not.toBe('read')
  })

  it('audit events are empty for an empty calls array', async () => {
    const proxy = createHostProxy({
      mutator: mock(() => Promise.resolve({})) as never,
      userId: 'user-abc',
      grantId: 'grant-xyz',
    })
    const handler = buildWriteExecuteHandler(proxy)
    const { auditEvents } = await handler({ calls: [] })
    expect(auditEvents).toHaveLength(0)
  })

  it('audit events carry read class for a read operation', async () => {
    const proxy = createHostProxy({
      mutator: mock(() => Promise.resolve({ data: [] })) as never,
      userId: 'user-abc',
      grantId: 'grant-xyz',
    })
    const handler = buildWriteExecuteHandler(proxy)
    const { auditEvents } = await handler({
      calls: [{ operationId: 'getTransactions' }],
    })
    expect(auditEvents[0]?.operationClass).toBe('read')
    expect(auditEvents[0]?.result).toBe('success')
  })
})

describe('agent.ts host-proxy user/grant binding (R4-3 + R4-9)', () => {
  it('confirmation token issued for userA+grantA is rejected by userB+grantB proxy', async () => {
    // This tests the PROPERTY that agent.ts must pass the correct userId+grantId
    // from this.props when creating the proxy — if it doesn't, the token binding fails.
    const proxyA = createHostProxy({
      mutator: mock(() => Promise.resolve({})) as never,
      userId: 'user-A',
      grantId: 'grant-A',
    })
    const challengeA = await proxyA.invoke(WRITE_OP, {}).catch((e) => e)
    const tokenA = challengeA.confirmationToken!

    const proxyB = createHostProxy({
      mutator: mock(() => Promise.resolve({})) as never,
      userId: 'user-B',
      grantId: 'grant-B',
    })
    const err = await proxyB.invoke(WRITE_OP, {}, { confirmationToken: tokenA }).catch((e) => e)
    expect(err.code).toBe('invalid_confirmation')
  })
})

// ── R5-19: toAuditEntry pure helper ─────────────────────────────────────────

describe('toAuditEntry (R5-19)', () => {
  const actor = { clerkUserId: 'user-abc', grantId: 'grant-xyz' }

  it('maps a read/success event correctly', () => {
    const entry = toAuditEntry({ operationId: 'getTransactions', operationClass: 'read', result: 'success' }, actor)
    expect(entry.actorUserId).toBe('user-abc')
    expect(entry.grantId).toBe('grant-xyz')
    expect(entry.operationId).toBe('getTransactions')
    expect(entry.operationClass).toBe('read')
    expect(entry.result).toBe('success')
    expect(entry.confirmationPresented).toBe(false)
    expect(entry.confirmationConfirmed).toBe(false)
  })

  // #1569 Task 16, F6. execute.ts now records capability.class verbatim, so a confirmed
  // destructive call arrives here as operationClass 'destructive'. toAuditEntry tested
  // `operationClass === 'write'`, which is false for it — every confirmed destructive operation
  // would have been recorded as UNCONFIRMED, the exact opposite of the audit story the tier
  // exists to tell. RED-verified: reverting audit-mapping.ts to `=== 'write'` fails this
  // assertion with `Expected: true / Received: false`.
  it('maps a destructive/success (confirmed) event — confirmationConfirmed=true (#1569 T16)', () => {
    const entry = toAuditEntry({ operationId: 'deleteTag', operationClass: 'destructive', result: 'success' }, actor)

    expect(entry.operationClass).toBe('destructive')
    expect(entry.confirmationPresented).toBe(false)
    expect(entry.confirmationConfirmed).toBe(true)
  })

  it('maps a destructive/confirmation_required event — confirmationConfirmed=false', () => {
    // The control for the case above: `!== 'read'` must not confirm a call that never ran.
    const entry = toAuditEntry(
      { operationId: 'deleteTag', operationClass: 'destructive', result: 'confirmation_required' },
      actor
    )

    expect(entry.confirmationPresented).toBe(true)
    expect(entry.confirmationConfirmed).toBe(false)
  })

  it('maps a write/success (confirmed) event — confirmationConfirmed=true', () => {
    const entry = toAuditEntry({ operationId: 'updateTransactionCategory', operationClass: 'write', result: 'success' }, actor)
    expect(entry.confirmationPresented).toBe(false)
    expect(entry.confirmationConfirmed).toBe(true)
  })

  it('maps a write/confirmation_required event — confirmationPresented=true, Confirmed=false', () => {
    const entry = toAuditEntry({ operationId: 'updateTransactionCategory', operationClass: 'write', result: 'confirmation_required' }, actor)
    expect(entry.confirmationPresented).toBe(true)
    expect(entry.confirmationConfirmed).toBe(false)
  })

  it('maps a denied event — both flags false', () => {
    const entry = toAuditEntry({ operationId: 'checkRole', operationClass: 'read', result: 'denied' }, actor)
    expect(entry.confirmationPresented).toBe(false)
    expect(entry.confirmationConfirmed).toBe(false)
  })

  it('maps an error event — both flags false', () => {
    const entry = toAuditEntry({ operationId: 'getAccounts', operationClass: 'read', result: 'error' }, actor)
    expect(entry.confirmationPresented).toBe(false)
    expect(entry.confirmationConfirmed).toBe(false)
  })
})
