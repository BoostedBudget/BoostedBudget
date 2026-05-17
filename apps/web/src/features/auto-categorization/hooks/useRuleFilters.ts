import { useCallback, useMemo } from 'react'
import type { RangeValue } from '@/shared/components/ColumnFilterDropdown'
import type { SortState } from '@/shared/components/DataTable'
import { useSortUrlState } from '@/shared/hooks/useSortUrlState'
import { useFilterState, type FilterSpec } from '@/shared/hooks/useFilterState'
import { RULE_SORTABLE_COLUMNS } from '../components/AutoCategorizationRuleTable'

export interface RuleFilters {
  status: string[]
  category: string[]
  priorityMin?: number
  priorityMax?: number
}

export interface UseRuleFiltersResult {
  filters: RuleFilters
  statusFilter: string[]
  categoryFilter: string[]
  priorityRange: RangeValue | null
  sort: SortState | null
  isDefaultSort: boolean
  setStatusFilter: (value: string[]) => void
  setCategoryFilter: (value: string[]) => void
  setPriorityRange: (value: RangeValue | null) => void
  setSort: (next: SortState | null) => void
  resetSort: () => void
  toggleSortColumn: (columnId: string) => void
  clearAllFilters: (options?: { includeSort?: boolean }) => void
  activeFilterCount: number
  hasActiveFilters: boolean
}

const SPEC = {
  status:   { kind: 'multi-select' },
  category: { kind: 'multi-select' },
  priority: { kind: 'range', codec: 'integer' as const },
} as const satisfies FilterSpec

export function useRuleFilters(): UseRuleFiltersResult {
  const state = useFilterState(SPEC)
  const { setFilter, clearAllFilters: genericClearAll, activeFilterCount, hasActiveFilters } = state

  const { sort, setSort, resetSort, toggleSortColumn, isDefaultSort } = useSortUrlState({
    defaultSort: null,
    validColumns: RULE_SORTABLE_COLUMNS,
  })

  const filters = useMemo<RuleFilters>(
    () => ({
      status:      state.filters.status,
      category:    state.filters.category,
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

  const clearAllFilters = useCallback(
    (options?: { includeSort?: boolean }) => {
      genericClearAll()
      if (options?.includeSort) resetSort()
    },
    [genericClearAll, resetSort],
  )

  return {
    filters,
    statusFilter:    state.filters.status,
    categoryFilter:  state.filters.category,
    priorityRange,
    sort,
    isDefaultSort,
    setStatusFilter,
    setCategoryFilter,
    setPriorityRange,
    setSort,
    resetSort,
    toggleSortColumn,
    clearAllFilters,
    activeFilterCount,
    hasActiveFilters,
  }
}
