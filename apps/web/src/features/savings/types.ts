/**
 * Savings Feature Types
 * Issue #954: Savings page
 *
 * Domain types and pure functions for savings goals.
 * Extracted from features/budgets/types.ts.
 */

/** Valid progress_basis values (Issue #1300). */
export type ProgressBasis = 'manual' | 'contributions' | 'balance' | 'account_percentage';

export const PROGRESS_BASIS_LABELS: Record<ProgressBasis, string> = {
  manual: 'Manual',
  contributions: 'Confirmed Contributions',
  balance: 'Account Balance',
  account_percentage: 'Account % Split',
};

export interface SavingsGoal {
  id: string;
  user_id: string;
  name: string;
  description?: string;
  target_amount: number;
  current_amount: number;
  currency_code: string;
  account_id: string;
  deadline?: string;
  is_completed: boolean;
  completed_at?: string;
  created_at: string;
  updated_at: string;
  /** How current_amount is derived (Issue #1300). Defaults to "manual". */
  progress_basis?: ProgressBasis;
  /** Required when progress_basis is "account_percentage" (0 < x ≤ 1). */
  account_percentage?: number;
}

export interface CreateSavingsGoalRequest {
  name: string;
  description?: string;
  target_amount: number;
  current_amount?: number;
  currency_code?: string;
  account_id: string;
  /** Stable logical account ID — survives Plaid re-links (Issue #1109). */
  user_account_id?: string;
  deadline?: string;
  /** How current_amount is derived (Issue #1300). Omit for default "manual". */
  progress_basis?: ProgressBasis;
  /** Required when progress_basis is "account_percentage". Range (0, 1]. */
  account_percentage?: number;
}

export interface UpdateSavingsGoalRequest {
  name?: string;
  description?: string;
  target_amount?: number;
  current_amount?: number;
  account_id?: string;
  deadline?: string;
  /** Change the progress basis (Issue #1300). Omit to preserve existing basis. */
  progress_basis?: ProgressBasis;
  /** Required when progress_basis is "account_percentage". Range (0, 1]. */
  account_percentage?: number;
}

export function computeSavingsProgress(goal: SavingsGoal): number {
  if (goal.target_amount === 0) return 0;
  return (goal.current_amount / goal.target_amount) * 100;
}

export function computeSavingsRemaining(goal: SavingsGoal): number {
  const remaining = goal.target_amount - goal.current_amount;
  return remaining < 0 ? 0 : remaining;
}
