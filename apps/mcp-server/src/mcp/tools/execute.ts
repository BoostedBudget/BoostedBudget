/**
 * MCP `execute` tool — chains API calls through the host-proxy.
 *
 * Each call is described by:
 *   operationId   — the API operation to invoke (from search())
 *   params        — query parameters
 *   body          — request body for write operations
 *   pathParams    — path parameters (e.g. { id: "abc" })
 *
 * Write operations require a confirmation token (issued by the host-proxy
 * on the first attempt). Pass the token back in confirmationToken to confirm.
 *
 * The host-proxy enforces: manifest gate, write gating, credential injection,
 * rate limits, and error redaction. This tool only handles orchestration.
 */

import { getCapability } from '../../manifest/capability-manifest'
import type { OperationClass as AuditOperationClass } from '../../manifest/capability-manifest'
import { HostProxyError } from '../../sandbox/host-proxy'
import type { McpToolResult } from './search'

export interface ExecuteCall {
  operationId: string
  params?: Record<string, unknown>
  body?: Record<string, unknown>
  pathParams?: Record<string, string>
}

export interface ExecuteInput {
  calls: ExecuteCall[]
  confirmationToken?: string
}

export interface HostProxy {
  invoke(operationId: string, args: Record<string, unknown>, opts?: { confirmationToken?: string; idempotencyKey?: string }): Promise<unknown>
}

/**
 * Per-call audit event surfaced to the caller (agent.ts) so it can record
 * the correct operationClass and result for each executed operation.
 * Previously agent.ts hardcoded operationClass:'read' for all execute calls —
 * confirmed writes and confirmation challenges were logged incorrectly.
 */
export interface ExecuteAuditEvent {
  operationId: string
  operationClass: AuditOperationClass
  result: 'success' | 'confirmation_required' | 'denied' | 'error'
}

/** Return type for the execute handler — includes tool output AND per-call audit trail */
export interface ExecuteResult {
  toolResult: McpToolResult
  auditEvents: ExecuteAuditEvent[]
}

/**
 * Build the URL for an operation, substituting path params.
 * Falls back to a lookup from the spec index for the path template.
 */
function buildRequestArgs(
  call: ExecuteCall,
  specEntries: { operationId: string; method: string; path: string }[],
): Record<string, unknown> {
  const spec = specEntries.find((e) => e.operationId === call.operationId)
  if (!spec) {
    return { url: `/api/v1/unknown/${call.operationId}`, method: 'GET', params: call.params }
  }

  // Substitute path params
  let path = spec.path
  if (call.pathParams) {
    for (const [key, value] of Object.entries(call.pathParams)) {
      path = path.replace(`{${key}}`, encodeURIComponent(value))
    }
  }

  const args: Record<string, unknown> = {
    url: path,
    method: spec.method,
  }
  if (call.params) args.params = call.params
  if (call.body) args.data = call.body

  return args
}

/** HTTP methods that cannot change server state. Everything else is a write. */
const READ_ONLY_METHODS = new Set(['GET', 'HEAD', 'OPTIONS'])

/**
 * Max characters of a single call's JSON result surfaced in the tool output.
 *
 * MCP results are consumed by a model, not a human — an unbounded result risks
 * blowing the client's context window on a single tool call. But the cap must
 * be large enough to hold realistic list responses (transactions, accounts,
 * budgets) or the tool becomes useless for exactly the operations that matter
 * most. 100_000 chars (~25k tokens at a rough 4 chars/token) comfortably fits
 * a page of most list endpoints while still bounding worst case. No documented
 * MCP or Cloudflare Workers response-size limit is lower than this for a
 * single tool-call text block, so this constant IS the limit.
 *
 * Issue #1609 item 8: the previous cap (500 chars, unnamed, silent) truncated
 * production traffic without any marker — on 2026-09-07 a user's MCP client
 * silently lost merchant names and the reversal field out of a transactions
 * response and fell back to reconstructing the answer from analytics
 * endpoints, reporting a confident answer built on a fragment. Truncation
 * must now be large enough to rarely fire, and self-describing when it does.
 */
const MAX_RESULT_CHARS = 100_000

/**
 * Render a call's JSON result, truncating to MAX_RESULT_CHARS and appending an
 * explicit marker when truncation occurs. The marker states the fact of
 * truncation, the shown size, the full size, and an actionable hint — it MUST
 * NOT include any sample of the omitted content (sizes only; no financial
 * data in the marker itself).
 */
function renderResult(result: unknown): string {
  const full = JSON.stringify(result)
  if (full.length <= MAX_RESULT_CHARS) return full

  const shown = full.slice(0, MAX_RESULT_CHARS)
  return (
    `${shown}\n… [truncated: showing ${MAX_RESULT_CHARS} of ${full.length} characters. ` +
    `Narrow the query — add filters, a date range, or a smaller limit — to receive the complete result.]`
  )
}

/**
 * The class to record for a call the manifest gate REFUSED — an excluded operation, or an
 * operationId that is in no manifest at all.
 *
 * ⚠ HISTORY, because the reason this function exists has changed. Round 14, finding CC-1: the
 * permitted path used to derive its class from `capability.confirmationRequired`, which is the
 * field capability-manifest.ts's `excluded()` helper hard-codes to `false` for every excluded
 * entry — so a denied `purgeAccount` was recorded as `operation_class = 'read'`. The HTTP method
 * was adopted as the honest source because the manifest genuinely had no opinion to read.
 *
 * Issue #1569 Task 16 (F1) fixed the underlying defect: excluded entries now carry their TRUE
 * class (excludedWrite() / destructive({ excluded: true })), and two spec-index-driven guards in
 * capability-summary.drift.test.ts keep every GET at 'read' and every state-changing method at
 * 'write' or 'destructive'. So the manifest IS now the honest source, and this function reads it
 * first. `purgeAccount` is recorded as 'destructive', not 'read' and not merely 'write'.
 *
 * The HTTP-method path survives only as the fallback for an operationId with no manifest entry
 * but a spec entry, and the no-evidence-at-all fallback is still `write` — the fail-LOUD
 * direction: for a hallucinated or misspelled operationId, a denial that over-states its class
 * costs an operator one dismissed row, while one that under-states it is invisible to the query
 * that would have found it.
 */
function deniedOperationClass(
  operationId: string,
  specEntries: { operationId: string; method: string; path: string }[],
): AuditOperationClass {
  const capability = getCapability(operationId)
  if (capability) return capability.class
  const spec = specEntries.find((e) => e.operationId === operationId)
  if (!spec) return 'write'
  return READ_ONLY_METHODS.has(spec.method.toUpperCase()) ? 'read' : 'write'
}

/**
 * Build the execute tool handler bound to a host-proxy and spec index.
 *
 * Returns an `ExecuteResult` containing both the MCP tool output and per-call
 * audit events that the caller (agent.ts) must record via audit-client. This
 * separation ensures each call is audited with the correct operationClass and
 * result — the previous behaviour hardcoded 'read'/'success' for all calls.
 */
export function buildExecuteHandler(
  proxy: HostProxy,
  specEntries: { operationId: string; method: string; path: string }[] = [],
): (input: ExecuteInput) => Promise<ExecuteResult> {
  return async (input: ExecuteInput): Promise<ExecuteResult> => {
    const { calls, confirmationToken } = input

    if (!calls || calls.length === 0) {
      return {
        toolResult: { content: [{ type: 'text', text: 'No operations specified. Provide at least one call.' }] },
        auditEvents: [],
      }
    }

    const results: string[] = []
    const auditEvents: ExecuteAuditEvent[] = []

    for (const call of calls) {
      const capability = getCapability(call.operationId)

      if (!capability || capability.excluded) {
        results.push(`❌ ${call.operationId}: not available via MCP`)
        // Emit a denied audit event so the audit trail is complete (R5-9). Its class
        // comes from the HTTP method, NOT from the manifest — see
        // deniedOperationClass for why the manifest cannot answer here (CC-1).
        auditEvents.push({
          operationId: call.operationId,
          operationClass: deniedOperationClass(call.operationId, specEntries),
          result: 'denied',
        })
        break
      }

      // The manifest class verbatim, since #1569 Task 16: it distinguishes a destructive call
      // from an ordinary write, which deriving from `confirmationRequired` could not (both are
      // confirmation-gated). An operator filtering mcp_audit_log for attempts against
      // data-destroying operations can now find them.
      const operationClass: AuditOperationClass = capability.class

      const args = buildRequestArgs(call, specEntries)

      try {
        const result = await proxy.invoke(call.operationId, args, {
          confirmationToken: capability.confirmationRequired ? confirmationToken : undefined,
        })
        results.push(`✅ ${call.operationId}: ${renderResult(result)}`)
        auditEvents.push({ operationId: call.operationId, operationClass, result: 'success' })
      } catch (err) {
        if (err instanceof HostProxyError) {
          if (err.code === 'confirmation_required') {
            const token = err.confirmationToken
            results.push(
              `⚠️ ${call.operationId} requires confirmation.\n` +
                `Re-invoke execute() with confirmationToken: "${token}"`,
            )
            auditEvents.push({ operationId: call.operationId, operationClass, result: 'confirmation_required' })
          } else {
            results.push(`❌ ${call.operationId}: ${err.message} (${err.code})`)
            auditEvents.push({ operationId: call.operationId, operationClass, result: 'error' })
          }
        } else {
          results.push(`❌ ${call.operationId}: unexpected error`)
          auditEvents.push({ operationId: call.operationId, operationClass, result: 'error' })
        }
        break // stop on first error
      }
    }

    return {
      toolResult: { content: [{ type: 'text', text: results.join('\n\n') }] },
      auditEvents,
    }
  }
}
