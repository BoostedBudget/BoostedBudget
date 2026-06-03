/**
 * GAP-8 test: verify the token handoff mechanism populates props.accessToken
 * correctly so init() never falls back to MISSING_ACCESS_TOKEN.
 *
 * The broken path: onStart() tries to read (this as unknown as { request? }).request
 * but the agents SDK does NOT set this.request in onStart — it takes props, not request.
 *
 * The fix: override fetch() which DOES receive the full request, extract headers
 * there, and call updateProps() before delegating to super.fetch().
 */

import { describe, expect, it } from 'bun:test'
import { extractMcpHeaders, buildPropsFromHeaders } from './agent-auth'

describe('extractMcpHeaders', () => {
  it('extracts all three MCP trusted headers from a request', () => {
    const request = new Request('https://example.com/mcp', {
      headers: {
        'X-MCP-Access-Token': 'clerk.jwt.token.here',
        'X-MCP-Clerk-User-Id': 'user_abc123',
        'X-MCP-Grant-Id': 'grant_xyz',
      },
    })
    const headers = extractMcpHeaders(request)
    expect(headers.accessToken).toBe('clerk.jwt.token.here')
    expect(headers.clerkUserId).toBe('user_abc123')
    expect(headers.grantId).toBe('grant_xyz')
  })

  it('returns null when X-MCP-Access-Token is missing', () => {
    const request = new Request('https://example.com/mcp', {
      headers: {
        'X-MCP-Clerk-User-Id': 'user_abc123',
      },
    })
    const headers = extractMcpHeaders(request)
    expect(headers).toBeNull()
  })

  it('returns null when X-MCP-Clerk-User-Id is missing', () => {
    const request = new Request('https://example.com/mcp', {
      headers: {
        'X-MCP-Access-Token': 'tok',
      },
    })
    const headers = extractMcpHeaders(request)
    expect(headers).toBeNull()
  })

  it('defaults grantId to "unknown" when X-MCP-Grant-Id is absent', () => {
    const request = new Request('https://example.com/mcp', {
      headers: {
        'X-MCP-Access-Token': 'tok',
        'X-MCP-Clerk-User-Id': 'user_1',
      },
    })
    const headers = extractMcpHeaders(request)
    expect(headers?.grantId).toBe('unknown')
  })
})

describe('buildPropsFromHeaders', () => {
  it('builds McpAgentProps from extracted headers', () => {
    const props = buildPropsFromHeaders({
      accessToken: 'clerk.jwt.abc',
      clerkUserId: 'user_123',
      grantId: 'grant_456',
    })
    expect(props.accessToken).toBe('clerk.jwt.abc')
    expect(props.clerkUserId).toBe('user_123')
    expect(props.grantId).toBe('grant_456')
  })

  it('ensures accessToken is NOT the sentinel value', () => {
    const props = buildPropsFromHeaders({
      accessToken: 'real-token',
      clerkUserId: 'u',
      grantId: 'g',
    })
    expect(props.accessToken).not.toBe('MISSING_ACCESS_TOKEN')
  })

  it('produces a different token on the second request with a refreshed token', () => {
    // GAP-R3-11: the DO fetch() override must call updateProps() on EVERY request
    // (no !this.props?.accessToken guard) so refreshed tokens are picked up.
    // This test verifies extractMcpHeaders + buildPropsFromHeaders correctly models the
    // per-request header→props pipeline; the actual guard removal is in agent.ts fetch().
    const firstRequest = new Request('https://example.com/mcp', {
      headers: {
        'X-MCP-Access-Token': 'token-v1',
        'X-MCP-Clerk-User-Id': 'user_1',
        'X-MCP-Grant-Id': 'grant_1',
      },
    })
    const secondRequest = new Request('https://example.com/mcp', {
      headers: {
        'X-MCP-Access-Token': 'token-v2-refreshed',
        'X-MCP-Clerk-User-Id': 'user_1',
        'X-MCP-Grant-Id': 'grant_1',
      },
    })
    const props1 = buildPropsFromHeaders(extractMcpHeaders(firstRequest)!)
    const props2 = buildPropsFromHeaders(extractMcpHeaders(secondRequest)!)
    expect(props1.accessToken).toBe('token-v1')
    expect(props2.accessToken).toBe('token-v2-refreshed')
    expect(props1.accessToken).not.toBe(props2.accessToken)
  })
})
