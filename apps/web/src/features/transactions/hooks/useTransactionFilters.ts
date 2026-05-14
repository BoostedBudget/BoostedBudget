import { useCallback, useMemo } from 'react'
import { useSearchParams } from 'react-router-dom'
import { getDateRange, inferPreset } from '@/shared/dates'
import type { SortState } from '@/shared/components/DataTable'
import { useSortUrlState, serializeSortParam, toggleSort } from '@/shared/hooks/useSortUrlState'
import { TRANSACTION_SORTABLE_COLUMNS } from '../components/TransactionTable'
import type { TransactionFilters, DatePreset, StatusFilter } from '../types'

const DEFAULT_SORT: SortState = { columnId: 'date', direction: 'desc' }

export interface UseTransactionFiltersResult {
  filters: TransactionFilters
  sort: SortState | null
  isDefaultSort: boolean
  setFilter: <K extends keyof TransactionFilters>(key: K, value: TransactionFilters[K]) => void
  setFilters: (updates: Partial<TransactionFilters>) => void
  clearFilter: (key: keyof TransactionFilters) => void
  clearAllFilters: (options?: { includeSort?: boolean }) => void
  setSort: (next: SortState | null) => void
  resetSort: () => void
  toggleSortColumn: (columnId: string) => void
  activeFilterCount: number
  hasActiveFilters: boolean
  datePreset: DatePreset
  setDatePreset: (preset: DatePreset) => void
  setCustomDateRange: (startDate: string, endDate: string) => void
  statusFilter: StatusFilter
  setStatusFilter: (status: StatusFilter) => void
}

const ARRAY_PARAMS = ['account_ids', 'pfc_primaries'] as const
const NUMERIC_PARAMS = ['min_amount', 'max_amount'] as const
const TRANSACTION_FILTER_KEYS = [
  ...ARRAY_PARAMS,
  ...NUMERIC_PARAMS,
  'start_date',
  'end_date',
  'search',
  'amount_type',
  'pending',
  'offset',
] as const

function valueToUrlParam(value: unknown): string | null {
  if (value === undefined || value === null || value === '') {
    return null
  }
  if (Array.isArray(value)) {
    if (value.length === 0) return null
    return value.join(',')
  }
  if (typeof value === 'boolean' || typeof value === 'number') {
    return String(value)
  }
  return value as string
}

export function useTransactionFilters(): UseTransactionFiltersResult {
  const [searchParams, setSearchParams] = useSearchParams()

  const { sort, isDefaultSort } = useSortUrlState({
    defaultSort: DEFAULT_SORT,
    validColumns: TRANSACTION_SORTABLE_COLUMNS,
  })

  const setSort = useCallback(
    (next: SortState | null) => {
      setSearchParams(
        (prev) => {
          const params = new URLSearchParams(prev)
          const serialized = serializeSortParam(next)
          if (serialized === null) params.delete('sort')
          else params.set('sort', serialized)
          params.delete('offset')
          return params
        },
        { replace: true },
      )
    },
    [setSearchParams],
  )

  const resetSort = useCallback(() => setSort(null), [setSort])

  const toggleSortColumn = useCallback(
    (columnId: string) => setSort(toggleSort(sort, columnId)),
    [sort, setSort],
  )

  const filters = useMemo<TransactionFilters>(() => {
    const result: TransactionFilters = {}

    // Parse array params (comma-separated)
    for (const key of ARRAY_PARAMS) {
      const value = searchParams.get(key)
      if (value) {
        const parsed = value.split(',').filter(Boolean).map((s) => s.trim())
        if (parsed.length > 0) {
          result[key] = parsed
        }
      }
    }

    // Parse numeric params
    for (const key of NUMERIC_PARAMS) {
      const value = searchParams.get(key)
      if (value) {
        const parsed = parseFloat(value)
        if (!isNaN(parsed)) {
          result[key] = parsed
        }
      }
    }

    // Parse string params
    const stringParams = ['start_date', 'end_date', 'search', 'amount_type'] as const
    for (const key of stringParams) {
      const value = searchParams.get(key)
      if (value) {
        (result as Record<string, string>)[key] = value
      }
    }

    // Parse boolean (pending)
    const pending = searchParams.get('pending')
    if (pending !== null) {
      result.pending = pending === 'true'
    }

    return result
  }, [searchParams])

  const setFilter = useCallback(
    <K extends keyof TransactionFilters>(key: K, value: TransactionFilters[K]) => {
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev)
          const urlValue = valueToUrlParam(value)

          if (urlValue === null) {
            next.delete(key)
          } else {
            next.set(key, urlValue)
          }

          next.delete('offset')

          return next
        },
        { replace: true }
      )
    },
    [setSearchParams]
  )

  const setFilters = useCallback(
    (updates: Partial<TransactionFilters>) => {
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev)

          for (const [key, value] of Object.entries(updates)) {
            const urlValue = valueToUrlParam(value)
            if (urlValue === null) {
              next.delete(key)
            } else {
              next.set(key, urlValue)
            }
          }

          next.delete('offset')

          return next
        },
        { replace: true }
      )
    },
    [setSearchParams]
  )

  const clearFilter = useCallback(
    (key: keyof TransactionFilters) => {
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev)
          next.delete(key)
          next.delete('offset')
          return next
        },
        { replace: true }
      )
    },
    [setSearchParams]
  )

  const clearAllFilters = useCallback(
    (options?: { includeSort?: boolean }) => {
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev)
          for (const key of TRANSACTION_FILTER_KEYS) {
            next.delete(key)
          }
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

    if (filters.account_ids && filters.account_ids.length > 0) count++
    if (filters.pfc_primaries && filters.pfc_primaries.length > 0) count++
    if (filters.start_date || filters.end_date) count++
    if (filters.search) count++
    if (filters.pending !== undefined) count++
    if (filters.min_amount !== undefined || filters.max_amount !== undefined) count++
    if (filters.amount_type && filters.amount_type !== 'all') count++

    return count
  }, [filters])

  const hasActiveFilters = activeFilterCount > 0

  const datePreset = useMemo<DatePreset>(
    () => inferPreset(filters.start_date, filters.end_date),
    [filters.start_date, filters.end_date]
  )

  const setDatePreset = useCallback(
    (preset: DatePreset) => {
      if (preset === 'custom') return

      const range = getDateRange(preset)
      if (range) {
        setFilters({
          start_date: range.startDate,
          end_date: range.endDate,
        })
      } else {
        setSearchParams(
          (prev) => {
            const next = new URLSearchParams(prev)
            next.delete('start_date')
            next.delete('end_date')
            next.delete('offset')
            return next
          },
          { replace: true }
        )
      }
    },
    [setFilters, setSearchParams]
  )

  const statusFilter = useMemo<StatusFilter>(() => {
    if (filters.pending === true) return 'pending'
    if (filters.pending === false) return 'posted'
    return 'all'
  }, [filters.pending])

  const setStatusFilter = useCallback(
    (status: StatusFilter) => {
      if (status === 'pending') {
        setFilter('pending', true)
      } else if (status === 'posted') {
        setFilter('pending', false)
      } else {
        clearFilter('pending')
      }
    },
    [setFilter, clearFilter]
  )

  const setCustomDateRange = useCallback(
    (startDate: string, endDate: string) => {
      setFilters({
        start_date: startDate,
        end_date: endDate,
      })
    },
    [setFilters]
  )

  return {
    filters,
    sort,
    isDefaultSort,
    setFilter,
    setFilters,
    clearFilter,
    clearAllFilters,
    setSort,
    resetSort,
    toggleSortColumn,
    activeFilterCount,
    hasActiveFilters,
    datePreset,
    setDatePreset,
    setCustomDateRange,
    statusFilter,
    setStatusFilter,
  }
}
