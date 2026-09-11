/**
 * AllocationBreakdown
 * Issue #497: Budget Category Manager + Match Config UI
 *
 * Visual breakdown of budget allocations with segmented progress bar and legend.
 */

import { useMemo } from 'react';
import { cn } from '@/lib/utils';
import type { BudgetCategory } from '../../types';
import { Alert } from '@/components/ui/alert';
import { formatBudgetCategoryLabel, filterTopLevel, isSubAllocationOfParent, formatCurrency } from '../../types';
import type { CustomCategoryMap } from '../../utils/groupBudgetCategoriesByPrimary';

export interface AllocationBreakdownProps {
  /** Budget categories with their allocations */
  categories: BudgetCategory[];
  /** Total income for the budget */
  totalIncome: number;
  /** Unallocated funds (totalIncome - sum of allocations) */
  unallocated: number;
  /**
   * Optional map from custom_category_id → { name } for resolving human-readable
   * names on custom budget categories. Without it, formatBudgetCategoryLabel is used
   * (which returns the raw UUID for custom categories).
   */
  customCategoryMap?: CustomCategoryMap;
}

/**
 * Resolves a human-readable label for a budget category.
 * For custom categories, uses customCategoryMap to avoid showing raw UUIDs.
 */
function resolveCategoryLabel(cat: BudgetCategory, customCategoryMap?: CustomCategoryMap): string {
  if (cat.category_kind === 'custom' && cat.custom_category_id) {
    const resolved = customCategoryMap?.get(cat.custom_category_id)?.name;
    return resolved ?? 'Custom category';
  }
  return formatBudgetCategoryLabel(cat);
}

/**
 * One nested sub-allocation segment, drawn inside its DIRECT parent's segment. Recursive: a
 * grandchild's own children (its great-grandchildren, etc.) nest inside IT in turn, each level
 * sized against its own immediate parent's allocation — see buildNestedSegments in
 * AllocationBreakdown for why this must stay direct-parent-relative rather than root-relative.
 */
interface NestedSegment {
  id: string;
  name: string;
  amount: number;
  percent: number;
  children: NestedSegment[];
}

/**
 * Renders one nested sub-allocation segment and recurses into its own children, so a
 * grandchild's segment is drawn inside its direct parent's div (not the top-level root's).
 */
function renderNestedSegment(segment: NestedSegment) {
  return (
    <div
      key={segment.id}
      data-testid={`segment-${segment.id}`}
      className="h-full min-w-[1px] border-l border-black/20 flex"
      style={{
        width: `${segment.percent}%`,
        backgroundColor: 'rgba(0, 0, 0, 0.25)',
      }}
      aria-label={`${Math.round(segment.percent)}% of parent allocation`}
    >
      {segment.children.map(renderNestedSegment)}
    </div>
  );
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
  customCategoryMap,
}: AllocationBreakdownProps) {
  // Top-level categories only (#1203/#1630) — a sub-allocation's amount is already inside
  // its parent's, so summing every row (including nested children) double-counts.
  const topLevel = useMemo(() => filterTopLevel(categories), [categories]);

  // Calculate totals
  const { totalAllocated, percentAllocated, isOverAllocated } = useMemo(() => {
    const total = topLevel.reduce((sum, cat) => sum + cat.allocated_amount, 0);
    const percent = totalIncome > 0 ? Math.round((total / totalIncome) * 100) : 0;
    return {
      totalAllocated: total,
      percentAllocated: percent,
      isOverAllocated: total > totalIncome,
    };
  }, [topLevel, totalIncome]);

  // Generate segments for the bar. The OUTER bar renders one segment per top-level
  // category, sized against totalIncome — this is the dedup'd total (#1630). Any category
  // that is a sub-allocation of that top-level row is rendered as a NESTED inner segment,
  // recursively, each one sized against its own DIRECT parent's allocation (not the root's) —
  // otherwise a grandchild in a chain X → C → G would be sized against X instead of its actual
  // direct parent C. `isSubAllocationOfParent` is called with `directOnly: true` at every level
  // of this recursion for exactly that reason: without it, G also matches transitively against
  // X (see custom_grandparent_pfc_primary in types.ts), which is the right relation for
  // "what rolls up into X's total" but the wrong one for "who is G nested directly under".
  const segments = useMemo(() => {
    if (totalIncome <= 0) return [];

    const nonTopLevel = categories.filter((cat) => !topLevel.includes(cat));

    const buildNestedSegments = (parent: BudgetCategory): NestedSegment[] =>
      nonTopLevel
        .filter((child) => isSubAllocationOfParent(child, parent, { directOnly: true }))
        .map((child) => ({
          id: child.id,
          name: resolveCategoryLabel(child, customCategoryMap),
          amount: child.allocated_amount,
          percent:
            parent.allocated_amount > 0
              ? Math.min((child.allocated_amount / parent.allocated_amount) * 100, 100)
              : 0,
          children: buildNestedSegments(child),
        }));

    const result: Array<{
      id: string;
      name: string;
      amount: number;
      percent: number;
      color: string;
      isUnallocated?: boolean;
      children: NestedSegment[];
    }> = [];

    // Add category segments (top-level only)
    topLevel.forEach((cat, index) => {
      const percent = (cat.allocated_amount / totalIncome) * 100;
      result.push({
        id: cat.id,
        name: resolveCategoryLabel(cat, customCategoryMap),
        amount: cat.allocated_amount,
        percent: Math.min(percent, 100), // Cap at 100%
        color: getCategoryColor(index),
        children: buildNestedSegments(cat),
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
        children: [],
      });
    }

    return result;
  }, [categories, topLevel, totalIncome, unallocated, isOverAllocated, customCategoryMap]);

  return (
    <div className="bg-muted border border-border rounded-lg p-4 flex flex-col gap-3">
      {/* Screen reader live region (Phase 4 Accessibility) */}
      <div
        role="status"
        aria-live="polite"
        aria-atomic="true"
        className="sr-only"
        data-ph-mask
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

      {/* Over-allocation warning — soft, non-blocking (live region is the sr-only div above) */}
      {isOverAllocated && (
        <Alert variant="warning">
          Allocations exceed expected income by <span data-ph-mask>{formatCurrency(Math.abs(unallocated))}</span>. You&apos;ll be dipping into savings or going over budget.
        </Alert>
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
              'h-full min-w-[2px] transition-[width] duration-300 flex',
              segment.isUnallocated && 'opacity-50'
            )}
            style={{
              width: `${segment.percent}%`,
              backgroundColor: segment.color,
            }}
            aria-label={`${Math.round(segment.percent)}% of budget`}
          >
            {/* Nested sub-allocation segments (#1630) — drawn inside their parent's
                segment rather than as siblings, so the outer bar's total width stays
                the dedup'd total while still surfacing every category. Recursive: a
                grandchild is drawn inside its DIRECT parent's segment, sized against
                that parent's own allocation, not the root's (see buildNestedSegments). */}
            {segment.children.map(renderNestedSegment)}
          </div>
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
            <span className="text-foreground whitespace-nowrap overflow-hidden text-ellipsis max-w-[150px]" data-ph-mask>
              {resolveCategoryLabel(cat, customCategoryMap)}
            </span>
            <span className="text-muted-foreground font-medium whitespace-nowrap" data-ph-mask>
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
            <span className="text-muted-foreground font-medium whitespace-nowrap" data-ph-mask>
              {formatCurrency(unallocated)}
            </span>
          </div>
        )}
      </div>

      {/* Total summary (Phase 5: Updated terminology and display) */}
      <div className="text-[0.8125rem] text-muted-foreground pt-2 border-t border-border">
        <div className="allocation-breakdown__summary">
          <span className="allocation-breakdown__total-label">
            Total Budget: <span data-ph-mask>{formatCurrency(totalIncome)}</span>
          </span>
          <div className="allocation-breakdown__stats">
            <span>Allocated: <span data-ph-mask>{formatCurrency(totalAllocated)}</span> ({percentAllocated}%)</span>
            <span className="allocation-breakdown__divider">|</span>
            <span className={isOverAllocated ? 'allocation-breakdown__over text-warning' : ''}>
              {isOverAllocated
                ? <>Over expected income: <span data-ph-mask>{formatCurrency(Math.abs(unallocated))}</span></>
                : <>Unallocated: <span data-ph-mask>{formatCurrency(unallocated)}</span> ({100 - percentAllocated}%)</>
              }
            </span>
          </div>
        </div>
        <span className="sr-only" data-ph-mask>Total Income: {formatCurrency(totalIncome)}</span>
      </div>
    </div>
  );
}
