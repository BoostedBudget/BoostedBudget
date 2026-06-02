/**
 * Test helpers for authenticated MCP integration tests.
 *
 * mintAccessToken(env): Generates a valid Clerk-style JWT by:
 *   1. Creating a fresh RSA-PS256 key pair (generated once per test run)
 *   2. Intercepting the JWKS endpoint via a fetch spy so the Worker's jose
 *      verifier accepts the test key
 *   3. Signing a JWT with the correct iss/aud/sub/sid/scope claims
 *
 * setupFetchMock(): Registers beforeAll/afterAll hooks that install a global
 *   fetch spy intercepting:
 *   - JWKS endpoint  (required for every token verification)
 *   - Clerk token introspection endpoint (defensive, in case revocation adds HTTP)
 *   - Backend audit endpoint (fire-and-forget after tool calls)
 *
 * Usage in a test file:
 *
 *   import { env } from 'cloudflare:test'
 *   import { exports } from 'cloudflare:workers'
 *   import { mintAccessToken, setupFetchMock } from './test/helpers/mint-token'
 *
 *   describe('Authenticated test', () => {
 *     setupFetchMock()
 *
 *     let bearerToken: string
 *     beforeAll(async () => {
 *       bearerToken = await mintAccessToken(env as Env)
 *     })
 *
 *     it('...', async () => {
 *       const worker = exports.default as { fetch: (req: Request) => Promise<Response> }
 *       const res = await worker.fetch(new Request('http://mcp.test/mcp', {
 *         headers: { Authorization: bearerToken },
 *       }))
 *     })
 *   })
 */

import { beforeAll, afterAll, vi } from 'vitest'
import { generateKeyPair, exportJWK, SignJWT } from 'jose'

// Minimal env shape needed to produce a correctly-signed JWT.
// Matches the fields used by verifyClerkToken() in clerk-verifier.ts.
interface TokenMintEnv {
  CLERK_JWKS_URL: string
  CLERK_ISSUER_URL: string
  MCP_OAUTH_AUDIENCE: string
}

// Module-level key pair: generated once per test run, reused across calls.
let _keyPairPromise: ReturnType<typeof generateKeyPair> | null = null
// The current JWKS JSON to serve from the mocked endpoint.
let _jwksPayload: string = JSON.stringify({ keys: [] })

function getKeyPair() {
  if (!_keyPairPromise) {
    _keyPairPromise = generateKeyPair('PS256')
  }
  return _keyPairPromise
}

// --- Fetch interceptor -------------------------------------------------------
// We intercept globalThis.fetch because the Worker runs in the same isolate as
// tests (same V8 context). vi.spyOn captures the Workers runtime fetch and lets
// us return test responses for known URLs while forwarding everything else.
//
// Note: the spy is only active when setupFetchMock() is used (via beforeAll).

type FetchInput = RequestInfo | URL
type FetchInit = RequestInit | undefined

function buildFetchSpy() {
  // Build a response factory for test URLs.
  async function mockFetch(input: FetchInput, init?: FetchInit): Promise<Response> {
    const url =
      input instanceof Request ? input.url : input instanceof URL ? input.href : String(input)
    const method = (input instanceof Request ? input.method : init?.method ?? 'GET').toUpperCase()

    // JWKS endpoint — jose fetches this to verify the JWT signature.
    if (url.includes('/.well-known/jwks.json')) {
      return new Response(_jwksPayload, {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    }

    // Clerk token introspection / revocation — defensive mock.
    // revocation.ts is currently a local-cache stub (no HTTP call), but if a
    // real introspection call is added this intercept prevents test failures.
    if (url.includes('api.clerk.com') && method === 'POST') {
      return new Response(
        JSON.stringify({ id: 'test-grant-id', revoked: false, expired: false }),
        { status: 200, headers: { 'Content-Type': 'application/json' } },
      )
    }

    // Backend audit endpoint — fire-and-forget; must not block or error.
    if (url.includes('backend.test') && url.includes('/api/v1/mcp/audit') && method === 'POST') {
      return new Response('{}', { status: 200, headers: { 'Content-Type': 'application/json' } })
    }

    // Unintercepted request — throw so tests fail loudly if a new outbound call
    // is added without a corresponding interceptor.
    throw new TypeError(
      `[setupFetchMock] Unintercepted fetch to: ${method} ${url}\n` +
        'Add an interceptor in setupFetchMock() or register it per-test.',
    )
  }

  return mockFetch
}

/**
 * Produce a signed JWT that the Worker's verifyClerkToken() will accept.
 * Requires setupFetchMock() to be active (the JWKS intercept must be in place
 * before jose attempts to fetch the public key).
 *
 * @param env - Worker env bindings (from vitest.config.ts miniflare bindings)
 * @returns "Bearer <jwt>" string ready for use as an Authorization header
 */
export async function mintAccessToken(env: TokenMintEnv): Promise<string> {
  const { privateKey, publicKey } = await getKeyPair()
  const publicJwk = await exportJWK(publicKey)

  // Update the in-memory JWKS with the generated public key.
  // The fetch spy reads _jwksPayload on every JWKS request.
  _jwksPayload = JSON.stringify({
    keys: [{ ...publicJwk, use: 'sig', alg: 'PS256' }],
  })

  const now = Math.floor(Date.now() / 1000)
  const token = await new SignJWT({
    scope: 'openid email profile',
    // sid is used as grantId by clerk-verifier.ts
    sid: 'test-grant-id',
  })
    .setProtectedHeader({ alg: 'PS256' })
    .setSubject('test-user-id')
    .setIssuer(env.CLERK_ISSUER_URL)
    .setAudience(env.MCP_OAUTH_AUDIENCE)
    .setIssuedAt(now)
    .setExpirationTime(now + 3600)
    .sign(privateKey)

  return `Bearer ${token}`
}

/**
 * Register beforeAll/afterAll hooks that manage a global fetch spy.
 *
 * Must be called inside a describe() block (or at the top level of a test file).
 *
 * The spy intercepts:
 *   - JWKS (required for JWT signature verification on every authenticated request)
 *   - Clerk token introspection / revocation (defensive)
 *   - Backend audit endpoint (fire-and-forget after tool calls)
 *
 * Per-tool backend API mocks should use vi.spyOn(globalThis, 'fetch') per-test,
 * or extend the interceptor table in buildFetchSpy().
 */
export function setupFetchMock(): void {
  let spy: ReturnType<typeof vi.spyOn> | null = null

  beforeAll(() => {
    spy = vi.spyOn(globalThis, 'fetch').mockImplementation(buildFetchSpy())
  })

  afterAll(() => {
    spy?.mockRestore()
    spy = null
  })
}
