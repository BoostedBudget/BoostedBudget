import { useCallback, useMemo } from 'react'
import { useSearchParams } from 'react-router-dom'
import type { RangeValue } from '@/shared/components/ColumnFilterDropdown'
import type { SortState } from '@/shared/components/DataTable'
import { useSortUrlState } from '@/shared/hooks/useSortUrlState'
import { useFilterState, type FilterSpec } from '@/shared/hooks/useFilterState'
import { NET_WORTH_SORTABLE_COLUMNS } from '../components/AccountBreakdownTable'

export type SortColumn = 'institution' | 'name' | 'type' | 'balance' | 'percent'
export type SortDirection = 'asc' | 'desc'

export interface NetWorthBreakdownFilters {
  institution: string[]
  type: string[]
  balanceMin?: number
  balanceMax?: number
}

export interface UseNetWorthBreakdownFiltersResult {
  filters: NetWorthBreakdownFilters
  institutionFilter: string[]
  typeFilter: string[]
  balanceRange: RangeValue | null
  sort: SortState | null
  sortColumn: SortColumn | null
  sortDirection: SortDirection
  setInstitutionFilter: (value: string[]) => void
  setTypeFilter: (value: string[]) => void
  setBalanceRange: (value: RangeValue | null) => void
  setSort: (next: SortState | null) => void
  resetSort: () => void
  setSortColumn: (column: SortColumn) => void
  clearAllFilters: (options?: { includeSort?: boolean }) => void
  activeFilterCount: number
  hasActiveFilters: boolean
}

const SPEC = {
  institution: { kind: 'multi-select' },
  type:        { kind: 'multi-select' },
  balance:     { kind: 'range' },
} as const satisfies FilterSpec

export function useNetWorthBreakdownFilters(): UseNetWorthBreakdownFiltersResult {
  const state = useFilterState(SPEC)
  const { setFilter } = state

  // Needed for atomic clearAllFilters (single setSearchParams call avoids React Router
  // batching issue where two sequential calls each read the pre-update URL).
  const [, setSearchParams] = useSearchParams()

  const { sort, setSort, resetSort, toggleSortColumn } = useSortUrlState({
    defaultSort: null,
    validColumns: NET_WORTH_SORTABLE_COLUMNS,
  })

  const filters = useMemo<NetWorthBreakdownFilters>(
    () => ({
      institution: state.filters.institution,
      type:        state.filters.type,
      balanceMin:  state.filters.balance?.min,
      balanceMax:  state.filters.balance?.max,
    }),
    [state.filters],
  )

  const balanceRange = useMemo<RangeValue | null>(
    () => state.filters.balance ?? null,
    [state.filters.balance],
  )

  const sortColumn = (sort?.columnId ?? null) as SortColumn | null
  const sortDirection: SortDirection = sort?.direction ?? 'asc'

  const setInstitutionFilter = useCallback(
    (value: string[]) => setFilter('institution', value),
    [setFilter],
  )

  const setTypeFilter = useCallback(
    (value: string[]) => setFilter('type', value),
    [setFilter],
  )

  const setBalanceRange = useCallback(
    (value: RangeValue | null) => setFilter('balance', value ?? undefined),
    [setFilter],
  )

  const setSortColumn = useCallback(
    (column: SortColumn) => toggleSortColumn(column),
    [toggleSortColumn],
  )

  const clearAllFilters = useCallback(
    (options?: { includeSort?: boolean }) => {
      // Single atomic setSearchParams call — avoids React Router functional-update
      // batching quirk where two calls both read the pre-update URL.
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev)
          next.delete('institution')
          next.delete('type')
          next.delete('balance_min')
          next.delete('balance_max')
          if (options?.includeSort) next.delete('sort')
          return next
        },
        { replace: true },
      )
    },
    [setSearchParams],
  )

  return {
    filters,
    institutionFilter: state.filters.institution,
    typeFilter:        state.filters.type,
    balanceRange,
    sort,
    sortColumn,
    sortDirection,
    setInstitutionFilter,
    setTypeFilter,
    setBalanceRange,
    setSort,
    resetSort,
    setSortColumn,
    clearAllFilters,
    activeFilterCount:  state.activeFilterCount,
    hasActiveFilters:   state.hasActiveFilters,
  }
}
