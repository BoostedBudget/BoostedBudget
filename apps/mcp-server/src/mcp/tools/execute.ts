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
  invoke(operationId: string, args: Record<string, unknown>, opts?: { confirmationToken?: string }): Promise<unknown>
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
 */
export function buildExecuteHandler(
  proxy: HostProxy,
  specEntries: { operationId: string; method: string; path: string }[] = [],
): (input: ExecuteInput) => Promise<McpToolResult> {
  return async (input: ExecuteInput): Promise<McpToolResult> => {
    const { calls, confirmationToken } = input

    if (!calls || calls.length === 0) {
      return {
        content: [{ type: 'text', text: 'No operations specified. Provide at least one call.' }],
      }
    }

    const results: string[] = []

    for (const call of calls) {
      const capability = getCapability(call.operationId)

      if (!capability || capability.excluded) {
        results.push(`❌ ${call.operationId}: not available via MCP`)
        break
      }

      const args = buildRequestArgs(call, specEntries)

      try {
        const result = await proxy.invoke(call.operationId, args, {
          confirmationToken: capability.confirmationRequired ? confirmationToken : undefined,
        })
        results.push(`✅ ${call.operationId}: ${JSON.stringify(result).slice(0, 500)}`)
      } catch (err) {
        if (err instanceof HostProxyError) {
          if (err.code === 'confirmation_required') {
            const token = err.confirmationToken
            results.push(
              `⚠️ ${call.operationId} requires confirmation.\n` +
                `Re-invoke execute() with confirmationToken: "${token}"`,
            )
          } else {
            results.push(`❌ ${call.operationId}: ${err.message} (${err.code})`)
          }
        } else {
          results.push(`❌ ${call.operationId}: unexpected error`)
        }
        break // stop on first error
      }
    }

    return {
      content: [{ type: 'text', text: results.join('\n\n') }],
    }
  }
}
