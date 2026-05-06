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
import { useGetAccounts } from '@boostedbudget/api-client/endpoints/accounts/accounts';
import { GOAL_TEMPLATES, calculateDeadlineFromTemplate } from '../goalTemplates';
import type { GoalTemplate } from '../goalTemplates';
import type { Account } from '../../accounts/types';
import type { SavingsGoal, CreateSavingsGoalRequest } from '../types';
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
}

interface FormErrors {
  name?: string;
  target_amount?: string;
  account_id?: string;
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
    data: accountsData,
    isLoading: accountsLoading,
  } = useGetAccounts(undefined, {
    query: {
      enabled: isOpen,
    },
  });

  const createSavingsGoalMutation = useCreateSavingsGoal();

  const accounts: Account[] = (accountsData?.accounts ?? [])
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

    if (!formData.account_id) {
      newErrors.account_id = 'Please select an account';
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
        const request: CreateSavingsGoalRequest = {
          name: formData.name.trim(),
          description: formData.description.trim() || undefined,
          target_amount: parseFloat(formData.target_amount),
          account_id: formData.account_id,
          deadline: formData.deadline ? new Date(formData.deadline).toISOString() : undefined,
        };

        const newGoal = await createSavingsGoalMutation.mutateAsync({ data: request });
        onGoalCreated(newGoal as SavingsGoal);
      } catch (err) {
        setApiError(err instanceof Error ? err.message : 'Failed to create goal');
      }
    },
    [formData, validate, onGoalCreated, createSavingsGoalMutation]
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
              className="w-full py-[0.625rem] px-3 mt-1 border border-input rounded-lg text-sm bg-card text-foreground transition-[border-color,box-shadow] focus:outline-none focus:border-primary focus:ring-2 focus:ring-primary/10 placeholder:text-muted-foreground"
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
              <option key={account.id} value={account.id}>
                {account.name}
                {account.mask ? ` (••••${account.mask})` : ''}
                {account.institution_name ? ` - ${account.institution_name}` : ''}
              </option>
            ))}
          </SelectField>
          {accounts.length === 0 && !accountsLoading && (
            <p className="mt-1 text-sm text-amber-600 dark:text-amber-400">
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

          {apiError && (
            <div className="p-3 rounded-lg bg-red-50 dark:bg-red-900/30 text-red-600 dark:text-red-400 text-sm">
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
              className="px-4 py-2 text-sm font-medium text-white bg-blue-600 rounded-lg hover:bg-blue-700 disabled:opacity-50 disabled:cursor-not-allowed transition-colors"
            >
              {isSubmitting ? 'Creating...' : 'Create Goal'}
            </button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}
