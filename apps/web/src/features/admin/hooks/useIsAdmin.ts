import { useAuth } from '@clerk/react-router'
import { useCheckRole } from '@boostedbudget/api-client/endpoints/admin/admin'

export interface UseIsAdminResult {
  isAdmin: boolean
  loading: boolean
  error: Error | null
  refetch: () => void
}

export function useIsAdmin(): UseIsAdminResult {
  const { isSignedIn } = useAuth()
  const { data, isLoading, error, refetch } = useCheckRole({
    query: {
      staleTime: 5 * 60 * 1000, // 5 minutes
      gcTime: 5 * 60 * 1000, // 5 minutes
      enabled: !!isSignedIn,
    },
  })

  return {
    isAdmin: data?.is_admin ?? false,
    loading: isSignedIn ? isLoading : false,
    error: error ? new Error('Failed to check admin role') : null,
    refetch,
  }
}
