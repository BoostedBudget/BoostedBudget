/**
 * Spending By Merchant Hook
 * Issue #953: Spending Reports Page
 */

import { useMemo } from 'react'
import {
  useGetSpendingByMerchant as useGetSpendingByMerchantQuery,
} from '@boostedbudget/api-client/endpoints/analytics/analytics'
import type { SpendingByMerchantResponse } from '@boostedbudget/api-client/models'

interface SpendingFilters {
  startDate?: string
  endDate?: string
  accountTypes?: string[]
}

interface UseSpendingByMerchantOptions {
  filters: SpendingFilters
  limit?: number
  enabled?: boolean
}

interface UseSpendingByMerchantResult {
  data: SpendingByMerchantResponse | null
  loading: boolean
  error: string | null
  refetch: () => Promise<void>
}

export function useSpendingByMerchant(
  options: UseSpendingByMerchantOptions
): UseSpendingByMerchantResult {
  const { filters, limit = 10, enabled = true } = options

  const params = useMemo(() => {
    if (!filters.startDate || !filters.endDate) return undefined
    return {
      start_date: filters.startDate,
      end_date: filters.endDate,
      account_types: filters.accountTypes?.join(','),
      limit,
    }
  }, [filters, limit])

  const shouldFetch = enabled && !!params

  const { data, isLoading, error, refetch: queryRefetch } = useGetSpendingByMerchantQuery(
    params as never,
    { query: { enabled: shouldFetch, staleTime: 60 * 1000 } }
  )

  return {
    data: data ?? null,
    loading: isLoading,
    error: error ? (error as Error).message ?? 'Failed to load merchant data' : null,
    refetch: () => queryRefetch().then(() => {}),
  }
}
