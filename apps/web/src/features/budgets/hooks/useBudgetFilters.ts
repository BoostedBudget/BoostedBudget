import { useCallback, useMemo } from 'react'
import type { RangeValue } from '@/shared/components/ColumnFilterDropdown'
import type { SortState } from '@/shared/components/DataTable'
import { useSortUrlState } from '@/shared/hooks/useSortUrlState'
import { useFilterState, type FilterSpec } from '@/shared/hooks/useFilterState'
import { BUDGET_SORTABLE_COLUMNS } from '../components/BudgetTable'

export interface BudgetFilters {
  period: string[]
  progressMin?: number
  progressMax?: number
}

export interface UseBudgetFiltersResult {
  filters: BudgetFilters
  periodFilter: string[]
  progressRange: RangeValue | null
  sort: SortState | null
  isDefaultSort: boolean
  setPeriodFilter: (value: string[]) => void
  setProgressRange: (value: RangeValue | null) => void
  setSort: (next: SortState | null) => void
  resetSort: () => void
  toggleSortColumn: (columnId: string) => void
  clearAllFilters: (options?: { includeSort?: boolean }) => void
  activeFilterCount: number
  hasActiveFilters: boolean
}

const SPEC = {
  period:   { kind: 'multi-select' },
  progress: { kind: 'range' },
} as const satisfies FilterSpec

export function useBudgetFilters(): UseBudgetFiltersResult {
  const state = useFilterState(SPEC)
  const { setFilter, clearAllFilters: genericClearAll, activeFilterCount, hasActiveFilters } = state

  const { sort, setSort, resetSort, toggleSortColumn, isDefaultSort } = useSortUrlState({
    defaultSort: null,
    validColumns: BUDGET_SORTABLE_COLUMNS,
  })

  const filters = useMemo<BudgetFilters>(
    () => ({
      period:      state.filters.period,
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

  const clearAllFilters = useCallback(
    (options?: { includeSort?: boolean }) => {
      genericClearAll()
      if (options?.includeSort) resetSort()
    },
    [genericClearAll, resetSort],
  )

  return {
    filters,
    periodFilter:  state.filters.period,
    progressRange,
    sort,
    isDefaultSort,
    setPeriodFilter,
    setProgressRange,
    setSort,
    resetSort,
    toggleSortColumn,
    clearAllFilters,
    activeFilterCount,
    hasActiveFilters,
  }
}
