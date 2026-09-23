import { configureApiClient } from '@boostedbudget/api-client/client'
import { QueryClient } from '@tanstack/react-query'
import { MobilePublicEnvironment, parsePublicEnvironment } from '../config/public-env'
import { recover, reportApiError } from './api-runtime'

export function createMobileQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: {
        retry: 1,
        staleTime: 30_000,
        refetchOnWindowFocus: false,
      },
      mutations: { retry: false },
    },
  })
}

export function initializeMobileRuntime(environment: MobilePublicEnvironment) {
  const { apiUrl, clerkPublishableKey } = parsePublicEnvironment(environment)
  // The single configureApiClient call. `recover` and `reportApiError` are the mobile bridges into
  // the shared client's response interceptor; the recovery handler itself is registered later by
  // `AuthProviders`, and `configureApiClient` must never be called a second time (it resets
  // `hasEverAuthenticated`).
  configureApiClient({
    baseUrl: apiUrl,
    onUnauthorized: recover,
    reportError: reportApiError,
  })
  return {
    apiUrlConfigured: Boolean(apiUrl),
    authAvailable: Boolean(clerkPublishableKey),
    clerkPublishableKey,
  }
}
