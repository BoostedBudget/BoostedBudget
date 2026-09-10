/**
 * CreateSavingsGoalDialog
 * Issue #954: Modal dialog for creating savings goals with template selection
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
import { useCreateSavingsGoal } from '@boostedbudget/api-client/endpoints/savings-goals/savings-goals';
import { useActiveAccounts } from '@/features/accounts/hooks';
import { useTrackEvent } from '@/shared/hooks/useTrackEvent';
import { ANALYTICS_EVENTS } from '@/shared/analytics/events';
import { GOAL_TEMPLATES, calculateDeadlineFromTemplate } from '../goalTemplates';
import type { GoalTemplate } from '../goalTemplates';
import type { Account } from '../../accounts/types';
import type { SavingsGoal, CreateSavingsGoalRequest, ProgressBasis } from '../types';
import { PROGRESS_BASIS_LABELS } from '../types';
import { TextField, NumberField, DateField, SelectField } from './form';

export interface CreateSavingsGoalDialogProps {
  isOpen: boolean;
  onCancel: () => void;
  onGoalCreated: (goal: SavingsGoal) => void;
}

interface FormData {
  name: string;
  description: string;
  target_amount: string;
  account_id: string;
  deadline: string;
  progress_basis: ProgressBasis;
  account_percentage: string;
}

interface FormErrors {
  name?: string;
  target_amount?: string;
  account_id?: string;
  account_percentage?: string;
}

function todayLocalDate(): string {
  const d = new Date()
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}

const initialFormData: FormData = {
  name: '',
  description: '',
  target_amount: '',
  account_id: '',
  deadline: '',
  progress_basis: 'contributions',
  account_percentage: '',
};

export function CreateSavingsGoalDialog({
  isOpen,
  onCancel,
  onGoalCreated,
}: CreateSavingsGoalDialogProps) {
  const [formData, setFormData] = useState<FormData>(initialFormData);
  const [errors, setErrors] = useState<FormErrors>({});
  const [apiError, setApiError] = useState<string | null>(null);
  const [selectedTemplate, setSelectedTemplate] = useState<string | null>(null);

  const {
    accounts: allAccounts,
    isLoading: accountsLoading,
  } = useActiveAccounts();

  const trackEvent = useTrackEvent();
  const createSavingsGoalMutation = useCreateSavingsGoal();

  // Issue #1109: useActiveAccounts excludes replaced accounts; also filter by depository type
  const accounts = (allAccounts ?? [])
    .filter((acc) => acc.type === 'depository') as Account[];

  const isSubmitting = createSavingsGoalMutation.isPending;

  const resetForm = useCallback(() => {
    setFormData(initialFormData);
    setErrors({});
    setApiError(null);
    setSelectedTemplate(null);
  }, []);

  const handleTemplateSelect = useCallback((template: GoalTemplate) => {
    setSelectedTemplate(template.id);
    setFormData((prev) => ({
      ...prev,
      name: template.id !== 'custom' ? template.name : prev.name,
      deadline: calculateDeadlineFromTemplate(template) || prev.deadline,
    }));
  }, []);

  const handleInputChange = useCallback(
    (e: React.ChangeEvent<HTMLInputElement | HTMLTextAreaElement | HTMLSelectElement>) => {
      const { name, value } = e.target;
      setFormData((prev) => ({ ...prev, [name]: value }));
      if (errors[name as keyof FormErrors]) {
        setErrors((prev) => ({ ...prev, [name]: undefined }));
      }
      if (name === 'progress_basis') {
        trackEvent(ANALYTICS_EVENTS.SAVINGS_GOAL_PROGRESS_BASIS_CHANGED, { new_basis: value });
      }
    },
    [errors, trackEvent]
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

    if (!formData.account_id) {
      newErrors.account_id = 'Please select an account';
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

      if (!validate()) {
        return;
      }

      try {
        const selectedAccount = accounts.find((acc) => acc.id === formData.account_id)
        const request: CreateSavingsGoalRequest = {
          name: formData.name.trim(),
          description: formData.description.trim() || undefined,
          target_amount: parseFloat(formData.target_amount),
          account_id: formData.account_id,
          user_account_id: selectedAccount?.user_account_id ?? undefined,
          deadline: formData.deadline ? new Date(formData.deadline).toISOString() : undefined,
          // #1625: always send the basis. This used to omit 'manual' and lean on the API
          // defaulting to manual — but a NEW goal now defaults to 'contributions', so omitting
          // it would silently turn an explicit "manual" choice into a contributions goal whose
          // current_amount the user can no longer set by hand.
          progress_basis: formData.progress_basis,
          account_percentage:
            formData.progress_basis === 'account_percentage' && formData.account_percentage
              ? parseFloat(formData.account_percentage) / 100
              : undefined,
        };

        const newGoal = await createSavingsGoalMutation.mutateAsync({ data: request });
        onGoalCreated(newGoal as SavingsGoal);
      } catch (err) {
        setApiError(err instanceof Error ? err.message : 'Failed to create goal');
      }
    },
    [formData, accounts, validate, onGoalCreated, createSavingsGoalMutation]
  );

  const handleOpenChange = useCallback(
    (open: boolean) => {
      if (!open) {
        resetForm();
        onCancel();
      }
    },
    [onCancel, resetForm]
  );

  return (
    <Dialog open={isOpen} onOpenChange={handleOpenChange}>
      <DialogContent
        className="create-goal-dialog w-full max-w-lg p-6 bg-card rounded-2xl"
        onInteractOutside={(e) => e.preventDefault()}
        onPointerDownOutside={(e) => e.preventDefault()}
        showCloseButton={false}
      >
        <DialogHeader className="flex flex-row items-center justify-between mb-6">
          <DialogTitle className="text-xl font-semibold text-foreground">
            Create Savings Goal
          </DialogTitle>
          <button
            type="button"
            onClick={onCancel}
            className="text-muted-foreground hover:text-foreground"
          >
            <XIcon className="h-6 w-6" />
          </button>
        </DialogHeader>

        <div className="mb-6">
          <label className="text-sm font-medium text-foreground mb-2 block">
            Choose a template (optional)
          </label>
          <div className="grid grid-cols-2 gap-2 sm:grid-cols-3">
            {GOAL_TEMPLATES.map((template) => (
              <button
                key={template.id}
                type="button"
                onClick={() => handleTemplateSelect(template)}
                className={cn(
                  'py-2 px-3 border border-border rounded-lg bg-muted text-muted-foreground cursor-pointer transition-all text-center hover:border-primary hover:bg-card',
                  selectedTemplate === template.id && 'selected border-primary bg-primary/10 text-primary'
                )}
              >
                <span className="text-sm font-medium">{template.name}</span>
              </button>
            ))}
          </div>
        </div>

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

          <SelectField
            name="account_id"
            label="Account"
            value={formData.account_id}
            onChange={handleInputChange}
            error={errors.account_id}
            disabled={accountsLoading}
          >
            <option value="">
              {accountsLoading ? 'Loading accounts...' : 'Select an account'}
            </option>
            {accounts.map((account) => (
              <option key={account.id} value={account.id} data-ph-mask>
                {account.name}
                {account.mask ? ` (••••${account.mask})` : ''}
                {account.institution_name ? ` - ${account.institution_name}` : ''}
              </option>
            ))}
          </SelectField>
          {accounts.length === 0 && !accountsLoading && (
            <p className="mt-1 text-sm text-warning">
              No savings accounts found. Link an account first.
            </p>
          )}

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
            onChange={handleInputChange}
          >
            {(Object.entries(PROGRESS_BASIS_LABELS) as [ProgressBasis, string][]).map(([value, label]) => (
              <option key={value} value={value}>{label}</option>
            ))}
          </SelectField>

          {formData.progress_basis === 'account_percentage' && (
            <div>
              <label htmlFor="account_percentage" className="block text-sm font-medium text-foreground">
                Account % (e.g. 50 for 50%)
              </label>
              <div className="relative">
                <input
                  type="number"
                  id="account_percentage"
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
              {isSubmitting ? 'Creating...' : 'Create Goal'}
            </button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}
