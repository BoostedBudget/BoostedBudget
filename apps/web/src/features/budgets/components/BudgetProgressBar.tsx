/**
 * BudgetProgressBar Component
 *
 * Visual progress bar showing budget usage with status indicators.
 *
 * TDD GREEN: Implementation to pass all tests.
 */


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

  const barHeightClass = size === 'small' ? 'h-2' : size === 'large' ? 'h-4' : 'h-3'
  const textSizeClass = size === 'small' ? 'text-xs' : size === 'large' ? 'text-base' : 'text-sm'

  const spentColorClass = status === 'danger' ? 'text-destructive' : status === 'warning' ? 'text-warning' : 'text-success'
  const fillColorClass = status === 'danger' ? 'bg-destructive' : status === 'warning' ? 'bg-warning' : 'bg-success'
  const percentColorClass = status === 'danger' ? 'text-destructive' : 'text-muted-foreground'

  return (
    <div className={`flex flex-col gap-2 w-full budget-progress--${size} budget-progress--${status}`}>
      {showAmounts && (
        <div className={`flex justify-between items-center ${textSizeClass}`}>
          <span className={`font-semibold ${spentColorClass}`} data-ph-mask>{formatCurrency(spent, currencyCode)}</span>
          <span className="text-muted-foreground" data-ph-mask>of {formatCurrency(total, currencyCode)}</span>
        </div>
      )}

      <div
        className={`w-full bg-muted rounded-full overflow-hidden ${barHeightClass}`}
        role="progressbar"
        aria-valuenow={percent}
        aria-valuemin={0}
        aria-valuemax={100}
        aria-label={label}
      >
        <div
          className={`h-full rounded-full transition-[width] duration-300 ease-in-out ${fillColorClass}`}
          style={{ width: `${visualWidth}%` }}
        />
      </div>

      <div className={`text-right font-semibold ${textSizeClass} ${percentColorClass}`}>{percent}%</div>
    </div>
  );
}
