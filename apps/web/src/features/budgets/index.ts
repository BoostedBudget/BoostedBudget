/**
 * Budgets Feature Module
 * Issue #30: Budget Dashboard UI
 * Issue #495: Enhanced Budget Types and API Layer for Envelope Budgeting
 *
 * Exports all public APIs for the budgets feature.
 */

export type {
  Budget,
  BudgetCategory,
  BudgetPeriod,
  BudgetWithMetrics,
  CreateBudgetRequest,
  UpdateBudgetRequest,
  CreateBudgetCategoryRequest,
  UpdateBudgetCategoryRequest,
  IncomeSource,
  IncomeSourceType,
  MatchLogic,
} from './types';

export {
  computeBudgetMetrics,
  formatCategoryPath,
} from './types';

export { useBudgets } from './hooks/useBudgets';
export type { UseBudgetsResult } from './hooks/useBudgets';

export { useCurrentBudget } from './hooks/useCurrentBudget';
export type { UseCurrentBudgetResult } from './hooks/useCurrentBudget';

export { useBudgetDetail } from './hooks/useBudgetDetail';
export type { UseBudgetDetailResult } from './hooks/useBudgetDetail';

export { useAvailableCategories } from './hooks/useAvailableCategories';
export type { UseAvailableCategoriesResult } from './hooks/useAvailableCategories';

export { BudgetProgressBar } from './components/BudgetProgressBar';
export type { BudgetProgressBarProps } from './components/BudgetProgressBar';

export { BudgetCard } from './components/BudgetCard';
export type { BudgetCardProps } from './components/BudgetCard';

export { BudgetsPage } from './components/BudgetsPage';

export { BudgetDetailPage } from './components/BudgetDetailPage';

export { CreateSavingsGoalDialog } from '../savings/components/CreateSavingsGoalDialog';
export type { CreateSavingsGoalDialogProps } from '../savings/components/CreateSavingsGoalDialog';

export { CreateBudgetDialog } from './components/CreateBudgetDialog';
export type { CreateBudgetDialogProps } from './components/CreateBudgetDialog';

export { MilestoneIndicator } from './components/MilestoneIndicator';
export type { MilestoneIndicatorProps } from './components/MilestoneIndicator';

export { useMilestoneTracking } from './hooks/useMilestoneTracking';
export type { UseMilestoneTrackingResult } from './hooks/useMilestoneTracking';

export { BudgetIncomeSection } from './components/budget-detail/BudgetIncomeSection';
export type { BudgetIncomeSectionProps } from './components/budget-detail/BudgetIncomeSection';

export { UnallocatedFundsCard } from './components/budget-detail/UnallocatedFundsCard';
export type { UnallocatedFundsCardProps } from './components/budget-detail/UnallocatedFundsCard';

// TEMPORARY re-exports — will be removed in T12 once all callers migrated
export type { SavingsGoal, CreateSavingsGoalRequest, UpdateSavingsGoalRequest } from '../savings/types';
export { computeSavingsProgress, computeSavingsRemaining } from '../savings/types';
export { useSavingsGoals } from '../savings/hooks/useSavingsGoals';
export type { SavingsGoalWithProgress, UseSavingsGoalsOptions, UseSavingsGoalsResult } from '../savings/hooks/useSavingsGoals';
export { SavingsGoalCard } from '../savings/components/SavingsGoalCard';
export type { SavingsGoalCardProps } from '../savings/components/SavingsGoalCard';
export { GOAL_TEMPLATES, getGoalTemplate, calculateDeadlineFromTemplate } from '../savings/goalTemplates';
export type { GoalTemplate } from '../savings/goalTemplates';
