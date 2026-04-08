/**
 * BudgetCategoryManager
 * Issue #497: Budget Category Manager + Match Config UI
 *
 * Container component for managing budget categories with CRUD operations.
 * Includes add/edit form dialog, delete confirmation, and allocation breakdown.
 */

import { useState, useCallback, useMemo, useRef } from 'react';
import { PlusIcon } from 'lucide-react';
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from '@/components/ui/dialog';
import type {
  BudgetCategory,
  CreateBudgetCategoryRequest,
  UpdateBudgetCategoryRequest,
} from '../../types';
import { formatCategoryPath } from '../../types';
import type { UserTag } from '@/features/tags/types';
import { BudgetCategoryCard } from './BudgetCategoryCard';
import { BudgetCategoryForm } from './BudgetCategoryForm';
import { AllocationBreakdown } from './AllocationBreakdown';

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

  // Refs for keyboard navigation (Phase 4 Accessibility)
  const categoryRefs = useRef<(HTMLDivElement | null)[]>([]);

  // Calculate unallocated funds
  const unallocatedFunds = useMemo(() => {
    const totalAllocated = categories.reduce((sum, cat) => sum + cat.allocated_amount, 0);
    return totalIncome - totalAllocated;
  }, [categories, totalIncome]);

  // Filter out already-used categories for the form dropdown
  const filteredAvailableCategories = useMemo(() => {
    const usedCategoryPaths = new Set(
      categories.map((cat) => formatCategoryPath(cat.category))
    );
    return availableCategories.filter(
      (cat) => !usedCategoryPaths.has(formatCategoryPath(cat))
    );
  }, [availableCategories, categories]);

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
    setFormDialog({
      isOpen: false,
      mode: 'create',
      category: null,
    });
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
    setDeleteDialog({
      isOpen: false,
      category: null,
    });
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

  // Keyboard navigation handler (Phase 4 Accessibility)
  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent, index: number, category: BudgetCategory) => {
      switch (e.key) {
        case 'ArrowDown':
          e.preventDefault();
          if (index < categories.length - 1) {
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
    [categories.length, handleOpenEditForm, handleOpenDeleteDialog]
  );

  const hasCategories = categories.length > 0;

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
        <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3" role="list">
          {categories.map((category, index) => (
            <div
              key={category.id}
              role="listitem"
              tabIndex={0}
              onKeyDown={(e) => handleKeyDown(e, index, category)}
              ref={(el) => {
                categoryRefs.current[index] = el;
              }}
            >
              <BudgetCategoryCard
                category={category}
                onEdit={() => handleOpenEditForm(category)}
                onDelete={() => handleOpenDeleteDialog(category)}
                disabled={disabled}
              />
            </div>
          ))}
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
      />

      {/* Delete Confirmation Dialog */}
      <Dialog open={deleteDialog.isOpen} onOpenChange={(open) => !open && handleCloseDeleteDialog()}>
        <DialogContent className="max-w-md">
          <DialogHeader>
            <DialogTitle>Confirm Delete</DialogTitle>
            <DialogDescription>
              Are you sure you want to delete the category "
              {deleteDialog.category ? formatCategoryPath(deleteDialog.category.category) : ''}"?
              This action cannot be undone.
            </DialogDescription>
          </DialogHeader>
          <div className="flex justify-end gap-3 mt-4">
            <button
              type="button"
              onClick={handleCloseDeleteDialog}
              disabled={isDeleting}
              className="px-4 py-2 text-sm font-medium text-gray-700 dark:text-gray-200 bg-gray-100 dark:bg-gray-800 rounded-lg hover:bg-gray-200 dark:hover:bg-gray-700 transition-colors"
            >
              Cancel
            </button>
            <button
              type="button"
              onClick={handleConfirmDelete}
              disabled={isDeleting}
              className="px-4 py-2 text-sm font-medium text-white bg-red-600 rounded-lg hover:bg-red-700 disabled:opacity-50 disabled:cursor-not-allowed transition-colors"
            >
              {isDeleting ? 'Deleting...' : 'Delete'}
            </button>
          </div>
        </DialogContent>
      </Dialog>
    </div>
  );
}
