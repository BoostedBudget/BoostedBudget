/**
 * Test helpers for authenticated MCP integration tests.
 *
 * mintAccessToken(worker, env): Generates a valid OAuthProvider opaque access
 * token (oat_...) by:
 *   1. Using getOAuthApi to register a test OAuth client in the in-memory KV
 *   2. Constructing an AuthRequest and completing authorization directly
 *      (bypasses the Clerk redirect flow — stores clerkUserId + fake accessToken
 *      as props in the grant)
 *   3. Extracting the authorization code from the redirect URL
 *   4. Exchanging the code for an opaque token via POST /oauth/token on the Worker
 *
 * The resulting token is an OAuthProvider opaque token (starts with oat_ or similar,
 * NOT a JWT), which is what the Worker's OAuthProvider validates on every MCP request.
 *
 * setupFetchMock(): Registers beforeEach/afterEach hooks that install a global
 *   fetch spy intercepting:
 *   - Clerk token introspection endpoint (assertNotRevoked in revocation.ts calls this)
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
 *       const worker = exports.default as { fetch: (req: Request) => Promise<Response> }
 *       bearerToken = await mintAccessToken(worker, env as Env)
 *     })
 *
 *     it('...', async () => {
 *       const res = await worker.fetch(new Request('http://mcp.test/mcp', {
 *         headers: { Authorization: bearerToken },
 *       }))
 *     })
 *   })
 */

import { beforeEach, afterEach, vi } from 'vitest'
import { getOAuthApi } from '@cloudflare/workers-oauth-provider'
import { oauthOptions } from '../../index'
import type { Env } from '../../env'

// --- Types -------------------------------------------------------------------

type WorkerFetch = { fetch: (req: Request) => Promise<Response> }

// --- Fetch interceptor -------------------------------------------------------
// We intercept globalThis.fetch because the Worker runs in the same isolate as
// tests (same V8 context). vi.spyOn captures the Workers runtime fetch and lets
// us return test responses for known URLs while forwarding everything else.
//
// Note: the spy is only active when setupFetchMock() is used (via beforeAll).

type FetchInput = RequestInfo | URL
type FetchInit = RequestInit | undefined

/**
 * Handle the shared base URLs (Clerk introspection, audit, JWKS) that every
 * integration test needs intercepted. Returns a `Response` if the URL was
 * handled, or `null` if the caller should continue to its own interceptors.
 *
 * Exported so that per-test extended fetch spies (e.g. in transport.integration.test.ts)
 * can delegate their fallback path here instead of duplicating the conditionals.
 */
export function handleBaseUrls(url: string, method: string): Response | null {
  // JWKS endpoint — clerk-verifier.ts fetches this during the Clerk redirect flow.
  // NOT called on the API path (OAuthProvider validates opaque tokens, not JWTs).
  // Intercept here so an accidental JWKS call fails immediately with a clear message
  // instead of a cryptic signature-verification error from an empty keyset.
  if (url.includes('/.well-known/jwks.json')) {
    throw new TypeError(
      `[setupFetchMock] Unexpected JWKS fetch — clerk-verifier.ts should not be called in integration tests (OAuthProvider uses opaque tokens). URL: ${url}`,
    )
  }

  // Clerk token introspection / revocation endpoint.
  // revocation.ts calls POST https://api.clerk.com/oauth_applications/access_tokens/verify
  // on every MCP request (cache miss). Return verified_active so assertNotRevoked passes.
  if (url.includes('api.clerk.com/oauth_applications/access_tokens/verify') && method === 'POST') {
    return new Response(
      JSON.stringify({ id: 'oat_test_clerk_access_token', revoked: false, expired: false }),
      { status: 200, headers: { 'Content-Type': 'application/json' } },
    )
  }

  // Backend audit endpoint — fire-and-forget; must not block or error.
  if (url.includes('backend.test') && url.includes('/api/v1/mcp/audit') && method === 'POST') {
    return new Response('{}', { status: 200, headers: { 'Content-Type': 'application/json' } })
  }

  // Backend product-telemetry endpoint — tool responses never depend on this best-effort call.
  if (url.includes('backend.test') && url.includes('/api/v1/mcp/telemetry') && method === 'POST') {
    return new Response(null, { status: 204 })
  }

  return null
}

function extractUrlAndMethod(input: FetchInput, init?: FetchInit): [string, string] {
  const url =
    input instanceof Request ? input.url : input instanceof URL ? input.href : String(input)
  const method = (input instanceof Request ? input.method : init?.method ?? 'GET').toUpperCase()
  return [url, method]
}

function buildFetchSpy() {
  // Build a response factory for test URLs.
  async function mockFetch(input: FetchInput, init?: FetchInit): Promise<Response> {
    const [url, method] = extractUrlAndMethod(input, init)
    const baseResponse = handleBaseUrls(url, method)
    if (baseResponse !== null) return baseResponse

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
 * Generate an RFC 7636 S256 PKCE pair.
 *
 * Required, not optional: workers-oauth-provider refuses `response_type=code`
 * from a public client (`tokenEndpointAuthMethod: 'none'`) that sends no
 * code_challenge — see oauth-provider.js, "Public clients must use PKCE with
 * the authorization code flow." Only S256 is accepted unless the provider is
 * built with `allowPlainPKCE`, which we deliberately do not set.
 *
 * The verifier is the unreserved-character set per RFC 7636 §4.1 (43-128 chars);
 * the challenge is BASE64URL(SHA256(ASCII(verifier))) per §4.2.
 */
async function createPkcePair(): Promise<{ verifier: string; challenge: string }> {
  const bytes = crypto.getRandomValues(new Uint8Array(32))
  // base64url with padding stripped — a verifier of 32 bytes yields 43 chars.
  const verifier = btoa(String.fromCharCode(...bytes))
    .replace(/\+/g, '-')
    .replace(/\//g, '_')
    .replace(/=+$/, '')

  const digest = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(verifier))
  const challenge = btoa(String.fromCharCode(...new Uint8Array(digest)))
    .replace(/\+/g, '-')
    .replace(/\//g, '_')
    .replace(/=+$/, '')

  return { verifier, challenge }
}

/**
 * Produce an OAuthProvider opaque access token that the Worker's OAuthProvider will accept.
 *
 * The flow bypasses the Clerk redirect — we call getOAuthApi() directly to register
 * a client and complete authorization, then exchange the code for a token via
 * POST /oauth/token on the real Worker.
 *
 * @param worker - The exported default Worker (from 'cloudflare:workers' exports.default)
 * @param env    - Worker env bindings (from vitest.config.ts miniflare bindings)
 * @returns "Bearer <opaque-token>" string ready for use as an Authorization header
 *
 * IMPORTANT: `setupFetchMock()` must be active before calling this function — the
 * Clerk revocation gate runs on the first authenticated MCP request and requires
 * the api.clerk.com interceptor to be in place.
 */
export async function mintAccessToken(worker: WorkerFetch, env: Env): Promise<string> {
  const helpers = getOAuthApi(oauthOptions, env)

  // 1. Register a test OAuth client (public client — no secret needed)
  const client = await helpers.createClient({
    redirectUris: ['http://mcp.test/callback'],
    tokenEndpointAuthMethod: 'none',
    clientName: 'test-client',
  })

  // 2. Parse a fabricated AuthRequest by calling the authorize endpoint
  //    with a minimal query string. parseAuthRequest reads from the URL params.
  const authorizeUrl = new URL('http://mcp.test/oauth/authorize')
  authorizeUrl.searchParams.set('response_type', 'code')
  authorizeUrl.searchParams.set('client_id', client.clientId)
  authorizeUrl.searchParams.set('redirect_uri', 'http://mcp.test/callback')
  authorizeUrl.searchParams.set('scope', 'openid email profile')
  authorizeUrl.searchParams.set('state', crypto.randomUUID())

  // PKCE is mandatory for this client: it is registered with
  // tokenEndpointAuthMethod 'none', which the provider treats as a public
  // client and therefore requires a code_challenge. Sending it here (and the
  // matching verifier at the token endpoint below) mirrors what a real MCP
  // client does, rather than downgrading the fixture to a confidential client
  // that production never creates.
  const pkce = await createPkcePair()
  authorizeUrl.searchParams.set('code_challenge', pkce.challenge)
  authorizeUrl.searchParams.set('code_challenge_method', 'S256')

  const authRequest = await helpers.parseAuthRequest(new Request(authorizeUrl.toString()))

  // 3. Complete authorization directly — store clerkUserId + fake Clerk accessToken as props.
  //    This is the step that normally happens after Clerk authenticates the user.
  const { redirectTo } = await helpers.completeAuthorization({
    request: authRequest,
    userId: 'test-user-id',
    metadata: { clerkUserId: 'test-user-id' },
    scope: ['openid', 'email', 'profile'],
    props: {
      clerkUserId: 'test-user-id',
      // Fake Clerk access token stored in props — used by McpApiHandler for backend calls
      // and checked by assertNotRevoked() (intercepted by setupFetchMock).
      accessToken: 'oat_test_clerk_access_token',
    },
  })

  // 4. Extract the authorization code from the redirect URL
  const redirectUrl = new URL(redirectTo)
  const code = redirectUrl.searchParams.get('code')
  if (!code) throw new Error(`No code in redirectTo: ${redirectTo}`)

  // 5. Exchange code for opaque OAuthProvider access token via the Worker
  const tokenRes = await worker.fetch(
    new Request('http://mcp.test/oauth/token', {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: new URLSearchParams({
        grant_type: 'authorization_code',
        client_id: client.clientId,
        code,
        redirect_uri: 'http://mcp.test/callback',
        // Must match the code_challenge sent to /oauth/authorize above.
        code_verifier: pkce.verifier,
      }).toString(),
    }),
  )

  if (!tokenRes.ok) {
    const body = await tokenRes.text()
    throw new Error(`Token exchange failed ${tokenRes.status}: ${body}`)
  }

  const { access_token } = (await tokenRes.json()) as { access_token: string }
  if (!access_token) throw new Error('No access_token in token response')
  return `Bearer ${access_token}`
}

/**
 * Register beforeEach/afterEach hooks that manage a global fetch spy.
 *
 * Must be called inside a describe() block (or at the top level of a test file).
 * Only one active call at a time — do NOT nest or call from two describe blocks
 * concurrently, as vi.spyOn wraps globalThis.fetch and the spy/restore order matters.
 *
 * The spy is re-installed before every test and torn down (via vi.restoreAllMocks)
 * after every test. This prevents afterEach(vi.restoreAllMocks) calls in consumer
 * test files from stranding later tests (e.g. SSE smoke) without a fetch interceptor.
 *
 * The spy intercepts:
 *   - JWKS endpoint (throws loudly — should not be reached in integration tests)
 *   - Clerk token introspection / revocation (POST api.clerk.com/.../verify)
 *   - Backend audit endpoint (fire-and-forget after tool calls)
 *
 * Per-tool backend API mocks (e.g. specific tool call responses) should be added
 * as additional vi.spyOn wrappers per-test, or extend the interceptor table in
 * buildFetchSpy() for globally needed interceptors.
 *
 * mintAccessToken() must be called after this mock is active (inside beforeAll or it).
 * Because setupFetchMock uses beforeEach, the spy is active during beforeAll only if
 * mintAccessToken is called from within an it() or a beforeEach that runs after this one.
 * If mintAccessToken is called from beforeAll, install a separate spy in that beforeAll.
 */
export function setupFetchMock(): void {
  beforeEach(() => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(buildFetchSpy())
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })
}
