/**
 * useCurrentBudget Hook
 *
 * React hook for fetching the current active budget with computed metrics.
 *
 * TDD GREEN: Implementation to pass all tests.
 */

import { useState, useEffect, useCallback, useMemo } from 'react';
import { fetchCurrentBudget } from '../api/budgetsApi';
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
  const [budget, setBudget] = useState<Budget | null>(null);
  const [loading, setLoading] = useState<boolean>(true);
  const [error, setError] = useState<Error | null>(null);
  const [hasNoBudget, setHasNoBudget] = useState<boolean>(false);

  // Load current budget function
  const loadCurrentBudget = useCallback(async () => {
    try {
      setLoading(true);
      setError(null);
      setHasNoBudget(false);
      const result = await fetchCurrentBudget();
      setBudget(result);
    } catch (err) {
      // Check for 404 - no current budget exists
      if (
        err &&
        typeof err === 'object' &&
        'response' in err &&
        (err as { response?: { status?: number } }).response?.status === 404
      ) {
        setHasNoBudget(true);
        setBudget(null);
        setError(null);
      } else {
        setError(err instanceof Error ? err : new Error('Failed to fetch current budget'));
        setBudget(null);
      }
    } finally {
      setLoading(false);
    }
  }, []);

  // Initial load
  useEffect(() => {
    loadCurrentBudget();
  }, [loadCurrentBudget]);

  // Refetch function
  const refetch = useCallback(async () => {
    await loadCurrentBudget();
  }, [loadCurrentBudget]);

  // Compute metrics from budget
  const metrics = useMemo<BudgetWithMetrics | null>(() => {
    if (!budget) return null;
    return computeBudgetMetrics(budget);
  }, [budget]);

  return {
    budget,
    metrics,
    loading,
    error,
    hasNoBudget,
    refetch,
  };
}
