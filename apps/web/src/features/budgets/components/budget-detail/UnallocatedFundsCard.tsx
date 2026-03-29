/**
 * UnallocatedFundsCard Component
 * Issue #498: Budget Detail Page for Envelope Budgeting
 *
 * Displays unallocated funds with action buttons for creating
 * savings goals or allocating to categories.
 *
 * TDD GREEN: Implementation to pass all tests.
 */

import './UnallocatedFundsCard.css';

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

  return (
    <section
      className={`unallocated-card unallocated-card--${status}`}
      role="region"
      aria-labelledby="unallocated-card-title"
    >
      <div className="unallocated-card__header">
        <svg
          data-testid="unallocated-icon"
          className="unallocated-card__icon"
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
        <h3 id="unallocated-card-title" className="unallocated-card__title">
          Unallocated Funds
        </h3>
      </div>

      <div className="unallocated-card__content">
        {status === 'neutral' ? (
          <div className="unallocated-card__status">
            <svg
              data-testid="allocation-complete-icon"
              className="unallocated-card__status-icon"
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
            <div className="unallocated-card__amount-row">
              <span className="unallocated-card__amount">
                {formatCurrency(displayAmount, currencyCode)}
              </span>
              <span className="unallocated-card__percentage">
                ({percentage}% of income)
                <span className="sr-only">
                  {status === 'positive'
                    ? `${percentage} percent of income is available`
                    : `${percentage} percent over allocated`}
                </span>
              </span>
            </div>
            <p className="unallocated-card__description">
              {status === 'positive'
                ? 'Not assigned to any spending category'
                : 'Over-allocated - categories exceed income'}
            </p>
          </>
        )}
      </div>

      {hasActions && (
        <div className="unallocated-card__actions">
          {onCreateSavingsGoal && (
            <button
              type="button"
              className="unallocated-card__btn unallocated-card__btn--primary"
              onClick={onCreateSavingsGoal}
            >
              Create Savings Goal
            </button>
          )}
          {onAllocateToCategory && (
            <button
              type="button"
              className="unallocated-card__btn unallocated-card__btn--secondary"
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
