/**
 * useUserFilters Hook
 * Issue #644: Admin User Management UI
 *
 * URL-synced state management for admin user list filters.
 * Manages search, tier filter, and pagination state.
 */

import { useCallback, useMemo } from 'react'
import { useSearchParams } from 'react-router-dom'
import type { ListAdminUsersParams } from '@boostedbudget/api-client/models'

// Default values
const DEFAULT_PAGE = 1
const DEFAULT_PAGE_SIZE = 20

/**
 * User filter state
 */
export interface UserFilters {
  /** Current page number (1-indexed) */
  page: number
  /** Number of items per page */
  pageSize: number
  /** Search term (email partial match) */
  search: string
  /** Subscription tier filter */
  tier: string
}

/**
 * Result of useUserFilters hook
 */
export interface UseUserFiltersResult {
  /** Current filter state */
  filters: UserFilters
  /** Set search filter (resets page to 1) */
  setSearch: (search: string) => void
  /** Set tier filter (resets page to 1) */
  setTier: (tier: string) => void
  /** Set current page */
  setPage: (page: number) => void
  /** Set page size (resets page to 1) */
  setPageSize: (pageSize: number) => void
  /** Clear all filters and reset to defaults */
  clearFilters: () => void
  /** Whether any filters (search/tier) are active */
  hasActiveFilters: boolean
  /** Params formatted for API call */
  apiParams: ListAdminUsersParams
}

/**
 * Hook for managing admin user list filters with URL synchronization.
 *
 * Features:
 * - Parses filters from URL search params
 * - Syncs filter changes to URL for bookmarking/sharing
 * - Resets pagination when filters change
 * - Provides API-formatted params for query
 */
export function useUserFilters(): UseUserFiltersResult {
  const [searchParams, setSearchParams] = useSearchParams()

  // Parse filters from URL
  const filters = useMemo<UserFilters>(() => {
    const pageStr = searchParams.get('page')
    const pageSizeStr = searchParams.get('page_size')

    return {
      page: pageStr ? parseInt(pageStr, 10) : DEFAULT_PAGE,
      pageSize: pageSizeStr ? parseInt(pageSizeStr, 10) : DEFAULT_PAGE_SIZE,
      search: searchParams.get('search') ?? '',
      tier: searchParams.get('tier') ?? '',
    }
  }, [searchParams])

  // Helper to update a filter and reset page
  const updateFilterAndResetPage = useCallback(
    (key: string, value: string) => {
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev)
          if (value) {
            next.set(key, value)
          } else {
            next.delete(key)
          }
          next.delete('page') // Reset to page 1
          return next
        },
        { replace: true }
      )
    },
    [setSearchParams]
  )

  // Set search (resets page)
  const setSearch = useCallback(
    (search: string) => updateFilterAndResetPage('search', search),
    [updateFilterAndResetPage]
  )

  // Set tier (resets page)
  const setTier = useCallback(
    (tier: string) => updateFilterAndResetPage('tier', tier),
    [updateFilterAndResetPage]
  )

  // Set page
  const setPage = useCallback(
    (page: number) => {
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev)
          if (page > 1) {
            next.set('page', String(page))
          } else {
            next.delete('page')
          }
          return next
        },
        { replace: true }
      )
    },
    [setSearchParams]
  )

  // Set page size (resets page)
  const setPageSize = useCallback(
    (pageSize: number) => {
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev)
          if (pageSize !== DEFAULT_PAGE_SIZE) {
            next.set('page_size', String(pageSize))
          } else {
            next.delete('page_size')
          }
          next.delete('page')
          return next
        },
        { replace: true }
      )
    },
    [setSearchParams]
  )

  // Clear all filters
  const clearFilters = useCallback(() => {
    setSearchParams(new URLSearchParams(), { replace: true })
  }, [setSearchParams])

  // Check if any filters are active (excluding pagination)
  const hasActiveFilters = useMemo(() => {
    return filters.search !== '' || filters.tier !== ''
  }, [filters.search, filters.tier])

  // Format params for API call
  const apiParams = useMemo<ListAdminUsersParams>(() => {
    const params: ListAdminUsersParams = {
      page: filters.page,
      page_size: filters.pageSize,
    }

    if (filters.search) {
      params.search = filters.search
    }

    if (filters.tier) {
      params.tier = filters.tier
    }

    return params
  }, [filters])

  return {
    filters,
    setSearch,
    setTier,
    setPage,
    setPageSize,
    clearFilters,
    hasActiveFilters,
    apiParams,
  }
}
