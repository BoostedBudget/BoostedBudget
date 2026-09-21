/**
 * Live end-to-end smoke for the folded MCP tool surface (#1699, todo 18).
 *
 * Unlike transport.integration.test.ts — which mocks the backend with
 * makeExtendedFetchSpy — this file lets the Worker talk to a REAL Go backend.
 * Only the Worker's own Clerk revocation check is intercepted: it needs
 * api.clerk.com and a genuine Clerk token, neither of which exists in a local run.
 * Every other outbound fetch (recipe validation, audit, telemetry) is passed
 * through untouched, so the assertions below exercise the real backend's 400.
 *
 * Preconditions (a live smoke cannot self-provision these):
 *   1. A backend on :8080 with MCP_WORKER_SECRET=test-worker-secret, configured so
 *      the forwarded test token verifies (see the todo-18 notepad for the local
 *      Clerk-verify stub used here).
 *   2. The smoke user has a confirmed subscription projection: without a
 *      revenuecat_customers row the read-only middleware answers 503 before any
 *      recipe validation runs.
 *
 * Run:
 *   MCP_SMOKE_API_URL=http://localhost:8080 \
 *     bun run vitest run --config vitest.transport.config.ts --no-isolate --max-workers=1
 */

import { afterAll, beforeAll, describe, expect, it, vi } from 'vitest'
import { env } from 'cloudflare:test'
import { exports } from 'cloudflare:workers'
import type { Env } from '../../src/env'
import { mintAccessToken } from '../../src/test/helpers/mint-token'

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

let _rpcId = 0

async function sendMcp(
  worker: WorkerFetch,
  bearerToken: string,
  method: string,
  params: unknown,
  sessionId?: string,
): Promise<SendMcpResult> {
  const headers: Record<string, string> = {
    Authorization: bearerToken,
    'Content-Type': 'application/json',
    Accept: 'application/json, text/event-stream',
    ...(sessionId ? { 'Mcp-Session-Id': sessionId } : {}),
  }

  const res = await worker.fetch(
    new Request('http://mcp.test/mcp', {
      method: 'POST',
      headers,
      body: JSON.stringify({ jsonrpc: '2.0', id: ++_rpcId, method, params }),
    }),
  )

  const rawText = await res.text()
  const responseSessionId = res.headers.get('Mcp-Session-Id') ?? undefined
  const contentType = res.headers.get('Content-Type') ?? ''

  let body: JsonRpcResponse | null = null
  if (contentType.includes('text/event-stream')) {
    for (const line of rawText.split('\n')) {
      const trimmed = line.trim()
      if (trimmed.startsWith('data: ')) {
        try {
          body = JSON.parse(trimmed.slice(6)) as JsonRpcResponse
          break
        } catch {
          // skip non-JSON data lines
        }
      }
    }
  } else {
    try {
      body = JSON.parse(rawText) as JsonRpcResponse
    } catch {
      // leave body null for non-JSON responses
    }
  }

  return { status: res.status, body, rawText, sessionId: responseSessionId }
}

function resultText(result: SendMcpResult): string {
  const content = (result.body?.result as Record<string, unknown> | undefined)?.content as
    | Array<{ type: string; text: string }>
    | undefined
  return content?.[0]?.text ?? ''
}

function raw(label: string, value: string): void {
  console.log(`\n[smoke:${label}] RAW OUTPUT >>>\n${value}\n[smoke:${label}] <<< END`)
}

describe('folded MCP surface — live smoke', () => {
  const realFetch = globalThis.fetch.bind(globalThis)
  let worker: WorkerFetch
  let bearerToken: string
  let sessionId: string | undefined

  beforeAll(async () => {
    worker = exports.default as WorkerFetch

    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const url =
        input instanceof Request ? input.url : input instanceof URL ? input.href : String(input)
      const method = (input instanceof Request ? input.method : init?.method ?? 'GET').toUpperCase()

      if (
        url.includes('api.clerk.com/oauth_applications/access_tokens/verify') &&
        method === 'POST'
      ) {
        return new Response(
          JSON.stringify({ id: 'oat_test_clerk_access_token', revoked: false, expired: false }),
          { status: 200, headers: { 'Content-Type': 'application/json' } },
        )
      }

      if (url.includes('/.well-known/jwks.json')) {
        throw new TypeError(`[smoke] Unexpected JWKS fetch — should not be reached: ${url}`)
      }

      return realFetch(input as RequestInfo, init)
    })

    bearerToken = await mintAccessToken(worker, env as Env)

    const initialized = await sendMcp(worker, bearerToken, 'initialize', {
      protocolVersion: '2024-11-05',
      capabilities: {},
      clientInfo: { name: 'smoke-live', version: '1.0.0' },
    })
    expect(initialized.status).not.toBe(401)
    expect(initialized.status).not.toBe(503)
    sessionId = initialized.sessionId
  })

  afterAll(() => {
    vi.restoreAllMocks()
  })

  it('(a) tools/list advertises exactly the four folded tools', async () => {
    const listed = await sendMcp(worker, bearerToken, 'tools/list', {}, sessionId)
    expect(listed.status).not.toBe(401)
    expect(listed.status).not.toBe(503)

    const tools = (listed.body?.result as Record<string, unknown> | undefined)?.tools as
      | Array<{ name: string }>
      | undefined
    const toolNames = (tools ?? []).map((t) => t.name)
    raw('a', JSON.stringify(toolNames))

    expect([...toolNames].sort()).toEqual([
      'readExecute',
      'recipe.templates',
      'search',
      'writeExecute',
    ])
  })

  it('(b) multi-word search finds getSpendingByMerchant', async () => {
    const result = await sendMcp(
      worker,
      bearerToken,
      'tools/call',
      { name: 'search', arguments: { query: 'top merchants spending' } },
      sessionId,
    )
    expect(result.status).not.toBe(401)
    expect(result.status).not.toBe(503)

    const text = resultText(result)
    raw('b', text)

    expect(text).toContain('getSpendingByMerchant')
  })

  it('(d) readExecute refuses runMCPRecipe with the use-writeExecute message', async () => {
    const result = await sendMcp(
      worker,
      bearerToken,
      'tools/call',
      {
        name: 'readExecute',
        arguments: { calls: [{ operationId: 'runMCPRecipe', pathParams: { id: 'rec_abc123' } }] },
      },
      sessionId,
    )
    expect(result.status).not.toBe(401)
    expect(result.status).not.toBe(503)

    const text = resultText(result)
    raw('d', text)

    expect(text).toContain('use writeExecute')
  })

  it('(c) bad createMCPRecipe challenges, then the confirmed call carries the 400 detail', async () => {
    const calls = [
      {
        operationId: 'createMCPRecipe',
        body: {
          name: 'Smoke Recipe',
          description: 'todo-18 live smoke',
          spec: [{ operation_id: 'notARealOperation' }],
        },
      },
    ]

    const challenge = await sendMcp(
      worker,
      bearerToken,
      'tools/call',
      { name: 'writeExecute', arguments: { calls } },
      sessionId,
    )
    expect(challenge.status).not.toBe(401)
    expect(challenge.status).not.toBe(503)

    const challengeText = resultText(challenge)
    raw('c1', challengeText)

    const tokenMatch = /confirmationToken: "([^"]+)"/.exec(challengeText)
    expect(tokenMatch).not.toBeNull()
    expect(challengeText).not.toContain('invalid recipe spec')

    const confirmed = await sendMcp(
      worker,
      bearerToken,
      'tools/call',
      { name: 'writeExecute', arguments: { calls, confirmationToken: tokenMatch![1] } },
      challenge.sessionId ?? sessionId,
    )
    expect(confirmed.status).not.toBe(401)
    expect(confirmed.status).not.toBe(503)

    const confirmedText = resultText(confirmed)
    raw('c2', confirmedText)

    expect(confirmedText).toContain('invalid recipe spec')
    expect(confirmedText).toContain('notARealOperation')
    expect(confirmedText).toContain('API error 4')
    expect(confirmedText).not.toContain('[REDACTED]')
  })
})
