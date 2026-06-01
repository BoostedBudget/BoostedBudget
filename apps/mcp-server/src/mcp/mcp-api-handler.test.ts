/**
 * Tests for the revocation gate in McpApiHandler.
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
const TEST_USER = 'user_test_123'
const TEST_ENV = {
  CLERK_SECRET_KEY: 'sk_test_abc123',
  MCP_RESOURCE_URL: 'https://staging-mcp.boostedbudget.com',
  BOOSTEDBUDGET_API_URL: 'https://staging-api.boostedbudget.com',
  BOOSTEDBUDGET_ORIGIN_SECRET: 'test_origin_secret',
  MCP_WORKER_SECRET: 'test_worker_secret',
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
    const result = await checkRevocationOrRespond({ accessToken: TEST_TOKEN, clerkUserId: TEST_USER }, TEST_ENV)
    expect(result).toBeNull()
  })

  it('returns 401 response when Clerk confirms token is revoked', async () => {
    globalThis.fetch = asFetch(mock(() => Promise.resolve(inactiveResponse())))
    const result = await checkRevocationOrRespond({ accessToken: TEST_TOKEN, clerkUserId: TEST_USER }, TEST_ENV)
    expect(result).not.toBeNull()
    expect(result?.status).toBe(401)
  })

  it('401 response body has error field', async () => {
    globalThis.fetch = asFetch(mock(() => Promise.resolve(inactiveResponse())))
    const result = await checkRevocationOrRespond({ accessToken: TEST_TOKEN, clerkUserId: TEST_USER }, TEST_ENV)
    const body = (await result?.json()) as { error?: string }
    expect(body?.error).toBeTruthy()
  })

  it('401 response includes WWW-Authenticate header with invalid_token', async () => {
    globalThis.fetch = asFetch(mock(() => Promise.resolve(inactiveResponse())))
    const result = await checkRevocationOrRespond({ accessToken: TEST_TOKEN, clerkUserId: TEST_USER }, TEST_ENV)
    const wwwAuth = result?.headers.get('WWW-Authenticate')
    expect(wwwAuth).toContain('invalid_token')
  })

  it('returns null (fail-open) when Clerk returns 5xx', async () => {
    globalThis.fetch = asFetch(
      mock(() => Promise.resolve(new Response('error', { status: 500 }))),
    )
    const result = await checkRevocationOrRespond({ accessToken: TEST_TOKEN, clerkUserId: TEST_USER }, TEST_ENV)
    expect(result).toBeNull()
  })

  it('returns null (fail-open) on network error', async () => {
    globalThis.fetch = asFetch(mock(() => Promise.reject(new Error('network'))))
    const result = await checkRevocationOrRespond({ accessToken: TEST_TOKEN, clerkUserId: TEST_USER }, TEST_ENV)
    expect(result).toBeNull()
  })
})

describe('checkRevocationOrRespond — audit on revoked 401', () => {
  it('fires a best-effort audit entry with result:denied on revoked 401', async () => {
    // Mock: Clerk returns inactive; audit endpoint returns 204.
    // The mock distinguishes calls by URL (Clerk vs boostedbudget audit).
    const fetchSpy = mock((url: string) => {
      if ((url as string).includes('clerk.com')) {
        return Promise.resolve(inactiveResponse())
      }
      return Promise.resolve(new Response(null, { status: 204 }))
    })
    globalThis.fetch = asFetch(fetchSpy)

    const result = await checkRevocationOrRespond({ accessToken: TEST_TOKEN, clerkUserId: TEST_USER }, TEST_ENV)
    expect(result?.status).toBe(401)

    // Allow the fire-and-forget audit Promise to resolve
    await new Promise(r => setTimeout(r, 10))

    // Verify audit was fired (two fetch calls: Clerk verify + audit POST)
    expect(fetchSpy.mock.calls.length).toBe(2)
    const [auditUrl, auditInit] = (fetchSpy.mock.calls[1] as unknown) as [string, RequestInit]
    expect(auditUrl).toContain('/api/v1/mcp/audit')
    const auditBody = JSON.parse(auditInit.body as string) as {
      result: string
      revocation_state: string
      actor_user_id: string
      operation_id: string
    }
    expect(auditBody.result).toBe('denied')
    expect(auditBody.revocation_state).toBe('revoked')
    expect(auditBody.actor_user_id).toBe(TEST_USER)
    expect(auditBody.operation_id).toBe('mcp.revocation_denied')
  })

  it('does NOT fire audit when token is active (null result)', async () => {
    const fetchSpy = mock(() => Promise.resolve(activeResponse()))
    globalThis.fetch = asFetch(fetchSpy)

    await checkRevocationOrRespond({ accessToken: TEST_TOKEN, clerkUserId: TEST_USER }, TEST_ENV)
    await new Promise(r => setTimeout(r, 10))

    // Only one fetch call — the Clerk verify. No audit for active tokens.
    expect(fetchSpy.mock.calls.length).toBe(1)
  })

  it('still returns 401 when audit env is not configured (audit is no-op)', async () => {
    // createAuditClient returns a no-op when audit fields are empty/absent.
    // The revoked 401 must still be returned regardless.
    globalThis.fetch = asFetch(mock(() => Promise.resolve(inactiveResponse())))
    const noAuditEnv = { ...TEST_ENV, BOOSTEDBUDGET_API_URL: '', BOOSTEDBUDGET_ORIGIN_SECRET: '', MCP_WORKER_SECRET: '' }
    const result = await checkRevocationOrRespond({ accessToken: TEST_TOKEN, clerkUserId: TEST_USER }, noAuditEnv)
    expect(result?.status).toBe(401)
  })

  it('audit failure does not affect the 401 response (fire-and-forget)', async () => {
    const fetchSpy = mock((url: string) => {
      if ((url as string).includes('clerk.com')) return Promise.resolve(inactiveResponse())
      // Audit endpoint fails
      return Promise.reject(new Error('audit failure'))
    })
    globalThis.fetch = asFetch(fetchSpy)

    // 401 is returned despite the audit POST failing
    const result = await checkRevocationOrRespond({ accessToken: TEST_TOKEN, clerkUserId: TEST_USER }, TEST_ENV)
    expect(result?.status).toBe(401)
  })
})
