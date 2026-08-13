/**
 * Custom Axios client for orval-generated API functions.
 * This module provides the customInstance function used by all generated API calls.
 */
import Axios, { AxiosError, AxiosRequestConfig } from 'axios'

type GetTokenFunction = () => Promise<string | null>

/**
 * Get the API URL from runtime or build-time configuration
 * Priority: window.ENV (runtime) > import.meta.env (build-time) > '/api' (fallback)
 */
export function getApiUrl(): string {
  // Runtime config (production) - injected by docker-entrypoint.sh
  if (typeof window !== 'undefined' && window.ENV?.API_URL) {
    return window.ENV.API_URL
  }

  // Build-time config (development)
  if (import.meta.env.VITE_API_URL) {
    return import.meta.env.VITE_API_URL
  }

  // Fallback for local dev with Vite proxy
  // Note: Empty string because generated endpoints already include /api/v1 prefix
  return ''
}

// Create axios instance with base configuration
export const AXIOS_INSTANCE = Axios.create({
  baseURL: getApiUrl(),
  timeout: 30000, // 30 second timeout to prevent infinite loading states
  headers: {
    'Content-Type': 'application/json',
  },
})

// Track whether the user has ever successfully authenticated (had a valid token).
// Only redirect on 401 after at least one successful auth, to avoid redirect
// loops during session hydration or on public pages.
let hasEverAuthenticated = false

// Public API paths that should never trigger a sign-in redirect on 401
const PUBLIC_API_PATHS = ['/api/v1/user/delete/cancel/']

/**
 * Issue #1514 (fix round 6): known route SHAPES whose path segment is a bearer
 * CAPABILITY token, not an opaque resource id — the backend validates the entire
 * request by the token alone (`/user/delete/cancel/{token}`,
 * `/households/invites/{token}[/accept|/decline]` — see
 * backend/internal/huma/operations/deletion.go and household.go).
 *
 * Duplicated here (not imported) rather than shared with
 * apps/web/src/shared/lib/piiScrubbing.ts's `redactUrlPath`, which is the more
 * heavily documented canonical version: this package does not depend on apps/web,
 * and apps/web depends on this package, so importing across that boundary would
 * invert it. Keep the two pattern lists in sync — add a new capability route to
 * both when one is introduced.
 *
 * Deliberately a route-SHAPE allowlist (regex per known route), not a value-shape
 * heuristic ("looks like a JWT"): shape-sniffing would either miss a non-JWT token
 * or over-redact legitimate opaque resource ids that are NOT capabilities on their
 * own (e.g. `/accounts/{id}`).
 */
const CAPABILITY_URL_PATH_PATTERNS: RegExp[] = [
  /(\/user\/delete\/cancel\/)[^/?#]+/gi,
  /(\/households\/invites\/)[^/?#]+/gi,
]

/** Replaces a known capability-token path segment with `[redacted]`. Exported for
 * direct unit testing (see client.test.ts). */
export function redactCapabilityUrlPath(url: string | undefined): string | undefined {
  if (!url) return url
  let out = url
  for (const pattern of CAPABILITY_URL_PATH_PATTERNS) {
    out = out.replace(pattern, (_match, prefix: string) => `${prefix}[redacted]`)
  }
  return out
}

// Response error interceptor
AXIOS_INSTANCE.interceptors.response.use(
  (response) => response,
  (error: AxiosError) => {
    // Extract request ID for tracing
    const requestId = error.response?.headers?.['x-request-id'] as string | undefined

    // Log structured error in development
    if (import.meta.env.DEV) {
      console.error('[API Error]', {
        url: redactCapabilityUrlPath(error.config?.url),
        status: error.response?.status,
        requestId,
      })
    }

    // Handle 401 — only redirect to sign-in if:
    // 1. The user was previously authenticated (token was used successfully), AND
    // 2. The request is not to a known public path
    // This prevents redirect loops during Clerk session hydration and on public pages.
    if (error.response?.status === 401) {
      const requestUrl = error.config?.url || ''
      const isPublicPath = PUBLIC_API_PATHS.some(path => requestUrl.includes(path))
      if (!isPublicPath && hasEverAuthenticated) {
        window.location.href = '/sign-in'
      }
    }

    // Handle 503 — could set maintenance state (future: dispatch to store)
    // if (error.response?.status === 503) { ... }

    return Promise.reject(error)
  }
)

// Token provider function - set via setAuthTokenProvider
let getAccessToken: GetTokenFunction | null = null

/**
 * Configure the auth token provider.
 * Called from main.tsx after Clerk is initialized.
 */
export const setAuthTokenProvider = (provider: GetTokenFunction) => {
  getAccessToken = provider
}

/**
 * Custom instance for orval-generated API functions.
 * This function is used as the mutator for all generated API calls.
 * It handles auth token injection and returns typed response data.
 */
export const customInstance = async <T>(
  config: AxiosRequestConfig,
  options?: AxiosRequestConfig
): Promise<T> => {
  // Merge configs
  const mergedConfig: AxiosRequestConfig = {
    ...config,
    ...options,
  }

  // Inject auth token if provider is configured
  if (getAccessToken) {
    try {
      const token = await getAccessToken()
      if (token) {
        hasEverAuthenticated = true
        mergedConfig.headers = {
          ...mergedConfig.headers,
          Authorization: `Bearer ${token}`,
        }
      }
    } catch (error) {
      console.error('Error getting access token:', error)
    }
  }

  const response = await AXIOS_INSTANCE(mergedConfig)
  return response.data
}

// Error type for better error handling
export type ErrorType<Error> = AxiosError<Error>

// Body type helper
export type BodyType<BodyData> = BodyData

// Export axios instance for cases where direct access is needed
export default AXIOS_INSTANCE
