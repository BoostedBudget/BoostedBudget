/**
 * Spending By Category Hook
 * Issue #953: Spending Reports Page
 */

import { useMemo } from 'react'
import {
  useGetSpendingByCategory as useGetSpendingByCategoryQuery,
} from '@boostedbudget/api-client/endpoints/analytics/analytics'
import type { SpendingByCategoryResponse } from '@boostedbudget/api-client/models'

interface SpendingFilters {
  startDate?: string
  endDate?: string
  accountTypes?: string[]
  category?: string
}

interface UseSpendingByCategoryOptions {
  filters: SpendingFilters
  enabled?: boolean
}

interface UseSpendingByCategoryResult {
  data: SpendingByCategoryResponse | null
  loading: boolean
  error: string | null
  refetch: () => Promise<void>
}

export function useSpendingByCategory(
  options: UseSpendingByCategoryOptions
): UseSpendingByCategoryResult {
  const { filters, enabled = true } = options

  const params = useMemo(() => {
    if (!filters.startDate || !filters.endDate) return undefined
    return {
      start_date: filters.startDate,
      end_date: filters.endDate,
      account_types: filters.accountTypes?.join(','),
      category: filters.category,
    }
  }, [filters])

  const shouldFetch = enabled && !!params

  const { data, isLoading, error, refetch: queryRefetch } = useGetSpendingByCategoryQuery(
    params as never,
    { query: { enabled: shouldFetch, staleTime: 60 * 1000 } }
  )

  return {
    data: data ?? null,
    loading: isLoading,
    error: error ? (error as Error).message ?? 'Failed to load spending data' : null,
    refetch: () => queryRefetch().then(() => {}),
  }
}
