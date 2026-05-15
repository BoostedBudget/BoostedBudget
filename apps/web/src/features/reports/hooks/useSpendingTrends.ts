/**
 * Spending Trends Hook
 * Issue #953: Spending Reports Page
 */

import { useMemo } from 'react'
import {
  useGetSpendingTrends as useGetSpendingTrendsQuery,
} from '@boostedbudget/api-client/endpoints/analytics/analytics'
import type { SpendingTrendsResponse } from '@boostedbudget/api-client/models'

interface SpendingFilters {
  startDate?: string
  endDate?: string
  accountTypes?: string[]
}

interface UseSpendingTrendsOptions {
  filters: SpendingFilters
  enabled?: boolean
}

interface UseSpendingTrendsResult {
  data: SpendingTrendsResponse | null
  loading: boolean
  error: string | null
  refetch: () => Promise<void>
}

export function useSpendingTrends(
  options: UseSpendingTrendsOptions
): UseSpendingTrendsResult {
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

  const { data, isLoading, error, refetch: queryRefetch } = useGetSpendingTrendsQuery(
    params as never,
    { query: { enabled: shouldFetch, staleTime: 60 * 1000 } }
  )

  return {
    data: data ?? null,
    loading: isLoading,
    error: error ? (error as Error).message ?? 'Failed to load spending trends' : null,
    refetch: () => queryRefetch().then(() => {}),
  }
}
