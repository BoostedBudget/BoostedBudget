/**
 * useRuleFilters Hook
 * Issue #590: Inline Column Filtering
 *
 * Manages auto-categorization rule filter state with URL synchronization.
 * Thin wrapper around the generic useFilterState hook.
 */

import { useCallback, useMemo } from 'react'
import type { RangeValue } from '@/shared/components/ColumnFilterDropdown'
import { useFilterState, type FilterSpec } from '@/shared/hooks/useFilterState'


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

const SPEC = {
  status:   { kind: 'multi-select' },
  category: { kind: 'multi-select' },
  priority: { kind: 'range', codec: 'integer' as const },
} as const satisfies FilterSpec

/**
 * Hook for managing auto-categorization rule filters with URL state synchronization
 *
 * @returns Filter state and management functions
 */
export function useRuleFilters(): UseRuleFiltersResult {
  const state = useFilterState(SPEC)
  const { setFilter, clearAllFilters, activeFilterCount, hasActiveFilters } = state

  const filters = useMemo<RuleFilters>(
    () => ({
      status: state.filters.status,
      category: state.filters.category,
      priorityMin: state.filters.priority?.min,
      priorityMax: state.filters.priority?.max,
    }),
    [state.filters],
  )

  const priorityRange = useMemo<RangeValue | null>(
    () => state.filters.priority ?? null,
    [state.filters.priority],
  )

  const setStatusFilter = useCallback(
    (v: string[]) => setFilter('status', v),
    [setFilter],
  )

  const setCategoryFilter = useCallback(
    (v: string[]) => setFilter('category', v),
    [setFilter],
  )

  const setPriorityRange = useCallback(
    (value: RangeValue | null) => {
      setFilter('priority', value ?? undefined)
    },
    [setFilter],
  )

  return {
    filters,
    statusFilter: state.filters.status,
    categoryFilter: state.filters.category,
    priorityRange,
    setStatusFilter,
    setCategoryFilter,
    setPriorityRange,
    clearAllFilters,
    activeFilterCount,
    hasActiveFilters,
  }
}
