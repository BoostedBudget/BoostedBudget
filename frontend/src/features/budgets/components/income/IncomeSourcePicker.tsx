/**
 * IncomeSourcePicker
 * Issue #496: Income Source Configuration UI for Envelope Budgeting
 *
 * Allows adding new income sources with type-specific pickers:
 * - plaid_category: Dropdown of Plaid categories
 * - tag: Dropdown of user tags
 * - manual: Amount input with optional description
 */

import { useState, useCallback, useMemo } from 'react';
import { PlusIcon } from 'lucide-react';
import type { IncomeSource, IncomeSourceType } from '../../types';
import type { UserTag } from '@/features/tags/types';
import { formatPlaidCategory } from '@/features/auto-categorization/utils/categoryUtils';
import './IncomeSourcePicker.css';

export interface IncomeSourcePickerProps {
  /** Available Plaid categories for selection */
  availableCategories: string[];
  /** Available user tags for selection */
  availableTags: UserTag[];
  /** Already configured sources (to filter from dropdowns) */
  existingSources: IncomeSource[];
  /** Callback when a new source is added */
  onAdd: (source: IncomeSource) => void;
  /** Whether the picker is disabled */
  disabled?: boolean;
}

const SOURCE_TYPE_OPTIONS: { value: IncomeSourceType; label: string }[] = [
  { value: 'plaid_category', label: 'Category' },
  { value: 'tag', label: 'Tag' },
  { value: 'manual', label: 'Manual' },
];

export function IncomeSourcePicker({
  availableCategories,
  availableTags,
  existingSources,
  onAdd,
  disabled = false,
}: IncomeSourcePickerProps) {
  const [sourceType, setSourceType] = useState<IncomeSourceType>('plaid_category');
  const [selectedCategory, setSelectedCategory] = useState('');
  const [selectedTag, setSelectedTag] = useState('');
  const [manualAmount, setManualAmount] = useState('');
  const [manualDescription, setManualDescription] = useState('');

  // Filter out already selected categories
  const availableCategoriesFiltered = useMemo(() => {
    const existingCategoryValues = existingSources
      .filter((s) => s.type === 'plaid_category')
      .map((s) => s.value);
    return availableCategories.filter((cat) => !existingCategoryValues.includes(cat));
  }, [availableCategories, existingSources]);

  // Filter out already selected tags
  const availableTagsFiltered = useMemo(() => {
    const existingTagValues = existingSources
      .filter((s) => s.type === 'tag')
      .map((s) => s.value);
    return availableTags.filter((tag) => !existingTagValues.includes(tag.name));
  }, [availableTags, existingSources]);

  // Check if add button should be enabled
  const isAddEnabled = useMemo(() => {
    if (disabled) return false;

    switch (sourceType) {
      case 'plaid_category':
        return selectedCategory !== '';
      case 'tag':
        return selectedTag !== '';
      case 'manual': {
        const amount = parseFloat(manualAmount);
        return !isNaN(amount) && amount > 0;
      }
    }
  }, [sourceType, selectedCategory, selectedTag, manualAmount, disabled]);

  // Handle type change
  const handleTypeChange = useCallback((e: React.ChangeEvent<HTMLSelectElement>) => {
    const newType = e.target.value as IncomeSourceType;
    setSourceType(newType);
    // Reset selections when type changes
    setSelectedCategory('');
    setSelectedTag('');
    setManualAmount('');
    setManualDescription('');
  }, []);

  // Handle add
  const handleAdd = useCallback(() => {
    if (!isAddEnabled) return;

    let source: IncomeSource;

    switch (sourceType) {
      case 'plaid_category':
        source = { type: 'plaid_category', value: selectedCategory };
        break;
      case 'tag':
        source = { type: 'tag', value: selectedTag };
        break;
      case 'manual':
        source = {
          type: 'manual',
          value: manualAmount,
          description: manualDescription.trim() || undefined,
        };
        break;
    }

    onAdd(source);

    // Reset selection
    setSelectedCategory('');
    setSelectedTag('');
    setManualAmount('');
    setManualDescription('');
  }, [sourceType, selectedCategory, selectedTag, manualAmount, manualDescription, isAddEnabled, onAdd]);

  return (
    <div className="income-source-picker">
      {/* Type Selector */}
      <div className="income-source-picker__field">
        <label htmlFor="source-type" className="income-source-picker__label">
          Source Type
        </label>
        <select
          id="source-type"
          className="income-source-picker__select"
          value={sourceType}
          onChange={handleTypeChange}
          disabled={disabled}
          aria-label="Source Type"
        >
          {SOURCE_TYPE_OPTIONS.map((option) => (
            <option key={option.value} value={option.value}>
              {option.label}
            </option>
          ))}
        </select>
      </div>

      {/* Category Picker */}
      {sourceType === 'plaid_category' && (
        <div className="income-source-picker__field">
          <label htmlFor="category-select" className="income-source-picker__label">
            Category
          </label>
          <select
            id="category-select"
            className="income-source-picker__select"
            value={selectedCategory}
            onChange={(e) => setSelectedCategory(e.target.value)}
            disabled={disabled}
            aria-label="Select Category"
          >
            <option value="">Select a category...</option>
            {availableCategoriesFiltered.map((cat) => (
              <option key={cat} value={cat}>
                {formatPlaidCategory(cat)}
              </option>
            ))}
          </select>
        </div>
      )}

      {/* Tag Picker */}
      {sourceType === 'tag' && (
        <div className="income-source-picker__field">
          <label htmlFor="tag-select" className="income-source-picker__label">
            Tag
          </label>
          <select
            id="tag-select"
            className="income-source-picker__select"
            value={selectedTag}
            onChange={(e) => setSelectedTag(e.target.value)}
            disabled={disabled}
            aria-label="Select Tag"
          >
            <option value="">Select a tag...</option>
            {availableTagsFiltered.map((tag) => (
              <option key={tag.id} value={tag.name}>
                {tag.name}
              </option>
            ))}
          </select>
        </div>
      )}

      {/* Manual Entry */}
      {sourceType === 'manual' && (
        <>
          <div className="income-source-picker__field">
            <label htmlFor="manual-amount" className="income-source-picker__label">
              Amount
            </label>
            <div className="income-source-picker__amount-wrapper">
              <span className="income-source-picker__currency">$</span>
              <input
                type="number"
                id="manual-amount"
                className="income-source-picker__input income-source-picker__input--amount"
                value={manualAmount}
                onChange={(e) => setManualAmount(e.target.value)}
                min="0"
                step="0.01"
                placeholder="0.00"
                disabled={disabled}
                aria-label="Amount"
              />
            </div>
          </div>
          <div className="income-source-picker__field">
            <label htmlFor="manual-description" className="income-source-picker__label">
              Description (optional)
            </label>
            <input
              type="text"
              id="manual-description"
              className="income-source-picker__input"
              value={manualDescription}
              onChange={(e) => setManualDescription(e.target.value)}
              placeholder="e.g., Monthly salary"
              disabled={disabled}
              aria-label="Description"
            />
          </div>
        </>
      )}

      {/* Add Button */}
      <button
        type="button"
        className="income-source-picker__add-button"
        onClick={handleAdd}
        disabled={!isAddEnabled}
        aria-label="Add income source"
      >
        <PlusIcon className="income-source-picker__add-icon" />
        Add
      </button>
    </div>
  );
}
