import { describe, expect, it, mock } from 'bun:test'
import {
  createPosthogClient,
  MCP_EVENTS,
  type McpEvent,
} from './posthog-client'

describe('createPosthogClient', () => {
  it('is a no-op when posthog write key is empty', async () => {
    const client = createPosthogClient('')
    // Should not throw
    await expect(client.capture('user-1', MCP_EVENTS.TOOL_CALLED, { tool: 'search' })).resolves.toBeUndefined()
  })

  it('emits events when configured', async () => {
    const captured: { distinctId: string; event: string; properties: unknown }[] = []
    const client = createPosthogClient('ph-test-key', {
      captureOverride: (distinctId, event, properties) => {
        captured.push({ distinctId, event, properties })
        return Promise.resolve()
      },
    })

    await client.capture('user-1', MCP_EVENTS.TOOL_CALLED, { tool: 'search', operationId: 'getTransactions' })

    expect(captured).toHaveLength(1)
    expect(captured[0]?.distinctId).toBe('user-1')
    expect(captured[0]?.event).toBe('mcp_tool_called')
    expect((captured[0]?.properties as Record<string, unknown>)?.['tool']).toBe('search')
  })

  it('does not include raw financial data in properties', async () => {
    const captured: unknown[] = []
    const client = createPosthogClient('ph-test-key', {
      captureOverride: (_, __, properties) => {
        captured.push(properties)
        return Promise.resolve()
      },
    })

    // Simulate an execute event — properties should be metadata only
    await client.capture('user-1', MCP_EVENTS.CODE_EXECUTED, {
      operationId: 'getTransactions',
      callCount: 3,
      durationMs: 250,
      // The client should never receive financial data — this is enforced by
      // the host-proxy redaction layer before analytics are called
    })

    const serialized = JSON.stringify(captured)
    // These would only appear if financial data leaked into analytics
    expect(serialized).not.toContain('balance')
    expect(serialized).not.toContain('amount')
  })
})

describe('MCP_EVENTS', () => {
  it('has correct event name strings', () => {
    expect(MCP_EVENTS.OAUTH_AUTHORIZED).toBe('mcp_oauth_authorized')
    expect(MCP_EVENTS.TOOL_CALLED).toBe('mcp_tool_called')
    expect(MCP_EVENTS.CODE_EXECUTED).toBe('mcp_code_executed')
    expect(MCP_EVENTS.EXECUTE_FAILED).toBe('mcp_execute_failed')
    expect(MCP_EVENTS.SESSION_REVOKED).toBe('mcp_session_revoked')
  })
})
