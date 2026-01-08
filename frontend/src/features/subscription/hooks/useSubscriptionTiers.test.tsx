/**
 * useSubscriptionTiers Hook Tests
 *
 * Tests the React hook with React Query caching strategy.
 * Uses MSW for API mocking.
 *
 * Note: Kept on Vitest during Phase 2B due to MSW+axios+Bun compatibility issues.
 * Will migrate in Phase 2C once MSW workaround is established.
 */

import { describe, it, expect, afterEach, afterAll, beforeAll } from 'vitest'
import { renderHook, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { setupServer } from 'msw/node'
import { http, HttpResponse } from 'msw'
import React from 'react'
import { useSubscriptionTiers, clearCache } from './useSubscriptionTiers'
import { mockSubscriptionTiersResponse } from '@/test/fixtures/subscriptionFixtures'

// MSW server setup
const server = setupServer(
  http.get('*/api/v1/subscription/tiers', () => {
    return HttpResponse.json(mockSubscriptionTiersResponse)
  })
)

// Create a fresh QueryClient for each test
function createTestQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: {
        retry: false,
        gcTime: 0,
      },
    },
  })
}

// Wrapper component for tests
function createWrapper() {
  const queryClient = createTestQueryClient()
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return (
      <QueryClientProvider client={queryClient}>
        {children}
      </QueryClientProvider>
    )
  }
}

// Start server before tests (MSW + Bun compatibility)
server.listen({ onUnhandledRequest: 'error' })
console.log('[MSW] Server started')

describe('useSubscriptionTiers', () => {
  beforeAll(() => {
    console.log('[MSW] beforeAll - server should be listening')
  })

  afterEach(() => {
    server.resetHandlers()
    clearCache()
  })

  afterAll(() => {
    server.close()
    console.log('[MSW] Server closed')
  })

  it('should return loading state initially', async () => {
    // Use a delayed response to capture loading state
    server.use(
      http.get('*/api/v1/subscription/tiers', async () => {
        await new Promise((resolve) => setTimeout(resolve, 100))
        return HttpResponse.json(mockSubscriptionTiersResponse)
      })
    )

    const { result } = renderHook(() => useSubscriptionTiers(), {
      wrapper: createWrapper(),
    })

    // Assert initial loading state
    expect(result.current.loading).toBe(true)
    expect(result.current.tiers).toBeNull()
    expect(result.current.error).toBeNull()

    // Wait for completion
    await waitFor(() => {
      expect(result.current.loading).toBe(false)
    })
  })

  it('should fetch and return subscription tiers on mount', async () => {
    const { result } = renderHook(() => useSubscriptionTiers(), {
      wrapper: createWrapper(),
    })

    // Wait for data to load
    await waitFor(() => {
      expect(result.current.loading).toBe(false)
    })

    // Assert - Data loaded successfully
    expect(result.current.tiers).toEqual(mockSubscriptionTiersResponse)
    expect(result.current.error).toBeNull()
  })

  it('should handle fetch errors gracefully', async () => {
    server.use(
      http.get('*/api/v1/subscription/tiers', () => {
        return HttpResponse.json(
          { message: 'Server error' },
          { status: 500 }
        )
      })
    )

    const { result } = renderHook(() => useSubscriptionTiers(), {
      wrapper: createWrapper(),
    })

    // Wait for error state
    await waitFor(() => {
      expect(result.current.loading).toBe(false)
    })

    // Assert
    expect(result.current.error).toBeTruthy()
    expect(result.current.tiers).toBeNull()
  })

  it('should use React Query caching', async () => {
    let fetchCount = 0
    server.use(
      http.get('*/api/v1/subscription/tiers', () => {
        fetchCount++
        return HttpResponse.json(mockSubscriptionTiersResponse)
      })
    )

    // Use shared QueryClient for caching test
    const queryClient = createTestQueryClient()
    const wrapper = ({ children }: { children: React.ReactNode }) => (
      <QueryClientProvider client={queryClient}>
        {children}
      </QueryClientProvider>
    )

    // First render
    const { result: result1, unmount: unmount1 } = renderHook(
      () => useSubscriptionTiers(),
      { wrapper }
    )

    await waitFor(() => {
      expect(result1.current.loading).toBe(false)
    })

    expect(fetchCount).toBe(1)
    unmount1()

    // Second render with same QueryClient - should use cache
    const { result: result2 } = renderHook(
      () => useSubscriptionTiers(),
      { wrapper }
    )

    await waitFor(() => {
      expect(result2.current.loading).toBe(false)
    })

    // Should not fetch again - using cached data
    expect(fetchCount).toBe(1)
    expect(result2.current.tiers).toEqual(mockSubscriptionTiersResponse)
  })

  it('should provide refetch function to force cache invalidation', async () => {
    let fetchCount = 0
    server.use(
      http.get('*/api/v1/subscription/tiers', () => {
        fetchCount++
        return HttpResponse.json(mockSubscriptionTiersResponse)
      })
    )

    const { result } = renderHook(() => useSubscriptionTiers(), {
      wrapper: createWrapper(),
    })

    await waitFor(() => {
      expect(result.current.loading).toBe(false)
    })

    expect(fetchCount).toBe(1)

    // Call refetch
    result.current.refetch()

    await waitFor(() => {
      expect(fetchCount).toBe(2)
    })

    // Should have fetched again
    expect(result.current.tiers).toEqual(mockSubscriptionTiersResponse)
  })

  it('should return correct tier structure with all fields', async () => {
    const { result } = renderHook(() => useSubscriptionTiers(), {
      wrapper: createWrapper(),
    })

    await waitFor(() => {
      expect(result.current.loading).toBe(false)
    })

    // Verify tier structure
    const tiers = result.current.tiers
    expect(tiers).toBeTruthy()
    expect(tiers?.default_tier).toBe('free')

    // Check free tier
    const freeTier = tiers?.tiers.free
    expect(freeTier?.display_name).toBe('Free')
    expect(freeTier?.limits.accounts).toBe(3)
    expect(freeTier?.limits.rules).toBe(1)
    expect(freeTier?.features.ads_enabled).toBe(true)
    expect(freeTier?.pricing.price_monthly).toBe(0)

    // Check premium_plus unlimited limits
    const premiumPlus = tiers?.tiers.premium_plus
    expect(premiumPlus?.limits.accounts).toBe(-1)
    expect(premiumPlus?.limits.rules).toBe(-1)
    expect(premiumPlus?.features.ai_insights).toBe(true)
  })

  it('clearCache function exists for backward compatibility', () => {
    // clearCache is now a no-op but should exist
    expect(typeof clearCache).toBe('function')
    expect(() => clearCache()).not.toThrow()
  })
})
