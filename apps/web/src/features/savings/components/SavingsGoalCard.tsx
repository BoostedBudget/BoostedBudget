import { useState } from 'react'
import { PlugZap } from 'lucide-react'
import { Card } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { computeSavingsProgress, computeSavingsRemaining } from '../types'
import type { SavingsGoal } from '../types'
import { GoalContributionsSheet } from './GoalContributionsSheet'

export interface SavingsGoalCardProps {
  /** The savings goal to display */
  goal: SavingsGoal;
  /** Callback when edit is clicked */
  onEdit?: (goal: SavingsGoal) => void;
  /** Callback when delete is clicked */
  onDelete?: (goalId: string) => void;
  /** Callback when add funds is clicked */
  onAddFunds?: (goal: SavingsGoal) => void;
  /** Callback when contributions change (refetch the goal list) */
  onContributionChanged?: () => void;
  /**
   * Freshness of a balance-derived amount, from GET /savings-goals/progress-summary
   * (Issue #1569 Task 13 / E14). Only 'basis_account_disconnected' renders anything: it is the
   * one value the user can act on, and the only one that means the number will NEVER move
   * again. 'stale' means the balance IS still being updated, just not in the last 24 h — a
   * badge for that would be noise on every card overnight.
   */
  freshness?: string;
}

/** The one freshness value that is both permanent and actionable. */
const FRESHNESS_BASIS_ACCOUNT_DISCONNECTED = 'basis_account_disconnected'

function formatCurrency(amount: number, currencyCode: string): string {
  try {
    return new Intl.NumberFormat('en-US', {
      style: 'currency',
      currency: currencyCode,
      minimumFractionDigits: 2,
      maximumFractionDigits: 2,
    }).format(amount)
  } catch {
    return `${currencyCode} ${amount.toFixed(2)}`
  }
}

function formatDate(dateString: string): string {
  const [year, month, day] = dateString.split('T')[0].split('-').map(Number)
  return new Date(year, month - 1, day).toLocaleDateString('en-US', { month: 'short', day: 'numeric', year: 'numeric' })
}

export function SavingsGoalCard({ goal, onEdit, onDelete, onAddFunds, onContributionChanged, freshness }: SavingsGoalCardProps) {
  const progress = Math.round(computeSavingsProgress(goal))
  const remaining = computeSavingsRemaining(goal)
  const visualWidth = Math.min(progress, 100)
  const [contributionsOpen, setContributionsOpen] = useState(false)
  const isDisconnected = freshness === FRESHNESS_BASIS_ACCOUNT_DISCONNECTED

  return (
    <Card className="flex flex-col gap-4 transition-shadow hover:shadow-md">
      <div className="flex flex-col gap-1">
        <div className="flex items-center gap-2">
          <h3 id={`savings-goal-name-${goal.id}`} className="m-0 text-lg font-semibold text-foreground">
            <span data-ph-mask>{goal.name}</span>
          </h3>
          {goal.is_completed && (
            <Badge variant="success">Completed</Badge>
          )}
          {isDisconnected && (
            <Badge variant="warning">
              <PlugZap className="mr-1 h-3 w-3" aria-hidden="true" />
              Account disconnected
            </Badge>
          )}
        </div>
        {goal.description && <p className="m-0 text-sm text-muted-foreground"><span data-ph-mask>{goal.description}</span></p>}
      </div>

      <div className="flex items-baseline gap-1">
        <span className="text-2xl font-bold text-primary" data-ph-mask>
          {formatCurrency(goal.current_amount, goal.currency_code)}
        </span>
        <span className="text-base text-muted-foreground">/</span>
        <span className="text-base text-muted-foreground" data-ph-mask>
          {formatCurrency(goal.target_amount, goal.currency_code)}
        </span>
      </div>

      <div className="flex items-center gap-3">
        <div
          className="flex-1 h-3 bg-muted rounded-full overflow-hidden"
          role="progressbar"
          aria-labelledby={`savings-goal-name-${goal.id}`}
          aria-valuenow={progress}
          aria-valuemin={0}
          aria-valuemax={100}
        >
          <div
            className="h-full bg-gradient-to-r from-primary to-primary/80 rounded-full transition-[width] duration-300 ease-in-out"
            style={{ width: `${visualWidth}%` }}
          />
        </div>
        <span className="text-sm font-semibold text-muted-foreground min-w-[3rem] text-right">{progress}%</span>
      </div>

      {isDisconnected && (
        // role="status" (not an alert): this is standing context about a goal, not an
        // interruption. The badge above carries the same fact, so the notice repeats it in
        // words — nothing here is conveyed by colour alone.
        <p role="status" className="m-0 text-sm text-muted-foreground">
          This goal&rsquo;s linked account is no longer connected, so the amount above is frozen at its
          last known value. Reconnect the account to resume updates.
        </p>
      )}

      <div className="flex justify-between items-center text-sm text-muted-foreground">
        <span className="font-medium" data-ph-mask>
          {formatCurrency(remaining, goal.currency_code)} to go
        </span>
        {goal.deadline && <span className="savings-goal-card__deadline text-xs">Due: {formatDate(goal.deadline)}</span>}
      </div>

      {(onEdit || onDelete || onAddFunds) && (
        <div className="flex gap-2 mt-2 pt-4 border-t border-border flex-wrap">
          {onAddFunds && !goal.is_completed && (
            <button
              className="px-4 py-2 text-sm font-medium bg-primary text-primary-foreground border border-primary rounded-md cursor-pointer hover:bg-primary/90 transition-colors"
              onClick={() => onAddFunds(goal)}
              aria-label="Add funds"
            >
              Add Funds
            </button>
          )}
          {onEdit && (
            <button
              className="px-4 py-2 text-sm font-medium text-primary border border-primary rounded-md cursor-pointer bg-transparent hover:bg-primary/10 transition-colors"
              onClick={() => onEdit(goal)}
              aria-label="Edit goal"
            >
              Edit
            </button>
          )}
          {onDelete && (
            <button
              className="px-4 py-2 text-sm font-medium text-destructive border border-destructive rounded-md cursor-pointer bg-transparent hover:bg-destructive hover:text-destructive-foreground transition-colors"
              onClick={() => onDelete(goal.id)}
              aria-label="Delete goal"
            >
              Delete
            </button>
          )}
        </div>
      )}

      {/* Contributions button — only shown for contributions-basis goals (R3-3 C8) */}
      {(onEdit || onDelete) && goal.progress_basis === 'contributions' && (
        <button
          className="mt-1 text-xs font-medium text-primary hover:underline self-start"
          onClick={() => setContributionsOpen(true)}
          aria-label="View contributions"
        >
          Contributions
        </button>
      )}

      <GoalContributionsSheet
        goalId={goal.id}
        goalName={goal.name}
        open={contributionsOpen}
        onOpenChange={setContributionsOpen}
        onContributionChanged={() => onContributionChanged?.()}
      />
    </Card>
  )
}
