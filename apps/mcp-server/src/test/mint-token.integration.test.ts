/**
 * Validates the mintAccessToken() helper end-to-end.
 *
 * Scenarios:
 *   1. Helper produces a correctly-formed JWT (Bearer prefix, 3 segments).
 *   2. A request with NO token → 401 (baseline, ensures auth is active).
 *   3. A request with a GARBAGE token → 401.
 *   4. A request with the MINTED token → NOT 401 (auth passed).
 *
 * Tests 2-4 use /.well-known/oauth-protected-resource which does NOT need the
 * MCP_AGENT Durable Object binding, so the Worker returns a proper HTTP response
 * rather than throwing a binding error. Auth is evaluated before any route
 * handler, so a 401-vs-not-401 split proves the token is accepted.
 *
 * Note: /.well-known/oauth-protected-resource is a public endpoint (no auth
 * required). To confirm that auth is actually being exercised we use /mcp, but
 * only check the response status — not the body — to avoid DO binding issues.
 */

import { describe, it, expect, beforeAll } from 'vitest'
import { env } from 'cloudflare:test'
import { exports } from 'cloudflare:workers'
import type { Env } from '../env'
import { mintAccessToken, setupFetchMock } from './helpers/mint-token'

describe('mintAccessToken helper', () => {
  setupFetchMock()

  let bearerToken: string

  beforeAll(async () => {
    bearerToken = await mintAccessToken(env as Env)
  })

  it('returns a string starting with "Bearer "', () => {
    expect(bearerToken).toMatch(/^Bearer /)
  })

  it('the token has three JWT segments (header.payload.signature)', () => {
    const token = bearerToken.replace(/^Bearer /, '')
    const parts = token.split('.')
    expect(parts).toHaveLength(3)
  })

  it('GET /mcp with NO token returns 401', async () => {
    const worker = exports.default as { fetch: (req: Request) => Promise<Response> }
    const res = await worker.fetch(new Request('http://mcp.test/mcp'))
    expect(res.status).toBe(401)
  })

  it('GET /mcp with garbage token returns 401', async () => {
    const worker = exports.default as { fetch: (req: Request) => Promise<Response> }
    const res = await worker.fetch(
      new Request('http://mcp.test/mcp', {
        headers: { Authorization: 'Bearer not.a.valid.token' },
      }),
    )
    expect(res.status).toBe(401)
  })

  it('Authenticated request reaches the auth gate and passes (not 401)', async () => {
    const worker = exports.default as { fetch: (req: Request) => Promise<Response> }
    // Route /no-such-path requires auth (all non-public routes do) but returns
    // 404 (not a DO call), so the Worker handles it with a plain HTTP response.
    // With a bad/missing token → 401. With a valid token → 404 (auth passed).
    const res = await worker.fetch(
      new Request('http://mcp.test/no-such-path', {
        headers: { Authorization: bearerToken },
      }),
    )
    // 401 = auth rejected. 503 = env misconfiguration before auth runs.
    // 404 = auth passed, route not found — exactly what we want.
    expect(res.status).not.toBe(401)
    expect(res.status).not.toBe(503)
    expect(res.status).toBe(404)
  })
})
