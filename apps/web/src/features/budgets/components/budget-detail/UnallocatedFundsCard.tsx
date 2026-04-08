/**
 * UnallocatedFundsCard Component
 * Issue #498: Budget Detail Page for Envelope Budgeting
 *
 * Displays unallocated funds with action buttons for creating
 * savings goals or allocating to categories.
 *
 * TDD GREEN: Implementation to pass all tests.
 */


export interface UnallocatedFundsCardProps {
  /** Amount of unallocated funds (can be negative if over-allocated) */
  unallocatedAmount: number;
  /** Total income for calculating percentage */
  totalIncome: number;
  /** Currency code for formatting */
  currencyCode: string;
  /** Callback when "Create Savings Goal" button is clicked */
  onCreateSavingsGoal?: () => void;
  /** Callback when "Allocate to Category" button is clicked */
  onAllocateToCategory?: () => void;
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
 * Calculate percentage with rounding
 */
function calculatePercentage(amount: number, total: number): number {
  if (total === 0) return 0;
  return Math.round((amount / total) * 100);
}

/**
 * Get card status based on unallocated amount
 */
function getCardStatus(amount: number): 'positive' | 'neutral' | 'warning' {
  if (amount > 0) return 'positive';
  if (amount < 0) return 'warning';
  return 'neutral';
}

/**
 * UnallocatedFundsCard component showing available unallocated funds
 *
 * @example
 * ```tsx
 * <UnallocatedFundsCard
 *   unallocatedAmount={900}
 *   totalIncome={4000}
 *   currencyCode="USD"
 *   onCreateSavingsGoal={() => navigate('/savings/new')}
 * />
 * ```
 */
export function UnallocatedFundsCard({
  unallocatedAmount,
  totalIncome,
  currencyCode,
  onCreateSavingsGoal,
  onAllocateToCategory,
}: UnallocatedFundsCardProps) {
  const status = getCardStatus(unallocatedAmount);
  const percentage = calculatePercentage(Math.abs(unallocatedAmount), totalIncome);
  const displayAmount = Math.abs(unallocatedAmount);
  const hasActions = (onCreateSavingsGoal || onAllocateToCategory) && status === 'positive';

  const cardBase = 'bg-card border border-border rounded-xl p-6'
  const cardVariant =
    status === 'neutral'
      ? 'unallocated-card--neutral border-l-4 border-l-green-600'
      : status === 'warning'
        ? 'unallocated-card--warning border-l-4 border-l-red-500 bg-destructive/5'
        : 'unallocated-card--positive border-l-4 border-l-primary'

  const iconColor =
    status === 'neutral' ? 'text-green-600' :
    status === 'warning' ? 'text-destructive' :
    'text-primary'

  return (
    <section
      className={`${cardBase} ${cardVariant}`}
      role="region"
      aria-labelledby="unallocated-card-title"
    >
      <div className="flex items-center gap-2 mb-3">
        <svg
          data-testid="unallocated-icon"
          className={`w-5 h-5 ${iconColor}`}
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
          aria-hidden="true"
        >
          {status === 'neutral' ? (
            <polyline points="20 6 9 17 4 12" />
          ) : status === 'warning' ? (
            <>
              <circle cx="12" cy="12" r="10" />
              <line x1="12" y1="8" x2="12" y2="12" />
              <line x1="12" y1="16" x2="12.01" y2="16" />
            </>
          ) : (
            <>
              <circle cx="12" cy="12" r="10" />
              <line x1="12" y1="8" x2="12" y2="16" />
              <line x1="8" y1="12" x2="16" y2="12" />
            </>
          )}
        </svg>
        <h3 id="unallocated-card-title" className="m-0 text-sm font-semibold uppercase tracking-wide text-muted-foreground">
          Unallocated Funds
        </h3>
      </div>

      <div className="flex flex-col gap-2">
        {status === 'neutral' ? (
          <div className="flex items-center gap-2 text-foreground">
            <svg
              data-testid="allocation-complete-icon"
              className="w-5 h-5 text-success"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="2"
              strokeLinecap="round"
              strokeLinejoin="round"
              aria-hidden="true"
            >
              <polyline points="20 6 9 17 4 12" />
            </svg>
            <span>Fully allocated</span>
          </div>
        ) : (
          <>
            <div className="flex items-baseline gap-2 flex-wrap">
              <span className={`text-2xl font-bold ${status === 'warning' ? 'text-destructive' : 'text-foreground'}`}>
                {formatCurrency(displayAmount, currencyCode)}
              </span>
              <span className="text-sm font-medium text-muted-foreground">
                ({percentage}% of income)
                <span className="sr-only">
                  {status === 'positive'
                    ? `${percentage} percent of income is available`
                    : `${percentage} percent over allocated`}
                </span>
              </span>
            </div>
            <p className={`m-0 text-sm ${status === 'warning' ? 'text-destructive' : 'text-muted-foreground'}`}>
              {status === 'positive'
                ? 'Not assigned to any spending category'
                : 'Over-allocated - categories exceed income'}
            </p>
          </>
        )}
      </div>

      {hasActions && (
        <div className="flex gap-3 mt-4 flex-wrap">
          {onCreateSavingsGoal && (
            <button
              type="button"
              className="px-4 py-2 text-sm font-medium rounded-md cursor-pointer transition-colors duration-200 bg-primary text-primary-foreground border border-primary hover:bg-primary/90"
              onClick={onCreateSavingsGoal}
            >
              Create Savings Goal
            </button>
          )}
          {onAllocateToCategory && (
            <button
              type="button"
              className="px-4 py-2 text-sm font-medium rounded-md cursor-pointer transition-colors duration-200 bg-transparent text-primary border border-primary hover:bg-primary/10"
              onClick={onAllocateToCategory}
            >
              Allocate to Category
            </button>
          )}
        </div>
      )}
    </section>
  );
}
