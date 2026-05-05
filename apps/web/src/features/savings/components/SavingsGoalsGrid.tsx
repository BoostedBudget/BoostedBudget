/**
 * SavingsGoalsGrid Component
 * Issue #954: Savings page — T10
 *
 * Renders a responsive grid of SavingsGoalCard components.
 * Shows an inline empty message when the filtered view is empty
 * (distinguished from the zero-goals empty state on SavingsPage).
 */

import { SavingsGoalCard } from './SavingsGoalCard'
import type { SavingsGoalWithProgress } from '../hooks/useSavingsGoals'
import type { SavingsGoal } from '../types'

export interface SavingsGoalsGridProps {
  goals: SavingsGoalWithProgress[]
  onEdit: (goal: SavingsGoal) => void
  onDelete: (goalId: string) => void
  /** Shown when goals array is empty due to filtering (not the zero-goals state) */
  emptyMessage?: string
}

export function SavingsGoalsGrid({ goals, onEdit, onDelete, emptyMessage }: SavingsGoalsGridProps) {
  if (goals.length === 0 && emptyMessage) {
    return <p className="text-sm text-muted-foreground py-8 text-center">{emptyMessage}</p>
  }

  return (
    <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
      {goals.map((goal) => (
        <SavingsGoalCard
          key={goal.id}
          goal={goal}
          onEdit={onEdit}
          onDelete={onDelete}
        />
      ))}
    </div>
  )
}
