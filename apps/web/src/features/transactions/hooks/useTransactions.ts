/**
 * useTransactions Hook
 *
 * React hook for fetching transactions with filtering, pagination, and debounced search.
 * Issue #564: Migrated to generated React Query hooks
 *
 * TDD GREEN: Implementation to pass all 27 tests.
 */

import { useState, useEffect, useCallback, useRef, useMemo } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import {
  useGetTransactions,
  getGetTransactionsQueryKey,
} from '@boostedbudget/api-client/endpoints/transactions/transactions';
import type { GetTransactionsResponse, TransactionFilters, Transaction } from '../types';

// Constants
const DEFAULT_LIMIT = 50
const SEARCH_DEBOUNCE_MS = 300

export type TransactionSortColumn = 'date' | 'merchant' | 'amount' | 'category'
export type SortDirection = 'asc' | 'desc'

export interface UseTransactionsOptions {
  filters?: TransactionFilters
  limit?: number
  initialOffset?: number
  sortBy?: TransactionSortColumn
  sortDir?: SortDirection
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
  const { filters = {}, limit = DEFAULT_LIMIT, initialOffset = 0, sortBy, sortDir } = options;
  const queryClient = useQueryClient();

  // State
  const [offset, setOffset] = useState<number>(initialOffset);
  const [localData, setLocalData] = useState<GetTransactionsResponse | null>(null);

  // Refs for debouncing search
  const searchTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const previousFiltersRef = useRef<TransactionFilters>(filters);
  const isFirstRenderRef = useRef<boolean>(true);
  const [debouncedSearch, setDebouncedSearch] = useState<string | undefined>(filters.search);

  // Build query params from filters
  const queryParams = useMemo(() => {
    // Sanitize filters - remove empty strings
    const params: Record<string, string | number | boolean | undefined> = {
      limit,
      offset,
    };

    if (filters.account_id) params.account_id = filters.account_id;
    if (filters.start_date) params.start_date = filters.start_date;
    if (filters.end_date) params.end_date = filters.end_date;
    if (debouncedSearch) params.search = debouncedSearch;
    if (filters.pending !== undefined) params.pending = filters.pending;

    if (filters.account_ids && filters.account_ids.length > 0) {
      params.account_ids = filters.account_ids.join(',');
    }
    if (filters.pfc_primaries && filters.pfc_primaries.length > 0) {
      params.pfc_primaries = filters.pfc_primaries.join(',');
    }
    if (filters.min_amount !== undefined) {
      params.min_amount = filters.min_amount;
    }
    if (filters.max_amount !== undefined) {
      params.max_amount = filters.max_amount;
    }
    if (filters.amount_type && filters.amount_type !== 'all') {
      params.amount_type = filters.amount_type;
    }

    if (sortBy) params.sort_by = sortBy;
    if (sortDir) params.sort_dir = sortDir;

    return params;
  }, [limit, offset, filters, debouncedSearch, sortBy, sortDir]);

  // Use generated query hook
  const {
    data: queryData,
    isLoading,
    error: queryError,
    refetch: queryRefetch,
  } = useGetTransactions(queryParams as never, {
    query: {
      staleTime: 30 * 1000, // 30 seconds
    },
  });

  // Sync query data with local state for optimistic updates
  // This is intentional - we need to sync server state to local state for optimistic updates
  useEffect(() => {
    if (queryData) {
      // eslint-disable-next-line react-hooks/set-state-in-effect
      setLocalData(queryData as unknown as GetTransactionsResponse);
    }
  }, [queryData]);

  // Effect: Handle filter changes with search debouncing
  useEffect(() => {
    // Clear any pending search timeout
    if (searchTimeoutRef.current) {
      clearTimeout(searchTimeoutRef.current);
    }

    // Detect if filters changed (excluding first render)
    const filtersChanged =
      !isFirstRenderRef.current &&
      JSON.stringify(previousFiltersRef.current) !== JSON.stringify(filters);

    // Reset offset to 0 if filters changed
    // This is intentional - pagination should reset when filters change
    if (filtersChanged) {
      // eslint-disable-next-line react-hooks/set-state-in-effect
      setOffset(0);
    }

    // Check if search filter changed (needs debouncing)
    const searchChanged =
      previousFiltersRef.current.search !== filters.search ||
      (isFirstRenderRef.current && filters.search);

    if (searchChanged) {
      // Debounce search input
      searchTimeoutRef.current = setTimeout(() => {
        setDebouncedSearch(filters.search);
        if (filtersChanged) {
          setOffset(0);
        }
      }, SEARCH_DEBOUNCE_MS);
    } else if (filtersChanged) {
      // Non-search filter changed - update debounced search immediately
      setDebouncedSearch(filters.search);
    }

    // Update refs
    previousFiltersRef.current = filters;
    isFirstRenderRef.current = false;

    // Cleanup timeout on unmount
    return () => {
      if (searchTimeoutRef.current) {
        clearTimeout(searchTimeoutRef.current);
      }
    };
  }, [filters]);

  // Reset offset to 0 when sort changes
  const prevSortByRef = useRef<TransactionSortColumn | undefined>(sortBy);
  const prevSortDirRef = useRef<SortDirection | undefined>(sortDir);
  useEffect(() => {
    const sortChanged = prevSortByRef.current !== sortBy || prevSortDirRef.current !== sortDir;
    prevSortByRef.current = sortBy;
    prevSortDirRef.current = sortDir;
    if (sortChanged) {
      // eslint-disable-next-line react-hooks/set-state-in-effect
      setOffset(0);
    }
  }, [sortBy, sortDir]);

  // Pagination controls
  const nextPage = useCallback(() => {
    setOffset((prevOffset) => prevOffset + limit);
  }, [limit]);

  const previousPage = useCallback(() => {
    setOffset((prevOffset) => {
      if (prevOffset === 0) {
        // Already at first page, don't go negative
        return prevOffset;
      }
      return Math.max(0, prevOffset - limit);
    });
  }, [limit]);

  const refetch = useCallback(async () => {
    await queryClient.invalidateQueries({
      queryKey: getGetTransactionsQueryKey(queryParams as never),
    });
    await queryRefetch();
  }, [queryClient, queryRefetch, queryParams]);

  // Update a single transaction in the current data
  // Uses merge strategy to preserve immutable fields (like original_category)
  // that may not be included in partial API responses
  const updateTransaction = useCallback((updatedTransaction: Transaction) => {
    setLocalData((prevData) => {
      if (!prevData) return prevData;
      return {
        ...prevData,
        transactions: prevData.transactions.map((txn) =>
          txn.id === updatedTransaction.id ? { ...txn, ...updatedTransaction } : txn
        ),
      };
    });
  }, []);

  // Convert error
  const error = useMemo<Error | null>(() => {
    if (!queryError) return null;
    return queryError instanceof Error ? queryError : new Error('Failed to fetch transactions');
  }, [queryError]);

  // Computed values
  const hasMore = localData?.has_more ?? false;
  const totalCount = localData?.total_count ?? 0;
  const currentPage = limit > 0 ? Math.floor(offset / limit) : 0;

  return {
    data: localData,
    loading: isLoading,
    error,
    nextPage,
    previousPage,
    refetch,
    updateTransaction,
    hasMore,
    totalCount,
    currentPage,
  };
}
