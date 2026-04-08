/**
 * AllocationBreakdown
 * Issue #497: Budget Category Manager + Match Config UI
 *
 * Visual breakdown of budget allocations with segmented progress bar and legend.
 */

import { useMemo } from 'react';
import type { BudgetCategory } from '../../types';
import { formatCategoryPath } from '../../types';
import './AllocationBreakdown.css';

export interface AllocationBreakdownProps {
  /** Budget categories with their allocations */
  categories: BudgetCategory[];
  /** Total income for the budget */
  totalIncome: number;
  /** Unallocated funds (totalIncome - sum of allocations) */
  unallocated: number;
}

/**
 * Formats a currency amount for display
 */
function formatCurrency(amount: number): string {
  return new Intl.NumberFormat('en-US', {
    style: 'currency',
    currency: 'USD',
  }).format(amount);
}

/**
 * Generates a color based on index for category segments
 */
function getCategoryColor(index: number): string {
  const colors = [
    '#3b82f6', // blue
    '#22c55e', // green
    '#f59e0b', // amber
    '#8b5cf6', // violet
    '#ec4899', // pink
    '#14b8a6', // teal
    '#f97316', // orange
    '#06b6d4', // cyan
  ];
  return colors[index % colors.length];
}

export function AllocationBreakdown({
  categories,
  totalIncome,
  unallocated,
}: AllocationBreakdownProps) {
  // Calculate totals
  const { totalAllocated, percentAllocated, isOverAllocated } = useMemo(() => {
    const total = categories.reduce((sum, cat) => sum + cat.allocated_amount, 0);
    const percent = totalIncome > 0 ? Math.round((total / totalIncome) * 100) : 0;
    return {
      totalAllocated: total,
      percentAllocated: percent,
      isOverAllocated: total > totalIncome,
    };
  }, [categories, totalIncome]);

  // Generate segments for the bar
  const segments = useMemo(() => {
    if (totalIncome <= 0) return [];

    const result: Array<{
      id: string;
      name: string;
      amount: number;
      percent: number;
      color: string;
      isUnallocated?: boolean;
    }> = [];

    // Add category segments
    categories.forEach((cat, index) => {
      const percent = (cat.allocated_amount / totalIncome) * 100;
      result.push({
        id: cat.id,
        name: formatCategoryPath(cat.category),
        amount: cat.allocated_amount,
        percent: Math.min(percent, 100), // Cap at 100%
        color: getCategoryColor(index),
      });
    });

    // Add unallocated segment if there's unallocated funds
    if (unallocated > 0 && !isOverAllocated) {
      const percent = (unallocated / totalIncome) * 100;
      result.push({
        id: 'unallocated',
        name: 'Unallocated',
        amount: unallocated,
        percent,
        color: 'var(--color-border)', // adapts to dark mode
        isUnallocated: true,
      });
    }

    return result;
  }, [categories, totalIncome, unallocated, isOverAllocated]);

  return (
    <div className="allocation-breakdown">
      {/* Screen reader live region (Phase 4 Accessibility) */}
      <div
        role="status"
        aria-live="polite"
        aria-atomic="true"
        className="sr-only"
      >
        {`${percentAllocated}% of budget allocated. ${
          isOverAllocated
            ? `Over-allocated by ${formatCurrency(Math.abs(unallocated))}`
            : `${formatCurrency(unallocated)} remaining`
        }`}
      </div>

      {/* Header with summary */}
      <div className="allocation-breakdown__header">
        <span className="allocation-breakdown__label">Allocation Overview</span>
        <span className="allocation-breakdown__percentage">
          {percentAllocated}% Allocated
        </span>
      </div>

      {/* Over-allocation warning */}
      {isOverAllocated && (
        <div className="allocation-breakdown__warning">
          Over-allocated by {formatCurrency(Math.abs(unallocated))}
        </div>
      )}

      {/* Segmented progress bar */}
      <div
        className="allocation-breakdown__bar"
        role="img"
        aria-label="Budget allocation breakdown"
      >
        {segments.map((segment) => (
          <div
            key={segment.id}
            data-testid={`segment-${segment.id}`}
            className={`allocation-breakdown__segment ${
              segment.isUnallocated ? 'allocation-breakdown__segment--unallocated' : ''
            }`}
            style={{
              width: `${segment.percent}%`,
              backgroundColor: segment.color,
            }}
            aria-label={`${segment.name}: ${formatCurrency(segment.amount)} (${Math.round(segment.percent)}%)`}
          />
        ))}
        {segments.length === 0 && totalIncome === 0 && (
          <div
            data-testid="segment-unallocated"
            className="allocation-breakdown__segment allocation-breakdown__segment--empty"
            style={{ width: '100%', backgroundColor: 'var(--color-border)' }}
            aria-label="No income set"
          />
        )}
      </div>

      {/* Legend */}
      <div className="allocation-breakdown__legend">
        {categories.map((cat, index) => (
          <div key={cat.id} className="allocation-breakdown__legend-item">
            <span
              className="allocation-breakdown__legend-dot"
              style={{ backgroundColor: getCategoryColor(index) }}
            />
            <span className="allocation-breakdown__legend-name">
              {formatCategoryPath(cat.category)}
            </span>
            <span className="allocation-breakdown__legend-amount">
              {formatCurrency(cat.allocated_amount)}
            </span>
          </div>
        ))}
        {unallocated > 0 && !isOverAllocated && (
          <div className="allocation-breakdown__legend-item">
            <span
              className="allocation-breakdown__legend-dot"
              style={{ backgroundColor: 'var(--color-border)' }}
            />
            <span className="allocation-breakdown__legend-name">Unallocated</span>
            <span className="allocation-breakdown__legend-amount">
              {formatCurrency(unallocated)}
            </span>
          </div>
        )}
      </div>

      {/* Total summary (Phase 5: Updated terminology and display) */}
      <div className="allocation-breakdown__total">
        <div className="allocation-breakdown__summary">
          <span className="allocation-breakdown__total-label">
            Total Budget: {formatCurrency(totalIncome)}
          </span>
          <div className="allocation-breakdown__stats">
            <span>Allocated: {formatCurrency(totalAllocated)} ({percentAllocated}%)</span>
            <span className="allocation-breakdown__divider">|</span>
            <span className={isOverAllocated ? 'allocation-breakdown__over' : ''}>
              {isOverAllocated
                ? `Over-allocated: ${formatCurrency(Math.abs(unallocated))}`
                : `Unallocated: ${formatCurrency(unallocated)} (${100 - percentAllocated}%)`
              }
            </span>
          </div>
        </div>
        {/* Keep legacy for backwards compatibility */}
        <span className="sr-only">Total Income: {formatCurrency(totalIncome)}</span>
      </div>
    </div>
  );
}
