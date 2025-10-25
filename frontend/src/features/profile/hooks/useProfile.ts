/**
 * useProfile Hook
 *
 * React hook for fetching user profile data.
 * Simple hook without caching (profile changes infrequently, fetched on demand).
 *
 * TDD GREEN: Implementation to pass AccountsPage tests.
 */

import { useState, useEffect } from 'react'
import { fetchProfile } from '../api/profileApi'
import type { User } from '../types'

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
  const [user, setUser] = useState<User | null>(null)
  const [loading, setLoading] = useState<boolean>(true)
  const [error, setError] = useState<Error | null>(null)

  async function loadProfile() {
    try {
      setLoading(true)
      setError(null)
      const data = await fetchProfile()
      setUser(data)
    } catch (err) {
      setError(err instanceof Error ? err : new Error('Failed to fetch user profile'))
      setUser(null)
    } finally {
      setLoading(false)
    }
  }

  function refetch() {
    loadProfile()
  }

  // Fetch on mount
  useEffect(() => {
    loadProfile()
  }, [])

  return {
    user,
    loading,
    error,
    refetch,
  }
}
