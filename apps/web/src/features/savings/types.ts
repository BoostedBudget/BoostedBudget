/**
 * Savings Feature Types
 * Issue #954: Savings page
 *
 * Domain types and pure functions for savings goals.
 * Extracted from features/budgets/types.ts.
 */

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
}

export interface UpdateSavingsGoalRequest {
  name?: string;
  description?: string;
  target_amount?: number;
  current_amount?: number;
  account_id?: string;
  deadline?: string;
}

export function computeSavingsProgress(goal: SavingsGoal): number {
  if (goal.target_amount === 0) return 0;
  return (goal.current_amount / goal.target_amount) * 100;
}

export function computeSavingsRemaining(goal: SavingsGoal): number {
  const remaining = goal.target_amount - goal.current_amount;
  return remaining < 0 ? 0 : remaining;
}
