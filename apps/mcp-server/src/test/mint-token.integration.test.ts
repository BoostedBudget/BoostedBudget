/**
 * Validates the mintAccessToken() helper end-to-end.
 *
 * Scenarios:
 *   1. Helper produces a correctly-formed opaque token (Bearer prefix, NOT a JWT).
 *   2. A request with NO token → 401 (baseline, ensures auth is active).
 *   3. A request with a GARBAGE token → 401.
 *   4. A request with the MINTED token → NOT 401 (auth passed).
 *
 * Tests 2-4 use /mcp which requires auth. The OAuthProvider layer:
 *   - Returns 401 for missing/invalid tokens (layer 1 validation)
 *   - Passes valid tokens to McpApiHandler (which then calls the DO)
 *
 * For test 4, we assert "not 401" — the request will reach the McpApiHandler
 * and attempt to call the MCP_AGENT DO. If the DO binding works in miniflare,
 * we expect a non-401 response. If the DO binding is missing, we may see a
 * 500/503 — still proves auth was accepted (not rejected with 401).
 */

import { describe, it, expect, beforeAll } from 'vitest'
import { env } from 'cloudflare:test'
import { exports } from 'cloudflare:workers'
import type { Env } from '../env'
import { mintAccessToken, setupFetchMock } from './helpers/mint-token'

describe('mintAccessToken helper', () => {
  setupFetchMock()

  let bearerToken: string
  let worker: { fetch: (req: Request) => Promise<Response> }

  beforeAll(async () => {
    worker = exports.default as { fetch: (req: Request) => Promise<Response> }
    bearerToken = await mintAccessToken(worker, env as Env)
  })

  it('returns a string starting with "Bearer "', () => {
    expect(bearerToken).toMatch(/^Bearer /)
  })

  it('the token is NOT a JWT (opaque token — does not contain exactly two dots)', () => {
    const token = bearerToken.replace(/^Bearer /, '')
    // JWTs have exactly two dots (header.payload.signature).
    // OAuthProvider opaque tokens are base64url-encoded blobs with no dots.
    const parts = token.split('.')
    expect(parts.length).not.toBe(3)
  })

  it('GET /mcp with NO token returns 401', async () => {
    const res = await worker.fetch(new Request('http://mcp.test/mcp'))
    expect(res.status).toBe(401)
  })

  it('GET /mcp with garbage token returns 401', async () => {
    const res = await worker.fetch(
      new Request('http://mcp.test/mcp', {
        headers: { Authorization: 'Bearer not-a-valid-token' },
      }),
    )
    expect(res.status).toBe(401)
  })

  it('Authenticated request is accepted by OAuthProvider (not 401)', async () => {
    const res = await worker.fetch(
      new Request('http://mcp.test/mcp', {
        headers: {
          Authorization: bearerToken,
          // Send MCP initialize request so the DO has a valid protocol message to respond to
          'Content-Type': 'application/json',
          Accept: 'application/json, text/event-stream',
        },
        method: 'POST',
        body: JSON.stringify({
          jsonrpc: '2.0',
          method: 'initialize',
          id: 1,
          params: {
            protocolVersion: '2024-11-05',
            capabilities: {},
            clientInfo: { name: 'test-client', version: '1.0.0' },
          },
        }),
      }),
    )
    // 401 = auth rejected by OAuthProvider. This must NOT happen with a valid minted token.
    // 503 = env misconfiguration. Also must not happen.
    // Any other status (200, 500 from DO error, etc.) proves auth was accepted.
    expect(res.status).not.toBe(401)
    expect(res.status).not.toBe(503)
  })
})
