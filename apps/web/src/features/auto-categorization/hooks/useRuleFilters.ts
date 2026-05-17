import { useCallback, useMemo } from 'react'
import type { RangeValue } from '@/shared/components/ColumnFilterDropdown'
import { useFilterState, type FilterSpec } from '@/shared/hooks/useFilterState'

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
  setStatusFilter: (value: string[]) => void
  setCategoryFilter: (value: string[]) => void
  setPriorityRange: (value: RangeValue | null) => void
  clearAllFilters: () => void
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
    (v: string[]) => state.setFilter('status', v),
    [state.setFilter],
  )

  const setCategoryFilter = useCallback(
    (v: string[]) => state.setFilter('category', v),
    [state.setFilter],
  )

  const setPriorityRange = useCallback(
    (value: RangeValue | null) => {
      state.setFilter('priority', value ?? undefined)
    },
    [state.setFilter],
  )

  return {
    filters,
    statusFilter: state.filters.status,
    categoryFilter: state.filters.category,
    priorityRange,
    setStatusFilter,
    setCategoryFilter,
    setPriorityRange,
    clearAllFilters: state.clearAllFilters,
    activeFilterCount: state.activeFilterCount,
    hasActiveFilters: state.hasActiveFilters,
  }
}
