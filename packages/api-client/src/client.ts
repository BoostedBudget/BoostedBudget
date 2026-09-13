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

let runtimeConfig: ApiClientRuntimeConfig = {}
let hasEverAuthenticated = false

/** Return the base URL configured by the host application. */
export function getApiUrl(): string {
  return normalizeBaseUrl(runtimeConfig.baseUrl)
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
  runtimeConfig = { ...config, baseUrl: normalizeBaseUrl(config.baseUrl) }
  hasEverAuthenticated = false
  AXIOS_INSTANCE.defaults.baseURL = runtimeConfig.baseUrl
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

AXIOS_INSTANCE.interceptors.response.use(
  (response) => response,
  (error: AxiosError) => {
    const details = errorDetails(error)
    safelyNotify(runtimeConfig.reportError, details)

    if (runtimeConfig.debug && runtimeConfig.debugLog) {
      try {
        runtimeConfig.debugLog('[API Error]', details)
      } catch {
        // Debug logging must never replace the API error.
      }
    }

    if (error.response?.status === 401) {
      const requestUrl = error.config?.url || ''
      const isPublicPath = PUBLIC_API_PATHS.some((path) => requestUrl.includes(path))
      if (!isPublicPath && hasEverAuthenticated) {
        safelyNotify(runtimeConfig.onUnauthorized, details)
      }
    }

    return Promise.reject(error)
  },
)

/** Compatibility helper for Clerk integration in the existing web app. */
export const setAuthTokenProvider = (provider: GetTokenFunction): void => {
  runtimeConfig = { ...runtimeConfig, getAccessToken: provider }
}

/** Mutator used by all Orval-generated API calls. */
export const customInstance = async <T>(
  config: AxiosRequestConfig,
  options?: AxiosRequestConfig,
): Promise<T> => {
  const mergedConfig: AxiosRequestConfig = {
    ...config,
    ...options,
  }

  if (runtimeConfig.getAccessToken) {
    try {
      const token = await runtimeConfig.getAccessToken()
      if (token) {
        hasEverAuthenticated = true
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
