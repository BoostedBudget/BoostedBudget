/**
 * BudgetCategoryForm
 * Issue #497: Budget Category Manager + Match Config UI
 * Issue #1073: Updated for PFC v2 canonical schema
 * Issue #1200: Replace flat category select with hierarchical picker
 *
 * Modal dialog for creating and editing budget categories with match configuration.
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
import { Alert } from '@/components/ui/alert';
import { Button } from '@/components/ui/button';
import type { BudgetCategory, CreateBudgetCategoryRequest, UpdateBudgetCategoryRequest } from '../../types';
import type { MatchLogic } from '@boostedbudget/shared-types/matching';
import { DEFAULT_MATCH_LOGIC } from '@boostedbudget/shared-types/matching';
import type { UserTag } from '@/features/tags/types';
import { CategoryMatchConfig } from './CategoryMatchConfig';
import { usePreviewCategories } from '../../hooks/usePreviewCategories';
import { formatPFCPrimary } from '@/shared/categories/formatPfc';
import { getDetailedLabel } from '@/shared/categories/pfcOptions';
import { HierarchicalCategoryTreeSelect } from '@/shared/components/HierarchicalCategoryTreeSelect';
import { useGetUserCategories } from '@boostedbudget/api-client/endpoints/categories/categories';

export interface BudgetCategoryFormProps {
  isOpen: boolean;
  onClose: () => void;
  onSubmit: (data: CreateBudgetCategoryRequest | UpdateBudgetCategoryRequest) => Promise<void>;
  mode: 'create' | 'edit';
  initialData?: BudgetCategory;
  /**
   * @deprecated No longer used — PFC picker fetches its own data; custom categories
   * are fetched via useGetUserCategories(). Kept for backward compatibility with callers.
   */
  availableCategories?: string[][];
  availableTags: UserTag[];
  existingCategories?: string[][];
  budgetStartDate?: string;
  budgetEndDate?: string;
  expectedIncome?: number;
  totalAllocatedExcludingCurrent?: number;
}

interface FormData {
  category_kind: string;
  pfc_primary: string;
  pfc_detailed: string;
  custom_category_id: string;
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
  category_kind: 'pfc',
  pfc_primary: '',
  pfc_detailed: '',
  custom_category_id: '',
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

  // Fetch user's custom categories to get real UUIDs for the select
  const { data: userCategories } = useGetUserCategories();

  useEffect(() => {
    if (isOpen) {
      if (mode === 'edit' && initialData) {
        setFormData({
          category_kind: initialData.category_kind || 'pfc',
          pfc_primary: initialData.pfc_primary || '',
          pfc_detailed: initialData.pfc_detailed || '',
          custom_category_id: initialData.custom_category_id || '',
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

  // Build dropdown options for the custom category select, keyed by real UUID.
  // Groups by parent_pfc_primary for <optgroup> display.
  const customCategoryOptionGroups = useMemo(() => {
    const cats = userCategories ?? [];
    const groups: Record<string, { id: string; label: string }[]> = {};
    for (const cat of cats) {
      const groupKey = cat.parent_pfc_primary ?? '__none__';
      if (!groups[groupKey]) groups[groupKey] = [];
      groups[groupKey].push({ id: cat.id, label: cat.name });
    }
    return groups;
  }, [userCategories]);

  const showTagPicker = formData.match_logic !== 'category_only';

  const hasCategory = formData.category_kind === 'pfc'
    ? !!formData.pfc_primary
    : !!formData.custom_category_id;

  const previewEnabled = useMemo(() => {
    return isOpen && hasCategory && !!budgetStartDate && !!budgetEndDate;
  }, [isOpen, hasCategory, budgetStartDate, budgetEndDate]);

  const { preview, loading: previewLoading, error: previewError } = usePreviewCategories(
    formData.category_kind,
    formData.pfc_primary || undefined,
    formData.pfc_detailed || undefined,
    formData.custom_category_id || undefined,
    formData.tags,
    formData.match_logic,
    budgetStartDate || '',
    budgetEndDate || '',
    previewEnabled
  );

  const allocationOverage = useMemo(() => {
    if (expectedIncome == null || expectedIncome <= 0) return null;
    const newAmount = parseFloat(formData.allocated_amount);
    if (isNaN(newAmount) || newAmount <= 0) return null;
    const projected = totalAllocatedExcludingCurrent + newAmount;
    return projected > expectedIncome ? projected - expectedIncome : null;
  }, [expectedIncome, totalAllocatedExcludingCurrent, formData.allocated_amount]);

  const formatCurrency = (amount: number): string =>
    new Intl.NumberFormat('en-US', {
      style: 'currency',
      currency: 'USD',
    }).format(amount);

  const handleCategoryKindChange = useCallback(
    (kind: 'pfc' | 'custom') => {
      setFormData((prev) => ({
        ...prev,
        category_kind: kind,
        ...(kind === 'pfc'
          ? { custom_category_id: '' }
          : { pfc_primary: '', pfc_detailed: '' }),
      }));
      if (errors.category) {
        setErrors((prev) => ({ ...prev, category: undefined }));
      }
    },
    [errors.category]
  );

  // PFC single-select value for the tree picker
  const pfcSingleSelectValue = useMemo(
    () =>
      formData.pfc_primary
        ? { primary: formData.pfc_primary, detailed: formData.pfc_detailed || undefined }
        : null,
    [formData.pfc_primary, formData.pfc_detailed]
  );

  const handlePFCSingleSelectChange = useCallback(
    (value: { primary: string; detailed?: string } | null) => {
      setFormData((prev) => ({
        ...prev,
        pfc_primary: value?.primary ?? '',
        pfc_detailed: value?.detailed ?? '',
      }));
      if (errors.category) {
        setErrors((prev) => ({ ...prev, category: undefined }));
      }
    },
    [errors.category]
  );

  const handleCustomCategoryChange = useCallback(
    (e: React.ChangeEvent<HTMLSelectElement>) => {
      setFormData((prev) => ({ ...prev, custom_category_id: e.target.value }));
      if (errors.category) {
        setErrors((prev) => ({ ...prev, category: undefined }));
      }
    },
    [errors.category]
  );

  const handleAmountChange = useCallback(
    (e: React.ChangeEvent<HTMLInputElement>) => {
      const value = e.target.value;
      setFormData((prev) => ({ ...prev, allocated_amount: value }));
      if (errors.allocated_amount) {
        setErrors((prev) => ({ ...prev, allocated_amount: undefined }));
      }
    },
    [errors.allocated_amount]
  );

  const handleMatchLogicChange = useCallback((logic: MatchLogic) => {
    setFormData((prev) => ({ ...prev, match_logic: logic }));
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
      if (errors.tags) {
        setErrors((prev) => ({ ...prev, tags: undefined }));
      }
    },
    [errors.tags]
  );

  const validate = useCallback((): boolean => {
    const newErrors: FormErrors = {};

    if (!hasCategory) {
      newErrors.category = 'Category is required';
    }

    const amount = parseFloat(formData.allocated_amount);
    if (isNaN(amount) || amount <= 0) {
      newErrors.allocated_amount = 'Amount must be greater than 0';
    }

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
  }, [formData, hasCategory]);

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
            category_kind: formData.category_kind,
            pfc_primary: formData.pfc_primary || undefined,
            pfc_detailed: formData.pfc_detailed || undefined,
            custom_category_id: formData.custom_category_id || undefined,
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

  const isEditMode = mode === 'edit';
  const dialogTitle = isEditMode ? 'Edit Category' : 'Add Category';
  const submitButtonText = isEditMode ? 'Save' : 'Add';

  const inputBase = 'w-full py-2.5 px-3 text-sm border border-input rounded-lg bg-card text-foreground transition-[border-color,box-shadow] duration-150 focus:outline-none focus:border-primary focus:ring-[3px] focus:ring-ring/15 disabled:bg-muted disabled:text-muted-foreground disabled:cursor-not-allowed';
  const inputError = 'border-destructive focus:ring-destructive/15';

  // PFC primary display label
  const pfcPrimaryLabel = formData.pfc_primary ? formatPFCPrimary(formData.pfc_primary) : '';
  const pfcDetailedLabel = formData.pfc_detailed ? (getDetailedLabel(formData.pfc_detailed) ?? formData.pfc_detailed) : '';

  return (
    <Dialog open={isOpen} onOpenChange={(open) => !open && onClose()}>
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
            <label className="block text-sm font-medium text-foreground mb-2">
              Category
            </label>

            {/* Category kind toggle */}
            <div className="flex rounded-md border border-border overflow-hidden mb-3">
              <button
                type="button"
                onClick={() => handleCategoryKindChange('pfc')}
                className={cn(
                  'flex-1 px-3 py-1.5 text-sm font-medium transition-colors',
                  formData.category_kind === 'pfc'
                    ? 'bg-primary text-primary-foreground'
                    : 'text-muted-foreground hover:text-foreground hover:bg-accent'
                )}
                disabled={isEditMode}
              >
                Plaid Categories
              </button>
              {(userCategories?.length ?? 0) > 0 && (
                <button
                  type="button"
                  onClick={() => handleCategoryKindChange('custom')}
                  className={cn(
                    'flex-1 px-3 py-1.5 text-sm font-medium transition-colors border-l border-border',
                    formData.category_kind === 'custom'
                      ? 'bg-primary text-primary-foreground'
                      : 'text-muted-foreground hover:text-foreground hover:bg-accent'
                  )}
                  disabled={isEditMode}
                >
                  Custom Categories
                </button>
              )}
            </div>

            {/* PFC section: hierarchical tree picker */}
            {formData.category_kind === 'pfc' && (
              <div>
                <HierarchicalCategoryTreeSelect
                  singleSelectValue={pfcSingleSelectValue}
                  onSingleSelectChange={handlePFCSingleSelectChange}
                  disabled={isEditMode}
                  aria-label="Select PFC category"
                />
                {pfcSingleSelectValue && (
                  <div className="mt-1.5 flex items-center gap-1.5 text-sm text-foreground">
                    <span>{pfcPrimaryLabel}</span>
                    {pfcDetailedLabel && (
                      <>
                        <span className="text-muted-foreground">›</span>
                        <span>{pfcDetailedLabel}</span>
                      </>
                    )}
                  </div>
                )}
              </div>
            )}

            {/* Custom section: select grouped by parent_pfc_primary */}
            {formData.category_kind === 'custom' && (
              <select
                id="category"
                value={formData.custom_category_id}
                onChange={handleCustomCategoryChange}
                disabled={isEditMode}
                aria-label="Category"
                className={cn(inputBase, errors.category && inputError)}
              >
                <option value="">Select a category...</option>
                {Object.entries(customCategoryOptionGroups).map(([groupKey, opts]) => (
                  <optgroup
                    key={groupKey}
                    label={groupKey === '__none__' ? 'Custom (no parent)' : formatPFCPrimary(groupKey)}
                  >
                    {opts.map((opt) => (
                      <option key={opt.id} value={opt.id}>
                        {opt.label}
                      </option>
                    ))}
                  </optgroup>
                ))}
              </select>
            )}

            {errors.category && (
              <p className="mt-1 text-sm text-destructive">
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
              <p className="mt-1 text-sm text-destructive">
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

          {/* Tags Field */}
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
                <p className="mt-1 text-sm text-destructive">
                  {errors.tags}
                </p>
              )}
            </div>
          )}

          {/* Transaction Preview */}
          {previewEnabled && (
            <Alert
              variant="info"
              role="status"
              aria-live="polite"
              data-testid="category-preview"
              className="budget-category-form__preview"
            >
              {previewLoading ? (
                <span className="flex items-center gap-2">
                  <Loader2Icon className="h-4 w-4 animate-spin" aria-hidden="true" />
                  <span>Calculating matches...</span>
                </span>
              ) : previewError ? (
                <span className="text-warning">
                  Unable to load preview
                </span>
              ) : preview ? (
                <span>
                  Matches <strong>{preview.transaction_count}</strong> transaction{preview.transaction_count !== 1 ? 's' : ''} totaling{' '}
                  <strong data-ph-mask>{formatCurrency(preview.total_amount)}</strong>
                </span>
              ) : null}
            </Alert>
          )}

          {/* Soft over-allocation warning */}
          {allocationOverage != null && (
            <Alert
              variant="warning"
              role="status"
              aria-live="polite"
            >
              Allocations exceed expected income by ${allocationOverage.toFixed(2)}. You&apos;ll be dipping into savings or going over budget.
            </Alert>
          )}

          {apiError && (
            <Alert variant="destructive">{apiError}</Alert>
          )}

          <div className="flex justify-end gap-3 pt-4">
            <Button variant="outline" type="button" onClick={onClose}>
              Cancel
            </Button>
            <Button type="submit" disabled={isSubmitting}>
              {isSubmitting ? 'Saving...' : submitButtonText}
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}
