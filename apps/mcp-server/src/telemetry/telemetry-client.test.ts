import { afterEach, describe, expect, it, mock } from 'bun:test'
import { createMCPToolTelemetryClient } from './telemetry-client'

const originalFetch = globalThis.fetch

afterEach(() => {
  globalThis.fetch = originalFetch
})

describe('createMCPToolTelemetryClient', () => {
  it('posts only bounded telemetry with the current bearer token and Worker credentials', async () => {
    const fetchSpy = mock(async () => new Response(null, { status: 204 }))
    globalThis.fetch = fetchSpy as unknown as typeof fetch
    const client = createMCPToolTelemetryClient({
      baseUrl: 'https://api.boostedbudget.com',
      originSecret: 'origin-secret',
      workerSecret: 'worker-secret',
      getAccessToken: () => 'oauth-access-token',
    })

    await client.record({
      tool_surface: 'search',
      tool_family: 'discovery',
      outcome: 'succeeded',
      duration_bucket: 'under_250ms',
    })

    expect(fetchSpy).toHaveBeenCalledTimes(1)
    const [url, init] = fetchSpy.mock.calls[0] as unknown as [string, RequestInit]
    expect(url).toBe('https://api.boostedbudget.com/api/v1/mcp/telemetry')
    expect(new Headers(init.headers).get('Authorization')).toBe('Bearer oauth-access-token')
    expect(new Headers(init.headers).get('X-MCP-Worker-Secret')).toBe('worker-secret')
    const body = JSON.parse(init.body as string)
    expect(body).toEqual({
      tool_surface: 'search',
      tool_family: 'discovery',
      outcome: 'succeeded',
      duration_bucket: 'under_250ms',
    })
    expect(JSON.stringify(body)).not.toContain('oauth-access-token')
    expect(JSON.stringify(body)).not.toContain('distinct_id')
  })

  it('resolves on non-2xx and network failures without logging payload, identity, or raw errors', async () => {
    const warnings: unknown[][] = []
    const oldWarn = console.warn
    console.warn = (...args: unknown[]) => { warnings.push(args) }
    try {
      globalThis.fetch = mock(async () => { throw new Error('secret raw failure') }) as unknown as typeof fetch
      const client = createMCPToolTelemetryClient({
        baseUrl: 'https://api.boostedbudget.com',
        originSecret: 'origin-secret',
        workerSecret: 'worker-secret',
        getAccessToken: () => 'private-user-token',
      })
      await expect(client.record({
        tool_surface: 'recipe_save', tool_family: 'workflows', outcome: 'failed', duration_bucket: 'over_5s',
      })).resolves.toBeUndefined()

      const logged = JSON.stringify(warnings)
      expect(logged).toContain('network')
      expect(logged).not.toContain('secret raw failure')
      expect(logged).not.toContain('private-user-token')
      expect(logged).not.toContain('recipe_save')
    } finally {
      console.warn = oldWarn
    }
  })

  it('classifies non-2xx responses and timeout aborts without rejecting', async () => {
    const warnings: unknown[][] = []
    const oldWarn = console.warn
    console.warn = (...args: unknown[]) => { warnings.push(args) }
    try {
      globalThis.fetch = mock(async () => new Response(null, { status: 503 })) as unknown as typeof fetch
      const client = createMCPToolTelemetryClient({
        baseUrl: 'https://api.boostedbudget.com', originSecret: 'origin', workerSecret: 'worker', getAccessToken: () => 'token',
      })
      await expect(client.record({
        tool_surface: 'search', tool_family: 'discovery', outcome: 'succeeded', duration_bucket: 'under_250ms',
      })).resolves.toBeUndefined()
      expect(JSON.stringify(warnings)).toContain('http_5xx')

      globalThis.fetch = mock((_input: RequestInfo | URL, init?: RequestInit) => new Promise<Response>((_resolve, reject) => {
        init?.signal?.addEventListener('abort', () => reject(init.signal?.reason), { once: true })
      })) as unknown as typeof fetch
      const timedClient = createMCPToolTelemetryClient({
        baseUrl: 'https://api.boostedbudget.com', originSecret: 'origin', workerSecret: 'worker', getAccessToken: () => 'token', timeoutMilliseconds: 1,
      })
      await expect(timedClient.record({
        tool_surface: 'search', tool_family: 'discovery', outcome: 'succeeded', duration_bucket: 'under_250ms',
      })).resolves.toBeUndefined()
      expect(JSON.stringify(warnings)).toContain('timeout')
    } finally {
      console.warn = oldWarn
    }
  })
})
