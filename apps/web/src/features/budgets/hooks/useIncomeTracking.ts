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
  /** budget.expected_income — first-class expected income field (Issue #1048) */
  budgetExpectedIncome?: number;
  /** budget.amount — fallback when expected_income is absent */
  budgetAmount?: number;
  /** budget.income_amount — actual income received */
  budgetIncomeAmount?: number;
}

export interface UseIncomeTrackingResult {
  /** Expected income (sum of manual sources) */
  expectedManualTotal: number;
  /** Expected total income: budget.expected_income ?? budget.amount ?? 0 */
  expectedTotal: number;
  /** Actual income: budget.income_amount ?? 0 */
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
  budgetExpectedIncome,
  budgetAmount,
  budgetIncomeAmount,
}: UseIncomeTrackingOptions): UseIncomeTrackingResult {
  // Note: _startDate and _endDate are reserved for future API integration
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

  // Issue #1048: Expected total from budget fields — expected_income is the
  // first-class field; fall back to amount for backwards compat with old data.
  const expectedTotal = budgetExpectedIncome ?? budgetAmount ?? 0;

  // Issue #1048: Actual income is what was received; no fallback to amount.
  const actualTotal = budgetIncomeAmount ?? 0;

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
      // Implement actual API call when endpoint is available
      // const result = await fetchBudgetIncomeActual(budgetId, startDate, endDate);
      // setActualTotal(result.total);
    } catch (err) {
      setError(err instanceof Error ? err : new Error('Failed to fetch income'));
    } finally {
      setLoading(false);
    }
  }, [budgetId, enabled]);

  return {
    expectedManualTotal,
    expectedTotal,
    actualTotal,
    loading,
    error,
    refetch: fetchActual,
  };
}
