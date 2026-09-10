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

/**
 * Reason why a transaction matched a budget category (Issue #499). Relocated here from the
 * now-deleted utils/transactionBudgetMatcher.ts (Issue #1630 task 3) — that module's matching
 * function had zero production callers and its pfc branch did not roll up a custom-override
 * child into its budgeted parent, so it was a third, dead, wrong copy of the match_logic rule.
 * The type itself is still live: TransactionCard and TransactionBudgetBadge both import it.
 */
export type MatchReason = 'category' | 'tag' | 'both';

/**
 * Result of matching a transaction to a budget category (Issue #499). See MatchReason above
 * for why this lives here now instead of in utils/transactionBudgetMatcher.ts.
 */
export interface BudgetCategoryMatch {
  category: BudgetCategory;
  matchReason: MatchReason;
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

/**
 * Reports whether `child` is specifically the sub-allocation nested under `parent` (as opposed
 * to merely "some" budgeted parent — see isSubAllocationWithin above). Mirrors backend
 * DuplicateParentIdentity's shape test (Issue #1630 Task 4). Exported so every caller shares
 * one relation: AllocationBreakdown, BudgetCategoryCard and BudgetCategoryManager all use it.
 * AllocationBreakdown briefly had its own private copy (isNestedUnder); that was removed in the
 * #1630 simplification pass, which is exactly what this export exists to prevent.
 */
export function isSubAllocationOfParent(child: BudgetCategory, parent: BudgetCategory): boolean {
  if (!isContainingMatchLogic(parent.match_logic)) return false;
  if (!isContainedMatchLogic(child.match_logic)) return false;
  if (child.category_kind === 'pfc') {
    return !!(
      child.pfc_detailed &&
      child.pfc_primary &&
      parent.category_kind === 'pfc' &&
      !parent.pfc_detailed &&
      parent.pfc_primary === child.pfc_primary
    );
  }
  if (child.category_kind === 'custom') {
    if (child.custom_parent_id) {
      return parent.category_kind === 'custom' && parent.custom_category_id === child.custom_parent_id;
    }
    if (child.custom_parent_pfc_primary) {
      return (
        parent.category_kind === 'pfc' &&
        !parent.pfc_detailed &&
        parent.pfc_primary === child.custom_parent_pfc_primary
      );
    }
  }
  return false;
}

/**
 * A parent's own unallocated ALLOCATION remainder (Issue #1630 Task 4): what is left of the
 * parent's own allocated_amount after its sub-allocation children's allocations are carved out.
 * Subtraction is valid here — allocated_amount is a number the user typed, and a sub-allocation
 * child's amount is genuinely a piece of the parent's own. Can be NEGATIVE: over-allocation
 * warns rather than blocks (see AllocationBreakdown's isOverAllocated for the budget-wide
 * analogue), so "$600 of children under a $500 parent" is a legal, persistable state whose
 * remainder is -$100. Only children `isSubAllocationOfParent` recognizes as this SPECIFIC
 * parent's are subtracted — a hierarchy child whose own match_logic escapes containment was
 * never carved out of the parent's allocation in the first place.
 */
export function computeParentAllocationRemainder(
  parent: BudgetCategory,
  children: BudgetCategory[]
): number {
  const childrenAllocated = children
    .filter((c) => isSubAllocationOfParent(c, parent))
    .reduce((sum, c) => sum + c.allocated_amount, 0);
  return parent.allocated_amount - childrenAllocated;
}

/**
 * A parent's own DIRECT spending (Issue #1630 Task 4), distinct from its children's — i.e. the
 * portion of the parent's own spent_amount not attributable to a sub-allocation child.
 *
 * Unlike the allocation remainder above, spend is NOT generally subtractive (a child's matched
 * spend need not be a subset of the parent's — see Task 0.5). This is safe here ONLY because it
 * subtracts exclusively over `isSubAllocationOfParent` children: the Task 0.5 containment rule
 * (isContainingMatchLogic(parent) && isContainedMatchLogic(child)) is exactly the condition that
 * GUARANTEES a contained child's matched-transaction set is a subset of the parent's — the same
 * guarantee the backend's GetBudgetVsActual dedup already relies on. A child that escapes
 * containment (tag_only/or, or an 'and'-parent's child) is never included here, because its
 * spend was never rolled into the parent's own spent_amount to begin with.
 */
export function computeParentDirectSpend(
  parent: BudgetCategory,
  children: BudgetCategory[]
): number {
  const childrenSpent = children
    .filter((c) => isSubAllocationOfParent(c, parent))
    .reduce((sum, c) => sum + c.spent_amount, 0);
  return parent.spent_amount - childrenSpent;
}

/**
 * Formats a number as USD currency for display. Shared utility to avoid duplication
 * across budget UI components.
 */
export function formatCurrency(amount: number): string {
  return new Intl.NumberFormat('en-US', {
    style: 'currency',
    currency: 'USD',
  }).format(amount);
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
