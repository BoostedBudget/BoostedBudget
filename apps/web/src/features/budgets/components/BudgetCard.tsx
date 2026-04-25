/**
 * BudgetCard Component
 *
 * Displays a budget with its details and progress.
 *
 * TDD GREEN: Implementation to pass all tests.
 */

import { Link } from 'react-router-dom';
import { Button } from '@/components/ui/button';
import { BudgetProgressBar } from './BudgetProgressBar';
import { computeBudgetMetrics } from '../types';
import { getBudgetDetailPath } from '../constants';
import type { Budget } from '../types';

export interface BudgetCardProps {
  /** The budget to display */
  budget: Budget;
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
export function BudgetCard({ budget, onDelete }: BudgetCardProps) {
  const metrics = computeBudgetMetrics(budget);
  const categoryCount = budget.categories?.length || 0;

  const handleDelete = () => {
    onDelete?.(budget.id);
  };

  return (
    <div
      data-testid="budget-card"
      className="bg-card border border-border rounded-xl p-6 flex flex-col gap-4 transition-shadow hover:shadow-md"
    >
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
        total={budget.expected_income ?? budget.amount}
        currencyCode={budget.currency_code}
        size="medium"
      />

      {/* Issue #1048: Income progress row */}
      {(budget.expected_income ?? 0) > 0 && (
        <div className="flex flex-col gap-1">
          <div className="flex justify-between text-xs text-muted-foreground">
            <span>Income</span>
            <span data-ph-mask>
              {formatCurrency(budget.income_amount ?? 0, budget.currency_code)} of {formatCurrency(budget.expected_income!, budget.currency_code)} expected
            </span>
          </div>
          <div
            className="h-1 bg-muted rounded-full overflow-hidden"
            role="progressbar"
            aria-label="Income received"
            aria-valuenow={Math.round(((budget.income_amount ?? 0) / budget.expected_income!) * 100)}
            aria-valuemin={0}
            aria-valuemax={100}
          >
            <div
              className="h-full rounded-full bg-success transition-[width] duration-300"
              style={{ width: `${Math.min(((budget.income_amount ?? 0) / budget.expected_income!) * 100, 100)}%` }}
            />
          </div>
        </div>
      )}

      <div className="flex justify-between items-center text-sm text-muted-foreground">
        <span className="font-medium" data-ph-mask>
          {formatCurrency(metrics.remaining_amount, budget.currency_code)} remaining
        </span>
        <span>
          {categoryCount} {categoryCount === 1 ? 'category' : 'categories'}
        </span>
      </div>

      <Button asChild variant="outline" className="w-full">
        <Link to={getBudgetDetailPath(budget.id)}>Manage budget</Link>
      </Button>

      {onDelete && (
        <div className="flex gap-2 mt-2 pt-4 border-t border-border">
          <Button
            variant="outline"
            size="sm"
            onClick={handleDelete}
            aria-label="Delete budget"
            className="text-destructive border-destructive hover:bg-destructive hover:text-destructive-foreground"
          >
            Delete
          </Button>
        </div>
      )}
    </div>
  );
}
