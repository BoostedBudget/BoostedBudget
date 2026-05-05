/**
 * Savings Feature Module
 * Issue #954: Savings page
 *
 * Exports all public APIs for the savings feature.
 */

export { SavingsPage } from './SavingsPage'
export { SavingsGoalCard } from './components/SavingsGoalCard'
export type { SavingsGoalCardProps } from './components/SavingsGoalCard'
export { useSavingsGoals } from './hooks/useSavingsGoals'
export type { SavingsGoalWithProgress, UseSavingsGoalsOptions, UseSavingsGoalsResult } from './hooks/useSavingsGoals'
export { GOAL_TEMPLATES, getGoalTemplate, calculateDeadlineFromTemplate } from './goalTemplates'
export type { GoalTemplate } from './goalTemplates'
export type { SavingsGoal, CreateSavingsGoalRequest, UpdateSavingsGoalRequest } from './types'
export { computeSavingsProgress, computeSavingsRemaining } from './types'
