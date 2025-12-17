/**
 * Budgets Feature Module
 * Issue #30: Budget Dashboard UI
 * Issue #495: Enhanced Budget Types and API Layer for Envelope Budgeting
 *
 * Exports all public APIs for the budgets feature.
 */

// Types
export type {
  Budget,
  BudgetCategory,
  BudgetPeriod,
  BudgetWithMetrics,
  SavingsGoal,
  CreateBudgetRequest,
  UpdateBudgetRequest,
  CreateBudgetCategoryRequest,
  UpdateBudgetCategoryRequest,
  CreateSavingsGoalRequest,
  UpdateSavingsGoalRequest,
  // Issue #495: Envelope budgeting types
  IncomeSource,
  IncomeSourceType,
  MatchLogic,
} from './types';

export {
  computeBudgetMetrics,
  computeSavingsProgress,
  computeSavingsRemaining,
  formatCategoryPath,
} from './types';

// API
export {
  fetchBudgets,
  fetchCurrentBudget,
  fetchBudget,
  createBudget,
  updateBudget,
  deleteBudget,
  fetchAvailableCategories,
  addBudgetCategory,
  updateBudgetCategory,
  deleteBudgetCategory,
  fetchSavingsGoals,
  fetchActiveSavingsGoals,
  fetchSavingsGoal,
  createSavingsGoal,
  updateSavingsGoal,
  deleteSavingsGoal,
  // Issue #495: Envelope budgeting APIs
  syncBudget,
  fetchBudgetTransactions,
} from './api/budgetsApi';
export type { BudgetTransactionsResponse } from './api/budgetsApi';

// Hooks
export { useBudgets } from './hooks/useBudgets';
export type { UseBudgetsResult } from './hooks/useBudgets';

export { useCurrentBudget } from './hooks/useCurrentBudget';
export type { UseCurrentBudgetResult } from './hooks/useCurrentBudget';

export { useSavingsGoals } from './hooks/useSavingsGoals';
export type {
  SavingsGoalWithProgress,
  UseSavingsGoalsOptions,
  UseSavingsGoalsResult,
} from './hooks/useSavingsGoals';

export { useBudgetDetail } from './hooks/useBudgetDetail';
export type { UseBudgetDetailResult } from './hooks/useBudgetDetail';

export { useAvailableCategories } from './hooks/useAvailableCategories';
export type { UseAvailableCategoriesResult } from './hooks/useAvailableCategories';

// Components
export { BudgetProgressBar } from './components/BudgetProgressBar';
export type { BudgetProgressBarProps } from './components/BudgetProgressBar';

export { BudgetCard } from './components/BudgetCard';
export type { BudgetCardProps } from './components/BudgetCard';

export { SavingsGoalCard } from './components/SavingsGoalCard';
export type { SavingsGoalCardProps } from './components/SavingsGoalCard';

export { BudgetsPage } from './components/BudgetsPage';

export { BudgetDetailPage } from './components/BudgetDetailPage';

export { CreateSavingsGoalDialog } from './components/CreateSavingsGoalDialog';
export type { CreateSavingsGoalDialogProps } from './components/CreateSavingsGoalDialog';

export { CreateBudgetDialog } from './components/CreateBudgetDialog';
export type { CreateBudgetDialogProps } from './components/CreateBudgetDialog';

export { MilestoneIndicator } from './components/MilestoneIndicator';
export type { MilestoneIndicatorProps } from './components/MilestoneIndicator';

export { useMilestoneTracking } from './hooks/useMilestoneTracking';
export type { UseMilestoneTrackingResult } from './hooks/useMilestoneTracking';

export { GOAL_TEMPLATES, getGoalTemplate, calculateDeadlineFromTemplate } from './goalTemplates';
export type { GoalTemplate } from './goalTemplates';
