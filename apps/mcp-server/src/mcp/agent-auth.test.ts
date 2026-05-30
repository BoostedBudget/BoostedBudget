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
})
