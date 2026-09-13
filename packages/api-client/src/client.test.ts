/**
 * client.ts tests.
 *
 * Issue #1514 (fix round 6): the dev-mode error logger in this file logged
 * `error.config?.url` verbatim. For the account-deletion cancellation endpoint
 * (`/api/v1/user/delete/cancel/{token}` — a PUBLIC endpoint the backend validates
 * by the token alone, see backend/internal/huma/operations/deletion.go), that
 * token is a bearer capability, not a resource id — logging it verbatim (even in
 * dev-only console output) is a real exposure, not a cosmetic one.
 *
 * This is the first test file in this package (no prior test infrastructure).
 * `bun test` runs it directly with no browser preload or module mocking.
 */
import { afterEach, describe, expect, test } from 'bun:test'
import type { AxiosRequestConfig, InternalAxiosRequestConfig } from 'axios'
import { AxiosError, AxiosHeaders } from 'axios'
import {
  AXIOS_INSTANCE,
  configureApiClient,
  customInstance,
  getApiUrl,
  redactCapabilityUrlPath,
  setAuthTokenProvider,
} from './client'

const originalAdapter = AXIOS_INSTANCE.defaults.adapter

afterEach(() => {
  configureApiClient()
  AXIOS_INSTANCE.defaults.adapter = originalAdapter
})

function requestConfig(config: AxiosRequestConfig): InternalAxiosRequestConfig {
  return {
    ...config,
    headers: AxiosHeaders.from(config.headers),
  } as InternalAxiosRequestConfig
}

function respondWith(status: number, headers: Record<string, string> = {}) {
  AXIOS_INSTANCE.defaults.adapter = async (config) => {
    if (status >= 400) {
      throw new AxiosError(
        `Request failed with status code ${status}`,
        undefined,
        requestConfig(config),
        undefined,
        {
          data: {},
          status,
          statusText: String(status),
          headers,
          config: requestConfig(config),
        },
      )
    }

    return {
      data: { ok: true },
      status,
      statusText: 'OK',
      headers,
      config: requestConfig(config),
    }
  }
}

describe('portable API client runtime', () => {
  test('does not couple package source to browser or Vite globals', async () => {
    const source = await Bun.file(new URL('./client.ts', import.meta.url)).text()

    expect(source).not.toContain('window.')
    expect(source).not.toContain('document.')
    expect(source).not.toContain('import.meta')
  })

  test('imports with no browser globals and retains compatibility exports', async () => {
    expect(typeof globalThis.window).toBe('undefined')
    expect(typeof configureApiClient).toBe('function')
    expect(typeof customInstance).toBe('function')
    expect(typeof setAuthTokenProvider).toBe('function')
    expect(AXIOS_INSTANCE.defaults.timeout).toBe(30000)
    expect(AXIOS_INSTANCE.defaults.headers['Content-Type']).toBe('application/json')
  })

  test('normalizes the configured base URL without changing the axios singleton', () => {
    const originalInstance = AXIOS_INSTANCE

    configureApiClient({ baseUrl: 'https://api.example.com///' })

    expect(AXIOS_INSTANCE).toBe(originalInstance)
    expect(getApiUrl()).toBe('https://api.example.com')
    expect(AXIOS_INSTANCE.defaults.baseURL).toBe('https://api.example.com')
  })

  test('injects the configured bearer token while retaining request headers', async () => {
    let request: AxiosRequestConfig | undefined
    AXIOS_INSTANCE.defaults.adapter = async (config) => {
      request = config
      return {
        data: { ok: true },
        status: 200,
        statusText: 'OK',
        headers: {},
        config: requestConfig(config),
      }
    }
    configureApiClient({ getAccessToken: async () => 'native-token' })

    await customInstance({ url: '/api/v1/accounts', headers: { 'X-Caller': 'mobile' } })

    expect(request?.headers?.Authorization).toBe('Bearer native-token')
    expect(request?.headers?.['X-Caller']).toBe('mobile')
  })

  test('invokes unauthorized only after a token was obtained', async () => {
    let unauthorizedCalls = 0
    respondWith(401)
    configureApiClient({
      getAccessToken: async () => null,
      onUnauthorized: () => { unauthorizedCalls += 1 },
    })
    await expect(customInstance({ url: '/api/v1/accounts' })).rejects.toBeInstanceOf(AxiosError)
    expect(unauthorizedCalls).toBe(0)

    setAuthTokenProvider(async () => 'valid-token')
    await expect(customInstance({ url: '/api/v1/accounts' })).rejects.toBeInstanceOf(AxiosError)
    expect(unauthorizedCalls).toBe(1)
  })

  test('never invokes unauthorized for public capability paths', async () => {
    let unauthorizedCalls = 0
    respondWith(401)
    configureApiClient({
      getAccessToken: async () => 'valid-token',
      onUnauthorized: () => { unauthorizedCalls += 1 },
    })

    await expect(customInstance({ url: '/api/v1/user/delete/cancel/secret' })).rejects.toBeInstanceOf(AxiosError)

    expect(unauthorizedCalls).toBe(0)
  })

  test('reports only redacted request metadata to the configured error capability', async () => {
    const reports: unknown[] = []
    respondWith(503, { 'x-request-id': 'req-123' })
    configureApiClient({ reportError: (details) => reports.push(details) })

    await expect(
      customInstance({ url: '/api/v1/households/invites/super-secret/accept' }),
    ).rejects.toBeInstanceOf(AxiosError)

    expect(reports).toEqual([{
      url: '/api/v1/households/invites/[redacted]/accept',
      status: 503,
      requestId: 'req-123',
    }])
    expect(JSON.stringify(reports)).not.toContain('super-secret')
  })

  test('reconfiguration replaces runtime state instead of leaking authentication', async () => {
    let firstUnauthorizedCalls = 0
    let secondUnauthorizedCalls = 0
    respondWith(401)
    configureApiClient({
      baseUrl: 'https://first.example.com',
      getAccessToken: async () => 'first-token',
      onUnauthorized: () => { firstUnauthorizedCalls += 1 },
    })
    await expect(customInstance({ url: '/api/v1/accounts' })).rejects.toBeInstanceOf(AxiosError)
    expect(firstUnauthorizedCalls).toBe(1)

    configureApiClient({
      baseUrl: 'https://second.example.com/',
      onUnauthorized: () => { secondUnauthorizedCalls += 1 },
    })
    await expect(customInstance({ url: '/api/v1/accounts' })).rejects.toBeInstanceOf(AxiosError)

    expect(firstUnauthorizedCalls).toBe(1)
    expect(secondUnauthorizedCalls).toBe(0)
    expect(getApiUrl()).toBe('https://second.example.com')
  })
})

describe('redactCapabilityUrlPath', () => {
  const TOKEN = 'eyJhbGciOiJIUzI1NiJ9.SENTINEL_CAPABILITY_TOKEN.abc123'

  test('redacts the account-deletion cancellation token path segment', () => {
    const url = `/api/v1/user/delete/cancel/${TOKEN}`
    const result = redactCapabilityUrlPath(url)
    expect(result).not.toContain(TOKEN)
    expect(result).toBe('/api/v1/user/delete/cancel/[redacted]')
  })

  test('redacts the household-invite token path segment, including trailing /accept', () => {
    const url = `/api/v1/households/invites/${TOKEN}/accept`
    const result = redactCapabilityUrlPath(url)
    expect(result).not.toContain(TOKEN)
    expect(result).toBe('/api/v1/households/invites/[redacted]/accept')
  })

  // Issue #1514 (fix round 7): round 6 only covered the API path shape. The frontend page
  // route the emailed link actually points at (`/cancel-deletion/{token}`, App.tsx) is a
  // separate shape this dev logger's `error.config?.url` would never carry (it only ever
  // logs API request URLs) — but this package is now the single source of truth for the
  // whole pattern list (see client.ts), so it's tested here regardless of whether this
  // particular call site can reach it, to keep the list itself correct.
  test('redacts the frontend cancel-deletion page route', () => {
    const url = `/cancel-deletion/${TOKEN}`
    const result = redactCapabilityUrlPath(url)
    expect(result).not.toContain(TOKEN)
    expect(result).toBe('/cancel-deletion/[redacted]')
  })

  test('redacts the legacy /household/invite/{token} alias route', () => {
    const url = `/household/invite/${TOKEN}`
    const result = redactCapabilityUrlPath(url)
    expect(result).not.toContain(TOKEN)
    expect(result).toBe('/household/invite/[redacted]')
  })

  test('leaves a URL with no known capability route unchanged', () => {
    const url = '/api/v1/accounts/acc_123'
    expect(redactCapabilityUrlPath(url)).toBe(url)
  })

  test('passes through undefined unchanged', () => {
    expect(redactCapabilityUrlPath(undefined)).toBeUndefined()
  })
})
