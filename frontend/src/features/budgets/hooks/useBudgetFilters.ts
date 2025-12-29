/**
 * useBudgetFilters Hook
 * Issue #590: Inline Column Filtering
 *
 * Manages budget filter state with URL synchronization.
 * Filters are persisted in URL params for bookmarking/sharing.
 */

import { useCallback, useMemo } from 'react'
import { useSearchParams } from 'react-router-dom'
import type { RangeValue } from '@/shared/components/ColumnFilterDropdown'

/**
 * Budget filter state
 */
export interface BudgetFilters {
  /** Budget periods to filter by */
  period: string[]
  /** Minimum progress percentage */
  progressMin?: number
  /** Maximum progress percentage */
  progressMax?: number
}

/**
 * Result of useBudgetFilters hook
 */
export interface UseBudgetFiltersResult {
  /** Current filter state derived from URL */
  filters: BudgetFilters
  /** Period filter array */
  periodFilter: string[]
  /** Progress range filter */
  progressRange: RangeValue | null
  /** Set period filter */
  setPeriodFilter: (value: string[]) => void
  /** Set progress range filter */
  setProgressRange: (value: RangeValue | null) => void
  /** Clear all filters */
  clearAllFilters: () => void
  /** Number of active filters */
  activeFilterCount: number
  /** Whether any filters are active */
  hasActiveFilters: boolean
}

/**
 * Hook for managing budget filters with URL state synchronization
 *
 * @returns Filter state and management functions
 */
export function useBudgetFilters(): UseBudgetFiltersResult {
  const [searchParams, setSearchParams] = useSearchParams()

  // Parse filters from URL search params
  const filters = useMemo<BudgetFilters>(() => {
    const result: BudgetFilters = {
      period: [],
    }

    // Parse period (comma-separated)
    const period = searchParams.get('period')
    if (period) {
      result.period = period.split(',').filter(Boolean).map((s) => s.trim())
    }

    // Parse progress range
    const progressMin = searchParams.get('progress_min')
    if (progressMin) {
      const parsed = parseFloat(progressMin)
      if (!isNaN(parsed)) {
        result.progressMin = parsed
      }
    }

    const progressMax = searchParams.get('progress_max')
    if (progressMax) {
      const parsed = parseFloat(progressMax)
      if (!isNaN(parsed)) {
        result.progressMax = parsed
      }
    }

    return result
  }, [searchParams])

  // Derived values for easier component integration
  const periodFilter = filters.period
  const progressRange: RangeValue | null = useMemo(() => {
    if (filters.progressMin === undefined && filters.progressMax === undefined) {
      return null
    }
    return {
      min: filters.progressMin,
      max: filters.progressMax,
    }
  }, [filters.progressMin, filters.progressMax])

  // Set period filter
  const setPeriodFilter = useCallback(
    (value: string[]) => {
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev)
          if (value.length === 0) {
            next.delete('period')
          } else {
            next.set('period', value.join(','))
          }
          return next
        },
        { replace: true }
      )
    },
    [setSearchParams]
  )

  // Set progress range filter
  const setProgressRange = useCallback(
    (value: RangeValue | null) => {
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev)
          if (!value || (value.min === undefined && value.max === undefined)) {
            next.delete('progress_min')
            next.delete('progress_max')
          } else {
            if (value.min !== undefined) {
              next.set('progress_min', String(value.min))
            } else {
              next.delete('progress_min')
            }
            if (value.max !== undefined) {
              next.set('progress_max', String(value.max))
            } else {
              next.delete('progress_max')
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
        next.delete('period')
        next.delete('progress_min')
        next.delete('progress_max')
        return next
      },
      { replace: true }
    )
  }, [setSearchParams])

  // Calculate active filter count
  const activeFilterCount = useMemo(() => {
    let count = 0
    if (filters.period.length > 0) count++
    if (filters.progressMin !== undefined || filters.progressMax !== undefined) count++
    return count
  }, [filters])

  const hasActiveFilters = activeFilterCount > 0

  return {
    filters,
    periodFilter,
    progressRange,
    setPeriodFilter,
    setProgressRange,
    clearAllFilters,
    activeFilterCount,
    hasActiveFilters,
  }
}
