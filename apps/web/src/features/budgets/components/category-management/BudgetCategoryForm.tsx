/**
 * BudgetCategoryForm
 * Issue #497: Budget Category Manager + Match Config UI
 *
 * Modal dialog for creating and editing budget categories with match configuration.
 * Uses shadcn/ui Dialog for accessibility.
 */

import { useState, useEffect, useCallback, useMemo } from 'react';
import { XIcon, Loader2Icon } from 'lucide-react';
import { cn } from '@/lib/utils';
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import type { BudgetCategory, CreateBudgetCategoryRequest, UpdateBudgetCategoryRequest } from '../../types';
import { formatCategoryPath } from '../../types';
import type { MatchLogic } from '@boostedbudget/shared-types/matching';
import { DEFAULT_MATCH_LOGIC } from '@boostedbudget/shared-types/matching';
import type { UserTag } from '@/features/tags/types';
import { CategoryMatchConfig } from './CategoryMatchConfig';
import { usePreviewCategories } from '../../hooks/usePreviewCategories';

export interface BudgetCategoryFormProps {
  /** Whether the dialog is open */
  isOpen: boolean;
  /** Callback when dialog should close */
  onClose: () => void;
  /** Callback when form is submitted */
  onSubmit: (data: CreateBudgetCategoryRequest | UpdateBudgetCategoryRequest) => Promise<void>;
  /** Whether in create or edit mode */
  mode: 'create' | 'edit';
  /** Initial data for edit mode */
  initialData?: BudgetCategory;
  /** Available Plaid categories for selection */
  availableCategories: string[][];
  /** Available user tags for selection */
  availableTags: UserTag[];
  /** Categories already used in this budget (to filter from dropdown) */
  existingCategories?: string[][];
  /** Budget start date for preview (YYYY-MM-DD) */
  budgetStartDate?: string;
  /** Budget end date for preview (YYYY-MM-DD) */
  budgetEndDate?: string;
  /** Expected income for the budget (used for soft over-allocation warning) */
  expectedIncome?: number;
  /** Sum of all other categories' allocations excluding the one being edited */
  totalAllocatedExcludingCurrent?: number;
}

interface FormData {
  category: string[];
  allocated_amount: string;
  tags: string[];
  match_logic: MatchLogic;
}

interface FormErrors {
  category?: string;
  allocated_amount?: string;
  tags?: string;
}

const initialFormData: FormData = {
  category: [],
  allocated_amount: '',
  tags: [],
  match_logic: DEFAULT_MATCH_LOGIC,
};

export function BudgetCategoryForm({
  isOpen,
  onClose,
  onSubmit,
  mode,
  initialData,
  availableCategories,
  availableTags,
  budgetStartDate,
  budgetEndDate,
  expectedIncome,
  totalAllocatedExcludingCurrent = 0,
}: BudgetCategoryFormProps) {
  const [formData, setFormData] = useState<FormData>(initialFormData);
  const [errors, setErrors] = useState<FormErrors>({});
  const [apiError, setApiError] = useState<string | null>(null);
  const [isSubmitting, setIsSubmitting] = useState(false);

  // Reset/initialize form when dialog opens or mode changes
  useEffect(() => {
    if (isOpen) {
      if (mode === 'edit' && initialData) {
        setFormData({
          category: initialData.category,
          allocated_amount: initialData.allocated_amount.toString(),
          tags: initialData.tags || [],
          match_logic: initialData.match_logic || DEFAULT_MATCH_LOGIC,
        });
      } else {
        setFormData(initialFormData);
      }
      setErrors({});
      setApiError(null);
    }
  }, [isOpen, mode, initialData]);

  // Format categories for dropdown
  const categoryOptions = useMemo(() => {
    return availableCategories.map((cat) => ({
      value: cat.join(' > '),
      label: formatCategoryPath(cat),
      category: cat,
    }));
  }, [availableCategories]);

  // Check if tags should be shown based on match_logic
  const showTagPicker = useMemo(() => {
    return formData.match_logic !== 'category_only';
  }, [formData.match_logic]);

  // Issue #497: Category preview - show matching transaction count and total
  const previewEnabled = useMemo(() => {
    return isOpen && formData.category.length > 0 && !!budgetStartDate && !!budgetEndDate;
  }, [isOpen, formData.category, budgetStartDate, budgetEndDate]);

  const previewCategories = useMemo(() => {
    return formData.category.length > 0 ? [formData.category] : [];
  }, [formData.category]);

  const { preview, loading: previewLoading, error: previewError } = usePreviewCategories(
    previewCategories,
    formData.tags,
    formData.match_logic,
    budgetStartDate || '',
    budgetEndDate || '',
    previewEnabled
  );

  // Compute soft over-allocation warning
  const allocationOverage = useMemo(() => {
    if (expectedIncome == null || expectedIncome <= 0) return null;
    const newAmount = parseFloat(formData.allocated_amount);
    if (isNaN(newAmount) || newAmount <= 0) return null;
    const projected = totalAllocatedExcludingCurrent + newAmount;
    return projected > expectedIncome ? projected - expectedIncome : null;
  }, [expectedIncome, totalAllocatedExcludingCurrent, formData.allocated_amount]);

  // Format currency for preview display
  const formatCurrency = (amount: number): string => {
    return new Intl.NumberFormat('en-US', {
      style: 'currency',
      currency: 'USD',
    }).format(amount);
  };

  const handleCategoryChange = useCallback(
    (e: React.ChangeEvent<HTMLSelectElement>) => {
      const selectedValue = e.target.value;
      const selectedOption = categoryOptions.find((opt) => opt.value === selectedValue);
      setFormData((prev) => ({
        ...prev,
        category: selectedOption ? selectedOption.category : [],
      }));
      // Clear error for this field
      if (errors.category) {
        setErrors((prev) => ({ ...prev, category: undefined }));
      }
    },
    [categoryOptions, errors.category]
  );

  const handleAmountChange = useCallback(
    (e: React.ChangeEvent<HTMLInputElement>) => {
      const value = e.target.value;
      setFormData((prev) => ({ ...prev, allocated_amount: value }));
      // Clear error for this field
      if (errors.allocated_amount) {
        setErrors((prev) => ({ ...prev, allocated_amount: undefined }));
      }
    },
    [errors.allocated_amount]
  );

  const handleMatchLogicChange = useCallback((logic: MatchLogic) => {
    setFormData((prev) => ({ ...prev, match_logic: logic }));
    // Clear tag error when switching away from tag-required modes
    if (logic === 'category_only') {
      setErrors((prev) => ({ ...prev, tags: undefined }));
    }
  }, []);

  const handleTagToggle = useCallback(
    (tagName: string) => {
      setFormData((prev) => {
        const isSelected = prev.tags.includes(tagName);
        const newTags = isSelected
          ? prev.tags.filter((t) => t !== tagName)
          : [...prev.tags, tagName];
        return { ...prev, tags: newTags };
      });
      // Clear error for tags
      if (errors.tags) {
        setErrors((prev) => ({ ...prev, tags: undefined }));
      }
    },
    [errors.tags]
  );

  const validate = useCallback((): boolean => {
    const newErrors: FormErrors = {};

    // Validate category selection
    if (formData.category.length === 0) {
      newErrors.category = 'Category is required';
    }

    // Validate amount
    const amount = parseFloat(formData.allocated_amount);
    if (isNaN(amount) || amount <= 0) {
      newErrors.allocated_amount = 'Amount must be greater than 0';
    }

    // Validate tags when match_logic requires them
    if (
      (formData.match_logic === 'tag_only' ||
        formData.match_logic === 'and' ||
        formData.match_logic === 'or') &&
      formData.tags.length === 0
    ) {
      newErrors.tags = 'At least one tag is required';
    }

    setErrors(newErrors);
    return Object.keys(newErrors).length === 0;
  }, [formData]);

  const handleSubmit = useCallback(
    async (e: React.FormEvent) => {
      e.preventDefault();
      setApiError(null);

      if (!validate()) {
        return;
      }

      setIsSubmitting(true);

      try {
        const amount = parseFloat(formData.allocated_amount);

        if (mode === 'create') {
          const request: CreateBudgetCategoryRequest = {
            category: formData.category,
            allocated_amount: amount,
            tags: formData.tags.length > 0 ? formData.tags : undefined,
            match_logic: formData.match_logic,
          };
          await onSubmit(request);
        } else {
          const request: UpdateBudgetCategoryRequest = {
            allocated_amount: amount,
            tags: formData.tags.length > 0 ? formData.tags : undefined,
            match_logic: formData.match_logic,
          };
          await onSubmit(request);
        }

        onClose();
      } catch (err) {
        setApiError(err instanceof Error ? err.message : 'Failed to save category');
      } finally {
        setIsSubmitting(false);
      }
    },
    [formData, mode, validate, onSubmit, onClose]
  );

  // Handle dialog open state changes
  const handleOpenChange = useCallback(
    (open: boolean) => {
      if (!open) {
        onClose();
      }
    },
    [onClose]
  );

  const dialogTitle = mode === 'create' ? 'Add Category' : 'Edit Category';
  const submitButtonText = mode === 'create' ? 'Add' : 'Save';

  // Shared input classes
  const inputBase =
    'w-full py-2.5 px-3 text-sm border border-input rounded-lg bg-card text-foreground transition-[border-color,box-shadow] duration-150 focus:outline-none focus:border-primary focus:ring-[3px] focus:ring-ring/15 disabled:bg-muted disabled:text-muted-foreground disabled:cursor-not-allowed';
  const inputError = 'border-destructive focus:ring-destructive/15';

  return (
    <Dialog open={isOpen} onOpenChange={handleOpenChange}>
      <DialogContent
        className="budget-category-form w-full max-w-md p-6 bg-card rounded-2xl"
        onInteractOutside={(e) => e.preventDefault()}
        onPointerDownOutside={(e) => e.preventDefault()}
        showCloseButton={false}
      >
        <DialogHeader className="flex flex-row items-center justify-between mb-6">
          <DialogTitle className="text-xl font-semibold text-foreground">
            {dialogTitle}
          </DialogTitle>
          <button
            type="button"
            onClick={onClose}
            className="text-muted-foreground hover:text-foreground"
          >
            <XIcon className="h-6 w-6" />
          </button>
        </DialogHeader>

        <form onSubmit={handleSubmit} className="space-y-4">
          {/* Category Field */}
          <div>
            <label
              htmlFor="category"
              className="block text-sm font-medium text-foreground"
            >
              Category
            </label>
            <select
              id="category"
              value={formData.category.join(' > ')}
              onChange={handleCategoryChange}
              disabled={mode === 'edit'}
              className={cn(inputBase, errors.category && inputError)}
            >
              <option value="">Select a category...</option>
              {categoryOptions.map((option) => (
                <option key={option.value} value={option.value}>
                  {option.label}
                </option>
              ))}
            </select>
            {errors.category && (
              <p className="mt-1 text-sm text-red-600 dark:text-red-400">
                {errors.category}
              </p>
            )}
          </div>

          {/* Amount Field */}
          <div>
            <label
              htmlFor="amount"
              className="block text-sm font-medium text-foreground"
            >
              Amount
            </label>
            <div className="relative">
              <span className="absolute left-3 top-1/2 -translate-y-1/2 text-muted-foreground">
                $
              </span>
              <input
                type="number"
                id="amount"
                value={formData.allocated_amount}
                onChange={handleAmountChange}
                min="0"
                step="0.01"
                className={cn(inputBase, 'pl-7', errors.allocated_amount && inputError)}
                placeholder="0.00"
              />
            </div>
            {errors.allocated_amount && (
              <p className="mt-1 text-sm text-red-600 dark:text-red-400">
                {errors.allocated_amount}
              </p>
            )}
          </div>

          {/* Match Logic Config */}
          <CategoryMatchConfig
            value={formData.match_logic}
            onChange={handleMatchLogicChange}
            disabled={isSubmitting}
          />

          {/* Tags Field - shown based on match_logic */}
          {showTagPicker && (
            <div>
              <label className="block text-sm font-medium text-foreground mb-2">
                Tags
              </label>
              <div className="flex flex-wrap gap-2">
                {availableTags.map((tag) => {
                  const isSelected = formData.tags.includes(tag.name);
                  return (
                    <button
                      key={tag.id}
                      type="button"
                      onClick={() => handleTagToggle(tag.name)}
                      disabled={isSubmitting}
                      className={cn(
                        'budget-category-form__tag py-1.5 px-3 text-[0.8125rem] font-medium rounded-full cursor-pointer transition-all duration-150 border disabled:opacity-50 disabled:cursor-not-allowed',
                        isSelected
                          ? 'budget-category-form__tag--selected text-white border-transparent hover:enabled:opacity-90'
                          : 'text-muted-foreground bg-muted border-border hover:enabled:bg-accent'
                      )}
                      style={
                        isSelected
                          ? { backgroundColor: tag.color ?? undefined, borderColor: tag.color ?? undefined }
                          : undefined
                      }
                    >
                      {tag.name}
                    </button>
                  );
                })}
              </div>
              {errors.tags && (
                <p className="mt-1 text-sm text-red-600 dark:text-red-400">
                  {errors.tags}
                </p>
              )}
            </div>
          )}

          {/* Issue #497: Transaction Preview */}
          {previewEnabled && (
            <div
              className="budget-category-form__preview p-3 rounded-lg bg-blue-50 dark:bg-blue-900/30 text-blue-700 dark:text-blue-300 text-sm"
              data-testid="category-preview"
              role="status"
              aria-live="polite"
            >
              {previewLoading ? (
                <span className="flex items-center gap-2">
                  <Loader2Icon className="h-4 w-4 animate-spin" aria-hidden="true" />
                  <span>Calculating matches...</span>
                </span>
              ) : previewError ? (
                <span className="text-amber-600 dark:text-amber-400">
                  Unable to load preview
                </span>
              ) : preview ? (
                <span>
                  Matches <strong>{preview.transaction_count}</strong> transaction{preview.transaction_count !== 1 ? 's' : ''} totaling{' '}
                  <strong data-ph-mask>{formatCurrency(preview.total_amount)}</strong>
                </span>
              ) : null}
            </div>
          )}

          {/* Soft over-allocation warning */}
          {allocationOverage != null && (
            <div
              role="status"
              aria-live="polite"
              className="text-sm text-amber-600 dark:text-amber-400 flex items-center gap-1.5"
            >
              <span aria-hidden="true">⚠️</span>
              Allocations exceed expected income by ${allocationOverage.toFixed(2)}. You&apos;ll be dipping into savings or going over budget.
            </div>
          )}

          {/* API Error */}
          {apiError && (
            <div className="p-3 rounded-lg bg-red-50 dark:bg-red-900/30 text-red-600 dark:text-red-400 text-sm">
              {apiError}
            </div>
          )}

          {/* Actions */}
          <div className="flex justify-end gap-3 pt-4">
            <button
              type="button"
              onClick={onClose}
              className="px-4 py-2 text-sm font-medium text-foreground bg-muted rounded-lg hover:bg-muted/80 transition-colors"
            >
              Cancel
            </button>
            <button
              type="submit"
              disabled={isSubmitting}
              className="px-4 py-2 text-sm font-medium text-white bg-blue-600 rounded-lg hover:bg-blue-700 disabled:opacity-50 disabled:cursor-not-allowed transition-colors"
            >
              {isSubmitting ? 'Saving...' : submitButtonText}
            </button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}
