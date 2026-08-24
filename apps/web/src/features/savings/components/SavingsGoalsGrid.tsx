/**
 * SavingsGoalsGrid Component
 * Issue #954: Renders responsive grid of goal cards with empty state
 */

import { SavingsGoalCard } from './SavingsGoalCard'
import type { SavingsGoalWithProgress } from '../hooks/useSavingsGoals'
import type { SavingsGoal } from '../types'
import { EmptyState } from '@/components/ui/empty-state'
import { SearchX } from 'lucide-react'

export interface SavingsGoalsGridProps {
  goals: SavingsGoalWithProgress[]
  onEdit: (goal: SavingsGoal) => void
  onDelete: (goalId: string) => void
  /** Shown when goals array is empty due to filtering (not the zero-goals state) */
  emptyMessage?: string
  /** Called when any contribution action (confirm/reject/delete/log) changes data.
   *  Forwarded to each SavingsGoalCard so the parent goal list refreshes (R3-5). */
  onContributionChanged?: () => void
  /**
   * goal id -> `freshness` from the progress summary (Issue #1569 Task 13 / E14). A goal
   * missing from this map is simply not annotated.
   */
  freshnessByGoalId?: Record<string, string>
}

export function SavingsGoalsGrid({ goals, onEdit, onDelete, emptyMessage, onContributionChanged, freshnessByGoalId }: SavingsGoalsGridProps) {
  if (goals.length === 0 && emptyMessage) {
    return <EmptyState size="compact" icon={<SearchX />} title={emptyMessage} />
  }

  return (
    <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
      {goals.map((goal) => (
        <SavingsGoalCard
          key={goal.id}
          goal={goal}
          onEdit={onEdit}
          onDelete={onDelete}
          onContributionChanged={onContributionChanged}
          freshness={freshnessByGoalId?.[goal.id]}
        />
      ))}
    </div>
  )
}
