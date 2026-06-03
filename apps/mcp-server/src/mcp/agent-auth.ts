/**
 * MCP Agent auth helpers — extract trusted X-MCP-* headers from requests.
 *
 * These headers are set exclusively by index.ts AFTER validating the Clerk OAuth
 * token via JWKS. They are deleted-then-set (injection-safe) before forwarding
 * to the Durable Object, so the DO can trust their values unconditionally.
 *
 * Extracted in the DurableObject's fetch() override (not onStart) because
 * the agents SDK onStart() receives props, NOT the original HTTP request.
 */

import type { McpAgentProps } from './agent'

interface ExtractedHeaders {
  accessToken: string
  clerkUserId: string
  grantId: string
}

/**
 * Extract the three trusted MCP headers from an incoming DO request.
 * Returns null if the required headers are absent (non-MCP request or unauthenticated).
 */
export function extractMcpHeaders(request: Request): ExtractedHeaders | null {
  const accessToken = request.headers.get('X-MCP-Access-Token')
  const clerkUserId = request.headers.get('X-MCP-Clerk-User-Id')
  if (!accessToken || !clerkUserId) return null

  return {
    accessToken,
    clerkUserId,
    grantId: request.headers.get('X-MCP-Grant-Id') ?? 'unknown',
  }
}

/**
 * Build McpAgentProps from extracted header values.
 * The resulting props are stored via updateProps() so init() can use them.
 */
export function buildPropsFromHeaders(headers: ExtractedHeaders): McpAgentProps {
  return {
    clerkUserId: headers.clerkUserId,
    grantId: headers.grantId,
    accessToken: headers.accessToken,
  }
}
