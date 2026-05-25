import { useCallback, useMemo } from 'react'
import { useSearchParams } from 'react-router-dom'
import { getDateRange, inferPreset } from '@/shared/dates'
import type { SortState } from '@/shared/components/DataTable'
import { useSortUrlState, serializeSortParam } from '@/shared/hooks/useSortUrlState'
import { useFilterState, type FilterSpec } from '@/shared/hooks/useFilterState'
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

const SPEC = {
  account_ids:   { kind: 'multi-select' },
  pfc_primaries: { kind: 'multi-select' },
  min_amount:    { kind: 'scalar', codec: 'number' as const },
  max_amount:    { kind: 'scalar', codec: 'number' as const },
  start_date:    { kind: 'scalar', codec: 'string' as const },
  end_date:      { kind: 'scalar', codec: 'string' as const },
  search:        { kind: 'scalar', codec: 'string' as const },
  amount_type:   { kind: 'scalar', codec: 'string' as const, defaultValue: 'all', activeWhen: (v: unknown) => v !== undefined && v !== 'all' },
  pending:       { kind: 'scalar', codec: 'boolean' as const },
  merchant_id:   { kind: 'scalar', codec: 'string' as const },
} as const satisfies FilterSpec

export function useTransactionFilters(): UseTransactionFiltersResult {
  const state = useFilterState(SPEC, { clearKeysOnWrite: ['offset'] })
  const { setFilter, setFilters, clearFilter, clearAllFilters: genericClearAll } = state

  const { sort, resetSort, toggleSortColumn, isDefaultSort } = useSortUrlState({
    defaultSort: DEFAULT_SORT,
    validColumns: TRANSACTION_SORTABLE_COLUMNS,
  })

  // Custom setSort that also clears offset (matches existing behavior)
  const [, setSearchParams] = useSearchParams()
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

  const clearAllFilters = useCallback(
    (options?: { includeSort?: boolean }) => {
      genericClearAll()
      if (options?.includeSort) resetSort()
    },
    [genericClearAll, resetSort],
  )

  const filters = useMemo<TransactionFilters>(
    () => ({
      account_ids:   state.filters.account_ids.length > 0 ? state.filters.account_ids : undefined,
      pfc_primaries: state.filters.pfc_primaries.length > 0 ? state.filters.pfc_primaries : undefined,
      start_date:    state.filters.start_date,
      end_date:      state.filters.end_date,
      search:        state.filters.search,
      pending:       state.filters.pending,
      min_amount:    state.filters.min_amount,
      max_amount:    state.filters.max_amount,
      amount_type:   state.filters.amount_type as TransactionFilters['amount_type'],
      merchant_id:   state.filters.merchant_id,
    }),
    [state.filters],
  )

  const activeFilterCount = useMemo(() => {
    let count = 0
    if (filters.account_ids && filters.account_ids.length > 0) count++
    if (state.filters.pfc_primaries.length > 0) count++
    if (filters.start_date || filters.end_date) count++
    if (filters.search) count++
    if (filters.pending !== undefined) count++
    if (filters.min_amount !== undefined || filters.max_amount !== undefined) count++
    if (filters.amount_type && filters.amount_type !== 'all') count++
    if (filters.merchant_id) count++
    return count
  }, [filters, state.filters.pfc_primaries])

  const datePreset = useMemo<DatePreset>(
    () => inferPreset(state.filters.start_date, state.filters.end_date),
    [state.filters.start_date, state.filters.end_date],
  )

  const statusFilter = useMemo<StatusFilter>(() => {
    if (state.filters.pending === true) return 'pending'
    if (state.filters.pending === false) return 'posted'
    return 'all'
  }, [state.filters.pending])

  const setDatePreset = useCallback(
    (preset: DatePreset) => {
      if (preset === 'custom') return
      const range = getDateRange(preset)
      if (range) {
        setFilters({ start_date: range.startDate, end_date: range.endDate })
      } else {
        setFilters({ start_date: undefined, end_date: undefined })
      }
    },
    [setFilters],
  )

  const setCustomDateRange = useCallback(
    (startDate: string, endDate: string) => {
      setFilters({ start_date: startDate, end_date: endDate })
    },
    [setFilters],
  )

  const setStatusFilter = useCallback(
    (status: StatusFilter) => {
      if (status === 'pending')     setFilter('pending', true)
      else if (status === 'posted') setFilter('pending', false)
      else                          clearFilter('pending')
    },
    [setFilter, clearFilter],
  )

  return {
    filters,
    sort,
    isDefaultSort,
    setFilter:         setFilter as UseTransactionFiltersResult['setFilter'],
    setFilters:        setFilters as UseTransactionFiltersResult['setFilters'],
    clearFilter:       clearFilter as UseTransactionFiltersResult['clearFilter'],
    clearAllFilters,
    setSort,
    resetSort,
    toggleSortColumn,
    activeFilterCount,
    hasActiveFilters:  activeFilterCount > 0,
    datePreset,
    setDatePreset,
    setCustomDateRange,
    statusFilter,
    setStatusFilter,
  }
}
