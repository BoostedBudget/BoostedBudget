import { describe, expect, it, mock } from 'bun:test'
import { buildWriteExecuteHandler } from './execute'

// Issue #1630, Task 0: reproduce (or strike) the claim that "preview forces a confirmation
// round-trip" for previewCategoryMatches. Static reading at planning time already contradicted
// this: capability-manifest.ts registers previewCategoryMatches via the `read()` helper (line
// ~453), which hardcodes `confirmationRequired: false` (see the `read` helper, ~lines 86-93).
// capability-manifest.test.ts's "treats previewCategoryMatches as read (non-mutating POST)"
// test already pins `confirmationRequired` to `false` at the manifest-entry level; this test
// confirms the CONSEQUENCE at the writeExecute runtime level — that calling it through
// writeExecute with NO confirmationToken runs it immediately, with no confirmation challenge
// text and no second round-trip required. No production code is changed here.
//
// Run with: bun test src/mcp/tools/execute.claim1630.test.ts

function makeProxy(invokeResult: unknown = {}) {
  return {
    invoke: mock(() => Promise.resolve(invokeResult)),
  }
}

describe('#1630 P6: previewCategoryMatches via writeExecute does NOT force a confirmation round-trip', () => {
  it('executes previewCategoryMatches immediately with no confirmationToken — no challenge is returned', async () => {
    const proxy = makeProxy({ matches: [] })
    const handler = buildWriteExecuteHandler(proxy)

    const { toolResult, auditEvents } = await handler({
      calls: [{ operationId: 'previewCategoryMatches', body: { pfc_primary: 'FOOD_AND_DRINK', tags: [] } }],
      // confirmationToken deliberately omitted, exactly as the issue's reported repro did.
    })

    // CLAIM: this call is refused/challenged pending confirmation.
    // ACTUAL: the proxy is invoked immediately — no confirmation text anywhere in the result.
    expect(proxy.invoke).toHaveBeenCalledTimes(1)
    expect(proxy.invoke).toHaveBeenCalledWith(
      'previewCategoryMatches',
      expect.any(Object),
      expect.any(Object),
    )
    const text = toolResult.content[0]?.text ?? ''
    expect(text).not.toContain('confirmation')
    expect(text).not.toContain('Re-invoke writeExecute')

    expect(auditEvents).toHaveLength(1)
    expect(auditEvents[0]?.operationId).toBe('previewCategoryMatches')
    expect(auditEvents[0]?.operationClass).toBe('read')
    expect(auditEvents[0]?.result).toBe('success')
  })
})
