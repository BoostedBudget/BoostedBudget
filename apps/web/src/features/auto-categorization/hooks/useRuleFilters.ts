import { useCallback, useMemo } from 'react'
import { useSearchParams } from 'react-router-dom'
import type { RangeValue } from '@/shared/components/ColumnFilterDropdown'
import type { SortState } from '@/shared/components/DataTable'
import { useSortUrlState } from '@/shared/hooks/useSortUrlState'
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

export function useRuleFilters(): UseRuleFiltersResult {
  const [searchParams, setSearchParams] = useSearchParams()

  const { sort, setSort, resetSort, toggleSortColumn, isDefaultSort } = useSortUrlState({
    defaultSort: null,
    validColumns: RULE_SORTABLE_COLUMNS,
  })

  const filters = useMemo<RuleFilters>(() => {
    const result: RuleFilters = { status: [], category: [] }

    const status = searchParams.get('status')
    if (status) {
      result.status = status.split(',').filter(Boolean).map((s) => s.trim())
    }

    const category = searchParams.get('category')
    if (category) {
      result.category = category.split(',').filter(Boolean).map((s) => s.trim())
    }

    const priorityMin = searchParams.get('priority_min')
    if (priorityMin) {
      const parsed = parseInt(priorityMin, 10)
      if (!isNaN(parsed)) result.priorityMin = parsed
    }

    const priorityMax = searchParams.get('priority_max')
    if (priorityMax) {
      const parsed = parseInt(priorityMax, 10)
      if (!isNaN(parsed)) result.priorityMax = parsed
    }

    return result
  }, [searchParams])

  const statusFilter = filters.status
  const categoryFilter = filters.category
  const priorityRange: RangeValue | null = useMemo(() => {
    if (filters.priorityMin === undefined && filters.priorityMax === undefined) return null
    return { min: filters.priorityMin, max: filters.priorityMax }
  }, [filters.priorityMin, filters.priorityMax])

  const setStatusFilter = useCallback(
    (value: string[]) => {
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev)
          if (value.length === 0) next.delete('status')
          else next.set('status', value.join(','))
          return next
        },
        { replace: true },
      )
    },
    [setSearchParams],
  )

  const setCategoryFilter = useCallback(
    (value: string[]) => {
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev)
          if (value.length === 0) next.delete('category')
          else next.set('category', value.join(','))
          return next
        },
        { replace: true },
      )
    },
    [setSearchParams],
  )

  const setPriorityRange = useCallback(
    (value: RangeValue | null) => {
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev)
          if (!value || (value.min === undefined && value.max === undefined)) {
            next.delete('priority_min')
            next.delete('priority_max')
          } else {
            if (value.min !== undefined) next.set('priority_min', String(value.min))
            else next.delete('priority_min')
            if (value.max !== undefined) next.set('priority_max', String(value.max))
            else next.delete('priority_max')
          }
          return next
        },
        { replace: true },
      )
    },
    [setSearchParams],
  )

  const clearAllFilters = useCallback(
    (options?: { includeSort?: boolean }) => {
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev)
          next.delete('status')
          next.delete('category')
          next.delete('priority_min')
          next.delete('priority_max')
          if (options?.includeSort) next.delete('sort')
          return next
        },
        { replace: true },
      )
    },
    [setSearchParams],
  )

  const activeFilterCount = useMemo(() => {
    let count = 0
    if (filters.status.length > 0) count++
    if (filters.category.length > 0) count++
    if (filters.priorityMin !== undefined || filters.priorityMax !== undefined) count++
    return count
  }, [filters])

  return {
    filters,
    statusFilter,
    categoryFilter,
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
    hasActiveFilters: activeFilterCount > 0,
  }
}
