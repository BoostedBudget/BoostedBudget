import { MatchLogic } from '@boostedbudget/shared-types/matching';
import { formatPFCEnum } from '@/shared/categories/formatPfc';
import type { Transaction } from '../transactions/types';
export type { MatchLogic } from '@boostedbudget/shared-types/matching';

export type BudgetPeriod = 'weekly' | 'monthly' | 'quarterly' | 'yearly' | 'custom';

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

export interface BudgetCategory {
  id: string;
  budget_id: string;
  category_kind: string; // 'pfc' | 'custom' | 'savings'
  pfc_primary?: string;
  pfc_detailed?: string;
  pfc_primary_display?: string;
  pfc_detailed_display?: string;
  custom_category_id?: string;
  // Parent linkage of the referenced custom category (#1203), populated by the API.
  // Used to detect sub-allocations so budget totals aren't double-counted.
  custom_parent_id?: string | null;
  custom_parent_pfc_primary?: string | null;
  // Issue #1300: savings bucket linkage
  savings_goal_id?: string | null;
  savings_goal_name?: string | null; // Server-supplied display name for the linked goal
  auto_created?: boolean;
  managed_by_goal_id?: string | null;
  allocated_amount: number;
  spent_amount: number;
  created_at: string;
  updated_at: string;
  tags: string[];
  match_logic: MatchLogic;
}

/**
 * Reports whether `cat` is a sub-allocation within a parent that is also budgeted
 * (#1203). Mirrors the backend Budget.IsSubAllocationWithin — such rows are excluded
 * from budget totals because their amount/spend is already inside the parent.
 */
export function isSubAllocationWithin(
  cat: BudgetCategory,
  budgetedPfcPrimaries: Set<string>,
  budgetedCustomIds: Set<string>
): boolean {
  if (cat.category_kind === 'pfc') {
    return !!(cat.pfc_detailed && cat.pfc_primary && budgetedPfcPrimaries.has(cat.pfc_primary));
  }
  if (cat.category_kind === 'custom') {
    if (cat.custom_parent_id && budgetedCustomIds.has(cat.custom_parent_id)) return true;
    if (cat.custom_parent_pfc_primary && budgetedPfcPrimaries.has(cat.custom_parent_pfc_primary))
      return true;
  }
  return false;
}

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
  income_amount: number;
  income_sources: IncomeSource[];
  expected_income?: number;
}

export interface BudgetWithMetrics extends Budget {
  total_allocated: number;
  total_spent: number;
  remaining_amount: number;
  percent_used: number;
  is_over_budget: boolean;
  unallocated_funds: number;
  is_fully_allocated: boolean;
}

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
  income_amount?: number;
  income_sources?: IncomeSource[];
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
  income_amount?: number;
  income_sources?: IncomeSource[];
}

export interface CreateBudgetCategoryRequest {
  category_kind: string; // 'pfc' | 'custom' | 'savings'
  pfc_primary?: string;
  pfc_detailed?: string;
  custom_category_id?: string;
  savings_goal_id?: string; // #1300: savings bucket linkage
  allocated_amount: number;
  tags?: string[];
  match_logic?: MatchLogic;
}

export interface UpdateBudgetCategoryRequest {
  allocated_amount?: number;
  tags?: string[];
  match_logic?: MatchLogic;
}

export function computeBudgetMetrics(budget: Budget): BudgetWithMetrics {
  const categories = budget.categories || [];

  // Totals sum TOP-LEVEL categories only — a parent's spend already rolls up its
  // children, and a sub-allocation's amount is within its parent (#1203). This
  // mirrors the backend Budget.TotalAllocated/TotalSpent.
  const budgetedPfcPrimaries = new Set<string>();
  const budgetedCustomIds = new Set<string>();
  for (const cat of categories) {
    if (cat.category_kind === 'pfc' && cat.pfc_primary && !cat.pfc_detailed) {
      budgetedPfcPrimaries.add(cat.pfc_primary);
    }
    if (cat.category_kind === 'custom' && cat.custom_category_id) {
      budgetedCustomIds.add(cat.custom_category_id);
    }
  }
  const topLevel = categories.filter(
    (cat) => !isSubAllocationWithin(cat, budgetedPfcPrimaries, budgetedCustomIds)
  );
  const total_allocated = topLevel.reduce((sum, cat) => sum + cat.allocated_amount, 0);
  const total_spent = topLevel.reduce((sum, cat) => sum + cat.spent_amount, 0);
  const spendingBasis = budget.expected_income ?? budget.amount;
  const remaining_amount = spendingBasis - total_spent;
  const percent_used = spendingBasis > 0 ? (total_spent / spendingBasis) * 100 : 0;
  const is_over_budget = total_spent > spendingBasis;

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

/** @deprecated Use formatBudgetCategoryLabel instead */
export function formatCategoryPath(category: string[]): string {
  return category.join(' > ');
}

export function formatBudgetCategoryLabel(cat: Pick<BudgetCategory, 'category_kind' | 'pfc_primary' | 'pfc_detailed' | 'pfc_primary_display' | 'pfc_detailed_display' | 'custom_category_id' | 'savings_goal_name'>): string {
  if (cat.category_kind === 'savings') {
    return cat.savings_goal_name ?? 'Savings Goal';
  }
  if (cat.category_kind === 'custom') {
    return cat.custom_category_id ?? 'Custom';
  }
  if (cat.pfc_detailed_display) return cat.pfc_detailed_display;
  if (cat.pfc_primary_display) return cat.pfc_primary_display;
  if (cat.pfc_detailed) return formatPFCEnum(cat.pfc_detailed);
  if (cat.pfc_primary) return formatPFCEnum(cat.pfc_primary);
  return 'Uncategorized';
}

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
