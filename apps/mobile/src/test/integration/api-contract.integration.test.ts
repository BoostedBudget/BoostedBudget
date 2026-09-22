import { afterEach, beforeEach, describe, expect, it } from '@jest/globals'
import MockAdapter from 'axios-mock-adapter'
import {
  AXIOS_INSTANCE,
  configureApiClient,
  setAuthTokenProvider,
  type ApiClientErrorDetails,
} from '@boostedbudget/api-client/client'
import { getHealth } from '@boostedbudget/api-client/endpoints/health/health'
import { cancelDeletion } from '@boostedbudget/api-client/endpoints/user/user'
import { recover, setAuthRecoveryHandler } from '../../bootstrap/api-runtime'

/**
 * Contract-integration tier (todo 21, #1657).
 *
 * Unlike the unit suites, this file imports the REAL `@boostedbudget/api-client`, attaches
 * `axios-mock-adapter` to the real `AXIOS_INSTANCE`, and drives requests through the REAL generated
 * functions (`getHealth`, `cancelDeletion`) so the whole client stack runs:
 *
 *   - the inline token attachment in `customInstance` (`client.ts:178-192`),
 *   - the synchronous request interceptor that stamps the runtime owner and base URL
 *     (`client.ts:117-126`),
 *   - the response interceptor that reports errors and applies the 401 gate
 *     (`client.ts:128-154`).
 *
 * Clerk is mocked at the adapter seam (`jest.setup.ts`), never here. There is no network and no
 * credentials: the mock adapter replaces the transport, so the tests are deterministic and CI-safe.
 * The file name ends in `*.integration.test.ts`, which jest's default `testMatch` discovers; it is
 * therefore part of the unchanged `test:ci` run (see the README's test-tier table).
 */

const BASE_URL = 'https://api.example.com'
const HEALTH_URL = `${BASE_URL}/health`
const CANCEL_PATH = '/api/v1/user/delete/cancel/cap-token-123'
const CANCEL_URL = `${BASE_URL}${CANCEL_PATH}`

interface RecordedHeaders {
  get?: (name: string) => unknown
  toJSON?: () => Record<string, unknown>
  Authorization?: unknown
  authorization?: unknown
}

interface RecordedRequest {
  url?: string
  baseURL?: string
  headers?: RecordedHeaders
}

/** Reads `Authorization` from an AxiosHeaders instance or a plain header object. */
function readAuthorization(headers: RecordedHeaders | undefined): string | undefined {
  if (!headers) return undefined
  if (typeof headers.get === 'function') {
    const value = headers.get('Authorization')
    return typeof value === 'string' ? value : undefined
  }
  const direct = headers.Authorization ?? headers.authorization
  return typeof direct === 'string' ? direct : undefined
}

function recordedRequest(index = 0): RecordedRequest {
  return mock.history[index] as unknown as RecordedRequest
}

/** The combined URL axios would have requested, as the mock adapter matches it. */
function outboundUrl(index = 0): string {
  const request = recordedRequest(index)
  return `${request.baseURL ?? ''}${request.url ?? ''}`
}

let mock: MockAdapter
let reportError: jest.Mock
const originalAdapter = AXIOS_INSTANCE.defaults.adapter

/**
 * Replicates the mobile wiring: one `configureApiClient` call passing the real `recover` bridge,
 * then the token provider that `AuthProviders` registers (`AuthProviders.tsx:77-87`). Calling
 * `configureApiClient` per test is a test-only reset of `hasEverAuthenticated`; the app calls it
 * exactly once.
 */
function configureRuntime(token: string | null): void {
  reportError = jest.fn()
  configureApiClient({ baseUrl: BASE_URL, onUnauthorized: recover, reportError })
  setAuthTokenProvider(async () => token)
}

beforeEach(() => {
  jest.clearAllMocks()
  setAuthRecoveryHandler(null)
  configureRuntime('tok_abc')
  mock = new MockAdapter(AXIOS_INSTANCE)
})

afterEach(() => {
  setAuthRecoveryHandler(null)
  mock.restore()
  AXIOS_INSTANCE.defaults.adapter = originalAdapter
  // Reset the shared runtime state to its defaults (test-only; see the note above).
  configureApiClient()
})

describe('contract-integration: bearer token and exact outbound URL', () => {
  it('attaches the Clerk bearer token and calls the generated health endpoint at its exact URL', async () => {
    mock.onGet(HEALTH_URL).reply(200, { status: 'ok' })

    const data = await getHealth()

    expect(data).toEqual({ status: 'ok' })
    expect(mock.history).toHaveLength(1)
    expect(recordedRequest().url).toBe('/health')
    expect(recordedRequest().baseURL).toBe(BASE_URL)
    expect(outboundUrl()).toBe(HEALTH_URL)
    expect(readAuthorization(recordedRequest().headers)).toBe('Bearer tok_abc')
  })

  it('composes a generated path parameter into the outbound URL and still attaches the token', async () => {
    mock.onGet(CANCEL_URL).reply(200, { status: 'cancelled' })

    await cancelDeletion('cap-token-123')

    expect(recordedRequest().url).toBe(CANCEL_PATH)
    expect(outboundUrl()).toBe(CANCEL_URL)
    expect(readAuthorization(recordedRequest().headers)).toBe('Bearer tok_abc')
  })

  it('sends no Authorization header when the token provider resolves null', async () => {
    configureRuntime(null)
    mock.onGet(HEALTH_URL).reply(200, { status: 'ok' })

    await getHealth()

    expect(readAuthorization(recordedRequest().headers)).toBeUndefined()
  })
})

describe('contract-integration: 401 gate', () => {
  it('dispatches the recovery bridge once for a 401 on a protected path after an authenticated request', async () => {
    const handler = jest.fn()
    setAuthRecoveryHandler(handler)
    mock.onGet(HEALTH_URL).reply(401, { message: 'unauthorized' })

    await expect(getHealth()).rejects.toThrow('Request failed with status code 401')

    expect(handler).toHaveBeenCalledTimes(1)
    const details = handler.mock.calls[0][0] as ApiClientErrorDetails
    expect(details.status).toBe(401)
    expect(details.url).toBe('/health')
    expect(readAuthorization(recordedRequest().headers)).toBe('Bearer tok_abc')
  })

  it('does not dispatch recovery when no request has ever carried a token', async () => {
    configureRuntime(null)
    const handler = jest.fn()
    setAuthRecoveryHandler(handler)
    mock.onGet(HEALTH_URL).reply(401)

    await expect(getHealth()).rejects.toThrow()

    expect(handler).not.toHaveBeenCalled()
  })

  it('does not dispatch recovery for a 401 on the public capability path, and reports it redacted', async () => {
    const handler = jest.fn()
    setAuthRecoveryHandler(handler)
    mock.onGet(CANCEL_URL).reply(401)

    await expect(cancelDeletion('cap-token-123')).rejects.toThrow()

    expect(handler).not.toHaveBeenCalled()
    // The response interceptor still reports the error, with the bearer capability token redacted.
    expect(reportError).toHaveBeenCalledTimes(1)
    const details = reportError.mock.calls[0][0] as ApiClientErrorDetails
    expect(details.status).toBe(401)
    expect(details.url).toBe('/api/v1/user/delete/cancel/[redacted]')
  })

  it('stays willing to recover after any authenticated request even when a later request carries no token', async () => {
    const handler = jest.fn()
    setAuthRecoveryHandler(handler)
    mock.onGet(HEALTH_URL).replyOnce(200, { status: 'ok' })

    await getHealth()

    // Swap the provider WITHOUT `configureApiClient`, so `hasEverAuthenticated` is preserved —
    // exactly the post-sign-out state documented in `src/bootstrap/api-runtime.ts`.
    setAuthTokenProvider(async () => null)
    mock.onGet(HEALTH_URL).replyOnce(401)

    await expect(getHealth()).rejects.toThrow()

    expect(handler).toHaveBeenCalledTimes(1)
  })
})
