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

import { describe, it, expect, beforeAll, vi } from 'vitest'
import { env } from 'cloudflare:test'
import { exports } from 'cloudflare:workers'
import type { Env } from '../../src/env'
import { mintAccessToken, setupFetchMock, handleBaseUrls } from '../../src/test/helpers/mint-token'
import { clearRevocationCache } from '../../src/oauth/revocation'

// ── Types ─────────────────────────────────────────────────────────────────────

type WorkerFetch = { fetch: (req: Request) => Promise<Response> }

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

/** Extract URL and HTTP method from Request/URL/string and RequestInit. */
function extractUrlAndMethod(input: RequestInfo | URL, init?: RequestInit): [string, string] {
  const url =
    input instanceof Request ? input.url : input instanceof URL ? input.href : String(input)
  const method = (input instanceof Request ? input.method : init?.method ?? 'GET').toUpperCase()
  return [url, method]
}

/** Auto-incrementing JSON-RPC request ID — ensures each call gets a unique ID. */
let _rpcId = 0

/**
 * Send a single JSON-RPC request to the Worker's /mcp endpoint.
 *
 * Returns the HTTP status, parsed JSON-RPC body (if JSON), raw text, and
 * the Mcp-Session-Id from the response headers (needed for subsequent calls
 * in the same logical session — the agents SDK uses it to route to the same DO).
 */
async function sendMcp(
  worker: WorkerFetch,
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
    id: ++_rpcId,
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
  worker: WorkerFetch,
  bearerToken: string,
  extraHeaders?: Record<string, string>,
): Promise<{
  sessionId: string | undefined
  serverName: string | undefined
  serverDescription: string | undefined
  protocolVersion: string | undefined
  instructions: string | undefined
}> {
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

  const initializeResult = result.body?.result as Record<string, unknown> | undefined
  const serverInfo = initializeResult?.serverInfo as
    | Record<string, unknown>
    | undefined
  const serverName = serverInfo?.name as string | undefined
  const serverDescription = serverInfo?.description as string | undefined
  const protocolVersion = initializeResult?.protocolVersion as string | undefined
  const instructions = initializeResult?.instructions as string | undefined

  return {
    sessionId: result.sessionId,
    serverName,
    serverDescription,
    protocolVersion,
    instructions,
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
    const [url, method] = extractUrlAndMethod(input, init)

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

    // Delegate to shared base handlers (Clerk revocation, audit, JWKS)
    const baseResponse = handleBaseUrls(url, method)
    if (baseResponse !== null) return baseResponse

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
  let worker: WorkerFetch

  beforeAll(async () => {
    worker = exports.default as WorkerFetch
    bearerToken = await mintAccessToken(worker, env as Env)
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
    const { sessionId, serverName, serverDescription, protocolVersion, instructions } =
      await initialize(worker, bearerToken)

    expect(serverName).toBe('boostedbudget')
    expect(protocolVersion).toBeTruthy()
    expect(instructions).toBe(
      'Analyze accounts, transactions, spending, net worth, and investments; manage envelope budgets, savings goals, categories, and tags. Boost definitions are read-only here; review their history, budget alerts, and transfer, rollover, or savings-goal suggestions. MCP writes require confirmation; BoostedBudget never moves money. Use search first, then readExecute or writeExecute.',
    )
    expect(new TextEncoder().encode(instructions).byteLength).toBeLessThanOrEqual(400)
    expect(serverDescription).toBeUndefined()
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

    // The server exposes at least search, readExecute, and writeExecute tools
    expect(Array.isArray(tools)).toBe(true)
    expect(tools!.length).toBeGreaterThan(0)
    const toolNames = tools!.map((t) => t.name)
    expect(toolNames).toContain('search')
    expect(toolNames).toContain('readExecute')
    expect(toolNames).toContain('writeExecute')

    // Step 3: tools/call — readExecute getCashFlowSummary (a read-only operation)
    const callResult = await sendMcp(
      worker,
      bearerToken,
      'tools/call',
      {
        name: 'readExecute',
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
    expect(backendInterceptors[0]!.callCount).toBe(1)
  })

  // ── Test 2 (Scenario 4): write-confirmation round trip ────────────────────

  it('discovers search detail, selected response fields, and rejects invalid detail over transport', async () => {
    const { sessionId } = await initialize(worker, bearerToken)
    const listed = await sendMcp(worker, bearerToken, 'tools/list', {}, sessionId)
    const tools = (listed.body!.result as { tools: Array<{ name: string; inputSchema: { properties: Record<string, { enum?: string[] }> } }> }).tools
    expect(tools.find((tool) => tool.name === 'search')!.inputSchema.properties.detail?.enum).toEqual(['compact', 'full'])
    for (const detail of [undefined, 'compact', 'full']) {
      const result = await sendMcp(worker, bearerToken, 'tools/call', { name: 'search', arguments: { query: 'getTransactions', ...(detail ? { detail } : {}) } }, sessionId)
      const text = (result.body!.result as { content: Array<{ text: string }> }).content[0]!.text
      expect(text).toContain(`Response detail: ${detail ?? 'compact'}`)
      expect(text).toContain('transactions[].account.name')
      if (detail === 'full') expect(text).toContain('transactions[].account.plaid_account_id')
      else {
        expect(text).not.toContain('transactions[].account.plaid_account_id')
        expect(text).toContain('transactions[].effective_pfc_primary_display')
      }
    }
    for (const detail of [null, 'verbose']) {
      const result = await sendMcp(worker, bearerToken, 'tools/call', { name: 'search', arguments: { query: 'getAccounts', detail } }, sessionId)
      expect(!!result.body?.error || (result.body?.result as { isError?: boolean })?.isError).toBe(true)
    }
  })

  it('discovers response detail and preserves compact/full calls and safe POST bodies', async () => {
    const payload = { accounts: [{ id: 'a', name: 'Checking', user_id: 'hidden-owner' }], total_count: 1 }
    const previewBody = { conditions: [{ field: 'name', operator: 'contains', value: 'Coffee' }], condition_logic: 'AND' }
    let receivedBody: unknown
    const base = makeExtendedFetchSpy([{ url: '/api/v1/accounts', method: 'GET', response: () => Response.json(payload), callCount: 0 }])
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const [url, method] = extractUrlAndMethod(input, init)
      if (url.includes('/preview') && method === 'POST') {
        receivedBody = input instanceof Request ? await input.clone().json() : JSON.parse(String(init?.body))
        return Response.json({ total_matches: 1 })
      }
      return base(input, init)
    })
    const { sessionId } = await initialize(worker, bearerToken)
    const listed = await sendMcp(worker, bearerToken, 'tools/list', {}, sessionId)
    const tools = (listed.body!.result as { tools: Array<{ name: string; inputSchema: { properties: { calls: { items: { properties: Record<string, { enum?: string[]; default?: unknown }> } } } } }> }).tools
    for (const name of ['readExecute', 'writeExecute']) {
      const properties = tools.find((tool) => tool.name === name)!.inputSchema.properties.calls.items.properties
      expect(properties.detail?.enum).toEqual(['compact', 'full'])
      expect(properties.detail).not.toHaveProperty('default')
      expect(properties.body).toBeDefined()
      for (const detail of [undefined, 'full']) {
        const result = await sendMcp(worker, bearerToken, 'tools/call', { name, arguments: { calls: [{ operationId: 'getAccounts', ...(detail ? { detail } : {}) }] } }, sessionId)
        const text = (result.body!.result as { content: Array<{ text: string }> }).content[0]!.text
        if (detail === 'full') expect(text).toBe(`✅ getAccounts: ${JSON.stringify(payload)}`)
        else {
          expect(text).not.toContain('hidden-owner')
          expect(text).toContain('detail: "full"')
        }
      }
    }
    const preview = await sendMcp(worker, bearerToken, 'tools/call', { name: 'readExecute', arguments: { calls: [{ operationId: 'previewCategoryMatches', body: previewBody }] } }, sessionId)
    expect((preview.body!.result as { content: Array<{ text: string }> }).content[0]!.text).toContain('total_matches')
    expect(receivedBody).toEqual(previewBody)
  })

  it('preserves recipe detail through schema, confirmed save, and complete resolution', async () => {
    const calls = [
      { operationId: 'getAccounts', params: { include_hidden: true }, detail: 'compact' },
      { operationId: 'getTransactions', body: { synthetic: true }, pathParams: { id: 'synthetic' }, detail: 'full' },
      ...Array.from({ length: 24 }, () => ({ operationId: 'getCashFlowSummary' })),
    ]
    const spec = calls.map(({ operationId, ...rest }) => {
      const { pathParams, ...args } = rest as Record<string, unknown>
      return { operation_id: operationId, ...args, ...(pathParams ? { path_params: pathParams } : {}) }
    })
    let savedBody: unknown
    let backendCalls = 0
    const base = makeExtendedFetchSpy([])
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const [url, method] = extractUrlAndMethod(input, init)
      if (url.includes('/api/v1/mcp/recipes') && method === 'POST') {
        backendCalls++
        if (url.endsWith('/run')) return Response.json({ name: 'Synthetic review', calls: spec })
        savedBody = input instanceof Request ? await input.clone().json() : JSON.parse(String(init?.body))
        return Response.json({ id: 'synthetic-recipe' })
      }
      return base(input, init)
    })
    const { sessionId } = await initialize(worker, bearerToken)
    const listed = await sendMcp(worker, bearerToken, 'tools/list', {}, sessionId)
    const tools = (listed.body!.result as { tools: Array<{ name: string; inputSchema: { properties: { calls: { maxItems?: number; items: { properties: Record<string, { enum?: string[] }> } } } } }> }).tools
    const schema = tools.find(tool => tool.name === 'recipe.save')!.inputSchema.properties.calls
    expect(schema.maxItems).toBeUndefined()
    expect(schema.items.properties.detail?.enum).toEqual(['compact', 'full'])
    expect(schema.items.properties.detail).not.toHaveProperty('default')
    for (const detail of [null, '', 'verbose']) {
      const invalid = await sendMcp(worker, bearerToken, 'tools/call', { name: 'recipe.save', arguments: { name: 'Invalid', calls: [{ operationId: 'getAccounts', detail }] } }, sessionId)
      expect(!!invalid.body?.error || (invalid.body?.result as { isError?: boolean })?.isError).toBe(true)
    }
    expect(backendCalls).toBe(0)
    const challenge = await sendMcp(worker, bearerToken, 'tools/call', { name: 'recipe.save', arguments: { name: 'Synthetic review', calls } }, sessionId)
    const challengeText = (challenge.body!.result as { content: Array<{ text: string }> }).content[0]!.text
    const token = /confirmationToken: "([^"]+)"/.exec(challengeText)?.[1]
    expect(token).toBeTruthy()
    expect(backendCalls).toBe(0)
    const confirmed = await sendMcp(worker, bearerToken, 'tools/call', { name: 'recipe.save', arguments: { name: 'Synthetic review', calls, confirmationToken: token } }, challenge.sessionId ?? sessionId)
    expect((confirmed.body!.result as { content: Array<{ text: string }> }).content[0]!.text).toContain('saved successfully')
    expect(savedBody).toEqual({ name: 'Synthetic review', spec })
    expect(backendCalls).toBe(1)
    const resolved = await sendMcp(worker, bearerToken, 'tools/call', { name: 'recipe.run', arguments: { id: 'synthetic-recipe' } }, sessionId)
    const text = (resolved.body!.result as { content: Array<{ text: string }> }).content[0]!.text
    expect(JSON.parse(text.slice(text.indexOf('{"calls":')))).toEqual({ calls })
    expect(backendCalls).toBe(2)
  })

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

    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const url =
        input instanceof Request ? input.url : input instanceof URL ? input.href : String(input)

      // Capture Idempotency-Key on backend recipe creation calls
      if (url.includes('/api/v1/mcp/recipes')) {
        const headers = input instanceof Request ? input.headers : new Headers(init?.headers as HeadersInit | undefined)
        capturedIdempotencyKey = headers.get('Idempotency-Key') ?? undefined
      }

      return makeExtendedFetchSpy(backendInterceptors)(input, init)
    })

    // Initialize a session
    const { sessionId } = await initialize(worker, bearerToken)

    // Step 0: search for the operation first (issue describes "search → writeExecute write" flow)
    // The search tool queries the spec index (static, no backend call) — the audit event it
    // fires is intercepted by the existing `handleBaseUrls` mock in setupFetchMock.
    const searchResult = await sendMcp(worker, bearerToken, 'tools/call', {
      name: 'search',
      arguments: { query: 'recipe' }, // 'recipe' matches createMCPRecipe, listMCPRecipes, etc.
    }, sessionId)
    expect(searchResult.status).not.toBe(401)
    expect(searchResult.status).not.toBe(503)
    const searchContent = (searchResult.body?.result as Record<string, unknown> | undefined)
      ?.content as Array<{ type: string; text: string }> | undefined
    // Search queries the spec index (static, no backend call) — assert it returned results
    expect(Array.isArray(searchContent)).toBe(true)
    const searchText = searchContent?.[0]?.text ?? ''
    expect(searchText.toLowerCase()).toContain('recipe')

    // Step 1: call writeExecute createMCPRecipe — expect confirmation challenge
    const challengeResult = await sendMcp(
      worker,
      bearerToken,
      'tools/call',
      {
        name: 'writeExecute',
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
    // Assert the confirmation token is embedded in the challenge text (extractable below)
    expect(challengeText).toMatch(/confirmationToken: "[^"]+"/)

    // Backend must NOT have been called on the challenge (no confirmed write yet)
    expect(backendInterceptors[0]!.callCount).toBe(0)

    // Step 2: extract confirmation token from challenge text
    // Format: `Re-invoke writeExecute() with confirmationToken: "<token>"`
    const tokenMatch = /confirmationToken: "([^"]+)"/.exec(challengeText)
    expect(tokenMatch).not.toBeNull()
    const confirmationToken = tokenMatch![1]!

    // Step 3: confirm the write with the extracted token.
    // Use the session ID from the challenge response if available — the agents SDK
    // may assign a session ID on the first non-initialize call, so the challenge
    // response is the authoritative source for routing subsequent requests to the
    // same Durable Object instance.
    const confirmResult = await sendMcp(
      worker,
      bearerToken,
      'tools/call',
      {
        name: 'writeExecute',
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
      challengeResult.sessionId ?? sessionId,
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
  // Validates mutator-level credential injection: the env value reaches the backend
  // (captured via fetch spy) and the attacker-supplied value does NOT.
  // Handler-level stripping of X-MCP-Worker-Secret is tested at unit level in
  // mcp-api-handler.test.ts.
  //
  // Note: X-MCP-Origin is an internal Worker header and is NOT forwarded to the
  // backend — it cannot be captured via the fetch spy (the DO consumes it before
  // any outbound request is made). Its stripping is therefore not asserted here.

  it('Scenario 3: env-sourced X-MCP-Worker-Secret reaches backend; attacker value is NOT forwarded', async () => {
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

    // Call the backend via readExecute — this triggers header injection via fetch-mutator
    const callResult = await sendMcp(
      worker,
      bearerToken,
      'tools/call',
      {
        name: 'readExecute',
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
    // SSE uses a WebSocket-backed DO transport in the agents SDK.
    // We test that auth is accepted and the SSE stream opens — not streaming end-to-end.
    //
    // Expected status: 200 with Content-Type: text/event-stream.
    // The legacy SSE handler (createLegacySseHandler in @cloudflare/agents) opens an SSE
    // ReadableStream immediately and sends the initial "event: endpoint\n..." message.
    // A 500 here would indicate the DO WebSocket upgrade failed, which is a real defect.

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

    // Read the first SSE chunk (bounded) to attempt extraction of the session endpoint.
    // The agents SDK sends `event: endpoint\ndata: /sse/message?sessionId=...` as the
    // first event. We capture it for the /sse/message follow-up test below.
    let sseChunk = ''
    if (res.body) {
      try {
        const reader = res.body.getReader()
        await Promise.race([
          (async () => {
            const { value } = await reader.read()
            if (value) sseChunk = new TextDecoder().decode(value)
            await reader.cancel()
          })(),
          new Promise<void>((resolve) => setTimeout(resolve, 300)),
        ])
      } catch {
        // Ignore body read errors — we only care that auth passed
      }
    }

    // The SSE handler returns 200 (text/event-stream) when auth passes and the
    // DO WebSocket upgrade succeeds in the test environment (miniflare supports it).
    // 101 would indicate a raw WebSocket upgrade (not used by the legacy SSE path).
    const status = res.status
    expect([101, 200]).toContain(status)

    // ── /sse/message follow-up: POST to the session endpoint if parseable ──────
    // Parse the session path from the first SSE chunk.
    // SSE format: "event: endpoint\ndata: /sse/message?sessionId=abc123\n\n"
    let sessionPath: string | undefined
    for (const line of sseChunk.split('\n')) {
      const trimmed = line.trim()
      if (trimmed.startsWith('data: ') && trimmed.includes('/sse/message')) {
        sessionPath = trimmed.slice(6).trim()
        break
      }
    }

    // The agents SDK always emits the endpoint event in the first SSE chunk in the
    // miniflare test environment. Fail loudly if it isn't there — a silent skip would
    // falsely satisfy the requirement that /sse/message reaches the SSE handler.
    expect(sessionPath, 'SSE endpoint event not received in first chunk — agents SDK must emit it immediately').toBeTruthy()

    // POST a notifications/initialized message to the /sse/message endpoint.
    // This verifies the endpoint accepts authenticated requests and the full
    // /sse + /sse/message routing chain works end-to-end.
    const msgRes = await worker.fetch(
      new Request(`http://mcp.test${sessionPath}`, {
        method: 'POST',
        headers: {
          Authorization: bearerToken,
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({ jsonrpc: '2.0', method: 'notifications/initialized', id: null }),
      }),
    )
    expect(msgRes.status).not.toBe(401)
    expect(msgRes.status).not.toBe(503)

    // ── X-MCP-* header injection on SSE: attacker headers do not cause auth failure ──
    // Header injection on SSE verified via auth acceptance — attacker headers stripped by
    // McpApiHandler before reaching DO. Direct header capture not possible on SSE transport path.
    const sseWithAttackerHeaders = await worker.fetch(
      new Request('http://mcp.test/sse', {
        headers: {
          Authorization: bearerToken,
          Accept: 'text/event-stream',
          'X-MCP-Worker-Secret': 'attacker-secret',
          'X-MCP-Origin': 'attacker-origin',
        },
      }),
    )
    // Auth must still succeed — Worker strips attacker headers and injects trusted env values
    expect(sseWithAttackerHeaders.status).not.toBe(401)
    // Drain the body
    if (sseWithAttackerHeaders.body) {
      try {
        const r = sseWithAttackerHeaders.body.getReader()
        await Promise.race([
          r.read().then(() => r.cancel()),
          new Promise<void>((resolve) => setTimeout(resolve, 300)),
        ])
      } catch {
        // Ignore
      }
    }
  })

  // ── Test 5 (Scenario 6): revoked token returns 401 ───────────────────────

  it('Scenario 6: revoked token returns 401', async () => {
    // Clear the per-isolate revocation cache so the token is not served from
    // a prior "verified_active" entry cached by an earlier test (TTL = 30 s).
    // Also clear at the end (try/finally) so the "revoked" entry written during
    // this test does not bleed into later test files that share the same isolate
    // (all tests use the same Clerk accessToken: 'oat_test_clerk_access_token').
    clearRevocationCache()

    try {
      // Override the fetch spy to return "token not found" (HTTP 404) from Clerk —
      // this is the response shape confirmed in the staging spike (issue #1224).
      vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
        const url =
          input instanceof Request ? input.url : input instanceof URL ? input.href : String(input)
        const method = (
          input instanceof Request ? input.method : init?.method ?? 'GET'
        ).toUpperCase()

        // Simulate Clerk returning "token not found" (revoked) for the introspection call.
        if (
          url.includes('api.clerk.com/oauth_applications/access_tokens/verify') &&
          method === 'POST'
        ) {
          return new Response(
            JSON.stringify({ errors: [{ code: 'oauth_access_token_not_found' }] }),
            { status: 404, headers: { 'Content-Type': 'application/json' } },
          )
        }

        const base = handleBaseUrls(url, method)
        if (base !== null) return base
        throw new TypeError(`[Scenario 6] Unintercepted: ${method} ${url}`)
      })

      // Send an MCP initialize request with the OAuthProvider-valid bearer token.
      // The token passes layer-1 (OAuthProvider), but the layer-2 Clerk introspection
      // returns 404 → revoked, so assertNotRevoked throws and the gate returns 401.
      const result = await sendMcp(worker, bearerToken, 'initialize', {
        protocolVersion: '2024-11-05',
        capabilities: {},
        clientInfo: { name: 'test-client', version: '1.0.0' },
      })

      expect(result.status).toBe(401)
    } finally {
      // Always clear the cache after this test so the "revoked" entry for
      // 'oat_test_clerk_access_token' does not poison subsequent test files.
      clearRevocationCache()
    }
  })
})
