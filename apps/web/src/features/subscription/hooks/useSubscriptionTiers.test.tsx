/**
 * useSubscriptionTiers Hook Tests
 * Issue #575: Migrated from MSW to centralized API mocks
 * Updated for simplified tier structure: premium + household tiers (no free tier)
 *
 * Tests the React hook with React Query caching strategy.
 */

import { describe, it, expect, beforeEach } from 'bun:test'
import { renderHook } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import React from 'react'
import { useSubscriptionTiers, clearCache } from './useSubscriptionTiers'
import { mockSubscriptionTiersResponse } from '@/test/fixtures/subscriptionFixtures'
import {
  mockUseGetSubscriptionTiers,
  mockRefetchSubscriptionTiers,
} from '@/test/mocks/subscription-api'

function createWrapper() {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: 0 },
    },
  })
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return (
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    )
  }
}

describe('useSubscriptionTiers', () => {
  beforeEach(() => {
    mockUseGetSubscriptionTiers.mockClear()
    mockRefetchSubscriptionTiers.mockClear()

    // Default successful response
    mockUseGetSubscriptionTiers.mockReturnValue({
      data: mockSubscriptionTiersResponse,
      isLoading: false,
      error: null,
      refetch: mockRefetchSubscriptionTiers,
    })
  })

  describe('Initial State', () => {
    it('should return loading=true initially', () => {
      mockUseGetSubscriptionTiers.mockReturnValue({
        data: undefined,
        isLoading: true,
        error: null,
        refetch: mockRefetchSubscriptionTiers,
      })

      const { result } = renderHook(() => useSubscriptionTiers(), {
        wrapper: createWrapper(),
      })

      expect(result.current.loading).toBe(true)
      expect(result.current.tiers).toBeNull()
      expect(result.current.error).toBeNull()
    })
  })

  describe('Successful Fetch', () => {
    it('should return tiers on successful fetch', () => {
      const { result } = renderHook(() => useSubscriptionTiers(), {
        wrapper: createWrapper(),
      })

      expect(result.current.loading).toBe(false)
      expect(result.current.tiers).toEqual(mockSubscriptionTiersResponse)
      expect(result.current.error).toBeNull()
    })

    it('should return correct tier structure', () => {
      const { result } = renderHook(() => useSubscriptionTiers(), {
        wrapper: createWrapper(),
      })

      const tiers = result.current.tiers
      // Default tier is now 'premium' (no free tier)
      expect(tiers?.default_tier).toBe('premium')
    })

    it('should return premium tier with correct limits', () => {
      const { result } = renderHook(() => useSubscriptionTiers(), {
        wrapper: createWrapper(),
      })

      const premiumTier = result.current.tiers?.tiers.premium
      expect(premiumTier?.display_name).toBe('Premium')
      // All tiers now have unlimited accounts
      expect(premiumTier?.limits.accounts).toBe(-1)
      expect(premiumTier?.limits.executions_per_day).toBe(-1)
    })

    it('should return premium tier as ad-free', () => {
      const { result } = renderHook(() => useSubscriptionTiers(), {
        wrapper: createWrapper(),
      })

      const premiumTier = result.current.tiers?.tiers.premium
      // All tiers are now ad-free
      expect(premiumTier?.features.ads_enabled).toBe(false)
      expect(premiumTier?.pricing.price_monthly).toBe(8.99)
    })

    it('should return household_5 tier with unlimited limits', () => {
      const { result } = renderHook(() => useSubscriptionTiers(), {
        wrapper: createWrapper(),
      })

      const household5 = result.current.tiers?.tiers.household_5
      expect(household5?.limits.accounts).toBe(-1)
      expect(household5?.limits.executions_per_day).toBe(-1)
      expect(household5?.features.ai_insights).toBe(true)
    })
  })

  describe('Error Handling', () => {
    it('should return error on failed fetch', () => {
      const error = new Error('Network error')
      mockUseGetSubscriptionTiers.mockReturnValue({
        data: undefined,
        isLoading: false,
        error,
        refetch: mockRefetchSubscriptionTiers,
      })

      const { result } = renderHook(() => useSubscriptionTiers(), {
        wrapper: createWrapper(),
      })

      expect(result.current.error).toBeTruthy()
      expect(result.current.tiers).toBeNull()
    })

    it('should wrap errors in Error instances', () => {
      mockUseGetSubscriptionTiers.mockReturnValue({
        data: undefined,
        isLoading: false,
        error: 'String error',
        refetch: mockRefetchSubscriptionTiers,
      })

      const { result } = renderHook(() => useSubscriptionTiers(), {
        wrapper: createWrapper(),
      })

      expect(result.current.error).toBeInstanceOf(Error)
    })
  })

  describe('Refetch', () => {
    it('should provide refetch function', () => {
      const { result } = renderHook(() => useSubscriptionTiers(), {
        wrapper: createWrapper(),
      })

      expect(typeof result.current.refetch).toBe('function')
    })

    it('should call underlying refetch when invoked', () => {
      const { result } = renderHook(() => useSubscriptionTiers(), {
        wrapper: createWrapper(),
      })

      result.current.refetch()

      expect(mockRefetchSubscriptionTiers).toHaveBeenCalled()
    })
  })

  describe('clearCache', () => {
    it('clearCache function exists for backward compatibility', () => {
      expect(typeof clearCache).toBe('function')
      expect(() => clearCache()).not.toThrow()
    })
  })
})
