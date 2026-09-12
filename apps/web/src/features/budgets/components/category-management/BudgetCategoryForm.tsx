/**
 * BudgetCategoryForm
 * Modal dialog for creating and editing budget categories with match configuration.
 * Supports PFC categories, custom categories, and savings goal linking.
 */

import { Fragment, useState, useEffect, useCallback, useMemo } from 'react';
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
import type { BudgetCategory, BudgetGroupMember, CreateBudgetCategoryRequest, UpdateBudgetCategoryRequest } from '../../types';
import type { MatchLogic } from '@boostedbudget/shared-types/matching';
import { DEFAULT_MATCH_LOGIC } from '@boostedbudget/shared-types/matching';
import type { UserTag } from '@/features/tags/types';
import { CategoryMatchConfig } from './CategoryMatchConfig';
import { usePreviewCategories, type BudgetPreviewContext } from '../../hooks/usePreviewCategories';
import { formatPFCPrimary } from '@/shared/categories/formatPfc';
import { getDetailedLabel, getPrimaryLabel, useCuratedPfcDetailed } from '@/shared/categories/pfcOptions';
import { HierarchicalCategoryTreeSelect } from '@/shared/components/HierarchicalCategoryTreeSelect';
import { useGetUserCategories } from '@boostedbudget/api-client/endpoints/categories/categories';
import { buildCategoryHierarchy } from '@/features/categories/utils/buildCategoryHierarchy';
import { SavingsGoalSelect } from './SavingsGoalSelect';
import type { CategoryPreviewRequest } from '@boostedbudget/api-client/models';

export interface BudgetCategoryFormProps {
  currencyCode?: string;
  budgetId?: string;
  draftCategories?: BudgetCategory[];
  isOpen: boolean;
  onClose: () => void;
  onSubmit: (data: CreateBudgetCategoryRequest | UpdateBudgetCategoryRequest) => Promise<void>;
  mode: 'create' | 'edit';
  initialData?: BudgetCategory;
  /**
   * Deprecated: PFC picker fetches its own data; custom categories via useGetUserCategories().
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
  category_kind: CreateBudgetCategoryRequest['category_kind'];
  group_name: string;
  members: BudgetGroupMember[];
  pfc_primary: string;
  pfc_detailed: string;
  custom_category_id: string;
  savings_goal_id: string; // #1300: savings bucket linkage
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
  group_name: '',
  members: [],
  pfc_primary: '',
  pfc_detailed: '',
  custom_category_id: '',
  savings_goal_id: '',
  allocated_amount: '',
  tags: [],
  match_logic: DEFAULT_MATCH_LOGIC,
};

function hasSelectedCategory(formData: FormData, retainedEmptyGroup: boolean): boolean {
  switch (formData.category_kind) {
    case 'remainder':
      return true;
    case 'group':
      return !!formData.group_name.trim() && (formData.members.length > 0 || retainedEmptyGroup);
    case 'pfc':
      return !!formData.pfc_primary;
    case 'savings':
      return !!formData.savings_goal_id;
    default:
      return !!formData.custom_category_id;
  }
}

function toPreviewDraftCategory(category: BudgetCategory): NonNullable<BudgetPreviewContext['draft_categories']>[number] {
  return {
    ...category,
    auto_created: category.auto_created ?? false,
    custom_parent_id: category.custom_parent_id ?? undefined,
    custom_parent_pfc_primary: category.custom_parent_pfc_primary ?? undefined,
    custom_grandparent_pfc_primary: category.custom_grandparent_pfc_primary ?? undefined,
    managed_by_goal_id: category.managed_by_goal_id ?? undefined,
    savings_goal_id: category.savings_goal_id ?? undefined,
    savings_goal_name: category.savings_goal_name ?? undefined,
  };
}

export function BudgetCategoryForm({
  currencyCode = 'USD',
  budgetId,
  draftCategories,
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
  const { data: pfcOptions } = useCuratedPfcDetailed();

  useEffect(() => {
    if (isOpen) {
      if (mode === 'edit' && initialData) {
        setFormData({
          category_kind: initialData.category_kind || 'pfc',
          group_name: initialData.group_name || '',
          members: initialData.members || [],
          pfc_primary: initialData.pfc_primary || '',
          pfc_detailed: initialData.pfc_detailed || '',
          custom_category_id: initialData.custom_category_id || '',
          savings_goal_id: initialData.savings_goal_id || '',
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

  // Build <optgroup> sections for the custom category select, reflecting the full custom
  // hierarchy (#1203): childless top-level customs, custom parents with their custom
  // children, then customs nested under a PFC primary.
  //
  // Issue #1514 (fix round 4): a custom parent's group used to be labeled with the
  // parent's own name (`g.parent.name`) via `<optgroup label>`. `label` is a DOM
  // ATTRIBUTE — PostHog session replay records attribute values verbatim, `data-ph-mask`
  // masks element TEXT only, and unlike `<option>`, `<optgroup>` has no child element to
  // put a mask on instead. There is no way to label a group with a user-authored name
  // without leaking it. `label: null` marks a group as "cannot be given a safe header" —
  // its options render flat (no `<optgroup>` wrapper) instead, with the parent's own
  // option immediately followed by its "— child" options so the relationship is still
  // conveyed by order, just without a group header naming the parent. This is a visible
  // UX change (the bold "Food"-style header row is gone for these groups specifically) —
  // see the #1514 fix-round-4 report. The "Custom (no parent)" and per-PFC-primary group
  // labels are static/system strings, never user-authored, so they keep real `<optgroup>`
  // headers.
  const customCategoryOptionGroups = useMemo(() => {
    const cats = (Array.isArray(userCategories) ? userCategories : []) as Parameters<typeof buildCategoryHierarchy>[0];
    const { topLevel, pfcGroups } = buildCategoryHierarchy(cats);
    const groups: { key: string; label: string | null; options: { id: string; label: string }[] }[] = [];

    const childless = topLevel.filter((g) => g.children.length === 0).map((g) => g.parent);
    if (childless.length > 0) {
      groups.push({
        key: '__none__',
        label: 'Custom (no parent)',
        options: childless.map((c) => ({ id: c.id, label: c.name })),
      });
    }

    for (const g of topLevel.filter((g) => g.children.length > 0)) {
      groups.push({
        key: g.parent.id,
        label: null,
        options: [
          { id: g.parent.id, label: g.parent.name },
          ...g.children.map((c) => ({ id: c.id, label: `— ${c.name}` })),
        ],
      });
    }

    for (const pg of pfcGroups) {
      groups.push({
        key: `pfc:${pg.pfcPrimary}`,
        label: getPrimaryLabel(pg.pfcPrimary) ?? formatPFCPrimary(pg.pfcPrimary),
        options: pg.categories.map((c) => ({ id: c.id, label: c.name })),
      });
    }

    return groups;
  }, [userCategories]);

  const isEditMode = mode === 'edit';
  const isSavingsKind = formData.category_kind === 'savings';
  const isGroupKind = formData.category_kind === 'group';
  const isRemainderKind = formData.category_kind === 'remainder';
  const retainedEmptyGroup = isEditMode && isGroupKind && !initialData?.members?.length && formData.members.length === 0;
  const supportsTags = !isSavingsKind && !isGroupKind && !isRemainderKind;
  const showTagPicker = supportsTags && formData.match_logic !== 'category_only';

  const hasCategory = hasSelectedCategory(formData, retainedEmptyGroup);

  const previewEnabled = useMemo(() => {
    // Savings categories don't match transactions — skip preview
    return isOpen && hasCategory && !isSavingsKind && !retainedEmptyGroup && !!budgetStartDate && !!budgetEndDate;
  }, [isOpen, hasCategory, isSavingsKind, retainedEmptyGroup, budgetStartDate, budgetEndDate]);
  // Savings has no preview contract, so keep the disabled hook call truthful instead of
  // fabricating a PFC discriminator that could become unsafe if the enablement logic changes.
  const previewCategoryKind: CategoryPreviewRequest['category_kind'] | undefined =
    formData.category_kind === 'savings' ? undefined : formData.category_kind;

  const { preview, loading: previewLoading, error: previewError } = usePreviewCategories(
    previewCategoryKind,
    formData.pfc_primary || undefined,
    formData.pfc_detailed || undefined,
    formData.custom_category_id || undefined,
    formData.tags,
    formData.match_logic,
    budgetStartDate || '',
    budgetEndDate || '',
    previewEnabled,
    {
      budget_id: budgetId,
      replace_category_id: isEditMode ? initialData?.id : undefined,
      draft_categories: budgetId ? undefined : draftCategories?.map(toPreviewDraftCategory),
      ...(isGroupKind && { group_name: formData.group_name, members: formData.members }),
    }
  );

  const allocationOverage = useMemo(() => {
    if (expectedIncome == null || expectedIncome <= 0) return null;
    const newAmount = parseFloat(formData.allocated_amount);
    if (isNaN(newAmount) || newAmount <= 0) return null;
    const projected = totalAllocatedExcludingCurrent + newAmount;
    return projected > expectedIncome ? projected - expectedIncome : null;
  }, [expectedIncome, totalAllocatedExcludingCurrent, formData.allocated_amount]);

  const formatCurrency = (amount: number, currency = currencyCode): string =>
    new Intl.NumberFormat('en-US', {
      style: 'currency',
      currency,
    }).format(amount);

  // Clear error on category change (category_kind, PFC, or custom selection)
  const clearCategoryError = useCallback(
    () => errors.category && setErrors((prev) => ({ ...prev, category: undefined })),
    [errors.category]
  );

  const handleCategoryKindChange = useCallback(
    (kind: 'pfc' | 'custom' | 'savings' | 'group' | 'remainder') => {
      setFormData((prev) => ({
        ...prev,
        category_kind: kind,
        // Clear fields that don't apply to the selected kind
        ...(kind === 'pfc' && { custom_category_id: '', savings_goal_id: '' }),
        ...(kind === 'custom' && { pfc_primary: '', pfc_detailed: '', savings_goal_id: '' }),
        ...((kind === 'savings' || kind === 'group' || kind === 'remainder') && {
          pfc_primary: '',
          pfc_detailed: '',
          custom_category_id: '',
          savings_goal_id: '',
          tags: [],
          match_logic: DEFAULT_MATCH_LOGIC,
        }),
      }));
      clearCategoryError();
    },
    [clearCategoryError]
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
      clearCategoryError();
    },
    [clearCategoryError]
  );

  const handleCustomCategoryChange = useCallback(
    (e: React.ChangeEvent<HTMLSelectElement>) => {
      setFormData((prev) => ({ ...prev, custom_category_id: e.target.value }));
      clearCategoryError();
    },
    [clearCategoryError]
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
      newErrors.category = isGroupKind ? 'Enter a group name and select at least one category.' : 'Category is required';
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
  }, [formData, hasCategory, isGroupKind]);

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
            ...(isGroupKind && { group_name: formData.group_name.trim(), members: formData.members }),
            pfc_primary: formData.pfc_primary || undefined,
            pfc_detailed: formData.pfc_detailed || undefined,
            custom_category_id: formData.custom_category_id || undefined,
            savings_goal_id: formData.savings_goal_id || undefined,
            allocated_amount: amount,
            tags: formData.tags.length > 0 ? formData.tags : undefined,
            match_logic: formData.match_logic,
          };
          await onSubmit(request);
        } else {
          const request: UpdateBudgetCategoryRequest = {
            ...(isGroupKind && { group_name: formData.group_name.trim(), ...(!retainedEmptyGroup && { members: formData.members }) }),
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
    [formData, mode, validate, onSubmit, onClose, isGroupKind, retainedEmptyGroup]
  );


  const inputBase = 'w-full py-2.5 px-3 text-sm border border-input rounded-lg bg-card text-foreground transition-[border-color,box-shadow] duration-150 focus:outline-none focus:border-primary focus:ring-[3px] focus:ring-ring/15 disabled:bg-muted disabled:text-muted-foreground disabled:cursor-not-allowed';
  const inputError = 'border-destructive focus:ring-destructive/15';

  // PFC primary and detailed display labels
  const pfcPrimaryLabel = formData.pfc_primary ? formatPFCPrimary(formData.pfc_primary) : null;
  const pfcDetailedLabel = formData.pfc_detailed ? (getDetailedLabel(formData.pfc_detailed) ?? formData.pfc_detailed) : null;

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
            {mode === 'edit' ? 'Edit Category' : 'Add Category'}
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
            <div className="flex flex-wrap rounded-md border border-border overflow-hidden mb-3">
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
              <button
                type="button"
                onClick={() => handleCategoryKindChange('savings')}
                className={cn(
                  'flex-1 px-3 py-1.5 text-sm font-medium transition-colors border-l border-border',
                  formData.category_kind === 'savings'
                    ? 'bg-success text-success-foreground'
                    : 'text-muted-foreground hover:text-foreground hover:bg-accent'
                )}
                disabled={isEditMode}
              >
                Savings Goal
              </button>
              {([{ kind: 'group', label: 'Shared group' }, { kind: 'remainder', label: 'Other' }] as const).map(({ kind, label }) => (
                <button key={kind} type="button" disabled={isEditMode}
                  aria-pressed={formData.category_kind === kind}
                  onClick={() => handleCategoryKindChange(kind)}
                  className={cn('flex-1 px-3 py-1.5 text-sm font-medium', formData.category_kind === kind ? 'bg-primary text-primary-foreground' : 'text-muted-foreground hover:bg-accent')}>
                  {label}
                </button>
              ))}
            </div>

            {isGroupKind && (
              <div className="space-y-3">
                <label htmlFor="budget-group-name" className="block text-sm font-medium">Group name</label>
                <input id="budget-group-name" data-ph-mask maxLength={100} className={inputBase}
                  value={formData.group_name} onChange={(e) => { setFormData((prev) => ({ ...prev, group_name: e.target.value })); clearCategoryError(); }} />
                <HierarchicalCategoryTreeSelect
                  aria-label="Select group categories"
                  selectedPrimaries={formData.members.flatMap((m) => m.category_kind === 'pfc' && m.pfc_primary && !m.pfc_detailed ? [m.pfc_primary] : [])}
                  onPrimariesChange={(values) => setFormData((prev) => ({ ...prev, members: [...prev.members.filter((m) => m.category_kind !== 'pfc' || !!m.pfc_detailed), ...values.map((primary): BudgetGroupMember => ({ category_kind: 'pfc', pfc_primary: primary }))] }))}
                  selectedDetaileds={formData.members.flatMap((m) => m.pfc_detailed ? [m.pfc_detailed] : [])}
                  onDetailedsChange={(values) => setFormData((prev) => ({ ...prev, members: [...prev.members.filter((m) => !m.pfc_detailed), ...values.flatMap((detailed): BudgetGroupMember[] => {
                    const primary = pfcOptions.find((p) => p.detailed.some((d) => d.value === detailed))?.primary;
                    return primary ? [{ category_kind: 'pfc', pfc_primary: primary, pfc_detailed: detailed }] : [];
                  })] }))}
                  customCategories={userCategories ?? []}
                  selectedCustomIds={formData.members.flatMap((m) => m.custom_category_id ? [m.custom_category_id] : [])}
                  onCustomIdsChange={(values) => setFormData((prev) => ({ ...prev, members: [...prev.members.filter((m) => m.category_kind !== 'custom'), ...values.map((id): BudgetGroupMember => ({ category_kind: 'custom', custom_category_id: id }))] }))}
                />
                <label className="flex items-center gap-2 text-sm">
                  <input type="checkbox" checked={formData.members.some((m) => m.category_kind === 'uncategorized')}
                    onChange={(e) => setFormData((prev) => ({ ...prev, members: [...prev.members.filter((m) => m.category_kind !== 'uncategorized'), ...(e.target.checked ? [{ category_kind: 'uncategorized' as const }] : [])] }))} />
                  Include Uncategorized
                </label>
                <p className="text-sm text-muted-foreground">These categories share one limit. Individual category and tag lines match first.</p>
                {retainedEmptyGroup && <p className="text-sm text-muted-foreground">This group has no categories and currently matches no spending.</p>}
              </div>
            )}
            {isRemainderKind && <p className="text-sm text-muted-foreground">Other automatically includes posted spending not matched by individual lines or shared groups. Transfers are excluded.</p>}

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
                {customCategoryOptionGroups.map((grp) =>
                  grp.label !== null ? (
                    <optgroup key={grp.key} label={grp.label}>
                      {grp.options.map((opt) => (
                        <option key={opt.id} value={opt.id} data-ph-mask>
                          {opt.label}
                        </option>
                      ))}
                    </optgroup>
                  ) : (
                    // No safe (non-PII) label exists for this group — see the
                    // customCategoryOptionGroups doc comment above. Render its options
                    // flat, without an <optgroup> wrapper.
                    <Fragment key={grp.key}>
                      {grp.options.map((opt) => (
                        <option key={opt.id} value={opt.id} data-ph-mask>
                          {opt.label}
                        </option>
                      ))}
                    </Fragment>
                  )
                )}
              </select>
            )}

            {/* Savings section: goal picker */}
            {formData.category_kind === 'savings' && (
              <SavingsGoalSelect
                id="savings-goal"
                value={formData.savings_goal_id}
                onChange={(goalId) => {
                  setFormData((prev) => ({ ...prev, savings_goal_id: goalId }));
                  clearCategoryError();
                }}
                disabled={isEditMode}
                className={cn(errors.category && inputError)}
              />
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

          {/* Match Logic Config — hidden for savings (not transaction-based) */}
          {supportsTags && (
            <CategoryMatchConfig
              value={formData.match_logic}
              onChange={handleMatchLogicChange}
              disabled={isSubmitting}
            />
          )}

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
                <div className="flex flex-col gap-1">
                  <span>
                    Matches <strong>{preview.transaction_count}</strong> transaction{preview.transaction_count !== 1 ? 's' : ''} totaling{' '}
                    <strong data-ph-mask>{formatCurrency(preview.total_amount, preview.currency_code)}</strong>
                  </span>
                  {/* Issue #1630 Task B5: total_amount is a REAL PARTITION of three buckets —
                      category-direct, nested-descendant (a pfc_primary or parent custom
                      category rolls up its nested children, Issue #1203), and
                      tag-only-outside-category (a tag pulled in a transaction the category
                      clause never matched at all, match_logic or/tag_only). Render whichever
                      buckets are non-empty so the total never again reads as unexplained. */}
                  {((preview.children && preview.children.length > 0) ||
                    (preview.direct_transaction_count ?? 0) > 0 ||
                    (preview.tag_only_transaction_count ?? 0) > 0) && (
                    <ul
                      className="budget-category-form__preview-breakdown flex flex-col gap-0.5 pl-4 text-sm text-muted-foreground"
                      data-testid="category-preview-breakdown"
                    >
                      {(preview.direct_transaction_count ?? 0) > 0 && (
                        <li data-ph-mask>
                          <strong>{formatCurrency(preview.direct_amount ?? 0, preview.currency_code)}</strong> direct
                          {' '}({preview.direct_transaction_count ?? 0} transaction{(preview.direct_transaction_count ?? 0) !== 1 ? 's' : ''})
                        </li>
                      )}
                      {preview.children?.map((child) => (
                        <li key={child.category_id} data-ph-mask>
                          <strong>{formatCurrency(child.total_amount, preview.currency_code)}</strong> from your{' '}
                          <strong>{child.category_name}</strong> category
                          {' '}({child.transaction_count} transaction{child.transaction_count !== 1 ? 's' : ''})
                        </li>
                      ))}
                      {(preview.tag_only_transaction_count ?? 0) > 0 && (
                        <li data-testid="category-preview-tag-only" data-ph-mask>
                          <strong>{formatCurrency(preview.tag_only_amount ?? 0, preview.currency_code)}</strong> matched only
                          by tag, outside this category
                          {' '}({preview.tag_only_transaction_count ?? 0} transaction{(preview.tag_only_transaction_count ?? 0) !== 1 ? 's' : ''})
                        </li>
                      )}
                    </ul>
                  )}
                </div>
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
              Allocations exceed expected income by{' '}
              <span data-ph-mask>{formatCurrency(allocationOverage)}</span>.{' '}
              You&apos;ll be dipping into savings or going over budget.
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
              {isSubmitting ? 'Saving...' : (mode === 'edit' ? 'Save' : 'Add')}
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}
