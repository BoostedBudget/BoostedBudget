/**
 * useSubscriptionTiers Hook
 *
 * React hook for fetching subscription tier configuration with 24hr caching.
 * Uses in-memory cache (for single session) and localStorage (for cache persistence).
 *
 * TDD GREEN: Implementation to pass all tests.
 */

import { useState, useEffect, useCallback } from 'react'
import { fetchSubscriptionTiers } from '../api/subscriptionApi'
import type { SubscriptionTiersResponse } from '../types'

// Constants
const CACHE_KEY_TIMESTAMP = 'subscription_tiers_cache_timestamp'
const CACHE_TTL_MS = 24 * 60 * 60 * 1000 // 24 hours in milliseconds

// Module-level in-memory cache
let memoryCache: SubscriptionTiersResponse | null = null

/**
 * Check if cached data is still valid based on timestamp
 */
function isCacheValid(): boolean {
  const cachedTimestamp = localStorage.getItem(CACHE_KEY_TIMESTAMP)
  if (!cachedTimestamp) {
    return false
  }

  const timestamp = Number(cachedTimestamp)
  const now = Date.now()
  const age = now - timestamp

  return age < CACHE_TTL_MS
}

/**
 * Update cache with fresh data
 */
function updateCache(data: SubscriptionTiersResponse): void {
  memoryCache = data
  localStorage.setItem(CACHE_KEY_TIMESTAMP, String(Date.now()))
}

/**
 * Clear the cache
 * @internal Exported for testing purposes
 */
export function clearCache(): void {
  memoryCache = null
  localStorage.removeItem(CACHE_KEY_TIMESTAMP)
}

export interface UseSubscriptionTiersResult {
  tiers: SubscriptionTiersResponse | null
  loading: boolean
  error: Error | null
  refetch: () => void
}

/**
 * Hook for fetching subscription tiers with caching
 *
 * @returns Object containing tiers data, loading state, error, and refetch function
 *
 * @example
 * ```tsx
 * function PricingPage() {
 *   const { tiers, loading, error, refetch } = useSubscriptionTiers()
 *
 *   if (loading) return <LoadingSpinner />
 *   if (error) return <ErrorMessage error={error} onRetry={refetch} />
 *
 *   return <TierCards tiers={tiers.tiers} />
 * }
 * ```
 */
export function useSubscriptionTiers(): UseSubscriptionTiersResult {
  const [tiers, setTiers] = useState<SubscriptionTiersResponse | null>(null)
  const [loading, setLoading] = useState<boolean>(true)
  const [error, setError] = useState<Error | null>(null)

  const loadTiers = useCallback(async (forceRefresh = false) => {
    try {
      setLoading(true)
      setError(null)

      // Check cache if not forcing refresh
      if (!forceRefresh && memoryCache && isCacheValid()) {
        // Use cached data
        setTiers(memoryCache)
        setLoading(false)
        return
      }

      // Fetch fresh data
      const data = await fetchSubscriptionTiers()

      // Update cache and state
      updateCache(data)
      setTiers(data)
    } catch (err) {
      setError(err instanceof Error ? err : new Error('Failed to fetch subscription tiers'))
      setTiers(null)
    } finally {
      setLoading(false)
    }
  }, [])

  const refetch = useCallback(() => {
    clearCache()
    loadTiers(true)
  }, [loadTiers])

  // Fetch on mount
  useEffect(() => {
    loadTiers()
  }, [loadTiers])

  return {
    tiers,
    loading,
    error,
    refetch,
  }
}
