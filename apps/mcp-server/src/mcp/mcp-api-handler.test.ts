/**
 * Tests for the revocation check in McpApiHandler.
 *
 * McpApiHandler extends WorkerEntrypoint<Env> which requires the Cloudflare
 * Workers runtime — it cannot be instantiated in bun:test (see GAP-R3-20).
 * We therefore extract the revocation gate logic into a standalone helper
 * (checkRevocationOrRespond) that is pure and unit-testable.
 *
 * The full handler path (revoked → 401 response returned to client) is
 * validated by the staging E2E test described in issue #1224's verification plan.
 */

import { describe, expect, it, beforeEach, mock } from 'bun:test'
import { checkRevocationOrRespond } from './revocation-gate'
import { clearRevocationCache } from '../oauth/revocation'

// ─── Test fixtures ────────────────────────────────────────────────────────────

const TEST_TOKEN = 'eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.test.sig'
const TEST_ENV = {
  CLERK_SECRET_KEY: 'sk_test_abc123',
  MCP_RESOURCE_URL: 'https://staging-mcp.boostedbudget.com',
}

function asFetch(m: ReturnType<typeof mock>) {
  return m as unknown as typeof globalThis.fetch
}

function activeResponse(): Response {
  return new Response(JSON.stringify({ active: true }), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  })
}

function inactiveResponse(): Response {
  return new Response(JSON.stringify({ active: false }), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  })
}

beforeEach(() => {
  clearRevocationCache()
  globalThis.fetch = asFetch(mock(() => Promise.resolve(activeResponse())))
})

describe('checkRevocationOrRespond', () => {
  it('returns null (allow) when Clerk confirms token is active', async () => {
    globalThis.fetch = asFetch(mock(() => Promise.resolve(activeResponse())))
    const result = await checkRevocationOrRespond(TEST_TOKEN, TEST_ENV)
    expect(result).toBeNull()
  })

  it('returns 401 response when Clerk confirms token is revoked', async () => {
    globalThis.fetch = asFetch(mock(() => Promise.resolve(inactiveResponse())))
    const result = await checkRevocationOrRespond(TEST_TOKEN, TEST_ENV)
    expect(result).not.toBeNull()
    expect(result?.status).toBe(401)
  })

  it('401 response body has error field', async () => {
    globalThis.fetch = asFetch(mock(() => Promise.resolve(inactiveResponse())))
    const result = await checkRevocationOrRespond(TEST_TOKEN, TEST_ENV)
    const body = (await result?.json()) as { error?: string }
    expect(body?.error).toBeTruthy()
  })

  it('401 response includes WWW-Authenticate header', async () => {
    globalThis.fetch = asFetch(mock(() => Promise.resolve(inactiveResponse())))
    const result = await checkRevocationOrRespond(TEST_TOKEN, TEST_ENV)
    const wwwAuth = result?.headers.get('WWW-Authenticate')
    expect(wwwAuth).toContain('invalid_token')
  })

  it('returns null (fail-open) when Clerk returns 5xx', async () => {
    globalThis.fetch = asFetch(
      mock(() => Promise.resolve(new Response('error', { status: 500 }))),
    )
    const result = await checkRevocationOrRespond(TEST_TOKEN, TEST_ENV)
    expect(result).toBeNull()
  })

  it('returns null (fail-open) on network error', async () => {
    globalThis.fetch = asFetch(mock(() => Promise.reject(new Error('network'))))
    const result = await checkRevocationOrRespond(TEST_TOKEN, TEST_ENV)
    expect(result).toBeNull()
  })
})
