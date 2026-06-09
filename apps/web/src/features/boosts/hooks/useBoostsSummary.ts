// Issue #1267: Boosts page — savings impact + active rules summary
import { useGetSavingsImpactMe } from '@boostedbudget/api-client/endpoints/savings-impact/savings-impact'
import { useGetAutomationRules } from '@boostedbudget/api-client/endpoints/automation-rules/automation-rules'

export interface UseBoostsSummaryResult {
  totalMoved: number | null
  /** Month-over-month change (null when prev month was $0 to avoid misleading % display) */
  momChange: number | null
  completedCount: number | null
  activeRulesCount: number
  isLoading: boolean
}

export function useBoostsSummary(): UseBoostsSummaryResult {
  const impactQuery = useGetSavingsImpactMe()
  const rulesQuery = useGetAutomationRules()

  const data = impactQuery.data
  const rules = rulesQuery.data ?? []

  const momChange = data && data.prev_month_amount !== 0 ? data.month_amount - data.prev_month_amount : null

  return {
    totalMoved: data?.total_amount ?? null,
    momChange,
    completedCount: data?.completed_count ?? null,
    activeRulesCount: rules.filter((r) => r.enabled).length,
    isLoading: impactQuery.isLoading || rulesQuery.isLoading,
  }
}
