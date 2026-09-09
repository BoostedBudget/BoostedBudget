import { useCallback, useMemo } from 'react'
import { useSearchParams } from 'react-router'
import { getDateRange, inferPreset } from '@/shared/dates'
import type { SortState } from '@/shared/components/DataTable'
import { useSortUrlState, serializeSortParam } from '@/shared/hooks/useSortUrlState'
import { useFilterState, type FilterSpec } from '@/shared/hooks/useFilterState'
import { TRANSACTION_SORTABLE_COLUMNS } from '../components/TransactionTable'
import type { TransactionFilters, DatePreset, StatusFilter, ActivityType } from '../types'

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
  setIncludeInvestmentActivity: (val: boolean) => void
  setActivityTypes: (types: ActivityType[]) => void
  /** Issue #1569: whether bank-reversed transactions are currently shown. Default false. */
  includeReversed: boolean
  setIncludeReversed: (val: boolean) => void
  /**
   * Issue #1626: whether uncategorized transactions are ALSO being matched. Default false.
   * OR-joined server-side with the three category filters, so this widens the result set — it
   * never means "hide the uncategorized ones".
   */
  uncategorized: boolean
  setUncategorized: (val: boolean) => void
}

const SPEC = {
  account_ids:        { kind: 'multi-select' },
  pfc_primaries:      { kind: 'multi-select' },
  pfc_detaileds:      { kind: 'multi-select' },
  custom_category_ids: { kind: 'multi-select' },
  activity_types:     { kind: 'multi-select' },
  min_amount:    { kind: 'scalar', codec: 'number' as const },
  max_amount:    { kind: 'scalar', codec: 'number' as const },
  start_date:    { kind: 'scalar', codec: 'string' as const },
  end_date:      { kind: 'scalar', codec: 'string' as const },
  search:        { kind: 'scalar', codec: 'string' as const },
  amount_type:   { kind: 'scalar', codec: 'string' as const, defaultValue: 'all', activeWhen: (v: unknown) => v !== undefined && v !== 'all' },
  pending:       { kind: 'scalar', codec: 'boolean' as const },
  reviewed:      { kind: 'scalar', codec: 'boolean' as const },
  merchant_id:   { kind: 'scalar', codec: 'string' as const },
  // Issue #1299: include_investment_activity defaults true — omit from URL when true to
  // avoid noise; serialize false only when explicitly disabled (canonicalization rule).
  include_investment_activity: {
    kind: 'scalar',
    codec: 'boolean' as const,
    activeWhen: (v: unknown) => v === false,
  },
  // Issue #1569 T12: reversals default OFF (mirroring the API's include_reversed default), so
  // the mirror image of the investment rule applies — `true` is the departure from default and
  // is the only value that belongs in the URL.
  include_reversed: {
    kind: 'scalar',
    codec: 'boolean' as const,
    activeWhen: (v: unknown) => v === true,
  },
  // Issue #1626: same shape as include_reversed — a positive-only opt-in whose default is off,
  // so `true` is the only value worth putting in the URL.
  uncategorized: {
    kind: 'scalar',
    codec: 'boolean' as const,
    activeWhen: (v: unknown) => v === true,
  },
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
      pfc_detaileds: state.filters.pfc_detaileds.length > 0 ? state.filters.pfc_detaileds : undefined,
      custom_category_ids: state.filters.custom_category_ids.length > 0 ? state.filters.custom_category_ids : undefined,
      start_date:    state.filters.start_date,
      end_date:      state.filters.end_date,
      search:        state.filters.search,
      pending:       state.filters.pending,
      reviewed:      state.filters.reviewed,
      min_amount:    state.filters.min_amount,
      max_amount:    state.filters.max_amount,
      amount_type:   state.filters.amount_type as TransactionFilters['amount_type'],
      merchant_id:   state.filters.merchant_id,
      // Issue #1299: include_investment_activity — undefined means default (true)
      include_investment_activity: state.filters.include_investment_activity,
      // Issue #1569: include_reversed — undefined means default (false). Normalised so an
      // explicit `false` in a hand-written URL produces the same filters object (and therefore
      // the same query cache key) as its absence.
      include_reversed: state.filters.include_reversed === true ? true : undefined,
      // Issue #1626: normalised the same way, for the same reason — an explicit `false` in a
      // hand-written URL must produce the identical filters object, and therefore the identical
      // query cache key, as its absence.
      uncategorized: state.filters.uncategorized === true ? true : undefined,
      // activity_types is meaningless when investment activity is disabled (include=false).
      // Suppress in derived state so deep-linked URLs (?include=false&activity_types=buy)
      // don't count stale types in activeFilterCount or show phantom filter pills (F2).
      activity_types:
        state.filters.include_investment_activity !== false &&
        state.filters.activity_types.length > 0
          ? state.filters.activity_types as ActivityType[]
          : undefined,
    }),
    [state.filters],
  )

  const activeFilterCount = useMemo(() => {
    const countIf = (condition: unknown) => condition ? 1 : 0
    return (
      countIf(filters.account_ids && filters.account_ids.length > 0) +
      countIf(state.filters.pfc_primaries.length > 0) +
      countIf(state.filters.pfc_detaileds.length > 0) +
      countIf(state.filters.custom_category_ids.length > 0) +
      countIf(filters.start_date || filters.end_date) +
      countIf(filters.search !== undefined) +
      countIf(filters.pending !== undefined) +
      countIf(filters.reviewed !== undefined) +
      countIf(filters.min_amount !== undefined || filters.max_amount !== undefined) +
      countIf(filters.amount_type && filters.amount_type !== 'all') +
      countIf(filters.merchant_id !== undefined) +
      countIf(filters.include_investment_activity === false) +
      countIf(filters.include_reversed === true) +
      countIf(filters.uncategorized === true) +
      countIf(filters.activity_types && filters.activity_types.length > 0)
    )
  }, [filters, state.filters.pfc_primaries, state.filters.pfc_detaileds, state.filters.custom_category_ids])

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

  // Issue #1299: Investment activity setters with canonicalization
  // Setting to true removes the URL param (default); false serializes explicitly.
  const setIncludeInvestmentActivity = useCallback(
    (val: boolean) => {
      if (val) {
        // Restore default: remove param + clear stale activity_types
        setFilters({ include_investment_activity: undefined, activity_types: [] })
      } else {
        // Explicitly disabled: serialize false, clear activity_types (canonicalization)
        setFilters({ include_investment_activity: false, activity_types: [] })
      }
    },
    [setFilters],
  )

  // Issue #1569 T12. Opting OUT clears the param rather than writing include_reversed=false:
  // false is the server default, so persisting it would make two identical requests look like
  // two different filter states (and split the React Query cache).
  const includeReversed = state.filters.include_reversed === true
  const setIncludeReversed = useCallback(
    (val: boolean) => {
      setFilter('include_reversed', val ? true : undefined)
    },
    [setFilter],
  )

  // Issue #1626. Mirrors setIncludeReversed exactly, and for a reason worth stating: the boolean
  // codec serialises `false` as the string "false" and only undefined/null delete a param, so
  // setFilter('uncategorized', false) would leave `?uncategorized=false` behind — a distinct URL
  // requesting identical rows, which splits the React Query cache and shows a stale pill.
  const uncategorized = state.filters.uncategorized === true
  const setUncategorized = useCallback(
    (val: boolean) => {
      setFilter('uncategorized', val ? true : undefined)
    },
    [setFilter],
  )

  // Setting activity types is a no-op when investment activity is disabled
  const setActivityTypes = useCallback(
    (types: ActivityType[]) => {
      if (filters.include_investment_activity === false) {
        // Canonicalization: ignore writes when investment activity is off
        return
      }
      setFilter('activity_types', types as string[])
    },
    [setFilter, filters.include_investment_activity],
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
    setIncludeInvestmentActivity,
    setActivityTypes,
    includeReversed,
    setIncludeReversed,
    uncategorized,
    setUncategorized,
  }
}
