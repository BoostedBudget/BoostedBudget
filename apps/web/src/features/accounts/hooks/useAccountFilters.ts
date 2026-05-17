/**
 * useAccountFilters Hook
 * Issue #590: Inline Column Filtering
 *
 * Manages account filter state with URL synchronization.
 * Thin wrapper around the generic useFilterState hook.
 */

import { useCallback, useMemo } from 'react'
import type { RangeValue } from '@/shared/components/ColumnFilterDropdown'
import type { SortState } from '@/shared/components/DataTable'
import { useSortUrlState } from '@/shared/hooks/useSortUrlState'
import { useFilterState, type FilterSpec } from '@/shared/hooks/useFilterState'
import { ACCOUNT_SORTABLE_COLUMNS } from '../components/AccountTable'

/**
 * Account filter state
 */
export interface AccountFilters {
  /** Institution names to filter by */
  institution: string[]
  /** Account types to filter by */
  type: string[]
  /** Minimum balance */
  balanceMin?: number
  /** Maximum balance */
  balanceMax?: number
}

/**
 * Result of useAccountFilters hook
 */
export interface UseAccountFiltersResult {
  /** Current filter state derived from URL */
  filters: AccountFilters
  /** Institution filter array */
  institutionFilter: string[]
  /** Type filter array */
  typeFilter: string[]
  /** Balance range filter */
  balanceRange: RangeValue | null
  /** Current sort state */
  sort: SortState | null
  /** Whether sort is at its default value */
  isDefaultSort: boolean
  /** Set institution filter */
  setInstitutionFilter: (value: string[]) => void
  /** Set type filter */
  setTypeFilter: (value: string[]) => void
  /** Set balance range filter */
  setBalanceRange: (value: RangeValue | null) => void
  /** Set sort state */
  setSort: (next: SortState | null) => void
  /** Reset sort to default */
  resetSort: () => void
  /** Toggle sort column */
  toggleSortColumn: (columnId: string) => void
  /** Clear all filters (and optionally sort) */
  clearAllFilters: (options?: { includeSort?: boolean }) => void
  /** Number of active filters */
  activeFilterCount: number
  /** Whether any filters are active */
  hasActiveFilters: boolean
}

const SPEC = {
  institution: { kind: 'multi-select' },
  type:        { kind: 'multi-select' },
  balance:     { kind: 'range' },
} as const satisfies FilterSpec

/**
 * Hook for managing account filters with URL state synchronization
 *
 * @returns Filter state and management functions
 */
export function useAccountFilters(): UseAccountFiltersResult {
  const state = useFilterState(SPEC)
  const { setFilter, clearAllFilters: genericClearAll } = state

  const { sort, setSort, resetSort, toggleSortColumn, isDefaultSort } = useSortUrlState({
    defaultSort: null,
    validColumns: ACCOUNT_SORTABLE_COLUMNS,
  })

  const filters = useMemo<AccountFilters>(
    () => ({
      institution: state.filters.institution,
      type: state.filters.type,
      balanceMin: state.filters.balance?.min,
      balanceMax: state.filters.balance?.max,
    }),
    [state.filters],
  )

  const balanceRange = useMemo<RangeValue | null>(
    () => state.filters.balance ?? null,
    [state.filters.balance],
  )

  const setInstitutionFilter = useCallback(
    (v: string[]) => setFilter('institution', v),
    [setFilter],
  )

  const setTypeFilter = useCallback(
    (v: string[]) => setFilter('type', v),
    [setFilter],
  )

  const setBalanceRange = useCallback(
    (value: RangeValue | null) => {
      setFilter('balance', value ?? undefined)
    },
    [setFilter],
  )

  const clearAllFilters = useCallback(
    (options?: { includeSort?: boolean }) => {
      genericClearAll()
      if (options?.includeSort) resetSort()
    },
    [genericClearAll, resetSort],
  )

  return {
    filters,
    institutionFilter: state.filters.institution,
    typeFilter: state.filters.type,
    balanceRange,
    sort,
    isDefaultSort,
    setInstitutionFilter,
    setTypeFilter,
    setBalanceRange,
    setSort,
    resetSort,
    toggleSortColumn,
    clearAllFilters,
    activeFilterCount: state.activeFilterCount,
    hasActiveFilters: state.hasActiveFilters,
  }
}
