import { formatPFCEnum } from '@/shared/categories/formatPfc';
import type { Transaction } from '../transactions/types';
import type { MatchLogic } from '@boostedbudget/shared-types/matching';
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
 * Reports whether a PARENT's match_logic still contains the plain category match — i.e.
 * its matched-transaction set is a superset of "category only" (Issue #1630). '' / undefined
 * is treated as 'category_only' (the domain/column default). Mirrors backend
 * isContainingMatchLogic in domain/budget.go.
 */
function isContainingMatchLogic(ml: MatchLogic | undefined | ''): boolean {
  return ml === undefined || ml === '' || ml === 'category_only' || ml === 'or';
}

/**
 * Reports whether a CHILD's match_logic keeps it inside the plain category match — i.e. it
 * cannot escape the category clause via tags (Issue #1630). '' / undefined is treated as
 * 'category_only'. Mirrors backend isContainedMatchLogic in domain/budget.go.
 */
function isContainedMatchLogic(ml: MatchLogic | undefined | ''): boolean {
  return ml === undefined || ml === '' || ml === 'category_only' || ml === 'and';
}

/**
 * Reports whether `cat` is a sub-allocation within a parent that is also budgeted
 * (#1203). Mirrors the backend Budget.IsSubAllocationWithin — such rows are excluded
 * from budget totals because their amount/spend is already inside the parent.
 *
 * Hierarchy alone is not sufficient (Issue #1630): exclusion is sound only when this
 * CHILD's own matched-transaction set is contained within the PARENT's.
 * budgetedPfcPrimaries/budgetedCustomIds already carry the parent-side containment test
 * (see filterTopLevel/buildBudgetedParentSets), so this function only needs to additionally
 * gate on the CHILD's own match_logic.
 */
export function isSubAllocationWithin(
  cat: BudgetCategory,
  budgetedPfcPrimaries: Set<string>,
  budgetedCustomIds: Set<string>
): boolean {
  if (!isContainedMatchLogic(cat.match_logic)) return false;
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

/**
 * Builds lookup sets of the PFC primaries and custom category IDs that are budgeted as
 * top-level rows AND eligible to adopt children — i.e. the row's own match_logic still
 * contains the plain category match (Issue #1630). A 'tag_only' or 'and' parent has no
 * envelope a plain-category child could fall inside, so it is deliberately excluded.
 */
function buildBudgetedParentSets(
  categories: BudgetCategory[]
): { budgetedPfcPrimaries: Set<string>; budgetedCustomIds: Set<string> } {
  const budgetedPfcPrimaries = new Set<string>();
  const budgetedCustomIds = new Set<string>();
  for (const cat of categories) {
    if (!isContainingMatchLogic(cat.match_logic)) continue;
    if (cat.category_kind === 'pfc' && cat.pfc_primary && !cat.pfc_detailed) {
      budgetedPfcPrimaries.add(cat.pfc_primary);
    }
    if (cat.category_kind === 'custom' && cat.custom_category_id) {
      budgetedCustomIds.add(cat.custom_category_id);
    }
  }
  return { budgetedPfcPrimaries, budgetedCustomIds };
}

/**
 * Filters `categories` down to top-level rows only — excludes any row that is a
 * sub-allocation within a budgeted parent (#1203, #1630). Every allocation/spend total
 * in the budgets feature must be computed from this filtered list rather than the raw
 * array, or a nested child double-counts dollars its parent's own amount already covers.
 */
export function filterTopLevel(categories: BudgetCategory[]): BudgetCategory[] {
  const { budgetedPfcPrimaries, budgetedCustomIds } = buildBudgetedParentSets(categories);
  return categories.filter(
    (cat) => !isSubAllocationWithin(cat, budgetedPfcPrimaries, budgetedCustomIds)
  );
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
  /** Inherit series_id from this budget (Issue #1300 M22). */
  from_budget_id?: string;
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
  const topLevel = filterTopLevel(categories);
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
