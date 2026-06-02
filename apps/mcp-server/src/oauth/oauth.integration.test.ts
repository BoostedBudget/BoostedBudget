import { describe, expect, it } from 'vitest'
import { exports } from 'cloudflare:workers'

// Scenario 2 — OAuth 401 challenge integration tests.
//
// The Worker itself handles both routes (no external OAuthProvider library):
//   - /.well-known/oauth-protected-resource  →  RFC 9728 metadata document
//   - /mcp without token                     →  401 with WWW-Authenticate challenge

describe('OAuth 401 challenge', () => {
  const worker = exports.default as { fetch: (req: Request) => Promise<Response> }

  it('GET /.well-known/oauth-protected-resource returns 200 with metadata', async () => {
    const response = await worker.fetch(
      new Request('http://mcp.test/.well-known/oauth-protected-resource'),
    )
    expect(response.status).toBe(200)
    const body = (await response.json()) as Record<string, unknown>
    // RFC 9728 fields built by buildProtectedResourceMetadata()
    expect(body).toHaveProperty('resource')
    expect(body).toHaveProperty('authorization_servers')
    expect(body.resource).toBe('http://mcp.test')
    expect(Array.isArray(body.authorization_servers)).toBe(true)
  })

  it('GET /mcp without token returns 401 with WWW-Authenticate', async () => {
    const response = await worker.fetch(new Request('http://mcp.test/mcp'))
    expect(response.status).toBe(401)
    const wwwAuth = response.headers.get('WWW-Authenticate')
    expect(wwwAuth).toBeTruthy()
    // The header must reference the protected-resource metadata URL (RFC 6750 + RFC 9728)
    expect(wwwAuth).toContain('Bearer')
    expect(wwwAuth).toContain('resource_metadata=')
    expect(wwwAuth).toContain('.well-known/oauth-protected-resource')
  })
})
