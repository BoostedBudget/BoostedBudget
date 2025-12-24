/**
 * useCurrentBudget Hook
 *
 * React hook for fetching the current active budget with computed metrics.
 * Issue #564: Migrated to generated React Query hooks
 *
 * TDD GREEN: Implementation to pass all tests.
 */

import { useCallback, useMemo } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import {
  useGetCurrentBudget,
  getGetCurrentBudgetQueryKey,
} from '@/api/generated/endpoints/budgets/budgets';
import { computeBudgetMetrics } from '../types';
import type { Budget, BudgetWithMetrics } from '../types';

export interface UseCurrentBudgetResult {
  budget: Budget | null;
  metrics: BudgetWithMetrics | null;
  loading: boolean;
  error: Error | null;
  hasNoBudget: boolean;
  refetch: () => Promise<void>;
}

/**
 * Check if error is a 404 error (no current budget exists)
 */
function is404Error(error: unknown): boolean {
  if (!error) return false;

  // Check for response status
  if (typeof error === 'object' && 'response' in error) {
    const response = (error as { response?: { status?: number } }).response;
    return response?.status === 404;
  }

  // Check for status directly on error
  if (typeof error === 'object' && 'status' in error) {
    return (error as { status?: number }).status === 404;
  }

  return false;
}

/**
 * Hook for fetching the current active budget with computed metrics
 *
 * @returns Object containing current budget, metrics, loading state, error, and refetch function
 *
 * @example
 * ```tsx
 * function BudgetOverview() {
 *   const { budget, metrics, loading, error, hasNoBudget, refetch } = useCurrentBudget();
 *
 *   if (loading) return <LoadingSpinner />;
 *   if (error) return <ErrorMessage error={error} onRetry={refetch} />;
 *   if (hasNoBudget) return <CreateBudgetPrompt />;
 *
 *   return (
 *     <BudgetCard budget={budget} metrics={metrics} />
 *   );
 * }
 * ```
 */
export function useCurrentBudget(): UseCurrentBudgetResult {
  const queryClient = useQueryClient();

  const {
    data,
    isLoading,
    error: queryError,
    refetch: queryRefetch,
  } = useGetCurrentBudget({
    query: {
      staleTime: 30 * 1000, // 30 seconds - budget data should be fairly fresh
      retry: (failureCount, error) => {
        // Don't retry on 404 errors
        if (is404Error(error)) return false;
        return failureCount < 2;
      },
    },
  });

  // Check if error is 404 (no current budget)
  const hasNoBudget = is404Error(queryError);

  // Convert error to Error instance (but not for 404s)
  const error = queryError && !hasNoBudget
    ? queryError instanceof Error
      ? queryError
      : new Error('Failed to fetch current budget')
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
      queryKey: getGetCurrentBudgetQueryKey(),
    });
    await queryRefetch();
  }, [queryClient, queryRefetch]);

  return {
    budget,
    metrics,
    loading: isLoading,
    error,
    hasNoBudget,
    refetch,
  };
}
