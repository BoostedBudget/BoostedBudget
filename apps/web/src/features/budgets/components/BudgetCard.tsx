/**
 * BudgetCard Component
 *
 * Displays a budget with its details and progress.
 *
 * TDD GREEN: Implementation to pass all tests.
 */

import { Link } from 'react-router-dom';
import { BudgetProgressBar } from './BudgetProgressBar';
import { computeBudgetMetrics } from '../types';
import type { Budget } from '../types';
import './BudgetCard.css';

export interface BudgetCardProps {
  /** The budget to display */
  budget: Budget;
  /** Callback when edit is clicked */
  onEdit?: (budget: Budget) => void;
  /** Callback when delete is clicked */
  onDelete?: (budgetId: string) => void;
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
  return date.toLocaleDateString('en-US', { month: 'short', day: 'numeric' });
}

/**
 * Format period for display
 */
function formatPeriod(period: string): string {
  const periodMap: Record<string, string> = {
    weekly: 'Weekly',
    monthly: 'Monthly',
    quarterly: 'Quarterly',
    yearly: 'Yearly',
    custom: 'Custom',
  };
  return periodMap[period] || period;
}

/**
 * BudgetCard component showing budget details with progress
 *
 * @example
 * ```tsx
 * <BudgetCard
 *   budget={budget}
 *   onEdit={(b) => handleEdit(b)}
 *   onDelete={(id) => handleDelete(id)}
 * />
 * ```
 */
export function BudgetCard({ budget, onEdit, onDelete }: BudgetCardProps) {
  const metrics = computeBudgetMetrics(budget);
  const categoryCount = budget.categories?.length || 0;

  const handleEdit = () => {
    onEdit?.(budget);
  };

  const handleDelete = () => {
    onDelete?.(budget.id);
  };

  return (
    <div className="budget-card">
      <div className="budget-card__header">
        <div className="budget-card__title-row">
          <h3 className="budget-card__name">{budget.name}</h3>
          {budget.is_active && <span className="budget-card__badge">Active</span>}
        </div>
        {budget.description && (
          <p className="budget-card__description">{budget.description}</p>
        )}
      </div>

      <div className="budget-card__meta">
        <span className="budget-card__period">{formatPeriod(budget.period)}</span>
        <span className="budget-card__dates">
          {formatDate(budget.start_date)}
          {budget.end_date && ` - ${formatDate(budget.end_date)}`}
        </span>
      </div>

      <div className="budget-card__amount" data-ph-mask>
        {formatCurrency(budget.amount, budget.currency_code)}
      </div>

      <BudgetProgressBar
        spent={metrics.total_spent}
        total={budget.amount}
        currencyCode={budget.currency_code}
        size="medium"
      />

      <div className="budget-card__footer">
        <span className="budget-card__remaining" data-ph-mask>
          {formatCurrency(metrics.remaining_amount, budget.currency_code)} remaining
        </span>
        <span className="budget-card__categories">
          {categoryCount} {categoryCount === 1 ? 'category' : 'categories'}
        </span>
      </div>

      <Link
        to={`/budgets/${budget.id}`}
        className="budget-card__manage-link"
      >
        Manage Categories
      </Link>

      {(onEdit || onDelete) && (
        <div className="budget-card__actions">
          {onEdit && (
            <button
              className="budget-card__btn budget-card__btn--edit"
              onClick={handleEdit}
              aria-label="Edit budget"
            >
              Edit
            </button>
          )}
          {onDelete && (
            <button
              className="budget-card__btn budget-card__btn--delete"
              onClick={handleDelete}
              aria-label="Delete budget"
            >
              Delete
            </button>
          )}
        </div>
      )}
    </div>
  );
}
