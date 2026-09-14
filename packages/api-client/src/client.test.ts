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
import { readdir } from 'node:fs/promises'
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

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((resolvePromise) => {
    resolve = resolvePromise
  })
  return { promise, resolve }
}

const forbiddenPackageCouplings = [
  ['vite/client', /vite\/client/],
  ['ImportMetaEnv', /\bImportMetaEnv\b/],
  ['ambient Window declaration', /\b(?:interface|type)\s+Window\b/],
  ['window.', /\bwindow\s*\./],
  ['document.', /\bdocument\s*\./],
  ['import.meta', /\bimport\.meta\b/],
  ['VITE_', /\bVITE_/],
] as const

function isTestSource(relativePath: string) {
  return relativePath.split('/').includes('__tests__')
    || /\.(?:test|spec)\.tsx?$/.test(relativePath)
}

async function findPackageSources(
  directory = new URL('./', import.meta.url),
  relativeDirectory = '',
): Promise<Array<{ relativePath: string; url: URL }>> {
  const entries = await readdir(directory, { withFileTypes: true })
  const sources: Array<{ relativePath: string; url: URL }> = []

  for (const entry of entries.sort((left, right) => left.name.localeCompare(right.name))) {
    const relativePath = `${relativeDirectory}${entry.name}`
    const url = new URL(entry.isDirectory() ? `${entry.name}/` : entry.name, directory)
    if (entry.isDirectory()) {
      sources.push(...await findPackageSources(url, `${relativePath}/`))
    } else if (/\.tsx?$/.test(relativePath) && !isTestSource(relativePath)) {
      sources.push({ relativePath, url })
    }
  }

  return sources
}

describe('portable API client runtime', () => {
  test('does not couple package-owned source to browser or Vite globals', async () => {
    const violations: string[] = []

    for (const { relativePath, url } of await findPackageSources()) {
      const source = await Bun.file(url).text()
      for (const [description, pattern] of forbiddenPackageCouplings) {
        if (pattern.test(source)) violations.push(`${relativePath}: ${description}`)
      }
    }

    expect(violations).toEqual([])
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

  test('overlapping requests retain their originating base URL and callbacks', async () => {
    const firstToken = deferred<string | null>()
    const requests: Array<{ baseUrl: string | undefined; authorization: unknown }> = []
    const unauthorized: string[] = []
    AXIOS_INSTANCE.defaults.adapter = async (config) => {
      requests.push({
        baseUrl: config.baseURL,
        authorization: config.headers.Authorization,
      })
      const internalConfig = requestConfig(config)
      throw new AxiosError('Unauthorized', undefined, internalConfig, undefined, {
        data: {},
        status: 401,
        statusText: 'Unauthorized',
        headers: {},
        config: internalConfig,
      })
    }

    configureApiClient({
      baseUrl: 'https://first.example.com',
      getAccessToken: () => firstToken.promise,
      onUnauthorized: () => { unauthorized.push('first') },
    })
    const firstRequest = customInstance({ url: '/api/v1/accounts' })

    configureApiClient({
      baseUrl: 'https://second.example.com',
      getAccessToken: async () => 'second-token',
      onUnauthorized: () => { unauthorized.push('second') },
    })
    const secondRequest = customInstance({ url: '/api/v1/accounts' })
    await expect(secondRequest).rejects.toBeInstanceOf(AxiosError)

    firstToken.resolve('first-token')
    await expect(firstRequest).rejects.toBeInstanceOf(AxiosError)

    expect(requests).toEqual([
      { baseUrl: 'https://second.example.com', authorization: 'Bearer second-token' },
      { baseUrl: 'https://first.example.com', authorization: 'Bearer first-token' },
    ])
    expect(unauthorized).toEqual(['second', 'first'])
  })

  test('overlapping direct axios requests retain their runtime ownership', async () => {
    const releaseFirst = deferred<void>()
    const firstStarted = deferred<void>()
    const requests: Array<{ baseUrl: string | undefined; url: string | undefined }> = []
    const unauthorized: string[] = []
    AXIOS_INSTANCE.defaults.adapter = async (config) => {
      requests.push({ baseUrl: config.baseURL, url: config.url })
      const internalConfig = requestConfig(config)
      if (config.url === '/prime') {
        return {
          data: { ok: true },
          status: 200,
          statusText: 'OK',
          headers: {},
          config: internalConfig,
        }
      }
      if (config.baseURL === 'https://first.example.com') {
        firstStarted.resolve()
        await releaseFirst.promise
      }
      throw new AxiosError('Unauthorized', undefined, internalConfig, undefined, {
        data: {},
        status: 401,
        statusText: 'Unauthorized',
        headers: {},
        config: internalConfig,
      })
    }

    configureApiClient({
      baseUrl: 'https://first.example.com',
      getAccessToken: async () => 'first-token',
      onUnauthorized: () => { unauthorized.push('first') },
    })
    await customInstance({ url: '/prime' })
    const firstRequest = AXIOS_INSTANCE({ url: '/overlap' })
    await firstStarted.promise

    configureApiClient({
      baseUrl: 'https://second.example.com',
      getAccessToken: async () => 'second-token',
      onUnauthorized: () => { unauthorized.push('second') },
    })
    await customInstance({ url: '/prime' })
    await expect(AXIOS_INSTANCE({ url: '/overlap' })).rejects.toBeInstanceOf(AxiosError)

    releaseFirst.resolve()
    await expect(firstRequest).rejects.toBeInstanceOf(AxiosError)

    expect(requests).toEqual([
      { baseUrl: 'https://first.example.com', url: '/prime' },
      { baseUrl: 'https://first.example.com', url: '/overlap' },
      { baseUrl: 'https://second.example.com', url: '/prime' },
      { baseUrl: 'https://second.example.com', url: '/overlap' },
    ])
    expect(unauthorized).toEqual(['second', 'first'])
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
