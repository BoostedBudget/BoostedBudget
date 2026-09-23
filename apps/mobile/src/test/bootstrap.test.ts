import { getApiUrl } from '@boostedbudget/api-client/client'
import { createMobileQueryClient, initializeMobileRuntime } from '../bootstrap/runtime'

describe('mobile runtime bootstrap', () => {
  it('configures the shared API client from validated public environment', () => {
    expect(initializeMobileRuntime({ EXPO_PUBLIC_API_URL: 'https://api.example.com/' })).toEqual({
      apiUrlConfigured: true,
      authAvailable: false,
      clerkPublishableKey: undefined,
    })
    expect(getApiUrl()).toBe('https://api.example.com')
  })

  it('reports auth availability and carries a valid Clerk publishable key', () => {
    expect(
      initializeMobileRuntime({
        EXPO_PUBLIC_API_URL: 'https://api.example.com/',
        EXPO_PUBLIC_CLERK_PUBLISHABLE_KEY: 'pk_test_abc123',
      }),
    ).toEqual({
      apiUrlConfigured: true,
      authAvailable: true,
      clerkPublishableKey: 'pk_test_abc123',
    })
  })

  it('keeps auth unavailable when the Clerk key is a secret-like value', () => {
    expect(
      initializeMobileRuntime({
        EXPO_PUBLIC_API_URL: 'https://api.example.com/',
        EXPO_PUBLIC_CLERK_PUBLISHABLE_KEY: 'sk_test_secret',
      }),
    ).toEqual({
      apiUrlConfigured: true,
      authAvailable: false,
      clerkPublishableKey: undefined,
    })
  })

  it('uses conservative query defaults without persistence or mutation retries', () => {
    const client = createMobileQueryClient()
    const queryDefaults = client.getDefaultOptions().queries
    const mutationDefaults = client.getDefaultOptions().mutations

    expect(queryDefaults?.retry).toBe(1)
    expect(queryDefaults?.staleTime).toBe(30_000)
    expect(mutationDefaults?.retry).toBe(false)
  })
})
