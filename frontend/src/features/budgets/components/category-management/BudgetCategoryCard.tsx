/**
 * BudgetCategoryCard
 * Issue #497: Budget Category Manager + Match Config UI
 *
 * Displays a single budget category with its allocation, spending, and match configuration.
 * Supports edit and delete actions.
 */

import { useMemo } from 'react';
import { PencilIcon, TrashIcon } from 'lucide-react';
import type { BudgetCategory } from '../../types';
import { formatCategoryPath } from '../../types';
import { MATCH_LOGIC_LABELS } from '@/shared/types/matching';
import { BudgetProgressBar } from '../BudgetProgressBar';
import './BudgetCategoryCard.css';

export interface BudgetCategoryCardProps {
  /** The budget category to display */
  category: BudgetCategory;
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
  onEdit,
  onDelete,
  disabled = false,
}: BudgetCategoryCardProps) {
  const categoryPath = useMemo(
    () => formatCategoryPath(category.category),
    [category.category]
  );

  const isOverBudget = category.spent_amount > category.allocated_amount;
  const hasTags = category.tags && category.tags.length > 0;
  const matchLogicLabel = MATCH_LOGIC_LABELS[category.match_logic];

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
      {(onEdit || onDelete) && (
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
        </div>
      )}
    </article>
  );
}
