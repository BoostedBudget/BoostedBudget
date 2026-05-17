/**
 * useBudgetFilters Hook
 * Issue #590: Inline Column Filtering
 *
 * Manages budget filter state with URL synchronization.
 * Filters are persisted in URL params for bookmarking/sharing.
 */

import { useCallback, useMemo } from 'react'
import type { RangeValue } from '@/shared/components/ColumnFilterDropdown'
import { useFilterState, type FilterSpec } from '@/shared/hooks/useFilterState'

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

const SPEC = {
  period:   { kind: 'multi-select' },
  progress: { kind: 'range' },
} as const satisfies FilterSpec

/**
 * Hook for managing budget filters with URL state synchronization
 *
 * @returns Filter state and management functions
 */
export function useBudgetFilters(): UseBudgetFiltersResult {
  const state = useFilterState(SPEC)
  const { setFilter, clearAllFilters, activeFilterCount, hasActiveFilters } = state

  const filters = useMemo<BudgetFilters>(
    () => ({
      period: state.filters.period,
      progressMin: state.filters.progress?.min,
      progressMax: state.filters.progress?.max,
    }),
    [state.filters],
  )

  const progressRange = useMemo<RangeValue | null>(
    () => state.filters.progress ?? null,
    [state.filters.progress],
  )

  const setPeriodFilter = useCallback(
    (v: string[]) => setFilter('period', v),
    [setFilter],
  )

  const setProgressRange = useCallback(
    (value: RangeValue | null) => {
      setFilter('progress', value ?? undefined)
    },
    [setFilter],
  )

  return {
    filters,
    periodFilter: state.filters.period,
    progressRange,
    setPeriodFilter,
    setProgressRange,
    clearAllFilters,
    activeFilterCount,
    hasActiveFilters,
  }
}
