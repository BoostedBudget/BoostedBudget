// Budget feature types - Issue #29 & #30 & #495

// Re-export shared matching types for convenience
import { MatchLogic } from '../../shared/types/matching';
export { MatchLogic } from '../../shared/types/matching';

// Budget period type matching backend domain
export type BudgetPeriod = 'weekly' | 'monthly' | 'quarterly' | 'yearly' | 'custom';

// =============================================================================
// Issue #495: Envelope Budgeting - Income Source Types
// =============================================================================

/**
 * Income source type - matches backend domain.IncomeSourceType
 */
export type IncomeSourceType = 'plaid_category' | 'tag' | 'manual';

/**
 * Income source configuration - matches backend domain.IncomeSource
 */
export interface IncomeSource {
  type: IncomeSourceType;
  value: string; // Category name, tag, or amount string
  description?: string; // Optional user-friendly label
}

// Budget category allocation within a budget
export interface BudgetCategory {
  id: string;
  budget_id: string;
  category: string[]; // Hierarchical category array e.g., ["Food and Drink", "Restaurants"]
  allocated_amount: number;
  spent_amount: number;
  created_at: string;
  updated_at: string;
  // Issue #495: Envelope budgeting - flexible matching
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
  category: string[];
  allocated_amount: number;
  // Issue #495: Envelope budgeting
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
  const remaining_amount = budget.amount - total_spent;
  const percent_used = budget.amount > 0 ? (total_spent / budget.amount) * 100 : 0;
  const is_over_budget = total_spent > budget.amount;

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

// Format category hierarchy for display
export function formatCategoryPath(category: string[]): string {
  return category.join(' > ');
}
