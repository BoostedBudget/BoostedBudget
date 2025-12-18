/**
 * Delete Budget Confirmation Dialog
 * Issue #521: Enable Budget and Savings Goal Deletion in UI
 *
 * Confirmation modal before deleting a budget.
 * Shows CASCADE warning about categories being deleted.
 * Requires user to type the exact budget name to confirm deletion.
 * Uses shadcn/ui Dialog built on Radix UI.
 */

import { useState, useCallback } from 'react';
import { AlertTriangle, Loader2 } from 'lucide-react';
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';

export interface DeleteBudgetDialogProps {
  isOpen: boolean;
  budgetName: string;
  categoryCount: number;
  onConfirm: () => void;
  onCancel: () => void;
  isDeleting: boolean;
}

export function DeleteBudgetDialog({
  isOpen,
  budgetName,
  categoryCount,
  onConfirm,
  onCancel,
  isDeleting,
}: DeleteBudgetDialogProps) {
  const [confirmationInput, setConfirmationInput] = useState('');

  const handleOpenChange = useCallback(
    (open: boolean) => {
      if (!open && !isDeleting) {
        setConfirmationInput(''); // Clear input when dialog closes
        onCancel();
      }
    },
    [onCancel, isDeleting]
  );

  const handleInputChange = useCallback(
    (e: React.ChangeEvent<HTMLInputElement>) => {
      setConfirmationInput(e.target.value);
    },
    []
  );

  const isConfirmationValid = confirmationInput === budgetName;
  const canDelete = isConfirmationValid && !isDeleting;

  const categoryText =
    categoryCount === 1 ? '1 category' : `${categoryCount} categories`;

  return (
    <Dialog open={isOpen} onOpenChange={handleOpenChange}>
      <DialogContent
        className="w-full max-w-md p-6 bg-white dark:bg-gray-900 rounded-2xl"
        showCloseButton={false}
      >
        <DialogHeader>
          <div className="flex items-start gap-4">
            <div className="flex-shrink-0 p-2 rounded-full bg-red-100 dark:bg-red-900/30">
              <AlertTriangle className="h-6 w-6 text-red-600 dark:text-red-400" />
            </div>
            <div>
              <DialogTitle className="text-lg font-semibold text-gray-900 dark:text-white">
                Delete Budget
              </DialogTitle>
              <p className="mt-2 text-sm text-gray-600 dark:text-gray-400">
                Are you sure you want to delete "{budgetName}"? This budget and
                its {categoryText} will be permanently deleted. This action
                cannot be undone.
              </p>
            </div>
          </div>
        </DialogHeader>

        <div className="mt-4">
          <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
            To confirm deletion, type "{budgetName}" below:
          </label>
          <input
            type="text"
            value={confirmationInput}
            onChange={handleInputChange}
            disabled={isDeleting}
            placeholder="Type name to confirm"
            aria-label="Confirmation input"
            className="w-full px-3 py-2 text-sm border border-gray-300 dark:border-gray-600 rounded-lg
                       bg-white dark:bg-gray-800 text-gray-900 dark:text-white
                       placeholder-gray-400 dark:placeholder-gray-500
                       focus:outline-none focus:ring-2 focus:ring-red-500 focus:border-transparent
                       disabled:opacity-50 disabled:cursor-not-allowed"
          />
        </div>

        <div className="mt-6 flex justify-end gap-3">
          <button
            onClick={() => handleOpenChange(false)}
            disabled={isDeleting}
            className="px-4 py-2 text-sm font-medium text-gray-700 dark:text-gray-200 bg-gray-100 dark:bg-gray-800 rounded-lg hover:bg-gray-200 dark:hover:bg-gray-700 transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
          >
            Cancel
          </button>
          <button
            onClick={onConfirm}
            disabled={!canDelete}
            className="px-4 py-2 text-sm font-medium text-white bg-red-600 rounded-lg hover:bg-red-700 transition-colors disabled:opacity-50 disabled:cursor-not-allowed flex items-center gap-2"
          >
            {isDeleting ? (
              <>
                <Loader2 className="h-4 w-4 animate-spin" />
                Deleting...
              </>
            ) : (
              'Delete'
            )}
          </button>
        </div>
      </DialogContent>
    </Dialog>
  );
}
