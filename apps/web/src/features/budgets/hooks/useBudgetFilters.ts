import { useCallback, useMemo } from 'react'
import { useSearchParams } from 'react-router-dom'
import type { RangeValue } from '@/shared/components/ColumnFilterDropdown'
import type { SortState } from '@/shared/components/DataTable'
import { useSortUrlState } from '@/shared/hooks/useSortUrlState'
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

export function useBudgetFilters(): UseBudgetFiltersResult {
  const [searchParams, setSearchParams] = useSearchParams()

  const { sort, setSort, resetSort, toggleSortColumn, isDefaultSort } = useSortUrlState({
    defaultSort: null,
    validColumns: BUDGET_SORTABLE_COLUMNS,
  })

  const filters = useMemo<BudgetFilters>(() => {
    const result: BudgetFilters = { period: [] }

    const period = searchParams.get('period')
    if (period) {
      result.period = period.split(',').filter(Boolean).map((s) => s.trim())
    }

    const progressMin = searchParams.get('progress_min')
    if (progressMin) {
      const parsed = parseFloat(progressMin)
      if (!isNaN(parsed)) result.progressMin = parsed
    }

    const progressMax = searchParams.get('progress_max')
    if (progressMax) {
      const parsed = parseFloat(progressMax)
      if (!isNaN(parsed)) result.progressMax = parsed
    }

    return result
  }, [searchParams])

  const periodFilter = filters.period
  const progressRange: RangeValue | null = useMemo(() => {
    if (filters.progressMin === undefined && filters.progressMax === undefined) return null
    return { min: filters.progressMin, max: filters.progressMax }
  }, [filters.progressMin, filters.progressMax])

  const setPeriodFilter = useCallback(
    (value: string[]) => {
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev)
          if (value.length === 0) next.delete('period')
          else next.set('period', value.join(','))
          return next
        },
        { replace: true },
      )
    },
    [setSearchParams],
  )

  const setProgressRange = useCallback(
    (value: RangeValue | null) => {
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev)
          if (!value || (value.min === undefined && value.max === undefined)) {
            next.delete('progress_min')
            next.delete('progress_max')
          } else {
            if (value.min !== undefined) next.set('progress_min', String(value.min))
            else next.delete('progress_min')
            if (value.max !== undefined) next.set('progress_max', String(value.max))
            else next.delete('progress_max')
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
          next.delete('period')
          next.delete('progress_min')
          next.delete('progress_max')
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
    if (filters.period.length > 0) count++
    if (filters.progressMin !== undefined || filters.progressMax !== undefined) count++
    return count
  }, [filters])

  return {
    filters,
    periodFilter,
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
    hasActiveFilters: activeFilterCount > 0,
  }
}
