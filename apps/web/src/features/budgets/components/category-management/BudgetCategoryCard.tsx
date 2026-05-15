/**
 * BudgetCategoryCard
 * Issue #497: Budget Category Manager + Match Config UI
 * Issue #499: Transaction-Budget Linking UI - Collapsible transactions
 *
 * Displays a single budget category with its allocation, spending, and match configuration.
 * Supports edit and delete actions.
 * Optionally shows collapsible transaction list when budgetId is provided.
 */

import { useState, useMemo } from 'react';
import { PencilIcon, TrashIcon, ChevronDownIcon, ChevronUpIcon } from 'lucide-react';
import { cn } from '@/lib/utils';
import type { BudgetCategory } from '../../types';
import { formatBudgetCategoryLabel } from '../../types';
import { MATCH_LOGIC_LABELS } from '@boostedbudget/shared-types/matching';
import { BudgetProgressBar } from '../BudgetProgressBar';
import { BudgetTransactionList } from '../transaction-link/BudgetTransactionList';

export interface BudgetCategoryCardProps {
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

export function BudgetCategoryCard({
  category,
  budgetId,
  onEdit,
  onDelete,
  disabled = false,
}: BudgetCategoryCardProps) {
  const [isTransactionsExpanded, setIsTransactionsExpanded] = useState(false);

  const categoryPath = useMemo(
    () => formatBudgetCategoryLabel(category),
    [category]
  );

  const isOverBudget = category.spent_amount > category.allocated_amount;
  const hasTags = category.tags && category.tags.length > 0;
  const matchLogicLabel = MATCH_LOGIC_LABELS[category.match_logic];
  const showTransactionsToggle = !!budgetId;

  return (
    <article
      className={cn(
        'budget-category-card',
        'bg-card border border-border rounded-lg p-4 transition-shadow duration-200 hover:shadow-sm',
        isOverBudget && 'budget-category-card--over-budget border-destructive bg-destructive/[0.02]'
      )}
    >
      {/* Header */}
      <div className="budget-category-card__header flex justify-between items-start gap-2 mb-3">
        <h3 className="budget-category-card__title text-[0.9375rem] font-semibold text-foreground m-0 leading-snug">
          {categoryPath}
        </h3>
        <span className="budget-category-card__badge text-[0.6875rem] font-medium text-primary bg-primary/10 py-0.5 px-2 rounded-full whitespace-nowrap shrink-0">
          {matchLogicLabel}
        </span>
      </div>

      {/* Amount Info */}
      <div className="budget-category-card__amounts flex flex-col gap-1 mb-3">
        <div className="budget-category-card__amount-row flex justify-between items-center">
          <span className="budget-category-card__label text-[0.8125rem] text-muted-foreground">Allocated:</span>
          <span className="budget-category-card__value text-sm font-semibold text-foreground">
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
          >
            {formatCurrency(category.spent_amount)}
          </span>
        </div>
      </div>

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
              >
                {tag}
              </span>
            ))}
          </div>
        </div>
      )}

      {/* Actions */}
      {(onEdit || onDelete || showTransactionsToggle) && (
        <div className="budget-category-card__actions flex gap-2 pt-3 border-t border-border">
          {onEdit && (
            <button
              type="button"
              className="budget-category-card__action-btn inline-flex items-center gap-1 px-2.5 py-1.5 text-[0.8125rem] font-medium text-muted-foreground bg-transparent border border-border rounded-md cursor-pointer transition-all duration-150 hover:enabled:bg-muted hover:enabled:text-foreground disabled:opacity-50 disabled:cursor-not-allowed"
              onClick={onEdit}
              disabled={disabled}
              aria-label={`Edit ${categoryPath}`}
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
              aria-label={`Delete ${categoryPath}`}
            >
              <TrashIcon className="budget-category-card__action-icon w-3.5 h-3.5" />
              Delete
            </button>
          )}
          {showTransactionsToggle && (
            <button
              type="button"
              className="budget-category-card__action-btn budget-category-card__action-btn--transactions ml-auto inline-flex items-center gap-1 px-2.5 py-1.5 text-[0.8125rem] font-medium text-muted-foreground bg-transparent border border-border rounded-md cursor-pointer transition-all duration-150 hover:enabled:bg-primary/5 hover:enabled:border-primary hover:enabled:text-primary disabled:opacity-50 disabled:cursor-not-allowed"
              onClick={() => setIsTransactionsExpanded(!isTransactionsExpanded)}
              aria-expanded={isTransactionsExpanded}
              aria-label={isTransactionsExpanded ? `Hide transactions for ${categoryPath}` : `View transactions for ${categoryPath}`}
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
      {showTransactionsToggle && isTransactionsExpanded && budgetId && (
        <div className="budget-category-card__transactions mt-3 pt-3 border-t border-border">
          <BudgetTransactionList budgetId={budgetId} categoryId={category.id} />
        </div>
      )}
    </article>
  );
}
