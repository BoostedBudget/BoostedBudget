/**
 * BudgetCategoryCard
 * Displays a single budget category with its allocation, spending, and match configuration.
 * Supports edit and delete actions.
 * Optionally shows collapsible transaction list when budgetId is provided.
 */

import { useState, useMemo } from 'react';
import { PencilIcon, TrashIcon, ChevronDownIcon, ChevronUpIcon, AlertTriangleIcon } from 'lucide-react';
import { cva, type VariantProps } from 'class-variance-authority';
import { cn } from '@/lib/utils';
import type { BudgetCategory } from '../../types';
import {
  formatBudgetCategoryLabel,
  computeParentAllocationRemainder,
  computeParentDirectSpend,
} from '../../types';
import { MATCH_LOGIC_LABELS } from '@boostedbudget/shared-types/matching';
import { BudgetProgressBar } from '../BudgetProgressBar';
import { BudgetTransactionList } from '../transaction-link/BudgetTransactionList';

/**
 * CVA variants for the card wrapper — controls visual hierarchy:
 * - standalone: default full card (custom categories, primary-only with no children)
 * - parent: primary-only PFC with children below (removes bottom rounding/border to connect)
 * - child: indented sub-budget (left indent, top-none rounding, left accent border)
 * - savings: savings bucket — emerald left accent to distinguish from spending categories (#1300)
 */
const budgetCategoryCardVariants = cva(
  'bg-card border border-border transition-shadow duration-200 hover:shadow-sm',
  {
    variants: {
      variant: {
        standalone: 'rounded-lg p-4',
        parent: 'rounded-lg rounded-b-none border-b-0 p-4',
        child: 'rounded-lg rounded-t-none border-t-border/50 border-l-2 border-l-primary/20 ml-6 pl-4 pr-4 pt-3 pb-3',
        savings: 'rounded-lg border-l-2 border-l-emerald-500 p-4',
      },
    },
    defaultVariants: { variant: 'standalone' },
  }
);

export interface BudgetCategoryCardProps extends VariantProps<typeof budgetCategoryCardVariants> {
  /** The budget category to display */
  category: BudgetCategory;
  /** Budget ID for fetching transactions (enables collapsible transactions when provided) */
  budgetId?: string;
  /** Callback when edit button is clicked */
  onEdit?: () => void;
  /** Callback when delete button is clicked */
  onDelete?: () => void;
  /** Whether actions are disabled */
  disabled?: boolean;
  /**
   * Visual hierarchy variant:
   * - 'standalone': default full card (custom categories, primary-only with no children)
   * - 'parent': primary-only PFC with children below (connects visually to children)
   * - 'child': indented sub-budget (left accent border, strips primary prefix from label)
   * - 'savings': savings bucket card with emerald left accent (#1300)
   */
  variant?: 'standalone' | 'parent' | 'child' | 'savings';
  /**
   * Human-readable name for custom categories (resolved from useGetUserCategories by the parent).
   * Required for category_kind='custom' to avoid showing a raw UUID as the label.
   */
  customCategoryName?: string;
  /**
   * For the 'child' variant: the parent category's display name. When provided, the
   * earmark indicator reads "Earmarked from {parentLabel}" so users can see the
   * sub-allocation belongs to its parent (#1203).
   */
  parentLabel?: string;
  /**
   * For the 'parent' variant: this parent's own hierarchy children (#1630 Task 4) — named
   * `childCategories`, not `children`, to avoid colliding with React's JSX-content prop. Used to
   * compute the parent's own unallocated remainder (allocated_amount minus children's) and its
   * own direct spending (spent_amount minus children's contained sub-allocation spend) — both
   * distinct from simply displaying category.allocated_amount/spent_amount as-is. Ignored for
   * every other variant. Pass every hierarchy child rendered under this card; a child whose
   * match_logic escapes containment is excluded automatically by computeParent* (types.ts).
   */
  childCategories?: BudgetCategory[];
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
 * For child variant: strips the primary category prefix from the display label.
 * E.g. "Food & Drink › Restaurants" → "Restaurants"
 * Uses › or > separators. Falls back to full label if no separator found.
 */
function stripPrimaryPrefix(label: string): string {
  // Try › (actual separator used by formatBudgetCategoryLabel / PFC display values)
  const guillemet = label.indexOf(' › ');
  if (guillemet !== -1) {
    return label.slice(guillemet + 3);
  }
  // Fallback: > (legacy separator)
  const arrow = label.indexOf(' > ');
  if (arrow !== -1) {
    return label.slice(arrow + 3);
  }
  return label;
}

export function BudgetCategoryCard({
  category,
  budgetId,
  onEdit,
  onDelete,
  disabled = false,
  variant = 'standalone',
  customCategoryName,
  parentLabel,
  childCategories,
}: BudgetCategoryCardProps) {
  const [isTransactionsExpanded, setIsTransactionsExpanded] = useState(false);

  const fullLabel = useMemo(() => {
    if (category.category_kind === 'custom' && customCategoryName) {
      return customCategoryName;
    }
    return formatBudgetCategoryLabel(category);
  }, [category, customCategoryName]);

  const categoryPath = variant === 'child' ? stripPrimaryPrefix(fullLabel) : fullLabel;

  const isOverBudget = category.spent_amount > category.allocated_amount;
  const hasTags = category.tags && category.tags.length > 0;
  const matchLogicLabel = MATCH_LOGIC_LABELS[category.match_logic];

  // Issue #1630 Task 4: the parent's own unallocated remainder and its own direct spending,
  // distinct from its children's. Allocation is subtractive (typed numbers); spend is NOT — see
  // computeParentDirectSpend's doc comment for why subtracting only contained sub-allocation
  // children is safe here despite spend not generally being subtractive.
  const isParent = variant === 'parent';
  const children = childCategories ?? [];
  // Cheap, small-array arithmetic — plain computation each render rather than useMemo, since
  // computeParentAllocationRemainder/computeParentDirectSpend are O(children.length) and children
  // lists are always small (a handful of sub-allocations per parent).
  const allocationRemainder = isParent ? computeParentAllocationRemainder(category, children) : 0;
  const directSpend = isParent ? computeParentDirectSpend(category, children) : 0;
  // Over-allocation WARNS, never blocks — the write already succeeded (#1630 Task 4). A negative
  // remainder ("$600 of children under a $500 parent") is a legal, persistable state.
  const isOverAllocated = isParent && allocationRemainder < 0;

  return (
    <article
      className={cn(
        'budget-category-card',
        budgetCategoryCardVariants({ variant }),
        isOverBudget && 'budget-category-card--over-budget border-destructive bg-destructive/[0.02]'
      )}
    >
      {/* Header */}
      <div className="budget-category-card__header flex justify-between items-start gap-2 mb-3">
        <h3 className="budget-category-card__title text-[0.9375rem] font-semibold text-foreground m-0 leading-snug">
          <span data-ph-mask>{categoryPath}</span>
          {variant === 'child' && (
            <span className="budget-category-card__earmark mt-1 block text-[0.6875rem] font-medium uppercase tracking-wide text-muted-foreground">
              {parentLabel ? `Earmarked from ${parentLabel}` : 'Earmarked'}
            </span>
          )}
        </h3>
        <span className={cn(
          'budget-category-card__badge text-[0.6875rem] font-medium py-0.5 px-2 rounded-full whitespace-nowrap shrink-0',
          category.category_kind === 'savings'
            ? 'bg-success/10 text-success'
            : 'text-primary bg-primary/10'
        )}>
          {category.category_kind === 'savings' ? 'Savings Bucket' : matchLogicLabel}
        </span>
      </div>

      {/* Amount Info */}
      <div className="budget-category-card__amounts flex flex-col gap-1 mb-3">
        <div className="budget-category-card__amount-row flex justify-between items-center">
          <span className="budget-category-card__label text-[0.8125rem] text-muted-foreground">Allocated:</span>
          <span className="budget-category-card__value text-sm font-semibold text-foreground" data-ph-mask>
            {formatCurrency(category.allocated_amount)}
          </span>
        </div>
        <div className="budget-category-card__amount-row flex justify-between items-center">
          <span className="budget-category-card__label text-[0.8125rem] text-muted-foreground">Spent:</span>
          <span
            className={cn(
              'budget-category-card__value text-sm font-semibold',
              isOverBudget ? 'budget-category-card__value--over text-destructive' : 'text-foreground'
            )}
            data-ph-mask
          >
            {formatCurrency(category.spent_amount)}
          </span>
        </div>
      </div>

      {/*
        Parent's own remainder + direct spending (#1630 Task 4) — distinct from its children's.
        Allocation remainder = allocated_amount - sub-allocation children's allocated_amount
        (subtraction is valid: these are numbers the user typed). Direct spending = spent_amount
        - sub-allocation children's spent_amount (safe ONLY because computeParentDirectSpend
        restricts the subtraction to children the Task 0.5 containment rule guarantees are a
        subset of the parent's own matched spend — see that function's doc comment).

        NEGATIVE remainder ("$600 of children under a $500 parent") is a legal, persistable
        state — over-allocation warns, never blocks. It renders here as a negative currency value
        in destructive styling plus an explicit warning line, rather than clamping to $0, so the
        user can see the true over-commitment. The progress bar below is UNCHANGED by any of
        this: it still plots category.spent_amount (rolled, inclusive of contained children)
        against category.allocated_amount — the "rolled spend ÷ inclusive allocation" figure —
        so it can legitimately disagree with an over-allocated remainder here (e.g. a parent
        under its own rolled-spend budget while its children are allocated past its total) or
        with a child's own bar below. Both are true at once and are not reconciled into one
        number; they answer different questions (allocation commitment vs. actual spend).
      */}
      {isParent && children.length > 0 && (
        <div className="budget-category-card__parent-summary flex flex-col gap-1 mb-3 pt-3 border-t border-border">
          <div className="budget-category-card__amount-row flex justify-between items-center">
            <span className="budget-category-card__label text-[0.8125rem] text-muted-foreground">
              Remaining after children:
            </span>
            <span
              className={cn(
                'budget-category-card__value text-sm font-semibold',
                isOverAllocated ? 'text-destructive' : 'text-foreground'
              )}
              data-ph-mask
            >
              {formatCurrency(allocationRemainder)}
            </span>
          </div>
          <div className="budget-category-card__amount-row flex justify-between items-center">
            <span className="budget-category-card__label text-[0.8125rem] text-muted-foreground">
              Direct spending:
            </span>
            <span className="budget-category-card__value text-sm font-semibold text-foreground" data-ph-mask>
              {formatCurrency(directSpend)}
            </span>
          </div>
          {isOverAllocated && (
            <p
              className="budget-category-card__over-allocated-warning flex items-start gap-1.5 mt-1 text-[0.8125rem] text-destructive"
              role="status"
            >
              <AlertTriangleIcon className="w-3.5 h-3.5 shrink-0 mt-0.5" aria-hidden="true" />
              <span>
                Children allocate{' '}
                <span data-ph-mask>{formatCurrency(Math.abs(allocationRemainder))}</span> more
                than this category&apos;s own{' '}
                <span data-ph-mask>{formatCurrency(category.allocated_amount)}</span> budget. This
                still saves — you can leave it as an intentional over-earmark.
              </span>
            </p>
          )}
        </div>
      )}

      {/* Progress Bar */}
      <div className="budget-category-card__progress mb-3">
        <BudgetProgressBar
          spent={category.spent_amount}
          total={category.allocated_amount}
          size="small"
        />
      </div>

      {/* Tags */}
      {hasTags && (
        <div className="budget-category-card__tags flex flex-wrap items-center gap-1.5 mb-3">
          <span className="budget-category-card__tags-label text-xs text-muted-foreground mr-1">Tags:</span>
          <div className="budget-category-card__tags-list flex flex-wrap gap-1.5">
            {category.tags.map((tag) => (
              <span
                key={tag}
                className="budget-category-card__tag text-[0.6875rem] text-muted-foreground bg-muted py-0.5 px-1.5 rounded"
                data-ph-mask
              >
                {tag}
              </span>
            ))}
          </div>
        </div>
      )}

      {/* Actions */}
      {(onEdit || onDelete || !!budgetId) && (
        <div className="budget-category-card__actions flex gap-2 pt-3 border-t border-border">
          {onEdit && (
            <button
              type="button"
              className="budget-category-card__action-btn inline-flex items-center gap-1 px-2.5 py-1.5 text-[0.8125rem] font-medium text-muted-foreground bg-transparent border border-border rounded-md cursor-pointer transition-all duration-150 hover:enabled:bg-muted hover:enabled:text-foreground disabled:opacity-50 disabled:cursor-not-allowed"
              onClick={onEdit}
              disabled={disabled}
              aria-label="Edit category"
            >
              <PencilIcon className="budget-category-card__action-icon w-3.5 h-3.5" />
              Edit
            </button>
          )}
          {onDelete && (
            <button
              type="button"
              className="budget-category-card__action-btn budget-category-card__action-btn--delete inline-flex items-center gap-1 px-2.5 py-1.5 text-[0.8125rem] font-medium text-muted-foreground bg-transparent border border-border rounded-md cursor-pointer transition-all duration-150 hover:enabled:bg-destructive/5 hover:enabled:border-destructive hover:enabled:text-destructive disabled:opacity-50 disabled:cursor-not-allowed"
              onClick={onDelete}
              disabled={disabled}
              aria-label="Delete category"
            >
              <TrashIcon className="budget-category-card__action-icon w-3.5 h-3.5" />
              Delete
            </button>
          )}
          {budgetId && (
            <button
              type="button"
              className="budget-category-card__action-btn budget-category-card__action-btn--transactions ml-auto inline-flex items-center gap-1 px-2.5 py-1.5 text-[0.8125rem] font-medium text-muted-foreground bg-transparent border border-border rounded-md cursor-pointer transition-all duration-150 hover:enabled:bg-primary/5 hover:enabled:border-primary hover:enabled:text-primary disabled:opacity-50 disabled:cursor-not-allowed"
              onClick={() => setIsTransactionsExpanded(!isTransactionsExpanded)}
              aria-expanded={isTransactionsExpanded}
              aria-label={isTransactionsExpanded ? 'Hide transactions' : 'View transactions'}
            >
              {isTransactionsExpanded ? (
                <>
                  <ChevronUpIcon className="budget-category-card__action-icon w-3.5 h-3.5" />
                  Hide Transactions
                </>
              ) : (
                <>
                  <ChevronDownIcon className="budget-category-card__action-icon w-3.5 h-3.5" />
                  View Transactions
                </>
              )}
            </button>
          )}
        </div>
      )}

      {/* Collapsible Transactions List */}
      {budgetId && isTransactionsExpanded && (
        <div className="budget-category-card__transactions mt-3 pt-3 border-t border-border">
          <BudgetTransactionList budgetId={budgetId} categoryId={category.id} />
        </div>
      )}
    </article>
  );
}
