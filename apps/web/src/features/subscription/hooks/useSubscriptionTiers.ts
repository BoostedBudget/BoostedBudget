/**
 * useSubscriptionTiers Hook
 *
 * React hook for fetching subscription tier configuration.
 * Uses generated React Query hook from OpenAPI spec with built-in caching.
 */

import {
  useGetSubscriptionTiers,
} from '@boostedbudget/api-client/endpoints/subscription/subscription'
import type { SubscriptionTiersResponse } from '@boostedbudget/api-client/models'

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
  const { data, isLoading, error, refetch: queryRefetch } = useGetSubscriptionTiers({
    query: {
      staleTime: 24 * 60 * 60 * 1000, // 24 hours cache
      gcTime: 24 * 60 * 60 * 1000,
    },
  })

  // Issue #698: Validate response structure
  // Backend may return malformed data if config file is missing
  const isValidResponse = data && data.tiers && typeof data.tiers === 'object'

  // Create error for invalid response structure
  const validationError = !isLoading && data && !isValidResponse
    ? new Error('Invalid subscription tiers response: missing tiers data')
    : null

  return {
    tiers: isValidResponse ? data : null,
    loading: isLoading,
    error: error
      ? (error instanceof Error ? error : new Error('Failed to fetch subscription tiers'))
      : validationError,
    refetch: queryRefetch,
  }
}

/**
 * Clear the cache - for testing purposes
 * @internal
 */
export function clearCache(): void {
  // No-op: React Query handles cache automatically
  // Kept for backward compatibility with tests
}
