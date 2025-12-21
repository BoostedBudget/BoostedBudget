/**
 * useTransactions Hook
 *
 * React hook for fetching transactions with filtering, pagination, and debounced search.
 *
 * TDD GREEN: Implementation to pass all 27 tests.
 */

import { useState, useEffect, useCallback, useRef } from 'react'
import { fetchTransactions } from '../api/transactionsApi'
import type { GetTransactionsResponse, TransactionFilters } from '../types'

// Constants
const DEFAULT_LIMIT = 50
const SEARCH_DEBOUNCE_MS = 300

export interface UseTransactionsOptions {
  filters?: TransactionFilters
  limit?: number
  initialOffset?: number
}

export interface UseTransactionsResult {
  data: GetTransactionsResponse | null
  loading: boolean
  error: Error | null
  nextPage: () => void
  previousPage: () => void
  refetch: () => void
  updateTransaction: (transaction: import('../types').Transaction) => void
  hasMore: boolean
  totalCount: number
  currentPage: number
}

/**
 * Hook for fetching transactions with filtering and pagination
 *
 * Features:
 * - Pagination with nextPage/previousPage
 * - Filtering by account, date range, category, pending status
 * - Debounced search (300ms delay)
 * - Manual refetch
 * - Auto-refetch on filter changes
 * - Resets to page 0 on filter changes
 *
 * @param options - Configuration for limit, offset, and filters
 * @returns Object containing data, loading state, error, pagination controls, and metadata
 *
 * @example
 * ```tsx
 * function TransactionsList() {
 *   const [filters, setFilters] = useState<TransactionFilters>({})
 *
 *   const {
 *     data,
 *     loading,
 *     error,
 *     nextPage,
 *     previousPage,
 *     refetch,
 *     hasMore,
 *     totalCount,
 *     currentPage
 *   } = useTransactions({ filters, limit: 25 })
 *
 *   if (loading) return <LoadingSpinner />
 *   if (error) return <ErrorMessage error={error} onRetry={refetch} />
 *
 *   return (
 *     <>
 *       <TransactionFilters filters={filters} onChange={setFilters} />
 *       {data?.transactions.map(txn => <TransactionCard key={txn.id} data={txn} />)}
 *       <Pagination
 *         onNext={nextPage}
 *         onPrevious={previousPage}
 *         hasMore={hasMore}
 *         currentPage={currentPage}
 *       />
 *     </>
 *   )
 * }
 * ```
 */
export function useTransactions(options: UseTransactionsOptions = {}): UseTransactionsResult {
  const { filters = {}, limit = DEFAULT_LIMIT, initialOffset = 0 } = options

  // State
  const [data, setData] = useState<GetTransactionsResponse | null>(null)
  const [loading, setLoading] = useState<boolean>(true)
  const [error, setError] = useState<Error | null>(null)
  const [offset, setOffset] = useState<number>(initialOffset)

  // Refs for debouncing search
  const searchTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const previousFiltersRef = useRef<TransactionFilters>(filters)
  const isFirstRenderRef = useRef<boolean>(true)

  // Load transactions function
  const loadTransactions = useCallback(
    async (currentOffset: number) => {
      try {
        setLoading(true)
        setError(null)

        // Sanitize filters - remove empty strings
        const sanitizedFilters: TransactionFilters = {}
        // Legacy single-value filters (backward compatibility)
        if (filters.account_id) sanitizedFilters.account_id = filters.account_id
        if (filters.start_date) sanitizedFilters.start_date = filters.start_date
        if (filters.end_date) sanitizedFilters.end_date = filters.end_date
        if (filters.category) sanitizedFilters.category = filters.category
        if (filters.search) sanitizedFilters.search = filters.search
        if (filters.pending !== undefined) sanitizedFilters.pending = filters.pending

        // Issue #529: New multi-value and range filters
        if (filters.account_ids && filters.account_ids.length > 0) {
          sanitizedFilters.account_ids = filters.account_ids
        }
        if (filters.categories && filters.categories.length > 0) {
          sanitizedFilters.categories = filters.categories
        }
        if (filters.min_amount !== undefined) {
          sanitizedFilters.min_amount = filters.min_amount
        }
        if (filters.max_amount !== undefined) {
          sanitizedFilters.max_amount = filters.max_amount
        }
        if (filters.amount_type && filters.amount_type !== 'all') {
          sanitizedFilters.amount_type = filters.amount_type
        }

        const result = await fetchTransactions({
          limit,
          offset: currentOffset,
          filters: sanitizedFilters,
        })

        setData(result)
      } catch (err) {
        setError(err instanceof Error ? err : new Error('Failed to fetch transactions'))
        setData(null)
      } finally {
        setLoading(false)
      }
    },
    [limit, filters]
  )

  // Pagination controls
  const nextPage = useCallback(() => {
    setOffset((prevOffset) => {
      const newOffset = prevOffset + limit
      loadTransactions(newOffset)
      return newOffset
    })
  }, [limit, loadTransactions])

  const previousPage = useCallback(() => {
    setOffset((prevOffset) => {
      if (prevOffset === 0) {
        // Already at first page, don't go negative
        return prevOffset
      }

      const newOffset = Math.max(0, prevOffset - limit)
      loadTransactions(newOffset)
      return newOffset
    })
  }, [limit, loadTransactions])

  const refetch = useCallback(() => {
    loadTransactions(offset)
  }, [offset, loadTransactions])

  // Update a single transaction in the current data
  // Uses merge strategy to preserve immutable fields (like original_category)
  // that may not be included in partial API responses
  const updateTransaction = useCallback((updatedTransaction: import('../types').Transaction) => {
    setData((prevData) => {
      if (!prevData) return prevData
      return {
        ...prevData,
        transactions: prevData.transactions.map((txn) =>
          txn.id === updatedTransaction.id ? { ...txn, ...updatedTransaction } : txn
        ),
      }
    })
  }, [])

  // Effect: Handle filter changes with search debouncing
  useEffect(() => {
    // Clear any pending search timeout
    if (searchTimeoutRef.current) {
      clearTimeout(searchTimeoutRef.current)
    }

    // Detect if filters changed (excluding first render)
    const filtersChanged =
      !isFirstRenderRef.current &&
      JSON.stringify(previousFiltersRef.current) !== JSON.stringify(filters)

    // Reset offset to 0 if filters changed
    if (filtersChanged) {
      setOffset(0)
    }

    // Check if search filter changed (needs debouncing)
    const searchChanged =
      previousFiltersRef.current.search !== filters.search ||
      (isFirstRenderRef.current && filters.search)

    if (searchChanged) {
      // Debounce search input - always reset to offset 0 on search
      searchTimeoutRef.current = setTimeout(() => {
        loadTransactions(filtersChanged ? 0 : 0) // Always 0 for search changes
      }, SEARCH_DEBOUNCE_MS)
    } else if (filtersChanged || isFirstRenderRef.current) {
      // Non-search filter changed or first render - fetch immediately at offset 0
      loadTransactions(0)
    }

    // Update refs
    previousFiltersRef.current = filters
    isFirstRenderRef.current = false

    // Cleanup timeout on unmount
    return () => {
      if (searchTimeoutRef.current) {
        clearTimeout(searchTimeoutRef.current)
      }
    }
  }, [filters, loadTransactions])

  // Computed values
  const hasMore = data?.has_more ?? false
  const totalCount = data?.total_count ?? 0
  const currentPage = limit > 0 ? Math.floor(offset / limit) : 0

  return {
    data,
    loading,
    error,
    nextPage,
    previousPage,
    refetch,
    updateTransaction,
    hasMore,
    totalCount,
    currentPage,
  }
}
