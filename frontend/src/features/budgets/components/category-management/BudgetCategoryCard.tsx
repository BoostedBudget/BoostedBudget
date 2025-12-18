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
import type { BudgetCategory } from '../../types';
import { formatCategoryPath } from '../../types';
import { MATCH_LOGIC_LABELS } from '@/shared/types/matching';
import { BudgetProgressBar } from '../BudgetProgressBar';
import { BudgetTransactionList } from '../transaction-link/BudgetTransactionList';
import './BudgetCategoryCard.css';

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
    () => formatCategoryPath(category.category),
    [category.category]
  );

  const isOverBudget = category.spent_amount > category.allocated_amount;
  const hasTags = category.tags && category.tags.length > 0;
  const matchLogicLabel = MATCH_LOGIC_LABELS[category.match_logic];
  const showTransactionsToggle = !!budgetId;

  return (
    <article
      className={`budget-category-card ${isOverBudget ? 'budget-category-card--over-budget' : ''}`}
    >
      {/* Header */}
      <div className="budget-category-card__header">
        <h3 className="budget-category-card__title">{categoryPath}</h3>
        <span className="budget-category-card__badge">{matchLogicLabel}</span>
      </div>

      {/* Amount Info */}
      <div className="budget-category-card__amounts">
        <div className="budget-category-card__amount-row">
          <span className="budget-category-card__label">Allocated:</span>
          <span className="budget-category-card__value">
            {formatCurrency(category.allocated_amount)}
          </span>
        </div>
        <div className="budget-category-card__amount-row">
          <span className="budget-category-card__label">Spent:</span>
          <span
            className={`budget-category-card__value ${isOverBudget ? 'budget-category-card__value--over' : ''}`}
          >
            {formatCurrency(category.spent_amount)}
          </span>
        </div>
      </div>

      {/* Progress Bar */}
      <div className="budget-category-card__progress">
        <BudgetProgressBar
          spent={category.spent_amount}
          total={category.allocated_amount}
          size="small"
        />
      </div>

      {/* Tags */}
      {hasTags && (
        <div className="budget-category-card__tags">
          <span className="budget-category-card__tags-label">Tags:</span>
          <div className="budget-category-card__tags-list">
            {category.tags.map((tag) => (
              <span key={tag} className="budget-category-card__tag">
                {tag}
              </span>
            ))}
          </div>
        </div>
      )}

      {/* Actions */}
      {(onEdit || onDelete || showTransactionsToggle) && (
        <div className="budget-category-card__actions">
          {onEdit && (
            <button
              type="button"
              className="budget-category-card__action-btn"
              onClick={onEdit}
              disabled={disabled}
              aria-label={`Edit ${categoryPath}`}
            >
              <PencilIcon className="budget-category-card__action-icon" />
              Edit
            </button>
          )}
          {onDelete && (
            <button
              type="button"
              className="budget-category-card__action-btn budget-category-card__action-btn--delete"
              onClick={onDelete}
              disabled={disabled}
              aria-label={`Delete ${categoryPath}`}
            >
              <TrashIcon className="budget-category-card__action-icon" />
              Delete
            </button>
          )}
          {showTransactionsToggle && (
            <button
              type="button"
              className="budget-category-card__action-btn budget-category-card__action-btn--transactions"
              onClick={() => setIsTransactionsExpanded(!isTransactionsExpanded)}
              aria-expanded={isTransactionsExpanded}
              aria-label={isTransactionsExpanded ? `Hide transactions for ${categoryPath}` : `View transactions for ${categoryPath}`}
            >
              {isTransactionsExpanded ? (
                <>
                  <ChevronUpIcon className="budget-category-card__action-icon" />
                  Hide Transactions
                </>
              ) : (
                <>
                  <ChevronDownIcon className="budget-category-card__action-icon" />
                  View Transactions
                </>
              )}
            </button>
          )}
        </div>
      )}

      {/* Collapsible Transactions List */}
      {showTransactionsToggle && isTransactionsExpanded && budgetId && (
        <div className="budget-category-card__transactions">
          <BudgetTransactionList budgetId={budgetId} categoryId={category.id} />
        </div>
      )}
    </article>
  );
}
