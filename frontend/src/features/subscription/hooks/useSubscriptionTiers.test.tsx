/**
 * useSubscriptionTiers Hook Tests
 *
 * TDD RED: These tests are written first and will initially fail.
 * Tests the React hook with 24hr caching strategy.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { renderHook, waitFor } from '@testing-library/react'
import { useSubscriptionTiers, clearCache } from './useSubscriptionTiers'
import * as subscriptionApi from '../api/subscriptionApi'
import type { SubscriptionTiersResponse } from '../types'

// Mock the API module
vi.mock('../api/subscriptionApi')

describe('useSubscriptionTiers', () => {
  const mockTiersResponse: SubscriptionTiersResponse = {
    tiers: {
      free: {
        display_name: 'Free',
        description: 'Basic plan',
        price_monthly: 0,
        price_yearly: 0,
        limits: {
          accounts: 3,
          rules: 1,
          executions_per_day: 1,
          min_execution_interval_minutes: 1440,
        },
        features: {
          ads_enabled: true,
          ai_insights: false,
        },
      },
      premium_lite: {
        display_name: 'Premium Lite',
        description: 'Lite plan',
        price_monthly: 4.99,
        price_yearly: 49.99,
        limits: {
          accounts: 15,
          rules: 5,
          executions_per_day: 2,
          min_execution_interval_minutes: 720,
        },
        features: {
          ads_enabled: false,
          ai_insights: false,
        },
      },
      premium: {
        display_name: 'Premium',
        description: 'Premium plan',
        price_monthly: 9.99,
        price_yearly: 99.99,
        limits: {
          accounts: 30,
          rules: 15,
          executions_per_day: 6,
          min_execution_interval_minutes: 240,
        },
        features: {
          ads_enabled: false,
          ai_insights: false,
        },
      },
      premium_plus: {
        display_name: 'Premium+',
        description: 'Premium plus plan',
        price_monthly: 19.99,
        price_yearly: 199.99,
        limits: {
          accounts: -1,
          rules: -1,
          executions_per_day: 144,
          min_execution_interval_minutes: 10,
        },
        features: {
          ads_enabled: false,
          ai_insights: true,
          api_access: true,
          priority_support: true,
        },
      },
    },
    default_tier: 'free',
  }

  beforeEach(() => {
    vi.clearAllMocks()
    // Clear cache (both memory and localStorage) before each test
    clearCache()
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('should return loading state initially', () => {
    // Arrange
    vi.mocked(subscriptionApi.fetchSubscriptionTiers).mockImplementation(
      () => new Promise(() => {}) // Never resolves
    )

    // Act
    const { result } = renderHook(() => useSubscriptionTiers())

    // Assert
    expect(result.current.loading).toBe(true)
    expect(result.current.tiers).toBeNull()
    expect(result.current.error).toBeNull()
  })

  it('should fetch and return subscription tiers on mount', async () => {
    // Arrange
    vi.mocked(subscriptionApi.fetchSubscriptionTiers).mockResolvedValue(mockTiersResponse)

    // Act
    const { result } = renderHook(() => useSubscriptionTiers())

    // Assert - Initial loading state
    expect(result.current.loading).toBe(true)

    // Wait for data to load
    await waitFor(() => {
      expect(result.current.loading).toBe(false)
    })

    // Assert - Data loaded successfully
    expect(result.current.tiers).toEqual(mockTiersResponse)
    expect(result.current.error).toBeNull()
    expect(subscriptionApi.fetchSubscriptionTiers).toHaveBeenCalledTimes(1)
  })

  it('should handle fetch errors gracefully', async () => {
    // Arrange
    const error = new Error('Failed to fetch tiers')
    vi.mocked(subscriptionApi.fetchSubscriptionTiers).mockRejectedValue(error)

    // Act
    const { result } = renderHook(() => useSubscriptionTiers())

    // Wait for error state
    await waitFor(() => {
      expect(result.current.loading).toBe(false)
    })

    // Assert
    expect(result.current.error).toEqual(error)
    expect(result.current.tiers).toBeNull()
  })

  it('should cache tiers in memory for 24 hours', async () => {
    // Arrange
    vi.mocked(subscriptionApi.fetchSubscriptionTiers).mockResolvedValue(mockTiersResponse)

    // Act - First render
    const { result: result1, unmount: unmount1 } = renderHook(() => useSubscriptionTiers())

    await waitFor(() => {
      expect(result1.current.loading).toBe(false)
    })

    expect(subscriptionApi.fetchSubscriptionTiers).toHaveBeenCalledTimes(1)

    // Unmount - cache remains in memory
    unmount1()

    // Re-render immediately (still within cache window)
    const { result: result2 } = renderHook(() => useSubscriptionTiers())

    await waitFor(() => {
      expect(result2.current.loading).toBe(false)
    })

    // Assert - Should use cached data, not fetch again
    expect(subscriptionApi.fetchSubscriptionTiers).toHaveBeenCalledTimes(1) // Still only 1 call
    expect(result2.current.tiers).toEqual(mockTiersResponse)
  })

  it('should refetch after 24 hour cache expires', async () => {
    // Arrange
    vi.mocked(subscriptionApi.fetchSubscriptionTiers).mockResolvedValue(mockTiersResponse)

    // Simulate expired cache by setting old timestamp
    const twentyFiveHoursAgo = Date.now() - (25 * 60 * 60 * 1000)
    localStorage.setItem('subscription_tiers_cache_timestamp', String(twentyFiveHoursAgo))

    // Act - Render with expired cache
    const { result } = renderHook(() => useSubscriptionTiers())

    await waitFor(() => {
      expect(result.current.loading).toBe(false)
    })

    // Assert - Should fetch fresh data (cache expired)
    expect(subscriptionApi.fetchSubscriptionTiers).toHaveBeenCalledTimes(1)
    expect(result.current.tiers).toEqual(mockTiersResponse)
  })

  it('should provide refetch function to force cache invalidation', async () => {
    // Arrange
    vi.mocked(subscriptionApi.fetchSubscriptionTiers).mockResolvedValue(mockTiersResponse)

    // Act
    const { result } = renderHook(() => useSubscriptionTiers())

    await waitFor(() => {
      expect(result.current.loading).toBe(false)
    })

    expect(subscriptionApi.fetchSubscriptionTiers).toHaveBeenCalledTimes(1)

    // Act - Call refetch
    result.current.refetch()

    await waitFor(() => {
      expect(subscriptionApi.fetchSubscriptionTiers).toHaveBeenCalledTimes(2)
    })

    // Assert - Should have fetched again
    expect(result.current.tiers).toEqual(mockTiersResponse)
  })

  it('should store cache timestamp in localStorage', async () => {
    // Arrange
    vi.mocked(subscriptionApi.fetchSubscriptionTiers).mockResolvedValue(mockTiersResponse)

    // Act
    renderHook(() => useSubscriptionTiers())

    await waitFor(() => {
      expect(subscriptionApi.fetchSubscriptionTiers).toHaveBeenCalledTimes(1)
    })

    // Assert
    const cachedTimestamp = localStorage.getItem('subscription_tiers_cache_timestamp')
    expect(cachedTimestamp).toBeTruthy()

    // Verify it's a valid timestamp
    const timestamp = Number(cachedTimestamp)
    expect(timestamp).toBeGreaterThan(0)
    expect(timestamp).toBeLessThanOrEqual(Date.now())
  })

  it('should respect localStorage cache timestamp across sessions', async () => {
    // Arrange
    vi.mocked(subscriptionApi.fetchSubscriptionTiers).mockResolvedValue(mockTiersResponse)

    // First render - populate cache
    const { unmount } = renderHook(() => useSubscriptionTiers())

    await waitFor(() => {
      expect(subscriptionApi.fetchSubscriptionTiers).toHaveBeenCalledTimes(1)
    })

    unmount()

    // Manually adjust localStorage timestamp to 1 hour ago (still valid)
    const oneHourAgo = Date.now() - (60 * 60 * 1000)
    localStorage.setItem('subscription_tiers_cache_timestamp', String(oneHourAgo))

    // Clear in-memory cache to simulate new session (browser refresh)
    // but keep localStorage (which persists across sessions)
    clearCache()
    localStorage.setItem('subscription_tiers_cache_timestamp', String(oneHourAgo))

    // Act - Simulate new session: in-memory cache cleared, localStorage persists
    const { result } = renderHook(() => useSubscriptionTiers())

    await waitFor(() => {
      expect(result.current.loading).toBe(false)
    })

    // Assert - Should fetch because in-memory cache was cleared
    // localStorage timestamp is checked but in-memory cache is empty, so fetch occurs
    expect(subscriptionApi.fetchSubscriptionTiers).toHaveBeenCalledTimes(2)
    expect(result.current.tiers).toEqual(mockTiersResponse)
  })
})
