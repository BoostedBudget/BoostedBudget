/**
 * useRuleFilters Hook
 * Issue #590: Inline Column Filtering
 *
 * Manages auto-categorization rule filter state with URL synchronization.
 * Filters are persisted in URL params for bookmarking/sharing.
 */

import { useCallback, useMemo } from 'react'
import { useSearchParams } from 'react-router-dom'
import type { RangeValue } from '@/shared/components/ColumnFilterDropdown'

/**
 * Rule filter state
 */
export interface RuleFilters {
  /** Rule statuses to filter by (enabled, disabled) */
  status: string[]
  /** Target categories to filter by */
  category: string[]
  /** Minimum priority */
  priorityMin?: number
  /** Maximum priority */
  priorityMax?: number
}

/**
 * Result of useRuleFilters hook
 */
export interface UseRuleFiltersResult {
  /** Current filter state derived from URL */
  filters: RuleFilters
  /** Status filter array */
  statusFilter: string[]
  /** Category filter array */
  categoryFilter: string[]
  /** Priority range filter */
  priorityRange: RangeValue | null
  /** Set status filter */
  setStatusFilter: (value: string[]) => void
  /** Set category filter */
  setCategoryFilter: (value: string[]) => void
  /** Set priority range filter */
  setPriorityRange: (value: RangeValue | null) => void
  /** Clear all filters */
  clearAllFilters: () => void
  /** Number of active filters */
  activeFilterCount: number
  /** Whether any filters are active */
  hasActiveFilters: boolean
}

/**
 * Hook for managing auto-categorization rule filters with URL state synchronization
 *
 * @returns Filter state and management functions
 */
export function useRuleFilters(): UseRuleFiltersResult {
  const [searchParams, setSearchParams] = useSearchParams()

  // Parse filters from URL search params
  const filters = useMemo<RuleFilters>(() => {
    const result: RuleFilters = {
      status: [],
      category: [],
    }

    // Parse status (comma-separated)
    const status = searchParams.get('status')
    if (status) {
      result.status = status.split(',').filter(Boolean).map((s) => s.trim())
    }

    // Parse category (comma-separated)
    const category = searchParams.get('category')
    if (category) {
      result.category = category.split(',').filter(Boolean).map((s) => s.trim())
    }

    // Parse priority range
    const priorityMin = searchParams.get('priority_min')
    if (priorityMin) {
      const parsed = parseInt(priorityMin, 10)
      if (!isNaN(parsed)) {
        result.priorityMin = parsed
      }
    }

    const priorityMax = searchParams.get('priority_max')
    if (priorityMax) {
      const parsed = parseInt(priorityMax, 10)
      if (!isNaN(parsed)) {
        result.priorityMax = parsed
      }
    }

    return result
  }, [searchParams])

  // Derived values for easier component integration
  const statusFilter = filters.status
  const categoryFilter = filters.category
  const priorityRange: RangeValue | null = useMemo(() => {
    if (filters.priorityMin === undefined && filters.priorityMax === undefined) {
      return null
    }
    return {
      min: filters.priorityMin,
      max: filters.priorityMax,
    }
  }, [filters.priorityMin, filters.priorityMax])

  // Set status filter
  const setStatusFilter = useCallback(
    (value: string[]) => {
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev)
          if (value.length === 0) {
            next.delete('status')
          } else {
            next.set('status', value.join(','))
          }
          return next
        },
        { replace: true }
      )
    },
    [setSearchParams]
  )

  // Set category filter
  const setCategoryFilter = useCallback(
    (value: string[]) => {
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev)
          if (value.length === 0) {
            next.delete('category')
          } else {
            next.set('category', value.join(','))
          }
          return next
        },
        { replace: true }
      )
    },
    [setSearchParams]
  )

  // Set priority range filter
  const setPriorityRange = useCallback(
    (value: RangeValue | null) => {
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev)
          if (!value || (value.min === undefined && value.max === undefined)) {
            next.delete('priority_min')
            next.delete('priority_max')
          } else {
            if (value.min !== undefined) {
              next.set('priority_min', String(value.min))
            } else {
              next.delete('priority_min')
            }
            if (value.max !== undefined) {
              next.set('priority_max', String(value.max))
            } else {
              next.delete('priority_max')
            }
          }
          return next
        },
        { replace: true }
      )
    },
    [setSearchParams]
  )

  // Clear all filters
  const clearAllFilters = useCallback(() => {
    setSearchParams(
      (prev) => {
        const next = new URLSearchParams(prev)
        next.delete('status')
        next.delete('category')
        next.delete('priority_min')
        next.delete('priority_max')
        return next
      },
      { replace: true }
    )
  }, [setSearchParams])

  // Calculate active filter count
  const activeFilterCount = useMemo(() => {
    let count = 0
    if (filters.status.length > 0) count++
    if (filters.category.length > 0) count++
    if (filters.priorityMin !== undefined || filters.priorityMax !== undefined) count++
    return count
  }, [filters])

  const hasActiveFilters = activeFilterCount > 0

  return {
    filters,
    statusFilter,
    categoryFilter,
    priorityRange,
    setStatusFilter,
    setCategoryFilter,
    setPriorityRange,
    clearAllFilters,
    activeFilterCount,
    hasActiveFilters,
  }
}
