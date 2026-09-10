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
   * Which side of the ledger. The endpoint returns the SAME full tree for both — direction selects
   * which transactions are aggregated, not how deep the result goes. How many levels to SHOW is the
   * CHART's choice: it renders income at leaf depth (wages, dividends, rental) and expenses at
   * primary depth. Saying "the endpoint's depth is asymmetric" here would restate the inaccuracy
   * the operation's own doc was corrected to remove.
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
