/**
 * useAccountFilters Hook
 * Issue #590: Inline Column Filtering
 *
 * Manages account filter state with URL synchronization.
 * Filters are persisted in URL params for bookmarking/sharing.
 */

import { useCallback, useMemo } from 'react'
import { useSearchParams } from 'react-router-dom'
import type { RangeValue } from '@/shared/components/ColumnFilterDropdown'

/**
 * Account filter state
 */
export interface AccountFilters {
  /** Institution names to filter by */
  institution: string[]
  /** Account types to filter by */
  type: string[]
  /** Minimum balance */
  balanceMin?: number
  /** Maximum balance */
  balanceMax?: number
}

/**
 * Result of useAccountFilters hook
 */
export interface UseAccountFiltersResult {
  /** Current filter state derived from URL */
  filters: AccountFilters
  /** Institution filter array */
  institutionFilter: string[]
  /** Type filter array */
  typeFilter: string[]
  /** Balance range filter */
  balanceRange: RangeValue | null
  /** Set institution filter */
  setInstitutionFilter: (value: string[]) => void
  /** Set type filter */
  setTypeFilter: (value: string[]) => void
  /** Set balance range filter */
  setBalanceRange: (value: RangeValue | null) => void
  /** Clear all filters */
  clearAllFilters: () => void
  /** Number of active filters */
  activeFilterCount: number
  /** Whether any filters are active */
  hasActiveFilters: boolean
}

/**
 * Hook for managing account filters with URL state synchronization
 *
 * @returns Filter state and management functions
 */
export function useAccountFilters(): UseAccountFiltersResult {
  const [searchParams, setSearchParams] = useSearchParams()

  // Parse filters from URL search params
  const filters = useMemo<AccountFilters>(() => {
    const result: AccountFilters = {
      institution: [],
      type: [],
    }

    // Parse institution (comma-separated)
    const institution = searchParams.get('institution')
    if (institution) {
      result.institution = institution.split(',').filter(Boolean).map((s) => s.trim())
    }

    // Parse type (comma-separated)
    const type = searchParams.get('type')
    if (type) {
      result.type = type.split(',').filter(Boolean).map((s) => s.trim())
    }

    // Parse balance range
    const balanceMin = searchParams.get('balance_min')
    if (balanceMin) {
      const parsed = parseFloat(balanceMin)
      if (!isNaN(parsed)) {
        result.balanceMin = parsed
      }
    }

    const balanceMax = searchParams.get('balance_max')
    if (balanceMax) {
      const parsed = parseFloat(balanceMax)
      if (!isNaN(parsed)) {
        result.balanceMax = parsed
      }
    }

    return result
  }, [searchParams])

  // Derived values for easier component integration
  const institutionFilter = filters.institution
  const typeFilter = filters.type
  const balanceRange: RangeValue | null = useMemo(() => {
    if (filters.balanceMin === undefined && filters.balanceMax === undefined) {
      return null
    }
    return {
      min: filters.balanceMin,
      max: filters.balanceMax,
    }
  }, [filters.balanceMin, filters.balanceMax])

  // Set institution filter
  const setInstitutionFilter = useCallback(
    (value: string[]) => {
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev)
          if (value.length === 0) {
            next.delete('institution')
          } else {
            next.set('institution', value.join(','))
          }
          return next
        },
        { replace: true }
      )
    },
    [setSearchParams]
  )

  // Set type filter
  const setTypeFilter = useCallback(
    (value: string[]) => {
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev)
          if (value.length === 0) {
            next.delete('type')
          } else {
            next.set('type', value.join(','))
          }
          return next
        },
        { replace: true }
      )
    },
    [setSearchParams]
  )

  // Set balance range filter
  const setBalanceRange = useCallback(
    (value: RangeValue | null) => {
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev)
          if (!value || (value.min === undefined && value.max === undefined)) {
            next.delete('balance_min')
            next.delete('balance_max')
          } else {
            if (value.min !== undefined) {
              next.set('balance_min', String(value.min))
            } else {
              next.delete('balance_min')
            }
            if (value.max !== undefined) {
              next.set('balance_max', String(value.max))
            } else {
              next.delete('balance_max')
            }
          }
          return next
        },
        { replace: true }
      )
    },
    [setSearchParams]
  )

  // Clear all filters
  const clearAllFilters = useCallback(() => {
    setSearchParams(
      (prev) => {
        const next = new URLSearchParams(prev)
        next.delete('institution')
        next.delete('type')
        next.delete('balance_min')
        next.delete('balance_max')
        return next
      },
      { replace: true }
    )
  }, [setSearchParams])

  // Calculate active filter count
  const activeFilterCount = useMemo(() => {
    let count = 0
    if (filters.institution.length > 0) count++
    if (filters.type.length > 0) count++
    if (filters.balanceMin !== undefined || filters.balanceMax !== undefined) count++
    return count
  }, [filters])

  const hasActiveFilters = activeFilterCount > 0

  return {
    filters,
    institutionFilter,
    typeFilter,
    balanceRange,
    setInstitutionFilter,
    setTypeFilter,
    setBalanceRange,
    clearAllFilters,
    activeFilterCount,
    hasActiveFilters,
  }
}
