// Reads the authenticated user's custom categories and derives their hierarchy (#1203).
import { useMemo } from 'react'
import { useGetUserCategories } from '@boostedbudget/api-client/endpoints/categories/categories'
import type { CustomCategory } from '../types'
import { buildCategoryHierarchy, type CategoryHierarchy } from '../utils/buildCategoryHierarchy'

export interface UseCustomCategoriesResult {
  categories: CustomCategory[]
  hierarchy: CategoryHierarchy
  isLoading: boolean
  error: Error | null
  refetch: () => void
}

export function useCustomCategories(): UseCustomCategoriesResult {
  const { data, isLoading, error, refetch } = useGetUserCategories()
  // Memoize so the array identity is stable for the hierarchy useMemo below.
  const categories = useMemo(() => (data ?? []) as CustomCategory[], [data])
  const hierarchy = useMemo(() => buildCategoryHierarchy(categories), [categories])

  return {
    categories,
    hierarchy,
    isLoading,
    error: (error as Error | null) ?? null,
    refetch,
  }
}
