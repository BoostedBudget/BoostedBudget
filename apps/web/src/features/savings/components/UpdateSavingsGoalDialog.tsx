/**
 * UpdateSavingsGoalDialog
 * Issue #954: Edit dialog for existing savings goals
 */

import { useState, useCallback } from 'react';
import { cn } from '@/lib/utils';
import { XIcon } from 'lucide-react';
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { useUpdateSavingsGoal } from '@boostedbudget/api-client/endpoints/savings-goals/savings-goals';
import type { SavingsGoal, UpdateSavingsGoalRequest, ProgressBasis } from '../types';
import { PROGRESS_BASIS_LABELS } from '../types';
import { TextField, NumberField, DateField, SelectField } from './form';
import { useTrackEvent } from '@/shared/hooks/useTrackEvent';
import { ANALYTICS_EVENTS } from '@/shared/analytics/events';

export interface UpdateSavingsGoalDialogProps {
  isOpen: boolean;
  goal: SavingsGoal | null;
  onCancel: () => void;
  onGoalUpdated: (goal: SavingsGoal) => void;
}

interface FormData {
  name: string;
  description: string;
  target_amount: string;
  deadline: string;
  progress_basis: ProgressBasis;
  account_percentage: string;
}

interface FormErrors {
  name?: string;
  target_amount?: string;
  account_percentage?: string;
}

function todayLocalDate(): string {
  const d = new Date()
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}

function goalToFormData(goal: SavingsGoal): FormData {
  return {
    name: goal.name,
    description: goal.description ?? '',
    target_amount: String(goal.target_amount),
    deadline: goal.deadline ? goal.deadline.split('T')[0] : '',
    progress_basis: goal.progress_basis ?? 'manual',
    // API stores 0-1, UI shows 0-100
    account_percentage: goal.account_percentage != null ? String(goal.account_percentage * 100) : '',
  }
}

export function UpdateSavingsGoalDialog({
  isOpen,
  goal,
  onCancel,
  onGoalUpdated,
}: UpdateSavingsGoalDialogProps) {
  const [formData, setFormData] = useState<FormData>(() =>
    goal
      ? goalToFormData(goal)
      : { name: '', description: '', target_amount: '', deadline: '', progress_basis: 'manual', account_percentage: '' },
  )
  const [errors, setErrors] = useState<FormErrors>({});
  const [apiError, setApiError] = useState<string | null>(null);

  const trackEvent = useTrackEvent();
  const updateMutation = useUpdateSavingsGoal();

  const isSubmitting = updateMutation.isPending;

  const handleInputChange = useCallback(
    (e: React.ChangeEvent<HTMLInputElement | HTMLTextAreaElement | HTMLSelectElement>) => {
      const { name, value } = e.target;
      setFormData((prev) => ({ ...prev, [name]: value }));
      if (errors[name as keyof FormErrors]) {
        setErrors((prev) => ({ ...prev, [name]: undefined }));
      }
    },
    [errors]
  );

  const validate = useCallback((): boolean => {
    const newErrors: FormErrors = {};

    if (!formData.name.trim()) {
      newErrors.name = 'Name is required';
    }

    const amount = parseFloat(formData.target_amount);
    if (isNaN(amount) || amount <= 0) {
      newErrors.target_amount = 'Amount must be greater than 0';
    }

    if (formData.progress_basis === 'account_percentage') {
      const pct = parseFloat(formData.account_percentage);
      if (isNaN(pct) || pct <= 0 || pct > 100) {
        newErrors.account_percentage = 'Enter a percentage between 0 and 100 (exclusive)';
      }
    }

    setErrors(newErrors);
    return Object.keys(newErrors).length === 0;
  }, [formData]);

  const handleSubmit = useCallback(
    async (e: React.FormEvent) => {
      e.preventDefault();
      setApiError(null);

      if (!goal || !validate()) {
        return;
      }

      try {
        const request: UpdateSavingsGoalRequest = {
          name: formData.name.trim(),
          description: formData.description.trim() || undefined,
          target_amount: parseFloat(formData.target_amount),
          deadline: formData.deadline ? new Date(formData.deadline).toISOString() : undefined,
          // Issue #1300 gap G1: include basis fields so non-manual modes work end-to-end.
          progress_basis: formData.progress_basis,
          account_percentage:
            formData.progress_basis === 'account_percentage' && formData.account_percentage
              ? parseFloat(formData.account_percentage) / 100 // UI shows 0-100%; API expects 0-1
              : undefined,
        };

        const updated = await updateMutation.mutateAsync({ id: goal.id, data: request as never });
        trackEvent(ANALYTICS_EVENTS.SAVINGS_GOAL_EDITED);
        onGoalUpdated(updated as SavingsGoal);
      } catch (err) {
        setApiError(err instanceof Error ? err.message : 'Failed to update goal');
      }
    },
    [goal, formData, validate, onGoalUpdated, updateMutation, trackEvent]
  );

  const handleProgressBasisChange = useCallback(
    (e: React.ChangeEvent<HTMLSelectElement>) => {
      const value = e.target.value as ProgressBasis;
      setFormData((prev) => ({
        ...prev,
        progress_basis: value,
        // Clear percentage when switching away from account_percentage
        account_percentage: value === 'account_percentage' ? prev.account_percentage : '',
      }));
      if (errors.account_percentage) {
        setErrors((prev) => ({ ...prev, account_percentage: undefined }));
      }
      trackEvent(ANALYTICS_EVENTS.SAVINGS_GOAL_PROGRESS_BASIS_CHANGED);
    },
    [errors.account_percentage, trackEvent]
  );

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
        className="update-goal-dialog w-full max-w-lg p-6 bg-card rounded-2xl"
        onInteractOutside={(e) => e.preventDefault()}
        onPointerDownOutside={(e) => e.preventDefault()}
        showCloseButton={false}
      >
        <DialogHeader className="flex flex-row items-center justify-between mb-6">
          <DialogTitle className="text-xl font-semibold text-foreground">
            Edit Savings Goal
          </DialogTitle>
          <button
            type="button"
            onClick={onCancel}
            className="text-muted-foreground hover:text-foreground"
          >
            <XIcon className="h-6 w-6" />
          </button>
        </DialogHeader>

        <form onSubmit={handleSubmit} className="space-y-4">
          <TextField
            name="name"
            label="Name"
            value={formData.name}
            onChange={handleInputChange}
            error={errors.name}
            placeholder="e.g., Vacation Fund"
          />

          <div>
            <label
              htmlFor="description"
              className="block text-sm font-medium text-foreground"
            >
              Description
            </label>
            <textarea
              id="description"
              name="description"
              value={formData.description}
              onChange={handleInputChange}
              rows={2}
              className="w-full py-[0.625rem] px-3 mt-1 border border-input rounded-lg text-sm bg-card text-foreground transition-[border-color,box-shadow] focus:outline-none focus:border-primary focus:ring-2 focus:ring-ring placeholder:text-muted-foreground"
              placeholder="What are you saving for?"
            />
          </div>

          <NumberField
            name="target_amount"
            label="Target Amount"
            value={formData.target_amount}
            onChange={handleInputChange}
            error={errors.target_amount}
            prefix="$"
            placeholder="0.00"
            min="0"
            step="0.01"
          />

          <DateField
            name="deadline"
            label="Deadline (optional)"
            value={formData.deadline}
            onChange={handleInputChange}
            min={todayLocalDate()}
          />

          {/* Issue #1300 gap G1: progress tracking basis */}
          <SelectField
            name="progress_basis"
            label="Progress Tracking"
            value={formData.progress_basis}
            onChange={handleProgressBasisChange}
          >
            {(Object.entries(PROGRESS_BASIS_LABELS) as [ProgressBasis, string][]).map(([value, label]) => (
              <option key={value} value={value}>{label}</option>
            ))}
          </SelectField>

          {formData.progress_basis === 'account_percentage' && (
            <div>
              <label htmlFor="account_percentage_update" className="block text-sm font-medium text-foreground">
                Account % (e.g. 50 for 50%)
              </label>
              <div className="relative">
                <input
                  type="number"
                  id="account_percentage_update"
                  name="account_percentage"
                  value={formData.account_percentage}
                  onChange={handleInputChange}
                  placeholder="e.g. 50"
                  min="0"
                  max="100"
                  step="0.01"
                  className={cn(
                    'w-full py-[0.625rem] px-3 mt-1 border border-input rounded-lg text-sm bg-card text-foreground transition-[border-color,box-shadow] focus:outline-none focus:border-primary focus:ring-2 focus:ring-ring placeholder:text-muted-foreground pr-8',
                    errors.account_percentage && 'border-destructive focus:border-destructive'
                  )}
                />
                <span className="absolute right-3 top-1/2 -translate-y-1/2 text-muted-foreground text-sm pointer-events-none">%</span>
              </div>
              {errors.account_percentage && (
                <p className="mt-1 text-sm text-destructive">{errors.account_percentage}</p>
              )}
            </div>
          )}

          {apiError && (
            <div className="p-3 rounded-lg bg-destructive/10 text-destructive text-sm">
              {apiError}
            </div>
          )}

          <div className="flex justify-end gap-3 pt-4">
            <button
              type="button"
              onClick={onCancel}
              className="px-4 py-2 text-sm font-medium text-foreground bg-muted rounded-lg hover:bg-muted/80 transition-colors"
            >
              Cancel
            </button>
            <button
              type="submit"
              disabled={isSubmitting}
              className="px-4 py-2 text-sm font-medium text-primary-foreground bg-primary rounded-lg hover:bg-primary/90 disabled:opacity-50 disabled:cursor-not-allowed transition-colors"
            >
              {isSubmitting ? 'Saving...' : 'Save Changes'}
            </button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}
