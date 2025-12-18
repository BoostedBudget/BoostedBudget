/**
 * useBudgetTransactions Hook
 * Issue #499: Transaction-Budget Linking UI
 *
 * Fetches paginated transactions matching a budget category.
 */

import { useState, useEffect, useCallback, useRef } from 'react';
import { fetchCategoryTransactions } from '../api/budgetsApi';
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

  const [transactions, setTransactions] = useState<Transaction[]>([]);
  const [totalCount, setTotalCount] = useState(0);
  const [totalAmount, setTotalAmount] = useState(0);
  const [loading, setLoading] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState<Error | null>(null);
  const [hasMore, setHasMore] = useState(false);

  // Track current offset for pagination
  const offsetRef = useRef(0);

  // Fetch initial data
  const fetchData = useCallback(async () => {
    setLoading(true);
    setError(null);
    offsetRef.current = 0;

    try {
      const response = await fetchCategoryTransactions(budgetId, categoryId, {
        limit: pageSize,
        offset: 0,
      });

      setTransactions(response.transactions);
      setTotalCount(response.total_count);
      setTotalAmount(response.total_amount);
      setHasMore(response.has_more);
      // Track offset based on page size for consistent pagination
      offsetRef.current = pageSize;
    } catch (err) {
      setError(err instanceof Error ? err : new Error('Failed to fetch transactions'));
      setTransactions([]);
    } finally {
      setLoading(false);
    }
  }, [budgetId, categoryId, pageSize]);

  // Load more transactions
  const loadMore = useCallback(async () => {
    if (loading || loadingMore || !hasMore) {
      return;
    }

    setLoadingMore(true);

    try {
      const response = await fetchCategoryTransactions(budgetId, categoryId, {
        limit: pageSize,
        offset: offsetRef.current,
      });

      setTransactions((prev) => [...prev, ...response.transactions]);
      setTotalCount(response.total_count);
      setTotalAmount(response.total_amount);
      setHasMore(response.has_more);
      // Increment offset by pageSize for consistent pagination
      offsetRef.current += pageSize;
    } catch (err) {
      setError(err instanceof Error ? err : new Error('Failed to load more transactions'));
    } finally {
      setLoadingMore(false);
    }
  }, [budgetId, categoryId, pageSize, loading, loadingMore, hasMore]);

  // Refetch from start
  const refetch = useCallback(async () => {
    await fetchData();
  }, [fetchData]);

  // Fetch on mount and when parameters change
  useEffect(() => {
    fetchData();
  }, [fetchData]);

  return {
    transactions,
    totalCount,
    totalAmount,
    loading,
    loadingMore,
    error,
    hasMore,
    loadMore,
    refetch,
  };
}
