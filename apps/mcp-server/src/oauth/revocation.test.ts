/**
 * Tests for assertNotRevoked / introspectClerkToken.
 *
 * The Clerk verify endpoint contract is:
 *   POST https://api.clerk.com/oauth_applications/access_tokens/verify
 *   Authorization: Bearer <CLERK_SECRET_KEY>
 *   { "access_token": "..." }
 *
 * Response mapping (provisional — update from Step-0 staging spike results):
 *   2xx { active: true }  → verified_active
 *   2xx { active: false } → revoked
 *   4xx/5xx/network       → unknown_fail_open (fail-open: allow request)
 *
 * ⚠️ These tests prove wiring/caching/fail-open logic.
 * They do NOT prove real grant revocation — only the staging spike does.
 */

import { describe, expect, it, beforeEach, mock } from 'bun:test'
import { assertNotRevoked, markRevoked, clearRevocationCache, computeCacheKey } from './revocation'
import { TokenVerificationError } from './clerk-verifier'

// ─── Test fixtures ────────────────────────────────────────────────────────────

const TEST_TOKEN = 'eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.test.sig'
const TEST_ENV = { CLERK_SECRET_KEY: 'sk_test_abc123' }

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

function serverErrorResponse(): Response {
  return new Response('Internal Server Error', { status: 500 })
}

beforeEach(() => {
  clearRevocationCache()
  globalThis.fetch = asFetch(mock(() => Promise.resolve(activeResponse())))
})

// ─── computeCacheKey ──────────────────────────────────────────────────────────

describe('computeCacheKey', () => {
  it('returns a 64-char hex string (SHA-256)', async () => {
    const key = await computeCacheKey(TEST_TOKEN)
    expect(key).toMatch(/^[0-9a-f]{64}$/)
  })

  it('same token always produces same key', async () => {
    const k1 = await computeCacheKey(TEST_TOKEN)
    const k2 = await computeCacheKey(TEST_TOKEN)
    expect(k1).toBe(k2)
  })

  it('different tokens produce different keys', async () => {
    const k1 = await computeCacheKey(TEST_TOKEN)
    const k2 = await computeCacheKey(TEST_TOKEN + '_other')
    expect(k1).not.toBe(k2)
  })
})

// ─── assertNotRevoked — Clerk API interaction ─────────────────────────────────

describe('assertNotRevoked — Clerk introspection', () => {
  it('resolves (allows) when Clerk returns active', async () => {
    globalThis.fetch = asFetch(mock(() => Promise.resolve(activeResponse())))
    await expect(assertNotRevoked({ accessToken: TEST_TOKEN }, TEST_ENV)).resolves.toBeUndefined()
  })

  it('throws TokenVerificationError when Clerk returns inactive', async () => {
    globalThis.fetch = asFetch(mock(() => Promise.resolve(inactiveResponse())))
    await expect(assertNotRevoked({ accessToken: TEST_TOKEN }, TEST_ENV)).rejects.toBeInstanceOf(
      TokenVerificationError,
    )
  })

  it('error code is invalid_token when Clerk returns inactive', async () => {
    globalThis.fetch = asFetch(mock(() => Promise.resolve(inactiveResponse())))
    let caught: TokenVerificationError | undefined
    try {
      await assertNotRevoked({ accessToken: TEST_TOKEN }, TEST_ENV)
    } catch (e) {
      caught = e as TokenVerificationError
    }
    expect(caught?.code).toBe('invalid_token')
  })

  it('fails open (allows) on Clerk 5xx', async () => {
    globalThis.fetch = asFetch(mock(() => Promise.resolve(serverErrorResponse())))
    await expect(assertNotRevoked({ accessToken: TEST_TOKEN }, TEST_ENV)).resolves.toBeUndefined()
  })

  it('fails open (allows) on unexpected response body shape (schema drift)', async () => {
    // Unexpected body (no `active` field) → fail-open, not cached
    const fetchSpy = mock(() =>
      Promise.resolve(
        new Response(JSON.stringify({ status: 'unknown' }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
      ),
    )
    globalThis.fetch = asFetch(fetchSpy)
    await expect(assertNotRevoked({ accessToken: TEST_TOKEN }, TEST_ENV)).resolves.toBeUndefined()
    // Must NOT cache the result — re-fetches on next call
    await expect(assertNotRevoked({ accessToken: TEST_TOKEN }, TEST_ENV)).resolves.toBeUndefined()
    expect(fetchSpy.mock.calls.length).toBe(2)
  })

  it('fails open (allows) on network error', async () => {
    globalThis.fetch = asFetch(mock(() => Promise.reject(new Error('Network error'))))
    await expect(assertNotRevoked({ accessToken: TEST_TOKEN }, TEST_ENV)).resolves.toBeUndefined()
  })

  it('fails open (allows) on AbortError (request timeout)', async () => {
    globalThis.fetch = asFetch(mock(() => Promise.reject(new DOMException('Aborted', 'AbortError'))))
    await expect(assertNotRevoked({ accessToken: TEST_TOKEN }, TEST_ENV)).resolves.toBeUndefined()
  })

  it('fails open (allows) when CLERK_SECRET_KEY is empty', async () => {
    const fetchSpy = mock(() => Promise.resolve(activeResponse()))
    globalThis.fetch = asFetch(fetchSpy)
    await expect(
      assertNotRevoked({ accessToken: TEST_TOKEN }, { CLERK_SECRET_KEY: '' }),
    ).resolves.toBeUndefined()
    // Must NOT have called Clerk when secret key is missing
    expect(fetchSpy.mock.calls.length).toBe(0)
  })

  it('sends Authorization header with CLERK_SECRET_KEY', async () => {
    const fetchSpy = mock(() => Promise.resolve(activeResponse()))
    globalThis.fetch = asFetch(fetchSpy)
    await assertNotRevoked({ accessToken: TEST_TOKEN }, TEST_ENV)
    const [, init] = (fetchSpy.mock.calls[0] as unknown) as [string, RequestInit]
    const headers = init.headers as Record<string, string>
    expect(headers['Authorization']).toBe(`Bearer ${TEST_ENV.CLERK_SECRET_KEY}`)
  })

  it('sends access_token in request body and calls clerk.com verify URL', async () => {
    const fetchSpy = mock(() => Promise.resolve(activeResponse()))
    globalThis.fetch = asFetch(fetchSpy)
    await assertNotRevoked({ accessToken: TEST_TOKEN }, TEST_ENV)
    const [url, init] = (fetchSpy.mock.calls[0] as unknown) as [string, RequestInit]
    expect(url).toContain('clerk.com')
    expect(url).toContain('access_tokens/verify')
    const body = JSON.parse(init.body as string) as { access_token: string }
    expect(body.access_token).toBe(TEST_TOKEN)
  })
})

// ─── assertNotRevoked — caching behavior ─────────────────────────────────────

describe('assertNotRevoked — caching', () => {
  it('caches verified_active: second call within TTL skips Clerk', async () => {
    const fetchSpy = mock(() => Promise.resolve(activeResponse()))
    globalThis.fetch = asFetch(fetchSpy)
    await assertNotRevoked({ accessToken: TEST_TOKEN }, TEST_ENV)
    await assertNotRevoked({ accessToken: TEST_TOKEN }, TEST_ENV)
    expect(fetchSpy.mock.calls.length).toBe(1)
  })

  it('caches revoked: repeated calls keep throwing without re-fetching', async () => {
    const fetchSpy = mock(() => Promise.resolve(inactiveResponse()))
    globalThis.fetch = asFetch(fetchSpy)
    await expect(assertNotRevoked({ accessToken: TEST_TOKEN }, TEST_ENV)).rejects.toBeInstanceOf(
      TokenVerificationError,
    )
    await expect(assertNotRevoked({ accessToken: TEST_TOKEN }, TEST_ENV)).rejects.toBeInstanceOf(
      TokenVerificationError,
    )
    expect(fetchSpy.mock.calls.length).toBe(1)
  })

  it('does NOT cache unknown_fail_open: re-fetches on next call', async () => {
    const fetchSpy = mock(() => Promise.resolve(serverErrorResponse()))
    globalThis.fetch = asFetch(fetchSpy)
    await assertNotRevoked({ accessToken: TEST_TOKEN }, TEST_ENV)
    await assertNotRevoked({ accessToken: TEST_TOKEN }, TEST_ENV)
    expect(fetchSpy.mock.calls.length).toBe(2)
  })

  it('cache is keyed by token, not shared across different tokens', async () => {
    const fetchSpy = mock(() => Promise.resolve(activeResponse()))
    globalThis.fetch = asFetch(fetchSpy)
    const otherToken = TEST_TOKEN + '_B'
    await assertNotRevoked({ accessToken: TEST_TOKEN }, TEST_ENV)
    await assertNotRevoked({ accessToken: otherToken }, TEST_ENV)
    // Different tokens → two separate Clerk calls
    expect(fetchSpy.mock.calls.length).toBe(2)
  })
})

// ─── markRevoked ─────────────────────────────────────────────────────────────

describe('markRevoked', () => {
  it('pre-marking causes assertNotRevoked to throw without calling Clerk', async () => {
    const fetchSpy = mock(() => Promise.resolve(activeResponse()))
    globalThis.fetch = asFetch(fetchSpy)
    const key = await computeCacheKey(TEST_TOKEN)
    markRevoked(key)
    await expect(assertNotRevoked({ accessToken: TEST_TOKEN }, TEST_ENV)).rejects.toBeInstanceOf(
      TokenVerificationError,
    )
    // Must not have called Clerk — revocation is already in cache
    expect(fetchSpy.mock.calls.length).toBe(0)
  })

  it('error code is invalid_token when pre-marked revoked', async () => {
    const key = await computeCacheKey(TEST_TOKEN)
    markRevoked(key)
    let caught: TokenVerificationError | undefined
    try {
      await assertNotRevoked({ accessToken: TEST_TOKEN }, TEST_ENV)
    } catch (e) {
      caught = e as TokenVerificationError
    }
    expect(caught?.code).toBe('invalid_token')
  })
})

// ─── clearRevocationCache ─────────────────────────────────────────────────────

describe('clearRevocationCache', () => {
  it('resets all cached state', async () => {
    const fetchSpy = mock(() => Promise.resolve(activeResponse()))
    globalThis.fetch = asFetch(fetchSpy)
    await assertNotRevoked({ accessToken: TEST_TOKEN }, TEST_ENV)
    expect(fetchSpy.mock.calls.length).toBe(1)
    clearRevocationCache()
    await assertNotRevoked({ accessToken: TEST_TOKEN }, TEST_ENV)
    expect(fetchSpy.mock.calls.length).toBe(2)
  })
})
