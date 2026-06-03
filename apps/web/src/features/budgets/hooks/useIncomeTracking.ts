import { useMemo } from 'react';
import type { IncomeSource } from '../types';

export interface UseIncomeTrackingOptions {
  /** Configured income sources */
  sources: IncomeSource[];
  /** budget.expected_income — first-class expected income field (Issue #1048) */
  budgetExpectedIncome?: number;
  /** budget.amount — fallback when expected_income is absent */
  budgetAmount?: number;
  /** budget.income_amount — actual income received */
  budgetIncomeAmount?: number;
  // Retained for API compatibility; currently unused (all income derived locally)
  startDate?: string;
  endDate?: string;
  budgetId?: string;
  enabled?: boolean;
}

export interface UseIncomeTrackingResult {
  /** Expected income (sum of manual sources) */
  expectedManualTotal: number;
  /** Expected total income: budget.expected_income ?? budget.amount ?? 0 */
  expectedTotal: number;
  /** Actual income: budget.income_amount ?? 0 */
  actualTotal: number;
}

export function useIncomeTracking({
  sources,
  budgetExpectedIncome,
  budgetAmount,
  budgetIncomeAmount,
}: UseIncomeTrackingOptions): UseIncomeTrackingResult {
  const expectedManualTotal = useMemo(() => {
    return sources
      .filter((s) => s.type === 'manual')
      .reduce((sum, s) => {
        const amount = parseFloat(s.value);
        return sum + (isNaN(amount) ? 0 : amount);
      }, 0);
  }, [sources]);

  // Issue #1048: expected_income is the first-class field; fall back to amount
  // for budgets created before this field existed.
  const expectedTotal = budgetExpectedIncome ?? budgetAmount ?? 0;

  // Issue #1048: Actual income is what was received; no fallback to amount.
  const actualTotal = budgetIncomeAmount ?? 0;

  return {
    expectedManualTotal,
    expectedTotal,
    actualTotal,
  };
}
