import { describe, expect, it, mock, beforeEach } from 'bun:test'
import { createFetchMutator, ApiError } from './fetch-mutator'

const CREDS = {
  authorization: 'Bearer test-token',
  originSecret: 'origin-secret-value',
  mcpWorkerSecret: 'worker-secret-value',
  grantId: 'grant-abc-123',
}
const BASE_URL = 'https://api.boostedbudget.com'

function makeSuccessResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

describe('createFetchMutator', () => {
  let fetchMock: ReturnType<typeof mock>

  beforeEach(() => {
    fetchMock = mock(() => Promise.resolve(makeSuccessResponse({ ok: true })))
    globalThis.fetch = fetchMock
  })

  it('injects Authorization header from credential provider', async () => {
    const mutator = createFetchMutator({
      baseUrl: BASE_URL,
      getCredentials: () => Promise.resolve(CREDS),
    })

    await mutator({ url: '/api/v1/analytics/cash-flow-summary', method: 'GET' })

    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    const headers = init.headers as Record<string, string>
    expect(headers['Authorization']).toBe('Bearer test-token')
    expect(url).toContain('/api/v1/analytics/cash-flow-summary')
  })

  it('injects X-Origin-Secret header from credential provider', async () => {
    const mutator = createFetchMutator({
      baseUrl: BASE_URL,
      getCredentials: () => Promise.resolve(CREDS),
    })

    await mutator({ url: '/api/v1/accounts', method: 'GET' })

    const [, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    const headers = init.headers as Record<string, string>
    expect(headers['X-Origin-Secret']).toBe('origin-secret-value')
  })

  it('R6-T1: injects X-MCP-Worker-Secret and X-MCP-Grant-Id from credential provider (security headers)', async () => {
    const mutator = createFetchMutator({
      baseUrl: BASE_URL,
      getCredentials: () => Promise.resolve(CREDS),
    })

    await mutator({ url: '/api/v1/budgets', method: 'GET' })

    const [, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    const headers = init.headers as Record<string, string>
    expect(headers['X-MCP-Worker-Secret']).toBe('worker-secret-value')
    expect(headers['X-MCP-Grant-Id']).toBe('grant-abc-123')
  })

  it('appends query params to URL', async () => {
    const mutator = createFetchMutator({
      baseUrl: BASE_URL,
      getCredentials: () => Promise.resolve(CREDS),
    })

    await mutator({
      url: '/api/v1/transactions',
      method: 'GET',
      params: { limit: 20, cursor: 'abc123', status: 'all' },
    })

    const [url] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(url).toContain('limit=20')
    expect(url).toContain('cursor=abc123')
    expect(url).toContain('status=all')
  })

  it('omits undefined/null query params', async () => {
    const mutator = createFetchMutator({
      baseUrl: BASE_URL,
      getCredentials: () => Promise.resolve(CREDS),
    })

    await mutator({
      url: '/api/v1/transactions',
      method: 'GET',
      params: { limit: 20, cursor: undefined, filter: null },
    })

    const [url] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(url).not.toContain('cursor=')
    expect(url).not.toContain('filter=')
  })

  it('serializes request body as JSON for POST', async () => {
    const mutator = createFetchMutator({
      baseUrl: BASE_URL,
      getCredentials: () => Promise.resolve(CREDS),
    })

    await mutator({
      url: '/api/v1/budgets',
      method: 'POST',
      data: { name: 'My Budget', amount: 1000 },
    })

    const [, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(init.body).toBe(JSON.stringify({ name: 'My Budget', amount: 1000 }))
    const headers = init.headers as Record<string, string>
    expect(headers['Content-Type']).toBe('application/json')
  })

  it('omits body for GET requests', async () => {
    const mutator = createFetchMutator({
      baseUrl: BASE_URL,
      getCredentials: () => Promise.resolve(CREDS),
    })

    await mutator({ url: '/api/v1/accounts', method: 'GET' })

    const [, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(init.body).toBeUndefined()
  })

  it('forwards AbortSignal', async () => {
    const mutator = createFetchMutator({
      baseUrl: BASE_URL,
      getCredentials: () => Promise.resolve(CREDS),
    })
    const controller = new AbortController()

    await mutator({ url: '/api/v1/accounts', method: 'GET', signal: controller.signal })

    const [, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(init.signal).toBe(controller.signal)
  })

  it('injects Idempotency-Key header when provided', async () => {
    const mutator = createFetchMutator({
      baseUrl: BASE_URL,
      getCredentials: () => Promise.resolve(CREDS),
    })

    await mutator(
      { url: '/api/v1/budgets', method: 'POST', data: {} },
      { idempotencyKey: 'idem-key-abc' },
    )

    const [, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    const headers = init.headers as Record<string, string>
    expect(headers['Idempotency-Key']).toBe('idem-key-abc')
  })

  it('returns parsed JSON response body', async () => {
    const responseBody = { transactions: [{ id: '1', amount: 42 }] }
    fetchMock = mock(() => Promise.resolve(makeSuccessResponse(responseBody)))
    globalThis.fetch = fetchMock

    const mutator = createFetchMutator({
      baseUrl: BASE_URL,
      getCredentials: () => Promise.resolve(CREDS),
    })

    const result = await mutator<typeof responseBody>({
      url: '/api/v1/transactions',
      method: 'GET',
    })

    expect(result).toEqual(responseBody)
  })

  it('returns undefined for 204 No Content', async () => {
    fetchMock = mock(() => Promise.resolve(new Response(null, { status: 204 })))
    globalThis.fetch = fetchMock

    const mutator = createFetchMutator({
      baseUrl: BASE_URL,
      getCredentials: () => Promise.resolve(CREDS),
    })

    const result = await mutator({ url: '/api/v1/something', method: 'DELETE' })
    expect(result).toBeUndefined()
  })

  it('throws ApiError with status + body on 4xx', async () => {
    const errorBody = { title: 'Not Found', status: 404 }
    fetchMock = mock(() =>
      Promise.resolve(new Response(JSON.stringify(errorBody), { status: 404 })),
    )
    globalThis.fetch = fetchMock

    const mutator = createFetchMutator({
      baseUrl: BASE_URL,
      getCredentials: () => Promise.resolve(CREDS),
    })

    let caught: ApiError | undefined
    try {
      await mutator({ url: '/api/v1/budgets/not-found', method: 'GET' })
    } catch (e) {
      caught = e as ApiError
    }

    expect(caught).toBeInstanceOf(ApiError)
    expect(caught?.status).toBe(404)
    expect(caught?.body).toEqual(errorBody)
  })

  it('throws ApiError on 5xx', async () => {
    fetchMock = mock(() =>
      Promise.resolve(new Response(JSON.stringify({ error: 'Server Error' }), { status: 500 })),
    )
    globalThis.fetch = fetchMock

    const mutator = createFetchMutator({
      baseUrl: BASE_URL,
      getCredentials: () => Promise.resolve(CREDS),
    })

    await expect(
      mutator({ url: '/api/v1/analytics/cash-flow-summary', method: 'GET' }),
    ).rejects.toBeInstanceOf(ApiError)
  })
})

describe('ApiError', () => {
  it('has the correct status and body', () => {
    const err = new ApiError(422, { title: 'Unprocessable' })
    expect(err.status).toBe(422)
    expect(err.body).toEqual({ title: 'Unprocessable' })
    expect(err).toBeInstanceOf(Error)
  })
})
