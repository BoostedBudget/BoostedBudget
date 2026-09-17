/**
 * useProfile Hook
 *
 * React hook for fetching user profile data.
 * Uses generated React Query hook from OpenAPI spec with built-in caching.
 */

import { useGetUserProfile } from '@boostedbudget/api-client/endpoints/profile/profile'
import type { User } from '@boostedbudget/api-client/models'

export interface UseProfileResult {
  user: User | null
  loading: boolean
  error: Error | null
  refetch: () => void
}

/**
 * Hook for fetching user profile
 *
 * @returns Object containing user data, loading state, error, and refetch function
 *
 * @example
 * ```tsx
 * function ProfilePage() {
 *   const { user, loading, error, refetch } = useProfile()
 *
 *   if (loading) return <LoadingSpinner />
 *   if (error) return <ErrorMessage error={error} onRetry={refetch} />
 *
 *   return <UserInfo user={user} />
 * }
 * ```
 */
export interface UseProfileOptions {
  /**
   * When false, the profile query is not issued. Defaults to true so every existing
   * caller is unaffected.
   *
   * Needed because `usePostHogIdentify` is mounted at the App root and therefore runs on
   * public routes (landing, pricing) where there is no signed-in user. Without a gate it
   * would fire `GET /api/v1/user/profile` on every anonymous page view and take a 401 —
   * previously the profile was only ever fetched behind `PremiumRoute`.
   */
  enabled?: boolean
  /** Adds the Clerk identity to the cache key for app-root consumers that survive user switches. */
  userId?: string
}

export function useProfile(options: UseProfileOptions = {}): UseProfileResult {
  const { enabled = true, userId } = options
  const { data, isLoading, error, refetch: queryRefetch } = useGetUserProfile({
    query: {
      enabled,
      staleTime: 5 * 60 * 1000, // 5 minutes cache
      gcTime: 5 * 60 * 1000,
      ...(userId ? { queryKey: ['/api/v1/user/profile', userId] } : {}),
    },
  })

  return {
    user: data ?? null,
    loading: isLoading,
    error: error ? (error instanceof Error ? error : new Error('Failed to fetch user profile')) : null,
    refetch: queryRefetch,
  }
}
