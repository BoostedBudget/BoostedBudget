/**
 * IncomeSourcePicker
 * Issue #496: Income Source Configuration UI for Envelope Budgeting
 *
 * Allows adding new income sources with type-specific pickers:
 * - plaid_category: Dropdown of Plaid categories
 * - tag: Dropdown of user tags
 */

import { useState, useCallback } from 'react';
import { PlusIcon } from 'lucide-react';
import type { IncomeSource, IncomeSourceType } from '../../types';
import type { UserTag } from '@/features/tags/types';
import type { Primary } from '@boostedbudget/api-client/models/primary';
import { Button } from '@/components/ui/button';

export interface IncomeSourcePickerProps {
  /** Available Plaid categories for selection */
  availableCategories: Primary[];
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
  { value: 'pfc_primary', label: 'Category' },
  { value: 'tag', label: 'Tag' },
];

export function IncomeSourcePicker({
  availableCategories,
  availableTags,
  existingSources,
  onAdd,
  disabled = false,
}: IncomeSourcePickerProps) {
  const [sourceType, setSourceType] = useState<IncomeSourceType>('pfc_primary');
  const [selectedCategory, setSelectedCategory] = useState('');
  const [selectedTag, setSelectedTag] = useState('');

  // Filter out already selected categories (match both canonical and legacy type)
  const existingCategoryValues = existingSources
    .filter((s) => s.type === 'pfc_primary' || s.type === 'plaid_category')
    .map((s) => s.value);
  const availableCategoriesFiltered = availableCategories.filter(
    (primary) => !existingCategoryValues.includes(primary.enum)
  );

  // Filter out already selected tags
  const existingTagValues = existingSources
    .filter((s) => s.type === 'tag')
    .map((s) => s.value);
  const availableTagsFiltered = availableTags.filter(
    (tag) => !existingTagValues.includes(tag.name)
  );

  // Determine if add button should be enabled
  const isAddEnabled =
    !disabled &&
    ((sourceType === 'pfc_primary' && selectedCategory !== '') ||
      (sourceType === 'tag' && selectedTag !== ''));

  // Handle type change
  const handleTypeChange = useCallback((e: React.ChangeEvent<HTMLSelectElement>) => {
    const newType = e.target.value as IncomeSourceType;
    setSourceType(newType);
    // Reset selections when type changes
    setSelectedCategory('');
    setSelectedTag('');
  }, []);

  // Handle add
  const handleAdd = useCallback(() => {
    if (!isAddEnabled) return;

    let source: IncomeSource | undefined;

    switch (sourceType) {
      case 'pfc_primary':
        source = { type: 'pfc_primary', value: selectedCategory };
        break;
      case 'tag':
        source = { type: 'tag', value: selectedTag };
        break;
    }

    if (!source) return;
    onAdd(source);

    // Reset selection
    setSelectedCategory('');
    setSelectedTag('');
  }, [sourceType, selectedCategory, selectedTag, isAddEnabled, onAdd]);

  const selectCls =
    'px-3 py-2 text-sm bg-card border border-input rounded-md text-foreground cursor-pointer transition-colors duration-150 focus:outline-none focus:border-ring focus:ring-2 focus:ring-ring disabled:bg-muted disabled:cursor-not-allowed disabled:opacity-60';

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
      {sourceType === 'pfc_primary' && (
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
            {availableCategoriesFiltered.map((primary) => (
              <option key={primary.enum} value={primary.enum}>
                {primary.display}
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

      {/* Add Button */}
      <Button
        type="button"
        className="mt-1"
        onClick={handleAdd}
        disabled={!isAddEnabled}
        aria-label="Add income source"
      >
        <PlusIcon className="w-4 h-4" />
        Add
      </Button>
    </div>
  );
}
