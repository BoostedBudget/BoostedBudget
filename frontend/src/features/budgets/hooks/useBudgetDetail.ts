/**
 * useBudgetDetail Hook
 * Issue #497: Budget Category Manager + Match Config UI
 * Issue #564: Migrated to generated React Query hooks
 *
 * React hook for fetching a single budget with computed metrics.
 */

import { useMemo, useCallback } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import {
  useGetBudget,
  getGetBudgetQueryKey,
} from '@/api/generated/endpoints/budgets/budgets';
import { computeBudgetMetrics } from '../types';
import type { Budget, BudgetWithMetrics } from '../types';

export interface UseBudgetDetailResult {
  /** The budget data */
  budget: Budget | null;
  /** Computed metrics for the budget */
  metrics: BudgetWithMetrics | null;
  /** Whether the budget is currently loading */
  loading: boolean;
  /** Any error that occurred during fetching */
  error: Error | null;
  /** Function to refetch the budget data */
  refetch: () => Promise<void>;
}

/**
 * Hook for fetching and managing a single budget's details
 *
 * @param budgetId - The ID of the budget to fetch
 * @returns Object containing budget data, metrics, loading state, error, and refetch function
 *
 * @example
 * ```tsx
 * function BudgetDetail({ budgetId }: { budgetId: string }) {
 *   const { budget, metrics, loading, error, refetch } = useBudgetDetail(budgetId);
 *
 *   if (loading) return <LoadingSpinner />;
 *   if (error) return <ErrorMessage error={error} onRetry={refetch} />;
 *   if (!budget) return <NotFound />;
 *
 *   return (
 *     <div>
 *       <h1>{budget.name}</h1>
 *       <p>Allocated: ${metrics?.total_allocated}</p>
 *       <p>Spent: ${metrics?.total_spent}</p>
 *     </div>
 *   );
 * }
 * ```
 */
export function useBudgetDetail(budgetId: string): UseBudgetDetailResult {
  const queryClient = useQueryClient();

  const {
    data,
    isLoading,
    error: queryError,
    refetch: queryRefetch,
  } = useGetBudget(budgetId, {
    query: {
      staleTime: 30 * 1000, // 30 seconds
      enabled: !!budgetId, // Only fetch if budgetId is provided
    },
  });

  // Convert error to Error instance
  const error = queryError
    ? queryError instanceof Error
      ? queryError
      : new Error('Failed to fetch budget')
    : null;

  // Convert generated type to local type
  const budget: Budget | null = data ? (data as unknown as Budget) : null;

  // Compute metrics from budget
  const metrics = useMemo<BudgetWithMetrics | null>(() => {
    if (!budget) return null;
    return computeBudgetMetrics(budget);
  }, [budget]);

  // Refetch function that invalidates the cache
  const refetch = useCallback(async () => {
    await queryClient.invalidateQueries({
      queryKey: getGetBudgetQueryKey(budgetId),
    });
    await queryRefetch();
  }, [queryClient, queryRefetch, budgetId]);

  return {
    budget,
    metrics,
    loading: isLoading,
    error,
    refetch,
  };
}
