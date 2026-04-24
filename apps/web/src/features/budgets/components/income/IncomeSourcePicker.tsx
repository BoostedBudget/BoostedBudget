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

// Note: 'manual' is intentionally excluded here — new budgets only support
// category and tag sources. The manual render path in IncomeSourceItem.tsx is
// preserved for existing budgets that already have manual sources in the DB.
const SOURCE_TYPE_OPTIONS: { value: IncomeSourceType; label: string }[] = [
  { value: 'plaid_category', label: 'Category' },
  { value: 'tag', label: 'Tag' },
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

  const selectCls =
    'px-3 py-2 text-sm bg-card border border-input rounded-md text-foreground cursor-pointer transition-colors duration-150 focus:outline-none focus:border-ring focus:ring-2 focus:ring-ring disabled:bg-muted disabled:cursor-not-allowed disabled:opacity-60';
  const inputCls =
    'px-3 py-2 text-sm bg-card border border-input rounded-md text-foreground transition-colors duration-150 focus:outline-none focus:border-ring focus:ring-2 focus:ring-ring disabled:bg-muted disabled:cursor-not-allowed disabled:opacity-60';

  return (
    <div className="flex flex-col gap-3 p-4 bg-muted border border-border rounded-lg">
      {/* Type Selector */}
      <div className="flex flex-col gap-1">
        <label htmlFor="source-type" className="text-xs font-medium text-muted-foreground">
          Source Type
        </label>
        <select
          id="source-type"
          className={selectCls}
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
        <div className="flex flex-col gap-1">
          <label htmlFor="category-select" className="text-xs font-medium text-muted-foreground">
            Category
          </label>
          <select
            id="category-select"
            className={selectCls}
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
        <div className="flex flex-col gap-1">
          <label htmlFor="tag-select" className="text-xs font-medium text-muted-foreground">
            Tag
          </label>
          <select
            id="tag-select"
            className={selectCls}
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
          <div className="flex flex-col gap-1">
            <label htmlFor="manual-amount" className="text-xs font-medium text-muted-foreground">
              Amount
            </label>
            <div className="relative flex items-center">
              <span className="absolute left-3 text-muted-foreground pointer-events-none">$</span>
              <input
                type="number"
                id="manual-amount"
                className={`${inputCls} pl-6 [appearance:textfield] [&::-webkit-outer-spin-button]:appearance-none [&::-webkit-inner-spin-button]:appearance-none`}
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
          <div className="flex flex-col gap-1">
            <label htmlFor="manual-description" className="text-xs font-medium text-muted-foreground">
              Description (optional)
            </label>
            <input
              type="text"
              id="manual-description"
              className={inputCls}
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
        className="flex items-center justify-center gap-1.5 px-4 py-2 mt-1 text-sm font-medium text-primary-foreground bg-primary border-none rounded-md cursor-pointer transition-colors duration-150 hover:bg-primary/90 disabled:opacity-50 disabled:cursor-not-allowed"
        onClick={handleAdd}
        disabled={!isAddEnabled}
        aria-label="Add income source"
      >
        <PlusIcon className="w-4 h-4" />
        Add
      </button>
    </div>
  );
}
