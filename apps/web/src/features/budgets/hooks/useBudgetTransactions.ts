/**
 * useBudgetTransactions Hook
 * Issue #499: Transaction-Budget Linking UI
 * Issue #564: Migrated to generated React Query hooks
 *
 * Fetches paginated transactions matching a budget category.
 */

import { useState, useCallback, useRef, useEffect, useMemo } from 'react';
import { hashKey, useQueryClient } from '@tanstack/react-query';
import {
  useGetCategoryTransactions,
  getGetCategoryTransactionsQueryKey,
  getCategoryTransactions,
} from '@boostedbudget/api-client/endpoints/budgets/budgets';
import type { Transaction } from '../../transactions/types';

interface UseBudgetTransactionsOptions {
  pageSize?: number;
  snapshotVersion?: string;
}

interface UseBudgetTransactionsResult {
  transactions: Transaction[];
  totalCount: number;
  totalAmount: number;
  loading: boolean;
  loadingMore: boolean;
  error: Error | null;
  hasMore: boolean;
  loadMore: () => Promise<void>;
  refetch: () => Promise<void>;
}

/**
 * Hook for fetching transactions that match a budget category.
 *
 * @param budgetId - The budget ID
 * @param categoryId - The category ID within the budget
 * @param options - Optional configuration (pageSize)
 * @returns Transaction data and pagination controls
 */
export function useBudgetTransactions(
  budgetId: string,
  categoryId: string,
  options?: UseBudgetTransactionsOptions
): UseBudgetTransactionsResult {
  const pageSize = options?.pageSize ?? 20;
  const snapshotVersion = options?.snapshotVersion;
  const queryClient = useQueryClient();
  const pageOneQueryKey = useMemo(() => {
    const canonicalKey = getGetCategoryTransactionsQueryKey(
      budgetId,
      categoryId,
      { limit: pageSize, offset: 0 }
    );
    return snapshotVersion === undefined
      ? canonicalKey
      : [...canonicalKey, snapshotVersion];
  }, [budgetId, categoryId, pageSize, snapshotVersion]);
  const pageOneQueryHash = useMemo(() => hashKey(pageOneQueryKey), [pageOneQueryKey]);

  // State for accumulated transactions (for load more)
  const [allTransactions, setAllTransactions] = useState<Transaction[]>([]);
  const [loadingMore, setLoadingMore] = useState(false);
  const [hasMore, setHasMore] = useState(false);
  const [totalCount, setTotalCount] = useState(0);
  const [totalAmount, setTotalAmount] = useState(0);
  const [paginationQueryHash, setPaginationQueryHash] = useState(pageOneQueryHash);
  const paginationIsCurrent = paginationQueryHash === pageOneQueryHash;

  // Track current offset for pagination
  const offsetRef = useRef(0);
  const pageEpoch = useRef(0);

  // Initial query
  const {
    data,
    dataUpdatedAt,
    isLoading,
    error: queryError,
    refetch: queryRefetch,
  } = useGetCategoryTransactions(budgetId, categoryId, { limit: pageSize, offset: 0 }, {
    query: {
      queryKey: pageOneQueryKey,
      staleTime: 30 * 1000, // 30 seconds
      refetchOnWindowFocus: true,
      refetchInterval: 30_000,
      refetchIntervalInBackground: false,
      enabled: !!budgetId && !!categoryId,
    },
  });
  const currentPageTransactions = data?.transactions as unknown as Transaction[] | undefined;

  // Invalidation happens before React renders the refetch. Retire direct pagination
  // requests immediately, including when structural sharing keeps page one identical.
  useEffect(() => {
    return queryClient.getQueryCache().subscribe((event) => {
      if (event.type === 'updated' && event.query.queryHash === pageOneQueryHash &&
        (event.action.type === 'invalidate' || event.action.type === 'fetch')) {
        pageEpoch.current += 1;
        setLoadingMore(false);
      }
    });
  }, [pageOneQueryHash, queryClient]);

  // Convert error to Error instance
  const error = queryError
    ? queryError instanceof Error
      ? queryError
      : new Error('Failed to fetch transactions')
    : null;

  // Update state when initial data arrives
  useEffect(() => {
    pageEpoch.current += 1;
    setLoadingMore(false);
    if (data) {
      // Cast transactions to local type
      const transactions = data.transactions as unknown as Transaction[];
       
      setAllTransactions(transactions);
       
      setTotalCount(data.total_count);
       
      setTotalAmount(data.total_amount);
       
      setHasMore(data.has_more);
      offsetRef.current = pageSize;
    } else {
      setAllTransactions([]);
      setTotalCount(0);
      setTotalAmount(0);
      setHasMore(false);
      offsetRef.current = 0;
    }
    setPaginationQueryHash(pageOneQueryHash);
    return () => { pageEpoch.current += 1; };
  }, [data, dataUpdatedAt, pageSize, pageOneQueryHash]);

  // Load more transactions
  const loadMore = useCallback(async () => {
    if (!paginationIsCurrent || isLoading || loadingMore || !hasMore) {
      return;
    }

    setLoadingMore(true);
    const epoch = pageEpoch.current;

    try {
      const response = await getCategoryTransactions(budgetId, categoryId, {
        limit: pageSize,
        offset: offsetRef.current,
      });
      if (epoch !== pageEpoch.current) return;

      // Cast transactions to local type
      const newTransactions = response.transactions as unknown as Transaction[];
      setAllTransactions((prev) => [...prev, ...newTransactions]);
      setTotalCount(response.total_count);
      setTotalAmount(response.total_amount);
      setHasMore(response.has_more);
      offsetRef.current += pageSize;
    } catch (err) {
      // Error is handled but we don't update the main error state for loadMore
      console.error('Failed to load more transactions:', err);
    } finally {
      if (epoch === pageEpoch.current) setLoadingMore(false);
    }
  }, [budgetId, categoryId, pageSize, isLoading, loadingMore, hasMore, paginationIsCurrent]);

  // Refetch from start
  const refetch = useCallback(async () => {
    pageEpoch.current += 1;
    setLoadingMore(false);
    offsetRef.current = 0;
    await queryClient.invalidateQueries({
      queryKey: pageOneQueryKey,
      exact: true,
    });
    await queryRefetch();
  }, [queryClient, queryRefetch, pageOneQueryKey]);

  return {
    transactions: paginationIsCurrent ? allTransactions : currentPageTransactions ?? [],
    totalCount: paginationIsCurrent ? totalCount : data?.total_count ?? 0,
    totalAmount: paginationIsCurrent ? totalAmount : data?.total_amount ?? 0,
    loading: isLoading,
    loadingMore: paginationIsCurrent && loadingMore,
    error,
    hasMore: paginationIsCurrent ? hasMore : data?.has_more ?? false,
    loadMore,
    refetch,
  };
}
