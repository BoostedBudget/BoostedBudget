/**
 * useBudgetDetail Hook
 * Issue #497: Budget Category Manager + Match Config UI
 *
 * React hook for fetching a single budget with computed metrics.
 */

import { useState, useEffect, useCallback } from 'react';
import { fetchBudget } from '../api/budgetsApi';
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
  const [budget, setBudget] = useState<Budget | null>(null);
  const [metrics, setMetrics] = useState<BudgetWithMetrics | null>(null);
  const [loading, setLoading] = useState<boolean>(true);
  const [error, setError] = useState<Error | null>(null);

  // Load budget function
  const loadBudget = useCallback(async () => {
    try {
      setLoading(true);
      setError(null);
      const result = await fetchBudget(budgetId);
      setBudget(result);
      setMetrics(computeBudgetMetrics(result));
    } catch (err) {
      setError(err instanceof Error ? err : new Error('Failed to fetch budget'));
      setBudget(null);
      setMetrics(null);
    } finally {
      setLoading(false);
    }
  }, [budgetId]);

  // Initial load and refetch when budgetId changes
  useEffect(() => {
    loadBudget();
  }, [loadBudget]);

  // Refetch function
  const refetch = useCallback(async () => {
    await loadBudget();
  }, [loadBudget]);

  return {
    budget,
    metrics,
    loading,
    error,
    refetch,
  };
}
