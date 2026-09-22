import { configureApiClient } from '@boostedbudget/api-client/client'
import { QueryClient } from '@tanstack/react-query'
import { MobilePublicEnvironment, parsePublicEnvironment } from '../config/public-env'

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
  configureApiClient({ baseUrl: apiUrl })
  return {
    apiUrlConfigured: Boolean(apiUrl),
    authAvailable: Boolean(clerkPublishableKey),
    clerkPublishableKey,
  }
}
