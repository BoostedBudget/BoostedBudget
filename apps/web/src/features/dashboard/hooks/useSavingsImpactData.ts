// Issue #972: Savings Transfer Automation — savings impact data hook
import { useGetSavingsImpactMe } from '@boostedbudget/api-client/endpoints/savings-impact/savings-impact'
import type { UserSavingsImpact } from '@boostedbudget/api-client/models'

export interface UseSavingsImpactDataResult {
  data: UserSavingsImpact | null
  loading: boolean
  error: Error | null
}

export function useSavingsImpactData(): UseSavingsImpactDataResult {
  const query = useGetSavingsImpactMe({ query: { staleTime: 60 * 1000 } })
  return {
    data: query.data ?? null,
    loading: query.isLoading,
    error: query.error instanceof Error
      ? query.error
      : query.error
        ? new Error('Failed to load savings data')
        : null,
  }
}
