import { useCheckRole } from '@boostedbudget/api-client/endpoints/admin/admin'

export interface UseIsAdminResult {
  isAdmin: boolean
  loading: boolean
  error: Error | null
  refetch: () => void
}

export function useIsAdmin(): UseIsAdminResult {
  const { data, isLoading, error, refetch } = useCheckRole({
    query: {
      staleTime: 5 * 60 * 1000, // 5 minutes
      gcTime: 5 * 60 * 1000, // 5 minutes
    },
  })

  return {
    isAdmin: data?.is_admin ?? false,
    loading: isLoading,
    error: error ? new Error('Failed to check admin role') : null,
    refetch,
  }
}
