/**
 * useBudgetTransactions Hook
 * Issue #499: Transaction-Budget Linking UI
 * Issue #564: Migrated to generated React Query hooks
 *
 * Fetches paginated transactions matching a budget category.
 */

import { useState, useCallback, useRef, useEffect } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import {
  useGetCategoryTransactions,
  getGetCategoryTransactionsQueryKey,
  getCategoryTransactions,
} from '@/api/generated/endpoints/budgets/budgets';
import type { Transaction } from '../../transactions/types';

interface UseBudgetTransactionsOptions {
  pageSize?: number;
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
  const queryClient = useQueryClient();

  // State for accumulated transactions (for load more)
  const [allTransactions, setAllTransactions] = useState<Transaction[]>([]);
  const [loadingMore, setLoadingMore] = useState(false);
  const [hasMore, setHasMore] = useState(false);
  const [totalCount, setTotalCount] = useState(0);
  const [totalAmount, setTotalAmount] = useState(0);

  // Track current offset for pagination
  const offsetRef = useRef(0);

  // Initial query
  const {
    data,
    isLoading,
    error: queryError,
    refetch: queryRefetch,
  } = useGetCategoryTransactions(budgetId, categoryId, { limit: pageSize, offset: 0 }, {
    query: {
      staleTime: 30 * 1000, // 30 seconds
      enabled: !!budgetId && !!categoryId,
    },
  });

  // Convert error to Error instance
  const error = queryError
    ? queryError instanceof Error
      ? queryError
      : new Error('Failed to fetch transactions')
    : null;

  // Update state when initial data arrives
  useEffect(() => {
    if (data) {
      // Cast transactions to local type
      const transactions = data.transactions as unknown as Transaction[];
      setAllTransactions(transactions);
      setTotalCount(data.total_count);
      setTotalAmount(data.total_amount);
      setHasMore(data.has_more);
      offsetRef.current = pageSize;
    }
  }, [data, pageSize]);

  // Load more transactions
  const loadMore = useCallback(async () => {
    if (isLoading || loadingMore || !hasMore) {
      return;
    }

    setLoadingMore(true);

    try {
      const response = await getCategoryTransactions(budgetId, categoryId, {
        limit: pageSize,
        offset: offsetRef.current,
      });

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
      setLoadingMore(false);
    }
  }, [budgetId, categoryId, pageSize, isLoading, loadingMore, hasMore]);

  // Refetch from start
  const refetch = useCallback(async () => {
    offsetRef.current = 0;
    await queryClient.invalidateQueries({
      queryKey: getGetCategoryTransactionsQueryKey(budgetId, categoryId, { limit: pageSize, offset: 0 }),
    });
    await queryRefetch();
  }, [queryClient, queryRefetch, budgetId, categoryId, pageSize]);

  return {
    transactions: allTransactions,
    totalCount,
    totalAmount,
    loading: isLoading,
    loadingMore,
    error,
    hasMore,
    loadMore,
    refetch,
  };
}
