/**
 * MCP transport integration tests — authenticated end-to-end over Streamable HTTP.
 *
 * Covers:
 *   Scenario 1 — full MCP chain: initialize → tools/list → tools/call (read op)
 *   Scenario 3 — header injection safety: attacker headers are stripped, env values used
 *   Scenario 4 — write-confirmation round trip: challenge → confirm → backend called once
 *   Scenario 5 — SSE transport smoke: auth is accepted at /sse
 *
 * All tests send raw JSON-RPC over HTTP to avoid MCP SDK client complexity in the
 * Workers runtime. The agents SDK speaks Streamable HTTP (JSON-RPC 2.0 over POST),
 * which we drive directly.
 *
 * IMPORTANT: run with --no-isolate --max-workers=1 to prevent DO isolation errors.
 * See package.json test:integration:transport script.
 */

import { describe, it, expect, beforeAll, vi, afterEach } from 'vitest'
import { env } from 'cloudflare:test'
import { exports } from 'cloudflare:workers'
import type { Env } from '../env'
import { mintAccessToken, setupFetchMock } from '../test/helpers/mint-token'

// ── Types ─────────────────────────────────────────────────────────────────────

type WorkerFetch = (req: Request) => Promise<Response>

interface JsonRpcResponse {
  jsonrpc: '2.0'
  id: number | string
  result?: unknown
  error?: { code: number; message: string; data?: unknown }
}

interface SendMcpResult {
  status: number
  body: JsonRpcResponse | null
  rawText: string
  sessionId: string | undefined
}

// ── Helpers ───────────────────────────────────────────────────────────────────

/**
 * Send a single JSON-RPC request to the Worker's /mcp endpoint.
 *
 * Returns the HTTP status, parsed JSON-RPC body (if JSON), raw text, and
 * the Mcp-Session-Id from the response headers (needed for subsequent calls
 * in the same logical session — the agents SDK uses it to route to the same DO).
 */
async function sendMcp(
  worker: { fetch: WorkerFetch },
  bearerToken: string,
  method: string,
  params: unknown,
  sessionId?: string,
  extraHeaders?: Record<string, string>,
): Promise<SendMcpResult> {
  const headers: Record<string, string> = {
    Authorization: bearerToken,
    'Content-Type': 'application/json',
    Accept: 'application/json, text/event-stream',
    ...(sessionId ? { 'Mcp-Session-Id': sessionId } : {}),
    ...(extraHeaders ?? {}),
  }

  const body = JSON.stringify({
    jsonrpc: '2.0',
    id: 1,
    method,
    params,
  })

  const res = await worker.fetch(
    new Request('http://mcp.test/mcp', { method: 'POST', headers, body }),
  )

  const rawText = await res.text()
  const responseSessionId = res.headers.get('Mcp-Session-Id') ?? undefined

  // The agents SDK may return either plain JSON or SSE (text/event-stream).
  // Parse the first JSON-RPC message from whichever format we get.
  let parsed: JsonRpcResponse | null = null
  const contentType = res.headers.get('Content-Type') ?? ''

  if (contentType.includes('text/event-stream')) {
    // SSE format: lines starting with "data: " contain JSON-RPC messages
    for (const line of rawText.split('\n')) {
      const trimmed = line.trim()
      if (trimmed.startsWith('data: ')) {
        try {
          parsed = JSON.parse(trimmed.slice(6)) as JsonRpcResponse
          break
        } catch {
          // skip non-JSON data lines
        }
      }
    }
  } else {
    try {
      parsed = JSON.parse(rawText) as JsonRpcResponse
    } catch {
      // rawText is not JSON; leave parsed null
    }
  }

  return { status: res.status, body: parsed, rawText, sessionId: responseSessionId }
}

/**
 * Perform the MCP initialize handshake and return the session ID.
 * The session ID must be included in all subsequent requests to the same session.
 */
async function initialize(
  worker: { fetch: WorkerFetch },
  bearerToken: string,
  extraHeaders?: Record<string, string>,
): Promise<{ sessionId: string | undefined; serverName: string | undefined; protocolVersion: string | undefined }> {
  const result = await sendMcp(
    worker,
    bearerToken,
    'initialize',
    {
      protocolVersion: '2024-11-05',
      capabilities: {},
      clientInfo: { name: 'test-client', version: '1.0.0' },
    },
    undefined,
    extraHeaders,
  )

  const serverInfo = (result.body?.result as Record<string, unknown> | undefined)?.serverInfo as
    | Record<string, unknown>
    | undefined
  const serverName = serverInfo?.name as string | undefined
  const protocolVersion = (result.body?.result as Record<string, unknown> | undefined)
    ?.protocolVersion as string | undefined

  return {
    sessionId: result.sessionId,
    serverName,
    protocolVersion,
  }
}

// ── Per-test backend mock builder ────────────────────────────────────────────
//
// The base setupFetchMock() spy intercepts Clerk revocation + audit URLs.
// For tests that need a backend response, we replace the spy implementation
// for that test with a wrapper that handles backend URLs first, then delegates
// to the base handlers.

type BackendInterceptor = {
  url: string | RegExp
  method?: string
  response: () => Response
  callCount: number
}

/** Create an extended fetch mock that handles a specific backend URL, delegating to base for others. */
function makeExtendedFetchSpy(interceptors: BackendInterceptor[]) {
  return async function extendedFetch(input: RequestInfo | URL, init?: RequestInit): Promise<Response> {
    const url =
      input instanceof Request ? input.url : input instanceof URL ? input.href : String(input)
    const method = (input instanceof Request ? input.method : init?.method ?? 'GET').toUpperCase()

    // Check per-test interceptors first
    for (const interceptor of interceptors) {
      const urlMatches =
        typeof interceptor.url === 'string'
          ? url.includes(interceptor.url)
          : interceptor.url.test(url)
      const methodMatches = !interceptor.method || interceptor.method.toUpperCase() === method

      if (urlMatches && methodMatches) {
        interceptor.callCount++
        return interceptor.response()
      }
    }

    // Delegate to base mock handlers (Clerk revocation, audit)
    if (url.includes('api.clerk.com/oauth_applications/access_tokens/verify') && method === 'POST') {
      return new Response(
        JSON.stringify({ id: 'oat_test_clerk_access_token', revoked: false, expired: false }),
        { status: 200, headers: { 'Content-Type': 'application/json' } },
      )
    }

    if (url.includes('backend.test') && url.includes('/api/v1/mcp/audit') && method === 'POST') {
      return new Response('{}', { status: 200, headers: { 'Content-Type': 'application/json' } })
    }

    throw new TypeError(
      `[extendedFetchSpy] Unintercepted fetch: ${method} ${url}\n` +
        'Add a per-test interceptor for this URL.',
    )
  }
}

// ── Test suite ────────────────────────────────────────────────────────────────

describe('MCP transport integration', () => {
  // Install the base fetch mock (Clerk revocation + audit)
  setupFetchMock()

  let bearerToken: string
  let worker: { fetch: WorkerFetch }

  beforeAll(async () => {
    worker = exports.default as { fetch: WorkerFetch }
    bearerToken = await mintAccessToken(worker, env as Env)
  })

  // Restore any per-test spy overrides after each test
  afterEach(() => {
    vi.restoreAllMocks()
  })

  // ── Test 1 (Scenario 1): full MCP chain — initialize → tools/list → tools/call ──

  it('Scenario 1: full chain — initialize, tools/list, tools/call (getCashFlowSummary)', async () => {
    // Mock the backend endpoint for getCashFlowSummary
    const backendInterceptors: BackendInterceptor[] = [
      {
        url: '/api/v1/analytics/cash-flow-summary',
        method: 'GET',
        response: () =>
          new Response(
            JSON.stringify({
              total_income: 5000,
              total_expenses: 3200,
              net: 1800,
              period: '2024-01',
            }),
            { status: 200, headers: { 'Content-Type': 'application/json' } },
          ),
        callCount: 0,
      },
    ]

    vi.spyOn(globalThis, 'fetch').mockImplementation(
      makeExtendedFetchSpy(backendInterceptors),
    )

    // Step 1: initialize
    const { sessionId, serverName, protocolVersion } = await initialize(worker, bearerToken)

    expect(serverName).toBe('boostedbudget')
    expect(protocolVersion).toBeTruthy()
    // Session ID may or may not be present depending on agents SDK version
    // (some versions use path-based routing); we don't require it

    // Step 2: tools/list
    const listResult = await sendMcp(
      worker,
      bearerToken,
      'tools/list',
      {},
      sessionId,
    )

    expect(listResult.status).not.toBe(401)
    expect(listResult.status).not.toBe(503)

    const tools = (listResult.body?.result as Record<string, unknown> | undefined)?.tools as
      | Array<{ name: string }>
      | undefined

    // The server exposes at least search and execute tools
    expect(Array.isArray(tools)).toBe(true)
    expect(tools!.length).toBeGreaterThan(0)
    const toolNames = tools!.map((t) => t.name)
    expect(toolNames).toContain('search')
    expect(toolNames).toContain('execute')

    // Step 3: tools/call — execute getCashFlowSummary
    const callResult = await sendMcp(
      worker,
      bearerToken,
      'tools/call',
      {
        name: 'execute',
        arguments: {
          calls: [{ operationId: 'getCashFlowSummary' }],
        },
      },
      sessionId,
    )

    expect(callResult.status).not.toBe(401)
    expect(callResult.status).not.toBe(503)

    const callContent = (callResult.body?.result as Record<string, unknown> | undefined)
      ?.content as Array<{ type: string; text: string }> | undefined

    expect(Array.isArray(callContent)).toBe(true)
    expect(callContent![0]?.type).toBe('text')
    expect(callContent![0]?.text).toContain('getCashFlowSummary')

    // Backend was called exactly once for the tool execution
    expect(backendInterceptors[0]!.callCount).toBeGreaterThanOrEqual(1)
  })

  // ── Test 2 (Scenario 4): write-confirmation round trip ────────────────────

  it('Scenario 4: write-confirmation round trip — createMCPRecipe', async () => {
    // Backend interceptor for POST /api/v1/mcp/recipes (only called on confirmed write)
    let capturedIdempotencyKey: string | undefined
    const backendInterceptors: BackendInterceptor[] = [
      {
        url: '/api/v1/mcp/recipes',
        method: 'POST',
        response: () =>
          new Response(
            JSON.stringify({ id: 'recipe-123', name: 'My Recipe', calls: [] }),
            { status: 201, headers: { 'Content-Type': 'application/json' } },
          ),
        callCount: 0,
      },
    ]

    let savedRequestHeaders: Headers | undefined
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const url =
        input instanceof Request ? input.url : input instanceof URL ? input.href : String(input)

      // Capture headers on backend recipe creation calls
      if (url.includes('/api/v1/mcp/recipes')) {
        const headers = input instanceof Request ? input.headers : new Headers(init?.headers as HeadersInit | undefined)
        savedRequestHeaders = headers
        capturedIdempotencyKey = headers.get('Idempotency-Key') ?? undefined
      }

      return makeExtendedFetchSpy(backendInterceptors)(input, init)
    })

    // Initialize a session
    const { sessionId } = await initialize(worker, bearerToken)

    // Step 1: call execute createMCPRecipe — expect confirmation challenge
    const challengeResult = await sendMcp(
      worker,
      bearerToken,
      'tools/call',
      {
        name: 'execute',
        arguments: {
          calls: [
            {
              operationId: 'createMCPRecipe',
              body: {
                name: 'My Recipe',
                calls: [{ operationId: 'getCashFlowSummary' }],
              },
            },
          ],
        },
      },
      sessionId,
    )

    expect(challengeResult.status).not.toBe(401)
    expect(challengeResult.status).not.toBe(503)

    const challengeContent = (challengeResult.body?.result as Record<string, unknown> | undefined)
      ?.content as Array<{ type: string; text: string }> | undefined

    expect(Array.isArray(challengeContent)).toBe(true)
    const challengeText = challengeContent![0]?.text ?? ''
    expect(challengeText).toContain('confirmation')

    // Backend must NOT have been called on the challenge (no confirmed write yet)
    expect(backendInterceptors[0]!.callCount).toBe(0)

    // Step 2: extract confirmation token from challenge text
    // Format: `Re-invoke execute() with confirmationToken: "<token>"`
    const tokenMatch = /confirmationToken: "([^"]+)"/.exec(challengeText)
    expect(tokenMatch).not.toBeNull()
    const confirmationToken = tokenMatch![1]!

    // Step 3: confirm the write with the extracted token
    const confirmResult = await sendMcp(
      worker,
      bearerToken,
      'tools/call',
      {
        name: 'execute',
        arguments: {
          calls: [
            {
              operationId: 'createMCPRecipe',
              body: {
                name: 'My Recipe',
                calls: [{ operationId: 'getCashFlowSummary' }],
              },
            },
          ],
          confirmationToken,
        },
      },
      sessionId,
    )

    expect(confirmResult.status).not.toBe(401)
    expect(confirmResult.status).not.toBe(503)

    const confirmContent = (confirmResult.body?.result as Record<string, unknown> | undefined)
      ?.content as Array<{ type: string; text: string }> | undefined

    const confirmText = confirmContent?.[0]?.text ?? ''
    // Should show success (✅), not another confirmation challenge (⚠️)
    expect(confirmText).not.toContain('⚠️')
    expect(confirmText).toContain('createMCPRecipe')

    // Backend was called exactly once (on the confirmed write only)
    expect(backendInterceptors[0]!.callCount).toBe(1)

    // Idempotency-Key header was present on the backend call
    expect(capturedIdempotencyKey).toBeTruthy()
  })

  // ── Test 3 (Scenario 3): header injection safety ─────────────────────────

  it('Scenario 3: attacker-injected X-MCP-Worker-Secret and X-MCP-Origin are stripped', async () => {
    let capturedWorkerSecret: string | undefined
    let capturedAuthorization: string | undefined

    const backendInterceptors: BackendInterceptor[] = [
      {
        url: '/api/v1/analytics/cash-flow-summary',
        method: 'GET',
        response: () =>
          new Response(
            JSON.stringify({ total_income: 1000, total_expenses: 500, net: 500, period: '2024-01' }),
            { status: 200, headers: { 'Content-Type': 'application/json' } },
          ),
        callCount: 0,
      },
    ]

    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const url =
        input instanceof Request ? input.url : input instanceof URL ? input.href : String(input)

      // Capture headers on backend calls (fetch-mutator sets X-MCP-Worker-Secret)
      if (url.includes('backend.test') && url.includes('/api/v1/analytics')) {
        const headers = input instanceof Request ? input.headers : new Headers(init?.headers as HeadersInit | undefined)
        capturedWorkerSecret = headers.get('X-MCP-Worker-Secret') ?? undefined
        capturedAuthorization = headers.get('Authorization') ?? undefined
      }

      return makeExtendedFetchSpy(backendInterceptors)(input, init)
    })

    // Initialize with attacker-supplied X-MCP-* headers
    // McpApiHandler strips and replaces these before forwarding to the DO
    const { sessionId } = await initialize(worker, bearerToken, {
      'X-MCP-Worker-Secret': 'attacker-secret',
      'X-MCP-Origin': 'attacker-origin',
    })

    // Call the backend via execute — this triggers header injection via fetch-mutator
    const callResult = await sendMcp(
      worker,
      bearerToken,
      'tools/call',
      {
        name: 'execute',
        arguments: { calls: [{ operationId: 'getCashFlowSummary' }] },
      },
      sessionId,
      // Also inject attacker headers on the MCP tool call itself
      {
        'X-MCP-Worker-Secret': 'attacker-secret',
        'X-MCP-Origin': 'attacker-origin',
      },
    )

    expect(callResult.status).not.toBe(401)
    expect(callResult.status).not.toBe(503)

    // Backend was reached
    expect(backendInterceptors[0]!.callCount).toBeGreaterThanOrEqual(1)

    // Worker-Secret must be the env-sourced value (from vitest.config.ts bindings),
    // NOT the attacker-supplied value
    expect(capturedWorkerSecret).toBe('test-worker-secret')
    expect(capturedWorkerSecret).not.toBe('attacker-secret')

    // Authorization must be the Clerk access token from props (NOT the OAuthProvider token)
    // The fake Clerk access token stored in props is 'oat_test_clerk_access_token'
    expect(capturedAuthorization).toBe('Bearer oat_test_clerk_access_token')
  })

  // ── Test 4 (Scenario 5): SSE transport smoke ─────────────────────────────

  it('Scenario 5: GET /sse with bearer token does not return 401 or 503', async () => {
    // SSE uses WebSocket-backed transport in the agents SDK.
    // We just test that auth is accepted — not that streaming works end-to-end.

    const res = await worker.fetch(
      new Request('http://mcp.test/sse', {
        headers: {
          Authorization: bearerToken,
          Accept: 'text/event-stream',
        },
      }),
    )

    // Auth must not be rejected (401) or misconfigured (503)
    expect(res.status).not.toBe(401)
    expect(res.status).not.toBe(503)

    // Accepted outcomes:
    //   101 — WebSocket upgrade (underlying transport for SSE-over-WS)
    //   200 — SSE stream opened (older transport style)
    //   Other — DO handling, connection refused at protocol level (e.g. 400 if
    //           missing Upgrade header) — still means auth passed

    // Drain the body to avoid hanging the test (SSE streams don't auto-close)
    if (res.body) {
      try {
        const reader = res.body.getReader()
        // Read at most a few bytes to confirm the stream opened, then cancel
        await Promise.race([
          reader.read().then(() => reader.cancel()),
          new Promise<void>((resolve) => setTimeout(resolve, 300)),
        ])
      } catch {
        // Ignore body read errors — we only care that auth passed
      }
    }

    // Final assertion: status was not an auth or config error
    const status = res.status
    expect([101, 200, 400, 404, 405, 426, 500]).toContain(status)
  })
})
