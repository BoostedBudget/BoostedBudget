import { useMemo } from 'react'
import {
  useGetIncomeSourceBreakdown as useGetIncomeSourceBreakdownQuery,
} from '@boostedbudget/api-client/endpoints/analytics/analytics'
import type { IncomeSourceBreakdownResponse } from '@boostedbudget/api-client/models'

interface IncomeFilters {
  startDate?: string
  endDate?: string
  accountTypes?: string[]
}

interface UseIncomeSourceBreakdownOptions {
  filters: IncomeFilters
  enabled?: boolean
}

interface UseIncomeSourceBreakdownResult {
  data: IncomeSourceBreakdownResponse | null
  loading: boolean
  error: string | null
  refetch: () => Promise<void>
}

export function useIncomeSourceBreakdown(
  options: UseIncomeSourceBreakdownOptions,
): UseIncomeSourceBreakdownResult {
  const { filters, enabled = true } = options

  const params = useMemo(() => {
    if (!filters.startDate || !filters.endDate) return undefined
    return {
      start_date: filters.startDate,
      end_date: filters.endDate,
      account_types: filters.accountTypes?.join(','),
    }
  }, [filters])

  const shouldFetch = enabled && !!params

  const { data, isLoading, error, refetch } = useGetIncomeSourceBreakdownQuery(
    params as never,
    { query: { enabled: shouldFetch, staleTime: 60 * 1000 } },
  )

  return {
    data: data ?? null,
    loading: isLoading,
    error: error ? (error as Error).message ?? 'Failed to load income data' : null,
    refetch: () => refetch().then(() => {}),
  }
}
