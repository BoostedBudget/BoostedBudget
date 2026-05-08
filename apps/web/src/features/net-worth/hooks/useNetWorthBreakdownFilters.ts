import { useCallback, useMemo } from 'react'
import { useSearchParams } from 'react-router-dom'
import type { RangeValue } from '@/shared/components/ColumnFilterDropdown'

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
  sortColumn: SortColumn | null
  sortDirection: SortDirection
  setInstitutionFilter: (value: string[]) => void
  setTypeFilter: (value: string[]) => void
  setBalanceRange: (value: RangeValue | null) => void
  setSortColumn: (column: SortColumn) => void
  clearAllFilters: () => void
  activeFilterCount: number
  hasActiveFilters: boolean
}

const VALID_SORT_COLUMNS = new Set<string>(['institution', 'name', 'type', 'balance', 'percent'])

const parseCSV = (value: string | null): string[] =>
  value ? value.split(',').filter(Boolean).map((s) => s.trim()) : []

const parseNumber = (value: string | null): number | undefined => {
  if (!value) return undefined
  const num = parseFloat(value)
  return isNaN(num) ? undefined : num
}

export function useNetWorthBreakdownFilters(): UseNetWorthBreakdownFiltersResult {
  const [searchParams, setSearchParams] = useSearchParams()

  const filters = useMemo<NetWorthBreakdownFilters>(() => ({
    institution: parseCSV(searchParams.get('institution')),
    type: parseCSV(searchParams.get('type')),
    balanceMin: parseNumber(searchParams.get('balance_min')),
    balanceMax: parseNumber(searchParams.get('balance_max')),
  }), [searchParams])

  const { sortColumn, sortDirection } = useMemo<{
    sortColumn: SortColumn | null
    sortDirection: SortDirection
  }>(() => {
    const raw = searchParams.get('sort')
    if (!raw) return { sortColumn: null, sortDirection: 'asc' }
    const [col, dir] = raw.split(':')
    if (!col || !VALID_SORT_COLUMNS.has(col)) return { sortColumn: null, sortDirection: 'asc' }
    return {
      sortColumn: col as SortColumn,
      sortDirection: dir === 'desc' ? 'desc' : 'asc',
    }
  }, [searchParams])

  const institutionFilter = filters.institution
  const typeFilter = filters.type

  const balanceRange = useMemo<RangeValue | null>(() => {
    if (filters.balanceMin === undefined && filters.balanceMax === undefined) return null
    return { min: filters.balanceMin, max: filters.balanceMax }
  }, [filters.balanceMin, filters.balanceMax])

  const updateParam = useCallback(
    (key: string, value: string | null) => {
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev)
          if (value) { next.set(key, value) } else { next.delete(key) }
          return next
        },
        { replace: true }
      )
    },
    [setSearchParams]
  )

  const setInstitutionFilter = useCallback(
    (value: string[]) => updateParam('institution', value.length > 0 ? value.join(',') : null),
    [updateParam]
  )

  const setTypeFilter = useCallback(
    (value: string[]) => updateParam('type', value.length > 0 ? value.join(',') : null),
    [updateParam]
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
        { replace: true }
      )
    },
    [setSearchParams]
  )

  const setSortColumn = useCallback(
    (column: SortColumn) => {
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev)
          const currentCol = next.get('sort')?.split(':')[0]
          const currentDir = next.get('sort')?.split(':')[1] ?? 'asc'
          const newDir: SortDirection =
            currentCol === column && currentDir === 'asc' ? 'desc' : 'asc'
          next.set('sort', `${column}:${newDir}`)
          return next
        },
        { replace: true }
      )
    },
    [setSearchParams]
  )

  const clearAllFilters = useCallback(() => {
    setSearchParams(
      (prev) => {
        const next = new URLSearchParams(prev)
        next.delete('institution')
        next.delete('type')
        next.delete('balance_min')
        next.delete('balance_max')
        next.delete('sort')
        return next
      },
      { replace: true }
    )
  }, [setSearchParams])

  const activeFilterCount = useMemo(
    () =>
      (filters.institution.length > 0 ? 1 : 0) +
      (filters.type.length > 0 ? 1 : 0) +
      (filters.balanceMin !== undefined || filters.balanceMax !== undefined ? 1 : 0),
    [filters]
  )

  return {
    filters,
    institutionFilter,
    typeFilter,
    balanceRange,
    sortColumn,
    sortDirection,
    setInstitutionFilter,
    setTypeFilter,
    setBalanceRange,
    setSortColumn,
    clearAllFilters,
    activeFilterCount,
    hasActiveFilters: activeFilterCount > 0,
  }
}
