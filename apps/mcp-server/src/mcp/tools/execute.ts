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
  operationClass: 'read' | 'write'
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
      const operationClass: 'read' | 'write' = capability?.confirmationRequired ? 'write' : 'read'

      if (!capability || capability.excluded) {
        results.push(`❌ ${call.operationId}: not available via MCP`)
        break // denied ops skip audit (manifest gate logs denial)
      }

      const args = buildRequestArgs(call, specEntries)

      try {
        const result = await proxy.invoke(call.operationId, args, {
          confirmationToken: capability.confirmationRequired ? confirmationToken : undefined,
        })
        results.push(`✅ ${call.operationId}: ${JSON.stringify(result).slice(0, 500)}`)
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
