import { afterEach, describe, expect, it, mock } from 'bun:test'
import { createMCPToolTelemetryClient } from './telemetry-client'
import { instrumentTool } from './tool-telemetry'

const originalFetch = globalThis.fetch

afterEach(() => {
  globalThis.fetch = originalFetch
})

class FakeTelemetryClock {
  private now = 0
  private tasks: Array<{ at: number, run: () => void }> = []

  after(milliseconds: number, run: () => void): void {
    this.tasks.push({ at: this.now + milliseconds, run })
  }

  timeoutSignal = (milliseconds: number): AbortSignal => {
    const controller = new AbortController()
    this.after(milliseconds, () => controller.abort(new DOMException('timed out', 'TimeoutError')))
    return controller.signal
  }

  advance(milliseconds: number): void {
    this.now += milliseconds
    const due = this.tasks.filter(task => task.at <= this.now)
    this.tasks = this.tasks.filter(task => task.at > this.now)
    due.sort((left, right) => left.at - right.at).forEach(task => task.run())
  }
}

function installDelayedTelemetryTransport(clock: FakeTelemetryClock, delayMilliseconds: number): void {
  globalThis.fetch = mock((_input: RequestInfo | URL, init?: RequestInit) => new Promise<Response>((resolve, reject) => {
    init?.signal?.addEventListener('abort', () => reject(init.signal?.reason), { once: true })
    clock.after(delayMilliseconds, () => resolve(new Response(null, { status: 204 })))
  })) as unknown as typeof fetch
}

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

  it('allows a four-second backend response within the default timeout without delaying the tool result', async () => {
    const clock = new FakeTelemetryClock()
    installDelayedTelemetryTransport(clock, 4_000)
    const requestedTimeouts: number[] = []
    const telemetry = createMCPToolTelemetryClient({
      baseUrl: 'https://api.boostedbudget.com',
      originSecret: 'origin',
      workerSecret: 'worker',
      getAccessToken: () => 'token',
      timeoutSignal: milliseconds => {
        requestedTimeouts.push(milliseconds)
        return clock.timeoutSignal(milliseconds)
      },
    })
    const scheduled: Promise<void>[] = []
    const wrapped = instrumentTool({
      surface: 'search',
      family: () => 'discovery',
      telemetry,
      schedule: promise => scheduled.push(promise),
      now: () => 0,
    }, async () => 'tool-result')

    await expect(wrapped({ query: 'private query' })).resolves.toBe('tool-result')
    expect(scheduled).toHaveLength(1)
    expect(requestedTimeouts).toEqual([5_000])

    clock.advance(4_000)
    await expect(scheduled[0]).resolves.toBeUndefined()
  })

  it('times out after five seconds without delaying the tool result or leaking an unhandled rejection', async () => {
    const clock = new FakeTelemetryClock()
    installDelayedTelemetryTransport(clock, 6_000)
    const warnings: unknown[][] = []
    const oldWarn = console.warn
    const unhandled: unknown[] = []
    const onUnhandled = (reason: unknown): void => { unhandled.push(reason) }
    console.warn = (...args: unknown[]) => { warnings.push(args) }
    process.on('unhandledRejection', onUnhandled)
    try {
      const telemetry = createMCPToolTelemetryClient({
        baseUrl: 'https://api.boostedbudget.com',
        originSecret: 'origin',
        workerSecret: 'worker',
        getAccessToken: () => 'token',
        timeoutSignal: clock.timeoutSignal,
      })
      const scheduled: Promise<void>[] = []
      const wrapped = instrumentTool({
        surface: 'search',
        family: () => 'discovery',
        telemetry,
        schedule: promise => scheduled.push(promise),
        now: () => 0,
      }, async () => 'tool-result')

      await expect(wrapped({})).resolves.toBe('tool-result')
      expect(scheduled).toHaveLength(1)

      clock.advance(5_000)
      await expect(scheduled[0]).resolves.toBeUndefined()
      await Promise.resolve()
      expect(JSON.stringify(warnings)).toContain('timeout')
      expect(unhandled).toEqual([])
    } finally {
      process.off('unhandledRejection', onUnhandled)
      console.warn = oldWarn
    }
  })
})
