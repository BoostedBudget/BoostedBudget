/**
 * Runtime-neutral Axios client for Orval-generated API functions.
 *
 * Platform applications own environment discovery, navigation, and logging. They
 * provide those capabilities through configureApiClient before making requests.
 */
import Axios, { AxiosError, AxiosRequestConfig } from 'axios'

export type GetTokenFunction = () => Promise<string | null>

export interface ApiClientErrorDetails {
  url?: string
  status?: number
  requestId?: string
}

export interface ApiClientRuntimeConfig {
  baseUrl?: string
  getAccessToken?: GetTokenFunction | null
  onUnauthorized?: (details: ApiClientErrorDetails) => void
  reportError?: (details: ApiClientErrorDetails) => void
  debug?: boolean
  debugLog?: (message: string, details: ApiClientErrorDetails) => void
}

function normalizeBaseUrl(value: string | undefined): string {
  return value?.trim().replace(/\/+$/, '') ?? ''
}

interface ApiClientRuntimeState {
  config: ApiClientRuntimeConfig
  hasEverAuthenticated: boolean
}

const RUNTIME_STATE_KEY = '__boostedBudgetRuntimeState'
type RuntimeOwnedRequestConfig = AxiosRequestConfig & {
  [RUNTIME_STATE_KEY]?: ApiClientRuntimeState
}

let runtimeState: ApiClientRuntimeState = {
  config: {},
  hasEverAuthenticated: false,
}

/** Return the base URL configured by the host application. */
export function getApiUrl(): string {
  return normalizeBaseUrl(runtimeState.config.baseUrl)
}

// This singleton identity is part of the public compatibility surface. Generated
// calls and tests may customize it directly.
export const AXIOS_INSTANCE = Axios.create({
  baseURL: '',
  timeout: 30000,
  headers: {
    'Content-Type': 'application/json',
  },
})

/**
 * Replace every platform capability atomically.
 *
 * Replacement semantics prevent auth/navigation state from one runtime or test
 * leaking into the next. The response interceptor is installed only once below.
 */
export function configureApiClient(config: ApiClientRuntimeConfig = {}): void {
  runtimeState = {
    config: { ...config, baseUrl: normalizeBaseUrl(config.baseUrl) },
    hasEverAuthenticated: false,
  }
  AXIOS_INSTANCE.defaults.baseURL = runtimeState.config.baseUrl
}

// Public API paths that should never trigger a sign-in redirect on 401.
const PUBLIC_API_PATHS = ['/api/v1/user/delete/cancel/']

/** Known route shapes whose path segment is a bearer capability token. */
export const CAPABILITY_URL_PATH_PATTERNS: RegExp[] = [
  /(\/user\/delete\/cancel\/)[^/?#]+/gi,
  /(\/cancel-deletion\/)[^/?#]+/gi,
  /(\/households\/invites\/)[^/?#]+/gi,
  /(\/household\/invite\/)[^/?#]+/gi,
]

export function redactCapabilityUrlPath(url: string | undefined): string | undefined {
  if (!url) return url
  let out = url
  for (const pattern of CAPABILITY_URL_PATH_PATTERNS) {
    out = out.replace(pattern, (_match, prefix: string) => `${prefix}[redacted]`)
  }
  return out
}

function errorDetails(error: AxiosError): ApiClientErrorDetails {
  return {
    url: redactCapabilityUrlPath(error.config?.url),
    status: error.response?.status,
    requestId: error.response?.headers?.['x-request-id'] as string | undefined,
  }
}

function safelyNotify(
  callback: ((details: ApiClientErrorDetails) => void) | undefined,
  details: ApiClientErrorDetails,
): void {
  try {
    callback?.(details)
  } catch {
    // Observability and navigation integrations must never replace the API error.
  }
}

// Axios merges instance defaults into a request before running interceptors. A
// synchronous interceptor therefore captures both the runtime owner and effective
// base URL in the same call stack as every direct AXIOS_INSTANCE invocation. Calls
// from customInstance arrive pre-stamped with the snapshot taken before token lookup.
AXIOS_INSTANCE.interceptors.request.use(
  (config) => {
    const ownedConfig = config as RuntimeOwnedRequestConfig
    ownedConfig[RUNTIME_STATE_KEY] ??= runtimeState
    config.baseURL ??= ownedConfig[RUNTIME_STATE_KEY].config.baseUrl
    return config
  },
  undefined,
  { synchronous: true },
)

AXIOS_INSTANCE.interceptors.response.use(
  (response) => response,
  (error: AxiosError) => {
    const requestRuntime = (error.config as RuntimeOwnedRequestConfig | undefined)?.[RUNTIME_STATE_KEY]
      ?? runtimeState
    const details = errorDetails(error)
    safelyNotify(requestRuntime.config.reportError, details)

    if (requestRuntime.config.debug && requestRuntime.config.debugLog) {
      try {
        requestRuntime.config.debugLog('[API Error]', details)
      } catch {
        // Debug logging must never replace the API error.
      }
    }

    if (error.response?.status === 401) {
      const requestUrl = error.config?.url || ''
      const isPublicPath = PUBLIC_API_PATHS.some((path) => requestUrl.includes(path))
      if (!isPublicPath && requestRuntime.hasEverAuthenticated) {
        safelyNotify(requestRuntime.config.onUnauthorized, details)
      }
    }

    return Promise.reject(error)
  },
)

/** Compatibility helper for Clerk integration in the existing web app. */
export const setAuthTokenProvider = (provider: GetTokenFunction): void => {
  runtimeState = {
    config: { ...runtimeState.config, getAccessToken: provider },
    hasEverAuthenticated: runtimeState.hasEverAuthenticated,
  }
}

/** Mutator used by all Orval-generated API calls. */
export const customInstance = async <T>(
  config: AxiosRequestConfig,
  options?: AxiosRequestConfig,
): Promise<T> => {
  const requestRuntime = runtimeState
  const baseURL = options?.baseURL ?? config.baseURL ?? AXIOS_INSTANCE.defaults.baseURL
  const mergedConfig: RuntimeOwnedRequestConfig = {
    ...config,
    ...options,
    baseURL,
    [RUNTIME_STATE_KEY]: requestRuntime,
  }

  if (requestRuntime.config.getAccessToken) {
    try {
      const token = await requestRuntime.config.getAccessToken()
      if (token) {
        requestRuntime.hasEverAuthenticated = true
        mergedConfig.headers = {
          ...mergedConfig.headers,
          Authorization: `Bearer ${token}`,
        }
      }
    } catch {
      // A missing token remains an unauthenticated request. The host auth layer
      // owns reporting token-provider failures and can safely add user context.
    }
  }

  const response = await AXIOS_INSTANCE(mergedConfig)
  return response.data
}

export type ErrorType<Error> = AxiosError<Error>
export type BodyType<BodyData> = BodyData

export default AXIOS_INSTANCE
