import { useCallback, useMemo } from 'react'
import { useSearchParams } from 'react-router-dom'
import type { RangeValue } from '@/shared/components/ColumnFilterDropdown'
import type { SortState } from '@/shared/components/DataTable'
import { useSortUrlState } from '@/shared/hooks/useSortUrlState'
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

const parseCSV = (value: string | null): string[] =>
  value ? value.split(',').filter(Boolean).map((s) => s.trim()) : []

const parseNumber = (value: string | null): number | undefined => {
  if (!value) return undefined
  const num = parseFloat(value)
  return isNaN(num) ? undefined : num
}

export function useNetWorthBreakdownFilters(): UseNetWorthBreakdownFiltersResult {
  const [searchParams, setSearchParams] = useSearchParams()

  const { sort, setSort, resetSort, toggleSortColumn } = useSortUrlState({
    defaultSort: null,
    validColumns: NET_WORTH_SORTABLE_COLUMNS,
  })

  const filters = useMemo<NetWorthBreakdownFilters>(() => ({
    institution: parseCSV(searchParams.get('institution')),
    type: parseCSV(searchParams.get('type')),
    balanceMin: parseNumber(searchParams.get('balance_min')),
    balanceMax: parseNumber(searchParams.get('balance_max')),
  }), [searchParams])

  const institutionFilter = filters.institution
  const typeFilter = filters.type

  const balanceRange: RangeValue | null = useMemo(() => {
    if (filters.balanceMin === undefined && filters.balanceMax === undefined) return null
    return { min: filters.balanceMin, max: filters.balanceMax }
  }, [filters.balanceMin, filters.balanceMax])

  const sortColumn = (sort?.columnId ?? null) as SortColumn | null
  const sortDirection: SortDirection = sort?.direction ?? 'asc'

  const setSortColumn = useCallback(
    (column: SortColumn) => toggleSortColumn(column),
    [toggleSortColumn],
  )

  const updateParam = useCallback(
    (key: string, value: string | null) => {
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev)
          if (value) { next.set(key, value) } else { next.delete(key) }
          return next
        },
        { replace: true },
      )
    },
    [setSearchParams],
  )

  const setInstitutionFilter = useCallback(
    (value: string[]) => updateParam('institution', value.length > 0 ? value.join(',') : null),
    [updateParam],
  )

  const setTypeFilter = useCallback(
    (value: string[]) => updateParam('type', value.length > 0 ? value.join(',') : null),
    [updateParam],
  )

  const setBalanceRange = useCallback(
    (value: RangeValue | null) => {
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev)
          if (!value || (value.min === undefined && value.max === undefined)) {
            next.delete('balance_min')
            next.delete('balance_max')
          } else {
            if (value.min !== undefined) next.set('balance_min', String(value.min))
            else next.delete('balance_min')
            if (value.max !== undefined) next.set('balance_max', String(value.max))
            else next.delete('balance_max')
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

  const activeFilterCount = useMemo(
    () =>
      (filters.institution.length > 0 ? 1 : 0) +
      (filters.type.length > 0 ? 1 : 0) +
      (filters.balanceMin !== undefined || filters.balanceMax !== undefined ? 1 : 0),
    [filters],
  )

  return {
    filters,
    institutionFilter,
    typeFilter,
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
    activeFilterCount,
    hasActiveFilters: activeFilterCount > 0,
  }
}
