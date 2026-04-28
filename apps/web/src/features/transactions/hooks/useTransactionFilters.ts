import { useCallback, useMemo } from 'react'
import { useSearchParams } from 'react-router-dom'
import { getDateRange, inferPreset } from '@/shared/dates'
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

// URL param names for array filters
const ARRAY_PARAMS = ['account_ids', 'pfc_primaries'] as const

// URL param names for numeric filters
const NUMERIC_PARAMS = ['min_amount', 'max_amount'] as const

/**
 * Helper function to convert a value to its URL representation
 */
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

/**
 * Hook for managing transaction filters with URL state synchronization
 *
 * Features:
 * - Parses filters from URL search params on mount
 * - Syncs filter changes to URL for bookmarking/sharing
 * - Calculates active filter count
 * - Provides setFilter, clearFilter, clearAllFilters methods
 * - Date preset management (converts presets to actual dates)
 * - Status filter management (converts to pending boolean)
 *
 * @returns Filter state and management functions
 */
export function useTransactionFilters(): UseTransactionFiltersResult {
  const [searchParams, setSearchParams] = useSearchParams()

  // Parse filters from URL search params
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

  // Set a single filter value
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

          // Reset offset when filters change (back to first page)
          next.delete('offset')

          return next
        },
        { replace: true }
      )
    },
    [setSearchParams]
  )

  // Set multiple filters at once
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

          // Reset offset when filters change
          next.delete('offset')

          return next
        },
        { replace: true }
      )
    },
    [setSearchParams]
  )

  // Clear a specific filter
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

  // Clear all filters
  const clearAllFilters = useCallback(() => {
    setSearchParams(new URLSearchParams(), { replace: true })
  }, [setSearchParams])

  // Calculate active filter count (exclude default values)
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

  // Date preset (inferred from current dates)
  const datePreset = useMemo<DatePreset>(
    () => inferPreset(filters.start_date, filters.end_date),
    [filters.start_date, filters.end_date]
  )

  // Set date preset (updates start_date and end_date)
  const setDatePreset = useCallback(
    (preset: DatePreset) => {
      if (preset === 'custom') {
        // Don't change dates for custom - user will set manually
        return
      }

      const range = getDateRange(preset)
      if (range) {
        setFilters({
          start_date: range.startDate,
          end_date: range.endDate,
        })
      } else {
        // 'all' preset - clear date filters
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

  // Status filter (derived from pending boolean)
  const statusFilter = useMemo<StatusFilter>(() => {
    if (filters.pending === true) return 'pending'
    if (filters.pending === false) return 'posted'
    return 'all'
  }, [filters.pending])

  // Set status filter (updates pending)
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

  // Set custom date range
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
    setFilter,
    setFilters,
    clearFilter,
    clearAllFilters,
    activeFilterCount,
    hasActiveFilters,
    datePreset,
    setDatePreset,
    setCustomDateRange,
    statusFilter,
    setStatusFilter,
  }
}
