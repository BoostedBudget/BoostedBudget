// Budget feature types - Issue #29 & #30 & #495

// Re-export shared matching types for convenience
import { MatchLogic } from '@boostedbudget/shared-types/matching';
import { formatPFCEnum } from '@/shared/categories/formatPfc';
import type { Transaction } from '../transactions/types';
export type { MatchLogic } from '@boostedbudget/shared-types/matching';

// Budget period type matching backend domain
export type BudgetPeriod = 'weekly' | 'monthly' | 'quarterly' | 'yearly' | 'custom';

// =============================================================================
// Issue #495: Envelope Budgeting - Income Source Types
// =============================================================================

/**
 * Income source type - matches backend domain.IncomeSourceType.
 * 'pfc_primary' is the canonical backend value for category-based income sources.
 */
export type IncomeSourceType = 'pfc_primary' | 'tag' | 'manual';

/**
 * Income source configuration - matches backend domain.IncomeSource
 */
export interface IncomeSource {
  type: IncomeSourceType;
  value: string; // Category name, tag, or amount string
  description?: string; // Optional user-friendly label
  value_display?: string; // Server-supplied humanized label for pfc_primary sources
}

// Budget category allocation within a budget (PFC v2 canonical)
export interface BudgetCategory {
  id: string;
  budget_id: string;
  category_kind: string; // 'pfc' | 'custom'
  pfc_primary?: string;
  pfc_detailed?: string;
  pfc_primary_display?: string;
  pfc_detailed_display?: string;
  custom_category_id?: string;
  allocated_amount: number;
  spent_amount: number;
  created_at: string;
  updated_at: string;
  tags: string[];
  match_logic: MatchLogic;
}

// Budget entity
export interface Budget {
  id: string;
  user_id: string;
  name: string;
  description?: string;
  period: BudgetPeriod;
  start_date: string;
  end_date?: string;
  amount: number;
  currency_code: string;
  is_active: boolean;
  categories?: BudgetCategory[];
  created_at: string;
  updated_at: string;
  // Issue #495: Envelope budgeting - income tracking
  income_amount: number;
  income_sources: IncomeSource[];
  // Issue #1048: Expected income (first-class field for income vs spending unification)
  expected_income?: number;
}

// Computed metrics for budget display
export interface BudgetWithMetrics extends Budget {
  total_allocated: number;
  total_spent: number;
  remaining_amount: number;
  percent_used: number;
  is_over_budget: boolean;
  // Issue #495: Envelope budgeting metrics
  unallocated_funds: number;
  is_fully_allocated: boolean;
}

// Savings goal entity
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

// Request types for API calls
export interface CreateBudgetRequest {
  name: string;
  description?: string;
  period: BudgetPeriod;
  start_date: string;
  end_date?: string;
  amount: number;
  currency_code?: string;
  is_active?: boolean;
  categories?: CreateBudgetCategoryRequest[];
  // Issue #495: Envelope budgeting
  income_amount?: number;
  income_sources?: IncomeSource[];
  // Issue #1048: Expected income (first-class field; also sent as `amount` for back-compat)
  expected_income?: number;
}

export interface UpdateBudgetRequest {
  name?: string;
  description?: string;
  period?: BudgetPeriod;
  start_date?: string;
  end_date?: string;
  amount?: number;
  currency_code?: string;
  is_active?: boolean;
  // Issue #495: Envelope budgeting
  income_amount?: number;
  income_sources?: IncomeSource[];
}

export interface CreateBudgetCategoryRequest {
  category_kind: string; // 'pfc' | 'custom'
  pfc_primary?: string;
  pfc_detailed?: string;
  custom_category_id?: string;
  allocated_amount: number;
  tags?: string[];
  match_logic?: MatchLogic;
}

export interface UpdateBudgetCategoryRequest {
  allocated_amount?: number;
  // Issue #495: Envelope budgeting
  tags?: string[];
  match_logic?: MatchLogic;
}

export interface CreateSavingsGoalRequest {
  name: string;
  description?: string;
  target_amount: number;
  current_amount?: number;
  currency_code?: string;
  account_id: string;
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

// Helper function to compute budget metrics
export function computeBudgetMetrics(budget: Budget): BudgetWithMetrics {
  const categories = budget.categories || [];
  const total_allocated = categories.reduce((sum, cat) => sum + cat.allocated_amount, 0);
  const total_spent = categories.reduce((sum, cat) => sum + cat.spent_amount, 0);
  // Issue #1048: Use expected_income as the denominator when present, fall back to amount
  const spendingBasis = budget.expected_income ?? budget.amount;
  const remaining_amount = spendingBasis - total_spent;
  const percent_used = spendingBasis > 0 ? (total_spent / spendingBasis) * 100 : 0;
  const is_over_budget = total_spent > spendingBasis;

  // Issue #495: Envelope budgeting metrics
  const income_amount = budget.income_amount || 0;
  const unallocated_funds = income_amount - total_allocated;
  const is_fully_allocated = total_allocated >= income_amount;

  return {
    ...budget,
    total_allocated,
    total_spent,
    remaining_amount,
    percent_used,
    is_over_budget,
    unallocated_funds,
    is_fully_allocated,
  };
}

// Helper function to compute savings goal progress
export function computeSavingsProgress(goal: SavingsGoal): number {
  if (goal.target_amount === 0) return 0;
  return (goal.current_amount / goal.target_amount) * 100;
}

// Helper function to compute remaining amount for savings goal
export function computeSavingsRemaining(goal: SavingsGoal): number {
  const remaining = goal.target_amount - goal.current_amount;
  return remaining < 0 ? 0 : remaining;
}

/** @deprecated Use formatBudgetCategoryLabel instead */
export function formatCategoryPath(category: string[]): string {
  return category.join(' > ');
}

// Format a budget category for display (PFC v2 canonical)
// Prefers server-supplied display fields, falls back to formatPFCEnum.
export function formatBudgetCategoryLabel(cat: Pick<BudgetCategory, 'category_kind' | 'pfc_primary' | 'pfc_detailed' | 'pfc_primary_display' | 'pfc_detailed_display' | 'custom_category_id'>): string {
  if (cat.category_kind === 'custom') {
    return cat.custom_category_id || 'Custom'
  }
  if (cat.pfc_detailed_display) return cat.pfc_detailed_display
  if (cat.pfc_primary_display) return cat.pfc_primary_display
  if (cat.pfc_detailed) return formatPFCEnum(cat.pfc_detailed)
  if (cat.pfc_primary) return formatPFCEnum(cat.pfc_primary)
  return 'Uncategorized'
}

// =============================================================================
// Issue #499: Transaction-Budget Linking UI
// =============================================================================

/**
 * Response type for GET /budgets/:id/categories/:categoryId/transactions
 * Returns paginated transactions matching a budget category
 */
export interface CategoryTransactionsResponse {
  transactions: Transaction[];
  total_count: number;
  total_amount: number;
  has_more: boolean;
}
