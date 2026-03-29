/**
 * Remove Widget Confirmation Dialog
 * Issue #412: Dashboard widget UX improvements
 * Issue #462: Migrated from HeadlessUI to shadcn/ui
 *
 * Confirmation modal before removing a widget from the dashboard.
 * Uses shadcn/ui Dialog built on Radix UI.
 */

import { useCallback } from 'react';
import { AlertTriangle } from 'lucide-react';
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';

interface RemoveWidgetDialogProps {
  isOpen: boolean;
  widgetTitle: string;
  onConfirm: () => void;
  onCancel: () => void;
}

export function RemoveWidgetDialog({
  isOpen,
  widgetTitle,
  onConfirm,
  onCancel,
}: RemoveWidgetDialogProps) {
  // Handle dialog open state changes - click outside closes the dialog
  const handleOpenChange = useCallback(
    (open: boolean) => {
      if (!open) {
        onCancel();
      }
    },
    [onCancel]
  );

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
                Remove Widget
              </DialogTitle>
              <p className="mt-2 text-sm text-gray-600 dark:text-gray-400">
                Are you sure you want to remove "{widgetTitle}" from your dashboard?
                You can add it back anytime from the widget catalog.
              </p>
            </div>
          </div>
        </DialogHeader>

        <div className="mt-6 flex justify-end gap-3">
          <button
            onClick={onCancel}
            className="px-4 py-2 text-sm font-medium text-gray-700 dark:text-gray-200 bg-gray-100 dark:bg-gray-800 rounded-lg hover:bg-gray-200 dark:hover:bg-gray-700 transition-colors"
          >
            Cancel
          </button>
          <button
            onClick={onConfirm}
            className="px-4 py-2 text-sm font-medium text-white bg-red-600 rounded-lg hover:bg-red-700 transition-colors"
          >
            Remove
          </button>
        </div>
      </DialogContent>
    </Dialog>
  );
}
