/**
 * Tests for assertNotRevoked / introspectClerkToken.
 *
 * Clerk verify endpoint contract (confirmed by Step-0 staging spike, 2026-06-02):
 *   POST https://api.clerk.com/oauth_applications/access_tokens/verify
 *   Authorization: Bearer <CLERK_SECRET_KEY>
 *   { "access_token": "..." }
 *
 * Response mapping:
 *   HTTP 200 + { id, revoked: false, expired: false } → verified_active
 *   HTTP 200 + { revoked: true }  OR { expired: true } → revoked
 *   HTTP 404 + { errors: [{ code: "oauth_access_token_not_found" }] } → revoked
 *   5xx / network / timeout / missing secret → unknown_fail_open (fail-open)
 *   Other 4xx or unexpected 200 shape       → unknown_fail_open (fail-open)
 */

import { describe, expect, it, beforeEach, afterEach, mock, setSystemTime } from 'bun:test'
import { assertNotRevoked, markRevoked, clearRevocationCache, computeCacheKey } from './revocation'
import { TokenVerificationError } from './clerk-verifier'

// ─── Test fixtures ────────────────────────────────────────────────────────────

const TEST_TOKEN = 'eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.test.sig'
const TEST_ENV = { CLERK_SECRET_KEY: 'sk_test_abc123' }

function asFetch(m: ReturnType<typeof mock>) {
  return m as unknown as typeof globalThis.fetch
}

// Real Clerk BAPI response shape for an active token (spike-verified 2026-06-02)
function activeResponse(): Response {
  return new Response(
    JSON.stringify({ id: 'oat_test123', revoked: false, expired: false, expiration: 9999999999 }),
    { status: 200, headers: { 'Content-Type': 'application/json' } },
  )
}

// Real Clerk BAPI response shape after revocation (spike-verified 2026-06-02):
// HTTP 404 with oauth_access_token_not_found error code
function revokedResponse(): Response {
  return new Response(
    JSON.stringify({ errors: [{ message: 'OAuth Access Token not found', code: 'oauth_access_token_not_found' }] }),
    { status: 404, headers: { 'Content-Type': 'application/json' } },
  )
}

function serverErrorResponse(): Response {
  return new Response('Internal Server Error', { status: 500 })
}

beforeEach(() => {
  clearRevocationCache()
  globalThis.fetch = asFetch(mock(() => Promise.resolve(activeResponse())))
})

afterEach(() => {
  // Reset any setSystemTime() calls so clock manipulation doesn't bleed between tests.
  setSystemTime()
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
    globalThis.fetch = asFetch(mock(() => Promise.resolve(revokedResponse())))
    await expect(assertNotRevoked({ accessToken: TEST_TOKEN }, TEST_ENV)).rejects.toBeInstanceOf(
      TokenVerificationError,
    )
  })

  it('error code is invalid_token when Clerk returns inactive', async () => {
    globalThis.fetch = asFetch(mock(() => Promise.resolve(revokedResponse())))
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
    const fetchSpy = mock(() => Promise.resolve(revokedResponse()))
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

  it('re-fetches after verified_active TTL expires (30s)', async () => {
    const fetchSpy = mock(() => Promise.resolve(activeResponse()))
    globalThis.fetch = asFetch(fetchSpy)
    // First call → Clerk (cache miss)
    await assertNotRevoked({ accessToken: TEST_TOKEN }, TEST_ENV)
    expect(fetchSpy.mock.calls.length).toBe(1)
    // Second call within TTL → cache hit (no re-fetch)
    await assertNotRevoked({ accessToken: TEST_TOKEN }, TEST_ENV)
    expect(fetchSpy.mock.calls.length).toBe(1)
    // Advance time past CACHE_TTLS.verified_active (30 000ms)
    setSystemTime(Date.now() + 31_000)
    // Third call — TTL expired → re-fetches from Clerk
    await assertNotRevoked({ accessToken: TEST_TOKEN }, TEST_ENV)
    expect(fetchSpy.mock.calls.length).toBe(2)
  })

  // Spike result (2026-06-02): HTTP 404 with oauth_access_token_not_found is the
  // revocation signal — mapped to 'revoked' (fail-CLOSED), not fail-open.
  it('throws on HTTP 404 oauth_access_token_not_found (revocation signal)', async () => {
    globalThis.fetch = asFetch(mock(() => Promise.resolve(revokedResponse())))
    await expect(assertNotRevoked({ accessToken: TEST_TOKEN }, TEST_ENV)).rejects.toBeInstanceOf(
      TokenVerificationError,
    )
  })

  it('fails open on other 4xx (non-revocation errors, e.g. 400/429)', async () => {
    globalThis.fetch = asFetch(
      mock(() => Promise.resolve(new Response(JSON.stringify({ error: 'rate_limited' }), { status: 429 }))),
    )
    await expect(assertNotRevoked({ accessToken: TEST_TOKEN }, TEST_ENV)).resolves.toBeUndefined()
  })

  it('fails open on HTTP 404 with unknown error code (not a revocation signal)', async () => {
    globalThis.fetch = asFetch(
      mock(() => Promise.resolve(new Response(
        JSON.stringify({ errors: [{ code: 'some_other_error' }] }),
        { status: 404 },
      ))),
    )
    await expect(assertNotRevoked({ accessToken: TEST_TOKEN }, TEST_ENV)).resolves.toBeUndefined()
  })

  it('throws on HTTP 200 with revoked:true (explicit revoked flag in body)', async () => {
    globalThis.fetch = asFetch(
      mock(() => Promise.resolve(new Response(
        JSON.stringify({ id: 'oat_test', revoked: true, expired: false }),
        { status: 200, headers: { 'Content-Type': 'application/json' } },
      ))),
    )
    await expect(assertNotRevoked({ accessToken: TEST_TOKEN }, TEST_ENV)).rejects.toBeInstanceOf(
      TokenVerificationError,
    )
  })

  it('throws on HTTP 200 with expired:true', async () => {
    globalThis.fetch = asFetch(
      mock(() => Promise.resolve(new Response(
        JSON.stringify({ id: 'oat_test', revoked: false, expired: true }),
        { status: 200, headers: { 'Content-Type': 'application/json' } },
      ))),
    )
    await expect(assertNotRevoked({ accessToken: TEST_TOKEN }, TEST_ENV)).rejects.toBeInstanceOf(
      TokenVerificationError,
    )
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
