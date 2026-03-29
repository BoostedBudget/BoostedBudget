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
import './BudgetIncomeSection.css';

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
      className="income-section"
      role="region"
      aria-labelledby="income-section-title"
    >
      <div className="income-section__header">
        <h3 id="income-section-title" className="income-section__title">
          Income
        </h3>
      </div>

      <div className="income-section__summary">
        <div className="income-section__stat">
          <span className="income-section__label">Expected</span>
          <span className="income-section__amount">
            {formatCurrency(expectedIncome, currencyCode)}
          </span>
        </div>

        <div className="income-section__stat">
          <span className="income-section__label">Actual</span>
          <span className="income-section__amount">
            {formatCurrency(actualIncome, currencyCode)}
          </span>
        </div>

        <div className={`income-section__stat income-section__difference income-section__difference--${differenceType}`}>
          <span className="income-section__label">Difference</span>
          <span className="income-section__amount">
            {formatDifference(difference, currencyCode)}
          </span>
          <span className="sr-only">
            {differenceType === 'surplus' && 'surplus'}
            {differenceType === 'shortfall' && 'shortfall'}
            {differenceType === 'neutral' && 'on target'}
          </span>
        </div>
      </div>

      <div className="income-section__progress-container">
        <div className="income-section__progress-header">
          <span className="income-section__progress-text">Income received</span>
          <span className="income-section__progress-percentage">{percentage}%</span>
        </div>
        <div className="income-section__progress-bar">
          <div
            className={`income-section__progress-fill ${
              percentage >= 100 ? 'income-section__progress-fill--complete' : ''
            } ${percentage > 100 ? 'income-section__progress-fill--over' : ''}`}
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
            className="income-section__sources-toggle"
            onClick={toggleExpanded}
            aria-expanded={isExpanded}
            aria-controls="income-sources-list"
          >
            <svg
              className={`income-section__toggle-icon ${
                isExpanded ? 'income-section__toggle-icon--expanded' : ''
              }`}
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
            className={`income-section__sources ${
              !isExpanded ? 'income-section__sources--collapsed' : ''
            }`}
          >
            <ul className="income-section__sources-list">
              {incomeSources.map((source, index) => {
                const sourceKey = source.description || `source-${index}`;
                const status = sourceStatuses[source.description || ''];
                const actualAmount = sourceActuals[source.description || ''];
                const expectedAmount = source.type === 'manual' ? parseFloat(source.value || '0') : 0;

                return (
                  <li key={sourceKey} className="income-section__source-item">
                    <div className="income-section__source-info">
                      <span className="income-section__source-name">
                        {source.description || getSourceTypeLabel(source.type)}
                      </span>
                      <span className="income-section__source-type">
                        {source.type === 'plaid_category' && 'Auto-tracked'}
                        {source.type === 'tag' && `Tag: ${source.value}`}
                        {source.type === 'manual' && 'Manual'}
                      </span>
                    </div>
                    <div className="income-section__source-status">
                      {source.type === 'manual' && expectedAmount > 0 && (
                        <span className="income-section__source-expected">
                          {formatCurrency(expectedAmount, currencyCode)} expected
                        </span>
                      )}
                      {status === 'partial' && actualAmount !== undefined && (
                        <span className="income-section__source-actual">
                          {formatCurrency(actualAmount, currencyCode)}
                        </span>
                      )}
                      {status === 'received' && (
                        <svg
                          data-testid="income-source-received"
                          className="income-section__source-icon income-section__source-icon--received"
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
