/**
 * PostHog analytics client for the MCP Worker.
 *
 * Emits mcp_* product events for operational visibility.
 * GDPR: user-behavior events are only emitted when the backend's
 * GDPRClient.CaptureIfConsented has already confirmed consent.
 * In the Worker, we emit only system/operational events directly;
 * user-behavior events are proxied through the Go backend.
 *
 * No financial data or PII is ever included in event properties.
 * The host-proxy redaction layer enforces this before analytics are called.
 */

export const MCP_EVENTS = {
  OAUTH_AUTHORIZED: 'mcp_oauth_authorized',
  TOOL_CALLED: 'mcp_tool_called',
  CODE_EXECUTED: 'mcp_code_executed',
  EXECUTE_FAILED: 'mcp_execute_failed',
  SESSION_REVOKED: 'mcp_session_revoked',
} as const

export type McpEvent = (typeof MCP_EVENTS)[keyof typeof MCP_EVENTS]

type CaptureOverride = (
  distinctId: string,
  event: string,
  properties: Record<string, unknown>,
) => Promise<void>

interface PosthogClientOptions {
  /** Override capture for testing */
  captureOverride?: CaptureOverride
}

export interface McpPosthogClient {
  capture(
    distinctId: string,
    event: McpEvent,
    properties?: Record<string, unknown>,
  ): Promise<void>
}

/**
 * Create a PostHog client for the MCP Worker.
 * Returns a no-op client when the write key is empty.
 *
 * Properties must never include financial data or PII —
 * only operational metadata (tool name, operationId, duration, counts).
 */
export function createPosthogClient(
  writeKey: string,
  opts: PosthogClientOptions = {},
): McpPosthogClient {
  if (!writeKey) {
    return {
      capture: () => Promise.resolve(),
    }
  }

  const capture: CaptureOverride = opts.captureOverride ?? (async (distinctId, event, properties) => {
    // PostHog Batch Capture API — works in Cloudflare Workers (no Node.js deps)
    await fetch('https://app.posthog.com/capture/', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        api_key: writeKey,
        event,
        distinct_id: distinctId,
        properties: {
          ...properties,
          $lib: 'mcp-server-worker',
        },
      }),
    }).catch(() => {
      // Fire-and-forget: analytics failures must never break MCP operations
    })
  })

  return {
    capture: (distinctId, event, properties = {}) =>
      capture(distinctId, event, properties),
  }
}
