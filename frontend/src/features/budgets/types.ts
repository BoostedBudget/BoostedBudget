// Budget feature types - Issue #29 & #30

// Budget period type matching backend domain
export type BudgetPeriod = 'weekly' | 'monthly' | 'quarterly' | 'yearly' | 'custom';

// Budget category allocation within a budget
export interface BudgetCategory {
  id: string;
  budget_id: string;
  category: string[]; // Hierarchical category array e.g., ["Food and Drink", "Restaurants"]
  allocated_amount: number;
  spent_amount: number;
  created_at: string;
  updated_at: string;
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
}

// Computed metrics for budget display
export interface BudgetWithMetrics extends Budget {
  total_allocated: number;
  total_spent: number;
  remaining_amount: number;
  percent_used: number;
  is_over_budget: boolean;
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
}

export interface CreateBudgetCategoryRequest {
  category: string[];
  allocated_amount: number;
}

export interface UpdateBudgetCategoryRequest {
  allocated_amount?: number;
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

  return {
    ...budget,
    total_allocated,
    total_spent,
    remaining_amount,
    percent_used,
    is_over_budget,
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
