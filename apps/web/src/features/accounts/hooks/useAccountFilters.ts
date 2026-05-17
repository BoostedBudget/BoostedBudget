/**
 * useAccountFilters Hook
 * Issue #590: Inline Column Filtering
 *
 * Manages account filter state with URL synchronization.
 * Thin wrapper around the generic useFilterState hook.
 */

import { useCallback, useMemo } from 'react'
import type { RangeValue } from '@/shared/components/ColumnFilterDropdown'
import { useFilterState, type FilterSpec } from '@/shared/hooks/useFilterState'

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
  /** Set institution filter */
  setInstitutionFilter: (value: string[]) => void
  /** Set type filter */
  setTypeFilter: (value: string[]) => void
  /** Set balance range filter */
  setBalanceRange: (value: RangeValue | null) => void
  /** Clear all filters */
  clearAllFilters: () => void
  /** Number of active filters */
  activeFilterCount: number
  /** Whether any filters are active */
  hasActiveFilters: boolean
}

const SPEC = {
  institution: { kind: 'multi-select' },
  type: { kind: 'multi-select' },
  balance: { kind: 'range' },
} as const satisfies FilterSpec

/**
 * Hook for managing account filters with URL state synchronization
 *
 * @returns Filter state and management functions
 */
export function useAccountFilters(): UseAccountFiltersResult {
  const state = useFilterState(SPEC)

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
    (v: string[]) => state.setFilter('institution', v),
    [state.setFilter],
  )

  const setTypeFilter = useCallback(
    (v: string[]) => state.setFilter('type', v),
    [state.setFilter],
  )

  const setBalanceRange = useCallback(
    (value: RangeValue | null) => {
      state.setFilter('balance', value ?? undefined)
    },
    [state.setFilter],
  )

  return {
    filters,
    institutionFilter: state.filters.institution,
    typeFilter: state.filters.type,
    balanceRange,
    setInstitutionFilter,
    setTypeFilter,
    setBalanceRange,
    clearAllFilters: state.clearAllFilters,
    activeFilterCount: state.activeFilterCount,
    hasActiveFilters: state.hasActiveFilters,
  }
}
