/**
 * useIncomeTracking Hook
 * Issue #496: Income Source Configuration UI for Envelope Budgeting
 *
 * Calculates expected income from manual sources and
 * optionally fetches actual income for existing budgets.
 */

import { useState, useMemo, useCallback } from 'react';
import type { IncomeSource } from '../types';

export interface UseIncomeTrackingOptions {
  /** Budget ID for fetching actual income (undefined for new budgets) */
  budgetId?: string;
  /** Configured income sources */
  sources: IncomeSource[];
  /** Budget start date (YYYY-MM-DD) */
  startDate: string;
  /** Budget end date (YYYY-MM-DD) */
  endDate: string;
  /** Whether tracking is enabled */
  enabled?: boolean;
}

export interface UseIncomeTrackingResult {
  /** Expected income (sum of manual sources) */
  expectedManualTotal: number;
  /** Actual income from API (matching transactions) */
  actualTotal: number;
  /** Whether actual income data is being fetched */
  loading: boolean;
  /** Error from API call */
  error: Error | null;
  /** Refetch actual income data */
  refetch: () => Promise<void>;
}

/**
 * Hook for tracking expected and actual income based on configured sources.
 *
 * Expected income is calculated locally from manual income sources.
 * Actual income (for existing budgets) would be fetched from the API.
 *
 * @param options - Configuration options
 * @returns Income tracking state and controls
 */
export function useIncomeTracking({
  budgetId,
  sources,
  startDate: _startDate,
  endDate: _endDate,
  enabled = true,
}: UseIncomeTrackingOptions): UseIncomeTrackingResult {
  // Note: _startDate and _endDate are reserved for future API integration
  const [actualTotal, setActualTotal] = useState(0);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<Error | null>(null);

  // Calculate expected total from manual sources only
  const expectedManualTotal = useMemo(() => {
    return sources
      .filter((s) => s.type === 'manual')
      .reduce((sum, s) => {
        const amount = parseFloat(s.value);
        return sum + (isNaN(amount) ? 0 : amount);
      }, 0);
  }, [sources]);

  // Fetch actual income for existing budgets
  // Note: This is a placeholder for future API integration
  // The backend already has BudgetIncomeService.CalculateBudgetIncome
  const fetchActual = useCallback(async () => {
    if (!budgetId || !enabled) {
      return;
    }

    setLoading(true);
    setError(null);

    try {
      // TODO: Implement actual API call when endpoint is available
      // const result = await fetchBudgetIncomeActual(budgetId, startDate, endDate);
      // setActualTotal(result.total);

      // For now, actual total remains 0 until API is implemented
      setActualTotal(0);
    } catch (err) {
      setError(err instanceof Error ? err : new Error('Failed to fetch income'));
    } finally {
      setLoading(false);
    }
  }, [budgetId, enabled]);

  return {
    expectedManualTotal,
    actualTotal,
    loading,
    error,
    refetch: fetchActual,
  };
}
