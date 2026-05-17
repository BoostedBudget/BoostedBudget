import { useCallback, useMemo } from 'react'
import type { RangeValue } from '@/shared/components/ColumnFilterDropdown'
import type { SortState } from '@/shared/components/DataTable'
import { useSortUrlState } from '@/shared/hooks/useSortUrlState'
import { useFilterState, type FilterSpec } from '@/shared/hooks/useFilterState'
import { ACCOUNT_SORTABLE_COLUMNS } from '../components/AccountTable'

export interface AccountFilters {
  institution: string[]
  type: string[]
  balanceMin?: number
  balanceMax?: number
}

export interface UseAccountFiltersResult {
  filters: AccountFilters
  institutionFilter: string[]
  typeFilter: string[]
  balanceRange: RangeValue | null
  sort: SortState | null
  isDefaultSort: boolean
  setInstitutionFilter: (value: string[]) => void
  setTypeFilter: (value: string[]) => void
  setBalanceRange: (value: RangeValue | null) => void
  setSort: (next: SortState | null) => void
  resetSort: () => void
  toggleSortColumn: (columnId: string) => void
  clearAllFilters: (options?: { includeSort?: boolean }) => void
  activeFilterCount: number
  hasActiveFilters: boolean
}

const SPEC = {
  institution: { kind: 'multi-select' },
  type:        { kind: 'multi-select' },
  balance:     { kind: 'range' },
} as const satisfies FilterSpec

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

  const setInstitutionFilter = useCallback((v: string[]) => setFilter('institution', v), [setFilter])
  const setTypeFilter = useCallback((v: string[]) => setFilter('type', v), [setFilter])
  const setBalanceRange = useCallback(
    (value: RangeValue | null) => setFilter('balance', value ?? undefined),
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
    typeFilter:        state.filters.type,
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
    hasActiveFilters:  state.hasActiveFilters,
  }
}
