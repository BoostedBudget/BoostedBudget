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

import { MCP_EVENTS, type McpEvent } from './events.generated'

export { MCP_EVENTS }
export type { McpEvent }

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

/** Create a PostHog client; no-op when writeKey is empty */
export function createPosthogClient(
  writeKey: string,
  opts: PosthogClientOptions = {},
): McpPosthogClient {
  if (!writeKey) {
    return { capture: () => Promise.resolve() }
  }

  const capture: CaptureOverride = opts.captureOverride ?? (async (distinctId, event, properties) => {
    await fetch('https://app.posthog.com/capture/', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        api_key: writeKey,
        event,
        distinct_id: distinctId,
        properties: { ...properties, $lib: 'mcp-server-worker' },
      }),
    }).catch((err: unknown) => {
      const errType = err instanceof Error ? err.constructor.name : typeof err
      const status = err instanceof Error && 'status' in err ? (err as { status: number }).status : 'unknown'
      console.warn('[mcp-analytics] capture failed (fire-and-forget)', { errType, status, event })
    })
  })

  return { capture: (distinctId, event, properties = {}) => capture(distinctId, event, properties) }
}
