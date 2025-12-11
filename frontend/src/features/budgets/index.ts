/**
 * Budgets Feature Module
 * Issue #30: Budget Dashboard UI
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
} from './api/budgetsApi';

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

// Components
export { BudgetProgressBar } from './components/BudgetProgressBar';
export type { BudgetProgressBarProps } from './components/BudgetProgressBar';

export { BudgetCard } from './components/BudgetCard';
export type { BudgetCardProps } from './components/BudgetCard';

export { SavingsGoalCard } from './components/SavingsGoalCard';
export type { SavingsGoalCardProps } from './components/SavingsGoalCard';

export { BudgetsPage } from './components/BudgetsPage';

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
