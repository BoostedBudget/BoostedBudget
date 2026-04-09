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
    <div className="bg-card border border-border rounded-xl p-6 flex flex-col gap-4 transition-shadow hover:shadow-md">
      <div className="flex flex-col gap-1">
        <div className="flex items-center gap-2">
          <h3 className="m-0 text-lg font-semibold text-foreground">{budget.name}</h3>
          {budget.is_active && <span className="bg-success/10 text-success text-xs font-medium px-2 py-0.5 rounded-full">Active</span>}
        </div>
        {budget.description && (
          <p className="m-0 text-sm text-muted-foreground">{budget.description}</p>
        )}
      </div>

      <div className="flex justify-between items-center text-sm">
        <span className="font-medium text-foreground">{formatPeriod(budget.period)}</span>
        <span className="budget-card__dates text-muted-foreground">
          {formatDate(budget.start_date)}
          {budget.end_date && ` - ${formatDate(budget.end_date)}`}
        </span>
      </div>

      <div className="text-2xl font-bold text-foreground" data-ph-mask>
        {formatCurrency(budget.amount, budget.currency_code)}
      </div>

      <BudgetProgressBar
        spent={metrics.total_spent}
        total={budget.amount}
        currencyCode={budget.currency_code}
        size="medium"
      />

      <div className="flex justify-between items-center text-sm text-muted-foreground">
        <span className="font-medium" data-ph-mask>
          {formatCurrency(metrics.remaining_amount, budget.currency_code)} remaining
        </span>
        <span>
          {categoryCount} {categoryCount === 1 ? 'category' : 'categories'}
        </span>
      </div>

      <Link
        to={`/budgets/${budget.id}`}
        className="block text-center px-4 py-2 text-sm font-medium text-primary bg-primary/10 border border-primary rounded-md no-underline hover:bg-primary hover:text-primary-foreground transition-colors"
      >
        Manage Categories
      </Link>

      {(onEdit || onDelete) && (
        <div className="flex gap-2 mt-2 pt-4 border-t border-border">
          {onEdit && (
            <button
              className="px-4 py-2 text-sm font-medium text-primary bg-primary/10 border border-primary rounded-md cursor-pointer hover:bg-primary hover:text-primary-foreground transition-colors"
              onClick={handleEdit}
              aria-label="Edit budget"
            >
              Edit
            </button>
          )}
          {onDelete && (
            <button
              className="px-4 py-2 text-sm font-medium text-destructive border border-destructive rounded-md cursor-pointer bg-transparent hover:bg-destructive hover:text-destructive-foreground transition-colors"
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
