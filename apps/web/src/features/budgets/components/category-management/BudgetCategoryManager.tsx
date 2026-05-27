/**
 * BudgetCategoryManager
 * Issue #497: Budget Category Manager + Match Config UI
 * Issue #1200: Multi-level category hierarchy — grouped visual layout
 *
 * Container component for managing budget categories with CRUD operations.
 * Includes add/edit form dialog, delete confirmation, and allocation breakdown.
 * Categories are rendered in a primary→children hierarchy using groupBudgetCategoriesByPrimary.
 */

import { useState, useCallback, useMemo, useRef } from 'react';
import { PlusIcon } from 'lucide-react';
import { ConfirmationDialog } from '@/components/ui/confirmation-dialog';
import type {
  BudgetCategory,
  CreateBudgetCategoryRequest,
  UpdateBudgetCategoryRequest,
} from '../../types';
import { formatBudgetCategoryLabel } from '../../types';
import type { UserTag } from '@/features/tags/types';
import { BudgetCategoryCard } from './BudgetCategoryCard';
import { BudgetCategoryForm } from './BudgetCategoryForm';
import { AllocationBreakdown } from './AllocationBreakdown';
import { groupBudgetCategoriesByPrimary } from '../../utils/groupBudgetCategoriesByPrimary';

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
  /** Available Plaid categories for selection */
  availableCategories: string[][];
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
  availableCategories,
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

  // Refs for keyboard navigation — indexed by flat order position
  const categoryRefs = useRef<(HTMLDivElement | null)[]>([]);

  // Calculate unallocated funds
  const unallocatedFunds = useMemo(() => {
    const totalAllocated = categories.reduce((sum, cat) => sum + cat.allocated_amount, 0);
    return totalIncome - totalAllocated;
  }, [categories, totalIncome]);

  // Filter out already-used categories for the form dropdown
  const filteredAvailableCategories = useMemo(() => {
    const usedLabels = new Set(categories.map(formatBudgetCategoryLabel));
    return availableCategories.filter(
      (cat) => !usedLabels.has(cat.join(' > '))
    );
  }, [availableCategories, categories]);

  // Grouped hierarchy for visual layout
  const groups = useMemo(
    () => groupBudgetCategoriesByPrimary(categories),
    [categories]
  );

  // Flat order of all categories in render order — for keyboard navigation
  const flatOrder = useMemo(
    () => groups.flatMap(({ group, children }) => (group ? [group, ...children] : children)),
    [groups]
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

  const totalAllocatedExcludingCurrent = useMemo(() => {
    if (formDialog.mode === 'create' || !formDialog.category) {
      return categories.reduce((sum, c) => sum + c.allocated_amount, 0);
    }
    return categories
      .filter((c) => c.id !== formDialog.category!.id)
      .reduce((sum, c) => sum + c.allocated_amount, 0);
  }, [formDialog.mode, formDialog.category, categories])

  const renderCategoryItem = useCallback(
    (category: BudgetCategory, variant: 'standalone' | 'parent' | 'child', flatIndex: number) => (
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
          onEdit={() => handleOpenEditForm(category)}
          onDelete={() => handleOpenDeleteDialog(category)}
          disabled={disabled}
        />
      </div>
    ),
    [handleKeyDown, handleOpenEditForm, handleOpenDeleteDialog, disabled]
  );

  return (
    <div className="flex flex-col gap-6">
      {/* Header with add button */}
      <div className="flex justify-between items-center gap-4">
        <h2 className="text-lg font-semibold text-foreground m-0">Budget Categories</h2>
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

      {/* Allocation breakdown */}
      {hasCategories && (
        <AllocationBreakdown
          categories={categories}
          totalIncome={totalIncome}
          unallocated={unallocatedFunds}
        />
      )}

      {/* Categories list or empty state */}
      {hasCategories ? (
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
                {renderCategoryItem(group, parentVariant, parentIdx)}
                {children.map((child) => {
                  const childIdx = flatOrder.findIndex(c => c.id === child.id);
                  return renderCategoryItem(child, 'child', childIdx);
                })}
              </div>
            );
          })}
        </div>
      ) : (
        <div className="p-8 text-center bg-muted border border-dashed border-border rounded-lg">
          <p className="text-sm text-muted-foreground m-0">
            No categories yet. Add a category to start tracking your budget allocations.
          </p>
        </div>
      )}

      {/* Add/Edit Form Dialog */}
      <BudgetCategoryForm
        isOpen={formDialog.isOpen}
        onClose={handleCloseForm}
        onSubmit={handleFormSubmit}
        mode={formDialog.mode}
        initialData={formDialog.category || undefined}
        availableCategories={
          formDialog.mode === 'edit' ? availableCategories : filteredAvailableCategories
        }
        availableTags={availableTags}
        expectedIncome={totalIncome}
        totalAllocatedExcludingCurrent={totalAllocatedExcludingCurrent}
      />

      {/* Delete Confirmation Dialog — Issue #960 */}
      <ConfirmationDialog
        open={deleteDialog.isOpen}
        onOpenChange={(open) => !open && handleCloseDeleteDialog()}
        onConfirm={handleConfirmDelete}
        title="Delete Category"
        description={deleteDialog.category ? `Are you sure you want to delete the category "${formatBudgetCategoryLabel(deleteDialog.category)}"? This action cannot be undone.` : ''}
        variant="destructive"
        loading={isDeleting}
      />
    </div>
  );
}
