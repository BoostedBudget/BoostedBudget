// Fetches live usage counts for a custom category, for the delete-impact warning (#1203).
import { useGetCategoryUsage } from '@boostedbudget/api-client/endpoints/categories/categories'
import type { CategoryUsage } from '../types'

export interface UseCategoryUsageResult {
  usage: CategoryUsage | null
  isLoading: boolean
  error: Error | null
}

/**
 * Loads usage counts for the given category. Pass enabled=false (e.g. when no
 * category is selected) to skip the request.
 */
export function useCategoryUsage(categoryID: string | null, enabled = true): UseCategoryUsageResult {
  const { data, isLoading, error } = useGetCategoryUsage(categoryID ?? '', {
    query: { enabled: enabled && !!categoryID },
  })

  return {
    usage: (data as CategoryUsage | undefined) ?? null,
    isLoading,
    error: (error as Error | null) ?? null,
  }
}
