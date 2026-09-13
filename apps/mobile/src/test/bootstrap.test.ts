import { getApiUrl } from '@boostedbudget/api-client/client'
import { createMobileQueryClient, initializeMobileRuntime } from '../bootstrap/runtime'

describe('mobile runtime bootstrap', () => {
  it('configures the shared API client from validated public environment', () => {
    expect(initializeMobileRuntime({ EXPO_PUBLIC_API_URL: 'https://api.example.com/' })).toEqual({
      apiUrlConfigured: true,
    })
    expect(getApiUrl()).toBe('https://api.example.com')
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
