import { useCallback, useMemo } from 'react'
import { useSearchParams } from 'react-router-dom'
import type { RangeValue } from '@/shared/components/ColumnFilterDropdown'
import type { SortState } from '@/shared/components/DataTable'
import { useSortUrlState } from '@/shared/hooks/useSortUrlState'
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

export function useAccountFilters(): UseAccountFiltersResult {
  const [searchParams, setSearchParams] = useSearchParams()

  const { sort, setSort, resetSort, toggleSortColumn, isDefaultSort } = useSortUrlState({
    defaultSort: null,
    validColumns: ACCOUNT_SORTABLE_COLUMNS,
  })

  const filters = useMemo<AccountFilters>(() => {
    const result: AccountFilters = { institution: [], type: [] }

    const institution = searchParams.get('institution')
    if (institution) {
      result.institution = institution.split(',').filter(Boolean).map((s) => s.trim())
    }

    const type = searchParams.get('type')
    if (type) {
      result.type = type.split(',').filter(Boolean).map((s) => s.trim())
    }

    const balanceMin = searchParams.get('balance_min')
    if (balanceMin) {
      const parsed = parseFloat(balanceMin)
      if (!isNaN(parsed)) result.balanceMin = parsed
    }

    const balanceMax = searchParams.get('balance_max')
    if (balanceMax) {
      const parsed = parseFloat(balanceMax)
      if (!isNaN(parsed)) result.balanceMax = parsed
    }

    return result
  }, [searchParams])

  const institutionFilter = filters.institution
  const typeFilter = filters.type
  const balanceRange: RangeValue | null = useMemo(() => {
    if (filters.balanceMin === undefined && filters.balanceMax === undefined) return null
    return { min: filters.balanceMin, max: filters.balanceMax }
  }, [filters.balanceMin, filters.balanceMax])

  const setInstitutionFilter = useCallback(
    (value: string[]) => {
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev)
          if (value.length === 0) next.delete('institution')
          else next.set('institution', value.join(','))
          return next
        },
        { replace: true },
      )
    },
    [setSearchParams],
  )

  const setTypeFilter = useCallback(
    (value: string[]) => {
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev)
          if (value.length === 0) next.delete('type')
          else next.set('type', value.join(','))
          return next
        },
        { replace: true },
      )
    },
    [setSearchParams],
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

  const activeFilterCount = useMemo(() => {
    let count = 0
    if (filters.institution.length > 0) count++
    if (filters.type.length > 0) count++
    if (filters.balanceMin !== undefined || filters.balanceMax !== undefined) count++
    return count
  }, [filters])

  return {
    filters,
    institutionFilter,
    typeFilter,
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
    activeFilterCount,
    hasActiveFilters: activeFilterCount > 0,
  }
}
