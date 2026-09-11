/**
 * BudgetCategoryManager
 * Issue #497: Budget Category Manager + Match Config UI
 * Issue #1200: Multi-level category hierarchy — grouped visual layout
 *
 * Container component for managing budget categories with CRUD operations.
 * Includes add/edit form dialog, delete confirmation, and allocation breakdown.
 * Categories are rendered in a primary→children hierarchy using groupBudgetCategoriesByPrimary.
 */

import { useState, useCallback, useMemo, useRef, lazy, Suspense } from 'react';
import { PlusIcon, SettingsIcon, LayoutList } from 'lucide-react';
import { EmptyState } from '@/components/ui/empty-state';
import { ConfirmationDialog } from '@/components/ui/confirmation-dialog';
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription } from '@/components/ui/dialog';

// Lazy-loaded so the manager (and its form/icon-picker deps) stays in its own chunk and
// doesn't bloat the budgets route bundle — it only loads when the dialog opens (#1203).
const CustomCategoryManager = lazy(() =>
  import('@/features/categories/components/CustomCategoryManager').then((m) => ({
    default: m.CustomCategoryManager,
  }))
);
import type {
  BudgetCategory,
  CreateBudgetCategoryRequest,
  UpdateBudgetCategoryRequest,
} from '../../types';
import {
  formatBudgetCategoryLabel,
  filterTopLevel,
  isSubAllocationOfParent,
  computeParentAllocationRemainder,
  formatCurrency,
} from '../../types';
import type { UserTag } from '@/features/tags/types';
import { BudgetCategoryCard } from './BudgetCategoryCard';
import { BudgetCategoryForm } from './BudgetCategoryForm';
import { AllocationBreakdown } from './AllocationBreakdown';
import { groupBudgetCategoriesByPrimary } from '../../utils/groupBudgetCategoriesByPrimary';
import type { CustomCategoryMap } from '../../utils/groupBudgetCategoriesByPrimary';
import { useGetUserCategories } from '@boostedbudget/api-client/endpoints/categories/categories';

export interface BudgetCategoryManagerProps {
  /** Budget ID for API calls */
  budgetId: string;
  /** List of budget categories to display */
  categories: BudgetCategory[];
  /** Total income for allocation breakdown */
  totalIncome: number;
  /** Callback when a new category is added */
  onAddCategory: (req: CreateBudgetCategoryRequest) => Promise<BudgetCategory>;
  /** Callback when a category is updated */
  onUpdateCategory: (id: string, req: UpdateBudgetCategoryRequest) => Promise<BudgetCategory>;
  /** Callback when a category is deleted */
  onDeleteCategory: (id: string) => Promise<void>;
  /**
   * @deprecated No longer used — BudgetCategoryForm fetches custom categories internally
   * via useGetUserCategories. Kept for backward compatibility; will be removed in a future cleanup.
   */
  availableCategories?: string[][];
  /** Available user tags for selection */
  availableTags: UserTag[];
  /** Whether all interactions are disabled */
  disabled?: boolean;
}

interface FormDialogState {
  isOpen: boolean;
  mode: 'create' | 'edit';
  category: BudgetCategory | null;
}

interface DeleteDialogState {
  isOpen: boolean;
  category: BudgetCategory | null;
}

export function BudgetCategoryManager({
  categories,
  totalIncome,
  onAddCategory,
  onUpdateCategory,
  onDeleteCategory,
  availableTags,
  disabled = false,
}: BudgetCategoryManagerProps) {
  const [formDialog, setFormDialog] = useState<FormDialogState>({
    isOpen: false,
    mode: 'create',
    category: null,
  });

  const [deleteDialog, setDeleteDialog] = useState<DeleteDialogState>({
    isOpen: false,
    category: null,
  });

  const [isDeleting, setIsDeleting] = useState(false);

  // "Manage categories" launcher → opens the full custom category manager (#1203)
  const [manageOpen, setManageOpen] = useState(false);

  // Refs for keyboard navigation — indexed by flat order position
  const categoryRefs = useRef<(HTMLDivElement | null)[]>([]);

  // Fetch user custom categories for display name resolution and parent nesting
  const { data: userCategories } = useGetUserCategories();

  const customCategoryMap = useMemo<CustomCategoryMap>(
    () => new Map(
      // Guard against a non-array query value (e.g. under a mocked/errored query).
      (Array.isArray(userCategories) ? userCategories : []).map(cat => [
        cat.id,
        { name: cat.name, parent_pfc_primary: cat.parent_pfc_primary ?? null },
      ])
    ),
    [userCategories]
  );

  // Calculate unallocated funds — top-level categories only (#1203/#1630): a
  // sub-allocation's amount is already inside its parent's, so summing every row
  // double-counts.
  const unallocatedFunds = useMemo(() => {
    const totalAllocated = filterTopLevel(categories).reduce(
      (sum, cat) => sum + cat.allocated_amount,
      0
    );
    return totalIncome - totalAllocated;
  }, [categories, totalIncome]);

  // Separate savings buckets from spending categories (#1300)
  const { spendingCategories, savingsCategories } = useMemo(
    () => ({
      spendingCategories: categories.filter((c) => c.category_kind !== 'savings'),
      savingsCategories: categories.filter((c) => c.category_kind === 'savings'),
    }),
    [categories]
  );

  // Grouped hierarchy for visual layout (custom categories nest under PFC parents when possible)
  const groups = useMemo(
    () => groupBudgetCategoriesByPrimary(spendingCategories, customCategoryMap),
    [spendingCategories, customCategoryMap]
  );

  // Flat order of all categories in render order — for keyboard navigation
  const flatOrder = useMemo(
    () => [
      ...groups.flatMap(({ group, children }) => (group ? [group, ...children] : children)),
      ...savingsCategories,
    ],
    [groups, savingsCategories]
  );

  // Handlers for form dialog
  const handleOpenAddForm = useCallback(() => {
    setFormDialog({
      isOpen: true,
      mode: 'create',
      category: null,
    });
  }, []);

  const handleOpenEditForm = useCallback((category: BudgetCategory) => {
    setFormDialog({
      isOpen: true,
      mode: 'edit',
      category,
    });
  }, []);

  const handleCloseForm = useCallback(() => {
    setFormDialog({ isOpen: false, mode: 'create', category: null });
  }, []);

  const handleFormSubmit = useCallback(
    async (data: CreateBudgetCategoryRequest | UpdateBudgetCategoryRequest) => {
      if (formDialog.mode === 'create') {
        await onAddCategory(data as CreateBudgetCategoryRequest);
      } else if (formDialog.category) {
        await onUpdateCategory(formDialog.category.id, data as UpdateBudgetCategoryRequest);
      }
      handleCloseForm();
    },
    [formDialog.mode, formDialog.category, onAddCategory, onUpdateCategory, handleCloseForm]
  );

  // Handlers for delete dialog
  const handleOpenDeleteDialog = useCallback((category: BudgetCategory) => {
    setDeleteDialog({
      isOpen: true,
      category,
    });
  }, []);

  const handleCloseDeleteDialog = useCallback(() => {
    setDeleteDialog({ isOpen: false, category: null });
  }, []);

  const handleConfirmDelete = useCallback(async () => {
    if (!deleteDialog.category) return;

    setIsDeleting(true);
    try {
      await onDeleteCategory(deleteDialog.category.id);
      handleCloseDeleteDialog();
    } finally {
      setIsDeleting(false);
    }
  }, [deleteDialog.category, onDeleteCategory, handleCloseDeleteDialog]);

  // Keyboard navigation handler — uses flat index into flatOrder
  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent, category: BudgetCategory) => {
      const index = flatOrder.findIndex(c => c.id === category.id);
      if (index === -1) return;
      switch (e.key) {
        case 'ArrowDown':
          e.preventDefault();
          if (index < flatOrder.length - 1) {
            categoryRefs.current[index + 1]?.focus();
          }
          break;
        case 'ArrowUp':
          e.preventDefault();
          if (index > 0) {
            categoryRefs.current[index - 1]?.focus();
          }
          break;
        case 'Enter':
        case ' ':
          e.preventDefault();
          handleOpenEditForm(category);
          break;
        case 'Delete':
          e.preventDefault();
          handleOpenDeleteDialog(category);
          break;
      }
    },
    [flatOrder, handleOpenEditForm, handleOpenDeleteDialog]
  );

  const hasCategories = categories.length > 0;

  // Top-level categories only (#1203/#1630) — otherwise a sub-allocation child summed
  // alongside its already-inclusive parent falsely trips the form's over-allocation warning.
  const totalAllocatedExcludingCurrent = useMemo(() => {
    const topLevel = filterTopLevel(categories);
    if (formDialog.mode === 'create' || !formDialog.category) {
      return topLevel.reduce((sum, c) => sum + c.allocated_amount, 0);
    }
    return topLevel
      .filter((c) => c.id !== formDialog.category!.id)
      .reduce((sum, c) => sum + c.allocated_amount, 0);
  }, [formDialog.mode, formDialog.category, categories])

  // Issue #1630 Task 4 (and the follow-up adversarial-review fix): when the category pending
  // deletion is a parent, its DIRECT sub-allocation children are promoted to top-level and the
  // budget total changes by the parent's own unallocated remainder — say so in the delete-confirm
  // copy rather than leaving it silent.
  //
  // `isSubAllocationOfParent` without `directOnly` also recognizes a transitive GRANDCHILD (a
  // child of a child, attributed to the root via custom_grandparent_pfc_primary) as "belonging"
  // to the root for total-exclusion purposes. But deleting the root does NOT promote a
  // grandchild to top level — only the root's direct child is promoted; the grandchild stays
  // exactly where it is, nested under that (still-existing) direct child. So the promoted-count
  // copy and the remainder arithmetic must use `directOnly: true` here, or they wrongly claim the
  // grandchild also moves to top level and double-subtract its allocation (which is already
  // inside its direct parent's own allocated_amount, not the root's).
  const deleteDialogDirectChildren = useMemo(() => {
    if (!deleteDialog.category) return [];
    return categories.filter((c) =>
      isSubAllocationOfParent(c, deleteDialog.category!, { directOnly: true })
    );
  }, [categories, deleteDialog.category]);

  // Transitive grandchildren of the category pending deletion — these are NOT promoted; they
  // remain nested under their own direct parent. Tracked only so the confirm copy can describe
  // their fate accurately instead of staying silent about them.
  const deleteDialogNestedGrandchildren = useMemo(() => {
    if (!deleteDialog.category) return [];
    return categories.filter(
      (c) =>
        isSubAllocationOfParent(c, deleteDialog.category!) &&
        !isSubAllocationOfParent(c, deleteDialog.category!, { directOnly: true })
    );
  }, [categories, deleteDialog.category]);

  const deleteDialogRemainder = useMemo(() => {
    if (!deleteDialog.category) return 0;
    return computeParentAllocationRemainder(deleteDialog.category, deleteDialogDirectChildren);
  }, [deleteDialog.category, deleteDialogDirectChildren]);

  // Resolve a custom category's display name, with a placeholder during loading/error
  // so a raw UUID is never shown (#1203).
  const getCustomCategoryName = useCallback(
    (customId: string | undefined) =>
      customId ? (customCategoryMap.get(customId)?.name ?? 'Custom category') : undefined,
    [customCategoryMap]
  );

  // Display label for a budget category (custom name or PFC label), used as the parent
  // label on earmarked sub-allocation children (#1203).
  const categoryDisplayLabel = useCallback(
    (category: BudgetCategory): string => {
      if (category.category_kind === 'custom' && category.custom_category_id) {
        return getCustomCategoryName(category.custom_category_id) || formatBudgetCategoryLabel(category);
      }
      return formatBudgetCategoryLabel(category);
    },
    [getCustomCategoryName]
  );

  const renderCategoryItem = useCallback(
    (
      category: BudgetCategory,
      variant: 'standalone' | 'parent' | 'child' | 'savings',
      flatIndex: number,
      parentLabel?: string,
      childCategories?: BudgetCategory[]
    ) => (
      <div
        key={category.id}
        role="listitem"
        tabIndex={0}
        onKeyDown={(e) => handleKeyDown(e, category)}
        ref={(el) => {
          categoryRefs.current[flatIndex] = el;
        }}
      >
        <BudgetCategoryCard
          category={category}
          variant={variant}
          customCategoryName={getCustomCategoryName(category.custom_category_id)}
          parentLabel={variant === 'child' ? parentLabel : undefined}
          childCategories={variant === 'parent' ? childCategories : undefined}
          onEdit={() => handleOpenEditForm(category)}
          onDelete={() => handleOpenDeleteDialog(category)}
          disabled={disabled}
        />
      </div>
    ),
    [handleKeyDown, handleOpenEditForm, handleOpenDeleteDialog, disabled, getCustomCategoryName]
  );

  return (
    <div className="flex flex-col gap-6">
      {/* Header with add button */}
      <div className="flex justify-between items-center gap-4">
        <h2 className="text-lg font-semibold text-foreground m-0">Budget Categories</h2>
        <div className="flex items-center gap-2">
          <button
            type="button"
            onClick={() => setManageOpen(true)}
            disabled={disabled}
            className="inline-flex items-center gap-1.5 px-3 py-2 text-sm font-medium text-foreground bg-transparent border border-border rounded-lg cursor-pointer transition-colors duration-150 hover:enabled:bg-accent disabled:opacity-50 disabled:cursor-not-allowed"
          >
            <SettingsIcon className="w-4 h-4" />
            Manage categories
          </button>
          <button
            type="button"
            onClick={handleOpenAddForm}
            disabled={disabled}
            className="inline-flex items-center gap-1.5 px-4 py-2 text-sm font-medium text-white bg-primary border-none rounded-lg cursor-pointer transition-colors duration-150 hover:enabled:bg-primary/90 disabled:opacity-50 disabled:cursor-not-allowed"
          >
            <PlusIcon className="w-4 h-4" />
            Add Category
          </button>
        </div>
      </div>

      {/* Custom category manager launcher dialog (#1203) */}
      <Dialog open={manageOpen} onOpenChange={setManageOpen}>
        <DialogContent className="max-w-2xl">
          <DialogHeader>
            <DialogTitle>Manage custom categories</DialogTitle>
            <DialogDescription>
              Create, rename, recolor, nest, and delete your custom categories. Changes apply
              everywhere they&apos;re used.
            </DialogDescription>
          </DialogHeader>
          <Suspense fallback={<p className="text-sm text-muted-foreground">Loading categories…</p>}>
            <CustomCategoryManager />
          </Suspense>
        </DialogContent>
      </Dialog>

      {/* Allocation breakdown */}
      {hasCategories && (
        <AllocationBreakdown
          categories={categories}
          totalIncome={totalIncome}
          unallocated={unallocatedFunds}
          customCategoryMap={customCategoryMap}
        />
      )}

      {/* Categories list or empty state */}
      {hasCategories ? (
        <div className="flex flex-col gap-6">
          {/* Spending categories */}
          {groups.length > 0 && (
            <div className="flex flex-col gap-4" role="list">
              {groups.map(({ group, children }) => {
                const hasChildren = children.length > 0;
                const groupKey = group?.id ?? `orphan-${children[0]?.id}`;

                if (!group) {
                  return children.map((child) =>
                    renderCategoryItem(child, 'standalone', flatOrder.findIndex(c => c.id === child.id))
                  );
                }

                const parentVariant = hasChildren ? 'parent' : 'standalone';
                const parentIdx = flatOrder.findIndex(c => c.id === group.id);
                return (
                  <div key={groupKey}>
                    {renderCategoryItem(group, parentVariant, parentIdx, undefined, children)}
                    {children.map((child) => {
                      const childIdx = flatOrder.findIndex(c => c.id === child.id);
                      return renderCategoryItem(child, 'child', childIdx, categoryDisplayLabel(group));
                    })}
                  </div>
                );
              })}
            </div>
          )}

          {/* Savings buckets (#1300) */}
          {savingsCategories.length > 0 && (
            <div className="flex flex-col gap-3">
              <h3 className="text-sm font-semibold text-muted-foreground uppercase tracking-wide">
                Savings Buckets
              </h3>
              <div className="flex flex-col gap-3" role="list">
                {savingsCategories.map((cat) =>
                  renderCategoryItem(cat, 'savings', flatOrder.findIndex(c => c.id === cat.id))
                )}
              </div>
            </div>
          )}
        </div>
      ) : (
        <EmptyState
          icon={<LayoutList />}
          title="No categories yet."
          description="Add a category to start tracking your budget allocations."
        />
      )}

      {/* Add/Edit Form Dialog */}
      <BudgetCategoryForm
        isOpen={formDialog.isOpen}
        onClose={handleCloseForm}
        onSubmit={handleFormSubmit}
        mode={formDialog.mode}
        initialData={formDialog.category || undefined}
        availableTags={availableTags}
        expectedIncome={totalIncome}
        totalAllocatedExcludingCurrent={totalAllocatedExcludingCurrent}
      />

      {/*
        Delete Confirmation Dialog — Issue #960.

        Issue #1569: this dialog was silent about what survives. `DeleteCategory` tombstones the
        row (`deleted_at = NOW()`), and the spending already recorded against it — along with the
        `budget_period_events` and `rollover_history` rows — is kept. "This action cannot be undone"
        stays because it is true: nothing in this system sets `deleted_at = NULL`. Retained is not
        recoverable, so the copy says the history is kept and promises nothing about getting the
        category itself back.
      */}
      <ConfirmationDialog
        open={deleteDialog.isOpen}
        onOpenChange={(open) => !open && handleCloseDeleteDialog()}
        onConfirm={handleConfirmDelete}
        title="Delete Category"
        description={
          deleteDialog.category ? (
            <>
              Are you sure you want to delete the category &ldquo;
              <span data-ph-mask>{categoryDisplayLabel(deleteDialog.category)}</span>&rdquo;? It
              leaves this budget, but nothing is thrown away &mdash; the spending already
              recorded against it is kept.
              {deleteDialogDirectChildren.length > 0 && (
                <>
                  {' '}Its {deleteDialogDirectChildren.length === 1 ? '1 child category' : `${deleteDialogDirectChildren.length} child categories`} will move up to the top level of
                  this budget, and the budget&apos;s total allocation will change by{' '}
                  <span data-ph-mask>{formatCurrency(deleteDialogRemainder)}</span> (this
                  category&apos;s own unallocated remainder).
                  {deleteDialogNestedGrandchildren.length > 0 && (
                    <>
                      {' '}
                      {deleteDialogNestedGrandchildren.length === 1
                        ? 'A category nested beneath that child will stay nested right where it is'
                        : `${deleteDialogNestedGrandchildren.length} categories nested beneath those children will stay nested right where they are`}
                      {' '}&mdash; only its direct{' '}
                      {deleteDialogDirectChildren.length === 1 ? 'child moves' : 'children move'} up.
                    </>
                  )}
                </>
              )}
              {' '}This action cannot be undone.
            </>
          ) : ''
        }
        variant="destructive"
        loading={isDeleting}
      />
    </div>
  );
}
