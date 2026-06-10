// Issue #1267: Boosts page — savings impact + active boosts summary
import { useGetSavingsImpactMe } from '@boostedbudget/api-client/endpoints/savings-impact/savings-impact'
import { useGetBoosts } from '@boostedbudget/api-client/endpoints/boosts/boosts'
import { TRANSFER_GENERATING_BOOST_TYPES } from '../constants'

export interface UseBoostsSummaryResult {
  totalMoved: number | null
  /** Month-over-month change (null when prev month was $0 to avoid misleading % display) */
  momChange: number | null
  completedCount: number | null
  activeBoostsCount: number
  isLoading: boolean
  error: Error | null
  refetch: () => void
}

export function useBoostsSummary(): UseBoostsSummaryResult {
  const impactQuery = useGetSavingsImpactMe()
  const boostsQuery = useGetBoosts()

  const data = impactQuery.data
  const boosts = boostsQuery.data ?? []

  const momChange = data && data.prev_month_amount !== 0 ? data.month_amount - data.prev_month_amount : null

  return {
    totalMoved: data?.total_amount ?? null,
    momChange,
    completedCount: data?.completed_count ?? null,
    activeBoostsCount: boosts.filter(
      (b) => b.enabled && TRANSFER_GENERATING_BOOST_TYPES.includes(b.boost_type as (typeof TRANSFER_GENERATING_BOOST_TYPES)[number])
    ).length,
    isLoading: impactQuery.isLoading || boostsQuery.isLoading,
    error: (impactQuery.error ?? boostsQuery.error) as Error | null,
    refetch: () => {
      void impactQuery.refetch()
      void boostsQuery.refetch()
    },
  }
}
