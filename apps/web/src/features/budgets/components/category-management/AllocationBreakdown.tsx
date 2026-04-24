/**
 * AllocationBreakdown
 * Issue #497: Budget Category Manager + Match Config UI
 *
 * Visual breakdown of budget allocations with segmented progress bar and legend.
 */

import { useMemo } from 'react';
import { cn } from '@/lib/utils';
import type { BudgetCategory } from '../../types';
import { formatCategoryPath } from '../../types';

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
    <div className="bg-muted border border-border rounded-lg p-4 flex flex-col gap-3">
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
      <div className="flex justify-between items-center">
        <span className="text-sm font-semibold text-foreground">Allocation Overview</span>
        <span className="text-sm font-medium text-muted-foreground">
          {percentAllocated}% Allocated
        </span>
      </div>

      {/* Over-allocation warning — soft amber, non-blocking */}
      {isOverAllocated && (
        <div
          className="text-sm text-amber-600 dark:text-amber-400 flex items-center gap-1.5"
          aria-label="over-income warning"
        >
          <span aria-hidden="true">⚠️</span>
          Allocations exceed expected income by {formatCurrency(Math.abs(unallocated))}. You&apos;ll be dipping into savings or going over budget.
        </div>
      )}

      {/* Segmented progress bar */}
      <div
        className="flex h-4 rounded-full overflow-hidden bg-border"
        role="img"
        aria-label="Budget allocation breakdown"
      >
        {segments.map((segment) => (
          <div
            key={segment.id}
            data-testid={`segment-${segment.id}`}
            className={cn(
              'h-full min-w-[2px] transition-[width] duration-300',
              segment.isUnallocated && 'opacity-50'
            )}
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
            className="h-full min-w-[2px] opacity-30"
            style={{ width: '100%', backgroundColor: 'var(--color-border)' }}
            aria-label="No income set"
          />
        )}
      </div>

      {/* Legend */}
      <div className="flex flex-wrap gap-y-3 gap-x-6 mt-1">
        {categories.map((cat, index) => (
          <div key={cat.id} className="flex items-center gap-2 text-[0.8125rem]">
            <span
              className="w-3 h-3 rounded-full shrink-0"
              style={{ backgroundColor: getCategoryColor(index) }}
            />
            <span className="text-foreground whitespace-nowrap overflow-hidden text-ellipsis max-w-[150px]">
              {formatCategoryPath(cat.category)}
            </span>
            <span className="text-muted-foreground font-medium whitespace-nowrap">
              {formatCurrency(cat.allocated_amount)}
            </span>
          </div>
        ))}
        {unallocated > 0 && !isOverAllocated && (
          <div className="flex items-center gap-2 text-[0.8125rem]">
            <span
              className="w-3 h-3 rounded-full shrink-0"
              style={{ backgroundColor: 'var(--color-border)' }}
            />
            <span className="text-foreground whitespace-nowrap">Unallocated</span>
            <span className="text-muted-foreground font-medium whitespace-nowrap">
              {formatCurrency(unallocated)}
            </span>
          </div>
        )}
      </div>

      {/* Total summary (Phase 5: Updated terminology and display) */}
      <div className="text-[0.8125rem] text-muted-foreground pt-2 border-t border-border">
        <div className="allocation-breakdown__summary">
          <span className="allocation-breakdown__total-label">
            Total Budget: {formatCurrency(totalIncome)}
          </span>
          <div className="allocation-breakdown__stats">
            <span>Allocated: {formatCurrency(totalAllocated)} ({percentAllocated}%)</span>
            <span className="allocation-breakdown__divider">|</span>
            <span className={isOverAllocated ? 'allocation-breakdown__over text-amber-600 dark:text-amber-400' : ''}>
              {isOverAllocated
                ? `Over expected income: ${formatCurrency(Math.abs(unallocated))}`
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
