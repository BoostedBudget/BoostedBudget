/**
 * Fetch-based mutator for Cloudflare Workers.
 *
 * Mirrors the calling convention of the Orval-generated axios `customInstance`:
 *   customInstance<T>(config, options?) => Promise<T>
 *
 * Key differences from the React frontend's client.ts:
 *   - Uses native fetch() (no axios, no axios-specific types)
 *   - Credentials injected per-call via a CredentialProvider factory
 *   - No window/DOM APIs
 *   - Supports Idempotency-Key header injection
 */

export interface RequestConfig {
  url: string
  method: string
  params?: Record<string, unknown>
  data?: unknown
  signal?: AbortSignal
  headers?: Record<string, string>
}

export interface MutatorOptions {
  /** Optional idempotency key for write operations */
  idempotencyKey?: string
  /** Additional headers to merge in */
  headers?: Record<string, string>
}

export interface Credentials {
  /** Full "Bearer <token>" string */
  authorization: string
  /** Value for the X-Origin-Secret header (satisfies the backend's CloudflareOnly middleware) */
  originSecret: string
  /** Value for the X-MCP-Worker-Secret header (proves request originated from the MCP Worker) */
  mcpWorkerSecret: string
}

export type CredentialProvider = () => Promise<Credentials>

export interface FetchMutatorConfig {
  baseUrl: string
  getCredentials: CredentialProvider
}

export type FetchMutator = <T>(config: RequestConfig, opts?: MutatorOptions) => Promise<T>

/**
 * Structured error thrown on non-2xx responses.
 */
export class ApiError extends Error {
  constructor(
    public readonly status: number,
    public readonly body: unknown,
  ) {
    super(`API error ${status}`)
    this.name = 'ApiError'
  }
}

/**
 * Create a fetch-based mutator bound to a base URL and credential provider.
 * The credential provider is called on every request so tokens are always fresh.
 */
export function createFetchMutator(config: FetchMutatorConfig): FetchMutator {
  const { baseUrl, getCredentials } = config

  return async function fetchMutator<T>(
    reqConfig: RequestConfig,
    opts: MutatorOptions = {},
  ): Promise<T> {
    const { url, method, params, data, signal, headers: reqHeaders = {} } = reqConfig

    const fullUrl = new URL(url, baseUrl)
    if (params) {
      for (const [key, value] of Object.entries(params)) {
        if (value !== undefined && value !== null) {
          fullUrl.searchParams.set(key, String(value))
        }
      }
    }

    const creds = await getCredentials()

    const headers: Record<string, string> = {
      'Content-Type': 'application/json',
      Authorization: creds.authorization,
      'X-Origin-Secret': creds.originSecret,
      'X-MCP-Worker-Secret': creds.mcpWorkerSecret,
      ...reqHeaders,
      ...opts.headers,
    }

    if (opts.idempotencyKey) {
      headers['Idempotency-Key'] = opts.idempotencyKey
    }

    const response = await fetch(fullUrl.toString(), {
      method: method.toUpperCase(),
      headers,
      body: data !== undefined ? JSON.stringify(data) : undefined,
      signal,
    })

    if (response.status === 204) {
      return undefined as T
    }

    if (!response.ok) {
      const errorBody = await response.json().catch(() => ({ error: response.statusText }))
      throw new ApiError(response.status, errorBody)
    }

    return response.json() as Promise<T>
  }
}
