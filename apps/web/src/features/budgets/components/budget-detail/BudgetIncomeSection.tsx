/**
 * BudgetIncomeSection Component
 * Issue #498: Budget Detail Page for Envelope Budgeting
 *
 * Displays expected vs actual income with visual comparison,
 * progress bar, and expandable income sources list.
 *
 * TDD GREEN: Implementation to pass all tests.
 */

import { useState } from 'react';
import type { IncomeSource } from '../../types';

export interface BudgetIncomeSectionProps {
  /** Expected total income for the budget period */
  expectedIncome: number;
  /** Actual income received so far */
  actualIncome: number;
  /** Configured income sources */
  incomeSources: IncomeSource[];
  /** Currency code for formatting */
  currencyCode: string;
  /** Status of each income source by description */
  sourceStatuses?: Record<string, 'received' | 'partial' | 'pending'>;
  /** Actual amounts received for each source by description */
  sourceActuals?: Record<string, number>;
  /** Whether sources section should start expanded (default: true) */
  defaultExpanded?: boolean;
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
 * Format difference with sign
 */
function formatDifference(amount: number, currencyCode: string): string {
  const formatted = formatCurrency(Math.abs(amount), currencyCode);
  if (amount > 0) return `+${formatted}`;
  if (amount < 0) return `-${formatted}`;
  return formatted;
}

/**
 * Get source type display label
 */
function getSourceTypeLabel(type: IncomeSource['type']): string {
  switch (type) {
    case 'plaid_category':
      return 'Auto-tracked';
    case 'tag':
      return 'Tag:';
    case 'manual':
      return 'Manual';
    default:
      return type;
  }
}

/**
 * BudgetIncomeSection component showing expected vs actual income
 *
 * @example
 * ```tsx
 * <BudgetIncomeSection
 *   expectedIncome={4000}
 *   actualIncome={3750}
 *   incomeSources={incomeSources}
 *   currencyCode="USD"
 * />
 * ```
 */
export function BudgetIncomeSection({
  expectedIncome,
  actualIncome,
  incomeSources,
  currencyCode,
  sourceStatuses = {},
  sourceActuals = {},
  defaultExpanded = true,
}: BudgetIncomeSectionProps) {
  const [isExpanded, setIsExpanded] = useState(defaultExpanded);

  const difference = actualIncome - expectedIncome;
  const percentage = expectedIncome > 0
    ? Math.round((actualIncome / expectedIncome) * 100)
    : 0;
  const cappedPercentage = Math.min(percentage, 100);
  const progressWidth = Math.min(percentage, 100);

  const differenceType = difference > 0 ? 'surplus' : difference < 0 ? 'shortfall' : 'neutral';
  const hasSources = incomeSources.length > 0;

  const toggleExpanded = () => {
    setIsExpanded(!isExpanded);
  };

  return (
    <section
      className="bg-card border border-border rounded-xl p-6"
      role="region"
      aria-labelledby="income-section-title"
    >
      <div className="flex items-center gap-2 mb-4">
        <h3 id="income-section-title" className="m-0 text-sm font-semibold uppercase tracking-wide text-muted-foreground">
          Income
        </h3>
      </div>

      <div className="flex flex-wrap gap-6 mb-4">
        <div className="flex flex-col gap-1">
          <span className="text-xs font-medium uppercase tracking-wide text-muted-foreground">Expected</span>
          <span className="text-xl font-semibold text-foreground">
            {formatCurrency(expectedIncome, currencyCode)}
          </span>
        </div>

        <div className="flex flex-col gap-1">
          <span className="text-xs font-medium uppercase tracking-wide text-muted-foreground">Actual</span>
          <span className="text-xl font-semibold text-foreground">
            {formatCurrency(actualIncome, currencyCode)}
          </span>
        </div>

        <div className="flex flex-col gap-1">
          <span className="text-xs font-medium uppercase tracking-wide text-muted-foreground">Difference</span>
          <div className="flex items-center gap-1">
            <span className={`text-xl font-semibold ${
              differenceType === 'surplus' ? 'text-success' :
              differenceType === 'shortfall' ? 'text-destructive' :
              'text-muted-foreground'
            }`}>
              {formatDifference(difference, currencyCode)}
            </span>
            <span className="sr-only">
              {differenceType === 'surplus' && 'surplus'}
              {differenceType === 'shortfall' && 'shortfall'}
              {differenceType === 'neutral' && 'on target'}
            </span>
          </div>
        </div>
      </div>

      <div className="mb-5">
        <div className="flex justify-between items-center mb-2">
          <span className="text-sm text-muted-foreground">Income received</span>
          <span className="text-sm font-semibold text-foreground">{percentage}%</span>
        </div>
        <div className="h-2 bg-muted rounded-full overflow-hidden">
          <div
            className={`h-full rounded-full transition-[width] duration-300 ${
              percentage >= 100 ? 'bg-green-600' : 'bg-primary'
            }`}
            style={{ width: `${progressWidth}%` }}
            role="progressbar"
            aria-valuenow={cappedPercentage}
            aria-valuemin={0}
            aria-valuemax={100}
            aria-label="Income progress"
          />
        </div>
      </div>

      {hasSources && (
        <>
          <button
            type="button"
            className="flex items-center gap-2 py-2 w-full bg-none border-none border-t border-border cursor-pointer text-muted-foreground text-sm font-medium text-left transition-colors duration-200 hover:text-primary focus:outline-2 focus:outline-primary focus:outline-offset-2"
            onClick={toggleExpanded}
            aria-expanded={isExpanded}
            aria-controls="income-sources-list"
          >
            <svg
              className={`w-4 h-4 transition-transform duration-200 ${isExpanded ? 'rotate-90' : ''}`}
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="2"
              strokeLinecap="round"
              strokeLinejoin="round"
              aria-hidden="true"
            >
              <polyline points="9 18 15 12 9 6" />
            </svg>
            Income Sources
          </button>

          <div
            id="income-sources-list"
            className={`pt-3 ${!isExpanded ? 'hidden' : ''}`}
          >
            <ul className="list-none m-0 p-0 flex flex-col gap-3">
              {incomeSources.map((source, index) => {
                const sourceKey = source.description || `source-${index}`;
                const status = sourceStatuses[source.description || ''];
                const actualAmount = sourceActuals[source.description || ''];
                const expectedAmount = source.type === 'manual' ? parseFloat(source.value || '0') : 0;

                return (
                  <li key={sourceKey} className="flex justify-between items-center p-3 bg-muted rounded-lg">
                    <div className="flex flex-col gap-0.5">
                      <span className="text-sm font-medium text-foreground">
                        {source.description || getSourceTypeLabel(source.type)}
                      </span>
                      <span className="text-xs text-muted-foreground">
                        {source.type === 'plaid_category' && 'Auto-tracked'}
                        {source.type === 'tag' && `Tag: ${source.value}`}
                        {source.type === 'manual' && 'Manual'}
                      </span>
                    </div>
                    <div className="flex items-center gap-2">
                      {source.type === 'manual' && expectedAmount > 0 && (
                        <span className="text-sm text-muted-foreground">
                          {formatCurrency(expectedAmount, currencyCode)} expected
                        </span>
                      )}
                      {status === 'partial' && actualAmount !== undefined && (
                        <span className="text-sm font-medium text-foreground">
                          {formatCurrency(actualAmount, currencyCode)}
                        </span>
                      )}
                      {status === 'received' && (
                        <svg
                          data-testid="income-source-received"
                          className="w-5 h-5 text-success"
                          viewBox="0 0 24 24"
                          fill="none"
                          stroke="currentColor"
                          strokeWidth="2"
                          strokeLinecap="round"
                          strokeLinejoin="round"
                        >
                          <polyline points="20 6 9 17 4 12" />
                        </svg>
                      )}
                    </div>
                  </li>
                );
              })}
            </ul>
          </div>
        </>
      )}
    </section>
  );
}
