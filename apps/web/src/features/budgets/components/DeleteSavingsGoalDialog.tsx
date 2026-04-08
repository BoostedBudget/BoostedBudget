/**
 * Delete Savings Goal Confirmation Dialog
 * Issue #521: Enable Budget and Savings Goal Deletion in UI
 *
 * Confirmation modal before deleting a savings goal.
 * Shows current progress information.
 * Requires user to type the exact goal name to confirm deletion.
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

export interface DeleteSavingsGoalDialogProps {
  isOpen: boolean;
  goalName: string;
  currentAmount: number;
  targetAmount: number;
  currencyCode: string;
  onConfirm: () => void;
  onCancel: () => void;
  isDeleting: boolean;
}

function formatCurrency(amount: number, currencyCode: string): string {
  try {
    return new Intl.NumberFormat('en-US', {
      style: 'currency',
      currency: currencyCode,
      minimumFractionDigits: 2,
      maximumFractionDigits: 2,
    }).format(amount);
  } catch {
    return `${currencyCode} ${amount.toFixed(2)}`;
  }
}

export function DeleteSavingsGoalDialog({
  isOpen,
  goalName,
  currentAmount,
  targetAmount,
  currencyCode,
  onConfirm,
  onCancel,
  isDeleting,
}: DeleteSavingsGoalDialogProps) {
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

  const isConfirmationValid = confirmationInput === goalName;
  const canDelete = isConfirmationValid && !isDeleting;

  const progress =
    targetAmount > 0 ? Math.round((currentAmount / targetAmount) * 100) : 0;

  return (
    <Dialog open={isOpen} onOpenChange={handleOpenChange}>
      <DialogContent
        className="w-full max-w-md p-6 bg-card rounded-2xl"
        showCloseButton={false}
      >
        <DialogHeader>
          <div className="flex items-start gap-4">
            <div className="flex-shrink-0 p-2 rounded-full bg-red-100 dark:bg-red-900/30">
              <AlertTriangle className="h-6 w-6 text-red-600 dark:text-red-400" />
            </div>
            <div>
              <DialogTitle className="text-lg font-semibold text-foreground">
                Delete Savings Goal
              </DialogTitle>
              <p className="mt-2 text-sm text-muted-foreground">
                Are you sure you want to delete "{goalName}"? You have saved{' '}
                {formatCurrency(currentAmount, currencyCode)} of{' '}
                {formatCurrency(targetAmount, currencyCode)} ({progress}%). This
                action cannot be undone.
              </p>
            </div>
          </div>
        </DialogHeader>

        <div className="mt-4">
          <label className="block text-sm font-medium text-foreground mb-2">
            To confirm deletion, type "{goalName}" below:
          </label>
          <input
            type="text"
            value={confirmationInput}
            onChange={handleInputChange}
            disabled={isDeleting}
            placeholder="Type name to confirm"
            aria-label="Confirmation input"
            className="w-full px-3 py-2 text-sm border border-border rounded-lg
                       bg-background text-foreground
                       placeholder:text-muted-foreground
                       focus:outline-none focus:ring-2 focus:ring-red-500 focus:border-transparent
                       disabled:opacity-50 disabled:cursor-not-allowed"
          />
        </div>

        <div className="mt-6 flex justify-end gap-3">
          <button
            onClick={() => handleOpenChange(false)}
            disabled={isDeleting}
            className="px-4 py-2 text-sm font-medium text-foreground bg-muted rounded-lg hover:bg-muted/80 transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
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
