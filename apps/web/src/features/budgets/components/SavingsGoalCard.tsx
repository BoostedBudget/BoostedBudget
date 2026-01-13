/**
 * SavingsGoalCard Component
 *
 * Displays a savings goal with progress.
 *
 * TDD GREEN: Implementation to pass all tests.
 */

import { computeSavingsProgress, computeSavingsRemaining } from '../types';
import type { SavingsGoal } from '../types';
import './SavingsGoalCard.css';

export interface SavingsGoalCardProps {
  /** The savings goal to display */
  goal: SavingsGoal;
  /** Callback when edit is clicked */
  onEdit?: (goal: SavingsGoal) => void;
  /** Callback when delete is clicked */
  onDelete?: (goalId: string) => void;
  /** Callback when add funds is clicked */
  onAddFunds?: (goal: SavingsGoal) => void;
}

/**
 * Format currency amount
 */
function formatCurrency(amount: number, currencyCode: string): string {
  try {
    return new Intl.NumberFormat('en-US', {
      style: 'currency',
      currency: currencyCode,
      minimumFractionDigits: 2,
      maximumFractionDigits: 2,
    }).format(amount);
  } catch {
    return `${currencyCode} ${amount.toFixed(2)}`;
  }
}

/**
 * Format date for display
 */
function formatDate(dateString: string): string {
  const date = new Date(dateString);
  return date.toLocaleDateString('en-US', { month: 'short', day: 'numeric', year: 'numeric' });
}

/**
 * SavingsGoalCard component showing goal details with progress
 *
 * @example
 * ```tsx
 * <SavingsGoalCard
 *   goal={goal}
 *   onEdit={(g) => handleEdit(g)}
 *   onDelete={(id) => handleDelete(id)}
 *   onAddFunds={(g) => handleAddFunds(g)}
 * />
 * ```
 */
export function SavingsGoalCard({ goal, onEdit, onDelete, onAddFunds }: SavingsGoalCardProps) {
  const progress = Math.round(computeSavingsProgress(goal));
  const remaining = computeSavingsRemaining(goal);

  const handleEdit = () => {
    onEdit?.(goal);
  };

  const handleDelete = () => {
    onDelete?.(goal.id);
  };

  const handleAddFunds = () => {
    onAddFunds?.(goal);
  };

  // Cap visual width at 100%
  const visualWidth = Math.min(progress, 100);

  return (
    <div className="savings-goal-card">
      <div className="savings-goal-card__header">
        <div className="savings-goal-card__title-row">
          <h3 className="savings-goal-card__name">{goal.name}</h3>
          {goal.is_completed && <span className="savings-goal-card__badge">Completed</span>}
        </div>
        {goal.description && (
          <p className="savings-goal-card__description">{goal.description}</p>
        )}
      </div>

      <div className="savings-goal-card__amounts">
        <span className="savings-goal-card__current">
          {formatCurrency(goal.current_amount, goal.currency_code)}
        </span>
        <span className="savings-goal-card__separator">/</span>
        <span className="savings-goal-card__target">
          {formatCurrency(goal.target_amount, goal.currency_code)}
        </span>
      </div>

      <div className="savings-goal-card__progress">
        <div
          className="savings-goal-card__progress-bar"
          role="progressbar"
          aria-valuenow={progress}
          aria-valuemin={0}
          aria-valuemax={100}
        >
          <div
            className="savings-goal-card__progress-fill"
            style={{ width: `${visualWidth}%` }}
          />
        </div>
        <span className="savings-goal-card__percentage">{progress}%</span>
      </div>

      <div className="savings-goal-card__footer">
        <span className="savings-goal-card__remaining">
          {formatCurrency(remaining, goal.currency_code)} to go
        </span>
        {goal.deadline && (
          <span className="savings-goal-card__deadline">
            Due: {formatDate(goal.deadline)}
          </span>
        )}
      </div>

      {(onEdit || onDelete || onAddFunds) && (
        <div className="savings-goal-card__actions">
          {onAddFunds && !goal.is_completed && (
            <button
              className="savings-goal-card__btn savings-goal-card__btn--primary"
              onClick={handleAddFunds}
              aria-label="Add funds"
            >
              Add Funds
            </button>
          )}
          {onEdit && (
            <button
              className="savings-goal-card__btn savings-goal-card__btn--edit"
              onClick={handleEdit}
              aria-label="Edit goal"
            >
              Edit
            </button>
          )}
          {onDelete && (
            <button
              className="savings-goal-card__btn savings-goal-card__btn--delete"
              onClick={handleDelete}
              aria-label="Delete goal"
            >
              Delete
            </button>
          )}
        </div>
      )}
    </div>
  );
}
