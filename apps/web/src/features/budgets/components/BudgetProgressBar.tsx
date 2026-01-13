/**
 * BudgetProgressBar Component
 *
 * Visual progress bar showing budget usage with status indicators.
 *
 * TDD GREEN: Implementation to pass all tests.
 */

import './BudgetProgressBar.css';

export interface BudgetProgressBarProps {
  /** Amount spent from the budget */
  spent: number;
  /** Total budget amount */
  total: number;
  /** Currency code for formatting (default: USD) */
  currencyCode?: string;
  /** Accessible label for the progress bar */
  label?: string;
  /** Size variant */
  size?: 'small' | 'medium' | 'large';
  /** Whether to show spent/total amounts */
  showAmounts?: boolean;
}

/**
 * Format currency amount with proper symbol and decimal places
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
 * Calculate percentage and status based on spending
 */
function calculateProgress(spent: number, total: number): { percent: number; status: string } {
  if (total <= 0) {
    return { percent: 0, status: 'normal' };
  }

  const percent = Math.round((spent / total) * 100);

  let status: string;
  if (percent >= 100) {
    status = 'danger';
  } else if (percent >= 75) {
    status = 'warning';
  } else {
    status = 'normal';
  }

  return { percent, status };
}

/**
 * Progress bar component showing budget usage
 *
 * Features:
 * - Visual progress bar with percentage
 * - Color status indicators (normal, warning, danger)
 * - Currency-formatted amounts
 * - Size variants
 * - Accessible ARIA attributes
 *
 * @example
 * ```tsx
 * <BudgetProgressBar
 *   spent={750}
 *   total={1000}
 *   currencyCode="USD"
 *   label="Monthly Budget"
 * />
 * ```
 */
export function BudgetProgressBar({
  spent,
  total,
  currencyCode = 'USD',
  label,
  size = 'medium',
  showAmounts = true,
}: BudgetProgressBarProps) {
  const { percent, status } = calculateProgress(spent, total);

  // Cap the visual width at 100% but show actual percentage in text
  const visualWidth = Math.min(percent, 100);

  const classNames = [
    'budget-progress',
    `budget-progress--${size}`,
    `budget-progress--${status}`,
  ].join(' ');

  return (
    <div className={classNames}>
      {showAmounts && (
        <div className="budget-progress__amounts">
          <span className="budget-progress__spent">{formatCurrency(spent, currencyCode)}</span>
          <span className="budget-progress__total">of {formatCurrency(total, currencyCode)}</span>
        </div>
      )}

      <div
        className="budget-progress__bar"
        role="progressbar"
        aria-valuenow={percent}
        aria-valuemin={0}
        aria-valuemax={100}
        aria-label={label}
      >
        <div
          className="budget-progress__fill"
          style={{ width: `${visualWidth}%` }}
        />
      </div>

      <div className="budget-progress__percentage">{percent}%</div>
    </div>
  );
}
