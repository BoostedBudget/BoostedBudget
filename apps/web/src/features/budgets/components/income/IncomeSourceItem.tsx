/**
 * IncomeSourceItem
 * Issue #496: Income Source Configuration UI for Envelope Budgeting
 *
 * Displays a single configured income source with type icon,
 * value display, optional description, and remove button.
 */

import { XIcon, TagIcon, DollarSignIcon, FolderIcon } from 'lucide-react';
import type { IncomeSource } from '../../types';
import { formatPlaidCategory } from '@/features/auto-categorization/utils/categoryUtils';
import './IncomeSourceItem.css';

export interface IncomeSourceItemProps {
  /** The income source to display */
  source: IncomeSource;
  /** Callback when remove button is clicked */
  onRemove: () => void;
  /** Whether the component is in disabled state */
  disabled?: boolean;
}

/**
 * Formats a currency amount for display
 */
function formatCurrency(value: string): string {
  const amount = parseFloat(value);
  if (isNaN(amount)) return '$0.00';
  return new Intl.NumberFormat('en-US', {
    style: 'currency',
    currency: 'USD',
  }).format(amount);
}

/**
 * Gets the display value based on source type
 */
function getDisplayValue(source: IncomeSource): string {
  switch (source.type) {
    case 'plaid_category':
      return formatPlaidCategory(source.value);
    case 'tag':
      return source.value;
    case 'manual':
      return formatCurrency(source.value);
  }
}

/**
 * Gets the icon component based on source type
 */
function getIcon(type: IncomeSource['type']): React.ReactNode {
  switch (type) {
    case 'plaid_category':
      return <FolderIcon className="income-source-item__icon" />;
    case 'tag':
      return <TagIcon className="income-source-item__icon" />;
    case 'manual':
      return <DollarSignIcon className="income-source-item__icon" />;
  }
}

/**
 * Gets the aria-label for the remove button
 */
function getRemoveAriaLabel(source: IncomeSource): string {
  const displayValue = getDisplayValue(source);
  return `Remove ${displayValue}`;
}

export function IncomeSourceItem({
  source,
  onRemove,
  disabled = false,
}: IncomeSourceItemProps) {
  const displayValue = getDisplayValue(source);
  const hasDescription = source.description && source.description.trim().length > 0;

  return (
    <div className="income-source-item">
      <div className="income-source-item__content">
        <span data-testid="income-source-icon" className="income-source-item__icon-wrapper">
          {getIcon(source.type)}
        </span>
        <div className="income-source-item__text">
          <span className="income-source-item__value">{displayValue}</span>
          {hasDescription && (
            <span
              className="income-source-item__description"
              data-testid="income-source-description"
            >
              {source.description}
            </span>
          )}
        </div>
      </div>
      <button
        type="button"
        className="income-source-item__remove"
        onClick={onRemove}
        disabled={disabled}
        aria-label={getRemoveAriaLabel(source)}
      >
        <XIcon className="income-source-item__remove-icon" />
      </button>
    </div>
  );
}
