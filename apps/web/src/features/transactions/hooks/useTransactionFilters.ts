import { useCallback, useMemo } from 'react'
import { format, subDays, subYears, startOfDay } from 'date-fns'
import { useFilterState, type FilterSpec } from '@/shared/hooks/useFilterState'
import type { TransactionFilters, DatePreset, StatusFilter } from '../types'

/**
 * Result of useTransactionFilters hook
 * Provides filter state, setters, and computed values
 */
export interface UseTransactionFiltersResult {
  /** Current filter state derived from URL */
  filters: TransactionFilters
  /** Set a single filter value */
  setFilter: <K extends keyof TransactionFilters>(key: K, value: TransactionFilters[K]) => void
  /** Set multiple filters at once */
  setFilters: (updates: Partial<TransactionFilters>) => void
  /** Clear a specific filter */
  clearFilter: (key: keyof TransactionFilters) => void
  /** Clear all filters */
  clearAllFilters: () => void
  /** Number of active filters (non-default values) */
  activeFilterCount: number
  /** Whether any filters are active */
  hasActiveFilters: boolean
  /** Current date preset */
  datePreset: DatePreset
  /** Set date preset (also updates start_date and end_date) */
  setDatePreset: (preset: DatePreset) => void
  /** Set custom date range */
  setCustomDateRange: (startDate: string, endDate: string) => void
  /** Current status filter */
  statusFilter: StatusFilter
  /** Set status filter (updates pending) */
  setStatusFilter: (status: StatusFilter) => void
}

// ---------------------------------------------------------------------------
// Filter spec for useFilterState
// ---------------------------------------------------------------------------

// Deprecated TransactionFilters fields `account_id` (singular) and `category` (singular)
// are intentionally absent — no consumer passes them through this hook.
const SPEC = {
  account_ids:   { kind: 'multi-select' },
  pfc_primaries: { kind: 'multi-select' },
  categories:    { kind: 'multi-select' },
  min_amount:    { kind: 'scalar', codec: 'number' as const },
  max_amount:    { kind: 'scalar', codec: 'number' as const },
  start_date:    { kind: 'scalar', codec: 'string' as const },
  end_date:      { kind: 'scalar', codec: 'string' as const },
  search:        { kind: 'scalar', codec: 'string' as const },
  amount_type:   { kind: 'scalar', codec: 'string' as const },
  pending:       { kind: 'scalar', codec: 'boolean' as const },
} as const satisfies FilterSpec

// ---------------------------------------------------------------------------
// Date helpers (inline — no external dates module dependency)
// ---------------------------------------------------------------------------

function getDateRange(preset: DatePreset): { startDate: string; endDate: string } | null {
  const today = startOfDay(new Date())
  const todayStr = format(today, 'yyyy-MM-dd')

  switch (preset) {
    case '7d':
      return { startDate: format(subDays(today, 7), 'yyyy-MM-dd'), endDate: todayStr }
    case '30d':
      return { startDate: format(subDays(today, 30), 'yyyy-MM-dd'), endDate: todayStr }
    case '90d':
      return { startDate: format(subDays(today, 90), 'yyyy-MM-dd'), endDate: todayStr }
    case '1y':
      return { startDate: format(subYears(today, 1), 'yyyy-MM-dd'), endDate: todayStr }
    default:
      return null // 'all' and 'custom' have no fixed range
  }
}

function inferPreset(startDate?: string, endDate?: string): DatePreset {
  if (!startDate && !endDate) return 'all'

  const today = format(startOfDay(new Date()), 'yyyy-MM-dd')

  if (endDate && endDate !== today) return 'custom'
  if (!startDate) return 'all'

  const todayDate = startOfDay(new Date())
  const start = new Date(startDate)
  const diffDays = Math.floor((todayDate.getTime() - start.getTime()) / (1000 * 60 * 60 * 24))

  if (diffDays === 7) return '7d'
  if (diffDays === 30) return '30d'
  if (diffDays === 90) return '90d'
  if (diffDays >= 364 && diffDays <= 366) return '1y'

  return 'custom'
}

// ---------------------------------------------------------------------------
// Main hook
// ---------------------------------------------------------------------------

/**
 * Hook for managing transaction filters with URL state synchronization.
 *
 * Features:
 * - Parses filters from URL search params on mount
 * - Syncs filter changes to URL for bookmarking/sharing
 * - Calculates active filter count (date range and amount range each count as 1)
 * - Date preset management (converts presets to actual dates)
 * - Status filter management (converts to pending boolean)
 *
 * @returns Filter state and management functions
 */
export function useTransactionFilters(): UseTransactionFiltersResult {
  const state = useFilterState(SPEC, { clearKeysOnWrite: ['offset'] })

  // Build TransactionFilters from generic state
  // Note: pfc_primaries is tracked in the URL spec for filtering but is not part of
  // the public TransactionFilters type (it maps to pfc_primaries via API filter params)
  const filters = useMemo<TransactionFilters>(
    () => ({
      account_ids:   state.filters.account_ids.length > 0 ? state.filters.account_ids : undefined,
      categories:    state.filters.categories.length > 0 ? state.filters.categories : undefined,
      start_date:    state.filters.start_date,
      end_date:      state.filters.end_date,
      search:        state.filters.search,
      pending:       state.filters.pending,
      min_amount:    state.filters.min_amount,
      max_amount:    state.filters.max_amount,
      amount_type:   state.filters.amount_type as TransactionFilters['amount_type'],
    }),
    [state.filters],
  )

  // Active filter count — date range and amount range each count as one grouped filter
  const activeFilterCount = useMemo(() => {
    let count = 0
    if (filters.account_ids && filters.account_ids.length > 0) count++
    if (state.filters.pfc_primaries.length > 0) count++
    if (filters.categories && filters.categories.length > 0) count++
    if (filters.start_date || filters.end_date) count++
    if (filters.search) count++
    if (filters.pending !== undefined) count++
    if (filters.min_amount !== undefined || filters.max_amount !== undefined) count++
    if (filters.amount_type && filters.amount_type !== 'all') count++
    return count
  }, [filters, state.filters.pfc_primaries])

  // Feature projections
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
        state.setFilters({ start_date: range.startDate, end_date: range.endDate })
      } else {
        state.setFilters({ start_date: undefined, end_date: undefined })
      }
    },
    [state.setFilters],
  )

  const setCustomDateRange = useCallback(
    (startDate: string, endDate: string) => {
      state.setFilters({ start_date: startDate, end_date: endDate })
    },
    [state.setFilters],
  )

  const setStatusFilter = useCallback(
    (status: StatusFilter) => {
      if (status === 'pending')     state.setFilter('pending', true)
      else if (status === 'posted') state.setFilter('pending', false)
      else                          state.clearFilter('pending')
    },
    [state.setFilter, state.clearFilter],
  )

  return {
    filters,
    setFilter:         state.setFilter as UseTransactionFiltersResult['setFilter'],
    setFilters:        state.setFilters as UseTransactionFiltersResult['setFilters'],
    clearFilter:       state.clearFilter as UseTransactionFiltersResult['clearFilter'],
    clearAllFilters:   state.clearAllFilters,
    activeFilterCount,
    hasActiveFilters:  activeFilterCount > 0,
    datePreset,
    setDatePreset,
    setCustomDateRange,
    statusFilter,
    setStatusFilter,
  }
}
