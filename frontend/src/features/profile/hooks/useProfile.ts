/**
 * useProfile Hook
 *
 * React hook for fetching user profile data.
 * Uses generated React Query hook from OpenAPI spec with built-in caching.
 */

import { useGetUserProfile } from '@/api/generated/endpoints/profile/profile'
import type { User } from '@/api/generated/models'

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
export function useProfile(): UseProfileResult {
  const { data, isLoading, error, refetch: queryRefetch } = useGetUserProfile({
    query: {
      staleTime: 5 * 60 * 1000, // 5 minutes cache
      gcTime: 5 * 60 * 1000,
    },
  })

  return {
    user: data ?? null,
    loading: isLoading,
    error: error ? (error instanceof Error ? error : new Error('Failed to fetch user profile')) : null,
    refetch: queryRefetch,
  }
}
