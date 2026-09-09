import { useMemo } from 'react'
import { useGetCategoryHierarchy as useGetCategoryHierarchyQuery } from '@boostedbudget/api-client/endpoints/analytics/analytics'
import type { CategoryHierarchyResponse } from '@boostedbudget/api-client/models'

interface CategoryHierarchyFilters {
  startDate?: string
  endDate?: string
  accountTypes?: string[]
  customCategoryIds?: string[]
}

interface UseCategoryHierarchyOptions {
  filters: CategoryHierarchyFilters
  /**
   * Which side of the ledger. The endpoint's default depth is asymmetric on purpose — income reads
   * best one level down (wages, dividends, rental), expenses at the primary level.
   */
  direction: 'income' | 'expense'
  enabled?: boolean
}

interface UseCategoryHierarchyResult {
  data: CategoryHierarchyResponse | null
  loading: boolean
  error: string | null
  refetch: () => Promise<void>
}

export function useCategoryHierarchy(options: UseCategoryHierarchyOptions): UseCategoryHierarchyResult {
  const { filters, direction, enabled = true } = options

  const params = useMemo(() => {
    if (!filters.startDate || !filters.endDate) return undefined
    return {
      start_date: filters.startDate,
      end_date: filters.endDate,
      direction,
      account_types: filters.accountTypes?.join(','),
      custom_category_ids: filters.customCategoryIds?.length
        ? filters.customCategoryIds.join(',')
        : undefined,
    }
  }, [filters, direction])

  const shouldFetch = enabled && !!params

  const { data, isLoading, error, refetch } = useGetCategoryHierarchyQuery(
    params as never,
    { query: { enabled: shouldFetch, staleTime: 60 * 1000 } },
  )

  return {
    data: data ?? null,
    loading: isLoading,
    error: error ? (error as Error).message ?? 'Failed to load category hierarchy' : null,
    refetch: () => refetch().then(() => {}),
  }
}
