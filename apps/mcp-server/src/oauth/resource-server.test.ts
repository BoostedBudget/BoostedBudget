import { describe, expect, it } from 'bun:test'
import { buildProtectedResourceMetadata, buildWwwAuthenticate } from './resource-server'

const ENV = {
  MCP_RESOURCE_URL: 'https://mcp.boostedbudget.com',
  CLERK_JWKS_URL: 'https://clerk.boostedbudget.com/.well-known/jwks.json',
}

describe('buildProtectedResourceMetadata', () => {
  it('includes the resource URL', () => {
    const meta = buildProtectedResourceMetadata(ENV)
    expect(meta.resource).toBe('https://mcp.boostedbudget.com')
  })

  it('lists authorization_servers pointing at Clerk JWKS base URL', () => {
    const meta = buildProtectedResourceMetadata(ENV)
    expect(meta.authorization_servers).toHaveLength(1)
    expect(meta.authorization_servers[0]).toBe(
      'https://clerk.boostedbudget.com',
    )
  })

  it('lists supported scopes', () => {
    const meta = buildProtectedResourceMetadata(ENV)
    expect(meta.scopes_supported).toContain('profile')
    expect(meta.scopes_supported).toContain('email')
  })

  it('specifies bearer header method', () => {
    const meta = buildProtectedResourceMetadata(ENV)
    expect(meta.bearer_methods_supported).toContain('header')
  })

  it('returns a plain object (JSON-serializable)', () => {
    const meta = buildProtectedResourceMetadata(ENV)
    expect(() => JSON.stringify(meta)).not.toThrow()
  })
})

describe('buildWwwAuthenticate', () => {
  it('includes Bearer scheme', () => {
    const header = buildWwwAuthenticate('https://mcp.boostedbudget.com', 'invalid_token')
    expect(header).toMatch(/^Bearer /)
  })

  it('includes resource_metadata URL', () => {
    const header = buildWwwAuthenticate('https://mcp.boostedbudget.com', 'invalid_token')
    expect(header).toContain('resource_metadata=')
    expect(header).toContain('mcp.boostedbudget.com')
  })

  it('includes error code when provided', () => {
    const header = buildWwwAuthenticate('https://mcp.boostedbudget.com', 'invalid_token')
    expect(header).toContain('error="invalid_token"')
  })

  it('works without error code', () => {
    const header = buildWwwAuthenticate('https://mcp.boostedbudget.com')
    expect(header).toMatch(/^Bearer /)
    expect(header).not.toContain('error=')
  })
})
